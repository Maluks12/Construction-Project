package app

import (
	"encoding/json"
	"net/http"
	"testing"

	"construction_db/internal/core/auth"
)

// Sessions run with tenant {UserID} only, so these exercise the sessions
// policy's user_id branch in both USING and WITH CHECK.
func TestSessions(t *testing.T) {
	router := newRouter(t)

	t.Run("logout", func(t *testing.T) {
		token := signup(t, router, "Logout Co", "owner@logout.test")
		if rec := do(t, router, http.MethodPost, "/v1/auth/logout", token, nil); rec.Code != http.StatusNoContent {
			t.Fatalf("logout: got %d (body: %s)", rec.Code, rec.Body.String())
		}
		if rec := get(t, router, "/v1/me", token); rec.Code != http.StatusUnauthorized {
			t.Fatalf("me after logout: got %d, want 401", rec.Code)
		}
	})

	t.Run("revoke a session in another org", func(t *testing.T) {
		ownerA := signup(t, router, "Sessions A", "owner@sessions-a.test")
		ownB := signup(t, router, "Sessions B", "u@sessions-b.test")
		orgA, orgB := me(t, router, ownerA).OrgID, me(t, router, ownB).OrgID

		invite := sendInvite(t, router, ownerA, map[string]any{"full_name": "U", "email": "u@sessions-b.test", "role": auth.RoleSupervisor})
		acceptInvite(t, router, invite.Token, "correcthorse1", http.StatusOK)

		inA := loginTokensFor(t, router, "u@sessions-b.test", orgA).AccessToken
		inB := loginTokensFor(t, router, "u@sessions-b.test", orgB).AccessToken
		sessionB := me(t, router, inB).SessionID

		if !listsSession(t, router, inA, sessionB) {
			t.Fatalf("org A token does not list the user's org B session %s", sessionB)
		}

		if rec := do(t, router, http.MethodDelete, "/v1/auth/sessions/"+sessionB, inA, nil); rec.Code != http.StatusNoContent {
			t.Fatalf("revoke org B session from org A: got %d (body: %s)", rec.Code, rec.Body.String())
		}
		if rec := get(t, router, "/v1/me", inB); rec.Code != http.StatusUnauthorized {
			t.Errorf("org B token after revoke: got %d, want 401", rec.Code)
		}
		if rec := get(t, router, "/v1/me", inA); rec.Code != http.StatusOK {
			t.Errorf("org A token after revoke: got %d, want 200", rec.Code)
		}
	})
}

func listsSession(t *testing.T, router http.Handler, token, sessionID string) bool {
	t.Helper()
	rec := get(t, router, "/v1/auth/sessions", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("list sessions: got %d (body: %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Sessions []auth.SessionResponse `json:"sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	for _, s := range resp.Sessions {
		if s.ID == sessionID {
			return true
		}
	}
	return false
}
