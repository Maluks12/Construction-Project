package app

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"construction_db/internal/core/sites"
	"construction_db/internal/platform/dbtest"
)

const testSecret = "test-secret-that-is-at-least-32-characters-long"

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)

	pool, cleanup, err := dbtest.Start(context.Background())
	if err != nil {
		log.Printf("start test database: %v", err)
		os.Exit(1)
	}
	testPool = pool

	code := m.Run()
	cleanup()
	os.Exit(code)
}

func TestSiteIsolation(t *testing.T) {
	router := NewRouter(testPool, testSecret)

	tokenA := signup(t, router, "Org A", "owner-a@isolation.test")
	tokenB := signup(t, router, "Org B", "owner-b@isolation.test")

	sitesB := listSites(t, router, tokenB)
	if len(sitesB) != 1 {
		t.Fatalf("org B: want 1 site, got %d", len(sitesB))
	}
	siteB := sitesB[0].ID

	tests := []struct {
		name  string
		token string
		path  string
		want  int
	}{
		{"owner reads own site", tokenB, "/v1/sites/" + siteB, http.StatusOK},
		{"other org gets 404", tokenA, "/v1/sites/" + siteB, http.StatusNotFound},
		{"no token is rejected", "", "/v1/sites/" + siteB, http.StatusUnauthorized},
		{"malformed id is 404", tokenA, "/v1/sites/not-a-uuid", http.StatusNotFound},
		{"unknown id is 404", tokenA, "/v1/sites/00000000-0000-0000-0000-000000000000", http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := get(t, router, tt.path, tt.token)
			if rec.Code != tt.want {
				t.Errorf("GET %s: got %d, want %d (body: %s)", tt.path, rec.Code, tt.want, rec.Body.String())
			}
		})
	}

	t.Run("list shows only own org's sites", func(t *testing.T) {
		sitesA := listSites(t, router, tokenA)
		if len(sitesA) != 1 {
			t.Fatalf("org A: want 1 site, got %d", len(sitesA))
		}
		if sitesA[0].ID == siteB {
			t.Fatalf("org A's list contains org B's site %s", siteB)
		}
	})
}

func signup(t *testing.T, router http.Handler, company, email string) string {
	t.Helper()

	body, err := json.Marshal(map[string]string{
		"company_name": company,
		"full_name":    "Test Owner",
		"email":        email,
		"password":     "correcthorse1",
		"site_name":    company + " Site",
		"device_name":  "test",
	})
	if err != nil {
		t.Fatalf("marshal signup: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("signup %s: got %d (body: %s)", email, rec.Code, rec.Body.String())
	}

	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tokens); err != nil {
		t.Fatalf("decode signup: %v", err)
	}
	return tokens.AccessToken
}

func get(t *testing.T, router http.Handler, path, token string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func listSites(t *testing.T, router http.Handler, token string) []sites.Site {
	t.Helper()

	rec := get(t, router, "/v1/sites", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("list sites: got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var body struct {
		Sites []sites.Site `json:"sites"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode sites: %v", err)
	}
	return body.Sites
}
