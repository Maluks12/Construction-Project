package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"construction_db/internal/core/actions"
	"construction_db/internal/core/auth"
	"construction_db/internal/core/checklists"
	"construction_db/internal/platform/database"
)

func actionPath(site, id string) string { return "/v1/sites/" + site + "/actions/" + id }
func eventPath(site, action, id string) string {
	return actionPath(site, action) + "/events/" + id
}
func closeoutPath(site, action, id string) string {
	return actionPath(site, action) + "/closeouts/" + id
}
func waiverPath(site, id string) string { return "/v1/sites/" + site + "/waivers/" + id }

var joburg = func() *time.Location {
	loc, err := time.LoadLocation("Africa/Johannesburg")
	if err != nil {
		panic(err)
	}
	return loc
}()

// localDay is t's date in the regime's zone plus days.
func localDay(t time.Time, days int) string {
	return t.In(joburg).AddDate(0, 0, days).Format(time.DateOnly)
}

// photoTakenAt declares, uploads and completes a JPEG captured at at.
func photoTakenAt(t *testing.T, f evidenceFixture, token, site string, at time.Time) string {
	t.Helper()
	content := testImage(t, "jpeg", 41, 31)
	id := uuid.NewString()
	body := declareFor(content, "image/jpeg")
	body["client_captured_at"] = rfc(at)
	rec := do(t, f.router, http.MethodPut, evidencePath(site, id), token, body)
	wantStatus(t, rec, http.StatusCreated)
	putUpload(t, decodeEvidence(t, rec).Upload, content)
	wantStatus(t, do(t, f.router, http.MethodPost, completePath(site, id), token, nil), http.StatusCreated)
	return id
}

func decodeAction(t *testing.T, rec *httptest.ResponseRecorder) actions.Action {
	t.Helper()
	var a actions.Action
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil {
		t.Fatalf("decode action: %v (body: %s)", err, rec.Body.String())
	}
	return a
}

