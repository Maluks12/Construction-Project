package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"construction_db/internal/core/files"
	"construction_db/internal/core/onboarding"
	"construction_db/internal/core/workers"
)

func decodeJSON[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return out
}

type declared struct {
	id      string
	content []byte
	upload  *files.File
}

// declareBatch declares PDFs of the given purposes in one request and
// returns them with their uploads.
func declareBatch(t *testing.T, router http.Handler, token string, purposes ...string) []declared {
	t.Helper()
	var items []map[string]any
	var out []declared
	for _, p := range purposes {
		d := declared{id: uuid.NewString()}
		d.content = testPDF(d.id)
		item := fileBody(d.content, "application/pdf", p, nil)
		item["file_id"] = d.id
		items = append(items, item)
		out = append(out, d)
	}
	rec := do(t, router, http.MethodPost, "/v1/files/batch", token, map[string]any{"files": items})
	wantStatus(t, rec, http.StatusOK)
	res := decodeJSON[files.BatchDeclareResponse](t, rec)
	for i, r := range res.Results {
		if r.Status != http.StatusCreated || r.File == nil || r.File.Upload == nil || r.FileID != out[i].id {
			t.Fatalf("declaration %d: %+v", i, r)
		}
		out[i].upload = r.File
	}
	return out
}

func (d declared) finish(t *testing.T, router http.Handler, token string) {
	t.Helper()
	putUpload(t, d.upload.Upload, d.content)
	wantStatus(t, do(t, router, http.MethodPost, filePath(d.id)+"/complete", token, nil), http.StatusCreated)
}

