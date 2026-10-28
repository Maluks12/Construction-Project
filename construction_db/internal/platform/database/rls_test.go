package database_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"construction_db/internal/platform/database"
)

// Two orgs with exactly one row in every RLS table, seeded as superuser.
const (
	rlsOrgA  = "20000000-0000-0000-0000-00000000000a"
	rlsOrgB  = "20000000-0000-0000-0000-00000000000b"
	rlsUserA = "21000000-0000-0000-0000-00000000000a"
	rlsUserB = "21000000-0000-0000-0000-00000000000b"
	rlsSiteB = "22000000-0000-0000-0000-00000000000b"
	rlsSessB = "24000000-0000-0000-0000-00000000000b"
)

type rlsTable struct {
	name, key    string // key identifies a row's org; refresh_tokens has no org_id
	wantA, wantB string
}

// In foreign-key-safe delete order.
var rlsTables = []rlsTable{
	{"refresh_tokens", "token_hash", "rls-refresh-a", "rls-refresh-b"},
	{"sessions", "org_id", rlsOrgA, rlsOrgB},
	{"invitations", "org_id", rlsOrgA, rlsOrgB},
	{"site_locations", "org_id", rlsOrgA, rlsOrgB},
	{"site_members", "org_id", rlsOrgA, rlsOrgB},
	{"sites", "org_id", rlsOrgA, rlsOrgB},
	{"org_members", "org_id", rlsOrgA, rlsOrgB},
	{"organisations", "id", rlsOrgA, rlsOrgB},
}

var seedOnce = sync.OnceValue(func() error {
	ctx := context.Background()
	tx, err := admin.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, x := range []string{"a", "b"} {
		id := func(prefix string) string { return prefix + "000000-0000-0000-0000-00000000000" + x }
		org, user, site, loc, sess, inv := id("20"), id("21"), id("22"), id("23"), id("24"), id("26")

		stmts := []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO organisations (id, trading_name, primary_edition, enabled_editions) VALUES ($1, 'RLS', 'construction', ARRAY['construction'])`, []any{org}},
			{`INSERT INTO users (id, email, full_name) VALUES ($1, $2, 'RLS user')`, []any{user, "rls-" + x + "@rls.test"}},
			{`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner')`, []any{org, user}},
			{`INSERT INTO sites (id, org_id, regime_id, name) SELECT $1, $2, id, 'RLS site' FROM regulatory_regimes LIMIT 1`, []any{site, org}},
			{`INSERT INTO site_members (org_id, site_id, user_id) VALUES ($1, $2, $3)`, []any{org, site, user}},
			{`INSERT INTO site_locations (id, org_id, site_id, kind, name) VALUES ($1, $2, $3, 'zone', $4)`, []any{loc, org, site, "Zone " + x}},
			{`INSERT INTO sessions (id, org_id, user_id, expires_at) VALUES ($1, $2, $3, now() + interval '1 hour')`, []any{sess, org, user}},
			{`INSERT INTO refresh_tokens (session_id, token_hash, expires_at) VALUES ($1, $2, now() + interval '1 hour')`, []any{sess, "rls-refresh-" + x}},
			{`INSERT INTO invitations (id, org_id, user_id, token_hash, invited_by, expires_at) VALUES ($1, $2, $3, $4, $3, now() + interval '1 day')`, []any{inv, org, user, "rls-invite-" + x}},
		}
		for _, s := range stmts {
			if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
				return fmt.Errorf("seed %q: %w", s.sql, err)
			}
		}
	}
	return tx.Commit(ctx)
})

