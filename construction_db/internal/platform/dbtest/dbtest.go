package dbtest

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"construction_db/internal/platform/database"
)

// MaxConns is the app pool's size in tests: fixed, so tests that fill the
// pool behave the same on every machine.
const MaxConns = 10

// Start runs Postgres with every migration applied. app connects like the API
// (as siteproof_app, under RLS); admin is the superuser, for fixtures the app
// may not write.
func Start(ctx context.Context) (app, admin *pgxpool.Pool, cleanup func(), err error) {
	files, err := migrationFiles()
	if err != nil {
		return nil, nil, nil, err
	}

	ctr, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("siteproof_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.WithOrderedInitScripts(files...),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		_ = testcontainers.TerminateContainer(ctr)
		return nil, nil, nil, fmt.Errorf("start postgres: %w", err)
	}

	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = testcontainers.TerminateContainer(ctr)
		return nil, nil, nil, fmt.Errorf("connection string: %w", err)
	}

	app, err = database.Connect(ctx, url, MaxConns)
	if err != nil {
		_ = testcontainers.TerminateContainer(ctr)
		return nil, nil, nil, fmt.Errorf("connect: %w", err)
	}

	admin, err = pgxpool.New(ctx, url)
	if err != nil {
		app.Close()
		_ = testcontainers.TerminateContainer(ctr)
		return nil, nil, nil, fmt.Errorf("connect admin: %w", err)
	}

	cleanup = func() {
		app.Close()
		admin.Close()
		_ = testcontainers.TerminateContainer(ctr)
	}
	return app, admin, cleanup, nil
}

func migrationFiles() ([]string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return nil, errors.New("locate dbtest.go")
	}

	pattern := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "migrations", "*.up.sql")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("find migrations: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no migrations match %s", pattern)
	}
	return files, nil
}
