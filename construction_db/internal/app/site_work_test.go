package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"construction_db/internal/core/deadlines"
	"construction_db/internal/core/exports"
	"construction_db/internal/core/files"
	"construction_db/internal/core/notifications"
	"construction_db/internal/core/reports"
	"construction_db/internal/editions/construction"
	"construction_db/internal/editions/construction/reg4"
	"construction_db/internal/platform/database"
)

const (
	reg4Key = "cr2014_reg4_notification"
	reg3Key = "cr2014_reg3_permit_application"
)

func workPath(site, id string) string { return "/v1/construction/sites/" + site + "/site-work/" + id }
func workVersionPath(site, id string, n int) string {
	return fmt.Sprintf("%s/versions/%d", workPath(site, id), n)
}

func workBody(c sastClock, starts string, facts ...string) map[string]any {
	if facts == nil {
		facts = []string{}
	}
	return map[string]any{"description": "Bulk earthworks, basement", "starts_on": starts, "facts": facts,
		"client_submitted_at": rfc(c.Earlier(10 * time.Minute))}
}

func amendWork(body map[string]any, reason string, kv ...any) map[string]any {
	out := with(body, "amend_reason", reason)
	for i := 0; i+1 < len(kv); i += 2 {
		out = with(out, kv[i].(string), kv[i+1])
	}
	return out
}

func decodeWork(t *testing.T, rec *httptest.ResponseRecorder) reg4.SiteWork {
	t.Helper()
	var w reg4.SiteWork
	if err := json.Unmarshal(rec.Body.Bytes(), &w); err != nil {
		t.Fatalf("decode site work: %v (body: %s)", err, rec.Body.String())
	}
	return w
}