func listActions(t *testing.T, router http.Handler, token, path string) []actions.ActionHeader {
	t.Helper()
	rec := do(t, router, http.MethodGet, path, token, nil)
	wantStatus(t, rec, http.StatusOK)
	var body struct {
		Actions []actions.ActionHeader `json:"actions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode actions: %v (body: %s)", err, rec.Body.String())
	}
	return body.Actions
}

func actionIDs(hs []actions.ActionHeader) []string {
	out := []string{}
	for _, h := range hs {
		out = append(out, h.ID)
	}
	return out
}

// actionFixture: a completed inspection on site A whose first item failed
// (high) with a photo taken after the fail was answered, and photos for
// close-outs.
type actionFixture struct {
	answerFixture
	supervisorID, ownerID string
	inspection, item      string // the finding
	failPhoto             string // the finding's own photo
	fixPhoto, fixPhoto2   string // taken now: can show a fix
}

// The fail is answered at t1 (14 Nov), so that's the finding's source time.
func newActionFixture(t *testing.T, name string) actionFixture {
	t.Helper()
	a := newAnswerFixture(t, name)
	f := actionFixture{answerFixture: a, supervisorID: me(t, a.router, a.supervisor).UserID, ownerID: a.ownerMe.UserID}
	f.item = a.tmpl.Items[0].Key
	f.failPhoto = photoTakenAt(t, a.evidenceFixture, a.supervisor, a.siteA, testClock.Now().Add(-2*time.Hour))
	f.fixPhoto = photoTakenAt(t, a.evidenceFixture, a.supervisor, a.siteA, testClock.Now())
	f.fixPhoto2 = photoTakenAt(t, a.evidenceFixture, a.supervisor, a.siteA, testClock.Now())
	f.inspection = completeFailing(t, a, "", map[string]string{f.item: "high"}, f.failPhoto)
	return f
}

// completeFailing is completeWithFails with a chosen photo on each fail.
func completeFailing(t *testing.T, a answerFixture, corrects string, fails map[string]string, photo string) string {
	t.Helper()
	id := uuid.NewString()
	body := startBody(a.tmpl)
	if corrects != "" {
		body = with(body, "corrects_id", corrects)
	}
	wantStatus(t, do(t, a.router, http.MethodPut, inspectionPath(a.siteA, id), a.supervisor, body), http.StatusCreated)
	answerAllBySQL(t, a.ownerMe.OrgID, id, len(a.tmpl.Items))
	for key, sev := range fails {
		wantStatus(t, do(t, a.router, http.MethodPut, itemPath(a.siteA, id, key), a.supervisor, answerBody("fail", sev, t1, photo)), http.StatusOK)
	}
	wantStatus(t, do(t, a.router, http.MethodPost, completePathFor(a.siteA, id), a.supervisor, completeBody()), http.StatusCreated)
	return id
}

// raiseBody raises on the fixture's finding, owned by owner, raised a
// minute ago (a fixed time, so a repeat sends the same).
func (f actionFixture) raiseBody(owner string, raisedAt time.Time) map[string]any {
	return map[string]any{
		"finding":          map[string]any{"inspection_id": f.inspection, "item_key": f.item},
		"title":            "Fit the missing guardrail",
		"owner_id":         owner,
		"client_raised_at": rfc(raisedAt),
	}
}

func (f actionFixture) raise(t *testing.T, token string, body map[string]any) actions.Action {
	t.Helper()
	rec := do(t, f.router, http.MethodPut, actionPath(f.siteA, uuid.NewString()), token, body)
	wantStatus(t, rec, http.StatusCreated)
	return decodeAction(t, rec)
}

func closeoutBody(at time.Time, photos ...string) map[string]any {
	return map[string]any{"evidence_ids": append([]string{}, photos...), "client_submitted_at": rfc(at), "note": "Guardrail fitted"}
}

func eventBody(kind string, at time.Time, fields map[string]any) map[string]any {
	b := map[string]any{"kind": kind, "client_at": rfc(at)}
	for k, v := range fields {
		b[k] = v
	}
	return b
}

func wantProblem(t *testing.T, rec *httptest.ResponseRecorder, status int, msg string, ids ...string) {
	t.Helper()
	var body struct {
		Error       string   `json:"error"`
		EvidenceIDs []string `json:"evidence_ids"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != status || body.Error != msg || !slices.Equal(body.EvidenceIDs, ids) {
		t.Errorf("got %d %s, want %d %q with evidence_ids %v", rec.Code, rec.Body.String(), status, msg, ids)
	}
}

func TestRaiseAction(t *testing.T) {
	f := newActionFixture(t, "raise")
	clk := newSASTClock(t)
	raisedAt := clk.Earlier(time.Minute)
	id := uuid.NewString()
	body := f.raiseBody(f.supervisorID, raisedAt)

	rec := do(t, f.router, http.MethodPut, actionPath(f.siteA, id), f.supervisor, body)
	wantStatus(t, rec, http.StatusCreated)
	created := rec.Body.String()
	a := decodeAction(t, rec)

	t.Run("takes the finding's severity and its default due date", func(t *testing.T) {
		if a.ID != id || a.Source != actions.SourceFinding || a.Severity != "high" || a.Status != actions.StatusOpen || a.State != actions.StateOpen ||
			a.OwnerID != f.supervisorID || a.RaisedBy != f.supervisorID || a.TimeZone != "Africa/Johannesburg" {
			t.Errorf("action %+v", a.ActionHeader)
		}
		if want := localDay(raisedAt, 1); a.DefaultDueOn != want || a.DueOn != want {
			t.Errorf("default %s, due %s; want %s (high: the next day)", a.DefaultDueOn, a.DueOn, want)
		}
		if len(a.Events) != 1 || a.Events[0].Kind != actions.KindRaised || a.Events[0].ID != id || len(a.Closeouts) != 0 {
			t.Errorf("events %+v, close-outs %+v; want only the raise", a.Events, a.Closeouts)
		}
		if a.SourceAt.Format(time.RFC3339Nano) != t1 {
			t.Errorf("source_at %s, want the fail's answer time %s", a.SourceAt.Format(time.RFC3339Nano), t1)
		}
	})

	t.Run("an identical repeat is 200 with the same body", func(t *testing.T) {
		rec := do(t, f.router, http.MethodPut, actionPath(f.siteA, id), f.supervisor, body)
		wantStatus(t, rec, http.StatusOK)
		if rec.Body.String() != created {
			t.Errorf("repeat body differs:\n%s\n%s", rec.Body.String(), created)
		}
	})

	t.Run("the id with other details is 409", func(t *testing.T) {
		wantError(t, do(t, f.router, http.MethodPut, actionPath(f.siteA, id), f.supervisor, with(body, "title", "Other")), http.StatusConflict,
			actions.ErrActionIDReused.Error())
		wantError(t, do(t, f.router, http.MethodPut, actionPath(f.siteA, id), f.owner, body), http.StatusConflict, actions.ErrActionIDReused.Error())
		wantError(t, do(t, f.router, http.MethodPut, actionPath(f.siteB, id), f.owner, body), http.StatusConflict, actions.ErrActionIDReused.Error())
	})

	t.Run("the finding now counts as actioned", func(t *testing.T) {
		for _, fi := range listFindings(t, f.router, f.owner, f.siteA, "") {
			if fi.InspectionID == f.inspection && (fi.Coverage != checklists.CoverageActioned || !slices.Equal(fi.CoveredBy, []string{id})) {
				t.Errorf("finding coverage %s by %v, want actioned by %s", fi.Coverage, fi.CoveredBy, id)
			}
		}
	})

	t.Run("due dates", func(t *testing.T) {
		later := with(f.raiseBody(f.supervisorID, raisedAt), "due_on", localDay(raisedAt, 3))
		wantError(t, do(t, f.router, http.MethodPut, actionPath(f.siteA, uuid.NewString()), f.supervisor, later), http.StatusForbidden,
			actions.ErrLaterNeedsLead.Error())
		wantError(t, do(t, f.router, http.MethodPut, actionPath(f.siteA, uuid.NewString()), f.owner, later), http.StatusBadRequest,
			actions.ErrDueReasonRequired.Error())
		a := f.raise(t, f.owner, with(later, "due_reason", "Scaffold hire arrives Wednesday"))
		if a.DueOn != localDay(raisedAt, 3) || a.Events[0].Reason == nil {
			t.Errorf("due %s, reason %v; want the later date with its reason", a.DueOn, a.Events[0].Reason)
		}
		f.raise(t, f.supervisor, with(f.raiseBody(f.supervisorID, raisedAt), "due_on", localDay(raisedAt, 0)))
		wantError(t, do(t, f.router, http.MethodPut, actionPath(f.siteA, uuid.NewString()), f.owner,
			with(body, "due_on", localDay(raisedAt, -1))), http.StatusBadRequest, actions.ErrDueBeforeRaised.Error())
	})

	t.Run("the owner sees the site", func(t *testing.T) {
		books := me(t, f.router, f.bookkeeper).UserID
		wantError(t, do(t, f.router, http.MethodPut, actionPath(f.siteA, uuid.NewString()), f.owner, f.raiseBody(books, raisedAt)),
			http.StatusBadRequest, actions.ErrOwnerNotEligible.Error())
		a := f.raise(t, f.owner, with(f.raiseBody(f.ownerID, raisedAt), "responsible_party", "ABC Scaffolding, J. Dlamini"))
		if a.ResponsibleParty == nil || *a.ResponsibleParty != "ABC Scaffolding, J. Dlamini" {
			t.Errorf("responsible_party %v", a.ResponsibleParty)
		}
	})

	t.Run("validation", func(t *testing.T) {
		for name, tc := range map[string]struct {
			body map[string]any
			msg  string
		}{
			"no source":         {with(body, "finding", nil), actions.ErrOneSource.Error()},
			"two sources":       {with(body, "incident", map[string]any{"incident_id": uuid.NewString(), "version": 1}), actions.ErrOneSource.Error()},
			"bad finding":       {with(body, "finding", map[string]any{"inspection_id": "x", "item_key": f.item}), actions.ErrBadFinding.Error()},
			"unknown finding":   {with(body, "finding", map[string]any{"inspection_id": f.inspection, "item_key": "nope"}), actions.ErrFindingNotFound.Error()},
			"other severity":    {with(body, "severity", "low"), actions.ErrSeverityMismatch.Error()},
			"bad severity":      {with(body, "severity", "urgent"), actions.ErrBadSeverity.Error()},
			"no title":          {with(body, "title", ""), actions.ErrBadTitle.Error()},
			"bad owner":         {with(body, "owner_id", "me"), actions.ErrBadOwnerID.Error()},
			"bad due":           {with(body, "due_on", "Friday"), actions.ErrBadDueOn.Error()},
			"future":            {with(body, "client_raised_at", rfc(clk.Now().Add(time.Hour))), actions.ErrRaisedInFuture.Error()},
			"no time":           {with(body, "client_raised_at", nil), actions.ErrNoRaisedAt.Error()},
			"empty party":       {with(body, "responsible_party", ""), actions.ErrBadParty.Error()},
			"incident severity": {with(with(body, "finding", nil), "incident", map[string]any{"incident_id": uuid.NewString(), "version": 1}), actions.ErrSeverityRequired.Error()},
		} {
			t.Run(name, func(t *testing.T) {
				wantError(t, do(t, f.router, http.MethodPut, actionPath(f.siteA, uuid.NewString()), f.owner, tc.body), http.StatusBadRequest, tc.msg)
			})
		}
		wantError(t, do(t, f.router, http.MethodPut, actionPath(f.siteA, "not-a-uuid"), f.owner, body), http.StatusBadRequest, actions.ErrBadActionID.Error())
	})

	t.Run("who may", func(t *testing.T) {
		wantStatus(t, do(t, f.router, http.MethodPut, actionPath(f.siteA, uuid.NewString()), f.bookkeeper, body), http.StatusForbidden)
		wantError(t, do(t, f.router, http.MethodPut, actionPath(f.siteB, uuid.NewString()), f.supervisor, body), http.StatusNotFound, "site not found")
		wantError(t, do(t, f.router, http.MethodPut, actionPath(f.otherSite, uuid.NewString()), f.owner, body), http.StatusNotFound, "site not found")
		// The finding is on site A, so not on site B.
		wantError(t, do(t, f.router, http.MethodPut, actionPath(f.siteB, uuid.NewString()), f.owner, body), http.StatusBadRequest,
			actions.ErrFindingNotFound.Error())
	})

	t.Run("an archived site takes no new action; a repeat still answers", func(t *testing.T) {
		wantStatus(t, do(t, f.router, http.MethodDelete, "/v1/sites/"+f.siteA, f.owner, nil), http.StatusNoContent)
		wantError(t, do(t, f.router, http.MethodPut, actionPath(f.siteA, uuid.NewString()), f.owner, body), http.StatusConflict,
			actions.ErrSiteArchived.Error())
		wantStatus(t, do(t, f.router, http.MethodPut, actionPath(f.siteA, id), f.supervisor, body), http.StatusOK)
		wantStatus(t, do(t, f.router, http.MethodGet, actionPath(f.siteA, id), f.supervisor, nil), http.StatusOK)
	})
}

func TestActionCloseoutAndVerification(t *testing.T) {
	f := newActionFixture(t, "closeout")
	clk := newSASTClock(t)
	now := clk.Now()
	a := f.raise(t, f.supervisor, f.raiseBody(f.supervisorID, clk.Earlier(time.Minute)))
	path := func(id string) string { return closeoutPath(f.siteA, a.ID, id) }
	early := photoTakenAt(t, f.evidenceFixture, f.supervisor, f.siteA, clk.Earlier(30*time.Minute)) // after the source, before the raise

	t.Run("photos that can't show the fix", func(t *testing.T) {
		wantError(t, do(t, f.router, http.MethodPut, path(uuid.NewString()), f.supervisor, closeoutBody(now)), http.StatusBadRequest, actions.ErrNoPhotos.Error())
		wantProblem(t, do(t, f.router, http.MethodPut, path(uuid.NewString()), f.supervisor, closeoutBody(now, f.photo)), http.StatusBadRequest,
			actions.ErrEvidenceTooOld.Error(), f.photo)
		wantProblem(t, do(t, f.router, http.MethodPut, path(uuid.NewString()), f.supervisor, closeoutBody(now, f.failPhoto)), http.StatusBadRequest,
			actions.ErrEvidenceIsSource.Error(), f.failPhoto)
		unknown := uuid.NewString()
		wantProblem(t, do(t, f.router, http.MethodPut, path(uuid.NewString()), f.supervisor, closeoutBody(now, unknown)), http.StatusBadRequest,
			actions.ErrEvidenceNotFound.Error(), unknown)
	})

	t.Run("only the owner or a lead closes out", func(t *testing.T) {
		mine := f.raise(t, f.owner, f.raiseBody(f.ownerID, clk.Earlier(time.Minute)))
		wantError(t, do(t, f.router, http.MethodPut, closeoutPath(f.siteA, mine.ID, uuid.NewString()), f.supervisor, closeoutBody(now, f.fixPhoto)),
			http.StatusForbidden, actions.ErrCantCloseOut.Error())
	})

	co := uuid.NewString()
	var awaiting actions.Action
	t.Run("a close-out awaits verification", func(t *testing.T) {
		rec := do(t, f.router, http.MethodPut, path(co), f.supervisor, closeoutBody(now, f.fixPhoto, early))
		wantStatus(t, rec, http.StatusCreated)
		awaiting = decodeAction(t, rec)
		if awaiting.State != actions.StateAwaiting || awaiting.PendingCloseoutID == nil || *awaiting.PendingCloseoutID != co || awaiting.AwaitingSince == nil {
			t.Fatalf("action %+v, want awaiting %s", awaiting.ActionHeader, co)
		}
		c := awaiting.Closeouts[0]
		if !c.SubmittedAt.Equal(*awaiting.AwaitingSince) || len(c.Evidence) != 2 || c.Decision != nil || c.Late {
			t.Errorf("close-out %+v", c)
		}
		for _, e := range c.Evidence {
			if e.CapturedBeforeRaised != (e.EvidenceID == early) {
				t.Errorf("photo %s captured_before_raised %v", e.EvidenceID, e.CapturedBeforeRaised)
			}
		}
		wantStatus(t, do(t, f.router, http.MethodPut, path(co), f.supervisor, closeoutBody(now, early, f.fixPhoto)), http.StatusOK)
		// The same retry with the action id in another case is still a repeat.
		wantStatus(t, do(t, f.router, http.MethodPut, closeoutPath(f.siteA, strings.ToUpper(a.ID), co), f.supervisor, closeoutBody(now, early, f.fixPhoto)),
			http.StatusOK)
		wantError(t, do(t, f.router, http.MethodPut, closeoutPath(f.siteA, "not-a-uuid", co), f.supervisor, closeoutBody(now, f.fixPhoto)),
			http.StatusNotFound, actions.ErrActionNotFound.Error())
		wantError(t, do(t, f.router, http.MethodPut, path(co), f.supervisor, closeoutBody(now, f.fixPhoto)), http.StatusConflict,
			actions.ErrCloseoutIDReused.Error())
		wantError(t, do(t, f.router, http.MethodPut, path(uuid.NewString()), f.supervisor, closeoutBody(now, f.fixPhoto2)), http.StatusConflict,
			actions.ErrAlreadyAwaiting.Error())
	})

	ev := func(token string, body map[string]any) *httptest.ResponseRecorder {
		return do(t, f.router, http.MethodPut, eventPath(f.siteA, a.ID, uuid.NewString()), token, body)
	}

	t.Run("verifying", func(t *testing.T) {
		verify := eventBody(actions.DoVerify, now, map[string]any{"closeout_id": co})
		wantError(t, ev(f.supervisor, verify), http.StatusForbidden, actions.ErrNeedsLead.Error())
		wantError(t, ev(f.owner, eventBody(actions.DoVerify, now, map[string]any{"closeout_id": uuid.NewString()})), http.StatusConflict,
			actions.ErrNotPending.Error())
		wantError(t, ev(f.owner, eventBody(actions.DoVerify, now, nil)), http.StatusBadRequest, actions.ErrBadCloseoutRef.Error())

		id := uuid.NewString()
		rec := do(t, f.router, http.MethodPut, eventPath(f.siteA, a.ID, id), f.owner, verify)
		wantStatus(t, rec, http.StatusCreated)
		closed := decodeAction(t, rec)
		last := closed.Events[len(closed.Events)-1]
		if closed.State != actions.StateClosed || closed.ClosedAt == nil || closed.AwaitingSince != nil || last.SelfVerified == nil || *last.SelfVerified ||
			closed.Closeouts[0].Decision == nil || *closed.Closeouts[0].Decision != actions.KindVerified {
			t.Errorf("after verifying: %+v, last event %+v", closed.ActionHeader, last)
		}
		wantStatus(t, do(t, f.router, http.MethodPut, eventPath(f.siteA, a.ID, id), f.owner, verify), http.StatusOK)
		wantStatus(t, do(t, f.router, http.MethodPut, eventPath(f.siteA, strings.ToUpper(a.ID), strings.ToUpper(id)), f.owner, verify), http.StatusOK)
		wantError(t, do(t, f.router, http.MethodPut, eventPath(f.siteA, "not-a-uuid", id), f.owner, verify), http.StatusNotFound,
			actions.ErrActionNotFound.Error())
		wantError(t, do(t, f.router, http.MethodPut, eventPath(f.siteA, a.ID, id), f.owner, eventBody(actions.DoCancel, now, map[string]any{"reason": "x"})),
			http.StatusConflict, actions.ErrEventIDReused.Error())
	})

	t.Run("a closed action changes only by reopening", func(t *testing.T) {
		wantError(t, ev(f.owner, eventBody(actions.DoReassign, now, map[string]any{"owner_id": f.ownerID})), http.StatusConflict, actions.ErrActionClosed.Error())
		wantError(t, do(t, f.router, http.MethodPut, path(uuid.NewString()), f.supervisor, closeoutBody(now, f.fixPhoto2)), http.StatusConflict,
			actions.ErrActionClosed.Error())
		wantError(t, ev(f.supervisor, eventBody(actions.DoReopen, now, map[string]any{"reason": "Loose again"})), http.StatusForbidden, actions.ErrNeedsLead.Error())
		wantError(t, ev(f.owner, eventBody(actions.DoReopen, now, nil)), http.StatusBadRequest, actions.ErrReasonRequired.Error())
		rec := ev(f.owner, eventBody(actions.DoReopen, now, map[string]any{"reason": "Loose again"}))
		wantStatus(t, rec, http.StatusCreated)
		if s := decodeAction(t, rec).State; s != actions.StateOpen {
			t.Errorf("state %s after reopening, want open", s)
		}
		wantError(t, ev(f.owner, eventBody(actions.DoReopen, now, map[string]any{"reason": "Again"})), http.StatusConflict, actions.ErrNotClosed.Error())
	})

	t.Run("rejecting, then a lead's own close-out", func(t *testing.T) {
		co2 := uuid.NewString()
		wantStatus(t, do(t, f.router, http.MethodPut, path(co2), f.owner, closeoutBody(now, f.fixPhoto2)), http.StatusCreated)
		wantError(t, ev(f.owner, eventBody(actions.DoReject, now, map[string]any{"closeout_id": co2})), http.StatusBadRequest, actions.ErrReasonRequired.Error())
		rec := ev(f.owner, eventBody(actions.DoReject, now, map[string]any{"closeout_id": co2, "reason": "Photo doesn't show the rail"}))
		wantStatus(t, rec, http.StatusCreated)
		if s := decodeAction(t, rec).State; s != actions.StateOpen {
			t.Errorf("state %s after rejecting, want open", s)
		}
		co3 := uuid.NewString()
		wantStatus(t, do(t, f.router, http.MethodPut, path(co3), f.owner, closeoutBody(now, f.fixPhoto2)), http.StatusCreated)
		rec = ev(f.owner, eventBody(actions.DoVerify, now, map[string]any{"closeout_id": co3}))
		wantStatus(t, rec, http.StatusCreated)
		got := decodeAction(t, rec)
		if last := got.Events[len(got.Events)-1]; last.SelfVerified == nil || !*last.SelfVerified {
			t.Errorf("self_verified %v, want true for a lead verifying their own close-out", last.SelfVerified)
		}
	})
}

func TestActionEvents(t *testing.T) {
	f := newActionFixture(t, "events")
	clk := newSASTClock(t)
	now := clk.Now()
	a := f.raise(t, f.owner, f.raiseBody(f.supervisorID, clk.Earlier(time.Minute)))
	ev := func(token string, body map[string]any) *httptest.ResponseRecorder {
		return do(t, f.router, http.MethodPut, eventPath(f.siteA, a.ID, uuid.NewString()), token, body)
	}

	t.Run("reassigning", func(t *testing.T) {
		wantError(t, ev(f.supervisor, eventBody(actions.DoReassign, now, map[string]any{"owner_id": f.ownerID})), http.StatusForbidden,
			actions.ErrCantReassign.Error())
		wantError(t, ev(f.owner, eventBody(actions.DoReassign, now, map[string]any{"owner_id": f.supervisorID})), http.StatusBadRequest,
			actions.ErrNothingChanges.Error())
		wantError(t, ev(f.owner, eventBody(actions.DoReassign, now, map[string]any{"owner_id": me(t, f.router, f.bookkeeper).UserID})),
			http.StatusBadRequest, actions.ErrOwnerNotEligible.Error())
		rec := ev(f.owner, eventBody(actions.DoReassign, now, map[string]any{"owner_id": f.supervisorID, "responsible_party": "Sub"}))
		wantStatus(t, rec, http.StatusCreated)
		if got := decodeAction(t, rec); got.ResponsibleParty == nil || *got.ResponsibleParty != "Sub" {
			t.Errorf("responsible_party %v, want Sub", got.ResponsibleParty)
		}
	})

	t.Run("moving the due date", func(t *testing.T) {
		later := eventBody(actions.DoMoveDue, now, map[string]any{"due_on": localDay(now, 5)})
		wantError(t, ev(f.supervisor, with(later, "reason", "Rain")), http.StatusForbidden, actions.ErrLaterNeedsLead.Error())
		wantError(t, ev(f.owner, later), http.StatusBadRequest, actions.ErrMoveLaterReason.Error())
		wantStatus(t, ev(f.owner, with(later, "reason", "Rain")), http.StatusCreated)
		// Earlier: not a supervisor who is neither the raiser nor the owner.
		email := "second-" + uuid.NewString()[:8] + "@actions.test"
		sup2ID := inviteMember(t, f.router, f.owner, email, auth.RoleSupervisor)
		wantStatus(t, do(t, f.router, http.MethodPut, "/v1/sites/"+f.siteA+"/members/"+sup2ID, f.owner, nil), http.StatusNoContent)
		sup2 := login(t, f.router, email)
		wantError(t, ev(sup2, eventBody(actions.DoMoveDue, now, map[string]any{"due_on": localDay(now, 4)})), http.StatusForbidden,
			actions.ErrCantMoveEarlier.Error())
		// Earlier: the owner (the supervisor) may.
		rec := ev(f.supervisor, eventBody(actions.DoMoveDue, now, map[string]any{"due_on": localDay(now, 2)}))
		wantStatus(t, rec, http.StatusCreated)
		if d := decodeAction(t, rec).DueOn; d != localDay(now, 2) {
			t.Errorf("due %s, want %s", d, localDay(now, 2))
		}
		wantError(t, ev(f.owner, eventBody(actions.DoMoveDue, now, map[string]any{"due_on": localDay(now, -1)})), http.StatusBadRequest,
			actions.ErrDueInPast.Error())
		wantError(t, ev(f.owner, eventBody(actions.DoMoveDue, now, map[string]any{"due_on": localDay(now, 2)})), http.StatusBadRequest,
			actions.ErrNothingChanges.Error())
	})

	t.Run("validation", func(t *testing.T) {
		for name, tc := range map[string]struct {
			body map[string]any
			msg  string
		}{
			"bad kind":         {eventBody("close", now, nil), actions.ErrBadKind.Error()},
			"no time":          {map[string]any{"kind": actions.DoCancel, "reason": "x"}, actions.ErrNoClientAt.Error()},
			"future":           {eventBody(actions.DoCancel, now.Add(time.Hour), map[string]any{"reason": "x"}), actions.ErrClientAtInFuture.Error()},
			"extra field":      {eventBody(actions.DoCancel, now, map[string]any{"reason": "x", "due_on": localDay(now, 1)}), actions.ErrUnexpectedField.Error()},
			"no reason":        {eventBody(actions.DoCancel, now, nil), actions.ErrReasonRequired.Error()},
			"long reason":      {eventBody(actions.DoCancel, now, map[string]any{"reason": strings.Repeat("x", 501)}), actions.ErrBadReason.Error()},
			"reassign no one":  {eventBody(actions.DoReassign, now, nil), actions.ErrBadOwnerID.Error()},
			"move without due": {eventBody(actions.DoMoveDue, now, nil), actions.ErrBadDueOn.Error()},
		} {
			t.Run(name, func(t *testing.T) { wantError(t, ev(f.owner, tc.body), http.StatusBadRequest, tc.msg) })
		}
		wantError(t, do(t, f.router, http.MethodPut, eventPath(f.siteA, a.ID, "x"), f.owner, eventBody(actions.DoCancel, now, map[string]any{"reason": "x"})),
			http.StatusBadRequest, actions.ErrBadEventID.Error())
		wantError(t, do(t, f.router, http.MethodPut, eventPath(f.siteA, uuid.NewString(), uuid.NewString()), f.owner,
			eventBody(actions.DoCancel, now, map[string]any{"reason": "x"})), http.StatusNotFound, actions.ErrActionNotFound.Error())
		// The raise's event id is the action's.
		wantError(t, do(t, f.router, http.MethodPut, eventPath(f.siteA, a.ID, a.ID), f.owner, eventBody(actions.DoCancel, now, map[string]any{"reason": "x"})),
			http.StatusConflict, actions.ErrEventIDReused.Error())
	})

	t.Run("cancelling", func(t *testing.T) {
		wantError(t, ev(f.supervisor, eventBody(actions.DoCancel, now, map[string]any{"reason": "Duplicate"})), http.StatusForbidden, actions.ErrNeedsLead.Error())
		rec := ev(f.owner, eventBody(actions.DoCancel, now, map[string]any{"reason": "Duplicate"}))
		wantStatus(t, rec, http.StatusCreated)
		if s := decodeAction(t, rec).State; s != actions.StateCancelled {
			t.Errorf("state %s, want cancelled", s)
		}
		wantError(t, ev(f.owner, eventBody(actions.DoReopen, now, map[string]any{"reason": "x"})), http.StatusConflict, actions.ErrActionCancelled.Error())
		for _, fi := range listFindings(t, f.router, f.owner, f.siteA, "?coverage=unactioned") {
			if fi.InspectionID == f.inspection {
				return
			}
		}
		t.Errorf("the finding isn't unactioned after its only action was cancelled")
	})
}

func TestActionRegister(t *testing.T) {
	f := newActionFixture(t, "register")
	clk := newSASTClock(t)
	now := clk.Now()
	// Raised three days ago, due the next day: overdue now.
	old := f.raise(t, f.owner, f.raiseBody(f.supervisorID, now.AddDate(0, 0, -3)))
	fresh := f.raise(t, f.owner, f.raiseBody(f.ownerID, clk.Earlier(time.Minute)))

	if old.State != actions.StateOverdue || fresh.State != actions.StateOpen {
		t.Fatalf("states %s and %s, want overdue and open", old.State, fresh.State)
	}

	// An action on site B, where the supervisor isn't.
	photoB := photoTakenAt(t, f.evidenceFixture, f.owner, f.siteB, clk.Earlier(time.Hour))
	inspB := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, inspectionPath(f.siteB, inspB), f.owner, startBody(f.tmpl)), http.StatusCreated)
	answerAllBySQL(t, f.ownerMe.OrgID, inspB, len(f.tmpl.Items))
	wantStatus(t, do(t, f.router, http.MethodPut, itemPath(f.siteB, inspB, f.item), f.owner, answerBody("fail", "low", t1, photoB)), http.StatusOK)
	wantStatus(t, do(t, f.router, http.MethodPost, completePathFor(f.siteB, inspB), f.owner, completeBody()), http.StatusCreated)
	onB := decodeAction(t, do(t, f.router, http.MethodPut, actionPath(f.siteB, uuid.NewString()), f.owner, map[string]any{
		"finding": map[string]any{"inspection_id": inspB, "item_key": f.item}, "title": "Tidy up", "owner_id": f.ownerID,
		"client_raised_at": rfc(clk.Earlier(time.Minute)),
	}))

	site := "/v1/sites/" + f.siteA + "/actions"
	for name, tc := range map[string]struct {
		token, path string
		want        []string
	}{
		"site, due soonest first": {f.owner, site, []string{old.ID, fresh.ID}},
		"overdue":                 {f.owner, site + "?state=overdue", []string{old.ID}},
		"open":                    {f.owner, site + "?state=open", []string{fresh.ID}},
		"mine":                    {f.owner, site + "?owner=me", []string{fresh.ID}},
		"by owner id":             {f.owner, site + "?owner=" + f.supervisorID, []string{old.ID}},
		"from findings":           {f.owner, site + "?source=incident", []string{}},
		"limit":                   {f.owner, site + "?limit=1", []string{old.ID}},
		"all sites, a lead":       {f.owner, "/v1/actions", []string{old.ID, fresh.ID, onB.ID}},
		"all sites, a supervisor": {f.supervisor, "/v1/actions", []string{old.ID, fresh.ID}},
		"overdue everywhere":      {f.owner, "/v1/actions?state=overdue", []string{old.ID}},
		"my overdue":              {f.supervisor, "/v1/actions?state=overdue&owner=me", []string{old.ID}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := actionIDs(listActions(t, f.router, tc.token, tc.path)); !slices.Equal(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}

	for query, msg := range map[string]string{
		"?state=late":    actions.ErrBadState.Error(),
		"?source=hazard": actions.ErrBadSource.Error(),
		"?owner=you":     actions.ErrBadOwner.Error(),
		"?limit=0":       actions.ErrBadLimit.Error(),
		"?limit=lots":    actions.ErrBadLimit.Error(),
	} {
		t.Run("bad query "+query, func(t *testing.T) {
			wantError(t, do(t, f.router, http.MethodGet, site+query, f.owner, nil), http.StatusBadRequest, msg)
		})
	}

	t.Run("scoping", func(t *testing.T) {
		wantError(t, do(t, f.router, http.MethodGet, "/v1/sites/"+f.siteB+"/actions", f.supervisor, nil), http.StatusNotFound, "site not found")
		wantError(t, do(t, f.router, http.MethodGet, actionPath(f.siteB, onB.ID), f.supervisor, nil), http.StatusNotFound, "site not found")
		wantError(t, do(t, f.router, http.MethodGet, actionPath(f.siteA, onB.ID), f.owner, nil), http.StatusNotFound, actions.ErrActionNotFound.Error())
		wantError(t, do(t, f.router, http.MethodGet, actionPath(f.siteA, "x"), f.owner, nil), http.StatusNotFound, actions.ErrActionNotFound.Error())
		wantStatus(t, do(t, f.router, http.MethodGet, "/v1/actions", f.bookkeeper, nil), http.StatusForbidden)
		if got := listActions(t, f.router, f.other, "/v1/actions"); len(got) != 0 {
			t.Errorf("another org sees %d actions", len(got))
		}
		rec := do(t, f.router, http.MethodGet, "/v1/sites/"+f.siteB+"/actions?state=overdue", f.owner, nil)
		if rec.Code != http.StatusOK || rec.Body.String() != `{"actions":[]}` {
			t.Errorf("empty list: %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("closed late", func(t *testing.T) {
		co := uuid.NewString()
		wantStatus(t, do(t, f.router, http.MethodPut, closeoutPath(f.siteA, old.ID, co), f.supervisor, closeoutBody(now, f.fixPhoto)), http.StatusCreated)
		got := listActions(t, f.router, f.owner, site+"?state=awaiting_verification")
		if len(got) != 1 || got[0].ID != old.ID || got[0].AwaitingSince == nil {
			t.Fatalf("awaiting %+v", got)
		}
		rec := do(t, f.router, http.MethodPut, eventPath(f.siteA, old.ID, uuid.NewString()), f.owner, eventBody(actions.DoVerify, now, map[string]any{"closeout_id": co}))
		wantStatus(t, rec, http.StatusCreated)
		if a := decodeAction(t, rec); a.State != actions.StateClosedLate || !a.Closeouts[0].Late {
			t.Errorf("state %s, late %v; want closed_late", a.State, a.Closeouts[0].Late)
		}
	})
}

func TestIncidentAction(t *testing.T) {
	f := newActionFixture(t, "incaction")
	clk := newSASTClock(t)
	now := clk.Now()
	inc := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, inc, 1), f.supervisor, versionBody(clk.Earlier(time.Hour), "near_miss")), http.StatusCreated)
	body := map[string]any{
		"incident": map[string]any{"incident_id": inc, "version": 1}, "severity": "medium", "title": "Barricade the edge",
		"owner_id": f.supervisorID, "client_raised_at": rfc(clk.Earlier(time.Minute)),
	}
	a := f.raise(t, f.supervisor, body)
	if a.Source != actions.SourceIncident || *a.IncidentVersion != 1 || *a.IncidentLatestVersion != 1 || a.DueOn != localDay(clk.Earlier(time.Minute), 7) {
		t.Errorf("incident action %+v", a.ActionHeader)
	}
	wantError(t, do(t, f.router, http.MethodPut, actionPath(f.siteA, uuid.NewString()), f.supervisor, with(body, "incident",
		map[string]any{"incident_id": inc, "version": 2})), http.StatusBadRequest, actions.ErrIncidentNotFound.Error())

	amend := with(versionBody(clk.Earlier(time.Hour), "near_miss", "first_aid"), "amend_reason", "Someone was hurt after all")
	wantStatus(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, inc, 2), f.supervisor, amend), http.StatusCreated)

	rec := do(t, f.router, http.MethodPut, actionPath(f.siteA, uuid.NewString()), f.supervisor, body)
	var stale struct {
		Error         string `json:"error"`
		LatestVersion int    `json:"latest_version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &stale); err != nil || rec.Code != http.StatusConflict ||
		stale.Error != actions.ErrNotLatestVersion.Error() || stale.LatestVersion != 2 {
		t.Errorf("raise against v1 of a v2 incident: %d %s", rec.Code, rec.Body.String())
	}
	got := decodeAction(t, do(t, f.router, http.MethodGet, actionPath(f.siteA, a.ID), f.owner, nil))
	if *got.IncidentVersion != 1 || *got.IncidentLatestVersion != 2 || got.State != actions.StateOpen {
		t.Errorf("after the amendment: %+v; want raised against 1, incident at 2, untouched", got.ActionHeader)
	}

	t.Run("a close-out can't use the incident's own photo", func(t *testing.T) {
		withPhoto := uuid.NewString()
		v := with(versionBody(clk.Earlier(time.Hour), "near_miss"), "evidence_ids", []string{f.fixPhoto2})
		wantStatus(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, withPhoto, 1), f.supervisor, v), http.StatusCreated)
		b := with(body, "incident", map[string]any{"incident_id": withPhoto, "version": 1})
		act := f.raise(t, f.supervisor, b)
		wantProblem(t, do(t, f.router, http.MethodPut, closeoutPath(f.siteA, act.ID, uuid.NewString()), f.supervisor, closeoutBody(now, f.fixPhoto2)),
			http.StatusBadRequest, actions.ErrEvidenceIsSource.Error(), f.fixPhoto2)
	})
}

// D7 through the API: an old action covers a correction's finding of the
// same or lower severity; a higher one is unactioned and flags the action.
func TestCorrectedFindingActions(t *testing.T) {
	f := newActionFixture(t, "corrected")
	clk := newSASTClock(t)
	now := clk.Now()
	a := f.raise(t, f.supervisor, f.raiseBody(f.supervisorID, clk.Earlier(time.Minute)))

	correction := completeFailing(t, f.answerFixture, f.inspection, map[string]string{f.item: "critical"}, f.failPhoto)
	findings := map[string]checklists.Finding{}
	for _, fi := range listFindings(t, f.router, f.owner, f.siteA, "") {
		findings[fi.InspectionID] = fi
	}
	orig, corr := findings[f.inspection], findings[correction]
	if orig.CorrectedBy == nil || *orig.CorrectedBy != correction || orig.Coverage != checklists.CoverageSuperseded {
		t.Errorf("original finding %+v, want superseded by %s", orig, correction)
	}
	if corr.Coverage != checklists.CoverageUnactioned || len(corr.CoveredBy) != 0 {
		t.Errorf("correction's finding %+v, want unactioned (its severity went up)", corr)
	}
	got := decodeAction(t, do(t, f.router, http.MethodGet, actionPath(f.siteA, a.ID), f.owner, nil))
	if got.SourceCorrectedBy == nil || *got.SourceCorrectedBy != correction || got.Review == nil || *got.Review != actions.ReviewSeverityRaised ||
		got.State != actions.StateOpen {
		t.Errorf("old action %+v, want flagged severity_raised and left open", got.ActionHeader)
	}
	wantError(t, do(t, f.router, http.MethodPut, actionPath(f.siteA, uuid.NewString()), f.supervisor, f.raiseBody(f.supervisorID, now)),
		http.StatusConflict, actions.ErrFindingSuperseded.Error())

	b := f.raiseBody(f.supervisorID, clk.Earlier(time.Minute))
	b["finding"] = map[string]any{"inspection_id": correction, "item_key": f.item}
	onCorrection := f.raise(t, f.supervisor, b)
	if onCorrection.Severity != "critical" || onCorrection.DueOn != localDay(clk.Earlier(time.Minute), 0) {
		t.Errorf("action on the correction: severity %s, due %s; want critical, today", onCorrection.Severity, onCorrection.DueOn)
	}
	if got := listFindings(t, f.router, f.owner, f.siteA, "?coverage=unactioned"); len(got) != 0 {
		t.Errorf("unactioned findings %+v, want none", got)
	}

	// A second correction, lower again: covered by the critical action.
	second := completeFailing(t, f.answerFixture, correction, map[string]string{f.item: "medium"}, f.failPhoto)
	for _, fi := range listFindings(t, f.router, f.owner, f.siteA, "") {
		if fi.InspectionID == second && (fi.Coverage != checklists.CoverageActioned || !slices.Contains(fi.CoveredBy, onCorrection.ID)) {
			t.Errorf("second correction's finding %+v, want actioned by %s", fi, onCorrection.ID)
		}
	}
}

func TestWaivers(t *testing.T) {
	f := newActionFixture(t, "waivers")
	clk := newSASTClock(t)
	low := completeFailing(t, f.answerFixture, "", map[string]string{f.item: "low"}, f.failPhoto)
	id := uuid.NewString()
	body := map[string]any{"inspection_id": low, "item_key": f.item, "reason": "Paperwork only", "client_waived_at": rfc(clk.Earlier(time.Minute))}

	wantError(t, do(t, f.router, http.MethodPut, waiverPath(f.siteA, id), f.supervisor, body), http.StatusForbidden, actions.ErrCantWaive.Error())
	rec := do(t, f.router, http.MethodPut, waiverPath(f.siteA, id), f.owner, body)
	wantStatus(t, rec, http.StatusCreated)
	created := rec.Body.String()
	rec = do(t, f.router, http.MethodPut, waiverPath(f.siteA, id), f.owner, body)
	if rec.Code != http.StatusOK || rec.Body.String() != created {
		t.Errorf("repeat: %d %s", rec.Code, rec.Body.String())
	}
	wantError(t, do(t, f.router, http.MethodPut, waiverPath(f.siteA, id), f.owner, with(body, "reason", "Other")), http.StatusConflict,
		actions.ErrWaiverIDReused.Error())
	wantError(t, do(t, f.router, http.MethodPut, waiverPath(f.siteA, uuid.NewString()), f.owner, body), http.StatusConflict, actions.ErrAlreadyWaived.Error())
	wantError(t, do(t, f.router, http.MethodPut, waiverPath(f.siteA, uuid.NewString()), f.owner, with(body, "inspection_id", f.inspection)),
		http.StatusBadRequest, actions.ErrNotWaivable.Error())

	coverageOf := func(insp string) string {
		for _, fi := range listFindings(t, f.router, f.owner, f.siteA, "") {
			if fi.InspectionID == insp {
				return fi.Coverage
			}
		}
		return ""
	}
	if c := coverageOf(low); c != checklists.CoverageWaived {
		t.Errorf("coverage %s, want waived", c)
	}

	retract := waiverPath(f.siteA, id) + "/retract"
	wantError(t, do(t, f.router, http.MethodPost, retract, f.supervisor, map[string]any{"reason": "Wrong"}), http.StatusForbidden, actions.ErrCantWaive.Error())
	wantError(t, do(t, f.router, http.MethodPost, retract, f.owner, map[string]any{"reason": ""}), http.StatusBadRequest, actions.ErrBadReason.Error())
	wantStatus(t, do(t, f.router, http.MethodPost, retract, f.owner, map[string]any{"reason": "It needs a fix"}), http.StatusCreated)
	wantStatus(t, do(t, f.router, http.MethodPost, retract, f.owner, map[string]any{"reason": "It needs a fix"}), http.StatusOK)
	wantError(t, do(t, f.router, http.MethodPost, retract, f.owner, map[string]any{"reason": "Other"}), http.StatusConflict,
		actions.ErrWaiverRetractionDiffers.Error())
	wantError(t, do(t, f.router, http.MethodPost, waiverPath(f.siteA, uuid.NewString())+"/retract", f.owner, map[string]any{"reason": "x"}),
		http.StatusNotFound, actions.ErrWaiverNotFound.Error())
	if c := coverageOf(low); c != checklists.CoverageUnactioned {
		t.Errorf("coverage %s after retracting, want unactioned", c)
	}
	wantError(t, do(t, f.router, http.MethodGet, "/v1/sites/"+f.siteA+"/findings?coverage=done", f.owner, nil), http.StatusBadRequest,
		checklists.ErrBadCoverage.Error())
}

// Two close-outs at once: the second waits for the action's lock, then finds
// one awaiting verification.
func TestCloseoutsQueue(t *testing.T) {
	f := newActionFixture(t, "cqueue")
	clk := newSASTClock(t)
	now := clk.Now()
	a := f.raise(t, f.supervisor, f.raiseBody(f.supervisorID, clk.Earlier(time.Minute)))

	first := beginTenant(t, f.ownerMe.OrgID)
	defer first.Rollback(context.Background())
	closeOutBySQL(t, first, f, a.ID, f.supervisorID, f.fixPhoto)

	second := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		second <- do(t, f.router, http.MethodPut, closeoutPath(f.siteA, a.ID, uuid.NewString()), f.supervisor, closeoutBody(now, f.fixPhoto2))
	}()
	waitForLock(t, "FOR NO KEY UPDATE")
	if err := first.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	wantError(t, <-second, http.StatusConflict, actions.ErrAlreadyAwaiting.Error())
}

// A close-out by the owner while a reassignment is open: it waits, then the
// supervisor is no longer the owner.
func TestCloseoutAfterReassignment(t *testing.T) {
	f := newActionFixture(t, "creassign")
	clk := newSASTClock(t)
	now := clk.Now()
	a := f.raise(t, f.owner, f.raiseBody(f.supervisorID, clk.Earlier(time.Minute)))

	reassigning := beginTenant(t, f.ownerMe.OrgID)
	defer reassigning.Rollback(context.Background())
	if _, err := reassigning.Exec(context.Background(), `
		INSERT INTO corrective_action_events (id, org_id, site_id, action_id, seq, kind, actor_id, client_at, status, owner_id, due_on)
		SELECT $1, org_id, site_id, id, 2, 'reassigned', $2, now(), 'open', $2, due_on FROM corrective_actions WHERE id = $3`,
		uuid.NewString(), f.ownerID, a.ID); err != nil {
		t.Fatal(err)
	}
	closed := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		closed <- do(t, f.router, http.MethodPut, closeoutPath(f.siteA, a.ID, uuid.NewString()), f.supervisor, closeoutBody(now, f.fixPhoto))
	}()
	waitForLock(t, "FOR NO KEY UPDATE")
	if err := reassigning.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	wantError(t, <-closed, http.StatusForbidden, actions.ErrCantCloseOut.Error())
}

// A close-out while a correction of the finding's inspection completes: they
// share no lock, so the close-out goes through at once, and afterwards the
// action shows the correction.
func TestCloseoutWhileCorrectionCompletes(t *testing.T) {
	f := newActionFixture(t, "ccorrect")
	clk := newSASTClock(t)
	now := clk.Now()
	a := f.raise(t, f.supervisor, f.raiseBody(f.supervisorID, clk.Earlier(time.Minute)))
	corr := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, inspectionPath(f.siteA, corr), f.supervisor, with(startBody(f.tmpl), "corrects_id", f.inspection)),
		http.StatusCreated)
	answerAllBySQL(t, f.ownerMe.OrgID, corr, len(f.tmpl.Items))

	completing := beginTenant(t, f.ownerMe.OrgID)
	defer completing.Rollback(context.Background())
	if _, err := completing.Exec(context.Background(),
		`UPDATE inspections SET status = 'completed', completed_at = now(), client_completed_at = now() WHERE id = $1`, corr); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, do(t, f.router, http.MethodPut, closeoutPath(f.siteA, a.ID, uuid.NewString()), f.supervisor, closeoutBody(now, f.fixPhoto)),
		http.StatusCreated)
	if err := completing.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := decodeAction(t, do(t, f.router, http.MethodGet, actionPath(f.siteA, a.ID), f.owner, nil))
	if got.State != actions.StateAwaiting || got.SourceCorrectedBy == nil || *got.SourceCorrectedBy != corr || got.Review == nil ||
		*got.Review != actions.ReviewItemNotFailed {
		t.Errorf("action %+v; want awaiting, corrected by %s, review item_not_failed", got.ActionHeader, corr)
	}
}

// The lost race on a raise, made deterministic: an identical raise is open
// but uncommitted; this request waits on the primary key, and when the
// other commits it answers as a repeat.
func TestRaiseLosesTheRace(t *testing.T) {
	f := newActionFixture(t, "raiserace")
	clk := newSASTClock(t)
	raisedAt := clk.Earlier(time.Minute)
	id := uuid.NewString()
	due := localDay(raisedAt, 1)

	winner := beginTenant(t, f.ownerMe.OrgID)
	defer winner.Rollback(context.Background())
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO corrective_actions (id, org_id, site_id, inspection_id, item_key, severity, title, raised_by, client_raised_at,
		                                  time_zone, owner_id, due_on)
		  VALUES ($1, $2, $3, $4, $5, 'high', 'Fit the missing guardrail', $6, $7, 'Africa/Johannesburg', $6, $8)`,
			[]any{id, f.ownerMe.OrgID, f.siteA, f.inspection, f.item, f.supervisorID, raisedAt, due}},
		{`INSERT INTO corrective_action_events (id, org_id, site_id, action_id, seq, kind, actor_id, client_at, status, owner_id, due_on)
		  VALUES ($1, $2, $3, $1, 1, 'raised', $4, $5, 'open', $4, $6)`,
			[]any{id, f.ownerMe.OrgID, f.siteA, f.supervisorID, raisedAt, due}},
	} {
		if _, err := winner.Exec(context.Background(), q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	loser := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		loser <- do(t, f.router, http.MethodPut, actionPath(f.siteA, id), f.supervisor, f.raiseBody(f.supervisorID, raisedAt))
	}()
	waitForLock(t, "INSERT INTO corrective_actions")
	if err := winner.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, <-loser, http.StatusOK)
}

