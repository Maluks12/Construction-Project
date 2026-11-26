package app

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"construction_db/internal/core/auth"
)

func TestRefreshRotationAndReuse(t *testing.T) {
	sfx := uuid.NewString()[:8] // fresh per run, so -count=N works
	router := newRouter(t)
	signup(t, router, "Refresh Co", "owner@refresh-"+sfx+".test")

	first := loginTokens(t, router, "owner@refresh-"+sfx+".test")
	second := refresh(t, router, first.RefreshToken, http.StatusOK)

	if rec := get(t, router, "/v1/me", second.AccessToken); rec.Code != http.StatusOK {
		t.Fatalf("me with refreshed token: got %d", rec.Code)
	}
	if !second.RefreshExpiresAt.Equal(first.RefreshExpiresAt) || first.RefreshExpiresAt.Before(time.Now().Add(29*24*time.Hour)) {
		t.Errorf("refresh_expires_at %s then %s: the session's end, never moved", first.RefreshExpiresAt, second.RefreshExpiresAt)
	}

	// Once the successor was used, replaying a used token is real reuse: it
	// revokes the whole session (and the revoke must commit).
	third := refresh(t, router, second.RefreshToken, http.StatusOK)
	refresh(t, router, first.RefreshToken, http.StatusUnauthorized)
	refresh(t, router, third.RefreshToken, http.StatusUnauthorized)
	if rec := get(t, router, "/v1/me", third.AccessToken); rec.Code != http.StatusUnauthorized {
		t.Fatalf("me after reuse: got %d, want 401", rec.Code)
	}

	refresh(t, router, "not-a-real-token", http.StatusUnauthorized)
}

// SYNC A.3: a phone whose refresh succeeded but lost the response retries
// with the same token. Within auth.RefreshGrace, while the new token is
// unused, it gets the same pair back; the session lives on.
func TestRefreshGrace(t *testing.T) {
	if auth.RefreshGrace != 5*time.Minute {
		t.Fatalf("RefreshGrace is %s; sync.md and the handover say 5 minutes", auth.RefreshGrace)
	}
	router := newRouter(t)
	email := "owner@grace-" + uuid.NewString()[:8] + ".test"
	signup(t, router, "Grace Co", email)

	t.Run("an exact retry gets the same pair", func(t *testing.T) {
		p0 := loginTokens(t, router, email)
		p1 := refresh(t, router, p0.RefreshToken, http.StatusOK)
		again := refresh(t, router, p0.RefreshToken, http.StatusOK)
		if again != p1 {
			t.Fatalf("retry answered another pair:\n%+v\n%+v", again, p1)
		}
		p2 := refresh(t, router, p1.RefreshToken, http.StatusOK)
		if rec := get(t, router, "/v1/me", p2.AccessToken); rec.Code != http.StatusOK {
			t.Errorf("session after the retry: %d", rec.Code)
		}
	})

	t.Run("just inside the window", func(t *testing.T) {
		p0 := loginTokens(t, router, email)
		p1 := refresh(t, router, p0.RefreshToken, http.StatusOK)
		backdateUse(t, p0.RefreshToken, auth.RefreshGrace-10*time.Second)
		if again := refresh(t, router, p0.RefreshToken, http.StatusOK); again != p1 {
			t.Error("another pair")
		}
	})

	t.Run("after the window it is reuse", func(t *testing.T) {
		p0 := loginTokens(t, router, email)
		p1 := refresh(t, router, p0.RefreshToken, http.StatusOK)
		backdateUse(t, p0.RefreshToken, auth.RefreshGrace+time.Second)
		refresh(t, router, p0.RefreshToken, http.StatusUnauthorized)
		if rec := get(t, router, "/v1/me", p1.AccessToken); rec.Code != http.StatusUnauthorized {
			t.Errorf("session after a late replay: %d, want revoked", rec.Code)
		}
	})

	t.Run("another token's box can't be opened", func(t *testing.T) {
		p0 := loginTokens(t, router, email)
		refresh(t, router, p0.RefreshToken, http.StatusOK)
		other := loginTokens(t, router, email)
		// Give p0's row other's hash: other's plaintext now finds p0's box,
		// which its key can't open, so the replay fails and revokes.
		if _, err := testAdmin.Exec(context.Background(), `UPDATE refresh_tokens SET token_hash = token_hash || 'x' WHERE token_hash = $1`,
			auth.HashRefreshToken(other.RefreshToken)); err != nil {
			t.Fatal(err)
		}
		if _, err := testAdmin.Exec(context.Background(), `UPDATE refresh_tokens SET token_hash = $2 WHERE token_hash = $1`,
			auth.HashRefreshToken(p0.RefreshToken), auth.HashRefreshToken(other.RefreshToken)); err != nil {
			t.Fatal(err)
		}
		refresh(t, router, other.RefreshToken, http.StatusUnauthorized)
	})
}