func liveWork(t *testing.T, w reg4.SiteWork, rule string) deadlines.Deadline {
	t.Helper()
	var found []deadlines.Deadline
	for _, d := range w.Deadlines {
		if d.RuleKey == rule && d.ClosedAs == nil {
			found = append(found, d)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d live %s rows, want 1: %+v", len(found), rule, w.Deadlines)
	}
	return found[0]
}

func liveWorkRules(w reg4.SiteWork) []string {
	out := []string{}
	for _, d := range w.Deadlines {
		if d.ClosedAs == nil {
			out = append(out, d.RuleKey)
		}
	}
	slices.Sort(out)
	return out
}

func submitWork(t *testing.T, f evidenceFixture, token, id string, n int, body map[string]any, want int) reg4.SiteWork {
	t.Helper()
	rec := do(t, f.router, http.MethodPut, workVersionPath(f.siteA, id, n), token, body)
	wantStatus(t, rec, want)
	return decodeWork(t, rec)
}

func TestSiteWorkSubmitAndGet(t *testing.T) {
	f := newEvidenceFixture(t, "sitework")
	c := newSASTClock(t)
	id := uuid.NewString()
	v1 := workBody(c, c.Day(20), "excavation")

	t.Run("excavation starts the reg 4 notice, 7 clear days before", func(t *testing.T) {
		w := submitWork(t, f, f.supervisor, id, 1, v1, http.StatusCreated)
		d := liveWork(t, w, reg4Key)
		if w.LatestVersion != 1 || w.Version.Status != "planned" || len(w.Version.Facts) != 1 || w.Version.Facts[0].Label != "Includes excavation work" ||
			w.Version.LateWhenRecorded || d.SubjectType != deadlines.SubjectSiteWork || d.SiteWorkID == nil || *d.SiteWorkID != id ||
			d.IncidentID != nil || d.Unit != "clear_days" || d.DueOn == nil || *d.DueOn != c.Day(12) || d.State != deadlines.StateOpen ||
			d.Caveat == nil || !slices.Equal(d.ProofDocumentTypes, []string{"notification_of_construction_work"}) || !slices.Equal(w.Changes.Added, []string{d.ID}) {
			t.Errorf("site work %+v; deadline %+v", w, d)
		}
	})
	t.Run("an identical repeat is 200; other details are 409", func(t *testing.T) {
		wantStatus(t, do(t, f.router, http.MethodPut, workVersionPath(f.siteA, id, 1), f.supervisor, v1), http.StatusOK)
		wantError(t, do(t, f.router, http.MethodPut, workVersionPath(f.siteA, id, 1), f.supervisor, with(v1, "description", "Other")),
			http.StatusConflict, "site work id already used with different details")
		wantError(t, do(t, f.router, http.MethodPut, workVersionPath(f.siteB, id, 1), f.owner, v1),
			http.StatusConflict, "site work id already used with different details")
	})
	t.Run("versions come one at a time", func(t *testing.T) {
		rec := do(t, f.router, http.MethodPut, workVersionPath(f.siteA, id, 3), f.owner, amendWork(v1, "Skipped"))
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"latest_version":1`) {
			t.Errorf("v3: %d %s", rec.Code, rec.Body.String())
		}
		wantError(t, do(t, f.router, http.MethodPut, workVersionPath(f.siteA, uuid.NewString(), 2), f.owner, amendWork(v1, "x")),
			http.StatusNotFound, "site work not found")
	})
	t.Run("validation", func(t *testing.T) {
		for _, tc := range []struct {
			body map[string]any
			n    int
			msg  string
		}{
			{with(v1, "description", ""), 1, "description must be 1-500 characters"},
			{with(v1, "starts_on", "next week"), 1, "starts_on must be a date (YYYY-MM-DD)"},
			{with(v1, "ends_on", c.Day(19)), 1, "ends_on must be a date (YYYY-MM-DD), not before starts_on"},
			{with(v1, "facts", []string{"excavation", "excavation"}), 1, "facts must not repeat"},
			{with(v1, "status", "started"), 1, "status must be planned or cancelled"},
			{with(v1, "status", "cancelled"), 1, "version 1 is planned; cancel in a later version"},
			{with(v1, "client_submitted_at", nil), 1, "client_submitted_at is required"},
			{with(v1, "amend_reason", "x"), 1, "version 1 has no amend_reason"},
			{with(v1, "starts_on", "2031-03-01"), 1, "starts_on is outside the years the holiday calendar covers"},
		} {
			wantError(t, do(t, f.router, http.MethodPut, workVersionPath(f.siteA, uuid.NewString(), tc.n), f.owner, tc.body), http.StatusBadRequest, tc.msg)
		}
		wantError(t, do(t, f.router, http.MethodPut, workVersionPath(f.siteA, id, 2), f.owner, v1), http.StatusBadRequest,
			"amend_reason is required for an amendment (1-500 characters)")
		rec := do(t, f.router, http.MethodPut, workVersionPath(f.siteA, uuid.NewString(), 1), f.owner, with(v1, "facts", []string{"tunneling"}))
		if rec.Code != http.StatusBadRequest || rec.Body.String() != `{"error":"facts must be site work facts of the site's regime","facts":["tunneling"]}` {
			t.Errorf("unknown fact: %d %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("read, scoped", func(t *testing.T) {
		rec := do(t, f.router, http.MethodGet, workPath(f.siteA, id), f.owner, nil)
		wantStatus(t, rec, http.StatusOK)
		if w := decodeWork(t, rec); w.ID != id || len(w.Deadlines) != 1 {
			t.Errorf("get %+v", w)
		}
		rec = do(t, f.router, http.MethodGet, "/v1/construction/sites/"+f.siteA+"/site-work", f.supervisor, nil)
		wantStatus(t, rec, http.StatusOK)
		var list struct {
			SiteWork []reg4.Summary `json:"site_work"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.SiteWork) != 1 || list.SiteWork[0].OpenDeadlines != 1 {
			t.Errorf("list %s", rec.Body.String())
		}
		wantError(t, do(t, f.router, http.MethodGet, workPath(f.siteA, id)+"?version=2", f.owner, nil), http.StatusNotFound, "version not found")
		wantError(t, do(t, f.router, http.MethodGet, workPath(f.siteB, id), f.supervisor, nil), http.StatusNotFound, "site not found")
		wantError(t, do(t, f.router, http.MethodGet, workPath(f.otherSite, id), f.owner, nil), http.StatusNotFound, "site not found")
		wantStatus(t, do(t, f.router, http.MethodGet, workPath(f.siteA, id), f.bookkeeper, nil), http.StatusForbidden)
		rec = do(t, f.router, http.MethodGet, "/v1/construction/sites/"+f.siteA+"/site-work-facts", f.supervisor, nil)
		wantStatus(t, rec, http.StatusOK)
		var cat reg4.Catalogue
		if err := json.Unmarshal(rec.Body.Bytes(), &cat); err != nil || len(cat.Facts) != 8 || len(cat.Rules) != 2 {
			t.Errorf("catalogue %s", rec.Body.String())
		}
	})
	t.Run("the site deadline list filters by subject", func(t *testing.T) {
		rec := do(t, f.router, http.MethodGet, "/v1/sites/"+f.siteA+"/deadlines?subject_type=site_work", f.owner, nil)
		wantStatus(t, rec, http.StatusOK)
		if !strings.Contains(rec.Body.String(), `"subject_type":"site_work"`) {
			t.Errorf("list %s", rec.Body.String())
		}
		wantError(t, do(t, f.router, http.MethodGet, "/v1/sites/"+f.siteA+"/deadlines?subject_type=job", f.owner, nil),
			http.StatusBadRequest, "subject_type must be incident or site_work")
	})
}

