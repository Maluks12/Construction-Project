package database_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

// seedMember adds user to org with role and status the way the app does
// (000056): an organisation's first member is its owner, inserted active;
// anyone else is invited by an active owner and accepts, then is suspended
// or removed with a reason if status says so. It runs in tx and leaves
// app.user_id as it found it.
func seedMember(t *testing.T, tx pgx.Tx, org, user, role, status string) {
	t.Helper()
	ctx := context.Background()
	var owner string
	err := tx.QueryRow(ctx, `SELECT coalesce((SELECT user_id::text FROM org_members
		WHERE org_id = $1 AND role = 'owner' AND status = 'active' ORDER BY created_at LIMIT 1), '')`, org).Scan(&owner)
	if err != nil {
		t.Fatal(err)
	}
	if owner == "" {
		if role != "owner" || status != "active" {
			t.Fatalf("seedMember: org %s has no owner to invite a %s (%s)", org, role, status)
		}
		must(t, tx, 1, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner')`, org, user)
		return
	}
	var prev string
	if err := tx.QueryRow(ctx, `SELECT coalesce(current_setting('app.user_id', true), '')`).Scan(&prev); err != nil {
		t.Fatal(err)
	}
	steps := []stmt{
		{`SELECT set_config('app.user_id', $1, true), set_config('siteproof.member_reason', 'Test fixture', true)`, []any{owner}},
		{`INSERT INTO org_members (org_id, user_id, role, status) VALUES ($1, $2, $3, 'invited')`, []any{org, user, role}},
	}
	if status != "invited" {
		steps = append(steps, stmt{`UPDATE org_members SET status = 'active' WHERE org_id = $1 AND user_id = $2`, []any{org, user}})
	}
	if status == "suspended" || status == "removed" {
		steps = append(steps, stmt{`UPDATE org_members SET status = $3 WHERE org_id = $1 AND user_id = $2`, []any{org, user, status}})
	}
	steps = append(steps, stmt{`SELECT set_config('app.user_id', $1, true), set_config('siteproof.member_reason', '', true)`, []any{prev}})
	for _, s := range steps {
		if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("seedMember %s: %s: %v", role, s.sql, err)
		}
	}
}

// seedMemberAdmin is seedMember in a committed transaction of its own, as
// the superuser (fixtures that commit).
func seedMemberAdmin(t *testing.T, org, user, role, status string) {
	t.Helper()
	ctx := context.Background()
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	seedMember(t, tx, org, user, role, status)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}