// Two refreshes with one token at once (a phone retrying while the first is
// still running). Forced: a held row lock queues both; the one that marks the
// token rotates it, the other then finds it used and gets the same pair.
func TestRefreshConcurrentRetry(t *testing.T) {
	router := newRouter(t)
	email := "owner@twice-" + uuid.NewString()[:8] + ".test"
	signup(t, router, "Twice Co", email)
	p0 := loginTokens(t, router, email)
	ctx := context.Background()

	hold, err := testAdmin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Rollback(ctx)
	if _, err := hold.Exec(ctx, `SELECT 1 FROM refresh_tokens WHERE token_hash = $1 FOR UPDATE`, auth.HashRefreshToken(p0.RefreshToken)); err != nil {
		t.Fatal(err)
	}
	pairs := make(chan auth.TokenPair, 2)
	for range 2 {
		go func() { pairs <- refresh(t, router, p0.RefreshToken, http.StatusOK) }()
	}
	waitForWaiters(t, "UPDATE refresh_tokens", 2)
	if err := hold.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	a, b := <-pairs, <-pairs
	if a != b {
		t.Fatalf("two pairs:\n%+v\n%+v", a, b)
	}
	if rec := get(t, router, "/v1/me", a.AccessToken); rec.Code != http.StatusOK {
		t.Errorf("session: %d", rec.Code)
	}
}

func backdateUse(t *testing.T, token string, by time.Duration) {
	t.Helper()
	tag, err := testAdmin.Exec(context.Background(), `UPDATE refresh_tokens SET used_at = used_at - make_interval(secs => $2) WHERE token_hash = $1`,
		auth.HashRefreshToken(token), by.Seconds())
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("backdate: %v (%d rows)", err, tag.RowsAffected())
	}
}

func loginTokens(t *testing.T, router http.Handler, email string) auth.TokenPair {
	t.Helper()
	return loginTokensFor(t, router, email, "")
}

// loginTokensFor logs in to orgID, which a user in several orgs must name.
func loginTokensFor(t *testing.T, router http.Handler, email, orgID string) auth.TokenPair {
	t.Helper()
	body := map[string]string{"email": email, "password": "correcthorse1", "device_name": "test"}
	if orgID != "" {
		body["org_id"] = orgID
	}
	rec := do(t, router, http.MethodPost, "/v1/auth/login", "", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: got %d (body: %s)", rec.Code, rec.Body.String())
	}
	var p auth.TokenPair
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	return p
}

func refresh(t *testing.T, router http.Handler, token string, want int) auth.TokenPair {
	t.Helper()
	rec := do(t, router, http.MethodPost, "/v1/auth/refresh", "", map[string]string{"refresh_token": token})
	if rec.Code != want {
		t.Fatalf("refresh: got %d, want %d (body: %s)", rec.Code, want, rec.Body.String())
	}
	var p auth.TokenPair
	if want == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatalf("decode refresh: %v", err)
		}
	}
	return p
}