// The plan's steps on site work, and who may weaken (REG4 D5).
func TestSiteWorkDeadlinePlan(t *testing.T) {
	f := newEvidenceFixture(t, "siteworkplan")
	c := newSASTClock(t)
	id := uuid.NewString()
	v1 := workBody(c, c.Day(20), "excavation")
	w := submitWork(t, f, f.supervisor, id, 1, v1, http.StatusCreated)
	first := liveWork(t, w, reg4Key)

	t.Run("an earlier start supersedes, open to supervisors", func(t *testing.T) {
		w = submitWork(t, f, f.supervisor, id, 2, amendWork(v1, "Mobilising sooner", "starts_on", c.Day(15)), http.StatusCreated)
		d := liveWork(t, w, reg4Key)
		if *d.DueOn != c.Day(7) || d.DutyID != first.DutyID || !slices.Equal(w.Changes.Superseded, []string{first.ID}) {
			t.Errorf("deadline %+v; changes %+v", d, w.Changes)
		}
	})
	t.Run("a later start takes a lead", func(t *testing.T) {
		later := amendWork(v1, "Plant delayed", "starts_on", c.Day(30))
		wantError(t, do(t, f.router, http.MethodPut, workVersionPath(f.siteA, id, 3), f.supervisor, later),
			http.StatusForbidden, "only an owner or a SHE officer can withdraw a notification deadline or move it later")
		w = submitWork(t, f, f.owner, id, 3, later, http.StatusCreated)
		if d := liveWork(t, w, reg4Key); *d.DueOn != c.Day(22) {
			t.Errorf("due %s, want %s", *d.DueOn, c.Day(22))
		}
		closed := w.Deadlines[1]
		if closed.ClosedBy == nil || *closed.ClosedBy != f.ownerMe.UserID || closed.ClosedReason == nil || *closed.ClosedReason != "Plant delayed" {
			t.Errorf("closed row %+v", closed)
		}
	})
	t.Run("a permit fact swaps the notice for the permit (reg 4(1)), with a lead", func(t *testing.T) {
		permit := amendWork(v1, "Tender is CIDB 8", "starts_on", c.Day(60), "facts", []string{"excavation", "cidb_grade_7_8_or_9"})
		wantStatus(t, do(t, f.router, http.MethodPut, workVersionPath(f.siteA, id, 4), f.supervisor, permit), http.StatusForbidden)
		w = submitWork(t, f, f.owner, id, 4, permit, http.StatusCreated)
		if got := liveWorkRules(w); !slices.Equal(got, []string{reg3Key}) || len(w.Changes.Withdrawn) != 1 || len(w.Changes.Added) != 1 {
			t.Errorf("live %v, changes %+v", got, w.Changes)
		}
		if d := liveWork(t, w, reg3Key); *d.DueOn != c.Day(29) {
			t.Errorf("reg 3 due %s, want %s (30 clear days)", *d.DueOn, c.Day(29))
		}
	})
	t.Run("cancelled withdraws everything; reinstated, the duty comes back", func(t *testing.T) {
		cancel := amendWork(v1, "Client cancelled", "starts_on", c.Day(60), "facts", []string{"excavation", "cidb_grade_7_8_or_9"}, "status", "cancelled")
		wantStatus(t, do(t, f.router, http.MethodPut, workVersionPath(f.siteA, id, 5), f.supervisor, cancel), http.StatusForbidden)
		w = submitWork(t, f, f.owner, id, 5, cancel, http.StatusCreated)
		if got := liveWorkRules(w); len(got) != 0 {
			t.Errorf("live after cancelling: %v", got)
		}
		back := amendWork(v1, "Back on", "starts_on", c.Day(60))
		w = submitWork(t, f, f.supervisor, id, 6, back, http.StatusCreated)
		if d := liveWork(t, w, reg4Key); d.DutyID != first.DutyID || d.ComputedInVersion != 6 {
			t.Errorf("reinstated %+v, want duty %s", d, first.DutyID)
		}
	})
}

