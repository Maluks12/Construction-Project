package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// LockOrganisation takes the organisation row FOR NO KEY UPDATE: the first
// lock of every writer of org_members (ONBOARD D3), so no two of them lock in
// opposite orders. org_members_guard (000054) takes it too, which makes the
// rule hold for any other path. It doesn't block foreign-key checks (they
// take FOR KEY SHARE).
func LockOrganisation(ctx context.Context, tx pgx.Tx, orgID string) error {
	if _, err := tx.Exec(ctx, `SELECT 1 FROM organisations WHERE id = $1 FOR NO KEY UPDATE`, orgID); err != nil {
		return fmt.Errorf("lock organisation: %w", err)
	}
	return nil
}

// SetMemberChange gives the next org_members update its reason and, for a
// change into supervisor, its sites (nil: none named), transaction-locally.
// The 000054 triggers read them: the guard requires the reason, and
// org_members_changed records both in member_history.
func SetMemberChange(ctx context.Context, tx pgx.Tx, reason string, siteIDs []string) error {
	if _, err := tx.Exec(ctx, `SELECT set_config('siteproof.member_reason', $1, true)`, reason); err != nil {
		return fmt.Errorf("set member change: %w", err)
	}
	sites := ""
	if siteIDs != nil {
		if err := tx.QueryRow(ctx, `SELECT $1::uuid[]::text`, siteIDs).Scan(&sites); err != nil {
			return fmt.Errorf("set member change: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('siteproof.member_site_ids', $1, true)`, sites); err != nil {
		return fmt.Errorf("set member change: %w", err)
	}
	return nil
}
