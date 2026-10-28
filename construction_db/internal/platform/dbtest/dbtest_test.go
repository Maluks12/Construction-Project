package dbtest

import (
	"context"
	"testing"
)

func TestStart(t *testing.T) {
	ctx := context.Background()

	pool, _, cleanup, err := Start(ctx)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer cleanup()

	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM regulatory_regimes`).Scan(&n); err != nil {
		t.Fatalf("query regimes: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 seeded regime, got %d", n)
	}

	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.refresh_tokens') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatalf("check refresh_tokens: %v", err)
	}
	if !exists {
		t.Fatal("refresh_tokens table missing: not every migration ran")
	}
}
