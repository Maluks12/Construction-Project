package notifications

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/platform/clock"
	"construction_db/internal/platform/database"
	"construction_db/internal/regimes"
)

// The worker's jobs and how often each runs. A job with no successful run
// for twice its interval is stale (D24); /healthz/jobs reports it.
const (
	JobReminders = "reminders"
	JobEmails    = "emails"
	JobHolidays  = "holidays"
	// JobExports builds audit packs (M12) in a loop of its own; each pass is
	// recorded here so a stopped builder shows as stale.
	JobExports = "exports"
)

var Schedule = map[string]time.Duration{
	JobReminders: 15 * time.Minute,
	JobEmails:    time.Minute,
	JobHolidays:  6 * time.Hour,
	JobExports:   15 * time.Minute, // a pass lasts as long as its builds
}

// SendingTimeout: an email claimed this long ago and still 'sending' was
// interrupted mid-send (a crash or restart). Its result is unknown, so it is
// marked unknown and never sent again: at most once.
const SendingTimeout = 10 * time.Minute

// Mailer sends one email: SMTP in production, or a log line with
// EMAIL_MODE=log. Delivers says whether Send really sends: a log mailer's
// emails are recorded 'logged', never 'sent'.
type Mailer interface {
	Send(ctx context.Context, to, subject, body string) error
	Delivers() bool
}

// Jobs runs the worker's work, organisation by organisation, each in its own
// tenant transaction under RLS.
type Jobs struct {
	Pool     *pgxpool.Pool
	Registry *regimes.Registry
	Mailer   Mailer
	// StaleSending overrides SendingTimeout (tests); zero means the default.
	StaleSending time.Duration
	// OnlyOrgs limits the jobs to these organisations (tests); nil means all.
	OnlyOrgs []string
	// Clock is now for the emails; nil is the real time.
	Clock clock.Clock
}