// A satisfaction of site work: proof from the H&S file, carried over a later
// start, retracted (REG4 D12).
func TestSiteWorkSatisfaction(t *testing.T) {
	f := newEvidenceFixture(t, "siteworksat")
	c := newSASTClock(t)
	id := uuid.NewString()
	v1 := workBody(c, c.Day(20), "work_at_height_fall_risk")
	notice := liveWork(t, submitWork(t, f, f.owner, id, 1, v1, http.StatusCreated), reg4Key)

	doc := func(typeKey string) string {
		file := uploadFile(t, f.router, f.owner, files.PurposeSiteDocument, &f.siteA)
		docID := uuid.NewString()
		wantStatus(t, do(t, f.router, http.MethodPut, safetyFilePath(f.siteA, docID), f.owner,
			map[string]any{"type_key": typeKey, "title": "Doc", "version": versionReq(file, nil)}), http.StatusCreated)
		return docID
	}
	annexure2, risk := doc("notification_of_construction_work"), doc("risk_assessment")
	body := func(docID string, version any) map[string]any {
		return map[string]any{"satisfied_at": rfc(c.Earlier(30 * time.Minute)), "method": "email", "reference": "DEL ref 77",
			"document_id": docID, "document_version": version}
	}
	path := satisfactionPath(f.siteA, notice.ID, uuid.NewString())

	wantError(t, do(t, f.router, http.MethodPut, path, f.supervisor, body(risk, 1)), http.StatusBadRequest, "that document type is not proof of this deadline")
	wantError(t, do(t, f.router, http.MethodPut, path, f.supervisor, body(annexure2, 2)), http.StatusBadRequest, "document version not found in this site's health and safety file")
	wantError(t, do(t, f.router, http.MethodPut, path, f.supervisor, body(annexure2, nil)), http.StatusBadRequest, "document_id and document_version are given together")
	rec := do(t, f.router, http.MethodPut, path, f.supervisor, body(annexure2, 1))
	wantStatus(t, rec, http.StatusCreated)
	if d := decodeDetail(t, rec); d.State != deadlines.StateSatisfied || d.Satisfaction == nil || d.Satisfaction.DocumentID == nil ||
		*d.Satisfaction.DocumentID != annexure2 || *d.Satisfaction.DocumentVersion != 1 {
		t.Errorf("satisfied %+v", d)
	}
	wantStatus(t, do(t, f.router, http.MethodPut, path, f.supervisor, body(annexure2, 1)), http.StatusOK)

	// A later start (a lead's, met or not) keeps the notice met: the satisfaction carries over.
	w := submitWork(t, f, f.owner, id, 2, amendWork(v1, "Scaffold late", "starts_on", c.Day(27)), http.StatusCreated)
	if d := liveWork(t, w, reg4Key); d.State != deadlines.StateSatisfied || d.ID == notice.ID {
		t.Errorf("after the later start: %+v", d)
	}
}

// Two version 2s, made deterministic: the open one holds the record's lock.
func TestSiteWorkVersionLosesTheRace(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFixture(t, "siteworkrace")
	c := newSASTClock(t)
	id := uuid.NewString()
	v1 := workBody(c, c.Day(20), "demolition")
	submitWork(t, f, f.owner, id, 1, v1, http.StatusCreated)
	v2 := amendWork(v1, "Mine")

	winner := holdOpen(t, f)
	if _, err := winner.Exec(ctx, `
		INSERT INTO site_work_versions (org_id, site_id, site_work_id, version, description, starts_on, facts, status,
		                                submitted_by, client_submitted_at, amend_reason)
		VALUES ($1, $2, $3, 2, 'Bulk earthworks, basement', $4, '{demolition}', 'planned', $5, now(), 'Theirs')`,
		f.ownerMe.OrgID, f.siteA, id, c.Day(20), f.ownerMe.UserID); err != nil {
		t.Fatal(err)
	}
	loser := make(chan *httptest.ResponseRecorder, 1)
	go func() { loser <- do(t, f.router, http.MethodPut, workVersionPath(f.siteA, id, 2), f.owner, v2) }()
	waitForLock(t, "FOR NO KEY UPDATE")
	if err := winner.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	wantError(t, <-loser, http.StatusConflict, "this version was already submitted with different details")
}

