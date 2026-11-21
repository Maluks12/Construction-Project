package app

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/platform/database"
	"construction_db/internal/platform/dbtest"
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
		// The upper bound only has to tell this router's deadline from a
		// longer one (the 10s production default, or a stray per-handler
		// timeout), so it is generous: a loaded machine running every
		// package's containers has taken 1.7s here.
		if elapsed < timeout || elapsed > timeout+5*time.Second {
			t.Errorf("answered after %s, want the %s deadline, not a longer one", elapsed, timeout)
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

// A lock wait past the API pool's lock_timeout is busy, not broken. Forced:
// the test holds a site's chain head (as a slow append would) while a burst
// of uploads to that site completes. Each waits its 5s, then answers 503
// with Retry-After, well inside the request deadline, and the log names the
// lock. Released, the same completes seal one unbroken chain.
func TestLockWaitIs503(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFixture(t, "lockwait")

	apiPool, err := database.Connect(ctx, testPool.Config().ConnString(), dbtest.MaxConns, database.APIBounds)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(apiPool.Close)
	const deadline = 30 * time.Second
	router, err := NewRouter(ctx, apiPool, testSecret, testRegistry, testEdition, testStore, HTTPOptions{RequestTimeout: deadline})
	if err != nil {
		t.Fatalf("new router: %v", err)
	}

	// The head row exists once the site has one sealed photo.
	first := declareAndUpload(t, router, f.owner, f.siteA, testImage(t, "jpeg", 7, 7), "image/jpeg")
	wantStatus(t, do(t, router, http.MethodPost, completePath(f.siteA, first), f.owner, nil), http.StatusCreated)

	const burst = 5
	ids := make([]string, burst)
	for i := range ids {
		ids[i] = declareAndUpload(t, router, f.owner, f.siteA, testImage(t, "jpeg", 20+i, 9), "image/jpeg")
	}

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	hold := holdOpen(t, f)
	if _, err := hold.Exec(ctx, `SELECT 1 FROM site_chain_heads WHERE site_id = $1 FOR UPDATE`, f.siteA); err != nil {
		t.Fatal(err)
	}

	type result struct {
		rec     *httptest.ResponseRecorder
		elapsed time.Duration
	}
	results := make([]result, burst)
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			rec, _ := request(router, http.MethodPost, completePath(f.siteA, id), f.owner, nil)
			results[i] = result{rec, time.Since(start)}
		}()
	}
	wg.Wait()
	if err := hold.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	lock := database.APIBounds.LockTimeout
	for i, r := range results {
		if r.rec.Code != http.StatusServiceUnavailable || r.rec.Body.String() != `{"error":"server busy, try again"}` {
			t.Errorf("complete %d: %d %s, want 503 server busy", i, r.rec.Code, r.rec.Body.String())
		}
		if got := r.rec.Header().Get("Retry-After"); got != httpapi.RetryAfterSeconds {
			t.Errorf("complete %d: Retry-After %q, want %q", i, got, httpapi.RetryAfterSeconds)
		}
		// lock_timeout, not the request deadline, ended it. It bounds each
		// wait: one queued behind another waiter waits for its turn at the
		// row, then for the row, so up to twice.
		if r.elapsed < lock || r.elapsed > 2*lock+5*time.Second {
			t.Errorf("complete %d answered after %s, want %s to %s (deadline %s)", i, r.elapsed, lock, 2*lock, deadline)
		}
	}
	// Each a warning for SQLSTATE 55P03; the one waiting on the row itself
	// names it (the others waited in its queue, which Postgres doesn't name).
	if n := strings.Count(logs.String(), `(SQLSTATE 55P03)","waited_on":`); n != burst || strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Errorf("logs %s: %d lock timeouts, want %d and no ERROR", logs.String(), n, burst)
	}
	named := regexp.MustCompile(`"waited_on":"while locking tuple \(\d+,\d+\) in relation \\"site_chain_heads\\""`)
	if !named.MatchString(logs.String()) {
		t.Errorf("logs %s, want one naming site_chain_heads", logs.String())
	}

	// Still pending, so the client's retry seals them.
	for _, id := range ids {
		wantStatus(t, do(t, router, http.MethodPost, completePath(f.siteA, id), f.owner, nil), http.StatusCreated)
	}
	wantUnbrokenChain(t, f.siteA, burst+1)
}
