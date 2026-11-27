package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"construction_db/internal/core/workers"
	"construction_db/internal/platform/httpapi"
)

func importPath(id string) string { return "/v1/worker-imports/" + id }

// sendCSV PUTs body as a CSV file.
func sendCSV(router http.Handler, token, path, contentType string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func decodeImport(t *testing.T, rec *httptest.ResponseRecorder) workers.ImportResult {
	t.Helper()
	var out workers.ImportResult
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode import %s: %v", rec.Body.String(), err)
	}
	return out
}

func registerSize(t *testing.T, org string) int {
	t.Helper()
	var n int
	if err := testAdmin.QueryRow(context.Background(), `SELECT count(*) FROM workers WHERE org_id = $1`, org).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// ONBOARD D9, D10: the register from a spreadsheet, all or nothing.
func TestWorkerImport(t *testing.T) {
	f := newEvidenceFixture(t, "import")
	r := f.router
	org := f.ownerMe.OrgID
	newWorker(t, r, f.owner, map[string]any{"name": "Already Here", "firm": "Bouwerk"})
	before := registerSize(t, org)

	problems := func(res workers.ImportResult) string {
		var out []string
		for _, p := range res.Problems {
			out = append(out, itoaApp(p.Row)+":"+p.Field+":"+p.Error)
		}
		return strings.Join(out, "\n")
	}

	t.Run("one bad row writes nothing", func(t *testing.T) {
		// Five good rows and one the database itself would accept: only the
		// import's own check refuses it, so this proves the all-or-nothing.
		file := "Name;Firm\r\nAna One;A\r\nBen Two;B\r\nCara Three;C,\r\nDan Four;D;stray cell\r\nEve Five;E\r\nFay Six;F\r\n"
		rec := sendCSV(r, f.owner, importPath(uuid.NewString()), "text/csv", []byte(file))
		wantStatus(t, rec, http.StatusBadRequest)
		res := decodeImport(t, rec)
		if res.Error != workers.ErrImportHasProblems.Error() || problems(res) != "5::"+workers.MsgExtraCells {
			t.Errorf("error %q, problems\n%s", res.Error, problems(res))
		}
		if n := registerSize(t, org); n != before {
			t.Fatalf("the register went from %d to %d workers", before, n)
		}
	})

	t.Run("every row's problems at once", func(t *testing.T) {
		file := "name,firm,clock_number\n" +
			"Sipho Dlamini,Bouwerk,C1\n" + // row 2: fine
			",Bouwerk,C2\n" + // row 3: no name
			"Sipho Dlamini,Bouwerk,C1\n" + // row 4: as row 2
			"Already Here,Bouwerk,\n" + // row 5: on the register
			"Long Clock,," + strings.Repeat("9", 51) + "\n" // row 6
		rec := sendCSV(r, f.owner, importPath(uuid.NewString()), "text/csv", []byte(file))
		wantStatus(t, rec, http.StatusBadRequest)
		want := strings.Join([]string{
			"3:name:" + workers.MsgName,
			"4:name:" + workers.MsgSameAsRow + "2",
			"5:name:" + workers.ErrIdentityTaken.Error(),
			"6:clock_number:" + workers.MsgClock,
		}, "\n")
		if got := problems(decodeImport(t, rec)); got != want {
			t.Errorf("problems\n%s\nwant\n%s", got, want)
		}
		if n := registerSize(t, org); n != before {
			t.Fatalf("the register went from %d to %d workers", before, n)
		}
	})

	// Plain "CSV" from South African Excel: Windows-1252, semicolons, CRLF.
	file := []byte("Full name;Company;Clock No.;Trade\r\nG\xebrhard Esterhuyse;Bouwerk M\xe9;E7;Rigger\r\nThabo Mokoena;;;\r\n;;;\r\n")
	id := uuid.NewString()

	t.Run("a dry run reads and writes nothing", func(t *testing.T) {
		rec := sendCSV(r, f.owner, importPath(id)+"?dry_run=true", "text/csv", file)
		wantStatus(t, rec, http.StatusOK)
		res := decodeImport(t, rec)
		if !res.DryRun || res.Encoding != workers.EncodingWindows1252 || res.Separator != ";" || res.RowCount != 2 ||
			res.Rows[0].Name != "Gërhard Esterhuyse" || *res.Rows[0].Firm != "Bouwerk Mé" || res.Rows[0].WorkerID != nil ||
			!slices.Equal(res.Columns, []string{"name", "firm", "clock_number", "trade"}) {
			t.Errorf("dry run %+v", res)
		}
		if n := registerSize(t, org); n != before {
			t.Fatalf("a dry run wrote %d workers", n-before)
		}
	})

	var ids []string
	t.Run("import", func(t *testing.T) {
		rec := sendCSV(r, f.owner, importPath(id), "text/csv; charset=windows-1252", file)
		wantStatus(t, rec, http.StatusCreated)
		res := decodeImport(t, rec)
		for _, row := range res.Rows {
			if row.WorkerID == nil {
				t.Fatalf("row %d has no worker", row.Row)
			}
			ids = append(ids, *row.WorkerID)
		}
		if n := registerSize(t, org); n != before+2 {
			t.Fatalf("the register has %d workers, want %d", n, before+2)
		}
		w := decodeWorker(t, do(t, r, http.MethodGet, workerPath(ids[0]), f.supervisor, nil))
		if w.Name != "Gërhard Esterhuyse" || w.ClockNumber == nil || *w.ClockNumber != "E7" {
			t.Errorf("imported worker %+v", w)
		}
	})

	t.Run("the same file again under its id", func(t *testing.T) {
		rec := sendCSV(r, f.owner, importPath(id), "text/csv", file)
		wantStatus(t, rec, http.StatusOK)
		var again []string
		for _, row := range decodeImport(t, rec).Rows {
			again = append(again, *row.WorkerID)
		}
		if !slices.Equal(again, ids) || registerSize(t, org) != before+2 {
			t.Errorf("repeat gave %v, first %v", again, ids)
		}
		wantError(t, sendCSV(r, f.owner, importPath(id), "text/csv", []byte("name\nSomeone Else\n")), 409, workers.ErrImportIDReused.Error())
	})

	t.Run("refused", func(t *testing.T) {
		wantStatus(t, sendCSV(r, f.supervisor, importPath(uuid.NewString()), "text/csv", []byte("name\nX\n")), http.StatusForbidden)
		wantError(t, sendCSV(r, f.owner, importPath(uuid.NewString()), "application/json", []byte(`{"name":"X"}`)), 415, workers.ErrNotCSV.Error())
		wantError(t, sendCSV(r, f.owner, importPath("nope"), "text/csv", []byte("name\nX\n")), 400, workers.ErrBadImportID.Error())
		wantError(t, sendCSV(r, f.owner, importPath(uuid.NewString())+"?dry_run=yes", "text/csv", []byte("name\nX\n")), 400, workers.ErrBadDryRun.Error())
		wantError(t, sendCSV(r, f.owner, importPath(uuid.NewString()), "text/csv", []byte("name\nNUL\x00here\n")), 400, workers.ErrNotText.Error())
		rec := sendCSV(r, f.owner, importPath(uuid.NewString()), "text/csv", []byte("name,ID number\nX,800101\n"))
		var fp workers.ImportFileProblem
		_ = json.Unmarshal(rec.Body.Bytes(), &fp)
		if rec.Code != 400 || fp.Error != workers.ErrUnknownColumn.Error() || fp.Column == nil || *fp.Column != "ID number" {
			t.Errorf("unknown column: %d %s", rec.Code, rec.Body.String())
		}
		big := "name\n" + strings.Repeat("Somebody With A Long Enough Name To Fill The File\n", 21000)
		wantStatus(t, sendCSV(r, f.owner, importPath(uuid.NewString()), "text/csv", []byte(big)), http.StatusRequestEntityTooLarge)
	})
}

func itoaApp(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// ONBOARD review R5, R6: the body limit is the shared 413, and what Windows
// with Excel calls a .csv is accepted; a real .xls under that name is not
// text.
func TestWorkerImportLimitsAndTypes(t *testing.T) {
	f := newEvidenceFixture(t, "importlimits")
	r := f.router
	csv := []byte("name\nExcel Worker\n")

	over := append([]byte("name\n"), bytes.Repeat([]byte("x"), httpapi.MaxBody+1-len("name\n"))...)
	if len(over) != httpapi.MaxBody+1 {
		t.Fatalf("body is %d bytes", len(over))
	}
	wantError(t, sendCSV(r, f.owner, importPath(uuid.NewString()), "text/csv", over), http.StatusRequestEntityTooLarge, httpapi.TooLargeText)
	// Exactly 1 MiB passes the limit (and is then read as a file).
	exact := over[:httpapi.MaxBody]
	if rec := sendCSV(r, f.owner, importPath(uuid.NewString())+"?dry_run=true", "text/csv", exact); rec.Code == http.StatusRequestEntityTooLarge {
		t.Errorf("exactly 1 MiB: 413")
	}

	rec := sendCSV(r, f.owner, importPath(uuid.NewString())+"?dry_run=true", "application/vnd.ms-excel", csv)
	wantStatus(t, rec, http.StatusOK)
	if res := decodeImport(t, rec); res.RowCount != 1 || res.Rows[0].Name != "Excel Worker" {
		t.Errorf("as application/vnd.ms-excel: %+v", res)
	}
	xls := append([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, bytes.Repeat([]byte{0}, 504)...)
	wantError(t, sendCSV(r, f.owner, importPath(uuid.NewString()), "application/vnd.ms-excel", xls), http.StatusBadRequest, workers.ErrNotText.Error())
	wantError(t, sendCSV(r, f.owner, importPath(uuid.NewString()), "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", csv),
		http.StatusUnsupportedMediaType, workers.ErrNotCSV.Error())
}
