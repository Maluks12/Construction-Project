package app

import (
	"context"
	"maps"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"construction_db/internal/core/devicesync"
	"construction_db/internal/core/files"
	"construction_db/internal/platform/database"
	"construction_db/internal/platform/dbtest"
)

// syncContextReads are read by every GET to place the caller and the scope;
// a change to them reaches the phone another way.
var syncContextReads = map[string]string{
	"sessions":           "auth: the caller's own session (401 signs out)",
	"org_members":        "auth: the caller's live role (a change resets every cursor, D5)",
	"sites":              "the scope itself: the site list is sent whole on every org pull (D4)",
	"regulatory_regimes": "the site's regime row: fixed for a site",
}

type tableScans map[string]int64

func scans(t *testing.T) tableScans {
	t.Helper()
	rows, err := testAdmin.Query(context.Background(), `SELECT relname, seq_scan + coalesce(idx_scan, 0) FROM pg_stat_user_tables`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := tableScans{}
	for rows.Next() {
		var name string
		var n int64
		if err := rows.Scan(&name, &n); err != nil {
			t.Fatal(err)
		}
		out[name] = n
	}
	return out
}

// TestSyncDepsCoverWhatGETsRead is D3's mechanical check (SYNC review 3):
// every table a kind's GET reads is a dep of the kind, a context read, or
// listed as of server_time with a reason. A dep left out of a kind fails it.
func TestSyncDepsCoverWhatGETsRead(t *testing.T) {
	ctx := context.Background()
	f := newActionFixture(t, "syncreads")
	c := newSASTClock(t)

	// One of everything a pull carries, on site A.
	s := newShift(c, f.siteA)
	push(t, f.router, f.supervisor, s.records...)
	action := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, actionPath(f.siteA, action), f.owner, f.raiseBody(f.supervisorID, c.Earlier(5*time.Minute))), http.StatusCreated)
	file := uploadFile(t, f.router, f.owner, files.PurposeCompetency, nil)
	wantStatus(t, do(t, f.router, http.MethodPut, workerPath(s.worker)+"/competencies/"+uuid.NewString(), f.owner,
		competencyBody(c, file, c.Today(), c.Day(10))), http.StatusCreated)
	vaultFile := uploadFile(t, f.router, f.owner, files.PurposeOrgDocument, nil)
	vaultDoc := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, vaultPath(vaultDoc), f.owner, map[string]any{"regime_code": "za_construction_2014",
		"type_key": "coida_letter_of_good_standing", "title": "LGS", "version": versionReq(vaultFile, map[string]any{"issuer": "FEM", "expires_on": c.Day(40)})}),
		http.StatusCreated)
	siteFile := uploadFile(t, f.router, f.owner, files.PurposeSiteDocument, &f.siteA)
	siteDoc := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, safetyFilePath(f.siteA, siteDoc), f.owner, map[string]any{"type_key": "health_and_safety_plan",
		"title": "H&S plan", "version": versionReq(siteFile, nil)}), http.StatusCreated)

	wantStatus(t, do(t, f.router, http.MethodPost, "/v1/sites/"+f.siteA+"/locations", f.owner,
		map[string]any{"kind": "zone", "name": "Block A"}), http.StatusCreated)
	letter := uploadFile(t, f.router, f.owner, files.PurposeAppointment, &f.siteA)
	wantStatus(t, do(t, f.router, http.MethodPut, appointmentPath(f.siteA, uuid.NewString()), f.owner, map[string]any{
		"worker_id": s.worker, "type_key": "construction_manager", "appointed_on": c.Today(), "file_id": letter,
		"client_recorded_at": rfc(c.Now())}), http.StatusCreated)

	// A reminder, so the owner has an inbox.
	if _, err := reminderJobs(f.evidenceFixture, c, &recordingMailer{}).Reminders(ctx, c.Now()); err != nil {
		t.Fatal(err)
	}

	// The items, as a pull finds them. Each GET then runs on a router whose
	// pool has one connection: that backend flushes its counters on demand
	// (pg_stat_force_next_flush), and the tables whose scans rose are the
	// tables the GET read.
	siteItems, _, _ := pullSite(t, f.router, f.owner, f.siteA, "", 500)
	orgItems := pullOrg(t, f.router, f.owner, "").Items

	one, err := database.Connect(ctx, testPool.Config().ConnString(), 1, dbtest.Bounds)
	if err != nil {
		t.Fatal(err)
	}
	defer one.Close()
	tracked, err := NewRouter(ctx, one, testSecret, testRegistry, testEdition, testStore, testHTTP)
	if err != nil {
		t.Fatal(err)
	}

	kinds := map[string]devicesync.Kind{}
	for _, k := range syncKinds() {
		kinds[k.Name] = k
	}
	seen := map[string]bool{}
	check := func(scope string, items []devicesync.Item) {
		for _, it := range items {
			if seen[it.Kind] {
				continue
			}
			seen[it.Kind] = true
			k := kinds[it.Kind]
			before := scans(t)
			wantStatus(t, get(t, tracked, k.Path(scope, it.ID), f.owner), http.StatusOK)
			if _, err := one.Exec(ctx, `SELECT pg_stat_force_next_flush()`); err != nil {
				t.Fatal(err)
			}
			after := scans(t)
			deps := map[string]bool{}
			for _, d := range k.Deps {
				deps[d.Table] = true
			}
			var read []string
			for _, name := range slices.Sorted(maps.Keys(after)) {
				if after[name] <= before[name] {
					continue
				}
				read = append(read, name)
				_, ctxRead := syncContextReads[name]
				_, asOf := k.AsOf[name]
				if !deps[name] && !ctxRead && !asOf {
					t.Errorf("%s: its GET reads %s, which is neither a dep, a context read, nor as of server_time", it.Kind, name)
				}
			}
			t.Logf("%s reads %v", it.Kind, read)
		}
	}
	check(f.siteA, siteItems)
	check(f.ownerMe.OrgID, orgItems)
	for _, k := range syncKinds() {
		if !seen[k.Name] {
			t.Errorf("no %s item to check", k.Name)
		}
	}
}
