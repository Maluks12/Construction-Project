package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"construction_db/internal/core/files"
	"construction_db/internal/core/workers"
)

func workerPath(id string) string { return "/v1/workers/" + id }

func decodeWorker(t *testing.T, rec *httptest.ResponseRecorder) workers.WorkerDetail {
	t.Helper()
	var v workers.WorkerDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode worker: %v (body: %s)", err, rec.Body.String())
	}
	return v
}

func newWorker(t *testing.T, router http.Handler, token string, body map[string]any) string {
	t.Helper()
	id := uuid.NewString()
	wantStatus(t, do(t, router, http.MethodPut, workerPath(id), token, body), http.StatusCreated)
	return id
}

func competencyBody(file, issued string, expires any) map[string]any {
	return map[string]any{"regime_code": "za_construction_2014", "type_key": "rope_access_licence", "issued_on": issued,
		"expires_on": expires, "file_id": file, "client_recorded_at": rfc(time.Now())}
}

func medicalBody(file, outcome, examined, expires string) map[string]any {
	return map[string]any{"regime_code": "za_construction_2014", "outcome": outcome, "examined_on": examined, "expires_on": expires,
		"practice_number": "MP 0123456", "file_id": file, "client_recorded_at": rfc(time.Now())}
}

func wantField(t *testing.T, rec *httptest.ResponseRecorder, field, msg string) {
	t.Helper()
	var body struct{ Error, Field string }
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusBadRequest || body.Field != field || body.Error != msg {
		t.Errorf("got %d %s; want 400 field %q %q", rec.Code, rec.Body.String(), field, msg)
	}
}

func TestWorkerRegister(t *testing.T) {
	f := newEvidenceFixture(t, "workers")
	id := uuid.NewString()
	body := map[string]any{"name": "Thabo Mokoena", "firm": "ABC Builders", "trade": "Scaffolder"}

	rec := do(t, f.router, http.MethodPut, workerPath(id), f.supervisor, body)
	wantStatus(t, rec, http.StatusCreated)
	if w := decodeWorker(t, rec); !w.Active || w.ClockNumber != nil {
		t.Errorf("created %+v", w)
	}
	wantStatus(t, do(t, f.router, http.MethodPut, workerPath(id), f.supervisor, body), http.StatusOK)
	// Supervisors add people; only leads edit them.
	wantError(t, do(t, f.router, http.MethodPut, workerPath(id), f.supervisor, with(body, "trade", "Rigger")), http.StatusForbidden,
		"only an owner or SHE officer can edit a worker")
	rec = do(t, f.router, http.MethodPut, workerPath(id), f.owner, with(body, "active", false))
	wantStatus(t, rec, http.StatusOK)
	if w := decodeWorker(t, rec); w.Active || w.UpdatedBy == nil {
		t.Errorf("edited %+v", w)
	}

	// The same person under another id: 409 naming the worker on the register.
	rec = do(t, f.router, http.MethodPut, workerPath(uuid.NewString()), f.supervisor, map[string]any{"name": " thabo  MOKOENA", "firm": "abc builders"})
	var clash workers.IdentityClash
	_ = json.Unmarshal(rec.Body.Bytes(), &clash)
	if rec.Code != http.StatusConflict || clash.WorkerID != id || clash.Error != workers.ErrIdentityTaken.Error() {
		t.Errorf("clash: %d %s", rec.Code, rec.Body.String())
	}
	// A clock number makes another person.
	newWorker(t, f.router, f.supervisor, with(body, "clock_number", "C-17"))

	wantField(t, do(t, f.router, http.MethodPut, workerPath(uuid.NewString()), f.owner, map[string]any{"name": " "}), "name", workers.MsgName)
	wantField(t, do(t, f.router, http.MethodPut, workerPath(uuid.NewString()), f.owner, map[string]any{"name": "A", "clock_number": ""}),
		"clock_number", workers.MsgClock)
	wantError(t, do(t, f.router, http.MethodPut, workerPath("x"), f.owner, body), http.StatusBadRequest, "worker id must be a UUID")

	list := func(token, query string) []workers.Worker {
		t.Helper()
		rec := do(t, f.router, http.MethodGet, "/v1/workers"+query, token, nil)
		wantStatus(t, rec, http.StatusOK)
		var b struct{ Workers []workers.Worker }
		_ = json.Unmarshal(rec.Body.Bytes(), &b)
		return b.Workers
	}
	if ws := list(f.supervisor, "?active=true"); len(ws) != 1 || ws[0].ClockNumber == nil {
		t.Errorf("active workers %+v", ws)
	}
	if ws := list(f.owner, "?q=MOKOENA"); len(ws) != 2 {
		t.Errorf("search found %d", len(ws))
	}
	if ws := list(f.other, ""); len(ws) != 0 {
		t.Errorf("another org sees %d workers", len(ws))
	}
	wantError(t, do(t, f.router, http.MethodGet, "/v1/workers?active=maybe", f.owner, nil), http.StatusBadRequest, "active filter must be true or false")
	wantError(t, do(t, f.router, http.MethodGet, workerPath(id), f.other, nil), http.StatusNotFound, "worker not found")
	wantStatus(t, do(t, f.router, http.MethodGet, "/v1/workers", f.bookkeeper, nil), http.StatusForbidden)

	// The regime's types.
	rec = do(t, f.router, http.MethodGet, "/v1/regimes/za_construction_2014/types", f.supervisor, nil)
	wantStatus(t, rec, http.StatusOK)
	var types workers.RegimeTypes
	_ = json.Unmarshal(rec.Body.Bytes(), &types)
	if len(types.CompetencyTypes) == 0 || len(types.AppointmentTypes) == 0 || types.Medical.DefaultValidityMonths != 12 {
		t.Errorf("types %+v", types)
	}
	wantError(t, do(t, f.router, http.MethodGet, "/v1/regimes/nope/types", f.owner, nil), http.StatusNotFound, "regime not found")
	wantStatus(t, do(t, f.router, http.MethodGet, "/v1/regimes", f.owner, nil), http.StatusOK)
}

