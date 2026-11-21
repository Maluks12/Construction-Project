package database_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/platform/database"
)

// Each pool carries its own limits; nothing is set on the role or the
// database, so a connection made another way (migrate, psql, the admin pool
// here) keeps Postgres's defaults.
func TestPoolBounds(t *testing.T) {
	ctx := context.Background()
	url := admin.Config().ConnString()

	show := func(t *testing.T, p *pgxpool.Pool) (lock, idle string) {
		t.Helper()
		// In a transaction, as the API's and the export builder's queries run.
		tx, err := p.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if err := tx.QueryRow(ctx, "SELECT current_setting('lock_timeout'), current_setting('idle_in_transaction_session_timeout')").Scan(&lock, &idle); err != nil {
			t.Fatal(err)
		}
		return lock, idle
	}
	connect := func(t *testing.T, b database.Bounds) *pgxpool.Pool {
		t.Helper()
		p, err := database.Connect(ctx, url, 2, b)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(p.Close)
		return p
	}

	for _, tc := range []struct {
		name       string
		pool       func(t *testing.T) *pgxpool.Pool
		lock, idle string
	}{
		{"API", func(t *testing.T) *pgxpool.Pool { return connect(t, database.APIBounds) }, "5s", "1min"},
		{"worker (export builder)", func(t *testing.T) *pgxpool.Pool { return connect(t, database.WorkerBounds) }, "1min", "0"},
		{"plain connection, as migrate's", func(t *testing.T) *pgxpool.Pool { return admin }, "0", "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if lock, idle := show(t, tc.pool(t)); lock != tc.lock || idle != tc.idle {
				t.Errorf("lock_timeout %s, idle_in_transaction_session_timeout %s; want %s, %s", lock, idle, tc.lock, tc.idle)
			}
		})
	}

	// The API pool's limit isn't left behind on the role or the database.
	connect(t, database.APIBounds)
	var roleOrDB int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM pg_db_role_setting s
		WHERE array_to_string(s.setconfig, ',') ~ '(lock|idle_in_transaction_session)_timeout'`).Scan(&roleOrDB); err != nil {
		t.Fatal(err)
	}
	if roleOrDB != 0 {
		t.Errorf("%d role or database settings carry a timeout; want none", roleOrDB)
	}
}

// A wait past lock_timeout ends in SQLSTATE 55P03 at once. (A row lock's
// error also names the relation in Where; TestLockWaitIs503 in internal/app
// shows that, and the 503 a client gets.)
func TestLockTimeoutEndsTheWait(t *testing.T) {
	ctx := context.Background()
	p, err := database.Connect(ctx, admin.Config().ConnString(), 2, database.Bounds{LockTimeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	holder, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(ctx)
	if _, err := holder.Exec(ctx, "LOCK TABLE regulatory_regimes IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}

	// The deadline only stops this test hanging if lock_timeout is lost.
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	start := time.Now()
	var n int
	err = p.QueryRow(waitCtx, "SELECT count(*) FROM regulatory_regimes").Scan(&n)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		t.Fatalf("got %v, want SQLSTATE 55P03", err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("gave up after %s, want about 200ms", took)
	}
}
