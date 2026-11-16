package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"construction_db/internal/core/toolbox"
)

// wantAlreadySigned: 409 with the error and the hint.
func wantAlreadySigned(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	var body struct{ Error, Hint string }
	if rec.Code != http.StatusConflict || json.Unmarshal(rec.Body.Bytes(), &body) != nil ||
		body.Error != toolbox.ErrAlreadySigned.Error() || body.Hint != toolbox.AlreadySignedHint {
		t.Errorf("got %d %s; want 409 with the already-signed error and hint", rec.Code, rec.Body.String())
	}
}

func TestToolboxSameNameWorkers(t *testing.T) {
	f := newEvidenceFixture(t, "samename")
	id := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, talkPath(f.siteA, id), f.supervisor, talkBody(nil)), http.StatusCreated)
	sign := func(name string, fields map[string]any) *httptest.ResponseRecorder {
		return do(t, f.router, http.MethodPut, signPath(f.siteA, id, uuid.NewString()), f.supervisor, signBody(name, signAt, fields))
	}

	// Same name, different firms: two people.
	wantStatus(t, sign("Thabo M", map[string]any{"firm": "Own team"}), http.StatusCreated)
	wantStatus(t, sign("Thabo M", map[string]any{"firm": "ABC Scaffolding"}), http.StatusCreated)
	// Same name, same firm (case and spacing don't count), no clock number.
	wantAlreadySigned(t, sign("thabo  m", map[string]any{"firm": " own TEAM"}))
	// Same name and firm, different clock numbers: two people.
	wantStatus(t, sign("Thabo M", map[string]any{"firm": "Own team", "clock_number": "A1"}), http.StatusCreated)
	wantStatus(t, sign("Thabo M", map[string]any{"firm": "Own team", "clock_number": "A2"}), http.StatusCreated)

	talk := decodeTalk(t, do(t, f.router, http.MethodGet, talkPath(f.siteA, id), f.owner, nil))
	if talk.SignedCount != 4 {
		t.Errorf("signed_count %d, want 4", talk.SignedCount)
	}
	var n int
	if err := testAdmin.QueryRow(context.Background(), `SELECT toolbox_headcount($1, $2)`, f.siteA, talk.TalkDate).Scan(&n); err != nil || n != 4 {
		t.Errorf("headcount %d, err %v; want 4", n, err)
	}
}

// Two identical signatures (same person, different ids) at once: exactly one
// lands. Forced: a held lock on the talk lets both pass every check and
// queue at the insert; released, they collide on the unique key.
func TestToolboxIdenticalSignaturesRace(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFixture(t, "twins")
	id := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, talkPath(f.siteA, id), f.supervisor, talkBody(nil)), http.StatusCreated)

	blocker := beginTenant(t, f.ownerMe.OrgID)
	defer blocker.Rollback(ctx)
	if _, err := blocker.Exec(ctx, `SELECT 1 FROM toolbox_talks WHERE id = $1 FOR UPDATE`, id); err != nil {
		t.Fatal(err)
	}
	codes := make(chan int, 2)
	for range 2 {
		go func() {
			codes <- do(t, f.router, http.MethodPut, signPath(f.siteA, id, uuid.NewString()), f.supervisor,
				signBody("Sipho N", signAt, map[string]any{"firm": "Own team"})).Code
		}()
	}
	waitForWaiters(t, "INSERT INTO toolbox_attendance", 2)
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	got := []int{<-codes, <-codes}
	slices.Sort(got)
	if !slices.Equal(got, []int{http.StatusCreated, http.StatusConflict}) {
		t.Errorf("statuses %v, want one 201 and one 409", got)
	}
	if n := countRows(t, `SELECT count(*) FROM toolbox_attendance WHERE talk_id = $1`, id); n != 1 {
		t.Errorf("%d signatures, want 1", n)
	}
}