func seedRLS(t *testing.T) {
	t.Helper()
	if err := seedOnce(); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func TestRLSReads(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()

	for _, tb := range rlsTables {
		t.Run(tb.name, func(t *testing.T) {
			var keys []string
			err := database.WithTenant(ctx, pool, database.Tenant{OrgID: rlsOrgA}, func(tx pgx.Tx) error {
				rows, err := tx.Query(ctx, fmt.Sprintf(`SELECT %s::text FROM %s`, tb.key, tb.name))
				if err != nil {
					return err
				}
				keys, err = pgx.CollectRows(rows, pgx.RowTo[string])
				return err
			})
			if err != nil {
				t.Fatalf("select as A: %v", err)
			}
			if !slices.Equal(keys, []string{tb.wantA}) {
				t.Errorf("unfiltered SELECT as A returned %v, want only [%s]", keys, tb.wantA)
			}

			var n int
			err = database.WithTenant(ctx, pool, database.Tenant{}, func(tx pgx.Tx) error {
				return tx.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s`, tb.name)).Scan(&n)
			})
			if err != nil {
				t.Fatalf("count with no tenant: %v", err)
			}
			if n != 0 {
				t.Errorf("no tenant sees %d rows, want 0", n)
			}
		})
	}
}

func TestRLSWrites(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()
	tenantA := database.Tenant{OrgID: rlsOrgA}

	t.Run("insert into B fails the policy", func(t *testing.T) {
		inserts := map[string]struct {
			sql  string
			args []any
		}{
			"organisations":  {`INSERT INTO organisations (id, trading_name, primary_edition, enabled_editions) VALUES ($1, 'X', 'construction', ARRAY['construction'])`, []any{"27000000-0000-0000-0000-00000000000b"}},
			"org_members":    {`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner')`, []any{rlsOrgB, rlsUserA}},
			"sites":          {`INSERT INTO sites (org_id, regime_id, name) SELECT $1, id, 'X' FROM regulatory_regimes LIMIT 1`, []any{rlsOrgB}},
			"site_members":   {`INSERT INTO site_members (org_id, site_id, user_id) VALUES ($1, $2, $3)`, []any{rlsOrgB, rlsSiteB, rlsUserA}},
			"site_locations": {`INSERT INTO site_locations (org_id, site_id, kind, name) VALUES ($1, $2, 'zone', 'Smuggled')`, []any{rlsOrgB, rlsSiteB}},
			"sessions":       {`INSERT INTO sessions (org_id, user_id, expires_at) VALUES ($1, $2, now() + interval '1 hour')`, []any{rlsOrgB, rlsUserB}},
			"invitations":    {`INSERT INTO invitations (org_id, user_id, token_hash, invited_by, expires_at) VALUES ($1, $2, 'rls-smuggled', $2, now() + interval '1 day')`, []any{rlsOrgB, rlsUserB}},
			"refresh_tokens": {`INSERT INTO refresh_tokens (session_id, token_hash, expires_at) VALUES ($1, 'rls-smuggled', now() + interval '1 day')`, []any{rlsSessB}},
		}
		for _, tb := range rlsTables {
			t.Run(tb.name, func(t *testing.T) {
				in := inserts[tb.name]
				err := database.WithTenant(ctx, pool, tenantA, func(tx pgx.Tx) error {
					_, err := tx.Exec(ctx, in.sql, in.args...)
					return err
				})
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
					t.Fatalf("got %v, want SQLSTATE 42501", err)
				}
			})
		}
	})

	// Rolled back so the fixtures survive. RowsAffected counts every row
	// changed, so 1 means only A's row: B's was never reached.
	t.Run("unfiltered update and delete reach only A", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback(ctx)
		if err := database.SetTenant(ctx, tx, tenantA); err != nil {
			t.Fatalf("set tenant: %v", err)
		}

		for _, tb := range rlsTables {
			tag, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET %s = %s`, tb.name, tb.key, tb.key))
			if err != nil {
				t.Fatalf("update %s: %v", tb.name, err)
			}
			if tag.RowsAffected() != 1 {
				t.Errorf("unfiltered UPDATE %s changed %d rows, want 1", tb.name, tag.RowsAffected())
			}
		}
		for _, tb := range rlsTables {
			tag, err := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s`, tb.name))
			if err != nil {
				t.Fatalf("delete %s: %v", tb.name, err)
			}
			if tag.RowsAffected() != 1 {
				t.Errorf("unfiltered DELETE %s removed %d rows, want 1", tb.name, tag.RowsAffected())
			}
		}
	})

	t.Run("B's rows are intact", func(t *testing.T) {
		for _, tb := range rlsTables {
			var n int
			q := fmt.Sprintf(`SELECT count(*) FROM %s WHERE %s::text = $1`, tb.name, tb.key)
			if err := admin.QueryRow(ctx, q, tb.wantB).Scan(&n); err != nil {
				t.Fatalf("count %s: %v", tb.name, err)
			}
			if n != 1 {
				t.Errorf("%s: B has %d rows, want 1", tb.name, n)
			}
		}
	})
}

// Tables that are deliberately outside RLS.
var rlsAllowlist = []string{
	"users",              // no org_id; accepted risk, see BUILD_PLAN DEPLOY
	"regulatory_regimes", // public system data, read-only to siteproof_app
	"schema_migrations",  // golang-migrate's bookkeeping (absent in tests)
}

