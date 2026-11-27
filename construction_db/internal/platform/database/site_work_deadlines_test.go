package database_test

import (
	"context"
	"fmt"
	"testing"
	"time"
)

const (
	reg4Rule = "cr2014_reg4_notification"
	reg3Rule = "cr2014_reg3_permit_application"
	// $1 id, $2 duty, $3 deadline, $4 satisfied_at offset, $5 recorder, $6 document, $7 version.
	satisfyDocSQL = `
		INSERT INTO deadline_satisfactions (id, org_id, site_id, duty_id, deadline_id, satisfied_at, method, recorded_by, document_id, document_version)
		VALUES ($1, '20000000-0000-0000-0000-00000000000a', '22000000-0000-0000-0000-00000000000a', $2, $3, now() + $4::interval, 'email', $5, $6, $7)`
)

var proof4 = []string{"notification_of_construction_work"}

func swDuty(id, work, rule string) stmt {
	return stmt{`INSERT INTO deadline_duties (id, org_id, site_id, site_work_id, rule_key) VALUES ($1, $2, $3, $4, $5)`,
		[]any{id, rlsOrgA, rlsSiteA, work, rule}}
}

// A before-deadline on site work, from starts (a date) at local midnight.
func swDl(id, duty, work, rule, unit string, period int, starts string, version int, proof []string) stmt {
	return stmt{`
		INSERT INTO deadlines (id, org_id, site_id, duty_id, site_work_id, regime_id, rule_key, rule_label, kind, period, unit,
		                       direction, basis, source_ref, proof_document_types, basis_at, time_zone, jurisdiction, due_at,
		                       excluded_days, computed_in_version)
		SELECT $1, $2, $3, $4, $5, id, $6, 'A rule', 'notification', $8, $7, 'before', 'work_starts_at', 'Test ref', $11,
		       $9::date::timestamp AT TIME ZONE 'Africa/Johannesburg', 'Africa/Johannesburg', 'ZA', '2000-01-01', '{}', $10
		FROM regulatory_regimes LIMIT 1`, []any{id, rlsOrgA, rlsSiteA, duty, work, rule, unit, period, starts, version, proof}}
}

func swVersion(work string, n int, starts, status, by string) stmt {
	return stmt{workVersionSQL, []any{rlsOrgA, rlsSiteA, work, n, starts, dig, status, by, "0 minutes", fmt.Sprintf("Amendment %d", n)}}
}

