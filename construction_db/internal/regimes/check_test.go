package regimes

import (
	"context"
	"strings"
	"testing"

	"construction_db/internal/platform/dbtest"
)

// The only test in this package that needs Docker: it runs the real
// migrations, so it checks the embedded data against the actual seed rows.
func TestCheckDatabase(t *testing.T) {
	ctx := context.Background()

	pool, admin, cleanup, err := dbtest.Start(ctx)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer cleanup()

	embedded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	t.Run("embedded data matches the migrations", func(t *testing.T) {
		if err := CheckDatabase(ctx, pool, embedded); err != nil {
			t.Fatalf("CheckDatabase: %v", err)
		}
	})

	// The UPDATE runs inside a transaction that is rolled back, so later
	// subtests still see the seeded row.
	t.Run("version mismatch", func(t *testing.T) {
		// admin: the app role can only read regulatory_regimes.
		tx, err := admin.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback(ctx)

		tag, err := tx.Exec(ctx, `UPDATE regulatory_regimes SET version = '2099' WHERE code = 'za_construction_2014'`)
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if tag.RowsAffected() != 1 {
			t.Fatalf("update changed %d rows, want 1", tag.RowsAffected())
		}
		err = CheckDatabase(ctx, tx, embedded)
		want := `za_construction_2014: database version "2099" but data set version "2014"`
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("error %v does not mention %q", err, want)
		}
	})

	t.Run("row without a data set", func(t *testing.T) {
		other, err := LoadFS(validFS())
		if err != nil {
			t.Fatalf("LoadFS: %v", err)
		}
		err = CheckDatabase(ctx, pool, other)
		want := "za_construction_2014: no data set"
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("error %v does not mention %q", err, want)
		}
	})

	t.Run("default regime row exists", func(t *testing.T) {
		if err := CheckDefaultRow(ctx, pool, "za_construction_2014"); err != nil {
			t.Fatalf("CheckDefaultRow: %v", err)
		}
	})

	t.Run("default regime row missing", func(t *testing.T) {
		err := CheckDefaultRow(ctx, pool, "test_regime")
		want := `default regime "test_regime" has no regulatory_regimes row`
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("error %v does not mention %q", err, want)
		}
	})

	t.Run("data set without a row is allowed", func(t *testing.T) {
		extra, err := LoadFS(validFS())
		if err != nil {
			t.Fatalf("LoadFS: %v", err)
		}
		za, _ := embedded.Get("za_construction_2014")
		extra.byCode["za_construction_2014"] = za

		if err := CheckDatabase(ctx, pool, extra); err != nil {
			t.Fatalf("CheckDatabase: %v", err)
		}
	})
}