// A satisfaction in progress (held at its insert) when a supervisor cancels:
// the satisfaction holds the record's lock (reg4.Hooks), so the cancel waits,
// sees the notice met, and withdrawing a met notice needs no lead. Without
// the lock the cancel read an open notice and answered 403.
func TestSiteWorkAmendmentWaitsForSatisfaction(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFixture(t, "siteworkwaits")
	c := newSASTClock(t)
	id := uuid.NewString()
	v1 := workBody(c, c.Day(20), "explosives")
	notice := liveWork(t, submitWork(t, f, f.owner, id, 1, v1, http.StatusCreated), reg4Key)

	hold := holdOpen(t, f)
	if _, err := hold.Exec(ctx, `SELECT 1 FROM deadline_duties WHERE id = $1 FOR UPDATE`, notice.DutyID); err != nil {
		t.Fatal(err)
	}
	call := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		call <- do(t, f.router, http.MethodPut, satisfactionPath(f.siteA, notice.ID, uuid.NewString()), f.supervisor,
			map[string]any{"satisfied_at": rfc(c.Earlier(5 * time.Minute)), "method": "email"})
	}()
	waitForLock(t, "INSERT INTO deadline_satisfactions")
	amend := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		amend <- do(t, f.router, http.MethodPut, workVersionPath(f.siteA, id, 2), f.supervisor, amendWork(v1, "Not blasting", "status", "cancelled"))
	}()
	waitForLock(t, "FOR NO KEY UPDATE")
	if err := hold.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, <-call, http.StatusCreated)
	wantStatus(t, <-amend, http.StatusCreated)
}

// Reminders (REG4 D9): 7 days before a notice is due, to the submitter, the
// leads and the site's supervisors, naming the start.
func TestSiteWorkReminders(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFixture(t, "siteworkremind")
	c := newSASTClock(t)
	submitWork(t, f, f.supervisor, uuid.NewString(), 1, workBody(c, c.Day(15), "excavation"), http.StatusCreated)
	mailer := &recordingMailer{}
	jobs := &notifications.Jobs{Pool: testPool, Registry: testRegistry, Mailer: mailer, OnlyOrgs: []string{f.ownerMe.OrgID}, Clock: c.Frozen()}
	if n, err := jobs.Reminders(ctx, c.Now()); err != nil || n != 2 {
		t.Fatalf("reminders: %d, err %v; want 2 (the supervisor who recorded it, and the owner), before_7d", n, err)
	}
	box := inbox(t, f.router, f.owner)
	site := listSites(t, f.router, f.owner)[0].Name
	title := "Notify the provincial director of the construction work (Annexure 2) (" + site + "; work starts " + c.Long(15) + ")"
	if len(box) != 1 || box[0].Kind != "site_work_deadline" || box[0].Threshold != "before_7d" || box[0].Title != title {
		t.Fatalf("inbox %+v, want %q", box, title)
	}
	if sent, err := jobs.Emails(ctx); err != nil || sent != 2 {
		t.Fatalf("emails: %d, err %v", sent, err)
	}
	if want := title + " is due on " + c.Long(7) + ", in 7 days."; !slices.ContainsFunc(mailer.bodies, func(b string) bool { return strings.HasPrefix(b, want) }) {
		t.Errorf("bodies %q, want one starting %q", mailer.bodies, want)
	}
}

// reminderJobs is the worker's reminders for f's organisation, at c.
func reminderJobs(f evidenceFixture, c sastClock, m *recordingMailer) *notifications.Jobs {
	return &notifications.Jobs{Pool: testPool, Registry: testRegistry, Mailer: m, OnlyOrgs: []string{f.ownerMe.OrgID}, Clock: c.Frozen()}
}

// ownerReminders: the owner's deadline reminders, oldest first, as "threshold@subject:state".
func ownerReminders(t *testing.T, f evidenceFixture) []string {
	t.Helper()
	out := []string{}
	box := inbox(t, f.router, f.owner)
	for i := len(box) - 1; i >= 0; i-- {
		if n := box[i]; n.SubjectType == "deadline" {
			out = append(out, n.Threshold+"@"+n.SubjectID+":"+n.SubjectState)
		}
	}
	return out
}

