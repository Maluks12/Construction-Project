package database_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"construction_db/internal/platform/database"
)

func workID(n int) string { return fmt.Sprintf("50000000-0000-0000-0000-%012d", n) }

const (
	siteWorkSQL = `
		INSERT INTO site_work (id, org_id, site_id, regime_id, created_by)
		SELECT $1, $2, $3, id, $4 FROM regulatory_regimes LIMIT 1`
	// $5 starts_on, $6 facts, $7 status, $8 submitter, $9 client_submitted_at offset, $10 amend_reason.
	workVersionSQL = `
		INSERT INTO site_work_versions (org_id, site_id, site_work_id, version, description, starts_on, facts, status,
		                                submitted_by, client_submitted_at, amend_reason)
		VALUES ($1, $2, $3, $4, 'Bulk earthworks', $5, $6, $7, $8, now() + $9::interval, $10)`
)

func workHeader(id string) stmt { return stmt{siteWorkSQL, []any{id, rlsOrgA, rlsSiteA, rlsUserA}} }

func wv(id string, n int, starts string, facts []string, status string, reason any) stmt {
	return stmt{workVersionSQL, []any{rlsOrgA, rlsSiteA, id, n, starts, facts, status, rlsUserA, "0 minutes", reason}}
}

var dig = []string{"excavation"}

