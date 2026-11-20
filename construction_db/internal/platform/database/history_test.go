package database_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"construction_db/internal/platform/database"
)

// historyOrg commits an organisation with a site and a user, who is also a
// supervisor in a second organisation. It returns their ids.
func historyOrg(t *testing.T) (org, other, user, site string) {
	t.Helper()
	ctx := context.Background()
	org, other, user, site = fresh(), fresh(), fresh(), fresh()
	for _, s := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO organisations (id, trading_name, primary_edition, enabled_editions) VALUES ($1, 'Hist Co', 'construction', ARRAY['construction'])`, []any{org}},
		{`INSERT INTO organisations (id, trading_name, primary_edition, enabled_editions) VALUES ($1, 'Other Co', 'construction', ARRAY['construction'])`, []any{other}},
		{`INSERT INTO users (id, email, full_name) VALUES ($1, $2, 'Hist User')`, []any{user, user + "@history.test"}},
		{`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner')`, []any{org, user}},
		{`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'supervisor')`, []any{other, user}},
		{`INSERT INTO sites (id, org_id, regime_id, name) SELECT $1, $2, id, 'Hist site' FROM regulatory_regimes LIMIT 1`, []any{site, org}},
	} {
		if _, err := admin.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("%s: %v", s.sql, err)
		}
	}
	return org, other, user, site
}

func historyValues(t *testing.T, sql string, args ...any) []string {
	t.Helper()
	rows, err := admin.Query(context.Background(), sql, args...)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func wantValues(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: got %q, want %q", what, got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s: got %q, want %q", what, got, want)
			return
		}
	}
}

// Every edit in place leaves its new state in history, by whom, and an edit
// that changes nothing leaves nothing. A name change reaches every
// organisation the user belongs to, whatever the tenant.
func TestHistoryTables(t *testing.T) {
	ctx := context.Background()
	org, other, user, site := historyOrg(t)
	talk := fresh()

	err := database.WithTenant(ctx, pool, database.Tenant{OrgID: org, UserID: user}, func(tx pgx.Tx) error {
		for _, s := range []struct {
			sql  string
			args []any
		}{
			{talkSQL, []any{talk, org, site, nil, "Working at height", time.Now().Add(-time.Hour), nil, "Africa/Johannesburg", user}},
			{`UPDATE sites SET name = 'Hist site 2', address = '1 Main Rd' WHERE id = $1`, []any{site}},
			{`UPDATE sites SET name = name WHERE id = $1`, []any{site}},
			{`UPDATE organisations SET trading_name = 'Hist Co 2', vat_no = '4123' WHERE id = $1`, []any{org}},
			{`UPDATE organisations SET trading_name = trading_name WHERE id = $1`, []any{org}},
			{`UPDATE org_members SET role = 'she_officer' WHERE org_id = $1 AND user_id = $2`, []any{org, user}},
			{`UPDATE org_members SET status = status WHERE org_id = $1 AND user_id = $2`, []any{org, user}},
			{`UPDATE users SET full_name = 'Hist User 2' WHERE id = $1`, []any{user}},
			{`UPDATE users SET mobile = '+27820000999' WHERE id = $1`, []any{user}},
			{`UPDATE toolbox_talks SET topic = 'Ladders' WHERE id = $1`, []any{talk}},
			{`UPDATE toolbox_talks SET notes = notes WHERE id = $1`, []any{talk}},
		} {
			if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
				t.Fatalf("%s: %v", s.sql, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	wantValues(t, "site_history", historyValues(t,
		`SELECT name || '|' || coalesce(address, '-') || '|' || coalesce(changed_by::text, '-') FROM site_history WHERE site_id = $1 ORDER BY id`, site),
		"Hist site|-|-", "Hist site 2|1 Main Rd|"+user)
	wantValues(t, "organisation_history", historyValues(t,
		`SELECT trading_name || '|' || coalesce(vat_no, '-') FROM organisation_history WHERE org_id = $1 ORDER BY id`, org),
		"Hist Co|-", "Hist Co 2|4123")
	wantValues(t, "member_history in the tenant's org", historyValues(t,
		`SELECT role || '|' || status || '|' || full_name FROM member_history WHERE org_id = $1 AND user_id = $2 ORDER BY id`, org, user),
		"owner|active|Hist User", "she_officer|active|Hist User", "she_officer|active|Hist User 2")
	wantValues(t, "member_history in the user's other org", historyValues(t,
		`SELECT role || '|' || full_name FROM member_history WHERE org_id = $1 AND user_id = $2 ORDER BY id`, other, user),
		"supervisor|Hist User", "supervisor|Hist User 2")
	wantValues(t, "toolbox_talk_history", historyValues(t,
		`SELECT topic FROM toolbox_talk_history WHERE talk_id = $1 ORDER BY id`, talk),
		"Working at height", "Ladders")

	// The app reads its own org's history and nothing else; it can't write it.
	err = database.WithTenant(ctx, pool, database.Tenant{OrgID: org, UserID: user}, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM member_history WHERE user_id = $1`, user).Scan(&n); err != nil {
			return err
		}
		if n != 3 {
			t.Errorf("the tenant sees %d member_history rows of the user, want its own org's 3", n)
		}
		for _, sql := range []string{
			`INSERT INTO site_history (org_id, site_id, regime_id, name, status, changed_at) SELECT org_id, site_id, regime_id, 'x', 'active', now() FROM site_history WHERE site_id = '` + site + `' LIMIT 1`,
			`UPDATE site_history SET name = 'x' WHERE site_id = '` + site + `'`,
			`DELETE FROM member_history WHERE user_id = '` + user + `'`,
		} {
			refused(t, tx, "42501", "", sql)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Append-only for every role, superusers included.
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	for _, table := range []string{"organisation_history", "site_history", "member_history", "toolbox_talk_history"} {
		refused(t, tx, "23514", "append-only", `UPDATE `+table+` SET changed_at = changed_at`)
		refused(t, tx, "23514", "append-only", `DELETE FROM `+table)
		refused(t, tx, "23514", "append-only", `TRUNCATE `+table)
	}
}

// Two edits of one site at once: the second waits for the first's row lock,
// so history records them in commit order and its newest row is the
// current value. That is what reading "as of" a time relies on.
func TestHistoryFollowsCommitOrder(t *testing.T) {
	ctx := context.Background()
	org, _, user, site := historyOrg(t)
	tenant := database.Tenant{OrgID: org, UserID: user}

	first, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Rollback(ctx)
	if err := database.SetTenant(ctx, first, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Exec(ctx, `UPDATE sites SET name = 'First' WHERE id = $1`, site); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- database.WithTenant(ctx, pool, tenant, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE sites SET name = 'Second' WHERE id = $1 /* history race */`, site)
			return err
		})
	}()
	waitingOn(t, "history race")
	if err := first.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	wantValues(t, "site_history in commit order", historyValues(t,
		`SELECT name FROM site_history WHERE site_id = $1 ORDER BY changed_at, id`, site),
		"Hist site", "First", "Second")
	wantValues(t, "the newest history row is the current value", historyValues(t,
		`SELECT (SELECT name FROM site_history WHERE site_id = $1 ORDER BY changed_at DESC, id DESC LIMIT 1) || '=' || s.name FROM sites s WHERE s.id = $1`, site),
		"Second=Second")
}