// waitForWaiters waits until n sessions are blocked on a lock while running a
// statement containing fragment.
func waitForWaiters(t *testing.T, fragment string, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var got int
		if err := testAdmin.QueryRow(context.Background(), `
			SELECT count(*) FROM pg_stat_activity
			WHERE wait_event_type = 'Lock' AND query LIKE '%' || $1 || '%'`, fragment).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got >= n {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("fewer than %d sessions waited on a lock running %q", n, fragment)
}

// toolbox.IdentityKey (Go) and identity_key (the generated column) agree.
func TestIdentityKeyMatchesDatabase(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFixture(t, "identity")
	supervisor := me(t, f.router, f.supervisor).UserID
	s := func(v string) *string { return &v }
	cases := []struct {
		name        string
		firm, clock *string
	}{
		{"Thabo M", nil, nil},
		{"  THABO   m ", nil, nil},
		{"Thabo\tM", s("Own Team"), nil},
		{"thabo m", s("  own\n\tteam  "), nil},
		{"Thabo M", nil, s(" A1 ")},
		{"Thabo M", s("ABC  Scaffolding"), s("a1")},
		{"Émile Ngcobo", s("Électricité SA"), s("X-9")},
		{"Lerato\r\nK", s("\vOwn team\f"), s("\t7\t")},
	}
	tx := beginTenant(t, f.ownerMe.OrgID)
	defer tx.Rollback(ctx)
	for i, c := range cases {
		talk, att := uuid.NewString(), uuid.NewString() // a talk each, so equal keys don't collide
		if _, err := tx.Exec(ctx, `
			INSERT INTO toolbox_talks (id, org_id, site_id, topic, presenter_name, started_at, time_zone, recorded_by)
			VALUES ($1, $2, $3, 'T', 'P', $4, 'Africa/Johannesburg', $5)`, talk, f.ownerMe.OrgID, f.siteA, talkAt, supervisor); err != nil {
			t.Fatal(err)
		}
		var key string
		if err := tx.QueryRow(ctx, `
			INSERT INTO toolbox_attendance (id, org_id, site_id, talk_id, name, firm, clock_number, method, recorded_by, client_signed_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'tap', $8, $9) RETURNING identity_key`,
			att, f.ownerMe.OrgID, f.siteA, talk, c.name, c.firm, c.clock, supervisor, signAt).Scan(&key); err != nil {
			t.Fatal(err)
		}
		if got := toolbox.IdentityKey(c.name, c.firm, c.clock); got != key {
			t.Errorf("case %d: Go %q, database %q", i, got, key)
		}
	}
}

// The signing window, checked three ways on the same cases: the Go check,
// the trigger (a direct insert) and the API. They must agree.
func TestSigningWindowAgrees(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFixture(t, "window")
	supervisor := me(t, f.router, f.supervisor).UserID
	start := time.Date(2026, 11, 14, 21, 40, 0, 0, time.UTC) // 23:40 SAST
	end := start.Add(50 * time.Minute)
	cases := []struct {
		what string
		end  *time.Time
		at   time.Time
		want error
	}{
		{"after midnight", nil, start.Add(25 * time.Minute), nil},
		{"at the start", nil, start, nil},
		{"before the start", nil, start.Add(-time.Microsecond), toolbox.ErrSignedBeforeStart},
		{"12 hours on", nil, start.Add(toolbox.SigningWindow), nil},
		{"12 hours and 1µs on", nil, start.Add(toolbox.SigningWindow + time.Microsecond), toolbox.ErrSignedTooLate},
		{"at the end", &end, end, nil},
		{"after the end", &end, end.Add(time.Microsecond), toolbox.ErrSignedAfterEnd},
		{"after the end, inside 12 hours", &end, start.Add(time.Hour), toolbox.ErrSignedAfterEnd},
	}
	// The trigger's messages, by the API's error.
	trigger := map[error]string{
		toolbox.ErrSignedBeforeStart: "signed before the talk started",
		toolbox.ErrSignedTooLate:     "signed more than 12 hours after the talk started",
		toolbox.ErrSignedAfterEnd:    "signed after the talk ended",
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			if err := toolbox.CheckSigningWindow(start, c.end, c.at); !errors.Is(err, c.want) {
				t.Errorf("Go: %v, want %v", err, c.want)
			}

			// The trigger, with the API's checks out of the way.
			tx := beginTenant(t, f.ownerMe.OrgID)
			defer tx.Rollback(ctx)
			talk := uuid.NewString()
			if _, err := tx.Exec(ctx, `
				INSERT INTO toolbox_talks (id, org_id, site_id, topic, presenter_name, started_at, ended_at, time_zone, recorded_by)
				VALUES ($1, $2, $3, 'T', 'P', $4, $5, 'Africa/Johannesburg', $6)`, talk, f.ownerMe.OrgID, f.siteA, start, c.end, supervisor); err != nil {
				t.Fatal(err)
			}
			_, err := tx.Exec(ctx, `
				INSERT INTO toolbox_attendance (id, org_id, site_id, talk_id, name, method, recorded_by, client_signed_at)
				VALUES ($1, $2, $3, $4, 'Night worker', 'tap', $5, $6)`, uuid.NewString(), f.ownerMe.OrgID, f.siteA, talk, supervisor, c.at)
			var pgErr *pgconn.PgError
			switch {
			case c.want == nil && err != nil:
				t.Errorf("trigger: %v, want accepted", err)
			case c.want != nil && (!errors.As(err, &pgErr) || pgErr.Code != "23514" || !strings.Contains(pgErr.Message, trigger[c.want])):
				t.Errorf("trigger: %v, want %q", err, trigger[c.want])
			}

			// The API.
			apiTalk := uuid.NewString()
			body := talkBody(map[string]any{"started_at": rfc(start)})
			if c.end != nil {
				body["ended_at"] = rfc(*c.end)
			}
			wantStatus(t, do(t, f.router, http.MethodPut, talkPath(f.siteA, apiTalk), f.supervisor, body), http.StatusCreated)
			rec := do(t, f.router, http.MethodPut, signPath(f.siteA, apiTalk, uuid.NewString()), f.supervisor, signBody("Night worker", c.at, nil))
			if c.want == nil {
				wantStatus(t, rec, http.StatusCreated)
			} else {
				wantError(t, rec, http.StatusBadRequest, c.want.Error())
			}
		})
	}
}