func TestSiteWorkRules(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()
	tx := beginAs(t, pool, rlsOrgA)
	const otherSite = "22000000-0000-0000-0000-0000000000f2"
	must(t, tx, 1, `INSERT INTO sites (id, org_id, regime_id, name) SELECT $1, $2, id, 'Second site' FROM regulatory_regimes LIMIT 1`, otherSite, rlsOrgA)

	t.Run("site work is created by its version 1", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "has no version", workHeader(workID(1)), stmt{checkNow, nil})
		refusedBatch(t, tx, "23514", "version 2 is not the next version", workHeader(workID(1)), wv(workID(1), 2, "2026-12-01", dig, "planned", "Why"))
		refusedBatch(t, tx, "23514", "new site work has no versions yet", stmt{`
			INSERT INTO site_work (id, org_id, site_id, regime_id, created_by, latest_version)
			SELECT $1, $2, $3, id, $4, 1 FROM regulatory_regimes LIMIT 1`, []any{workID(1), rlsOrgA, rlsSiteA, rlsUserA}})
	})

	okBatch(t, tx, workHeader(workID(2)), wv(workID(2), 1, "2026-12-01", dig, "planned", nil), stmt{checkNow, nil})

	t.Run("versions are numbered with no gaps", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "version 3 is not the next version", wv(workID(2), 3, "2026-12-01", dig, "planned", "Why"))
		refusedBatch(t, tx, "23505", "site_work_versions_pkey", wv(workID(2), 1, "2026-12-01", dig, "planned", nil))
		okBatch(t, tx, wv(workID(2), 2, "2026-12-08", dig, "planned", "Start moved"))
		var latest int
		if err := tx.QueryRow(ctx, `SELECT latest_version FROM site_work WHERE id = $1`, workID(2)).Scan(&latest); err != nil || latest != 2 {
			t.Errorf("latest_version %d, err %v; want 2", latest, err)
		}
	})

	t.Run("an amendment needs a reason, version 1 has none", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "site_work_versions_amend_reason", wv(workID(2), 3, "2026-12-01", dig, "planned", nil))
		refusedBatch(t, tx, "23514", "site_work_versions_amend_reason", workHeader(workID(3)), wv(workID(3), 1, "2026-12-01", dig, "planned", "Why"))
	})

	t.Run("version 1 is planned; later ones may cancel", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "site_work_versions_first_planned", workHeader(workID(3)), wv(workID(3), 1, "2026-12-01", dig, "cancelled", nil))
		refusedBatch(t, tx, "23514", "site_work_versions_status_check", wv(workID(2), 3, "2026-12-01", dig, "started", "Why"))
		okBatch(t, tx, wv(workID(2), 3, "2026-12-08", dig, "cancelled", "Not going ahead"))
	})

	t.Run("facts", func(t *testing.T) {
		for _, facts := range [][]string{{"Excavation"}, {"excavation", "excavation"}, {"work-at-height"}} {
			refusedBatch(t, tx, "23514", "site_work_versions_facts_check", workHeader(workID(4)), wv(workID(4), 1, "2026-12-01", facts, "planned", nil))
		}
		okBatch(t, tx, workHeader(workID(4)), wv(workID(4), 1, "2026-12-01", []string{}, "planned", nil))
	})

	t.Run("dates", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "site_work_versions_ends_after_start", workHeader(workID(5)), stmt{`
			INSERT INTO site_work_versions (org_id, site_id, site_work_id, version, description, starts_on, ends_on, facts, status,
			                                submitted_by, client_submitted_at)
			VALUES ($1, $2, $3, 1, 'Short', '2026-12-10', '2026-12-09', '{}', 'planned', $4, now())`, []any{rlsOrgA, rlsSiteA, workID(5), rlsUserA}})
		refusedBatch(t, tx, "22000", "ZA starts on 2024-01-01; 2023-06-01 is before it", workHeader(workID(5)), wv(workID(5), 1, "2023-06-01", dig, "planned", nil))
		refusedBatch(t, tx, "22000", "ZA ends on 2030-12-31; 2031-01-05 is after it", workHeader(workID(5)), wv(workID(5), 1, "2031-01-05", dig, "planned", nil))
		refusedBatch(t, tx, "23514", "site_work_versions_description_check", workHeader(workID(5)), stmt{`
			INSERT INTO site_work_versions (org_id, site_id, site_work_id, version, description, starts_on, facts, status, submitted_by, client_submitted_at)
			VALUES ($1, $2, $3, 1, '', '2026-12-10', '{}', 'planned', $4, now())`, []any{rlsOrgA, rlsSiteA, workID(5), rlsUserA}})
	})

	t.Run("times", func(t *testing.T) {
		future := stmt{workVersionSQL, []any{rlsOrgA, rlsSiteA, workID(6), 1, "2026-12-01", dig, "planned", rlsUserA, "1 hour", nil}}
		refusedBatch(t, tx, "23514", "client_submitted_at is in the future", workHeader(workID(6)), future)
		skew := stmt{workVersionSQL, []any{rlsOrgA, rlsSiteA, workID(6), 1, "2026-12-01", dig, "planned", rlsUserA, "4 minutes", nil}}
		okBatch(t, tx, workHeader(workID(6)), skew)
		okBatch(t, tx, workHeader(workID(7)), stmt{`
			INSERT INTO site_work_versions (org_id, site_id, site_work_id, version, description, starts_on, facts, status,
			                                submitted_by, client_submitted_at, submitted_at)
			VALUES ($1, $2, $3, 1, 'D', '2026-12-01', '{}', 'planned', $4, now(), '2000-01-01')`, []any{rlsOrgA, rlsSiteA, workID(7), rlsUserA}})
		var stale int
		err := tx.QueryRow(ctx, `
			SELECT (SELECT count(*) FROM site_work_versions WHERE site_work_id = $1 AND submitted_at < now() - interval '1 minute')
			     + (SELECT count(*) FROM site_work WHERE id = $1 AND created_at < now() - interval '1 minute')`, workID(7)).Scan(&stale)
		if err != nil || stale != 0 {
			t.Errorf("%d rows kept the client's time, err %v; want 0", stale, err)
		}
	})

	t.Run("an archived site takes nothing submitted after it was archived", func(t *testing.T) {
		must(t, tx, 1, `UPDATE sites SET status = 'archived', archived_at = now() - interval '1 day' WHERE id = $1`, otherSite)
		other := func(id, submitted string) []stmt {
			return []stmt{
				{siteWorkSQL, []any{id, rlsOrgA, otherSite, rlsUserA}},
				{workVersionSQL, []any{rlsOrgA, otherSite, id, 1, "2026-12-01", dig, "planned", rlsUserA, submitted, nil}},
			}
		}
		refusedBatch(t, tx, "23514", "submitted after its site was archived", other(workID(8), "-1 hour")...)
		okBatch(t, tx, other(workID(8), "-2 days")...)
	})

	t.Run("nothing changes as the app", func(t *testing.T) {
		refused(t, tx, "42501", "", `UPDATE site_work_versions SET description = 'x' WHERE site_work_id = $1`, workID(2))
		refused(t, tx, "42501", "", `DELETE FROM site_work_versions WHERE site_work_id = $1`, workID(2))
		refused(t, tx, "42501", "", `UPDATE site_work SET created_by = created_by WHERE id = $1`, workID(2))
		refused(t, tx, "42501", "", `DELETE FROM site_work WHERE id = $1`, workID(2))
		refused(t, tx, "23514", "changes only by a new version", `UPDATE site_work SET latest_version = 5 WHERE id = $1`, workID(2))
		refused(t, tx, "23514", "changes only by a new version", `UPDATE site_work SET latest_version = 0 WHERE id = $1`, workID(2))
	})
}

