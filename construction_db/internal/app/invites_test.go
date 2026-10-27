package app

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/invites"
)

func TestInvites(t *testing.T) {
	router := NewRouter(testPool, testSecret)

	ownerToken := signup(t, router, "Invite Co", "owner@invites.test")
	otherToken := signup(t, router, "Invite Other Co", "owner@invites-other.test")

	inviteMember(t, router, ownerToken, "she@invites.test", auth.RoleSHEOfficer)
	inviteMember(t, router, ownerToken, "sup@invites.test", auth.RoleSupervisor)
	sheToken := login(t, router, "she@invites.test")
	supToken := login(t, router, "sup@invites.test")

	sipho := sendInvite(t, router, ownerToken, map[string]any{"full_name": "Sipho Dlamini", "mobile": "+27820000001", "role": auth.RoleSupervisor})
	revoked := sendInvite(t, router, ownerToken, map[string]any{"full_name": "Revoked", "mobile": "+27820000002", "role": auth.RoleBookkeeper})
	expired := sendInvite(t, router, ownerToken, map[string]any{"full_name": "Late", "mobile": "+27820000003", "role": auth.RoleBookkeeper})
	existing := sendInvite(t, router, ownerToken, map[string]any{"full_name": "ignored", "email": "owner@invites-other.test", "role": auth.RoleSHEOfficer})

	if _, err := testPool.Exec(context.Background(),
		`UPDATE invitations SET expires_at = now() - interval '1 minute' WHERE id = $1`, expired.ID); err != nil {
		t.Fatalf("expire invite: %v", err)
	}

	t.Run("owner sees pending invites, other org sees none", func(t *testing.T) {
		if n := len(listInvites(t, router, ownerToken)); n != 3 { // sipho, revoked, existing (expired is hidden)
			t.Fatalf("owner: want 3 pending, got %d", n)
		}
		if n := len(listInvites(t, router, otherToken)); n != 0 {
			t.Fatalf("other org: want 0 pending, got %d", n)
		}
	})

	tests := []struct {
		name   string
		token  string
		method string
		path   string
		body   any
		want   int
	}{
		{"no token", "", http.MethodPost, "/v1/invites", map[string]any{"full_name": "X", "mobile": "+27820000009", "role": "supervisor"}, http.StatusUnauthorized},
		{"supervisor cannot invite", supToken, http.MethodPost, "/v1/invites", map[string]any{"full_name": "X", "mobile": "+27820000009", "role": "supervisor"}, http.StatusForbidden},
		{"supervisor cannot list", supToken, http.MethodGet, "/v1/invites", nil, http.StatusForbidden},
		{"SHE officer cannot invite an owner", sheToken, http.MethodPost, "/v1/invites", map[string]any{"full_name": "X", "mobile": "+27820000009", "role": "owner"}, http.StatusForbidden},
		{"no email or mobile", ownerToken, http.MethodPost, "/v1/invites", map[string]any{"full_name": "X", "role": "supervisor"}, http.StatusBadRequest},
		{"blank name", ownerToken, http.MethodPost, "/v1/invites", map[string]any{"full_name": "   ", "mobile": "+27820000009", "role": "supervisor"}, http.StatusBadRequest},
		{"mobile not in +27 format", ownerToken, http.MethodPost, "/v1/invites", map[string]any{"full_name": "X", "mobile": "0820000009", "role": "supervisor"}, http.StatusBadRequest},
		{"already an active member", ownerToken, http.MethodPost, "/v1/invites", map[string]any{"full_name": "X", "email": "SUP@invites.test", "role": "bookkeeper"}, http.StatusConflict},
		{"email and mobile of different people", ownerToken, http.MethodPost, "/v1/invites", map[string]any{"full_name": "X", "email": "sup@invites.test", "mobile": "+27820000001", "role": "bookkeeper"}, http.StatusConflict},
		{"other org cannot revoke", otherToken, http.MethodDelete, "/v1/invites/" + revoked.ID, nil, http.StatusNotFound},
		{"malformed invite id", ownerToken, http.MethodDelete, "/v1/invites/not-a-uuid", nil, http.StatusNotFound},
		{"owner revokes", ownerToken, http.MethodDelete, "/v1/invites/" + revoked.ID, nil, http.StatusNoContent},
		{"revoke twice", ownerToken, http.MethodDelete, "/v1/invites/" + revoked.ID, nil, http.StatusNotFound},

		{"preview bad token", "", http.MethodPost, "/v1/invites/preview", map[string]any{"token": "garbage"}, http.StatusBadRequest},
		{"preview expired", "", http.MethodPost, "/v1/invites/preview", map[string]any{"token": expired.Token}, http.StatusBadRequest},
		{"accept revoked", "", http.MethodPost, "/v1/invites/accept", map[string]any{"token": revoked.Token, "password": "correcthorse1"}, http.StatusBadRequest},
		{"accept expired", "", http.MethodPost, "/v1/invites/accept", map[string]any{"token": expired.Token, "password": "correcthorse1"}, http.StatusBadRequest},
		{"accept with short password", "", http.MethodPost, "/v1/invites/accept", map[string]any{"token": sipho.Token, "password": "short"}, http.StatusBadRequest},
		{"existing account, wrong password", "", http.MethodPost, "/v1/invites/accept", map[string]any{"token": existing.Token, "password": "not-their-password"}, http.StatusUnauthorized},
		{"login before accepting", "", http.MethodPost, "/v1/auth/login", map[string]any{"mobile": "+27820000001", "password": "correcthorse1"}, http.StatusUnauthorized},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, router, tc.method, tc.path, tc.token, tc.body)
			if rec.Code != tc.want {
				t.Fatalf("got %d, want %d (body: %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}

	t.Run("new person: preview, accept, then log in by mobile", func(t *testing.T) {
		rec := do(t, router, http.MethodPost, "/v1/invites/preview", "", map[string]any{"token": sipho.Token})
		if rec.Code != http.StatusOK {
			t.Fatalf("preview: got %d (body: %s)", rec.Code, rec.Body.String())
		}
		var p invites.InvitePreview
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatalf("decode preview: %v", err)
		}
		if p.OrgName != "Invite Co" || p.Role != auth.RoleSupervisor || p.HasPassword {
			t.Fatalf("preview came back wrong: %+v", p)
		}

		tokens := acceptInvite(t, router, sipho.Token, "correcthorse1", http.StatusOK)
		if got := me(t, router, tokens.AccessToken); got.UserID != sipho.UserID {
			t.Fatalf("accept logged in user %s, want %s", got.UserID, sipho.UserID)
		}

		acceptInvite(t, router, sipho.Token, "correcthorse1", http.StatusBadRequest) // can't use it twice

		rec = do(t, router, http.MethodPost, "/v1/auth/login", "", map[string]any{"mobile": "+27820000001", "password": "correcthorse1"})
		if rec.Code != http.StatusOK {
			t.Fatalf("login by mobile: got %d (body: %s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("existing account joins with its own password", func(t *testing.T) {
		tokens := acceptInvite(t, router, existing.Token, "correcthorse1", http.StatusOK)
		if got := me(t, router, tokens.AccessToken); got.OrgID == me(t, router, otherToken).OrgID {
			t.Fatal("accept should log in to the inviting org, not the old one")
		}
	})

	t.Run("pending list is empty after accepts and revokes", func(t *testing.T) {
		if n := len(listInvites(t, router, ownerToken)); n != 0 {
			t.Fatalf("want 0 pending, got %d", n)
		}
	})
}

func sendInvite(t *testing.T, router http.Handler, token string, body map[string]any) invites.InviteResponse {
	t.Helper()

	rec := do(t, router, http.MethodPost, "/v1/invites", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("send invite: got %d (body: %s)", rec.Code, rec.Body.String())
	}
	var resp invites.InviteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode invite: %v", err)
	}
	return resp
}

func listInvites(t *testing.T, router http.Handler, token string) []invites.PendingInvite {
	t.Helper()

	rec := do(t, router, http.MethodGet, "/v1/invites", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list invites: got %d (body: %s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Invites []invites.PendingInvite `json:"invites"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode invites: %v", err)
	}
	return body.Invites
}

func acceptInvite(t *testing.T, router http.Handler, token, password string, want int) auth.TokenPair {
	t.Helper()

	rec := do(t, router, http.MethodPost, "/v1/invites/accept", "", map[string]any{
		"token":       token,
		"password":    password,
		"device_name": "test",
	})
	if rec.Code != want {
		t.Fatalf("accept: got %d, want %d (body: %s)", rec.Code, want, rec.Body.String())
	}
	var tokens auth.TokenPair
	if want == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &tokens); err != nil {
			t.Fatalf("decode tokens: %v", err)
		}
	}
	return tokens
}