// R1: reminders belong to the computed deadline. A later start gives the
// new date its own before_7d; the old one's queued emails are cancelled.
func TestSiteWorkRemindersAfterTheStartMoves(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFixture(t, "siteworkmove")
	c := newSASTClock(t)
	id := uuid.NewString()
	v1 := workBody(c, c.Day(15), "excavation")
	first := liveWork(t, submitWork(t, f, f.owner, id, 1, v1, http.StatusCreated), reg4Key)
	mailer := &recordingMailer{}
	jobs := reminderJobs(f, c, mailer)
	if n, err := jobs.Reminders(ctx, c.Now()); err != nil || n != 2 {
		t.Fatalf("first run: %d, err %v; want 2 (the owner and the site's supervisor, before_7d)", n, err)
	}

	moved := liveWork(t, submitWork(t, f, f.owner, id, 2, amendWork(v1, "Plant delayed", "starts_on", c.Day(25)), http.StatusCreated), reg4Key)
	if sent, err := jobs.Emails(ctx); err != nil || sent != 0 {
		t.Errorf("emails about the superseded date: sent %d, err %v; want 0", sent, err)
	}
	if n := countRows(t, `SELECT count(*) FROM notification_emails WHERE org_id = $1 AND status = 'cancelled'`, f.ownerMe.OrgID); n != 2 {
		t.Errorf("%d cancelled emails, want 2", n)
	}
	// Ten days on, the new date is 7 days away: before_7d again, for the new row.
	if n, err := jobs.Reminders(ctx, c.Now().AddDate(0, 0, 10)); err != nil || n != 2 {
		t.Fatalf("after the move: %d, err %v; want 2", n, err)
	}
	want := []string{"before_7d@" + first.ID + ":superseded", "before_7d@" + moved.ID + ":current"}
	if got := ownerReminders(t, f); !slices.Equal(got, want) {
		t.Errorf("inbox %v, want %v", got, want)
	}
}

// A satisfaction stops a deadline's reminders: its queued emails are
// cancelled and its inbox item is superseded.
func TestSiteWorkRemindersStopWhenMet(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFixture(t, "siteworkmet")
	c := newSASTClock(t)
	notice := liveWork(t, submitWork(t, f, f.owner, uuid.NewString(), 1, workBody(c, c.Day(15), "excavation"), http.StatusCreated), reg4Key)
	jobs := reminderJobs(f, c, &recordingMailer{})
	if n, _ := jobs.Reminders(ctx, c.Now()); n != 2 {
		t.Fatalf("first run: %d, want 2", n)
	}
	wantStatus(t, do(t, f.router, http.MethodPut, satisfactionPath(f.siteA, notice.ID, uuid.NewString()), f.owner,
		map[string]any{"satisfied_at": rfc(c.Earlier(time.Minute)), "method": "email"}), http.StatusCreated)
	if sent, _ := jobs.Emails(ctx); sent != 0 {
		t.Errorf("sent %d emails about a met notice", sent)
	}
	if got := ownerReminders(t, f); !slices.Equal(got, []string{"before_7d@" + notice.ID + ":superseded"}) {
		t.Errorf("inbox %v", got)
	}
}

// R1: work cancelled then reinstated is told again.
func TestSiteWorkRemindersAfterReinstating(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFixture(t, "siteworkreinstate")
	c := newSASTClock(t)
	id := uuid.NewString()
	v1 := workBody(c, c.Day(15), "excavation")
	submitWork(t, f, f.owner, id, 1, v1, http.StatusCreated)
	jobs := reminderJobs(f, c, &recordingMailer{})
	if n, _ := jobs.Reminders(ctx, c.Now()); n != 2 {
		t.Fatalf("first run: %d, want 2", n)
	}
	submitWork(t, f, f.owner, id, 2, amendWork(v1, "On hold", "status", "cancelled"), http.StatusCreated)
	if n, _ := jobs.Reminders(ctx, c.Now()); n != 0 {
		t.Errorf("while cancelled: %d, want 0", n)
	}
	back := liveWork(t, submitWork(t, f, f.owner, id, 3, amendWork(v1, "Back on"), http.StatusCreated), reg4Key)
	if n, err := jobs.Reminders(ctx, c.Now()); err != nil || n != 2 {
		t.Fatalf("after reinstating: %d, err %v; want 2", n, err)
	}
	if got := ownerReminders(t, f); len(got) != 2 || got[1] != "before_7d@"+back.ID+":current" || !strings.HasSuffix(got[0], ":superseded") {
		t.Errorf("inbox %v", got)
	}
}

