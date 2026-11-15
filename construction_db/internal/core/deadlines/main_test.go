package deadlines_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/platform/dbtest"
)

var (
	pool  *pgxpool.Pool // as siteproof_app
	admin *pgxpool.Pool // superuser
)

func TestMain(m *testing.M) {
	app, adm, cleanup, err := dbtest.Start(context.Background())
	if err != nil {
		log.Printf("start test database: %v", err)
		os.Exit(1)
	}
	pool, admin = app, adm

	code := m.Run()
	cleanup()
	os.Exit(code)
}
