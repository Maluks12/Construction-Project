package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Tenant is the row-level security context for one transaction. Either field may be empty.
type Tenant struct {
	OrgID  string
	UserID string
}

// SetTenant sets the context transaction-locally (set_config(..., true)), so it
// ends with tx and never leaks to the next user of the pooled connection.
func SetTenant(ctx context.Context, tx pgx.Tx, t Tenant) error {
	if _, err := tx.Exec(ctx, `SELECT set_config('app.org_id', $1, true), set_config('app.user_id', $2, true)`, t.OrgID, t.UserID); err != nil {
		return fmt.Errorf("set tenant: %w", err)
	}
	return nil
}

// WithTenant runs fn in a transaction with tenant t. It commits only if fn
// returns nil, and returns fn's error unwrapped so callers can match sentinels.
func WithTenant(ctx context.Context, pool *pgxpool.Pool, t Tenant, fn func(pgx.Tx) error) error {
	return withTenant(ctx, pool, t, pgx.TxOptions{}, fn)
}

// WithTenantSnapshot is WithTenant for reads that must agree with each other:
// read-only REPEATABLE READ, so every query sees the database as it was at
// the first one, and commits by others in between are invisible.
func WithTenantSnapshot(ctx context.Context, pool *pgxpool.Pool, t Tenant, fn func(pgx.Tx) error) error {
	return withTenant(ctx, pool, t, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, fn)
}

func withTenant(ctx context.Context, pool *pgxpool.Pool, t Tenant, opts pgx.TxOptions, fn func(pgx.Tx) error) error {
	tx, err := pool.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("with tenant: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := SetTenant(ctx, tx, t); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("with tenant: %w", err)
	}
	return nil
}