// R1 for incidents: a corrected occurred_at makes a new deadline, told again.
func TestIncidentRemindersAfterACorrection(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFixture(t, "incidentmove")
	c := newSASTClock(t)
	// Medical treatment: the GAR 9(2) investigation, 7 days, due in 2.
	body := versionBody(c.At(-5, "10:00"), "medical_treatment")
	inc := submitNew(t, f, f.owner, body)
	first := live(t, inc, gar92)
	jobs := reminderJobs(f, c, &recordingMailer{})
	if n, err := jobs.Reminders(ctx, c.Now()); err != nil || n != 2 {
		t.Fatalf("first run: %d, err %v; want 2 (the owner and the site's supervisor, before_2d)", n, err)
	}
	// It happened a day earlier: due tomorrow, a new row, before_2d again.
	fixed := with(versionBody(c.At(-6, "10:00"), "medical_treatment"), "amend_reason", "Wrong day")
	rec := do(t, f.router, http.MethodPut, versionPath(f.siteA, inc.ID, 2), f.owner, fixed)
	wantStatus(t, rec, http.StatusCreated)
	second := live(t, decodeIncident(t, rec), gar92)
	if n, err := jobs.Reminders(ctx, c.Now()); err != nil || n != 2 {
		t.Fatalf("after the correction: %d, err %v; want 2", n, err)
	}
	want := []string{"before_2d@" + first.ID + ":superseded", "before_2d@" + second.ID + ":current"}
	if got := ownerReminders(t, f); !slices.Equal(got, want) {
		t.Errorf("inbox %v, want %v", got, want)
	}
}

// Two workers at once never tell a deadline twice: the second waits on the
// first's uncommitted reminder, then inserts nothing.
func TestSiteWorkRemindersTwoWorkers(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFixture(t, "siteworkrace2")
	c := newSASTClock(t)
	submitWork(t, f, f.owner, uuid.NewString(), 1, workBody(c, c.Day(15), "excavation"), http.StatusCreated)
	jobs := reminderJobs(f, c, &recordingMailer{})

	tx := holdOpen(t, f)
	if _, err := tx.Exec(ctx, `
		INSERT INTO notifications (org_id, user_id, kind, subject_type, subject_id, threshold, title)
		SELECT app_org_id(), user_id, kind, subject_type, subject_id, threshold, title
		FROM notifications_due($1, 'za_construction_2014', 'Africa/Johannesburg', ARRAY[30,14,7,0], ARRAY[60,30,7,0], ARRAY[2,0],
		                       ARRAY[0,1,3], 2, '{}', 14, ARRAY[7,2,0])`, c.Now()); err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	go func() {
		n, err := jobs.Reminders(ctx, c.Now())
		if err != nil {
			t.Error(err)
		}
		done <- n
	}()
	waitForLock(t, "INSERT INTO notifications")
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if n := <-done; n != 0 {
		t.Errorf("the second worker inserted %d, want 0", n)
	}
	if n := countRows(t, `SELECT count(*) FROM notifications WHERE org_id = $1 AND subject_type = 'deadline'`, f.ownerMe.OrgID); n != 2 {
		t.Errorf("%d deadline reminders stored, want 2", n)
	}
}

