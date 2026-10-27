package app

import (
	"bytes"
	"construction_db/internal/core/invites"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/sites"
)

func TestSiteScoping(t *testing.T) {
	router := NewRouter(testPool, testSecret)

	ownerToken := signup(t, router, "Scope Co", "owner@scoping.test")
	otherToken := signup(t, router, "Other Co", "owner@other-scoping.test")

	other := me(t, router, otherToken)

	siteA := listSites(t, router, ownerToken)[0].ID
	siteB := createSite(t, router, ownerToken, "Unassigned Site")
	otherSite := listSites(t, router, otherToken)[0].ID

	supervisorID := inviteMember(t, router, ownerToken, "foreman@scoping.test", auth.RoleSupervisor)
	supToken := login(t, router, "foreman@scoping.test")

	if rec := do(t, router, http.MethodPut, "/v1/sites/"+siteA+"/members/"+supervisorID, ownerToken, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("assign supervisor: got %d (body: %s)", rec.Code, rec.Body.String())
	}

	t.Run("supervisor lists only assigned site", func(t *testing.T) {
		got := listSites(t, router, supToken)
		if len(got) != 1 || got[0].ID != siteA {
			t.Fatalf("want only site %s, got %+v", siteA, got)
		}
	})

	t.Run("member list shows the supervisor", func(t *testing.T) {
		rec := do(t, router, http.MethodGet, "/v1/sites/"+siteA+"/members", ownerToken, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("list members: got %d (body: %s)", rec.Code, rec.Body.String())
		}
		var body struct {
			Members []sites.SiteMember `json:"members"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode members: %v", err)
		}
		if len(body.Members) != 1 || body.Members[0].UserID != supervisorID || body.Members[0].Role != auth.RoleSupervisor {
			t.Fatalf("want the supervisor as the only member, got %+v", body.Members)
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
		{"supervisor reads assigned site", supToken, http.MethodGet, "/v1/sites/" + siteA, nil, http.StatusOK},
		{"supervisor gets 404 on unassigned site", supToken, http.MethodGet, "/v1/sites/" + siteB, nil, http.StatusNotFound},
		{"supervisor cannot create sites", supToken, http.MethodPost, "/v1/sites", map[string]string{"name": "Sneaky Site"}, http.StatusForbidden},
		{"supervisor cannot archive sites", supToken, http.MethodDelete, "/v1/sites/" + siteA, nil, http.StatusForbidden},
		{"supervisor cannot list members", supToken, http.MethodGet, "/v1/sites/" + siteA + "/members", nil, http.StatusForbidden},
		{"owner reads unassigned site", ownerToken, http.MethodGet, "/v1/sites/" + siteB, nil, http.StatusOK},
		{"assigning twice is harmless", ownerToken, http.MethodPut, "/v1/sites/" + siteA + "/members/" + supervisorID, nil, http.StatusNoContent},
		{"cannot assign another org's user", ownerToken, http.MethodPut, "/v1/sites/" + siteA + "/members/" + other.UserID, nil, http.StatusNotFound},
		{"cannot assign to another org's site", ownerToken, http.MethodPut, "/v1/sites/" + otherSite + "/members/" + supervisorID, nil, http.StatusNotFound},
		{"malformed user id is 404", ownerToken, http.MethodPut, "/v1/sites/" + siteA + "/members/not-a-uuid", nil, http.StatusNotFound},
		{"removing an unassigned member is 404", ownerToken, http.MethodDelete, "/v1/sites/" + siteB + "/members/" + supervisorID, nil, http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, router, tt.method, tt.path, tt.token, tt.body)
			if rec.Code != tt.want {
				t.Errorf("%s %s: got %d, want %d (body: %s)", tt.method, tt.path, rec.Code, tt.want, rec.Body.String())
			}
		})
	}

	t.Run("removed supervisor loses access", func(t *testing.T) {
		if rec := do(t, router, http.MethodDelete, "/v1/sites/"+siteA+"/members/"+supervisorID, ownerToken, nil); rec.Code != http.StatusNoContent {
			t.Fatalf("remove supervisor: got %d (body: %s)", rec.Code, rec.Body.String())
		}
		if rec := do(t, router, http.MethodGet, "/v1/sites/"+siteA, supToken, nil); rec.Code != http.StatusNotFound {
			t.Fatalf("supervisor after removal: got %d, want 404", rec.Code)
		}
		if got := listSites(t, router, supToken); len(got) != 0 {
			t.Fatalf("supervisor after removal: want no sites, got %d", len(got))
		}
	})
}

func do(t *testing.T, router http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	}

	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func me(t *testing.T, router http.Handler, token string) auth.MeResponse {
	t.Helper()

	rec := do(t, router, http.MethodGet, "/v1/me", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("me: got %d (body: %s)", rec.Code, rec.Body.String())
	}
	var resp auth.MeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	return resp
}

func createSite(t *testing.T, router http.Handler, token, name string) string {
	t.Helper()

	rec := do(t, router, http.MethodPost, "/v1/sites", token, map[string]string{"name": name})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create site: got %d (body: %s)", rec.Code, rec.Body.String())
	}
	var site sites.Site
	if err := json.Unmarshal(rec.Body.Bytes(), &site); err != nil {
		t.Fatalf("decode site: %v", err)
	}
	return site.ID
}

// inviteMember adds someone to the owner's organisation the real way:
// invite them by email, then accept with the shared test password.
// login(t, router, email) works for them afterwards.
func inviteMember(t *testing.T, router http.Handler, ownerToken, email, role string) string {
	t.Helper()

	rec := do(t, router, http.MethodPost, "/v1/invites", ownerToken, map[string]string{
		"email":     email,
		"full_name": "Test " + role,
		"role":      role,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("invite %s: got %d (body: %s)", email, rec.Code, rec.Body.String())
	}
	var invite invites.InviteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &invite); err != nil {
		t.Fatalf("decode invite: %v", err)
	}

	rec = do(t, router, http.MethodPost, "/v1/invites/accept", "", map[string]string{
		"token":       invite.Token,
		"password":    "correcthorse1",
		"device_name": "test",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("accept invite %s: got %d (body: %s)", email, rec.Code, rec.Body.String())
	}
	return invite.UserID
}

func login(t *testing.T, router http.Handler, email string) string {
	t.Helper()

	rec := do(t, router, http.MethodPost, "/v1/auth/login", "", map[string]string{
		"email":       email,
		"password":    "correcthorse1",
		"device_name": "test",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login %s: got %d (body: %s)", email, rec.Code, rec.Body.String())
	}
	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tokens); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	return tokens.AccessToken
}