func TestWorkerRecords(t *testing.T) {
	f := newEvidenceFixture(t, "records")
	w := newWorker(t, f.router, f.supervisor, map[string]any{"name": "Sipho Dlamini"})
	compPath := func(id string) string { return workerPath(w) + "/competencies/" + id }
	medPath := func(id string) string { return workerPath(w) + "/medicals/" + id }
	today := time.Now().Format(time.DateOnly)
	soon := time.Now().AddDate(0, 0, 10).Format(time.DateOnly)
	later := time.Now().AddDate(1, 0, 0).Format(time.DateOnly)

	// A supervisor captures a licence on site.
	f1 := uploadFile(t, f.router, f.supervisor, files.PurposeCompetency, nil)
	c1 := uuid.NewString()
	rec := do(t, f.router, http.MethodPut, compPath(c1), f.supervisor, competencyBody(f1, today, soon))
	wantStatus(t, rec, http.StatusCreated)
	var c workers.Competency
	_ = json.Unmarshal(rec.Body.Bytes(), &c)
	if c.State != workers.StateExpiring {
		t.Errorf("a licence expiring in 10 days is %q", c.State)
	}
	// The licence type needs an expiry date.
	wantField(t, do(t, f.router, http.MethodPut, compPath(uuid.NewString()), f.owner, competencyBody(f1, today, nil)), "expires_on", workers.MsgExpiryNeed)
	wantField(t, do(t, f.router, http.MethodPut, compPath(uuid.NewString()), f.owner, with(competencyBody(f1, today, soon), "type_key", "astronaut")),
		"type_key", workers.MsgType)
	// A file is one record's, of its own purpose, and verified.
	wantError(t, do(t, f.router, http.MethodPut, compPath(uuid.NewString()), f.owner, competencyBody(f1, today, soon)),
		http.StatusConflict, "file already attached to another record")
	medFile := uploadFile(t, f.router, f.supervisor, files.PurposeMedical, nil)
	wantError(t, do(t, f.router, http.MethodPut, compPath(uuid.NewString()), f.owner, competencyBody(medFile, today, soon)),
		http.StatusBadRequest, "file was uploaded for another purpose")

	// Renewal: a new record superseding the old; nothing overwritten.
	f2 := uploadFile(t, f.router, f.owner, files.PurposeCompetency, nil)
	c2 := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, compPath(c2), f.owner, with(competencyBody(f2, today, later), "supersedes_id", c1)), http.StatusCreated)
	f3 := uploadFile(t, f.router, f.owner, files.PurposeCompetency, nil)
	wantError(t, do(t, f.router, http.MethodPut, compPath(uuid.NewString()), f.owner, with(competencyBody(f3, today, later), "supersedes_id", c1)),
		http.StatusConflict, "that record already has a renewal")

	// Retraction: leads only, once, kept.
	wantError(t, do(t, f.router, http.MethodPost, compPath(c2)+"/retract", f.supervisor, reason("Wrong person")), http.StatusForbidden,
		"only an owner or SHE officer can retract a record")
	wantStatus(t, do(t, f.router, http.MethodPost, compPath(c2)+"/retract", f.owner, reason("Wrong person")), http.StatusCreated)
	wantStatus(t, do(t, f.router, http.MethodPost, compPath(c2)+"/retract", f.owner, reason("Wrong person")), http.StatusOK)
	wantError(t, do(t, f.router, http.MethodPost, compPath(c2)+"/retract", f.owner, reason("Other")), http.StatusConflict,
		"record already retracted with another reason")

	d := decodeWorker(t, do(t, f.router, http.MethodGet, workerPath(w), f.supervisor, nil))
	if len(d.Competencies) != 2 || d.Competencies[0].State != workers.StateExpiring || d.Competencies[1].State != workers.StateRetracted {
		t.Errorf("competencies %+v", d.Competencies)
	}

	// The medical: outcome and dates for everyone; the certificate for leads.
	m1 := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, medPath(m1), f.supervisor, medicalBody(medFile, "fit_with_restrictions", today, later)), http.StatusCreated)
	wantField(t, do(t, f.router, http.MethodPut, medPath(uuid.NewString()), f.owner, medicalBody(medFile, "fit_for_heights", today, later)),
		"outcome", workers.MsgOutcome)
	wantField(t, do(t, f.router, http.MethodPut, medPath(uuid.NewString()), f.owner, with(medicalBody(medFile, "fit", today, later), "practice_number", "no heights: epilepsy")),
		"practice_number", workers.MsgPractice)
	wantError(t, do(t, f.router, http.MethodPost, medPath(m1)+"/view", f.supervisor, nil), http.StatusForbidden,
		"only an owner or SHE officer can open a medical certificate")
	if n := countRows(t, `SELECT count(*) FROM file_access_log WHERE file_id = $1`, medFile); n != 0 {
		t.Errorf("a refused view logged %d rows", n)
	}
	wantStatus(t, do(t, f.router, http.MethodPost, medPath(m1)+"/view", f.owner, nil), http.StatusOK)
	if n := countRows(t, `SELECT count(*) FROM file_access_log WHERE file_id = $1`, medFile); n != 1 {
		t.Errorf("the owner's view logged %d rows, want 1", n)
	}
	// A certificate of competence opens for a supervisor, logged.
	wantStatus(t, do(t, f.router, http.MethodPost, compPath(c1)+"/view", f.supervisor, nil), http.StatusOK)
	if n := countRows(t, `SELECT count(*) FROM file_access_log WHERE file_id = $1`, f1); n != 1 {
		t.Errorf("the supervisor's view logged %d rows, want 1", n)
	}
	wantError(t, do(t, f.router, http.MethodPost, compPath(m1)+"/view", f.owner, nil), http.StatusNotFound, "record not found")
}

