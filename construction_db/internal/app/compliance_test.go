package app

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"construction_db/internal/core/files"
	"construction_db/internal/core/reports"
	"construction_db/internal/platform/clock"
)

func compliancePath(site string) string { return "/v1/sites/" + site + "/compliance" }

// routerAt is a router whose services read now from c.
func routerAt(t *testing.T, c clock.Clock) *gin.Engine {
	t.Helper()
	opts := testHTTP
	opts.Clock = c
	router, err := NewRouter(context.Background(), testPool, testSecret, testRegistry, testEdition, testStore, opts)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	return router
}

func siteSummary(t *testing.T, router http.Handler, token, site string) reports.SiteSummary {
	t.Helper()
	rec := do(t, router, http.MethodGet, compliancePath(site), token, nil)
	wantStatus(t, rec, http.StatusOK)
	var s reports.SiteSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func rollup(t *testing.T, router http.Handler, token string) reports.Rollup {
	t.Helper()
	rec := do(t, router, http.MethodGet, "/v1/compliance", token, nil)
	wantStatus(t, rec, http.StatusOK)
	var r reports.Rollup
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func category(t *testing.T, s reports.SiteSummary, key string) reports.Category {
	t.Helper()
	for _, c := range s.Categories {
		if c.Key == key {
			return c
		}
	}
	t.Fatalf("no category %q in %+v", key, s.Categories)
	return reports.Category{}
}

// states lists a category's items as "state/colour/label" for comparison.
func states(c reports.Category) []string {
	out := []string{}
	for _, it := range c.Items {
		col := "-"
		if it.Colour != nil {
			col = *it.Colour
		}
		out = append(out, it.State+"/"+col+"/"+it.Label)
	}
	return out
}

func wantCategory(t *testing.T, s reports.SiteSummary, key, status string, items ...string) reports.Category {
	t.Helper()
	c := category(t, s, key)
	got := "-"
	if c.Status != nil {
		got = *c.Status
	}
	if got != status || c.Count != len(items) || !slices.Equal(states(c), items) {
		t.Errorf("%s: got status %s, items %q; want %s, %q", key, got, states(c), status, items)
	}
	return c
}

// backdate moves a pending upload's declaration back, past the triggers
// (only a test can).
func backdate(t *testing.T, table, id string, by time.Duration) {
	t.Helper()
	ctx := context.Background()
	tx, err := testAdmin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
		t.Fatal(err)
	}
	tag, err := tx.Exec(ctx, `UPDATE `+table+` SET created_at = now() - $2::interval WHERE id = $1`, id, by.String())
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("backdate %s %s: %v", table, id, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

// The site compliance summary (M12 D1-D4): every category with its colour
// rule, "today" in the site's zone, items linked to their records, and the
// rollup's order and scope.
func TestComplianceSummary(t *testing.T) {
	f := newActionFixture(t, "compliance")
	clk := newSASTClock(t)
	r := f.router

	// A fresh site: the failed high item is unactioned (red), the H&S file
	// misses its three required types (red), both required appointments
	// have no appointee (amber, with the caveat). The empty vault isn't
	// counted (missing vault documents aren't).
	s := siteSummary(t, r, f.owner, f.siteA)
	if s.Status != reports.Red || s.Date != clk.Today() || s.TimeZone != "Africa/Johannesburg" || s.SiteID != f.siteA {
		t.Errorf("summary header %+v", s)
	}
	fc := wantCategory(t, s, reports.CatFindings, reports.Red, "unactioned/red/"+f.tmpl.Items[0].Prompt)
	if it := fc.Items[0]; *it.InspectionID != f.inspection || *it.ItemKey != f.item || *it.Severity != "high" {
		t.Errorf("finding item %+v", it)
	}
	wantCategory(t, s, reports.CatHSFile, reports.Red, "missing/red/Client's health and safety specification",
		"missing/red/Health and safety plan", "missing/red/Risk assessment")
	ac := wantCategory(t, s, reports.CatAppointments, reports.Amber, "no_appointee/amber/Construction manager",
		"no_appointee/amber/Construction supervisor")
	if it := ac.Items[0]; it.Note == nil || *it.TypeKey != "construction_manager" || it.SubjectID != nil {
		t.Errorf("appointment item %+v", it)
	}
	wantCategory(t, s, reports.CatVault, reports.Green)
	for _, key := range []string{reports.CatActions, reports.CatVerifications, reports.CatSignIns, reports.CatIncidentDeadlines, reports.CatWorkerRecords} {
		wantCategory(t, s, key, reports.Green)
	}
	wantCategory(t, s, reports.CatAbsentWorkers, "-")
	wantCategory(t, s, reports.CatPendingUploads, "-")
	if s.RedItems != 4 || s.AmberItems != 2 {
		t.Errorf("red %d, amber %d; want 4 and 2", s.RedItems, s.AmberItems)
	}

	// Two actions on the finding: one stays open, one is closed out.
	open := f.raise(t, f.owner, f.raiseBody(f.ownerID, testClock.Now().Add(-time.Minute)))
	closed := f.raise(t, f.owner, f.raiseBody(f.ownerID, testClock.Now().Add(-time.Minute)))
	wantStatus(t, do(t, r, http.MethodPut, closeoutPath(f.siteA, closed.ID, uuid.NewString()), f.owner,
		closeoutBody(testClock.Now(), f.fixPhoto)), http.StatusCreated)

	// Someone new signs in today without a medical: a red sign-in.
	worker := uuid.NewString()
	wantStatus(t, do(t, r, http.MethodPut, signInPath(f.siteA, uuid.NewString()), f.supervisor,
		signInBody(worker, clk.Earlier(time.Minute), map[string]any{"new_worker": map[string]any{"name": "Sipho Dlamini"}})), http.StatusCreated)
	// Their licence expires in 10 days: amber on the roster.
	licence := uploadFile(t, r, f.supervisor, files.PurposeCompetency, nil)
	wantStatus(t, do(t, r, http.MethodPut, workerPath(worker)+"/competencies/"+uuid.NewString(), f.supervisor,
		competencyBody(clk, licence, clk.Today(), clk.Day(10))), http.StatusCreated)
	// Someone on the roster who hasn't signed in: shown, not counted.
	absentee := newWorker(t, r, f.owner, map[string]any{"name": "Thabo Absent"})
	wantStatus(t, do(t, r, http.MethodPut, "/v1/sites/"+f.siteA+"/roster/"+absentee, f.owner, nil), http.StatusCreated)
	// Their licence expired yesterday: not "expiring". An expired one counts
	// through a sign-in's warnings, when they are on site.
	expired := uploadFile(t, r, f.owner, files.PurposeCompetency, nil)
	wantStatus(t, do(t, r, http.MethodPut, workerPath(absentee)+"/competencies/"+uuid.NewString(), f.owner,
		competencyBody(clk, expired, clk.Day(-30), clk.Day(-1))), http.StatusCreated)
	// Taken off the roster: neither absent nor their expiring licence counts.
	gone := newWorker(t, r, f.owner, map[string]any{"name": "Gone Worker"})
	wantStatus(t, do(t, r, http.MethodPut, "/v1/sites/"+f.siteA+"/roster/"+gone, f.owner, nil), http.StatusCreated)
	goneLicence := uploadFile(t, r, f.owner, files.PurposeCompetency, nil)
	wantStatus(t, do(t, r, http.MethodPut, workerPath(gone)+"/competencies/"+uuid.NewString(), f.owner,
		competencyBody(clk, goneLicence, clk.Today(), clk.Day(5))), http.StatusCreated)
	wantStatus(t, do(t, r, http.MethodPost, "/v1/sites/"+f.siteA+"/roster/"+gone+"/remove", f.owner, reason("Left the site")), http.StatusOK)
	// A sign-in retracted as a mistake counts nowhere.
	wrong := uuid.NewString()
	wantStatus(t, do(t, r, http.MethodPut, signInPath(f.siteA, wrong), f.supervisor,
		signInBody(uuid.NewString(), clk.Earlier(time.Minute), map[string]any{"new_worker": map[string]any{"name": "Wrong Person"}})), http.StatusCreated)
	wantStatus(t, do(t, r, http.MethodPost, signInPath(f.siteA, wrong)+"/retract", f.supervisor, reason("Different person")), http.StatusCreated)
	// A construction manager appointed until 10 days from now: the type is
	// covered while the appointment is expiring, which is amber itself.
	manager := newWorker(t, r, f.owner, map[string]any{"name": "Site Manager"})
	letter := uploadFile(t, r, f.owner, files.PurposeAppointment, &f.siteA)
	wantStatus(t, do(t, r, http.MethodPut, appointmentPath(f.siteA, uuid.NewString()), f.owner, map[string]any{
		"worker_id": manager, "type_key": "construction_manager", "appointed_on": clk.Today(), "expires_on": clk.Day(10),
		"file_id": letter, "client_recorded_at": rfc(clk.Now())}), http.StatusCreated)
	// An unconscious worker: a forthwith report (red) and a 7-day notice.
	incident := uuid.NewString()
	wantStatus(t, do(t, r, http.MethodPut, versionPath(f.siteA, incident, 1), f.supervisor,
		versionBody(clk.Earlier(10*time.Minute), "unconsciousness")), http.StatusCreated)
	// The Letter of Good Standing expired yesterday: red on every site.
	lgsFile := uploadFile(t, r, f.owner, files.PurposeOrgDocument, nil)
	wantStatus(t, do(t, r, http.MethodPut, vaultPath(uuid.NewString()), f.owner, map[string]any{
		"regime_code": "za_construction_2014", "type_key": "coida_letter_of_good_standing", "title": "LGS 2025",
		"version": versionReq(lgsFile, map[string]any{"issuer": "FEM", "expires_on": clk.Day(-1)})}), http.StatusCreated)
	// Uploads declared and never completed: 25 hours ago is listed, 23 isn't.
	old, recent := uuid.NewString(), uuid.NewString()
	wantStatus(t, do(t, r, http.MethodPut, evidencePath(f.siteA, old), f.supervisor, declareBody("never sent "+old)), http.StatusCreated)
	wantStatus(t, do(t, r, http.MethodPut, evidencePath(f.siteA, recent), f.supervisor, declareBody("never sent "+recent)), http.StatusCreated)
	backdate(t, "evidence", old, 25*time.Hour)
	backdate(t, "evidence", recent, 23*time.Hour)
	orgFile := uuid.NewString()
	wantStatus(t, do(t, r, http.MethodPut, filePath(orgFile), f.supervisor,
		fileBody(testPDF(orgFile), "application/pdf", files.PurposeCompetency, nil)), http.StatusCreated)
	backdate(t, "files", orgFile, 25*time.Hour)

	s = siteSummary(t, r, f.owner, f.siteA)
	wantCategory(t, s, reports.CatFindings, reports.Green)
	wantCategory(t, s, reports.CatActions, reports.Green)
	wantCategory(t, s, reports.CatVerifications, reports.Green)
	si := wantCategory(t, s, reports.CatSignIns, reports.Red, "warnings/red/Sipho Dlamini")
	if it := si.Items[0]; *it.WorkerID != worker || !slices.Contains(it.Warnings, "no_valid_medical") {
		t.Errorf("sign-in item %+v", it)
	}
	dl := wantCategory(t, s, reports.CatIncidentDeadlines, reports.Red,
		"due_now/red/Report the incident to the provincial director forthwith by telephone or similar")
	if it := dl.Items[0]; it.IncidentID == nil || *it.IncidentID != incident {
		t.Errorf("deadline item %+v", it)
	}
	wantCategory(t, s, reports.CatWorkerRecords, reports.Amber, "expiring/amber/Sipho Dlamini: Rope access operator licence")
	// Taken off today: still absent today (site_absent: the roster as the day began).
	wantCategory(t, s, reports.CatAbsentWorkers, "-", "absent/-/Gone Worker", "absent/-/Thabo Absent")
	pu := wantCategory(t, s, reports.CatPendingUploads, "-", "pending/-/Photo (image/jpeg)")
	if *pu.Items[0].SubjectID != old {
		t.Errorf("pending item %+v", pu.Items[0])
	}
	wantCategory(t, s, reports.CatVault, reports.Red, "expired/red/LGS 2025")
	wantCategory(t, s, reports.CatAppointments, reports.Amber, "no_appointee/amber/Construction supervisor",
		"expiring/amber/Site Manager: Construction manager")

	// Six days on, by the site's clock: the open action is overdue at
	// escalation step 3, the close-out has waited past 2 days, the 7-day
	// notice is due within 2 days, and nobody has signed in "today".
	later := routerAt(t, func() time.Time { return testClock.Now().Add(6 * 24 * time.Hour) })
	s = siteSummary(t, later, f.owner, f.siteA)
	if s.Date != clk.Day(6) {
		t.Errorf("later date %s, want %s", s.Date, clk.Day(6))
	}
	act := wantCategory(t, s, reports.CatActions, reports.Red, "overdue/red/"+open.Title)
	if it := act.Items[0]; *it.SubjectID != open.ID || it.EscalationStep == nil || *it.EscalationStep != 3 {
		t.Errorf("overdue item %+v", it)
	}
	ver := wantCategory(t, s, reports.CatVerifications, reports.Amber, "stalled/amber/"+closed.Title)
	if it := ver.Items[0]; *it.ActionID != closed.ID {
		t.Errorf("stalled item %+v", it)
	}
	wantCategory(t, s, reports.CatSignIns, reports.Green)
	soon := false
	for _, x := range states(category(t, s, reports.CatIncidentDeadlines)) {
		soon = soon || x == "due_soon/amber/Written notice of the incident to the provincial director (WCL1 or WCL2)"
	}
	if !soon {
		t.Errorf("no due-soon written notice six days on: %q", states(category(t, s, reports.CatIncidentDeadlines)))
	}

	// Scoping: a supervisor sees their own site, not another; other roles 403.
	if got := siteSummary(t, r, f.supervisor, f.siteA); got.Status != reports.Red {
		t.Errorf("supervisor's summary %s", got.Status)
	}
	wantError(t, do(t, r, http.MethodGet, compliancePath(f.siteB), f.supervisor, nil), http.StatusNotFound, "site not found")
	wantError(t, do(t, r, http.MethodGet, compliancePath(f.otherSite), f.owner, nil), http.StatusNotFound, "site not found")
	wantStatus(t, do(t, r, http.MethodGet, compliancePath(f.siteA), f.bookkeeper, nil), http.StatusForbidden)
	wantStatus(t, do(t, r, http.MethodGet, "/v1/compliance", f.bookkeeper, nil), http.StatusForbidden)

	// The rollup: worst first, then by red items; the organisation's items
	// once; the file with no site for leads only.
	ro := rollup(t, r, f.owner)
	if len(ro.Sites) != 2 || ro.Sites[0].SiteID != f.siteA || ro.Sites[1].SiteID != f.siteB ||
		ro.Sites[0].RedItems <= ro.Sites[1].RedItems || ro.Sites[0].Counts[reports.CatSignIns] != 1 {
		t.Errorf("rollup sites %+v", ro.Sites)
	}
	if len(ro.OrgItems) != 2 || ro.OrgItems[0].Key != reports.CatVault || *ro.OrgItems[0].Status != reports.Red ||
		ro.OrgItems[1].Key != reports.CatPendingUploads || ro.OrgItems[1].Count != 1 || *ro.OrgItems[1].Items[0].SubjectID != orgFile {
		t.Errorf("rollup org items %+v", ro.OrgItems)
	}
	sup := rollup(t, r, f.supervisor)
	if len(sup.Sites) != 1 || sup.Sites[0].SiteID != f.siteA || len(sup.OrgItems) != 1 {
		t.Errorf("supervisor's rollup %+v", sup)
	}
}

// D3: worst status first, then more red items, then more amber, then name.
func TestComplianceRollupOrder(t *testing.T) {
	f := newEvidenceFixture(t, "rollup")
	// Every site misses its H&S file (red). Give site B a further red item:
	// a forthwith report.
	wantStatus(t, do(t, f.router, http.MethodPut, versionPath(f.siteB, uuid.NewString(), 1), f.owner,
		versionBody(testClock.Now().Add(-time.Hour), "unconsciousness")), http.StatusCreated)
	c := createSite(t, f.router, f.owner, "AAA same reds as A")
	ro := rollup(t, f.router, f.owner)
	var order []string
	for _, s := range ro.Sites {
		order = append(order, s.SiteID)
	}
	if want := []string{f.siteB, c, f.siteA}; !slices.Equal(order, want) {
		t.Errorf("order %v, want %v (B has an extra red; then by name)", order, want)
	}
}
