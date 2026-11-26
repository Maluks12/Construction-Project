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

	"construction_db/internal/core/attendance"
	"construction_db/internal/core/toolbox"
	"construction_db/internal/core/workers"
)

// D7: a phone resending a roster removal gets the removal back.
func TestRosterRemoveRepeat(t *testing.T) {
	f := newEvidenceFixture(t, "rosterrepeat")
	w := newWorker(t, f.router, f.owner, map[string]any{"name": "Repeat Worker " + uuid.NewString()[:6]})
	path := "/v1/sites/" + f.siteA + "/roster/" + w
	wantStatus(t, do(t, f.router, http.MethodPut, path, f.supervisor, nil), http.StatusCreated)

	decode := func(res []byte) attendance.RosterEntry {
		var e attendance.RosterEntry
		if err := json.Unmarshal(res, &e); err != nil {
			t.Fatal(err)
		}
		return e
	}
	first := do(t, f.router, http.MethodPost, path+"/remove", f.supervisor, map[string]any{"reason": "Contract ended"})
	wantStatus(t, first, http.StatusOK)
	again := do(t, f.router, http.MethodPost, path+"/remove", f.supervisor, map[string]any{"reason": "Contract ended"})
	wantStatus(t, again, http.StatusOK)
	a, b := decode(first.Body.Bytes()), decode(again.Body.Bytes())
	if a.ID != b.ID || b.RemovedAt == nil || !a.RemovedAt.Equal(*b.RemovedAt) {
		t.Errorf("repeat answered %+v, first %+v", b, a)
	}
	wantError(t, do(t, f.router, http.MethodPost, path+"/remove", f.supervisor, map[string]any{"reason": "Another reason"}),
		http.StatusConflict, "worker already removed from the roster with another reason")

	other := newWorker(t, f.router, f.owner, map[string]any{"name": "Never Rostered " + uuid.NewString()[:6]})
	wantError(t, do(t, f.router, http.MethodPost, "/v1/sites/"+f.siteA+"/roster/"+other+"/remove", f.supervisor,
		map[string]any{"reason": "Contract ended"}), http.StatusNotFound, "worker is not on this site's roster")

	// Back on the roster, a removal removes the new entry.
	wantStatus(t, do(t, f.router, http.MethodPut, path, f.supervisor, nil), http.StatusCreated)
	third := do(t, f.router, http.MethodPost, path+"/remove", f.supervisor, map[string]any{"reason": "Contract ended"})
	wantStatus(t, third, http.StatusOK)
	if decode(third.Body.Bytes()).ID == a.ID {
		t.Error("the second removal answered the first entry")
	}
}

// D7: a satisfaction names its device, and a repeat compares it.
func TestSatisfactionDeviceID(t *testing.T) {
	f := newEvidenceFixture(t, "satdevice")
	c := newSASTClock(t)
	inc := submitNew(t, f, f.supervisor, versionBody(c.Earlier(40*time.Minute), "unconsciousness"))
	var deadline string
	for _, d := range inc.Deadlines {
		if d.ClosedAs == nil {
			deadline = d.ID
			break
		}
	}
	id := uuid.NewString()
	body := func(device any) map[string]any {
		b := map[string]any{"satisfied_at": rfc(c.Earlier(10 * time.Minute)), "method": "phone"}
		if device != nil {
			b["device_id"] = device
		}
		return b
	}
	path := satisfactionPath(f.siteA, deadline, id)
	res := do(t, f.router, http.MethodPut, path, f.supervisor, body("phone-1"))
	wantStatus(t, res, http.StatusCreated)
	d := decodeDetail(t, res)
	if d.Satisfaction == nil || d.Satisfaction.DeviceID == nil || *d.Satisfaction.DeviceID != "phone-1" {
		t.Errorf("satisfaction %+v", d.Satisfaction)
	}
	wantStatus(t, do(t, f.router, http.MethodPut, path, f.supervisor, body("phone-1")), http.StatusOK)
	wantError(t, do(t, f.router, http.MethodPut, path, f.supervisor, body("phone-2")), http.StatusConflict,
		"satisfaction id already used with different details")
	wantError(t, do(t, f.router, http.MethodPut, path, f.supervisor, body(nil)), http.StatusConflict,
		"satisfaction id already used with different details")
	for _, bad := range []string{"", strings.Repeat("x", 201)} {
		wantError(t, do(t, f.router, http.MethodPut, satisfactionPath(f.siteA, deadline, uuid.NewString()), f.supervisor, body(bad)),
			http.StatusBadRequest, "device_id must be 1-200 bytes")
	}
}