// ONBOARD D11: declare many, upload each, then save every record or none.
func TestBulkDocuments(t *testing.T) {
	f := newEvidenceFixture(t, "bulkdocs")
	r := f.router
	clk := newSASTClock(t)
	w1 := newWorker(t, r, f.owner, map[string]any{"name": "Bulk One"})
	w2 := newWorker(t, r, f.owner, map[string]any{"name": "Bulk Two"})

	t.Run("the batch declare answers each file as its own PUT", func(t *testing.T) {
		wantError(t, do(t, r, http.MethodPost, "/v1/files/batch", f.owner, map[string]any{"files": []any{}}), 400, files.ErrBatchSize.Error())
		good := fileBody(testPDF("ok"), "application/pdf", files.PurposeCompetency, nil)
		good["file_id"] = uuid.NewString()
		bad := fileBody(testPDF("bad"), "text/plain", files.PurposeCompetency, nil)
		bad["file_id"] = uuid.NewString()
		vault := fileBody(testPDF("vault"), "application/pdf", files.PurposeOrgDocument, nil)
		vault["file_id"] = uuid.NewString()
		res := decodeJSON[files.BatchDeclareResponse](t, do(t, r, http.MethodPost, "/v1/files/batch", f.supervisor,
			map[string]any{"files": []any{good, bad, vault}}))
		got := []int{res.Results[0].Status, res.Results[1].Status, res.Results[2].Status}
		if got[0] != 201 || got[1] != 400 || got[2] != 403 || res.Results[1].Error == nil || res.Results[0].File == nil {
			t.Errorf("results %+v", res.Results)
		}
		// Sent again: the first is a repeat, with a fresh upload.
		again := decodeJSON[files.BatchDeclareResponse](t, do(t, r, http.MethodPost, "/v1/files/batch", f.supervisor,
			map[string]any{"files": []any{good}}))
		if again.Results[0].Status != 200 || again.Results[0].File.Upload == nil {
			t.Errorf("repeat %+v", again.Results[0])
		}
	})

	ds := declareBatch(t, r, f.owner, files.PurposeCompetency, files.PurposeCompetency, files.PurposeOrgDocument, files.PurposeMedical)
	for _, d := range ds[:3] {
		d.finish(t, r, f.owner)
	}
	compID, comp2ID, medID, docID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	items := func(typeKey string) []map[string]any {
		return []map[string]any{
			{"kind": "competency", "id": compID, "worker_id": w1, "competency": competencyBody(clk, ds[0].id, clk.Day(-300), clk.Day(60))},
			{"kind": "competency", "id": comp2ID, "worker_id": w2, "competency": with(competencyBody(clk, ds[1].id, clk.Day(-10), clk.Day(355)), "type_key", typeKey)},
			{"kind": "medical", "id": medID, "worker_id": w1, "medical": medicalBody(clk, ds[3].id, "fit", clk.Day(-5), clk.Day(360))},
			{"kind": "vault_document", "id": docID, "document": map[string]any{"regime_code": "za_construction_2014",
				"type_key": "coida_letter_of_good_standing", "title": "LGS " + clk.Today(),
				"version": versionReq(ds[2].id, map[string]any{"issuer": "FEM", "expires_on": clk.Day(30)})}},
		}
	}
	nothingSaved := func(t *testing.T) {
		t.Helper()
		var n int
		if err := testAdmin.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM worker_competencies WHERE id = ANY ($1::uuid[]))
			+ (SELECT count(*) FROM worker_medicals WHERE id = ANY ($1::uuid[])) + (SELECT count(*) FROM documents WHERE id = ANY ($1::uuid[]))`,
			[]string{compID, comp2ID, medID, docID}).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("%d records were saved", n)
		}
	}
	good := items("rope_access_licence") // once: versionReq stamps the time
	post := func(query string, items []map[string]any) *httptest.ResponseRecorder {
		return do(t, r, http.MethodPost, "/v1/onboarding/documents"+query, f.owner, map[string]any{"items": items})
	}

	t.Run("a file not verified yet: 409, nothing saved", func(t *testing.T) {
		rec := post("", good)
		wantStatus(t, rec, http.StatusConflict)
		res := decodeJSON[onboarding.DocumentsResult](t, rec)
		if res.Error != onboarding.ErrFilesNotReady.Error() || len(res.Problems) != 1 || res.Problems[0].Index != 2 ||
			res.Problems[0].Error != files.ErrNotVerified.Error() {
			t.Errorf("result %+v", res)
		}
		nothingSaved(t)
	})
	ds[3].finish(t, r, f.owner)

	t.Run("one bad item: 400 with every problem, nothing saved", func(t *testing.T) {
		bad := items("no_such_type")
		bad = append(bad, map[string]any{"kind": "vault_document", "id": uuid.NewString(), "worker_id": w1,
			"document": map[string]any{"type_key": "x"}}, map[string]any{"kind": "photo", "id": uuid.NewString()},
			map[string]any{"kind": "competency", "id": compID, "worker_id": w2, "competency": competencyBody(clk, ds[0].id, clk.Day(-1), clk.Day(1))})
		rec := post("", bad)
		wantStatus(t, rec, http.StatusBadRequest)
		res := decodeJSON[onboarding.DocumentsResult](t, rec)
		var got []string
		for _, p := range res.Problems {
			field := ""
			if p.Field != nil {
				field = *p.Field
			}
			got = append(got, itoaApp(p.Index)+":"+field+":"+p.Error)
		}
		want := []string{"1:type_key:" + workers.MsgType, "4::" + onboarding.ErrPayload.Error(),
			"5::" + onboarding.ErrBadKind.Error(), "6::" + onboarding.ErrRepeatedID.Error()}
		if res.Error != onboarding.ErrHasProblems.Error() || strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("problems\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
		nothingSaved(t)
	})

	t.Run("a dry run checks and saves nothing", func(t *testing.T) {
		rec := post("?dry_run=true", good)
		wantStatus(t, rec, http.StatusOK)
		if res := decodeJSON[onboarding.DocumentsResult](t, rec); !res.DryRun || len(res.Problems) != 0 || res.Created != 4 {
			t.Errorf("dry run %+v", res)
		}
		nothingSaved(t)
	})

	t.Run("saved, then repeated", func(t *testing.T) {
		rec := post("", good)
		wantStatus(t, rec, http.StatusCreated)
		if res := decodeJSON[onboarding.DocumentsResult](t, rec); res.Created != 4 || res.Repeated != 0 {
			t.Errorf("saved %+v", res)
		}
		rec = post("", good)
		wantStatus(t, rec, http.StatusOK)
		if res := decodeJSON[onboarding.DocumentsResult](t, rec); res.Created != 0 || res.Repeated != 4 {
			t.Errorf("repeat %+v", res)
		}
		w := decodeWorker(t, do(t, r, http.MethodGet, workerPath(w1), f.owner, nil))
		if len(w.Competencies) != 1 || len(w.Medicals) != 1 {
			t.Errorf("worker one has %d competencies and %d medicals", len(w.Competencies), len(w.Medicals))
		}
	})

	t.Run("refused", func(t *testing.T) {
		wantStatus(t, do(t, r, http.MethodPost, "/v1/onboarding/documents", f.supervisor, map[string]any{"items": items("x")}), http.StatusForbidden)
		wantError(t, post("", nil), 400, onboarding.ErrBatchSize.Error())
		wantError(t, post("?dry_run=maybe", items("x")), 400, onboarding.ErrBadDryRun.Error())
	})
}

func progressOf(t *testing.T, router http.Handler, token string) onboarding.Progress {
	t.Helper()
	rec := do(t, router, http.MethodGet, "/v1/onboarding/progress", token, nil)
	wantStatus(t, rec, http.StatusOK)
	return decodeJSON[onboarding.Progress](t, rec)
}

func step(t *testing.T, p onboarding.Progress, key string) onboarding.Step {
	t.Helper()
	for _, s := range p.Steps {
		if s.Key == key {
			return s
		}
	}
	t.Fatalf("no step %s in %+v", key, p.Steps)
	return onboarding.Step{}
}

// ONBOARD D12: the wizard moves as records arrive; "in date" is the site
// regime's date (a date test: run at 00:30 and 23:30 SAST).
func TestOnboardingProgress(t *testing.T) {
	sfx := uuid.NewString()[:8]
	r := newRouter(t)
	clk := newSASTClock(t)
	owner := signup(t, r, "Progress Co", "owner@progress-"+sfx+".test")
	site := listSites(t, r, owner)[0].ID

	p := progressOf(t, r, owner)
	if p.Complete {
		t.Fatal("a new organisation is complete")
	}
	wantSteps := func(t *testing.T, p onboarding.Progress, want string) {
		t.Helper()
		var got []string
		for _, s := range p.Steps {
			mark := "-"
			if s.Done {
				mark = "done"
			}
			req := "opt"
			if s.Required {
				req = "req"
			}
			got = append(got, s.Key+":"+req+":"+mark)
		}
		if strings.Join(got, " ") != want {
			t.Errorf("steps\n%s\nwant\n%s", strings.Join(got, " "), want)
		}
	}
	wantSteps(t, p, "sites:req:done team:opt:- company_documents:req:- workers:req:- locations:opt:- worker_documents:opt:- "+
		"hs_file:req:- required_appointments:opt:- site_work:opt:-")
	if s := step(t, p, onboarding.StepCompanyDocuments); len(s.Gaps) != 2 || *s.Total != 2 || s.Count != 0 {
		t.Errorf("company documents %+v", s)
	}
	if s := step(t, p, onboarding.StepHSFile); len(s.Gaps) != 3 || *s.Gaps[0].SiteID != site {
		t.Errorf("h&s file %+v", s)
	}
	if s := step(t, p, onboarding.StepRequiredAppointments); len(s.Gaps) != 2 || s.Gaps[0].Note == nil || s.Note == nil {
		t.Errorf("appointments %+v", s)
	}
	if s := step(t, p, onboarding.StepTeam); s.Note == nil || *s.Note != onboarding.NoteTeam {
		t.Errorf("team %+v", s)
	}

	// The required records: a worker, the vault's two, the H&S file's three.
	// The Letter of Good Standing expires today: in date today.
	newWorker(t, r, owner, map[string]any{"name": "Progress Worker"})
	vault := func(typeKey string, fields map[string]any) {
		file := uploadFile(t, r, owner, files.PurposeOrgDocument, nil)
		wantStatus(t, do(t, r, http.MethodPut, vaultPath(uuid.NewString()), owner, map[string]any{
			"regime_code": "za_construction_2014", "type_key": typeKey, "title": typeKey, "version": versionReq(file, fields)}), http.StatusCreated)
	}
	vault("coida_letter_of_good_standing", map[string]any{"issuer": "FEM", "expires_on": clk.Today()})
	vault("coida_registration", nil)
	for _, key := range []string{"health_and_safety_specification", "health_and_safety_plan", "risk_assessment"} {
		file := uploadFile(t, r, owner, files.PurposeSiteDocument, &site)
		wantStatus(t, do(t, r, http.MethodPut, safetyFilePath(site, uuid.NewString()), owner, map[string]any{
			"type_key": key, "title": key, "version": versionReq(file, nil)}), http.StatusCreated)
	}
	inviteMember(t, r, owner, "she@progress-"+sfx+".test", "she_officer")

	p = progressOf(t, r, owner)
	if !p.Complete {
		t.Errorf("not complete: %+v", p.Steps)
	}
	wantSteps(t, p, "sites:req:done team:opt:done company_documents:req:done workers:req:done locations:opt:- worker_documents:opt:- "+
		"hs_file:req:done required_appointments:opt:- site_work:opt:-")

	// Tomorrow, by the site's clock, the letter has expired: not complete.
	tomorrow := routerAt(t, func() time.Time { return testClock.Now().Add(24 * time.Hour) })
	p = progressOf(t, tomorrow, owner)
	cd := step(t, p, onboarding.StepCompanyDocuments)
	if p.Complete || cd.Done || len(cd.Gaps) != 1 || *cd.Gaps[0].TypeKey != "coida_letter_of_good_standing" || cd.Gaps[0].Note == nil {
		t.Errorf("tomorrow: complete %v, company documents %+v", p.Complete, cd)
	}

	sup := inviteMember(t, r, owner, "sup@progress-"+sfx+".test", "supervisor")
	_ = sup
	wantStatus(t, do(t, r, http.MethodGet, "/v1/onboarding/progress", login(t, r, "sup@progress-"+sfx+".test"), nil), http.StatusForbidden)
}
