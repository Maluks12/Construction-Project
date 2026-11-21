package dbtest

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"construction_db/internal/platform/containertest"
	"construction_db/internal/platform/database"
)

// MaxConns is the app pool's size in tests: fixed, so tests that fill the
// pool behave the same on every machine.
const MaxConns = 10

// Bounds make a lock wait fail in seconds, Postgres naming the lock, instead
// of hanging until the package timeout. Not under libfaketime (StartAt with
// an offset): there Postgres's timers fired early, an idle limit ending
// pooled connections (25P03) and a 10 s lock_timeout firing after about 1 s.
var Bounds = database.Bounds{LockTimeout: 10 * time.Second}

// Start runs Postgres with every migration applied. app connects like the API
// (as siteproof_app, under RLS); admin is the superuser, for fixtures the app
// may not write.
func Start(ctx context.Context) (app, admin *pgxpool.Pool, cleanup func(), err error) {
	return StartAt(ctx, 0)
}

// StartAt is Start with the database's clock moved by offset (whole seconds),
// so now() agrees with a test clock.Offset(offset): the app tests run as if
// at another hour (SITEPROOF_TEST_SAST). A non-zero offset builds
// faketime/Dockerfile (Postgres with libfaketime) and needs network once.
func StartAt(ctx context.Context, offset time.Duration) (app, admin *pgxpool.Pool, cleanup func(), err error) {
	files, err := migrationFiles()
	if err != nil {
		return nil, nil, nil, err
	}
	opts := []testcontainers.ContainerCustomizer{
		postgres.WithDatabase("siteproof_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.WithOrderedInitScripts(files...),
		postgres.BasicWaitStrategies(),
	}
	if offset != 0 {
		dir, err := sourceDir()
		if err != nil {
			return nil, nil, nil, err
		}
		opts = append(opts,
			testcontainers.WithDockerfile(testcontainers.FromDockerfile{
				Context: filepath.Join(dir, "faketime"), Repo: "siteproof-postgres-faketime", Tag: "16", KeepImage: true,
			}),
			// The postgres module always names its image; a build needs none.
			testcontainers.CustomizeRequestOption(func(req *testcontainers.GenericContainerRequest) error {
				req.Image = ""
				return nil
			}),
			testcontainers.WithEnv(map[string]string{
				"LD_PRELOAD": "/usr/lib/faketime/libfaketimeMT.so.1",
				"FAKETIME":   fmt.Sprintf("%+d", int64(offset/time.Second)),
				// Postgres forks: libfaketime's shared-memory lock fails there.
				"FAKETIME_DISABLE_SHM": "1",
				"DONT_FAKE_MONOTONIC":  "1",
			}))
	}

	ctr, err := containertest.StartWithRetry(ctx, "postgres", containertest.RetryWait,
		func(ctx context.Context) (*postgres.PostgresContainer, error) {
			return postgres.Run(ctx, "postgres:16-alpine", opts...)
		},
		func(c *postgres.PostgresContainer) { _ = testcontainers.TerminateContainer(c) },
	)
	if err != nil {
		return nil, nil, nil, err
	}

	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = testcontainers.TerminateContainer(ctr)
		return nil, nil, nil, fmt.Errorf("connection string: %w", err)
	}

	bounds := Bounds
	if offset != 0 {
		bounds = database.Bounds{}
	}
	app, err = database.Connect(ctx, url, MaxConns, bounds)
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

func sourceDir() (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("locate dbtest.go")
	}
	return filepath.Dir(thisFile), nil
}

func migrationFiles() ([]string, error) {
	dir, err := sourceDir()
	if err != nil {
		return nil, err
	}

	pattern := filepath.Join(dir, "..", "..", "..", "migrations", "*.up.sql")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("find migrations: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no migrations match %s", pattern)
	}
	return files, nil
}