// packWork: the ids in the site_work section of a pack of the period, as of now (after the records).
func packWork(t *testing.T, f evidenceFixture, c sastClock, from, to string) []string {
	t.Helper()
	ctx := context.Background()
	var ids []string
	err := database.WithTenant(ctx, testPool, database.Tenant{OrgID: f.ownerMe.OrgID}, func(tx pgx.Tx) error {
		sections, err := construction.New().ExportsContributor().PackSections(ctx, tx, exports.Query{OrgID: f.ownerMe.OrgID,
			SiteID: f.siteA, AsOf: testClock.Now(), From: from, To: to, TimeZone: "Africa/Johannesburg", IncludesHealth: true})
		if err != nil {
			return err
		}
		for _, s := range sections {
			if s.Key != "site_work" {
				continue
			}
			var recs []struct{ ID string }
			if err := json.Unmarshal(s.Records, &recs); err != nil {
				return err
			}
			for _, r := range recs {
				ids = append(ids, r.ID)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

// R5: a record's span starts at its earliest due date (a permit application
// a month before the work) and, cancelled, ends on the cancellation day.
func TestSiteWorkPackSpans(t *testing.T) {
	f := newEvidenceFixture(t, "siteworkspans")
	c := newSASTClock(t)
	permit := submitWork(t, f, f.owner, uuid.NewString(), 1, workBody(c, c.Day(40), "cidb_grade_7_8_or_9"), http.StatusCreated) // due Day(9)
	dropped := uuid.NewString()
	v1 := workBody(c, c.Day(20), "excavation")
	submitWork(t, f, f.owner, dropped, 1, v1, http.StatusCreated)
	submitWork(t, f, f.owner, dropped, 2, amendWork(v1, "Not going ahead", "status", "cancelled"), http.StatusCreated)

	for _, tc := range []struct {
		name, from, to string
		want           []string
	}{
		{"the permit's month, before the work starts", c.Day(5), c.Day(12), []string{permit.ID}},
		{"open-ended: long after the start", c.Day(41), c.Day(60), []string{permit.ID}},
		{"before anything is due", c.Day(0), c.Day(3), nil},
		{"the cancelled work, up to its cancellation", c.Day(-1), c.Day(25), []string{dropped, permit.ID}},
		{"not after its cancellation", c.Day(30), c.Day(39), []string{permit.ID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := packWork(t, f, c, tc.from, tc.to)
			slices.Sort(got)
			want := slices.Clone(tc.want)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("period %s to %s: %v, want %v", tc.from, tc.to, got, want)
			}
		})
	}
}

// The compliance summary (REG4 D3, D10): its own category; reg 4 overdue is
// red, reg 3 overdue amber with its caveat, a notice due within 7 days amber.
func TestSiteWorkCompliance(t *testing.T) {
	f := newEvidenceFixture(t, "siteworkcompliance")
	c := newSASTClock(t)
	late := submitWork(t, f, f.owner, uuid.NewString(), 1, workBody(c, c.Day(3), "excavation"), http.StatusCreated)
	permit := submitWork(t, f, f.owner, uuid.NewString(), 1, workBody(c, c.Day(20), "cidb_grade_7_8_or_9"), http.StatusCreated)
	soon := submitWork(t, f, f.owner, uuid.NewString(), 1, workBody(c, c.Day(12), "demolition"), http.StatusCreated)

	s := siteSummary(t, f.router, f.owner, f.siteA)
	byWork := map[string]reports.Item{}
	for _, cat := range s.Categories {
		switch cat.Key {
		case reports.CatIncidentDeadlines:
			if len(cat.Items) != 0 {
				t.Errorf("incident deadlines list site work: %+v", cat.Items)
			}
		case reports.CatSiteWorkDeadlines:
			if cat.Status == nil || *cat.Status != reports.Red {
				t.Errorf("site work status %v, want red", cat.Status)
			}
			for _, it := range cat.Items {
				byWork[*it.SiteWorkID] = it
			}
		}
	}
	for _, tc := range []struct {
		id, state, colour string
		note              bool
	}{
		{late.ID, reports.ItemOverdue, reports.Red, false},
		{permit.ID, reports.ItemOverdue, reports.Amber, true},
		{soon.ID, reports.ItemDueSoon, reports.Amber, false},
	} {
		it, ok := byWork[tc.id]
		if !ok || it.State != tc.state || *it.Colour != tc.colour || (it.Note != nil) != tc.note {
			t.Errorf("work %s: %+v; want %s %s note %v", tc.id, it, tc.state, tc.colour, tc.note)
		}
	}
}

// Dates from the SAST clock (DATE_TESTS): a start less than 8 days away is
// overdue from the moment it is recorded (REG4 D4).
func TestSiteWorkDates(t *testing.T) {
	f := newEvidenceFixture(t, "siteworkdates")
	c := newSASTClock(t)
	for _, tc := range []struct {
		starts, dueOn, state string
		late                 bool
	}{
		{c.Day(9), c.Day(1), deadlines.StateOpen, false},
		{c.Day(8), c.Day(0), deadlines.StateOpen, false},
		{c.Day(7), c.Day(-1), deadlines.StateOverdue, true},
		{c.Day(1), c.Day(-7), deadlines.StateOverdue, true},
		{c.Day(0), c.Day(-8), deadlines.StateOverdue, true},
		{c.Day(-1), c.Day(-9), deadlines.StateOverdue, true},
	} {
		t.Run("starts "+tc.starts, func(t *testing.T) {
			w := submitWork(t, f, f.owner, uuid.NewString(), 1, workBody(c, tc.starts, "excavation"), http.StatusCreated)
			d := liveWork(t, w, reg4Key)
			if *d.DueOn != tc.dueOn || d.State != tc.state || d.OverdueWhenComputed != tc.late || w.Version.LateWhenRecorded != tc.late {
				t.Errorf("due %s state %s overdue_when_computed %v late %v; want %s %s %v", *d.DueOn, d.State, d.OverdueWhenComputed,
					w.Version.LateWhenRecorded, tc.dueOn, tc.state, tc.late)
			}
		})
	}
}
