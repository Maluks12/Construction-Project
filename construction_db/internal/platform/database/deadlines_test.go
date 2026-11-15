package database_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"construction_db/internal/platform/database"
)

func dutyID(n int) string { return fmt.Sprintf("30000000-0000-0000-0000-%012d", n) }
func dlID(n int) string   { return fmt.Sprintf("31000000-0000-0000-0000-%012d", n) }
func satID(n int) string  { return fmt.Sprintf("32000000-0000-0000-0000-%012d", n) }

const (
	dutySQL    = `INSERT INTO deadline_duties (id, org_id, site_id, incident_id, rule_key) VALUES ($1, $2, $3, $4, $5)`
	closeSQL   = `UPDATE deadlines SET closed_as = $2, superseded_by = $3, closed_in_version = $4 WHERE id = $1`
	satisfySQL = `
		INSERT INTO deadline_satisfactions (id, org_id, site_id, duty_id, deadline_id, satisfied_at, method, recorded_by)
		VALUES ($1, $2, $3, $4, $5, now() + $6::interval, 'phone', $7)`
	retractSQL = `
		INSERT INTO deadline_satisfaction_retractions (satisfaction_id, org_id, site_id, duty_id, retracted_by, reason)
		VALUES ($1, $2, $3, $4, $5, 'Recorded in error')`
	// $1 incident, $2 version, $3 submitted_by, $4 occurred offset, $5 reason.
	versionBySQL = `
		INSERT INTO incident_versions (org_id, site_id, incident_id, version, taxonomy_codes, occurred_at, submitted_by,
		                               client_submitted_at, amend_reason, people_count, evidence_count)
		VALUES ('20000000-0000-0000-0000-00000000000a', '22000000-0000-0000-0000-00000000000a', $1, $2, ARRAY['fatality'],
		        now() + $4::interval, $3, now(), $5, 0, 0)`
)

// A deadline row for org A, site A: rule, kind, period, unit, basis offset
// from now, and the version it is computed in.
func dl(id, duty, inc, rule, kind string, period int, unit, basis string, version int) stmt {
	return stmt{`
		INSERT INTO deadlines (id, org_id, site_id, duty_id, incident_id, regime_id, rule_key, rule_label, kind, period, unit,
		                       direction, basis, source_ref, basis_at, time_zone, jurisdiction, due_at, excluded_days, computed_in_version)
		SELECT $1, $2, $3, $4, $5, id, $6, 'A rule', $7, $8, $9, 'after', 'incident_occurred_at', 'Test ref', now() + $10::interval,
		       'Africa/Johannesburg', 'ZA', '2000-01-01', '{}', $11
		FROM regulatory_regimes LIMIT 1`, []any{id, rlsOrgA, rlsSiteA, duty, inc, rule, kind, period, unit, basis, version}}
}

// addedDl is a GAR 8(2)-style row that recording satisfaction sat added.
func addedDl(id, duty, inc, rule string, version int, sat string) stmt {
	return stmt{`
		INSERT INTO deadlines (id, org_id, site_id, duty_id, incident_id, regime_id, rule_key, rule_label, kind, period, unit,
		                       direction, basis, source_ref, basis_at, time_zone, jurisdiction, due_at, excluded_days, computed_in_version,
		                       added_by_satisfaction_id)
		SELECT $1, $2, $3, $4, $5, id, $6, 'A rule', 'report', 0, 'immediately', 'after', 'person_died_at', 'Test ref', now(),
		       'Africa/Johannesburg', 'ZA', '2000-01-01', '{}', $7, $8
		FROM regulatory_regimes LIMIT 1`, []any{id, rlsOrgA, rlsSiteA, duty, inc, rule, version, sat}}
}

func duty(id, inc, rule string) stmt { return stmt{dutySQL, []any{id, rlsOrgA, rlsSiteA, inc, rule}} }

func closeDl(id, as string, by any, version int) stmt {
	return stmt{closeSQL, []any{id, as, by, version}}
}

func satisfy(id, duty, deadline, at, by string) stmt {
	return stmt{satisfySQL, []any{id, rlsOrgA, rlsSiteA, duty, deadline, at, by}}
}

func retract(sat, duty, by string) stmt {
	return stmt{retractSQL, []any{sat, rlsOrgA, rlsSiteA, duty, by}}
}

