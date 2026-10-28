package app

import (
	"encoding/json"
	"net/http"
	"testing"

	"construction_db/internal/core/auth"
)

func TestRefreshRotationAndReuse(t *testing.T) {
	router := newRouter(t)
	signup(t, router, "Refresh Co", "owner@refresh.test")

	first := loginTokens(t, router, "owner@refresh.test")
	second := refresh(t, router, first.RefreshToken, http.StatusOK)

	if rec := get(t, router, "/v1/me", second.AccessToken); rec.Code != http.StatusOK {
		t.Fatalf("me with refreshed token: got %d", rec.Code)
	}

	// Replaying a used refresh token revokes the whole session (and the revoke must commit).
	refresh(t, router, first.RefreshToken, http.StatusUnauthorized)
	refresh(t, router, second.RefreshToken, http.StatusUnauthorized)
	if rec := get(t, router, "/v1/me", second.AccessToken); rec.Code != http.StatusUnauthorized {
		t.Fatalf("me after reuse: got %d, want 401", rec.Code)
	}

	refresh(t, router, "not-a-real-token", http.StatusUnauthorized)
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