type tableRLS struct {
	name                    string
	enabled, forced, orgCol bool
	policies                int
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func loadRLSCatalog(t *testing.T, db querier) []tableRLS {
	t.Helper()
	rows, err := db.Query(context.Background(), `
		SELECT c.relname, c.relrowsecurity, c.relforcerowsecurity,
		       EXISTS (SELECT 1 FROM pg_attribute a
		               WHERE a.attrelid = c.oid AND a.attname = 'org_id' AND a.attnum > 0 AND NOT a.attisdropped),
		       (SELECT count(*)::int FROM pg_policy p WHERE p.polrelid = c.oid)
		FROM pg_class c
		WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p')
		ORDER BY c.relname
	`)
	if err != nil {
		t.Fatalf("read catalog: %v", err)
	}
	tables, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (tableRLS, error) {
		var x tableRLS
		err := r.Scan(&x.name, &x.enabled, &x.forced, &x.orgCol, &x.policies)
		return x, err
	})
	if err != nil {
		t.Fatalf("read catalog: %v", err)
	}
	return tables
}

// coverageProblems: every table with org_id, and every public table not on the
// allowlist, must have RLS enabled and forced with at least one policy.
func coverageProblems(tables []tableRLS) []string {
	var out []string
	for _, x := range tables {
		allowed := slices.Contains(rlsAllowlist, x.name)
		if allowed && !x.orgCol {
			continue
		}
		if allowed {
			out = append(out, x.name+": has org_id but is on the RLS allowlist")
		}
		if !x.enabled {
			out = append(out, x.name+": RLS not enabled")
		}
		if !x.forced {
			out = append(out, x.name+": RLS not forced")
		}
		if x.policies == 0 {
			out = append(out, x.name+": no policy")
		}
	}
	return out
}

func TestRLSCoverage(t *testing.T) {
	ctx := context.Background()
	tables := loadRLSCatalog(t, pool)

	var names, orgTables []string
	for _, x := range tables {
		names = append(names, x.name)
		if x.orgCol {
			orgTables = append(orgTables, x.name)
		}
	}
	if !slices.Contains(orgTables, "sites") {
		t.Fatalf("catalog query found no org_id on sites (got %v); it would pass vacuously", orgTables)
	}
	for _, tb := range rlsTables {
		if !slices.Contains(names, tb.name) {
			t.Fatalf("catalog query is missing %s (got %v)", tb.name, names)
		}
	}

	for _, p := range coverageProblems(tables) {
		t.Error(p)
	}

	// DDL is transactional, so these probes are rolled back and never seen by other tests.
	probe := func(t *testing.T, ddl, want string) {
		t.Helper()
		tx, err := admin.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx, ddl); err != nil {
			t.Fatalf("probe ddl: %v", err)
		}
		if got := coverageProblems(loadRLSCatalog(t, tx)); !slices.Contains(got, want) {
			t.Errorf("coverage problems %q do not include %q", got, want)
		}
	}
	t.Run("catches a new table with org_id and no RLS", func(t *testing.T) {
		probe(t, `CREATE TABLE rls_probe (org_id uuid)`, "rls_probe: RLS not enabled")
	})
	t.Run("catches a new table without org_id and not allowlisted", func(t *testing.T) {
		probe(t, `CREATE TABLE rls_probe_indirect (session_id uuid)`, "rls_probe_indirect: no policy")
	})
	t.Run("catches RLS switched off on a tenant table", func(t *testing.T) {
		probe(t, `ALTER TABLE sites NO FORCE ROW LEVEL SECURITY`, "sites: RLS not forced")
	})
}

func TestTokenLookups(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()

	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM refresh_token_tenant('unknown')`).Scan(&n); err != nil || n != 0 {
		t.Errorf("refresh_token_tenant(unknown): %d rows, err %v; want 0", n, err)
	}
	var org *string
	if err := pool.QueryRow(ctx, `SELECT invitation_org('unknown')::text`).Scan(&org); err != nil || org != nil {
		t.Errorf("invitation_org(unknown) = %v, err %v; want NULL", org, err)
	}

	var gotOrg, gotUser string
	if err := pool.QueryRow(ctx, `SELECT org_id::text, user_id::text FROM refresh_token_tenant('rls-refresh-b')`).Scan(&gotOrg, &gotUser); err != nil {
		t.Fatalf("refresh_token_tenant(b): %v", err)
	}
	if gotOrg != rlsOrgB || gotUser != rlsUserB {
		t.Errorf("refresh_token_tenant(b) = (%s, %s), want (%s, %s)", gotOrg, gotUser, rlsOrgB, rlsUserB)
	}
	if err := pool.QueryRow(ctx, `SELECT invitation_org('rls-invite-b')::text`).Scan(&gotOrg); err != nil || gotOrg != rlsOrgB {
		t.Errorf("invitation_org(b) = %s, err %v; want %s", gotOrg, err, rlsOrgB)
	}

	// No tenant set and no WithTenant: the app role sees none of the rows the lookups found.
	for _, table := range []string{"sessions", "invitations"} {
		if err := pool.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s`, table)).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s with no tenant: %d rows, err %v; want 0", table, n, err)
		}
	}
}
