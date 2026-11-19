package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"construction_db/internal/core/files"
	"construction_db/internal/core/notifications"
)

// recordingMailer remembers what it sent; fail makes every send fail.
type recordingMailer struct {
	mu     sync.Mutex
	sent   []string
	bodies []string
	fail   bool
	// logOnly stands in for EMAIL_MODE=log.
	logOnly bool
}

func (m *recordingMailer) Send(_ context.Context, to, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return errors.New("smtp: 451 try later")
	}
	m.sent = append(m.sent, to+" "+subject)
	m.bodies = append(m.bodies, body)
	return nil
}

// Delivers: the recording mailer stands in for SMTP unless logOnly.
func (m *recordingMailer) Delivers() bool { return !m.logOnly }

func (m *recordingMailer) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sent)
}

func inbox(t *testing.T, router http.Handler, token string) []notifications.Notification {
	t.Helper()
	rec := do(t, router, http.MethodGet, "/v1/notifications", token, nil)
	wantStatus(t, rec, http.StatusOK)
	var b struct{ Notifications []notifications.Notification }
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	return b.Notifications
}

// expiringLicence records a rope access licence expiring in 10 days and
// returns the competency's id.
func expiringLicence(t *testing.T, f evidenceFixture, worker string, supersedes any) string {
	t.Helper()
	id := uuid.NewString()
	file := uploadFile(t, f.router, f.owner, files.PurposeCompetency, nil)
	body := competencyBody(file, time.Now().Format(time.DateOnly), time.Now().AddDate(0, 0, 10).Format(time.DateOnly))
	if supersedes != nil {
		body = with(with(body, "supersedes_id", supersedes), "expires_on", time.Now().AddDate(1, 0, 0).Format(time.DateOnly))
	}
	wantStatus(t, do(t, f.router, http.MethodPut, workerPath(worker)+"/competencies/"+id, f.owner, body), http.StatusCreated)
	return id
}

func TestRemindersExactlyOnce(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFixture(t, "remind")
	w := newWorker(t, f.router, f.owner, map[string]any{"name": "Licensed Worker"})
	c1 := expiringLicence(t, f, w, nil)
	mailer := &recordingMailer{}
	jobs := &notifications.Jobs{Pool: testPool, Registry: testRegistry, Mailer: mailer, OnlyOrgs: []string{f.ownerMe.OrgID}}

	n, err := jobs.Reminders(ctx, time.Now())
	if err != nil || n != 1 {
		t.Fatalf("first run: %d new, err %v; want 1 (the owner, before_14d)", n, err)
	}
	// A restart, or the next run: nothing new, nothing queued twice.
	if n, err := jobs.Reminders(ctx, time.Now()); err != nil || n != 0 {
		t.Errorf("second run: %d new, err %v; want 0", n, err)
	}
	if sent, err := jobs.Emails(ctx); err != nil || sent != 1 || mailer.count() != 1 {
		t.Errorf("emails: sent %d (mailer %d), err %v; want 1", sent, mailer.count(), err)
	}
	if sent, _ := jobs.Emails(ctx); sent != 0 || mailer.count() != 1 {
		t.Errorf("a second send run sent %d more", sent)
	}

	box := inbox(t, f.router, f.owner)
	if len(box) != 1 || box[0].SubjectID != c1 || box[0].Threshold != "before_14d" || box[0].SubjectState != notifications.SubjectCurrent {
		t.Fatalf("owner's inbox %+v", box)
	}
	if len(inbox(t, f.router, f.supervisor)) != 0 {
		t.Error("the supervisor (worker not on their site's roster) got the reminder")
	}
	rec := do(t, f.router, http.MethodPost, "/v1/notifications/"+box[0].ID+"/read", f.owner, nil)
	wantStatus(t, rec, http.StatusOK)
	wantStatus(t, do(t, f.router, http.MethodPost, "/v1/notifications/"+box[0].ID+"/read", f.owner, nil), http.StatusOK)
	wantError(t, do(t, f.router, http.MethodPost, "/v1/notifications/"+box[0].ID+"/read", f.supervisor, nil), http.StatusNotFound, "notification not found")
	wantError(t, do(t, f.router, http.MethodGet, "/v1/notifications?limit=0", f.owner, nil), http.StatusBadRequest, "limit must be 1-200")

	// Seven days out: the next threshold, once.
	if n, _ := jobs.Reminders(ctx, time.Now().AddDate(0, 0, 4)); n != 1 {
		t.Errorf("at 6 days left: %d new, want 1 (before_7d)", n)
	}
	// A renewal before the email goes: it is cancelled, never sent, and the
	// inbox shows the old reminder superseded.
	expiringLicence(t, f, w, c1)
	if sent, _ := jobs.Emails(ctx); sent != 0 {
		t.Errorf("sent %d emails about a renewed licence", sent)
	}
	if n := countRows(t, `SELECT count(*) FROM notification_emails WHERE org_id = $1 AND status = 'cancelled'`, f.ownerMe.OrgID); n != 1 {
		t.Errorf("%d cancelled emails, want 1", n)
	}
	for _, x := range inbox(t, f.router, f.owner) {
		if x.SubjectState != notifications.SubjectSuperseded {
			t.Errorf("reminder %s about the renewed licence is %s", x.Threshold, x.SubjectState)
		}
	}
	if n, _ := jobs.Reminders(ctx, time.Now().AddDate(0, 0, 9)); n != 0 {
		t.Errorf("the renewed licence still reminds: %d new", n)
	}
}

