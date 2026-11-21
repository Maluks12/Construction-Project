package database

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AppRole is the role every API connection switches to, so row-level security
// applies even when DATABASE_URL is a superuser (migration 000008).
const AppRole = "siteproof_app"

// Bounds are a pool's Postgres session limits: how long a statement waits
// for a lock (lock_timeout, SQLSTATE 55P03, naming the lock) and how long a
// transaction may sit idle (idle_in_transaction_session_timeout). Zero is
// Postgres's default, no limit. Set per pool, so migrations and other
// connections don't inherit them.
type Bounds struct {
	LockTimeout       time.Duration
	IdleInTransaction time.Duration
}

var (
	// The API: a lock wait ends in 5 s, a 503 to retry. lock_timeout bounds
	// each wait, so one queued behind another waiter can take 10 s and meet
	// REQUEST_TIMEOUT's default instead: a 503 either way. A leaked
	// transaction can't hold its locks for long.
	APIBounds = Bounds{LockTimeout: 5 * time.Second, IdleInTransaction: time.Minute}
	// The worker: longer lock waits; no idle limit, because the export
	// builder's snapshot stays open while it streams a pack to S3.
	WorkerBounds = Bounds{LockTimeout: time.Minute}
)

// maxConns caps the pool. pgxpool's own default (max(4, CPUs)) varies by
// machine; a request that can't get a connection before its deadline is a 503.
func Connect(ctx context.Context, url string, maxConns int32, b Bounds) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = maxConns
	// Sent at connection start, so they hold for every statement on it.
	if b.LockTimeout > 0 {
		cfg.ConnConfig.RuntimeParams["lock_timeout"] = strconv.FormatInt(b.LockTimeout.Milliseconds(), 10)
	}
	if b.IdleInTransaction > 0 {
		cfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = strconv.FormatInt(b.IdleInTransaction.Milliseconds(), 10)
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		if _, err := conn.Exec(ctx, "SET ROLE "+AppRole); err != nil {
			return fmt.Errorf("set role %s (run migrations first?): %w", AppRole, err)
		}
		return nil
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}
