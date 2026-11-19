package app

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"construction_db/internal/core/files"
	"construction_db/internal/core/workers"
	"construction_db/internal/editions/construction/appointments"
)

func appointmentPath(site, id string) string {
	return "/v1/construction/sites/" + site + "/appointments/" + id
}

func TestAppointments(t *testing.T) {
	f := newEvidenceFixture(t, "appointments")
	manager := newWorker(t, f.router, f.owner, map[string]any{"name": "Site Manager"})
	next := newWorker(t, f.router, f.owner, map[string]any{"name": "Next Manager"})
	letter := uploadFile(t, f.router, f.owner, files.PurposeAppointment, &f.siteA)
	body := map[string]any{"worker_id": manager, "type_key": "construction_manager", "appointed_on": time.Now().Format(time.DateOnly),
		"file_id": letter, "client_recorded_at": rfc(time.Now())}

	id := uuid.NewString()
	wantError(t, do(t, f.router, http.MethodPut, appointmentPath(f.siteA, id), f.supervisor, body), http.StatusForbidden,
		"only an owner or SHE officer can record or retract appointments")
	wantError(t, do(t, f.router, http.MethodPut, appointmentPath(f.siteA, id), f.owner, with(body, "type_key", "tea_maker")), http.StatusBadRequest,
		"type_key is not an appointment type of this site's regime")
	rec := do(t, f.router, http.MethodPut, appointmentPath(f.siteA, id), f.owner, body)
	wantStatus(t, rec, http.StatusCreated)
	var a appointments.Appointment
	_ = json.Unmarshal(rec.Body.Bytes(), &a)
	if a.WorkerName != "Site Manager" || a.State != workers.StateValid || a.SiteID != f.siteA {
		t.Errorf("appointed %+v", a)
	}
	wantStatus(t, do(t, f.router, http.MethodPut, appointmentPath(f.siteA, id), f.owner, body), http.StatusOK)

	// The next construction manager supersedes; the first stays, superseded.
	letter2 := uploadFile(t, f.router, f.owner, files.PurposeAppointment, &f.siteA)
	wantStatus(t, do(t, f.router, http.MethodPut, appointmentPath(f.siteA, uuid.NewString()), f.owner,
		with(with(with(body, "worker_id", next), "file_id", letter2), "supersedes_id", id)), http.StatusCreated)
	rec = do(t, f.router, http.MethodGet, "/v1/construction/sites/"+f.siteA+"/appointments", f.supervisor, nil)
	wantStatus(t, rec, http.StatusOK)
	var list struct{ Appointments []appointments.Appointment }
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Appointments) != 2 || list.Appointments[0].State != workers.StateSuperseded {
		t.Errorf("appointments %+v", list.Appointments)
	}
	// The letter opens for the site's supervisor, logged; another site's letter can't be used.
	wantStatus(t, do(t, f.router, http.MethodPost, appointmentPath(f.siteA, id)+"/view", f.supervisor, nil), http.StatusOK)
	other := uploadFile(t, f.router, f.owner, files.PurposeAppointment, &f.siteB)
	wantError(t, do(t, f.router, http.MethodPut, appointmentPath(f.siteA, uuid.NewString()), f.owner, with(body, "file_id", other)),
		http.StatusBadRequest, "file was uploaded for another purpose")
	wantError(t, do(t, f.router, http.MethodGet, "/v1/construction/sites/"+f.siteB+"/appointments", f.supervisor, nil), http.StatusNotFound, "site not found")
	wantStatus(t, do(t, f.router, http.MethodPost, appointmentPath(f.siteA, id)+"/retract", f.owner, reason("Recorded on the wrong site")), http.StatusCreated)
}