// A raise whose checks passed while an amendment was still open: the
// database makes it wait, then refuses it, and the request answers as if it
// had come after the amendment (409 with latest_version), not 500.
func TestRaiseRacesAmendment(t *testing.T) {
	f := newActionFixture(t, "raceamend")
	clk := newSASTClock(t)
	inc := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, inc, 1), f.supervisor, versionBody(clk.Earlier(time.Hour), "near_miss")), http.StatusCreated)

	amending := beginTenant(t, f.ownerMe.OrgID)
	defer amending.Rollback(context.Background())
	if _, err := amending.Exec(context.Background(), `
		INSERT INTO incident_versions (org_id, site_id, incident_id, version, taxonomy_codes, occurred_at, submitted_by,
		                               client_submitted_at, amend_reason, people_count, evidence_count)
		VALUES ($1, $2, $3, 2, ARRAY['near_miss'], now() - interval '1 hour', $4, now(), 'Reclassified', 0, 0)`,
		f.ownerMe.OrgID, f.siteA, inc, f.supervisorID); err != nil {
		t.Fatal(err)
	}
	raised := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		raised <- do(t, f.router, http.MethodPut, actionPath(f.siteA, uuid.NewString()), f.supervisor, map[string]any{
			"incident": map[string]any{"incident_id": inc, "version": 1}, "severity": "medium", "title": "Barricade",
			"owner_id": f.supervisorID, "client_raised_at": rfc(clk.Earlier(time.Minute)),
		})
	}()
	waitForLock(t, "INSERT INTO corrective_actions")
	if err := amending.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec := <-raised
	var body struct {
		Error         string `json:"error"`
		LatestVersion int    `json:"latest_version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusConflict ||
		body.Error != actions.ErrNotLatestVersion.Error() || body.LatestVersion != 2 {
		t.Errorf("got %d %s, want 409 not latest with latest_version 2", rec.Code, rec.Body.String())
	}
}

// Likewise a raise on a finding whose correction completes in between.
func TestRaiseRacesCorrection(t *testing.T) {
	f := newActionFixture(t, "racecorr")
	clk := newSASTClock(t)
	corr := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, inspectionPath(f.siteA, corr), f.supervisor, with(startBody(f.tmpl), "corrects_id", f.inspection)),
		http.StatusCreated)
	answerAllBySQL(t, f.ownerMe.OrgID, corr, len(f.tmpl.Items))

	completing := beginTenant(t, f.ownerMe.OrgID)
	defer completing.Rollback(context.Background())
	if _, err := completing.Exec(context.Background(),
		`UPDATE inspections SET status = 'completed', completed_at = now(), client_completed_at = now() WHERE id = $1`, corr); err != nil {
		t.Fatal(err)
	}
	raised := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		raised <- do(t, f.router, http.MethodPut, actionPath(f.siteA, uuid.NewString()), f.supervisor, f.raiseBody(f.supervisorID, clk.Earlier(time.Minute)))
	}()
	waitForLock(t, "INSERT INTO corrective_actions")
	if err := completing.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	wantError(t, <-raised, http.StatusConflict, actions.ErrFindingSuperseded.Error())
}

// A supervisor taken off a site loses its actions: they can't record events
// on them and no longer see them across sites. One they owned stays
// theirs until a lead reassigns it, which works.
func TestRemovedSupervisor(t *testing.T) {
	f := newActionFixture(t, "removed")
	clk := newSASTClock(t)
	now := clk.Now()
	a := f.raise(t, f.supervisor, f.raiseBody(f.supervisorID, clk.Earlier(time.Minute)))
	if got := actionIDs(listActions(t, f.router, f.supervisor, "/v1/actions")); !slices.Contains(got, a.ID) {
		t.Fatalf("before removal the supervisor sees %v, want %s", got, a.ID)
	}

	wantStatus(t, do(t, f.router, http.MethodDelete, "/v1/sites/"+f.siteA+"/members/"+f.supervisorID, f.owner, nil), http.StatusNoContent)

	t.Run("no events, no close-outs", func(t *testing.T) {
		wantError(t, do(t, f.router, http.MethodPut, eventPath(f.siteA, a.ID, uuid.NewString()), f.supervisor,
			eventBody(actions.DoMoveDue, now, map[string]any{"due_on": localDay(now, 0)})), http.StatusNotFound, "site not found")
		wantError(t, do(t, f.router, http.MethodPut, closeoutPath(f.siteA, a.ID, uuid.NewString()), f.supervisor, closeoutBody(now, f.fixPhoto)),
			http.StatusNotFound, "site not found")
		wantError(t, do(t, f.router, http.MethodGet, actionPath(f.siteA, a.ID), f.supervisor, nil), http.StatusNotFound, "site not found")
	})

	t.Run("gone from the cross-site register", func(t *testing.T) {
		if got := listActions(t, f.router, f.supervisor, "/v1/actions"); len(got) != 0 {
			t.Errorf("the removed supervisor sees %v, want none", actionIDs(got))
		}
	})

	t.Run("a lead reassigns it", func(t *testing.T) {
		if got := decodeAction(t, do(t, f.router, http.MethodGet, actionPath(f.siteA, a.ID), f.owner, nil)); got.OwnerID != f.supervisorID {
			t.Fatalf("owner %s, want still the supervisor until reassigned", got.OwnerID)
		}
		// Not back to the removed supervisor: they no longer see the site.
		wantError(t, do(t, f.router, http.MethodPut, eventPath(f.siteA, a.ID, uuid.NewString()), f.owner,
			eventBody(actions.DoReassign, now, map[string]any{"owner_id": f.supervisorID, "responsible_party": "x"})),
			http.StatusBadRequest, actions.ErrOwnerNotEligible.Error())
		rec := do(t, f.router, http.MethodPut, eventPath(f.siteA, a.ID, uuid.NewString()), f.owner,
			eventBody(actions.DoReassign, now, map[string]any{"owner_id": f.ownerID}))
		wantStatus(t, rec, http.StatusCreated)
		if got := decodeAction(t, rec); got.OwnerID != f.ownerID {
			t.Errorf("owner %s after reassigning, want the owner", got.OwnerID)
		}
	})
}

// The Go mirrors of the database's rules agree with it: the default due days
// (shown in the catalogue and the API reference) and the severity order
// (D7 compares severities by it).
func TestActionRulesMatchDatabase(t *testing.T) {
	for i, l := range checklists.Severities {
		var rank, days int
		if err := testPool.QueryRow(context.Background(), `SELECT severity_rank($1), action_default_due_days($1)`, l.Key).Scan(&rank, &days); err != nil {
			t.Fatal(err)
		}
		if rank != i+1 {
			t.Errorf("severity_rank(%s) = %d, want %d (its place in checklists.Severities)", l.Key, rank, i+1)
		}
		if days != actions.DefaultDueDays[l.Key] {
			t.Errorf("action_default_due_days(%s) = %d, actions.DefaultDueDays %d", l.Key, days, actions.DefaultDueDays[l.Key])
		}
	}
}

// beginTenant opens a transaction as the app in org, for holding locks.
func beginTenant(t *testing.T, orgID string) pgx.Tx {
	t.Helper()
	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SetTenant(ctx, tx, database.Tenant{OrgID: orgID}); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	return tx
}

// closeOutBySQL records a close-out of action (its second event) in tx.
func closeOutBySQL(t *testing.T, tx pgx.Tx, f actionFixture, action, by, photo string) {
	t.Helper()
	ctx := context.Background()
	co := uuid.NewString()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO corrective_action_closeouts (id, org_id, site_id, action_id, submitted_by, client_submitted_at, evidence_count)
		  VALUES ($1, $2, $3, $4, $5, now(), 1)`, []any{co, f.ownerMe.OrgID, f.siteA, action, by}},
		{`INSERT INTO corrective_action_closeout_evidence (org_id, site_id, closeout_id, evidence_id) VALUES ($1, $2, $3, $4)`,
			[]any{f.ownerMe.OrgID, f.siteA, co, photo}},
		{`INSERT INTO corrective_action_events (id, org_id, site_id, action_id, seq, kind, actor_id, client_at, status, owner_id, due_on, closeout_id)
		  SELECT $1, org_id, site_id, id, 2, 'closeout_submitted', $2, now(), 'awaiting_verification', owner_id, due_on, $1
		  FROM corrective_actions WHERE id = $3`, []any{co, by, action}},
	} {
		if _, err := tx.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
}
