package app

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/platform/httpapi"
)

// holdPool takes every connection in the app pool until release is called.
func holdPool(t *testing.T) (release func()) {
	t.Helper()
	var held []*pgxpool.Conn
	for range testPool.Config().MaxConns {
		c, err := testPool.Acquire(context.Background())
		if err != nil {
			t.Fatalf("acquire: %v", err)
		}
		held = append(held, c)
	}
	return func() {
		for _, c := range held {
			c.Release()
		}
	}
}

// A request that can't get a pool connection before its deadline is busy,
// not broken: 503 with Retry-After.
func TestBusyIs503(t *testing.T) {
	// Sign up with the normal deadline: password hashing alone can take over
	// 300ms under -race. Only the request under test gets the short one.
	token := signup(t, newRouter(t), "Busy Co", "owner@busy-"+uuid.NewString()[:8]+".test")

	const timeout = 300 * time.Millisecond
	router, err := NewRouter(context.Background(), testPool, testSecret, testRegistry, testEdition, testStore, HTTPOptions{RequestTimeout: timeout})
	if err != nil {
		t.Fatalf("new router: %v", err)
	}

	t.Run("full pool", func(t *testing.T) {
		release := holdPool(t)
		start := time.Now()
		rec := do(t, router, http.MethodGet, "/v1/me", token, nil)
		elapsed := time.Since(start)
		release()

		if rec.Code != http.StatusServiceUnavailable || rec.Body.String() != `{"error":"server busy, try again"}` {
			t.Errorf("got %d %s, want 503 server busy", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Retry-After"); got != httpapi.RetryAfterSeconds {
			t.Errorf("Retry-After %q, want %q", got, httpapi.RetryAfterSeconds)
		}
		if elapsed < timeout || elapsed > timeout+time.Second {
			t.Errorf("answered after %s, want about the %s deadline", elapsed, timeout)
		}
	})

	t.Run("released pool", func(t *testing.T) {
		if rec := do(t, router, http.MethodGet, "/v1/me", token, nil); rec.Code != http.StatusOK {
			t.Errorf("got %d %s, want 200", rec.Code, rec.Body.String())
		}
	})
}

// A client that disconnects isn't a server error: through the real auth path,
// pgx refuses to begin with context.Canceled, logged at info, never error.
func TestClientGoneIsNotAServerError(t *testing.T) {
	router := newRouter(t)
	token := signup(t, router, "Gone Co", "owner@gone-"+uuid.NewString()[:8]+".test")

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	ctx, disconnect := context.WithCancel(context.Background())
	disconnect()
	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != httpapi.StatusClientClosedRequest {
		t.Errorf("status %d, want %d", rec.Code, httpapi.StatusClientClosedRequest)
	}
	if !strings.Contains(logs.String(), `"level":"INFO"`) || strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Errorf("logs %s, want an INFO entry and no ERROR", logs.String())
	}
}

// A real failure is still 500, not mistaken for busy.
func TestInternalErrorIs500(t *testing.T) {
	ctx := context.Background()
	router := newRouter(t)
	token := signup(t, router, "Broken Co", "owner@broken-"+uuid.NewString()[:8]+".test")

	// Auth reads sessions and memberships, not sites, so the request gets as
	// far as the sites query and fails there with permission denied.
	if _, err := testAdmin.Exec(ctx, `REVOKE SELECT ON sites FROM siteproof_app`); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	t.Cleanup(func() {
		if _, err := testAdmin.Exec(ctx, `GRANT SELECT ON sites TO siteproof_app`); err != nil {
			t.Errorf("restore grant: %v", err)
		}
	})

	rec := do(t, router, http.MethodGet, "/v1/sites", token, nil)
	if rec.Code != http.StatusInternalServerError || rec.Body.String() != `{"error":"internal error"}` {
		t.Errorf("got %d %s, want 500 internal error", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got != "" {
		t.Errorf("Retry-After %q on a 500, want none", got)
	}
}
