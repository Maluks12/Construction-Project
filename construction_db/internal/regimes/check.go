package regimes

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// CheckDatabase fails if a regulatory_regimes row has no data set, or has one
// with a different version or edition. Sites point at these rows, so a row
// without matching data would leave its sites with no checklists or deadline
// rules. Refusing to start is better than failing on the first request.
//
// A data set with no row is allowed: it can ship before the migration that
// adds the row.
func CheckDatabase(ctx context.Context, db DBTX, reg *Registry) error {
	rows, err := db.Query(ctx, `SELECT code, version, edition FROM regulatory_regimes ORDER BY code`)
	if err != nil {
		return fmt.Errorf("check regimes: %w", err)
	}
	defer rows.Close()

	var errs []error
	for rows.Next() {
		var code, version, edition string
		if err := rows.Scan(&code, &version, &edition); err != nil {
			return fmt.Errorf("check regimes: %w", err)
		}

		ds, ok := reg.Get(code)
		if !ok {
			errs = append(errs, fmt.Errorf("%s: no data set for this regulatory_regimes row", code))
			continue
		}
		if ds.Regime.Version != version {
			errs = append(errs, fmt.Errorf("%s: database version %q but data set version %q", code, version, ds.Regime.Version))
		}
		if ds.Regime.Edition != edition {
			errs = append(errs, fmt.Errorf("%s: database edition %q but data set edition %q", code, edition, ds.Regime.Edition))
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("check regimes: %w", err)
	}

	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("check regimes: %w", err)
	}
	return nil
}

// CheckDefaultRow fails unless the default regime has a regulatory_regimes row;
// without one, creating a site inserts nothing.
func CheckDefaultRow(ctx context.Context, db DBTX, regime string) error {
	var exists bool
	err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM regulatory_regimes WHERE code = $1)`, regime).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check default regime row: %w", err)
	}
	if !exists {
		return fmt.Errorf("default regime %q has no regulatory_regimes row", regime)
	}
	return nil
}