// amend submits version n of inc; version 1 has no reason.
func amend(inc string, n int, by, occurred string) stmt {
	var reason any
	if n > 1 {
		reason = fmt.Sprintf("Amendment %d", n)
	}
	return stmt{versionBySQL, []any{inc, n, by, occurred, reason}}
}

const (
	gar8a = "ohsa_gar8_1a_written_notice"
	gar8b = "ohsa_gar8_1b_immediate_notice"
	gar92 = "ohsa_gar9_2_investigation"
	gar82 = "ohsa_gar8_2_death_after_notice"
)

// The rules the database enforces on deadlines, as the app role.
func TestDeadlineRules(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()
	tx := beginAs(t, pool, rlsOrgA)

	// A supervisor in org A, for the role rules.
	const supervisor = "21000000-0000-0000-0000-0000000000a5"
	must(t, tx, 1, `INSERT INTO users (id, email, full_name) VALUES ($1, 'sup-a@rls.test', 'Supervisor A')`, supervisor)
	must(t, tx, 1, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'supervisor')`, rlsOrgA, supervisor)

	// incID(20): version 1 by the owner, two hours ago.
	inc := incID(20)
	okBatch(t, tx, header(inc), amend(inc, 1, rlsUserA, "-2 hours"))
	okBatch(t, tx, duty(dutyID(1), inc, gar8a), dl(dlID(1), dutyID(1), inc, gar8a, "report", 7, "calendar_days", "-2 hours", 1))

	t.Run("due values come from deadline_due, not the client", func(t *testing.T) {
		var mismatched int
		err := tx.QueryRow(ctx, `
			SELECT count(*) FROM deadlines d, deadline_due(d.basis_at, d.period, d.unit, d.direction, d.time_zone, d.jurisdiction) f
			WHERE d.id = $1 AND (d.due_at, d.due_on, d.excluded_days) IS DISTINCT FROM (f.due_at, f.due_on, f.excluded_days)`, dlID(1)).Scan(&mismatched)
		if err != nil || mismatched != 0 {
			t.Errorf("stored due values differ from deadline_due: %d rows, err %v", mismatched, err)
		}
		var computed time.Time
		if err := tx.QueryRow(ctx, `SELECT computed_at FROM deadlines WHERE id = $1`, dlID(1)).Scan(&computed); err != nil || time.Since(computed) > time.Minute {
			t.Errorf("computed_at %v, err %v; want the server's time", computed, err)
		}
	})

	t.Run("a new deadline is live and belongs to the latest version", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "a new deadline is live", stmt{`
			INSERT INTO deadlines (org_id, site_id, duty_id, incident_id, regime_id, rule_key, rule_label, kind, period, unit, direction,
			                       basis, source_ref, basis_at, time_zone, jurisdiction, due_at, excluded_days, computed_in_version, closed_as)
			SELECT $1, $2, $3, $4, id, $5, 'L', 'report', 7, 'calendar_days', 'after', 'incident_occurred_at', 'r', now(),
			       'Africa/Johannesburg', 'ZA', now(), '{}', 1, 'withdrawn' FROM regulatory_regimes LIMIT 1`,
			[]any{rlsOrgA, rlsSiteA, dutyID(2), inc, gar92}})
		refusedBatch(t, tx, "23514", "computed in version 2 but the incident is at version 1",
			duty(dutyID(2), inc, gar92), dl(dlID(2), dutyID(2), inc, gar92, "investigation", 7, "calendar_days", "-2 hours", 2))
		// One live row per duty; one duty per incident and rule; a row's
		// rule is its duty's.
		refusedBatch(t, tx, "23505", "deadlines_one_live", dl(dlID(2), dutyID(1), inc, gar8a, "report", 7, "calendar_days", "-1 hours", 1))
		refusedBatch(t, tx, "23505", "deadline_duties_once", duty(dutyID(2), inc, gar8a))
		refusedBatch(t, tx, "23503", "", duty(dutyID(2), inc, gar92), dl(dlID(2), dutyID(2), inc, gar8b, "report", 0, "immediately", "-2 hours", 1))
	})

	t.Run("closing needs a later version", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "closed only by the incident's latest version", closeDl(dlID(1), "withdrawn", nil, 1))
	})

	// Version 2 by the owner: the incident was two days earlier than first
	// said. The written notice is superseded with an earlier due date.
	okBatch(t, tx, amend(inc, 2, rlsUserA, "-2 days"),
		closeDl(dlID(1), "superseded", dlID(3), 2),
		dl(dlID(3), dutyID(1), inc, gar8a, "report", 7, "calendar_days", "-2 days", 2),
		stmt{checkNow, nil})

	t.Run("who, when and why come from the closing version", func(t *testing.T) {
		var ok bool
		err := tx.QueryRow(ctx, `
			SELECT d.closed_by = v.submitted_by AND d.closed_at = v.submitted_at AND d.closed_reason = v.amend_reason
			FROM deadlines d JOIN incident_versions v ON v.incident_id = d.incident_id AND v.version = d.closed_in_version
			WHERE d.id = $1`, dlID(1)).Scan(&ok)
		if err != nil || !ok {
			t.Errorf("closing details don't match version 2: %v, err %v", ok, err)
		}
	})

	t.Run("a closed deadline never changes", func(t *testing.T) {
		refused(t, tx, "23514", "is superseded; it never changes again", closeSQL, dlID(1), "withdrawn", nil, 2)
		refused(t, tx, "42501", "", `UPDATE deadlines SET due_at = now() WHERE id = $1`, dlID(3))
		refused(t, tx, "42501", "", `DELETE FROM deadlines WHERE id = $1`, dlID(3))
	})

	t.Run("a replacement must exist and be the same duty", func(t *testing.T) {
		okBatch(t, tx, duty(dutyID(4), inc, gar92), dl(dlID(4), dutyID(4), inc, gar92, "investigation", 7, "calendar_days", "-2 days", 2))
		v3 := amend(inc, 3, rlsUserA, "-2 days")
		refusedBatch(t, tx, "23503", "deadlines_superseded_by_fkey", v3, closeDl(dlID(3), "superseded", dlID(99), 3), stmt{checkNow, nil})
		refusedBatch(t, tx, "23503", "deadlines_superseded_by_fkey", v3, closeDl(dlID(3), "superseded", dlID(4), 3), stmt{checkNow, nil})
		refusedBatch(t, tx, "23514", "deadlines_closed", v3, closeDl(dlID(3), "superseded", nil, 3))
		refusedBatch(t, tx, "23514", "deadlines_closed", v3, closeDl(dlID(3), "withdrawn", dlID(4), 3))
	})

	t.Run("satisfactions", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "record the satisfaction against the live deadline", satisfy(satID(1), dutyID(1), dlID(1), "-1 hours", rlsUserA))
		refusedBatch(t, tx, "23514", "satisfied_at is in the future", satisfy(satID(1), dutyID(1), dlID(3), "1 hour", rlsUserA))
		refusedBatch(t, tx, "23514", "before the deadline's basis", satisfy(satID(1), dutyID(1), dlID(3), "-3 days", rlsUserA))
		refusedBatch(t, tx, "23503", "", satisfy(satID(1), dutyID(4), dlID(3), "-1 hours", rlsUserA))
		okBatch(t, tx, satisfy(satID(1), dutyID(1), dlID(3), "-1 hours", supervisor))
		refusedBatch(t, tx, "23514", "already satisfied", satisfy(satID(2), dutyID(1), dlID(3), "-30 minutes", rlsUserA))
		refused(t, tx, "42501", "", `UPDATE deadline_satisfactions SET method = 'email' WHERE id = $1`, satID(1))
		refused(t, tx, "42501", "", `DELETE FROM deadline_satisfactions WHERE id = $1`, satID(1))
		// The duty can't be cleared or swapped by hand.
		refused(t, tx, "23514", "cleared only by retracting", `UPDATE deadline_duties SET satisfaction_id = NULL WHERE id = $1`, dutyID(1))
		refused(t, tx, "42501", "", `UPDATE deadline_duties SET rule_key = 'x' WHERE id = $1`, dutyID(1))
	})

	t.Run("retracting reopens the duty; the satisfaction stays", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "only an owner or a SHE officer can retract", retract(satID(1), dutyID(1), supervisor))
		okBatch(t, tx, retract(satID(1), dutyID(1), rlsUserA))
		var open bool
		var kept int
		if err := tx.QueryRow(ctx, `SELECT satisfaction_id IS NULL, (SELECT count(*) FROM deadline_satisfactions WHERE id = $2)
			FROM deadline_duties WHERE id = $1`, dutyID(1), satID(1)).Scan(&open, &kept); err != nil || !open || kept != 1 {
			t.Errorf("duty open %v, satisfaction kept %d, err %v; want open and kept", open, kept, err)
		}
		refusedBatch(t, tx, "23505", "deadline_satisfaction_retractions_pkey", retract(satID(1), dutyID(1), rlsUserA))
		refused(t, tx, "23514", "that satisfaction was retracted", `UPDATE deadline_duties SET satisfaction_id = $2 WHERE id = $1`, dutyID(1), satID(1))
		refused(t, tx, "42501", "", `DELETE FROM deadline_satisfaction_retractions WHERE satisfaction_id = $1`, satID(1))
		okBatch(t, tx, satisfy(satID(2), dutyID(1), dlID(3), "-30 minutes", rlsUserA))
	})

	t.Run("a row added by a satisfaction names one of its incident's, from the same transaction", func(t *testing.T) {
		const msg = "added by a satisfaction of this incident recorded in the same transaction only"
		const seededSatA = "32000000-0000-0000-0000-00000000000a" // on rlsIncA, recorded by seedRLS
		if err := batch(ctx, tx, false, duty(dutyID(9), inc, gar82), addedDl(dlID(9), dutyID(9), inc, gar82, 2, satID(2))); err != nil {
			t.Errorf("added by satID(2), in this transaction: %v", err)
		}
		refusedBatch(t, tx, "23514", msg, duty(dutyID(9), rlsIncA, gar82), addedDl(dlID(9), dutyID(9), rlsIncA, gar82, 1, satID(2)))
		refusedBatch(t, tx, "23514", msg, duty(dutyID(9), rlsIncA, gar82), addedDl(dlID(9), dutyID(9), rlsIncA, gar82, 1, seededSatA))
		refused(t, tx, "42501", "", `UPDATE deadlines SET added_by_satisfaction_id = $2 WHERE id = $1`, dlID(3), satID(2))
	})

	t.Run("a satisfaction carries over a recomputation", func(t *testing.T) {
		okBatch(t, tx, amend(inc, 3, rlsUserA, "-3 days"),
			closeDl(dlID(3), "superseded", dlID(5), 3),
			dl(dlID(5), dutyID(1), inc, gar8a, "report", 7, "calendar_days", "-3 days", 3),
			closeDl(dlID(4), "superseded", dlID(6), 3),
			dl(dlID(6), dutyID(4), inc, gar92, "investigation", 7, "calendar_days", "-3 days", 3),
			stmt{checkNow, nil})
		var sat string
		if err := tx.QueryRow(ctx, `SELECT satisfaction_id FROM deadline_duties WHERE id = $1`, dutyID(1)).Scan(&sat); err != nil || sat != satID(2) {
			t.Errorf("duty's satisfaction %q, err %v; want %s", sat, err, satID(2))
		}
	})

	t.Run("weakening a report clock takes an owner or SHE officer", func(t *testing.T) {
		// Version 4 by the supervisor; each case is rolled back.
		v4 := amend(inc, 4, supervisor, "-3 days")
		later := amend(inc, 4, supervisor, "-1 days")
		const msg = "only an owner or a SHE officer can withdraw a report deadline or move it later"

		okBatch(t, tx, duty(dutyID(7), inc, gar8b), dl(dlID(7), dutyID(7), inc, gar8b, "report", 0, "immediately", "-3 days", 3))
		// Withdrawing an open report deadline.
		refusedBatch(t, tx, "23514", msg, v4, closeDl(dlID(7), "withdrawn", nil, 4), stmt{checkNow, nil})
		// Moving one later, open or satisfied (dlID(5) is satisfied).
		refusedBatch(t, tx, "23514", msg, later, closeDl(dlID(7), "superseded", dlID(8), 4),
			dl(dlID(8), dutyID(7), inc, gar8b, "report", 0, "immediately", "-1 days", 4), stmt{checkNow, nil})
		refusedBatch(t, tx, "23514", msg, later, closeDl(dlID(5), "superseded", dlID(8), 4),
			dl(dlID(8), dutyID(1), inc, gar8a, "report", 7, "calendar_days", "-1 days", 4), stmt{checkNow, nil})
		// Not weakening: earlier, a satisfied withdrawal, or not a report.
		if err := batch(ctx, tx, false, amend(inc, 4, supervisor, "-4 days"), closeDl(dlID(7), "superseded", dlID(8), 4),
			dl(dlID(8), dutyID(7), inc, gar8b, "report", 0, "immediately", "-4 days", 4), stmt{checkNow, nil}); err != nil {
			t.Errorf("earlier, by a supervisor: %v", err)
		}
		if err := batch(ctx, tx, false, v4, closeDl(dlID(5), "withdrawn", nil, 4), stmt{checkNow, nil}); err != nil {
			t.Errorf("withdrawing a satisfied report, by a supervisor: %v", err)
		}
		if err := batch(ctx, tx, false, v4, closeDl(dlID(6), "withdrawn", nil, 4), stmt{checkNow, nil}); err != nil {
			t.Errorf("withdrawing an investigation, by a supervisor: %v", err)
		}
		// The owner may weaken.
		if err := batch(ctx, tx, false, amend(inc, 4, rlsUserA, "-3 days"), closeDl(dlID(7), "withdrawn", nil, 4), stmt{checkNow, nil}); err != nil {
			t.Errorf("withdrawing an open report, by the owner: %v", err)
		}
		// Only the latest version closes a row, not just any later one.
		refusedBatch(t, tx, "23514", "closed only by the incident's latest version",
			amend(inc, 4, rlsUserA, "-3 days"), amend(inc, 5, rlsUserA, "-3 days"), closeDl(dlID(7), "withdrawn", nil, 4))
		// A replacement must come from the version that closed the row.
		refusedBatch(t, tx, "23514", "its replacement must be computed by the version that superseded it",
			amend(inc, 4, rlsUserA, "-3 days"), closeDl(dlID(7), "superseded", dlID(8), 4),
			amend(inc, 5, rlsUserA, "-3 days"), dl(dlID(8), dutyID(7), inc, gar8b, "report", 0, "immediately", "-3 days", 5), stmt{checkNow, nil})
	})

	t.Run("deadlines a new holiday could move", func(t *testing.T) {
		rows, err := tx.Query(ctx, `SELECT id::text FROM deadlines_affected_by_day('ZA', (now() AT TIME ZONE 'Africa/Johannesburg')::date + 1)
		                            WHERE incident_id = $1 ORDER BY id`, inc)
		if err != nil {
			t.Fatal(err)
		}
		got, err := pgx.CollectRows(rows, pgx.RowTo[string])
		// Live, day-based and covering tomorrow: dlID(5) and dlID(6), due in
		// four days. dlID(7) is immediate; the closed ones don't count.
		if err != nil || strings.Join(got, ",") != dlID(5)+","+dlID(6) {
			t.Errorf("affected %v, err %v; want [%s %s]", got, err, dlID(5), dlID(6))
		}
	})
}

// The guards hold for the superuser too.
func TestDeadlinesFrozenForEveryRole(t *testing.T) {
	seedRLS(t)
	tx := beginAs(t, admin, "")
	for _, s := range []struct{ sql, msg string }{
		{`UPDATE deadlines SET due_at = now() WHERE id = $1`, "the only change allowed is closing it"},
		{`UPDATE deadlines SET added_by_satisfaction_id = '32000000-0000-0000-0000-00000000000a' WHERE id = $1`, "the only change allowed is closing it"},
		{`DELETE FROM deadlines WHERE id = $1`, "never deleted"},
	} {
		refused(t, tx, "23514", s.msg, s.sql, rlsDeadA)
	}
	refused(t, tx, "23514", "only its satisfaction changes", `UPDATE deadline_duties SET rule_key = 'x' WHERE id = $1`, rlsDutyA)
	refused(t, tx, "23514", "never deleted", `DELETE FROM deadline_duties WHERE id = $1`, rlsDutyA)
	refused(t, tx, "23514", "append-only", `UPDATE deadline_satisfactions SET method = 'email' WHERE duty_id = $1`, rlsDutyA)
	refused(t, tx, "23514", "append-only", `DELETE FROM deadline_satisfaction_retractions WHERE duty_id = $1`, rlsDutyA)
	refuseTruncates(t, tx, "deadline_duties", "deadlines", "deadline_satisfactions", "deadline_satisfaction_retractions")

	// One live satisfaction per duty is held twice; each layer alone. rlsDutyA
	// is open (its satisfaction was retracted).
	const later = "2026-11-10T11:00:00Z"
	sat := func(id string) stmt {
		return stmt{`INSERT INTO deadline_satisfactions (id, org_id, site_id, duty_id, deadline_id, satisfied_at, method, recorded_by)
			VALUES ($1, $2, $3, $4, $5, $6, 'phone', $7)`, []any{id, rlsOrgA, rlsSiteA, rlsDutyA, rlsDeadA, later, rlsUserA}}
	}
	okBatch(t, tx, sat(satID(50)))
	// The guard: with the satisfaction's own trigger off, setting a second
	// one on the duty by hand is refused.
	refusedBatch(t, tx, "23514", "already satisfied; retract that satisfaction first",
		stmt{`ALTER TABLE deadline_satisfactions DISABLE TRIGGER deadline_satisfactions_take`, nil}, sat(satID(51)),
		stmt{`UPDATE deadline_duties SET satisfaction_id = $2 WHERE id = $1`, []any{rlsDutyA, satID(51)}})
	// The satisfaction's trigger: with the guard off, a second one is refused.
	refusedBatch(t, tx, "23514", "already satisfied; retract that satisfaction first",
		stmt{`ALTER TABLE deadline_duties DISABLE TRIGGER deadline_duties_guard`, nil}, sat(satID(52)))
}

// committedDuty creates, in org C, an incident (version 1) with one live
// GAR 8(1)(b) deadline, committed.
func committedDuty(t *testing.T, inc, duty, deadline string) {
	t.Helper()
	committedIncident(t, inc)
	ctx := context.Background()
	err := database.WithTenant(ctx, pool, database.Tenant{OrgID: orgC}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, dutySQL, duty, orgC, siteC, inc, gar8b); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, deadlineSQL, deadline, orgC, siteC, duty, inc, gar8b, "report", 0, "immediately", time.Now().Add(-2*time.Hour), 1)
		return err
	})
	if err != nil {
		t.Fatalf("committed duty: %v", err)
	}
}

func satisfyC(ctx context.Context, tx pgx.Tx, id, duty, deadline string) error {
	_, err := tx.Exec(ctx, satisfySQL, id, orgC, siteC, duty, deadline, "-1 hours", userC)
	return err
}

// Two satisfactions of one duty: the second waits for the first, then fails.
func TestSatisfactionsQueue(t *testing.T) {
	ctx := context.Background()
	inc, duty, deadline := incID(42), dutyID(42), dlID(42)
	committedDuty(t, inc, duty, deadline)

	first := tenantTx(t)
	defer first.Rollback(ctx)
	if err := satisfyC(ctx, first, satID(42), duty, deadline); err != nil {
		t.Fatalf("first satisfaction: %v", err)
	}

	second := make(chan error, 1)
	go func() {
		second <- database.WithTenant(ctx, pool, database.Tenant{OrgID: orgC}, func(tx pgx.Tx) error {
			return satisfyC(ctx, tx, satID(43), duty, deadline)
		})
	}()
	waitingOn(t, "INSERT INTO deadline_satisfactions")

	if err := first.Commit(ctx); err != nil {
		t.Fatalf("commit first: %v", err)
	}
	if err := <-second; pgCode(err) != "23514" || !strings.Contains(err.Error(), "already satisfied") {
		t.Fatalf("second satisfaction: %v, want SQLSTATE 23514 (already satisfied)", err)
	}
}

// A satisfaction arriving while an amendment closes its deadline waits for
// the amendment, then fails: it would otherwise land on a closed row.
func TestSatisfactionWaitsForClosing(t *testing.T) {
	ctx := context.Background()
	inc, duty, deadline := incID(44), dutyID(44), dlID(44)
	committedDuty(t, inc, duty, deadline)

	closing := tenantTx(t)
	defer closing.Rollback(ctx)
	for _, s := range []stmt{
		{versionSQL, []any{orgC, siteC, inc, 2, nearMiss, ago, nil, userC, "Not reportable after all", 0, 0}},
		{closeSQL, []any{deadline, "withdrawn", nil, 2}},
	} {
		if _, err := closing.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("close: %v", err)
		}
	}

	satisfied := make(chan error, 1)
	go func() {
		satisfied <- database.WithTenant(ctx, pool, database.Tenant{OrgID: orgC}, func(tx pgx.Tx) error {
			return satisfyC(ctx, tx, satID(44), duty, deadline)
		})
	}()
	waitingOn(t, "INSERT INTO deadline_satisfactions")

	if err := closing.Commit(ctx); err != nil {
		t.Fatalf("commit closing: %v", err)
	}
	if err := <-satisfied; pgCode(err) != "23514" || !strings.Contains(err.Error(), "is withdrawn; record the satisfaction against the live deadline") {
		t.Fatalf("satisfaction after the close: %v, want SQLSTATE 23514 (withdrawn)", err)
	}
}