func (j *Jobs) orgs(ctx context.Context) ([]string, error) {
	if j.OnlyOrgs != nil {
		return j.OnlyOrgs, nil
	}
	var out []string
	err := database.WithTenant(ctx, j.Pool, database.Tenant{}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id::text FROM worker_org_ids() id`)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	return out, err
}

func (j *Jobs) inOrg(ctx context.Context, orgID string, fn func(pgx.Tx) error) error {
	return database.WithTenant(ctx, j.Pool, database.Tenant{OrgID: orgID}, fn)
}

// remindersSQL inserts what is due and queues an email for each new row
// whose recipient has an email address. ON CONFLICT DO NOTHING is the
// once-only rule: a reminder already given inserts nothing, so it queues no
// email either.
const remindersSQL = `
	WITH due AS (
	    SELECT * FROM notifications_due($1, $2, $3, $4, $5, $6, $7, $8, $9)
	), added AS (
	    INSERT INTO notifications (org_id, user_id, kind, subject_type, subject_id, threshold, site_id, title, due_on, due_at)
	    SELECT app_org_id(), user_id, kind, subject_type, subject_id, threshold, site_id, title, due_on, due_at FROM due
	    ON CONFLICT ON CONSTRAINT notifications_once DO NOTHING
	    RETURNING id, org_id, user_id
	), mailed AS (
	    INSERT INTO notification_emails (notification_id, org_id, to_address)
	    SELECT a.id, a.org_id, u.email FROM added a JOIN users u ON u.id = a.user_id WHERE u.email IS NOT NULL
	    RETURNING 1
	)
	SELECT (SELECT count(*) FROM added), (SELECT count(*) FROM mailed)`

// Reminders inserts every reminder due at now, for every organisation and
// regime, and returns how many were new.
func (j *Jobs) Reminders(ctx context.Context, now time.Time) (int, error) {
	orgs, err := j.orgs(ctx)
	if err != nil {
		return 0, fmt.Errorf("reminders: %w", err)
	}
	total := 0
	for _, org := range orgs {
		err := j.inOrg(ctx, org, func(tx pgx.Tx) error {
			for _, code := range j.Registry.Codes() {
				ds, _ := j.Registry.Get(code)
				r := ds.Reminders
				var added, mailed int
				if err := tx.QueryRow(ctx, remindersSQL, now, code, ds.Regime.TimeZone, r.WorkerExpiryDays, r.CompanyDocumentDays,
					r.DeadlineDaysBefore, r.ActionEscalationDays, r.VerificationStalledDays, Labels(ds)).Scan(&added, &mailed); err != nil {
					return err
				}
				total += added
			}
			return nil
		})
		if err != nil {
			return total, fmt.Errorf("reminders for org %s: %w", org, err)
		}
	}
	return total, nil
}

type queued struct {
	id, to, title, kind, threshold string
	dueOn                          *string
	dueAt                          *time.Time
	regime                         *string
}

// Emails sends the queued emails, at most once each:
//  1. a pending email whose subject no longer stands is cancelled;
//  2. one left 'sending' longer than SendingTimeout is marked unknown;
//  3. a batch of pending ones is claimed ('sending', FOR UPDATE SKIP LOCKED,
//     so two workers never claim the same row) and committed before any
//     SMTP call;
//  4. each is sent, then marked sent or failed.
//
// A crash between 3 and 4 leaves the row 'sending', and step 2 later marks
// it unknown: it is never sent again.
func (j *Jobs) Emails(ctx context.Context) (sent int, err error) {
	orgs, err := j.orgs(ctx)
	if err != nil {
		return 0, fmt.Errorf("emails: %w", err)
	}
	for _, org := range orgs {
		var batch []queued
		err := j.inOrg(ctx, org, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `
				UPDATE notification_emails e SET status = 'cancelled'
				FROM notifications n
				WHERE n.id = e.notification_id AND e.status = 'pending'
				  AND NOT notification_subject_current(n.subject_type, n.subject_id, n.threshold)`); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE notification_emails SET status = 'unknown', error = 'interrupted while sending'
				WHERE status = $$sending$$ AND claimed_at < clock_timestamp() - make_interval(secs => $1)`, j.staleSending().Seconds()); err != nil {
				return err
			}
			rows, err := tx.Query(ctx, `
				WITH claimed AS (
				    -- status = pending again outside: a row another worker claimed
				    -- while this one waited is not claimed twice.
				    UPDATE notification_emails SET status = $$sending$$
				    WHERE status = $$pending$$ AND notification_id IN (SELECT notification_id FROM notification_emails WHERE status = $$pending$$
				                              ORDER BY created_at LIMIT 50 FOR UPDATE SKIP LOCKED)
				    RETURNING notification_id, to_address
				)
				SELECT c.notification_id::text, c.to_address, n.title, n.kind, n.threshold, n.due_on::text, n.due_at, notification_regime(n)
				FROM claimed c JOIN notifications n ON n.id = c.notification_id`)
			if err != nil {
				return err
			}
			batch, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (queued, error) {
				var q queued
				return q, r.Scan(&q.id, &q.to, &q.title, &q.kind, &q.threshold, &q.dueOn, &q.dueAt, &q.regime)
			})
			return err
		})
		if err != nil {
			return sent, fmt.Errorf("emails for org %s: claim: %w", org, err)
		}
		for _, q := range batch {
			subject, body := Render(q.title, q.kind, q.threshold, q.dueOn, q.dueAt, j.Clock.Now(), j.zone(q.regime))
			sendErr := j.Mailer.Send(ctx, q.to, subject, body)
			status, msg := "sent", (*string)(nil)
			if !j.Mailer.Delivers() {
				status = "logged"
			}
			if sendErr != nil {
				status = "failed"
				m := truncate(sendErr.Error(), 1000)
				msg = &m
				slog.Warn("email failed", "notification_id", q.id, "error", sendErr)
			}
			if err := j.inOrg(ctx, org, func(tx pgx.Tx) error {
				_, err := tx.Exec(ctx, `UPDATE notification_emails SET status = $2, error = $3 WHERE notification_id = $1 AND status = 'sending'`,
					q.id, status, msg)
				return err
			}); err != nil {
				return sent, fmt.Errorf("emails for org %s: record: %w", org, err)
			}
			if sendErr == nil {
				sent++
			}
		}
	}
	return sent, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// Holidays recomputes, per organisation, the live deadlines a public holiday
