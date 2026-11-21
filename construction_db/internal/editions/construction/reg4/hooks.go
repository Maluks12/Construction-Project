package reg4

import (
	"context"

	"github.com/jackc/pgx/v5"

	"construction_db/internal/core/deadlines"
)

// Hooks are site work's part in the site deadline endpoints.
type Hooks struct{}

var _ deadlines.SubjectHooks = Hooks{}

func (Hooks) LockForSatisfaction(ctx context.Context, tx pgx.Tx, orgID, siteID, workID string) error {
	_, err := lockHeader(ctx, tx, orgID, siteID, workID)
	return err
}

// Satisfied: no site work rule follows a satisfaction.
func (Hooks) Satisfied(context.Context, pgx.Tx, deadlines.Satisfied) error { return nil }
