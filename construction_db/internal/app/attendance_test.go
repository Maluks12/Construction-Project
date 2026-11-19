package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"construction_db/internal/core/attendance"
	"construction_db/internal/core/workers"
)

func signInPath(site, id string) string { return "/v1/sites/" + site + "/sign-ins/" + id }

func signInBody(worker string, at time.Time, fields map[string]any) map[string]any {
	body := map[string]any{"worker_id": worker, "method": "tap", "client_signed_in_at": rfc(at)}
	for k, v := range fields {
		body = with(body, k, v)
	}
	return body
}

func decodeSignIn(t *testing.T, rec *httptest.ResponseRecorder) attendance.SignIn {
	t.Helper()
	var v attendance.SignIn
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode sign-in: %v (body: %s)", err, rec.Body.String())
	}
	return v
}

func decodeDay(t *testing.T, router http.Handler, token, site string) attendance.Day {
	t.Helper()
	rec := do(t, router, http.MethodGet, "/v1/sites/"+site+"/sign-ins", token, nil)
	wantStatus(t, rec, http.StatusOK)
	var d attendance.Day
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func wantStanding(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	var body attendance.AlreadySignedIn
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusConflict || body.SignInID != want || body.Error != attendance.ErrAlreadySignedIn.Error() {
		t.Errorf("got %d %s; want 409 already signed in, sign_in_id %s", rec.Code, rec.Body.String(), want)
	}
}

func TestSignInFlow(t *testing.T) {
	f := newEvidenceFixture(t, "signin")
	thabo := newWorker(t, f.router, f.owner, map[string]any{"name": "Thabo Mokoena", "firm": "ABC"})
	absentee := newWorker(t, f.router, f.owner, map[string]any{"name": "Away Today"})
	wantStatus(t, do(t, f.router, http.MethodPut, "/v1/sites/"+f.siteA+"/roster/"+absentee, f.supervisor, nil), http.StatusCreated)
	morning := time.Now().Add(-3 * time.Hour)

	// A tap on a register name.
	s1 := uuid.NewString()
	rec := do(t, f.router, http.MethodPut, signInPath(f.siteA, s1), f.supervisor, signInBody(thabo, morning, nil))
	wantStatus(t, rec, http.StatusCreated)
	si := decodeSignIn(t, rec)
	if si.Name != "Thabo Mokoena" || si.MatchedExisting || len(si.Warnings) != 1 || si.Warnings[0] != workers.WarningNoValidMedical {
		t.Errorf("signed in %+v", si)
	}
	wantStatus(t, do(t, f.router, http.MethodPut, signInPath(f.siteA, s1), f.supervisor, signInBody(thabo, morning, nil)), http.StatusOK)
	// The second phone's tap: 409 naming the sign-in already standing.
	wantStanding(t, do(t, f.router, http.MethodPut, signInPath(f.siteA, uuid.NewString()), f.owner, signInBody(thabo, morning.Add(time.Minute), nil)), s1)

	// On the spot: someone new is added to the register...
	newID := uuid.NewString()
	rec = do(t, f.router, http.MethodPut, signInPath(f.siteA, uuid.NewString()), f.supervisor,
		signInBody(newID, morning, map[string]any{"new_worker": map[string]any{"name": "Sipho Dlamini"}}))
	wantStatus(t, rec, http.StatusCreated)
	if si := decodeSignIn(t, rec); si.WorkerID != newID || si.MatchedExisting {
		t.Errorf("on the spot %+v", si)
	}
	// ...and someone already there is matched, and shown.
	tsepo := newWorker(t, f.router, f.owner, map[string]any{"name": "Tsepo Nkosi"})
	wrong := uuid.NewString()
	rec = do(t, f.router, http.MethodPut, signInPath(f.siteA, wrong), f.supervisor,
		signInBody(uuid.NewString(), morning, map[string]any{"new_worker": map[string]any{"name": " TSEPO nkosi"}}))
	wantStatus(t, rec, http.StatusCreated)
	if si := decodeSignIn(t, rec); si.WorkerID != tsepo || !si.MatchedExisting || si.Name != "Tsepo Nkosi" {
		t.Errorf("matched %+v", si)
	}
	// "Different person": retract, then sign in a new worker with a clock number.
	wantStatus(t, do(t, f.router, http.MethodPost, signInPath(f.siteA, wrong)+"/retract", f.supervisor, reason("Different person")), http.StatusCreated)
	other := uuid.NewString()
	rec = do(t, f.router, http.MethodPut, signInPath(f.siteA, uuid.NewString()), f.supervisor,
		signInBody(other, morning, map[string]any{"new_worker": map[string]any{"name": "Tsepo Nkosi", "clock_number": "T-2"}}))
	wantStatus(t, rec, http.StatusCreated)
	if si := decodeSignIn(t, rec); si.WorkerID != other || si.MatchedExisting {
		t.Errorf("the different person %+v", si)
	}

	// Sign-out: once.
	out := map[string]any{"method": "tap", "client_signed_out_at": rfc(morning.Add(2 * time.Hour))}
	wantStatus(t, do(t, f.router, http.MethodPost, signInPath(f.siteA, s1)+"/sign-out", f.supervisor, out), http.StatusCreated)
	wantStatus(t, do(t, f.router, http.MethodPost, signInPath(f.siteA, s1)+"/sign-out", f.supervisor, out), http.StatusOK)
	wantError(t, do(t, f.router, http.MethodPost, signInPath(f.siteA, s1)+"/sign-out", f.supervisor, with(out, "client_signed_out_at", rfc(morning.Add(time.Hour)))),
		http.StatusConflict, "already signed out at another time")
	wantError(t, do(t, f.router, http.MethodPost, signInPath(f.siteA, wrong)+"/sign-out", f.supervisor, out), http.StatusConflict, "sign-in was retracted")

	// Paper needs the day's sheet photo first.
	papers := newWorker(t, f.router, f.owner, map[string]any{"name": "On Paper"})
	paper := signInBody(papers, morning, map[string]any{"method": "paper"})
	wantError(t, do(t, f.router, http.MethodPut, signInPath(f.siteA, uuid.NewString()), f.supervisor, paper), http.StatusConflict,
		"paper sign-in needs a photo of that day's signed sheet; add it first")
	photo := declareAndUpload(t, f.router, f.supervisor, f.siteA, testImage(t, "jpeg", 40, 30), "image/jpeg")
	wantStatus(t, do(t, f.router, http.MethodPost, completePath(f.siteA, photo), f.supervisor, nil), http.StatusCreated)
	loc, _ := time.LoadLocation("Africa/Johannesburg")
	today := morning.In(loc).Format(time.DateOnly)
	wantStatus(t, do(t, f.router, http.MethodPut, "/v1/sites/"+f.siteA+"/sign-in-sheets/"+today+"/"+photo, f.supervisor, nil), http.StatusCreated)
	wantStatus(t, do(t, f.router, http.MethodPut, signInPath(f.siteA, uuid.NewString()), f.supervisor, paper), http.StatusCreated)

	// The day: who signed in, who on the roster didn't, and the headcount.
	d := decodeDay(t, f.router, f.supervisor, f.siteA)
	if d.Date != today || len(d.SignIns) != 5 || len(d.Absent) != 1 || d.Absent[0].WorkerID != absentee || d.Headcount != 4 || len(d.Sheets) != 1 {
		t.Errorf("day: %d sign-ins, absent %+v, headcount %d, %d sheets; want 5, [Away Today], 4 (the retracted one not counted), 1",
			len(d.SignIns), d.Absent, d.Headcount, len(d.Sheets))
	}

	// Validation and scope.
	wantError(t, do(t, f.router, http.MethodPut, signInPath(f.siteA, uuid.NewString()), f.supervisor, signInBody(uuid.NewString(), morning, nil)),
		http.StatusNotFound, "worker not found")
	wantError(t, do(t, f.router, http.MethodPut, signInPath(f.siteA, uuid.NewString()), f.supervisor, signInBody(thabo, time.Now().Add(time.Hour), nil)),
		http.StatusBadRequest, "client_signed_in_at is in the future; check the device clock")
	wantError(t, do(t, f.router, http.MethodPut, signInPath(f.siteA, uuid.NewString()), f.supervisor, signInBody(thabo, morning, map[string]any{"method": "qr"})),
		http.StatusBadRequest, "method must be tap or paper")
	wantError(t, do(t, f.router, http.MethodPut, signInPath(f.siteB, uuid.NewString()), f.supervisor, signInBody(thabo, morning, nil)),
		http.StatusNotFound, "site not found")
	wantStatus(t, do(t, f.router, http.MethodGet, "/v1/sites/"+f.siteA+"/sign-ins", f.bookkeeper, nil), http.StatusForbidden)
	wantError(t, do(t, f.router, http.MethodPost, signInPath(f.siteA, s1)+"/retract", f.bookkeeper, reason("x")), http.StatusForbidden, "forbidden")
}

// The diary suggests distinct people who signed in or signed a talk; a
// talk shows who was signed in on site but didn't sign it.
func TestSignInsInDiaryAndTalks(t *testing.T) {
	f := newEvidenceFixture(t, "signin-diary")
	thabo := newWorker(t, f.router, f.owner, map[string]any{"name": "Thabo Mokoena"})
	sipho := newWorker(t, f.router, f.owner, map[string]any{"name": "Sipho Dlamini"})
	start := time.Now().Add(-2 * time.Hour)
	for _, w := range []string{thabo, sipho} {
		wantStatus(t, do(t, f.router, http.MethodPut, signInPath(f.siteA, uuid.NewString()), f.supervisor, signInBody(w, start, nil)), http.StatusCreated)
	}
	talk := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, talkPath(f.siteA, talk), f.supervisor, talkBody(map[string]any{"started_at": rfc(start.Add(time.Hour))})), http.StatusCreated)
	// Thabo signs the talk; a visitor not on the register signs it too.
	wantStatus(t, do(t, f.router, http.MethodPut, signPath(f.siteA, talk, uuid.NewString()), f.supervisor, signBody("thabo MOKOENA", start.Add(time.Hour), nil)), http.StatusCreated)
	wantStatus(t, do(t, f.router, http.MethodPut, signPath(f.siteA, talk, uuid.NewString()), f.supervisor, signBody("A Visitor", start.Add(time.Hour), nil)), http.StatusCreated)

	tk := decodeTalk(t, do(t, f.router, http.MethodGet, talkPath(f.siteA, talk), f.owner, nil))
	if len(tk.NotSigned) != 1 || tk.NotSigned[0].WorkerID != sipho {
		t.Errorf("not_signed %+v, want only Sipho", tk.NotSigned)
	}
	if d := decodeDay(t, f.router, f.owner, f.siteA); d.Headcount != 3 {
		t.Errorf("headcount %d, want 3 (Thabo once, Sipho, the visitor)", d.Headcount)
	}

	loc, _ := time.LoadLocation("Africa/Johannesburg")
	date := time.Now().In(loc).Format(time.DateOnly)
	rec := do(t, f.router, http.MethodPut, revisionPath(f.siteA, date, uuid.NewString()), f.supervisor, revisionBody(time.Now(), map[string]any{
		"sections": map[string]any{"labour": []map[string]any{{"id": uuid.NewString(), "firm": "Own", "own_team": true, "headcount": 2,
			"worker_ids": []string{thabo, sipho}}}}}))
	wantStatus(t, rec, http.StatusCreated)
	if d := decodeDiary(t, rec); d.HeadcountSuggestedNow != 3 || d.Current.HeadcountSuggested != 3 {
		t.Errorf("diary suggestion now %d, stored %d; want 3", d.HeadcountSuggestedNow, d.Current.HeadcountSuggested)
	}
	// A labour row names register workers only, at most its headcount.
	wantStatus(t, do(t, f.router, http.MethodPut, revisionPath(f.siteA, date, uuid.NewString()), f.supervisor, revisionBody(time.Now(), map[string]any{
		"sections": map[string]any{"labour": []map[string]any{{"id": uuid.NewString(), "firm": "Own", "own_team": true, "headcount": 1,
			"worker_ids": []string{thabo, sipho}}}}})), http.StatusBadRequest)
	wantStatus(t, do(t, f.router, http.MethodPut, revisionPath(f.siteA, date, uuid.NewString()), f.supervisor, revisionBody(time.Now(), map[string]any{
		"sections": map[string]any{"labour": []map[string]any{{"id": uuid.NewString(), "firm": "Own", "own_team": true, "headcount": 1,
			"worker_ids": []string{uuid.NewString()}}}}})), http.StatusBadRequest)
}

