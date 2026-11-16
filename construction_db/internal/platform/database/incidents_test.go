package database_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"construction_db/internal/platform/database"
)

// Incident and statement ids used inside rolled-back transactions; "2e"/"2f"
// as in the RLS fixtures, but with decimal suffixes so they never collide.
func incID(n int) string  { return fmt.Sprintf("2e000000-0000-0000-0000-%012d", n) }
func stmtID(n int) string { return fmt.Sprintf("2f000000-0000-0000-0000-%012d", n) }

const (
	incidentSQL = `
		INSERT INTO incidents (id, org_id, site_id, regime_id, reported_by)
		SELECT $1, $2, $3, id, $4 FROM regulatory_regimes LIMIT 1`
	// $5 codes, $6 occurred_at offset, $7 employer_aware_at offset (both
	// intervals from now; null for none), $9 amend_reason.
	versionSQL = `
		INSERT INTO incident_versions (org_id, site_id, incident_id, version, taxonomy_codes, occurred_at, employer_aware_at,
		                               submitted_by, client_submitted_at, amend_reason, people_count, evidence_count)
		VALUES ($1, $2, $3, $4, $5, now() + $6::interval, now() + $7::interval, $8, now(), $9, $10, $11)`
	personSQL = `
		INSERT INTO incident_people (org_id, site_id, incident_id, version, position, employee_of_org, injured, treatment, died_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	incPhotoSQL = `
		INSERT INTO incident_version_evidence (org_id, site_id, incident_id, version, evidence_id) VALUES ($1, $2, $3, $4, $5)`
	statementSQL = `
		INSERT INTO witness_statements (id, org_id, site_id, incident_id, witness_name, statement, taken_by, taken_at, evidence_id, supersedes_id)
		VALUES ($1, $2, $3, $4, 'A witness', 'What I saw.', $5, now() + $6::interval, $7, $8)`
	checkNow = `SET CONSTRAINTS ALL IMMEDIATE`
)

type stmt struct {
	sql  string
	args []any
}

// batch runs statements in one savepoint and returns the first error. keep
// keeps them; otherwise they are rolled back either way. SET CONSTRAINTS ALL
// IMMEDIATE (checkNow) as the last statement fires the deferred checks a
// commit would. The mode outlives a savepoint rollback, so it is reset after.
func batch(ctx context.Context, tx pgx.Tx, keep bool, stmts ...stmt) error {
	defer tx.Exec(ctx, `SET CONSTRAINTS ALL DEFERRED`)
	sp, err := tx.Begin(ctx)
	if err != nil {
		return err
	}
	for _, s := range stmts {
		if _, err := sp.Exec(ctx, s.sql, s.args...); err != nil {
			_ = sp.Rollback(ctx)
			return err
		}
	}
	if !keep {
		return sp.Rollback(ctx)
	}
	return sp.Commit(ctx)
}

func refusedBatch(t *testing.T, tx pgx.Tx, code, msg string, stmts ...stmt) {
	t.Helper()
	err := batch(context.Background(), tx, false, stmts...)
	if pgCode(err) != code || (msg != "" && (err == nil || !strings.Contains(err.Error(), msg))) {
		t.Errorf("got %v\n  want SQLSTATE %s %q", err, code, msg)
	}
}

func okBatch(t *testing.T, tx pgx.Tx, stmts ...stmt) {
	t.Helper()
	if err := batch(context.Background(), tx, true, stmts...); err != nil {
		t.Fatalf("batch: %v", err)
	}
}

// v builds a version insert for org A, site A, user A.
func v(inc string, n int, codes []string, occurred, aware, reason any, people, photos int) stmt {
	return stmt{versionSQL, []any{rlsOrgA, rlsSiteA, inc, n, codes, occurred, aware, rlsUserA, reason, people, photos}}
}

func header(inc string) stmt { return stmt{incidentSQL, []any{inc, rlsOrgA, rlsSiteA, rlsUserA}} }

func person(inc string, n, pos int, employee, injured bool, treatment, died any) stmt {
	return stmt{personSQL, []any{rlsOrgA, rlsSiteA, inc, n, pos, employee, injured, treatment, died}}
}

var (
	nearMiss = []string{"near_miss"}
	ago      = "-1 hour"
)

// The rules the database enforces on incidents, as the app role.
func TestIncidentRules(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()
	tx := beginAs(t, pool, rlsOrgA)

	const (
		otherSite = "22000000-0000-0000-0000-0000000000e2"
		pendingEv = "2d000000-0000-0000-0000-0000000000e1"
		otherEv   = "2d000000-0000-0000-0000-0000000000e2"
		secondEv  = "2d000000-0000-0000-0000-0000000000e3"
	)
	must(t, tx, 1, `INSERT INTO sites (id, org_id, regime_id, name) SELECT $1, $2, id, 'Second site' FROM regulatory_regimes LIMIT 1`, otherSite, rlsOrgA)
	must(t, tx, 1, evidenceSQL, pendingEv, rlsOrgA, rlsSiteA, rlsUserA, hash32(16), "inc/pending", nil, "pending")
	must(t, tx, 1, evidenceSQL, otherEv, rlsOrgA, otherSite, rlsUserA, hash32(17), "inc/other-site", "v1", "verified")
	must(t, tx, 1, evidenceSQL, secondEv, rlsOrgA, rlsSiteA, rlsUserA, hash32(18), "inc/second", "v1", "verified")

	t.Run("an incident is created by its version 1", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "has no version", header(incID(1)), stmt{checkNow, nil})
		refusedBatch(t, tx, "23514", "version 2 is not the next version",
			header(incID(1)), v(incID(1), 2, nearMiss, ago, nil, "Why", 0, 0))
		refusedBatch(t, tx, "23514", "a new incident has no versions yet",
			stmt{`INSERT INTO incidents (id, org_id, site_id, regime_id, reported_by, latest_version)
			      SELECT $1, $2, $3, id, $4, 1 FROM regulatory_regimes LIMIT 1`, []any{incID(1), rlsOrgA, rlsSiteA, rlsUserA}})
	})

	okBatch(t, tx, header(incID(2)), v(incID(2), 1, nearMiss, ago, nil, nil, 0, 0), stmt{checkNow, nil})

	t.Run("versions are numbered 1, 2, 3 with no gaps", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "version 3 is not the next version", v(incID(2), 3, nearMiss, ago, nil, "Why", 0, 0))
		refusedBatch(t, tx, "23505", "incident_versions_pkey", v(incID(2), 1, nearMiss, ago, nil, nil, 0, 0))
		okBatch(t, tx, v(incID(2), 2, []string{"first_aid"}, ago, nil, "Was first aid", 0, 0))
		var latest int
		if err := tx.QueryRow(ctx, `SELECT latest_version FROM incidents WHERE id = $1`, incID(2)).Scan(&latest); err != nil || latest != 2 {
			t.Errorf("latest_version %d, err %v; want 2", latest, err)
		}
	})

	t.Run("an amendment needs a reason, version 1 has none", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "incident_versions_amend_reason", v(incID(2), 3, nearMiss, ago, nil, nil, 0, 0))
		refusedBatch(t, tx, "23514", "incident_versions_amend_reason", header(incID(3)), v(incID(3), 1, nearMiss, ago, nil, "Why", 0, 0))
	})

	t.Run("taxonomy codes", func(t *testing.T) {
		for _, codes := range [][]string{{}, {"Fatality"}, {"near_miss", "near_miss"}, {"near-miss"}} {
			refusedBatch(t, tx, "23514", "incident_versions_taxonomy_codes_check", header(incID(3)), v(incID(3), 1, codes, ago, nil, nil, 0, 0))
		}
		okBatch(t, tx, header(incID(3)), v(incID(3), 1, []string{"pressure_release", "unconsciousness"}, ago, nil, nil, 0, 0))
	})

	t.Run("times", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "occurred_at is in the future", header(incID(4)), v(incID(4), 1, nearMiss, "1 hour", nil, nil, 0, 0))
		refusedBatch(t, tx, "23514", "incident_versions_aware_after_occurred", header(incID(4)), v(incID(4), 1, nearMiss, ago, "-2 hours", nil, 0, 0))
		refusedBatch(t, tx, "23514", "employer_aware_at is in the future", header(incID(4)), v(incID(4), 1, nearMiss, ago, "1 hour", nil, 0, 0))
		// Clock skew: 4 minutes ahead is accepted.
		okBatch(t, tx, header(incID(4)), v(incID(4), 1, nearMiss, "4 minutes", nil, nil, 0, 0))
	})

	t.Run("server times win over the client's", func(t *testing.T) {
		okBatch(t, tx, header(incID(10)), stmt{`
			INSERT INTO incident_versions (org_id, site_id, incident_id, version, taxonomy_codes, occurred_at, submitted_by,
			                               client_submitted_at, submitted_at, people_count, evidence_count)
			VALUES ($1, $2, $3, 1, $4, now(), $5, now(), '2000-01-01', 0, 0)`, []any{rlsOrgA, rlsSiteA, incID(10), nearMiss, rlsUserA}},
			stmt{`INSERT INTO witness_statements (id, org_id, site_id, incident_id, witness_name, statement, taken_by, taken_at, recorded_at)
			      VALUES ($1, $2, $3, $4, 'W', 'S', $5, now(), '2000-01-01')`, []any{stmtID(10), rlsOrgA, rlsSiteA, incID(10), rlsUserA}})
		var stale int
		err := tx.QueryRow(ctx, `
			SELECT (SELECT count(*) FROM incident_versions WHERE incident_id = $1 AND submitted_at < now() - interval '1 minute')
			     + (SELECT count(*) FROM witness_statements WHERE id = $2 AND recorded_at < now() - interval '1 minute')
			     + (SELECT count(*) FROM incidents WHERE id = $1 AND created_at < now() - interval '1 minute')`, incID(10), stmtID(10)).Scan(&stale)
		if err != nil || stale != 0 {
			t.Errorf("%d rows kept the client's time, err %v; want 0", stale, err)
		}
	})

	t.Run("people and photos are counted at commit", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "has 0 people but people_count is 1",
			header(incID(5)), v(incID(5), 1, nearMiss, ago, nil, nil, 1, 0), stmt{checkNow, nil})
		refusedBatch(t, tx, "23514", "has 0 photos but evidence_count is 1",
			header(incID(5)), v(incID(5), 1, nearMiss, ago, nil, nil, 0, 1), stmt{checkNow, nil})
		// A person added to the committed fixture version breaks its count.
		refusedBatch(t, tx, "23514", "has 2 people but people_count is 1",
			stmt{personSQL, []any{rlsOrgA, rlsSiteA, rlsIncA, 1, 2, false, false, nil, nil}}, stmt{checkNow, nil})
		refusedBatch(t, tx, "23514", "has 2 photos but evidence_count is 1",
			stmt{incPhotoSQL, []any{rlsOrgA, rlsSiteA, rlsIncA, 1, secondEv}}, stmt{checkNow, nil})
	})

	t.Run("an injured employee needs employer_aware_at", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "an injured employee needs employer_aware_at",
			header(incID(6)), v(incID(6), 1, []string{"first_aid"}, ago, nil, nil, 1, 0),
			person(incID(6), 1, 1, true, true, "first_aid", nil), stmt{checkNow, nil})
		// Not an employee of the organisation: no COIDA clock, so not needed.
		okBatch(t, tx, header(incID(6)), v(incID(6), 1, []string{"first_aid"}, ago, nil, nil, 1, 0),
			person(incID(6), 1, 1, false, true, "first_aid", nil), stmt{checkNow, nil})
	})

	t.Run("people", func(t *testing.T) {
		base := []stmt{header(incID(7)), v(incID(7), 1, []string{"fatality"}, ago, ago, nil, 1, 0)}
		refusedBatch(t, tx, "23514", "died_at is before the incident", append(base, stmt{personSQL,
			[]any{rlsOrgA, rlsSiteA, incID(7), 1, 1, true, true, nil, "2020-01-01T00:00:00Z"}})...)
		refusedBatch(t, tx, "23514", "incident_people_death_is_injury", append(base, stmt{`
			INSERT INTO incident_people (org_id, site_id, incident_id, version, position, employee_of_org, injured, died_at)
			VALUES ($1, $2, $3, 1, 1, true, false, now())`, []any{rlsOrgA, rlsSiteA, incID(7)}})...)
		refusedBatch(t, tx, "23514", "incident_people_injury_details", append(base, person(incID(7), 1, 1, true, false, "first_aid", nil))...)
		refusedBatch(t, tx, "23514", "incident_people_treatment_check", append(base, person(incID(7), 1, 1, true, true, "surgery", nil))...)
		refusedBatch(t, tx, "23514", "incident_people_employer", append(base, stmt{`
			INSERT INTO incident_people (org_id, site_id, incident_id, version, position, employee_of_org, employer_name, injured)
			VALUES ($1, $2, $3, 1, 1, true, 'Sub (Pty) Ltd', false)`, []any{rlsOrgA, rlsSiteA, incID(7)}})...)
	})

	t.Run("photos: only verified evidence from the same site", func(t *testing.T) {
		base := []stmt{header(incID(8)), v(incID(8), 1, nearMiss, ago, nil, nil, 0, 1)}
		refusedBatch(t, tx, "23503", "", append(base, stmt{incPhotoSQL, []any{rlsOrgA, rlsSiteA, incID(8), 1, pendingEv}})...)
		refusedBatch(t, tx, "23503", "", append(base, stmt{incPhotoSQL, []any{rlsOrgA, rlsSiteA, incID(8), 1, otherEv}})...)
		// The constant can't be overridden to reach a pending row.
		refusedBatch(t, tx, "23514", "evidence_status_check", append(base, stmt{`
			INSERT INTO incident_version_evidence (org_id, site_id, incident_id, version, evidence_id, evidence_status)
			VALUES ($1, $2, $3, 1, $4, 'pending')`, []any{rlsOrgA, rlsSiteA, incID(8), pendingEv}})...)
		refusedBatch(t, tx, "23514", "evidence_status_check", stmt{`
			INSERT INTO witness_statements (id, org_id, site_id, incident_id, witness_name, statement, taken_by, taken_at, evidence_id, evidence_status)
			VALUES ($1, $2, $3, $4, 'W', 'S', $5, now(), $6, 'pending')`, []any{stmtID(9), rlsOrgA, rlsSiteA, incID(2), rlsUserA, pendingEv}})
		okBatch(t, tx, append(base, stmt{incPhotoSQL, []any{rlsOrgA, rlsSiteA, incID(8), 1, rlsEvidenceA}}, stmt{checkNow, nil})...)
	})

	t.Run("an archived site takes no incident dated after it was archived", func(t *testing.T) {
		must(t, tx, 1, `UPDATE sites SET status = 'archived', archived_at = now() - interval '1 day' WHERE id = $1`, otherSite)
		other := func(inc string, occurred string) []stmt {
			return []stmt{
				{incidentSQL, []any{inc, rlsOrgA, otherSite, rlsUserA}},
				{versionSQL, []any{rlsOrgA, otherSite, inc, 1, nearMiss, occurred, nil, rlsUserA, nil, 0, 0}},
			}
		}
		refusedBatch(t, tx, "23514", "occurred after its site was archived", other(incID(9), ago)...)
		okBatch(t, tx, other(incID(9), "-2 days")...)
	})

	t.Run("statements", func(t *testing.T) {
		st := func(id, inc string, taken string, ev, supersedes any) stmt {
			return stmt{statementSQL, []any{id, rlsOrgA, rlsSiteA, inc, rlsUserA, taken, ev, supersedes}}
		}
		okBatch(t, tx, st(stmtID(1), incID(2), "0 minutes", rlsEvidenceA, nil))
		refusedBatch(t, tx, "23514", "taken_at is in the future", st(stmtID(2), incID(2), "1 hour", nil, nil))
		refusedBatch(t, tx, "23503", "", st(stmtID(2), incID(2), "0 minutes", pendingEv, nil))
		// A correction is on the same incident, and only one per statement.
		refusedBatch(t, tx, "23503", "witness_statements_supersedes_fkey", st(stmtID(2), incID(3), "0 minutes", nil, stmtID(1)))
		okBatch(t, tx, st(stmtID(2), incID(2), "0 minutes", nil, stmtID(1)))
		refusedBatch(t, tx, "23505", "witness_statements_supersedes_once", st(stmtID(3), incID(2), "0 minutes", nil, stmtID(1)))
	})

	t.Run("nothing changes as the app", func(t *testing.T) {
		refused(t, tx, "42501", "", `UPDATE incident_versions SET description = 'x' WHERE incident_id = $1`, rlsIncA)
		refused(t, tx, "42501", "", `DELETE FROM incident_versions WHERE incident_id = $1`, rlsIncA)
		refused(t, tx, "42501", "", `UPDATE incident_people SET injured = false WHERE incident_id = $1`, rlsIncA)
		refused(t, tx, "42501", "", `DELETE FROM incident_version_evidence WHERE incident_id = $1`, rlsIncA)
		refused(t, tx, "42501", "", `UPDATE witness_statements SET statement = 'x' WHERE incident_id = $1`, rlsIncA)
		refused(t, tx, "42501", "", `UPDATE incidents SET reported_by = reported_by WHERE id = $1`, rlsIncA)
		refused(t, tx, "42501", "", `DELETE FROM incidents WHERE id = $1`, rlsIncA)
		// latest_version moves only to a version that exists, by one.
		refused(t, tx, "23514", "changes only by a new version", `UPDATE incidents SET latest_version = 2 WHERE id = $1`, rlsIncA)
		refused(t, tx, "23514", "changes only by a new version", `UPDATE incidents SET latest_version = 0 WHERE id = $1`, rlsIncA)
	})
}

// The guards hold for the superuser too.
func TestIncidentsFrozenForEveryRole(t *testing.T) {
	seedRLS(t)
	tx := beginAs(t, admin, "")
	for _, sql := range []string{
		`UPDATE incident_versions SET description = 'x' WHERE incident_id = $1`,
		`DELETE FROM incident_versions WHERE incident_id = $1`,
		`UPDATE incident_people SET injured = false WHERE incident_id = $1`,
		`DELETE FROM incident_people WHERE incident_id = $1`,
		`DELETE FROM incident_version_evidence WHERE incident_id = $1`,
		`UPDATE witness_statements SET statement = 'x' WHERE incident_id = $1`,
		`DELETE FROM witness_statements WHERE incident_id = $1`,
		`UPDATE incidents SET reported_by = reported_by, created_at = now() - interval '1 year' WHERE id = $1`,
		`DELETE FROM incidents WHERE id = $1`,
	} {
		refused(t, tx, "23514", "", sql, rlsIncA)
	}
	refuseTruncates(t, tx, "incidents", "incident_versions", "incident_people", "incident_version_evidence", "witness_statements")

	// incidents_guard on its own: with the advance trigger off (rolled back
	// with the test), versions 2 and 3 exist but latest_version stays 1.
	must(t, tx, 0, `ALTER TABLE incident_versions DISABLE TRIGGER incident_versions_advance`)
	for n := 2; n <= 3; n++ {
		must(t, tx, 1, versionSQL, rlsOrgA, rlsSiteA, rlsIncA, n, nearMiss, ago, nil, rlsUserA, "Why", 0, 0)
	}
	refused(t, tx, "23514", "changes only by a new version", `UPDATE incidents SET latest_version = 3 WHERE id = $1`, rlsIncA)
	refused(t, tx, "23514", "changes only by a new version",
		`UPDATE incidents SET latest_version = 2, created_at = created_at - interval '1 year' WHERE id = $1`, rlsIncA)
	must(t, tx, 1, `UPDATE incidents SET latest_version = 2 WHERE id = $1`, rlsIncA)
}

// refuseTruncates truncates each table with the other tables' truncate
// triggers off, so each table's own trigger is shown to refuse it (CASCADE
// would otherwise reach a neighbour's trigger first).
func refuseTruncates(t *testing.T, tx pgx.Tx, tables ...string) {
	t.Helper()
	for _, target := range tables {
		var stmts []stmt
		for _, other := range tables {
			if other != target {
				stmts = append(stmts, stmt{`ALTER TABLE ` + other + ` DISABLE TRIGGER ` + other + `_no_truncate`, nil})
			}
		}
		stmts = append(stmts, stmt{`TRUNCATE ` + target + ` CASCADE`, nil})
		refusedBatch(t, tx, "23514", target+" is append-only", stmts...)
	}
}

// committedIncident creates an incident in org C with version 1 and commits it.
func committedIncident(t *testing.T, id string) {
	t.Helper()
	if err := seedOrgC(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	err := database.WithTenant(ctx, pool, database.Tenant{OrgID: orgC}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, incidentSQL, id, orgC, siteC, userC); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, versionSQL, orgC, siteC, id, 1, nearMiss, ago, nil, userC, nil, 0, 0)
		return err
	})
	if err != nil {
		t.Fatalf("committed incident: %v", err)
	}
}

func submitC(ctx context.Context, tx pgx.Tx, id string, n int) error {
	_, err := tx.Exec(ctx, versionSQL, orgC, siteC, id, n, nearMiss, ago, nil, userC, fmt.Sprintf("Amendment %d", n), 0, 0)
	return err
}

// Two submissions of the same version: the second waits for the first, then
// fails on the version number.
func TestSameVersionSubmissionsQueue(t *testing.T) {
	ctx := context.Background()
	id := fresh()
	committedIncident(t, id)

	first := tenantTx(t)
	defer first.Rollback(ctx)
	if err := submitC(ctx, first, id, 2); err != nil {
		t.Fatalf("first v2: %v", err)
	}

	second := make(chan error, 1)
	go func() {
		second <- database.WithTenant(ctx, pool, database.Tenant{OrgID: orgC}, func(tx pgx.Tx) error { return submitC(ctx, tx, id, 2) })
	}()
	waitingOn(t, "INSERT INTO incident_versions")

	if err := first.Commit(ctx); err != nil {
		t.Fatalf("commit first: %v", err)
	}
	if err := <-second; pgCode(err) != "23505" || !strings.Contains(err.Error(), "incident_versions_pkey") {
		t.Fatalf("second v2: %v, want SQLSTATE 23505 on incident_versions_pkey", err)
	}
}

// While version 2 is open, version 3 is refused at once: it isn't next as
// far as anything committed goes. A second version 2 waits for the first, and
// lands if the first is rolled back.
func TestVersionWhileAnotherIsOpen(t *testing.T) {
	ctx := context.Background()
	id := fresh()
	committedIncident(t, id)

	first := tenantTx(t)
	if err := submitC(ctx, first, id, 2); err != nil {
		t.Fatalf("open v2: %v", err)
	}

	err := database.WithTenant(ctx, pool, database.Tenant{OrgID: orgC}, func(tx pgx.Tx) error { return submitC(ctx, tx, id, 3) })
	if pgCode(err) != "23514" || !strings.Contains(err.Error(), "version 3 is not the next version") {
		t.Fatalf("v3 while v2 is open: %v, want SQLSTATE 23514 (not the next version)", err)
	}

	second := make(chan error, 1)
	go func() {
		second <- database.WithTenant(ctx, pool, database.Tenant{OrgID: orgC}, func(tx pgx.Tx) error { return submitC(ctx, tx, id, 2) })
	}()
	waitingOn(t, "INSERT INTO incident_versions")

	if err := first.Rollback(ctx); err != nil {
		t.Fatalf("roll back the first v2: %v", err)
	}
	if err := <-second; err != nil {
		t.Fatalf("second v2 after the first rolled back: %v", err)
	}
	var latest int
	if err := admin.QueryRow(ctx, `SELECT latest_version FROM incidents WHERE id = $1`, id).Scan(&latest); err != nil || latest != 2 {
		t.Errorf("latest_version %d, err %v; want 2", latest, err)
	}
}