// not yet processed falls inside (000029's deadlines_recompute). Only
// holidays inside some live deadline's window are tried.
func (j *Jobs) Holidays(ctx context.Context) (int, error) {
	orgs, err := j.orgs(ctx)
	if err != nil {
		return 0, fmt.Errorf("holidays: %w", err)
	}
	total := 0
	for _, org := range orgs {
		err := j.inOrg(ctx, org, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `
				SELECT h.jurisdiction, h.holiday_date::text FROM public_holidays h
				WHERE NOT EXISTS (SELECT 1 FROM holiday_recomputations r WHERE r.org_id = app_org_id()
				                  AND r.jurisdiction = h.jurisdiction AND r.holiday_date = h.holiday_date)
				  AND EXISTS (SELECT 1 FROM deadlines d WHERE d.closed_as IS NULL AND d.jurisdiction = h.jurisdiction AND d.due_on IS NOT NULL
				              AND h.holiday_date BETWEEN least((d.basis_at AT TIME ZONE d.time_zone)::date, d.due_on)
				                                     AND greatest((d.basis_at AT TIME ZONE d.time_zone)::date, coalesce(d.legal_latest_on, d.due_on)))
				ORDER BY h.holiday_date`)
			if err != nil {
				return err
			}
			type day struct{ j, d string }
			days, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (day, error) {
				var x day
				return x, r.Scan(&x.j, &x.d)
			})
			if err != nil {
				return err
			}
			for _, d := range days {
				var n *int
				if err := tx.QueryRow(ctx, `SELECT deadlines_recompute($1, $2::date)`, d.j, d.d).Scan(&n); err != nil {
					return err
				}
				if n != nil {
					total += *n
				}
			}
			return nil
		})
		if err != nil {
			return total, fmt.Errorf("holidays for org %s: %w", org, err)
		}
	}
	return total, nil
}

// RecordRun runs fn as one job run: a job_runs row before (so a run that
// never finishes is visible) and its outcome after.
func RecordRun(ctx context.Context, pool *pgxpool.Pool, job, instance string, fn func(context.Context) (int, error)) error {
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO job_runs (job, instance) VALUES ($1, $2) RETURNING id`, job, instance).Scan(&id); err != nil {
		return fmt.Errorf("record run of %s: %w", job, err)
	}
	n, runErr := fn(ctx)
	var msg *string
	if runErr != nil {
		m := truncate(runErr.Error(), 2000)
		msg = &m
	}
	// A fresh context: the job's own may have been cancelled, and the
	// outcome must still be recorded.
	fin, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if _, err := pool.Exec(fin, `UPDATE job_runs SET ok = $2, error = $3, counts = jsonb_build_object('count', $4::int) WHERE id = $1`,
		id, runErr == nil, msg, n); err != nil {
		return errors.Join(runErr, fmt.Errorf("record end of %s: %w", job, err))
	}
	return runErr
}

// Stale lists the jobs with no successful run within twice their interval
// before now. A job that never ran counts as stale too.
func Stale(ctx context.Context, pool *pgxpool.Pool, now time.Time) ([]string, error) {
	var out []string
	for _, job := range []string{JobReminders, JobEmails, JobHolidays, JobExports} {
		var ok bool
		err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM job_runs WHERE job = $1 AND ok AND finished_at >= $2)`,
			job, now.Add(-2*Schedule[job])).Scan(&ok)
		if err != nil {
			return nil, fmt.Errorf("stale jobs: %w", err)
		}
		if !ok {
			out = append(out, job)
		}
	}
	return out, nil
}

func (j *Jobs) staleSending() time.Duration {
	if j.StaleSending > 0 {
		return j.StaleSending
	}
	return SendingTimeout
}
