package database_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// The regime's lead times, as reminders.json has them.
const dueSQL = `
	SELECT user_id::text, kind, subject_type, subject_id::text, threshold
	FROM notifications_due($1, 'za_construction_2014', 'Africa/Johannesburg', ARRAY[30,14,7,0], ARRAY[60,30,7,0], ARRAY[2,0], ARRAY[0,1,3], 2)`

type due struct{ user, kind, subjectType, subject, threshold string }

func dueAt(t *testing.T, tx pgx.Tx, at time.Time) []due {
	t.Helper()
	rows, err := tx.Query(context.Background(), dueSQL, at)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (due, error) {
		var d due
		return d, r.Scan(&d.user, &d.kind, &d.subjectType, &d.subject, &d.threshold)
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func dueFor(ds []due, subject string) map[string][]string {
	out := map[string][]string{}
	for _, d := range ds {
		if d.subject == subject {
			out[d.threshold] = append(out[d.threshold], d.user)
		}
	}
	for k := range out {
		slices.Sort(out[k])
	}
	return out
}

func sorted(s ...string) []string { slices.Sort(s); return s }

func TestNotificationsDue(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()
	tx := beginAs(t, pool, rlsOrgA)
	addActionMembers(t, tx)
	now := time.Now()
	in := func(days int) string { return now.In(johannesburg()).AddDate(0, 0, days).Format(time.DateOnly) }
	w := workerID(501)
	must(t, tx, 1, workerSQL, w, rlsOrgA, "Expiring Worker", nil, nil, rlsUserA)
	must(t, tx, 1, `INSERT INTO site_roster (org_id, site_id, worker_id, added_via, added_by) VALUES ($1, $2, $3, 'manual', $4)`, rlsOrgA, rlsSiteA, w, rlsUserA)
	const comp = `
		INSERT INTO worker_competencies (id, org_id, worker_id, regime_code, type_key, issued_on, expires_on, file_id, supersedes_id,
		                                 recorded_by, client_recorded_at)
		VALUES ($1, $2, $3, 'za_construction_2014', 'rope_access_licence', '2026-01-01', $4, $5, $6, $7, now())`
	c1 := compID(501)
	must(t, tx, 1, comp, c1, rlsOrgA, w, in(10), verifiedFile(t, tx, 501, "competency"), nil, rlsUserA)

	leadsAndSiteSups := sorted(rlsUserA, sheOfficer, supOnSite, supOnSite2)
	t.Run("an expiry is told at the latest lead crossed, to leads and the worker's site supervisors", func(t *testing.T) {
		got := dueFor(dueAt(t, tx, now), c1)
		if !slices.Equal(got["before_14d"], leadsAndSiteSups) || len(got) != 1 {
			t.Errorf("due for the licence 10 days out: %v; want before_14d to %v", got, leadsAndSiteSups)
		}
		// Its last day: on_day. Expired since: still on_day, nothing new.
		if got := dueFor(dueAt(t, tx, now.AddDate(0, 0, 10)), c1); len(got["on_day"]) != 4 {
			t.Errorf("on the day: %v", got)
		}
		if got := dueFor(dueAt(t, tx, now.AddDate(0, 0, 40)), c1); len(got["on_day"]) != 4 || len(got) != 1 {
			t.Errorf("long expired: %v", got)
		}
		// 40 days out, before the first lead: nothing.
		must(t, tx, 1, `UPDATE workers SET trade = 'x' WHERE id = $1`, w) // no effect on reminders
		if got := dueFor(dueAt(t, tx, now.AddDate(0, 0, -30)), c1); len(got) != 0 {
			t.Errorf("40 days before expiry: %v", got)
		}
	})

	t.Run("each reminder once, however often it is inserted", func(t *testing.T) {
		const insert = `
			INSERT INTO notifications (org_id, user_id, kind, subject_type, subject_id, threshold, title)
			SELECT $2, user_id, kind, subject_type, subject_id, threshold, title FROM notifications_due($1, 'za_construction_2014',
			       'Africa/Johannesburg', ARRAY[30,14,7,0], ARRAY[60,30,7,0], ARRAY[2,0], ARRAY[0,1,3], 2)
			ON CONFLICT ON CONSTRAINT notifications_once DO NOTHING`
		first, err := inSavepoint(ctx, tx, true, insert, now, rlsOrgA)
		if err != nil || first == 0 {
			t.Fatalf("first run inserted %d, err %v", first, err)
		}
		if again, err := inSavepoint(ctx, tx, true, insert, now, rlsOrgA); err != nil || again != 0 {
			t.Errorf("a second run inserted %d, err %v; want 0", again, err)
		}
		// Without the unique key, the second run would double-send.
		refused(t, tx, "23505", "notifications_once", `INSERT INTO notifications (org_id, user_id, kind, subject_type, subject_id, threshold, title)
			VALUES ($1, $2, 'competency_expiry', 'competency', $3, 'before_14d', 'x')`, rlsOrgA, rlsUserA, c1)
	})

	t.Run("a renewal cancels the old record's reminders", func(t *testing.T) {
		c2 := compID(502)
		must(t, tx, 1, comp, c2, rlsOrgA, w, in(400), verifiedFile(t, tx, 502, "competency"), c1, rlsUserA)
		if got := dueFor(dueAt(t, tx, now.AddDate(0, 0, 10)), c1); len(got) != 0 {
			t.Errorf("the renewed licence is still due: %v", got)
		}
		var current bool
		if err := tx.QueryRow(ctx, `SELECT notification_subject_current('competency', $1, 'before_14d')`, c1).Scan(&current); err != nil || current {
			t.Errorf("superseded subject current = %v, err %v", current, err)
		}
		if err := tx.QueryRow(ctx, `SELECT notification_subject_current('competency', $1, 'before_14d')`, c2).Scan(&current); err != nil || !current {
			t.Errorf("renewal current = %v, err %v", current, err)
		}
	})

	t.Run("the seeded deadline is forthwith; the awaiting action stalls after 2 days", func(t *testing.T) {
		ds := dueAt(t, tx, now.AddDate(0, 0, 3))
		if got := dueFor(ds, rlsDutyA); len(got["forthwith"]) == 0 {
			t.Errorf("the forthwith deadline: %v", got)
		}
		var closeout string
		if err := tx.QueryRow(ctx, `SELECT pending_closeout_id::text FROM corrective_actions WHERE id = $1`, rlsActionA).Scan(&closeout); err != nil {
			t.Fatal(err)
		}
		if got := dueFor(ds, closeout); !slices.Equal(got["stalled"], sorted(rlsUserA, sheOfficer)) {
			t.Errorf("stalled verification: %v", got)
		}
		if got := dueFor(dueAt(t, tx, now), closeout); len(got) != 0 {
			t.Errorf("not yet stalled: %v", got)
		}
	})
}

func TestNotificationRules(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()
	tx := beginAs(t, pool, rlsOrgA)
	addActionMembers(t, tx)
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO notifications (org_id, user_id, kind, subject_type, subject_id, threshold, title)
		VALUES ($1, $2, 'action_overdue', 'action', $3, 'escalation_1', 'Fix it') RETURNING id::text`, rlsOrgA, sheOfficer, rlsActionA).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Run("only its recipient reads it, once", func(t *testing.T) {
		_, _ = tx.Exec(ctx, `SELECT set_config('app.user_id', $1, true)`, rlsUserA)
		refused(t, tx, "23514", "only its recipient reads it", `UPDATE notifications SET read_at = now() WHERE id = $1`, id)
		_, _ = tx.Exec(ctx, `SELECT set_config('app.user_id', $1, true)`, sheOfficer)
		must(t, tx, 1, `UPDATE notifications SET read_at = now() WHERE id = $1`, id)
		refused(t, tx, "23514", "the only change is reading it, once", `UPDATE notifications SET read_at = now() + interval '1 hour' WHERE id = $1`, id)
		refused(t, tx, "42501", "", `UPDATE notifications SET title = 'x' WHERE id = $1`, id)
		refused(t, tx, "42501", "", `DELETE FROM notifications WHERE id = $1`, id)
	})
	t.Run("an email goes pending, sending, then sent, failed or unknown; never back", func(t *testing.T) {
		must(t, tx, 1, `INSERT INTO notification_emails (notification_id, org_id, to_address) VALUES ($1, $2, 'she@test')`, id, rlsOrgA)
		const set = `UPDATE notification_emails SET status = $2 WHERE notification_id = $1`
		refused(t, tx, "23514", "pending can't become sent", set, id, "sent")
		refused(t, tx, "23514", "pending can't become unknown", set, id, "unknown")
		must(t, tx, 1, set, id, "sending")
		refused(t, tx, "23514", "sending can't become pending", set, id, "pending")
		refused(t, tx, "23514", "sending can't become cancelled", set, id, "cancelled")
		must(t, tx, 1, set, id, "sent")
		refused(t, tx, "23514", "sent can't become sending", set, id, "sending")
		refused(t, tx, "23514", "sent can't become pending", set, id, "pending")
		var claimed, finished *time.Time
		if err := tx.QueryRow(ctx, `SELECT claimed_at, finished_at FROM notification_emails WHERE notification_id = $1`, id).Scan(&claimed, &finished); err != nil ||
			claimed == nil || finished == nil {
			t.Errorf("claimed %v, finished %v, err %v", claimed, finished, err)
		}
	})
	t.Run("a job run finishes once", func(t *testing.T) {
		var run int64
		if err := tx.QueryRow(ctx, `INSERT INTO job_runs (job, instance) VALUES ('expiries', 'test') RETURNING id`).Scan(&run); err != nil {
			t.Fatal(err)
		}
		must(t, tx, 1, `UPDATE job_runs SET ok = true WHERE id = $1`, run)
		refused(t, tx, "23514", "finished once", `UPDATE job_runs SET ok = false WHERE id = $1`, run)
		refused(t, tx, "42501", "", `DELETE FROM job_runs WHERE id = $1`, run)
	})
}

// The holiday recompute: a live, unsatisfied working-day deadline whose
// window includes a new holiday is closed as recomputed and replaced, later;
// a satisfied or closed one never changes. As the app (RLS) after the
// superuser adds the holiday, all in one rolled-back transaction.
func TestHolidayRecompute(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()
	tx := beginAs(t, admin, "")
	var day time.Time
	if err := tx.QueryRow(ctx, `
		SELECT min(d)::date FROM generate_series(current_date + 2, current_date + 9, interval '1 day') d
		WHERE extract(isodow FROM d) < 6 AND NOT EXISTS (SELECT 1 FROM public_holidays WHERE jurisdiction = 'ZA' AND holiday_date = d::date)`).Scan(&day); err != nil {
		t.Fatal(err)
	}
	dayStr := day.Format(time.DateOnly)
	// The deadlines exist before the holiday is proclaimed.
	okBatch(t, tx, duty(dutyID(601), rlsIncA, "test_wd"), dl(dlID(601), dutyID(601), rlsIncA, "test_wd", "report", 10, "working_days", "-1 days", 1),
		duty(dutyID(602), rlsIncA, "test_wd_met"), dl(dlID(602), dutyID(602), rlsIncA, "test_wd_met", "report", 10, "working_days", "-1 days", 1),
		satisfy(satID(602), dutyID(602), dlID(602), "-1 minutes", rlsUserA), stmt{`SET CONSTRAINTS ALL IMMEDIATE`, nil})
	must(t, tx, 1, `INSERT INTO public_holidays (jurisdiction, holiday_date, name, kind, source_ref) VALUES ('ZA', $1, 'Test Day', 'proclaimed', 'Test proclamation')`, dayStr)
	okBatch(t, tx, stmt{`SET LOCAL ROLE siteproof_app`, nil})
	okBatch(t, tx, stmt{`SELECT set_config('app.org_id', $1, true)`, []any{rlsOrgA}})
	dueOf := func(id string) (time.Time, *string) {
		t.Helper()
		var at time.Time
		var closed *string
		if err := tx.QueryRow(ctx, `SELECT due_at, closed_as FROM deadlines WHERE id = $1`, id).Scan(&at, &closed); err != nil {
			t.Fatal(err)
		}
		return at, closed
	}
	before, _ := dueOf(dlID(601))
	metBefore, _ := dueOf(dlID(602))
	// The dates the deadline is computed with, as text, to compare whole.
	const datesSQL = `SELECT due_at::text, due_on::text, coalesce(legal_latest_on::text, '-'), excluded_days::text FROM deadlines WHERE id = $1`
	dates := func(id string) [4]string {
		t.Helper()
		var d [4]string
		if err := tx.QueryRow(ctx, datesSQL, id).Scan(&d[0], &d[1], &d[2], &d[3]); err != nil {
			t.Fatal(err)
		}
		return d
	}
	oldDates, metDates := dates(dlID(601)), dates(dlID(602))

	t.Run("only the recompute closes a deadline as recomputed, never a satisfied one", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "recomputed only by deadlines_recompute",
			stmt{`UPDATE deadlines SET closed_as = 'recomputed', superseded_by = $2, recomputed_for = $3 WHERE id = $1`, []any{dlID(601), dlID(699), dayStr}})
		// Even with the recompute's own setting, a satisfied duty's row stays.
		refusedBatch(t, tx, "23514", "a satisfied deadline is never recomputed",
			stmt{`SELECT set_config('app.recompute_holiday', $1, true)`, []any{dayStr}},
			stmt{`UPDATE deadlines SET closed_as = 'recomputed', superseded_by = $2, recomputed_for = $3 WHERE id = $1`, []any{dlID(602), dlID(699), dayStr}})
		okBatch(t, tx, stmt{`SELECT set_config('app.recompute_holiday', '', true)`, nil})
	})

	var n *int
	if err := tx.QueryRow(ctx, `SELECT deadlines_recompute('ZA', $1)`, dayStr).Scan(&n); err != nil {
		t.Fatalf("recompute: %v", err)
	}
	if n == nil || *n < 1 {
		t.Fatalf("recomputed %v rows, want at least 1", n)
	}
	t.Run("the open deadline moves; the old row is kept, closed", func(t *testing.T) {
		_, closed := dueOf(dlID(601))
		if closed == nil || *closed != "recomputed" {
			t.Fatalf("old row closed_as %v", closed)
		}
		var after time.Time
		var reason string
		if err := tx.QueryRow(ctx, `SELECT n.due_at, o.closed_reason FROM deadlines o JOIN deadlines n ON n.id = o.superseded_by WHERE o.id = $1`, dlID(601)).
			Scan(&after, &reason); err != nil {
			t.Fatal(err)
		}
		if !after.After(before) || reason != "Recomputed for a new public holiday: Test Day (Test proclamation)" {
			t.Errorf("due %s -> %s, reason %q", before, after, reason)
		}
		// The replacement's due_on and legal_latest_on are the calendar's
		// with the new holiday: what deadline_due says now, which the old
		// row (computed without it) doesn't.
		var replacement string
		if err := tx.QueryRow(ctx, `SELECT superseded_by::text FROM deadlines WHERE id = $1`, dlID(601)).Scan(&replacement); err != nil {
			t.Fatal(err)
		}
		var want [4]string
		if err := tx.QueryRow(ctx, `
			SELECT r.due_at::text, r.due_on::text, coalesce(r.legal_latest_on::text, '-'), r.excluded_days::text
			FROM deadlines d, deadline_due(d.basis_at, d.period, d.unit, d.direction, d.time_zone, d.jurisdiction) r WHERE d.id = $1`, dlID(601)).
			Scan(&want[0], &want[1], &want[2], &want[3]); err != nil {
			t.Fatal(err)
		}
		got := dates(replacement)
		if got != want || got[1] == oldDates[1] || !strings.Contains(got[3], dayStr) {
			t.Errorf("replacement dates %v, want %v (the calendar's, now excluding %s); the old row had %v", got, want, dayStr, oldDates)
		}
	})
	t.Run("the closed row keeps the dates it had", func(t *testing.T) {
		if got := dates(dlID(601)); got != oldDates {
			t.Errorf("closed row's dates %v, were %v", got, oldDates)
		}
	})
	t.Run("a satisfied deadline never changes", func(t *testing.T) {
		at, closed := dueOf(dlID(602))
		if closed != nil || !at.Equal(metBefore) || dates(dlID(602)) != metDates {
			t.Errorf("satisfied deadline: closed %v, due %s (was %s)", closed, at, metBefore)
		}
	})
	t.Run("a closed deadline never changes; a holiday is done once", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "it never changes again", stmt{`UPDATE deadlines SET closed_as = 'withdrawn', superseded_by = NULL WHERE id = $1`, []any{dlID(601)}})
		var rows int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM deadlines`).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		var again *int
		if err := tx.QueryRow(ctx, `SELECT deadlines_recompute('ZA', $1)`, dayStr).Scan(&again); err != nil || again != nil {
			t.Errorf("second recompute: %v, err %v; want NULL (already done)", again, err)
		}
		var after int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM deadlines`).Scan(&after); err != nil || after != rows {
			t.Errorf("the second run changed the deadlines: %d rows, was %d", after, rows)
		}
	})
}

func johannesburg() *time.Location {
	loc, err := time.LoadLocation("Africa/Johannesburg")
	if err != nil {
		panic(err)
	}
	return loc
}