func TestSignatureWorkerLinks(t *testing.T) {
	f := newEvidenceFixture(t, "links")
	w := newWorker(t, f.router, f.supervisor, map[string]any{"name": "Sipho Dlamini"})
	talk := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, talkPath(f.siteA, talk), f.supervisor, talkBody(nil)), http.StatusCreated)

	// Signing with the register's name links it; another key doesn't.
	att := uuid.NewString()
	rec := do(t, f.router, http.MethodPut, signPath(f.siteA, talk, att), f.supervisor, signBody("SIPHO dlamini", signAt, nil))
	wantStatus(t, rec, http.StatusCreated)
	if a := decodeAttendance(t, rec); a.WorkerID == nil || *a.WorkerID != w {
		t.Errorf("signature worker_id %v, want %s", a.WorkerID, w)
	}
	other := uuid.NewString()
	rec = do(t, f.router, http.MethodPut, signPath(f.siteA, talk, other), f.supervisor, signBody("Sipho Dlamini", signAt, map[string]any{"clock_number": "C-9"}))
	if a := decodeAttendance(t, rec); a.WorkerID != nil {
		t.Errorf("a different clock number matched worker %v", *a.WorkerID)
	}

	linkPath := signPath(f.siteA, talk, other) + "/worker"
	wantError(t, do(t, f.router, http.MethodPut, linkPath, f.supervisor, map[string]any{"worker_id": w}), http.StatusForbidden,
		"only an owner or SHE officer can link or unlink a signature")
	wantStatus(t, do(t, f.router, http.MethodPut, linkPath, f.owner, map[string]any{"worker_id": w}), http.StatusCreated)
	wantStatus(t, do(t, f.router, http.MethodPut, linkPath, f.owner, map[string]any{"worker_id": w}), http.StatusOK)

	// "Different person": unlinked, it's no longer counted as the worker.
	wantStatus(t, do(t, f.router, http.MethodPost, signPath(f.siteA, talk, att)+"/worker/unlink", f.owner, reason("Different person")), http.StatusOK)
	tk := decodeTalk(t, do(t, f.router, http.MethodGet, talkPath(f.siteA, talk), f.owner, nil))
	for _, a := range tk.Attendance {
		switch a.ID {
		case att:
			if a.WorkerID != nil {
				t.Errorf("unlinked signature still counts as %v", *a.WorkerID)
			}
		case other:
			if a.WorkerID == nil || *a.WorkerID != w {
				t.Errorf("manually linked signature worker_id %v", a.WorkerID)
			}
		}
	}
	wantError(t, do(t, f.router, http.MethodPost, signPath(f.siteA, talk, att)+"/worker/unlink", f.owner, reason("Again")), http.StatusConflict,
		"signature is not linked to a worker")
	wantError(t, do(t, f.router, http.MethodPut, signPath(f.siteB, talk, other)+"/worker", f.owner, map[string]any{"worker_id": w}),
		http.StatusNotFound, "attendance not found")
}
