package database_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/platform/database"
	"construction_db/internal/platform/dbtest"
)

const orgA = "00000000-0000-0000-0000-00000000000a"

func TestTenant(t *testing.T) {
	ctx := context.Background()
	pool, _, cleanup, err := dbtest.Start(ctx)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer cleanup()

	t.Run("connections run as the app role", func(t *testing.T) {
		var role string
		if err := pool.QueryRow(ctx, `SELECT current_user`).Scan(&role); err != nil {
			t.Fatalf("current_user: %v", err)
		}
		if role != database.AppRole {
			t.Fatalf("current_user = %q, want %q", role, database.AppRole)
		}
	})

	t.Run("tenant is set inside and gone after", func(t *testing.T) {
		err := database.WithTenant(ctx, pool, database.Tenant{OrgID: orgA}, func(tx pgx.Tx) error {
			if got := appOrgID(t, tx); got == nil || *got != orgA {
				t.Errorf("inside: app_org_id() = %v, want %s", got, orgA)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WithTenant: %v", err)
		}

		// A one-connection pool guarantees the next transaction reuses the same connection.
		single := singleConnPool(t, pool)
		_ = database.WithTenant(ctx, single, database.Tenant{OrgID: orgA}, func(pgx.Tx) error { return nil })
		err = database.WithTenant(ctx, single, database.Tenant{}, func(tx pgx.Tx) error {
			if got := appOrgID(t, tx); got != nil {
				t.Errorf("next transaction: app_org_id() = %s, want NULL", *got)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WithTenant: %v", err)
		}
	})

	t.Run("an error from fn rolls back and is returned as is", func(t *testing.T) {
		boom := errors.New("boom")
		err := database.WithTenant(ctx, pool, database.Tenant{}, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `CREATE TEMP TABLE rollback_probe (id int)`); err != nil {
				return err
			}
			return boom
		})
		if err != boom {
			t.Fatalf("err = %v, want boom unwrapped", err)
		}
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass('pg_temp.rollback_probe') IS NOT NULL`).Scan(&exists); err != nil {
			t.Fatalf("check: %v", err)
		}
		if exists {
			t.Fatal("rollback_probe exists: the transaction was committed")
		}
	})
}

func appOrgID(t *testing.T, tx pgx.Tx) *string {
	t.Helper()
	var id *string
	if err := tx.QueryRow(context.Background(), `SELECT app_org_id()::text`).Scan(&id); err != nil {
		t.Fatalf("app_org_id: %v", err)
	}
	return id
}

func singleConnPool(t *testing.T, pool *pgxpool.Pool) *pgxpool.Pool {
	t.Helper()
	cfg := pool.Config()
	cfg.MaxConns = 1
	single, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("single pool: %v", err)
	}
	t.Cleanup(single.Close)
	return single
}