func TestSiteWorkDeadlineRules(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()
	tx := beginAs(t, pool, rlsOrgA)
	const (
		supervisor = "21000000-0000-0000-0000-0000000000a6"
		otherSite  = "22000000-0000-0000-0000-0000000000f3"
	)
	must(t, tx, 1, `INSERT INTO users (id, email, full_name) VALUES ($1, 'sup-sw@rls.test', 'Supervisor SW')`, supervisor)
	seedMember(t, tx, rlsOrgA, supervisor, "supervisor", "active")
	must(t, tx, 1, `INSERT INTO sites (id, org_id, regime_id, name) SELECT $1, $2, id, 'Third site' FROM regulatory_regimes LIMIT 1`, otherSite, rlsOrgA)

	// Excavation from Mon 7 Dec 2026, version 1 by the owner.
	work, other := workID(30), workID(31)
	okBatch(t, tx, workHeader(work), wv(work, 1, "2026-12-07", dig, "planned", nil),
		workHeader(other), wv(other, 1, "2026-12-07", dig, "planned", nil), stmt{checkNow, nil})
	okBatch(t, tx, swDuty(dutyID(30), work, reg4Rule), swDl(dlID(30), dutyID(30), work, reg4Rule, "clear_days", 7, "2026-12-07", 1, proof4))

	t.Run("due 7 clear days before the start", func(t *testing.T) {
		var on string
		var at time.Time
		if err := tx.QueryRow(ctx, `SELECT due_on::text, due_at FROM deadlines WHERE id = $1`, dlID(30)).Scan(&on, &at); err != nil {
			t.Fatal(err)
		}
		if on != "2026-11-29" || !at.Equal(time.Date(2026, 11, 29, 22, 0, 0, 0, time.UTC)) {
			t.Errorf("due_on %s, due_at %s; want end of Sun 29 Nov (30 Nov to 6 Dec clear)", on, at)
		}
	})

	t.Run("exactly one subject", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "deadline_duties_one_subject", stmt{
			`INSERT INTO deadline_duties (id, org_id, site_id, incident_id, site_work_id, rule_key) VALUES ($1, $2, $3, $4, $5, $6)`,
			[]any{dutyID(31), rlsOrgA, rlsSiteA, rlsIncA, work, reg3Rule}})
		refusedBatch(t, tx, "23514", "deadline_duties_one_subject", stmt{
			`INSERT INTO deadline_duties (id, org_id, site_id, rule_key) VALUES ($1, $2, $3, $4)`, []any{dutyID(31), rlsOrgA, rlsSiteA, reg3Rule}})
		refusedBatch(t, tx, "23505", "deadline_duties_once_site_work", swDuty(dutyID(31), work, reg4Rule))
	})

	t.Run("a deadline is its duty's subject's", func(t *testing.T) {
		okBatch(t, tx, swDuty(dutyID(32), work, reg3Rule))
		refusedBatch(t, tx, "23503", "deadlines_site_work_duty_fkey", swDl(dlID(31), dutyID(32), other, reg3Rule, "clear_days", 30, "2026-12-07", 1, []string{}))
		refusedBatch(t, tx, "23503", "deadlines_site_work_duty_fkey", swDl(dlID(31), dutyID(32), work, reg4Rule, "clear_days", 30, "2026-12-07", 1, []string{}))
		// An incident's duty can't take a site work row, nor the other way round.
		refusedBatch(t, tx, "23503", "deadlines_site_work_duty_fkey", duty(dutyID(34), rlsIncA, "test_inc_rule"),
			swDl(dlID(31), dutyID(34), work, "test_inc_rule", "clear_days", 7, "2026-12-07", 1, []string{}))
		refusedBatch(t, tx, "23514", "deadlines_one_subject", stmt{`
			INSERT INTO deadlines (id, org_id, site_id, duty_id, incident_id, site_work_id, regime_id, rule_key, rule_label, kind, period, unit,
			                       direction, basis, source_ref, basis_at, time_zone, jurisdiction, due_at, excluded_days, computed_in_version)
			SELECT $1, $2, $3, $4, $5, $6, id, $7, 'A', 'notification', 30, 'clear_days', 'before', 'work_starts_at', 'r',
			       '2026-12-07'::timestamp AT TIME ZONE 'Africa/Johannesburg', 'Africa/Johannesburg', 'ZA', now(), '{}', 1
			FROM regulatory_regimes LIMIT 1`, []any{dlID(31), rlsOrgA, rlsSiteA, dutyID(32), rlsIncA, work, reg3Rule}})
	})

	t.Run("it runs from its version's start", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "basis_at must be the start of starts_on (2026-12-07)",
			swDl(dlID(31), dutyID(32), work, reg3Rule, "clear_days", 30, "2026-12-08", 1, []string{}))
		refusedBatch(t, tx, "23514", "computed in version 2 but the site work is at version 1",
			swDl(dlID(31), dutyID(32), work, reg3Rule, "clear_days", 30, "2026-12-07", 2, []string{}))
		refusedBatch(t, tx, "23514", "a site work deadline runs from work_starts_at", stmt{`
			INSERT INTO deadlines (id, org_id, site_id, duty_id, site_work_id, regime_id, rule_key, rule_label, kind, period, unit,
			                       direction, basis, source_ref, basis_at, time_zone, jurisdiction, due_at, excluded_days, computed_in_version)
			SELECT $1, $2, $3, $4, $5, id, $6, 'A', 'notification', 30, 'clear_days', 'before', 'incident_occurred_at', 'r',
			       '2026-12-07'::timestamp AT TIME ZONE 'Africa/Johannesburg', 'Africa/Johannesburg', 'ZA', now(), '{}', 1
			FROM regulatory_regimes LIMIT 1`, []any{dlID(31), rlsOrgA, rlsSiteA, dutyID(32), work, reg3Rule}})
	})

	t.Run("closing needs the latest version", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "closed only by the site work's latest version", closeDl(dlID(30), "withdrawn", nil, 1))
	})

	t.Run("weakening a notification takes an owner or SHE officer", func(t *testing.T) {
		const msg = "only an owner or a SHE officer can withdraw a notification deadline or move it later"
		later := func(by string) []stmt {
			return []stmt{swVersion(work, 2, "2026-12-14", "planned", by), closeDl(dlID(30), "superseded", dlID(33), 2),
				swDl(dlID(33), dutyID(30), work, reg4Rule, "clear_days", 7, "2026-12-14", 2, proof4), {checkNow, nil}}
		}
		refusedBatch(t, tx, "23514", msg, later(supervisor)...)
		refusedBatch(t, tx, "23514", msg, swVersion(work, 2, "2026-12-07", "cancelled", supervisor), closeDl(dlID(30), "withdrawn", nil, 2), stmt{checkNow, nil})
		// An earlier start is stricter, so a supervisor may record it.
		if err := batch(ctx, tx, false, swVersion(work, 2, "2026-12-03", "planned", supervisor), closeDl(dlID(30), "superseded", dlID(33), 2),
			swDl(dlID(33), dutyID(30), work, reg4Rule, "clear_days", 7, "2026-12-03", 2, proof4), stmt{checkNow, nil}); err != nil {
			t.Errorf("earlier start by a supervisor: %v", err)
		}
		okBatch(t, tx, later(rlsUserA)...)
		var ok bool
		err := tx.QueryRow(ctx, `
			SELECT d.closed_by = v.submitted_by AND d.closed_at = v.submitted_at AND d.closed_reason = v.amend_reason
			FROM deadlines d JOIN site_work_versions v ON v.site_work_id = d.site_work_id AND v.version = d.closed_in_version
			WHERE d.id = $1`, dlID(30)).Scan(&ok)
		if err != nil || !ok {
			t.Errorf("closing details don't match the site work's version 2: %v, err %v", ok, err)
		}
	})

	t.Run("a notice is met before its basis, never in the future", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "satisfied_at is in the future", satisfy(satID(30), dutyID(30), dlID(33), "1 hour", rlsUserA))
		if err := batch(ctx, tx, false, satisfy(satID(30), dutyID(30), dlID(33), "-60 days", supervisor)); err != nil {
			t.Errorf("a notice sent long before the start: %v", err)
		}
	})

	t.Run("proof is a site document of the rule's type", func(t *testing.T) {
		const (
			fileNotice, fileRisk, fileOther = "40000000-0000-0000-0000-0000000000f1", "40000000-0000-0000-0000-0000000000f2", "40000000-0000-0000-0000-0000000000f3"
			docNotice, docRisk, docOther    = "46000000-0000-0000-0000-0000000000f1", "46000000-0000-0000-0000-0000000000f2", "46000000-0000-0000-0000-0000000000f3"
		)
		okBatch(t, tx,
			stmt{fileSQL, []any{fileNotice, rlsOrgA, rlsSiteA, "site_document", rlsUserA, hash32(41), "verified"}},
			stmt{fileSQL, []any{fileRisk, rlsOrgA, rlsSiteA, "site_document", rlsUserA, hash32(42), "verified"}},
			stmt{fileSQL, []any{fileOther, rlsOrgA, otherSite, "site_document", rlsUserA, hash32(43), "verified"}},
			stmt{docSQL, []any{docNotice, rlsOrgA, rlsSiteA, "site", "notification_of_construction_work", rlsUserA}},
			stmt{docSQL, []any{docRisk, rlsOrgA, rlsSiteA, "site", "risk_assessment", rlsUserA}},
			stmt{docSQL, []any{docOther, rlsOrgA, otherSite, "site", "notification_of_construction_work", rlsUserA}},
			stmt{docVersionSQL, []any{rlsOrgA, rlsSiteA, docNotice, 1, "site", fileNotice, rlsUserA}},
			stmt{docVersionSQL, []any{rlsOrgA, rlsSiteA, docRisk, 1, "site", fileRisk, rlsUserA}},
			stmt{docVersionSQL, []any{rlsOrgA, otherSite, docOther, 1, "site", fileOther, rlsUserA}})
		sat := func(doc, version any) stmt {
			return stmt{satisfyDocSQL, []any{satID(31), dutyID(30), dlID(33), "-1 days", rlsUserA, doc, version}}
		}
		refusedBatch(t, tx, "23514", "a risk_assessment document is not proof of this deadline", sat(docRisk, 1))
		refusedBatch(t, tx, "23503", "deadline_satisfactions_document_fkey", sat(docOther, 1))
		refusedBatch(t, tx, "23503", "deadline_satisfactions_document_version_fkey", sat(docNotice, 2))
		refusedBatch(t, tx, "23514", "deadline_satisfactions_document", sat(docNotice, nil))
		refusedBatch(t, tx, "23514", "that document version is retracted",
			stmt{`UPDATE documents SET retracted_at = now(), retracted_by = $2, retract_reason = 'Wrong file' WHERE id = $1`, []any{docNotice, rlsUserA}},
			sat(docNotice, 1))
		okBatch(t, tx, sat(docNotice, 1))
		refused(t, tx, "42501", "", `UPDATE deadline_satisfactions SET document_id = NULL WHERE id = $1`, satID(31))
	})
}