func TestSiteWorkFrozenForEveryRole(t *testing.T) {
	seedRLS(t)
	tx := beginAs(t, admin, "")
	for _, sql := range []string{
		`UPDATE site_work_versions SET description = 'x' WHERE site_work_id = $1`,
		`UPDATE site_work_versions SET starts_on = starts_on + 7 WHERE site_work_id = $1`,
		`DELETE FROM site_work_versions WHERE site_work_id = $1`,
		`UPDATE site_work SET created_at = now() - interval '1 year' WHERE id = $1`,
		`DELETE FROM site_work WHERE id = $1`,
	} {
		refused(t, tx, "23514", "", sql, rlsWorkA)
	}
	refuseTruncates(t, tx, "site_work", "site_work_versions")

	must(t, tx, 0, `ALTER TABLE site_work_versions DISABLE TRIGGER site_work_versions_advance`)
	for n := 2; n <= 3; n++ {
		must(t, tx, 1, workVersionSQL, rlsOrgA, rlsSiteA, rlsWorkA, n, "2026-12-01", dig, "planned", rlsUserA, "0 minutes", "Why")
	}
	refused(t, tx, "23514", "changes only by a new version", `UPDATE site_work SET latest_version = 3 WHERE id = $1`, rlsWorkA)
	must(t, tx, 1, `UPDATE site_work SET latest_version = 2 WHERE id = $1`, rlsWorkA)
}

func committedSiteWork(t *testing.T, id string) {
	t.Helper()
	if err := seedOrgC(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	err := database.WithTenant(ctx, pool, database.Tenant{OrgID: orgC}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, siteWorkSQL, id, orgC, siteC, userC); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, workVersionSQL, orgC, siteC, id, 1, "2026-12-01", dig, "planned", userC, "0 minutes", nil)
		return err
	})
	if err != nil {
		t.Fatalf("committed site work: %v", err)
	}
}

// Two version 2s: the second waits on the header's row lock, then fails.
func TestSiteWorkSameVersionQueues(t *testing.T) {
	ctx := context.Background()
	id := fresh()
	committedSiteWork(t, id)
	submit := func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, workVersionSQL, orgC, siteC, id, 2, "2026-12-08", dig, "planned", userC, "0 minutes", "Moved")
		return err
	}

	first := tenantTx(t)
	defer first.Rollback(ctx)
	if err := submit(first); err != nil {
		t.Fatalf("first v2: %v", err)
	}
	second := make(chan error, 1)
	go func() {
		second <- database.WithTenant(ctx, pool, database.Tenant{OrgID: orgC}, submit)
	}()
	waitingOn(t, "INSERT INTO site_work_versions")
	if err := first.Commit(ctx); err != nil {
		t.Fatalf("commit first: %v", err)
	}
	if err := <-second; pgCode(err) != "23505" || !strings.Contains(err.Error(), "site_work_versions_pkey") {
		t.Fatalf("second v2: %v, want SQLSTATE 23505 on site_work_versions_pkey", err)
	}
}