// Two phones tap one worker at once, through the API. Forced: the first
// phone's sign-in is held uncommitted; the request waits on the worker's
// row, then sees it and answers 409 naming it.
func TestSignInDoubleTapThroughAPI(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFixture(t, "signin-race")
	w := newWorker(t, f.router, f.owner, map[string]any{"name": "Race Worker"})
	at := time.Now().Add(-time.Hour)
	held := uuid.NewString()

	tx := holdOpen(t, f)
	if _, err := tx.Exec(ctx, `INSERT INTO site_sign_ins (id, org_id, site_id, worker_id, name, method, recorded_by, client_signed_in_at, time_zone)
		VALUES ($1, $2, $3, $4, '-', 'tap', $5, $6, 'Africa/Johannesburg')`, held, f.ownerMe.OrgID, f.siteA, w, f.ownerMe.UserID, at); err != nil {
		t.Fatal(err)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- do(t, f.router, http.MethodPut, signInPath(f.siteA, uuid.NewString()), f.supervisor, signInBody(w, at.Add(time.Second), nil))
	}()
	waitForLock(t, "INSERT INTO site_sign_ins")
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	wantStanding(t, <-done, held)
	if n := countRows(t, `SELECT count(*) FROM site_sign_ins WHERE worker_id = $1`, w); n != 1 {
		t.Errorf("%d sign-ins stored, want 1", n)
	}
}