// A new holiday never moves a clear-days notice; a site work row it does move
// (a working-days rule here) is replaced in its site work's latest version.
func TestSiteWorkHolidayRecompute(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()
	tx := beginAs(t, admin, "")
	// rlsWorkA starts Tue 1 Dec 2026; 10 working days before is Tue 17 Nov.
	okBatch(t, tx, swDuty(dutyID(40), rlsWorkA, reg4Rule),
		swDl(dlID(40), dutyID(40), rlsWorkA, reg4Rule, "clear_days", 7, "2026-12-01", 1, proof4),
		swDuty(dutyID(41), rlsWorkA, "test_sw_wd"),
		swDl(dlID(41), dutyID(41), rlsWorkA, "test_sw_wd", "working_days", 10, "2026-12-01", 1, proof4))
	const day = "2026-11-25"
	must(t, tx, 1, `INSERT INTO public_holidays (jurisdiction, holiday_date, name, kind, source_ref) VALUES ('ZA', $1, 'Test Day', 'proclaimed', 'Test proclamation')`, day)
	okBatch(t, tx, stmt{`SET LOCAL ROLE siteproof_app`, nil}, stmt{`SELECT set_config('app.org_id', $1, true)`, []any{rlsOrgA}})

	var n *int
	if err := tx.QueryRow(ctx, `SELECT deadlines_recompute('ZA', $1)`, day).Scan(&n); err != nil || n == nil || *n != 1 {
		t.Fatalf("recompute: %v rows, err %v; want 1", n, err)
	}
	var closed *string
	if err := tx.QueryRow(ctx, `SELECT closed_as FROM deadlines WHERE id = $1`, dlID(40)).Scan(&closed); err != nil || closed != nil {
		t.Errorf("clear-days notice closed_as %v, err %v; want live", closed, err)
	}
	var work, on string
	var version int
	var proof []string
	err := tx.QueryRow(ctx, `
		SELECT n.site_work_id::text, n.computed_in_version, n.due_on::text, n.proof_document_types
		FROM deadlines o JOIN deadlines n ON n.id = o.superseded_by WHERE o.id = $1 AND o.closed_as = 'recomputed'`, dlID(41)).
		Scan(&work, &version, &on, &proof)
	if err != nil || work != rlsWorkA || version != 1 || on != "2026-11-16" || len(proof) != 1 {
		t.Errorf("replacement: site work %s, version %d, due %s, proof %v, err %v; want %s, 1, 2026-11-16, the proof types",
			work, version, on, proof, err, rlsWorkA)
	}
}