// Batch 2: an edit of a worker or a talk may name the copy it was made
// from; a stale one is 409 with the record as stored.
func TestEditConflicts(t *testing.T) {
	f := newEvidenceFixture(t, "editconflict")
	c := newSASTClock(t)

	t.Run("worker", func(t *testing.T) {
		id := newWorker(t, f.router, f.owner, map[string]any{"name": "Base Worker " + uuid.NewString()[:6]})
		got := decodeWorker(t, get(t, f.router, workerPath(id), f.owner))
		created := rfc(got.CreatedAt)
		edit := func(name, base string) map[string]any {
			b := map[string]any{"name": name}
			if base != "" {
				b["base_updated_at"] = base
			}
			return b
		}
		first := do(t, f.router, http.MethodPut, workerPath(id), f.owner, edit("Edited "+id[:6], created))
		wantStatus(t, first, http.StatusOK)
		stale := do(t, f.router, http.MethodPut, workerPath(id), f.owner, edit("Other "+id[:6], created))
		wantErrorText(t, stale, http.StatusConflict, "worker changed since base_updated_at; get it and edit again")
		var body struct {
			Current workers.Worker `json:"current"`
		}
		_ = json.Unmarshal(stale.Body.Bytes(), &body)
		if body.Current.Name != "Edited "+id[:6] || body.Current.UpdatedAt == nil {
			t.Errorf("current %+v", body.Current)
		}
		// The same body again is a repeat, whatever its base.
		wantStatus(t, do(t, f.router, http.MethodPut, workerPath(id), f.owner, edit("Edited "+id[:6], created)), http.StatusOK)
		// No base: the last edit wins, as before.
		wantStatus(t, do(t, f.router, http.MethodPut, workerPath(id), f.owner, edit("Last "+id[:6], "")), http.StatusOK)
		// The current base works.
		now := decodeWorker(t, get(t, f.router, workerPath(id), f.owner))
		wantStatus(t, do(t, f.router, http.MethodPut, workerPath(id), f.owner, edit("Based "+id[:6], rfc(*now.UpdatedAt))), http.StatusOK)
	})

	t.Run("two edits from one base at once", func(t *testing.T) {
		ctx := context.Background()
		id := newWorker(t, f.router, f.owner, map[string]any{"name": "Race Worker " + uuid.NewString()[:6]})
		base := rfc(decodeWorker(t, get(t, f.router, workerPath(id), f.owner)).CreatedAt)
		hold := holdOpen(t, f)
		if _, err := hold.Exec(ctx, `SELECT 1 FROM workers WHERE id = $1 FOR UPDATE`, id); err != nil {
			t.Fatal(err)
		}
		codes := make(chan int, 2)
		for _, name := range []string{"Edit A " + id[:6], "Edit B " + id[:6]} {
			go func() {
				codes <- do(t, f.router, http.MethodPut, workerPath(id), f.owner, map[string]any{"name": name, "base_updated_at": base}).Code
			}()
		}
		waitForWaiters(t, "SELECT 1 FROM workers", 2)
		if err := hold.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		a, b := <-codes, <-codes
		if a+b != http.StatusOK+http.StatusConflict {
			t.Errorf("codes %d and %d, want one 200 and one 409", a, b)
		}
	})

	t.Run("talk", func(t *testing.T) {
		id := uuid.NewString()
		started := rfc(c.Earlier(30 * time.Minute))
		res := do(t, f.router, http.MethodPut, talkPath(f.siteA, id), f.supervisor, talkBody(map[string]any{"started_at": started}))
		wantStatus(t, res, http.StatusCreated)
		created := rfc(decodeTalk(t, res).CreatedAt)
		edit := func(presenter, base string) map[string]any {
			return talkBody(map[string]any{"started_at": started, "presenter_name": presenter, "base_updated_at": base})
		}
		wantStatus(t, do(t, f.router, http.MethodPut, talkPath(f.siteA, id), f.supervisor, edit("Zanele", created)), http.StatusOK)
		stale := do(t, f.router, http.MethodPut, talkPath(f.siteA, id), f.supervisor, edit("Bongani", created))
		wantErrorText(t, stale, http.StatusConflict, "talk changed since base_updated_at; get it and edit again")
		var body struct {
			Current toolbox.Talk `json:"current"`
		}
		_ = json.Unmarshal(stale.Body.Bytes(), &body)
		if body.Current.PresenterName != "Zanele" {
			t.Errorf("current %+v", body.Current.TalkHeader)
		}
		wantStatus(t, do(t, f.router, http.MethodPut, talkPath(f.siteA, id), f.supervisor, edit("Zanele", created)), http.StatusOK)
	})
}