// Two phones add the same new person on the spot at once: the second
// waits on the identity key, then matches the first's worker.
func TestOnTheSpotRace(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFixture(t, "spot-race")
	first := uuid.NewString()
	tx := holdOpen(t, f)
	if _, err := tx.Exec(ctx, `INSERT INTO workers (id, org_id, name, created_by) VALUES ($1, $2, 'New Person', $3)`,
		first, f.ownerMe.OrgID, f.ownerMe.UserID); err != nil {
		t.Fatal(err)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- do(t, f.router, http.MethodPut, signInPath(f.siteA, uuid.NewString()), f.supervisor,
			signInBody(uuid.NewString(), time.Now().Add(-time.Hour), map[string]any{"new_worker": map[string]any{"name": "new  PERSON"}}))
	}()
	waitForLock(t, "INSERT INTO workers")
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	rec := <-done
	wantStatus(t, rec, http.StatusCreated)
	if si := decodeSignIn(t, rec); si.WorkerID != first || !si.MatchedExisting {
		t.Errorf("signed in %+v, want the first phone's worker, matched", si)
	}
}

// attendance.OpenWindow is the database's site_sign_in_window().
func TestSignInWindowMatchesDatabase(t *testing.T) {
	var seconds float64
	if err := testPool.QueryRow(context.Background(), `SELECT extract(epoch FROM site_sign_in_window())`).Scan(&seconds); err != nil {
		t.Fatal(err)
	}
	if time.Duration(seconds)*time.Second != attendance.OpenWindow {
		t.Errorf("site_sign_in_window() is %vs, attendance.OpenWindow %s", seconds, attendance.OpenWindow)
	}
}