// Two workers at once: the second's inserts wait on the first's
// uncommitted rows (the unique key), then insert nothing. Forced: the first
// worker's transaction is held open.
func TestRemindersTwoWorkers(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFixture(t, "remind-race")
	w := newWorker(t, f.router, f.owner, map[string]any{"name": "Race Licence"})
	expiringLicence(t, f, w, nil)
	jobs := &notifications.Jobs{Pool: testPool, Registry: testRegistry, Mailer: &recordingMailer{}, OnlyOrgs: []string{f.ownerMe.OrgID}}

	tx := holdOpen(t, f)
	if _, err := tx.Exec(ctx, `
		INSERT INTO notifications (org_id, user_id, kind, subject_type, subject_id, threshold, title)
		SELECT app_org_id(), user_id, kind, subject_type, subject_id, threshold, title
		FROM notifications_due(now(), 'za_construction_2014', 'Africa/Johannesburg', ARRAY[30,14,7,0], ARRAY[60,30,7,0], ARRAY[2,0], ARRAY[0,1,3], 2)`); err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	go func() {
		n, err := jobs.Reminders(ctx, time.Now())
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
	if n := countRows(t, `SELECT count(*) FROM notifications WHERE org_id = $1`, f.ownerMe.OrgID); n != 1 {
		t.Errorf("%d notifications stored, want 1", n)
	}
}

// At most once: an email interrupted mid-send is never sent again, and a
// failed one is not retried.
func TestEmailsAtMostOnce(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFixture(t, "email-once")
	w := newWorker(t, f.router, f.owner, map[string]any{"name": "Mail Worker"})
	expiringLicence(t, f, w, nil)
	mailer := &recordingMailer{}
	jobs := &notifications.Jobs{Pool: testPool, Registry: testRegistry, Mailer: mailer, OnlyOrgs: []string{f.ownerMe.OrgID}, StaleSending: time.Nanosecond}
	if n, err := jobs.Reminders(ctx, time.Now()); err != nil || n != 1 {
		t.Fatalf("reminders: %d, %v", n, err)
	}
	// A worker claimed it and died before the SMTP call returned.
	execAsTenant(t, f.ownerMe.OrgID, 1, `UPDATE notification_emails SET status = 'sending' WHERE status = 'pending'`)
	if sent, err := jobs.Emails(ctx); err != nil || sent != 0 || mailer.count() != 0 {
		t.Errorf("sent %d (mailer %d), err %v; an interrupted send must never be repeated", sent, mailer.count(), err)
	}
	if n := countRows(t, `SELECT count(*) FROM notification_emails WHERE org_id = $1 AND status = 'unknown'`, f.ownerMe.OrgID); n != 1 {
		t.Errorf("%d emails marked unknown, want 1", n)
	}

	// A failed send is recorded and not retried.
	if n, _ := jobs.Reminders(ctx, time.Now().AddDate(0, 0, 4)); n != 1 {
		t.Fatalf("next threshold: %d", n)
	}
	mailer.fail = true
	if sent, _ := jobs.Emails(ctx); sent != 0 {
		t.Errorf("a failing mailer counted %d sent", sent)
	}
	mailer.fail = false
	if sent, _ := jobs.Emails(ctx); sent != 0 || mailer.count() != 0 {
		t.Errorf("a failed email was retried")
	}
	if n := countRows(t, `SELECT count(*) FROM notification_emails WHERE org_id = $1 AND status = 'failed'`, f.ownerMe.OrgID); n != 1 {
		t.Errorf("%d failed emails, want 1", n)
	}
}

// Every run is recorded; a run that never finished doesn't count as done,
// and /healthz/jobs reports a job gone twice its interval without a
// completed run.
func TestJobRunsAndHealth(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFixture(t, "jobs")
	jobs := &notifications.Jobs{Pool: testPool, Registry: testRegistry, Mailer: &recordingMailer{}, OnlyOrgs: []string{f.ownerMe.OrgID}}
	for name, fn := range map[string]func(context.Context) (int, error){
		notifications.JobReminders: func(ctx context.Context) (int, error) { return jobs.Reminders(ctx, time.Now()) },
		notifications.JobEmails:    jobs.Emails,
		notifications.JobHolidays:  jobs.Holidays,
	} {
		if err := notifications.RecordRun(ctx, testPool, name, "test", fn); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	rec := do(t, f.router, http.MethodGet, "/healthz/jobs", "", nil)
	wantStatus(t, rec, http.StatusOK)

	failing := errors.New("boom")
	if err := notifications.RecordRun(ctx, testPool, notifications.JobHolidays, "test", func(context.Context) (int, error) { return 0, failing }); !errors.Is(err, failing) {
		t.Errorf("RecordRun returned %v", err)
	}
	if n := countRows(t, `SELECT count(*) FROM job_runs WHERE instance = 'test' AND NOT ok AND error = 'boom'`); n < 1 {
		t.Error("the failed run wasn't recorded")
	}
	// A killed run: started, never finished.
	if _, err := testAdmin.Exec(ctx, `INSERT INTO job_runs (job, instance) VALUES ('reminders', 'killed')`); err != nil {
		t.Fatal(err)
	}
	stale, err := notifications.Stale(ctx, testPool, time.Now().Add(3*time.Hour))
	if err != nil || !slices.Contains(stale, notifications.JobReminders) || !slices.Contains(stale, notifications.JobEmails) {
		t.Errorf("3 hours on with no new runs: stale %v, err %v; want reminders and emails", stale, err)
	}
	if stale, _ := notifications.Stale(ctx, testPool, time.Now()); len(stale) != 0 {
		t.Errorf("just after runs: stale %v", stale)
	}
}

// Two senders at once: while one worker's claim is uncommitted, another
// skips the row at once (SKIP LOCKED) rather than waiting and sending it too.
func TestEmailClaimSkipsLockedRows(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFixture(t, "email-claim")
	w := newWorker(t, f.router, f.owner, map[string]any{"name": "Claim Worker"})
	expiringLicence(t, f, w, nil)
	mailer := &recordingMailer{}
	jobs := &notifications.Jobs{Pool: testPool, Registry: testRegistry, Mailer: mailer, OnlyOrgs: []string{f.ownerMe.OrgID}}
	if n, err := jobs.Reminders(ctx, time.Now()); err != nil || n != 1 {
		t.Fatalf("reminders: %d, %v", n, err)
	}

	tx := holdOpen(t, f)
	if tag, err := tx.Exec(ctx, `UPDATE notification_emails SET status = 'sending' WHERE status = 'pending'`); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("first worker's claim: %v %v", tag, err)
	}
	done := make(chan int, 1)
	go func() {
		n, err := jobs.Emails(ctx)
		if err != nil {
			t.Error(err)
		}
		done <- n
	}()
	select {
	case n := <-done:
		if n != 0 || mailer.count() != 0 {
			t.Errorf("the second worker sent %d (mailer %d) of a row the first had claimed", n, mailer.count())
		}
	case <-time.After(5 * time.Second):
		t.Error("the second worker waited on the first's claim instead of skipping it")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if n, _ := jobs.Emails(ctx); n != 1 || mailer.count() != 1 {
		t.Errorf("after the first worker gave up its claim: sent %d (mailer %d), want 1", n, mailer.count())
	}
}

// EMAIL_MODE=log: emails are recorded 'logged', never 'sent' (M11 review).
func TestEmailsLoggedNotSent(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFixture(t, "email-log")
	w := newWorker(t, f.router, f.owner, map[string]any{"name": "Logged Worker"})
	expiringLicence(t, f, w, nil)
	jobs := &notifications.Jobs{Pool: testPool, Registry: testRegistry, Mailer: &recordingMailer{logOnly: true}, OnlyOrgs: []string{f.ownerMe.OrgID}}
	if n, err := jobs.Reminders(ctx, time.Now()); err != nil || n != 1 {
		t.Fatalf("reminders: %d, %v", n, err)
	}
	if _, err := jobs.Emails(ctx); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, `SELECT count(*) FROM notification_emails WHERE org_id = $1 AND status = 'logged'`, f.ownerMe.OrgID); n != 1 {
		t.Errorf("%d emails logged, want 1", n)
	}
	if n := countRows(t, `SELECT count(*) FROM notification_emails WHERE org_id = $1 AND status = 'sent'`, f.ownerMe.OrgID); n != 0 {
		t.Errorf("%d emails recorded as sent by a log-only mailer", n)
	}
}