// Batch 2: inspection start and completion get the 5-minute clock check
// every other capture has.
func TestInspectionClockChecks(t *testing.T) {
	a := newAnswerFixture(t, "inspclock")
	c := newSASTClock(t)
	future := rfc(c.Now().Add(10 * time.Minute))
	wantError(t, do(t, a.router, http.MethodPut, inspectionPath(a.siteA, uuid.NewString()), a.supervisor,
		with(startBody(a.tmpl), "client_started_at", future)), http.StatusBadRequest,
		"client_started_at is in the future; check the device clock")
	wantError(t, do(t, a.router, http.MethodPost, completePathFor(a.siteA, a.inspection), a.supervisor,
		map[string]any{"client_completed_at": future}), http.StatusBadRequest,
		"client_completed_at is in the future; check the device clock")
	wantStatus(t, do(t, a.router, http.MethodPut, inspectionPath(a.siteA, uuid.NewString()), a.supervisor,
		with(startBody(a.tmpl), "client_started_at", rfc(c.Now().Add(4*time.Minute)))), http.StatusCreated)
}

func TestFindingsByInspection(t *testing.T) {
	f := newActionFixture(t, "findingsbyinsp")
	other := completeFailing(t, f.answerFixture, "", map[string]string{f.item: "low"}, f.failPhoto)
	all := listFindings(t, f.router, f.owner, f.siteA, "")
	one := listFindings(t, f.router, f.owner, f.siteA, "?inspection_id="+other)
	if len(all) < 2 || len(one) != 1 || one[0].InspectionID != other {
		t.Errorf("all %d, one %+v", len(all), one)
	}
	if none := listFindings(t, f.router, f.owner, f.siteA, "?inspection_id=not-a-uuid"); len(none) != 0 {
		t.Errorf("a bad id lists %+v", none)
	}
}