func TestSigningWindowMatchesDatabase(t *testing.T) {
	var secs float64
	if err := testAdmin.QueryRow(context.Background(), `SELECT extract(epoch FROM toolbox_signing_window())`).Scan(&secs); err != nil {
		t.Fatal(err)
	}
	if time.Duration(secs)*time.Second != toolbox.SigningWindow {
		t.Errorf("toolbox_signing_window() is %vs, toolbox.SigningWindow %v", secs, toolbox.SigningWindow)
	}
}

func TestToolboxEndTime(t *testing.T) {
	f := newEvidenceFixture(t, "endtime")
	id := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, talkPath(f.siteA, id), f.supervisor, talkBody(nil)), http.StatusCreated)
	att := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, signPath(f.siteA, id, att), f.supervisor, signBody("Sipho N", talkAt.Add(40*time.Minute), nil)), http.StatusCreated)

	end := func(d time.Duration) map[string]any { return talkBody(map[string]any{"ended_at": rfc(talkAt.Add(d))}) }
	wantError(t, do(t, f.router, http.MethodPut, talkPath(f.siteA, id), f.supervisor, end(30*time.Minute)),
		http.StatusConflict, toolbox.ErrEndsBeforeSigned.Error())
	wantError(t, do(t, f.router, http.MethodPut, talkPath(f.siteA, id), f.supervisor, end(toolbox.SigningWindow+time.Microsecond)),
		http.StatusBadRequest, toolbox.ErrEndsTooLate.Error())
	wantError(t, do(t, f.router, http.MethodPut, talkPath(f.siteA, id), f.supervisor, end(-time.Microsecond)),
		http.StatusBadRequest, toolbox.ErrEndsBeforeStart.Error())
	// A retracted signature doesn't hold the end back.
	wantStatus(t, do(t, f.router, http.MethodPost, signPath(f.siteA, id, att)+"/retract", f.supervisor,
		map[string]any{"reason": "Wrong talk", "client_retracted_at": rfc(time.Now())}), http.StatusCreated)
	wantStatus(t, do(t, f.router, http.MethodPut, talkPath(f.siteA, id), f.supervisor, end(30*time.Minute)), http.StatusOK)
	wantError(t, do(t, f.router, http.MethodPut, signPath(f.siteA, id, uuid.NewString()), f.supervisor, signBody("Late", talkAt.Add(31*time.Minute), nil)),
		http.StatusBadRequest, toolbox.ErrSignedAfterEnd.Error())
}