// D3: an aggregate comes back in the next delta whenever a row its GET reads
// changes, one write at a time.
func TestSyncDeltaFollowsEveryChange(t *testing.T) {
	f := newActionFixture(t, "syncdeps")
	c := newSASTClock(t)
	_, cursor, _ := pullSite(t, f.router, f.owner, f.siteA, "", 500)
	orgCursor := pullOrg(t, f.router, f.owner, "").Cursor

	step := func(name string, write func(), want ...[2]string) {
		t.Helper()
		write()
		items, next, reset := pullSite(t, f.router, f.owner, f.siteA, cursor, 500)
		if reset {
			t.Fatalf("%s: reset", name)
		}
		for _, w := range want {
			if !has(items, w[0], w[1]) {
				t.Errorf("%s: the delta lacks %s %s; has %v", name, w[0], w[1], kindsOf(items))
			}
		}
		cursor = next
	}
	put := func(path, token string, body any, want int) func() {
		return func() { wantStatus(t, do(t, f.router, http.MethodPut, path, token, body), want) }
	}
	post := func(path, token string, body any, want int) func() {
		return func() { wantStatus(t, do(t, f.router, http.MethodPost, path, token, body), want) }
	}

	// Inspections, findings and actions.
	insp := uuid.NewString()
	step("start", put(inspectionPath(f.siteA, insp), f.supervisor, startBody(f.tmpl), 201), [2]string{"inspection", insp})
	step("answer with a photo", put(itemPath(f.siteA, insp, f.key), f.supervisor, answerBody("pass", "", rfc(c.Earlier(20*time.Minute)), f.photo), 200),
		[2]string{"inspection", insp})
	step("photos replaced", put(itemPath(f.siteA, insp, f.key), f.supervisor, answerBody("pass", "", rfc(c.Earlier(10*time.Minute)), f.photo2), 200),
		[2]string{"inspection", insp})
	action := uuid.NewString()
	step("raise on a finding", put(actionPath(f.siteA, action), f.owner, f.raiseBody(f.supervisorID, c.Earlier(5*time.Minute)), 201),
		[2]string{"action", action}, [2]string{"finding_list", f.inspection})
	step("an event", put(eventPath(f.siteA, action, uuid.NewString()), f.owner,
		eventBody("move_due", c.Earlier(4*time.Minute), map[string]any{"due_on": c.Day(30), "reason": "Steel arrives later"}), 201),
		[2]string{"action", action})
	step("a correction completes", func() {
		completeFailing(t, f.answerFixture, f.inspection, map[string]string{f.item: "high"}, f.failPhoto)
	},
		[2]string{"finding_list", f.inspection}, [2]string{"action", action})

	// Incidents and deadlines.
	inc := uuid.NewString()
	var deadline string
	step("an incident", func() {
		res := do(t, f.router, http.MethodPut, versionPath(f.siteA, inc, 1), f.supervisor, versionBody(c.Earlier(50*time.Minute), "unconsciousness"))
		wantStatus(t, res, http.StatusCreated)
		for _, d := range decodeIncident(t, res).Deadlines {
			if d.ClosedAs == nil {
				deadline = d.ID
			}
		}
	}, [2]string{"incident", inc})
	step("a statement", put(statementPath(f.siteA, inc, uuid.NewString()), f.supervisor,
		map[string]any{"witness_name": "Thandi", "statement": "I saw it", "taken_at": rfc(c.Earlier(30 * time.Minute))}, 201),
		[2]string{"incident", inc})
	step("a satisfaction", put(satisfactionPath(f.siteA, deadline, uuid.NewString()), f.supervisor,
		map[string]any{"satisfied_at": rfc(c.Earlier(20 * time.Minute)), "method": "phone"}, 201),
		[2]string{"deadline", deadline}, [2]string{"incident", inc})

	// Talks, sign-ins, the roster, workers.
	talk := uuid.NewString()
	step("a talk", put(talkPath(f.siteA, talk), f.supervisor, talkBody(map[string]any{"started_at": rfc(c.Earlier(45 * time.Minute))}), 201),
		[2]string{"toolbox_talk", talk})
	att := uuid.NewString()
	step("a signature", put(signPath(f.siteA, talk, att), f.supervisor, signBody("Mandla Z", c.Earlier(40*time.Minute), nil), 201),
		[2]string{"toolbox_talk", talk})
	step("a retraction", post(signPath(f.siteA, talk, att)+"/retract", f.supervisor,
		map[string]any{"reason": "Wrong person", "client_retracted_at": rfc(c.Earlier(35 * time.Minute))}, 201),
		[2]string{"toolbox_talk", talk})
	w := newWorker(t, f.router, f.owner, map[string]any{"name": "Deps Worker " + uuid.NewString()[:6]})
	step("a sign-in", put(signInPath(f.siteA, uuid.NewString()), f.supervisor, signInBody(w, c.Earlier(30*time.Minute), nil), 201),
		[2]string{"sign_in_day", c.Today()}, [2]string{"roster", f.siteA})
	step("a rename", put(workerPath(w), f.owner, map[string]any{"name": "Renamed " + w[:6]}, 200),
		[2]string{"roster", f.siteA})
	org := pullOrg(t, f.router, f.owner, orgCursor)
	if !has(org.Items, "worker", w) {
		t.Errorf("org delta lacks the worker: %v", kindsOf(org.Items))
	}

	// The edition's records and locations.
	rev := uuid.NewString()
	step("a diary", put(revisionPath(f.siteA, c.Today(), rev), f.supervisor, revisionBody(c.Earlier(5*time.Minute), nil), 201),
		[2]string{"diary_day", c.Today()})
	step("a sign-in moves the day's suggestion", put(signInPath(f.siteA, uuid.NewString()), f.supervisor,
		signInBody(newWorker(t, f.router, f.owner, map[string]any{"name": "Late " + uuid.NewString()[:6]}), c.Earlier(3*time.Minute), nil), 201),
		[2]string{"diary_day", c.Today()})
	work := uuid.NewString()
	step("site work", put(workVersionPath(f.siteA, work, 1), f.owner, workBody(c, c.Day(20), "excavation"), 201),
		[2]string{"site_work", work}, [2]string{"deadline", ""})
	step("a location", post("/v1/sites/"+f.siteA+"/locations", f.owner, map[string]any{"kind": "zone", "name": "Zone " + uuid.NewString()[:6]}, 201),
		[2]string{"location_list", f.siteA})
}

// wantErrorText checks the status and the error text of a body that carries more.
func wantErrorText(t *testing.T, rec *httptest.ResponseRecorder, status int, msg string) {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != status || body.Error != msg {
		t.Errorf("got %d %s, want %d %q", rec.Code, rec.Body.String(), status, msg)
	}
}
