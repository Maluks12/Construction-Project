package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/sites"
	"construction_db/internal/editions/construction"
	"construction_db/internal/platform/clock"
	"construction_db/internal/platform/database"
	"construction_db/internal/platform/dbtest"
	"construction_db/internal/platform/storage"
	"construction_db/internal/platform/storagetest"
	"construction_db/internal/regimes"
)

const (
	testSecret = "test-secret-that-is-at-least-32-characters-long"
	// Generous: these tests check behaviour, not speed, and run under -race
	// beside other packages' containers. TestBusyIs503 tests the deadline.
	testRequestTimeout = time.Minute
)

var testEdition = construction.New()

// No trusted proxies, as in production by default.
var testHTTP = HTTPOptions{RequestTimeout: testRequestTimeout}

var (
	testPool     *pgxpool.Pool // as siteproof_app, like the API
	testAdmin    *pgxpool.Pool // superuser, only for regulatory_regimes fixtures
	testRegistry *regimes.Registry
	testStore    *storage.S3      // MinIO with a locked bucket, like the API's
	testExports  *storage.S3      // MinIO, the versioned export bucket
	testS3       *storagetest.Env // its admin client writes objects directly, as a tamperer would
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	// Every fixture signs people up and in: at bcrypt's default cost that was
	// most of this package's time under -race (CI run #90).
	auth.PasswordCost = bcrypt.MinCost

	reg, err := regimes.Load()
	if err != nil {
		log.Printf("load regimes: %v", err)
		os.Exit(1)
	}
	testRegistry = reg

	offset, err := sastOffset(time.Now())
	if err != nil {
		log.Print(err)
		os.Exit(1)
	}
	testClock = clock.Offset(offset)
	testHTTP.Clock = testClock

	pool, admin, cleanup, err := dbtest.StartAt(context.Background(), offset)
	if err != nil {
		log.Printf("start test database: %v", err)
		os.Exit(1)
	}
	testPool = pool
	testAdmin = admin
	if offset != 0 {
		// The database's now() must agree with testClock, or nothing is as if.
		var dbNow time.Time
		if err := pool.QueryRow(context.Background(), "SELECT now()").Scan(&dbNow); err != nil || dbNow.Sub(testClock.Now()).Abs() > 5*time.Second {
			cleanup()
			log.Printf("database clock %s, test clock %s (err %v)", dbNow, testClock.Now(), err)
			os.Exit(1)
		}
		log.Printf("running as if at %s (offset %s)", testClock.Now().In(joburgLoc).Format("2006-01-02 15:04:05 MST"), offset)
	}

	s3env, cleanupS3, err := storagetest.Start(context.Background())
	if err != nil {
		cleanup()
		log.Printf("start test storage: %v", err)
		os.Exit(1)
	}
	testStore = s3env.Store
	testExports = s3env.Exports
	testHTTP.Exports = s3env.Exports
	testS3 = s3env

	code := m.Run()
	cleanupS3()
	cleanup()
	os.Exit(code)
}

// execAsTenant runs a fixture statement under orgID's RLS context and fails
// unless it changed exactly want rows, so a policy-filtered no-op can't pass.
func execAsTenant(t *testing.T, orgID string, want int64, sql string, args ...any) {
	t.Helper()
	err := database.WithTenant(context.Background(), testPool, database.Tenant{OrgID: orgID}, func(tx pgx.Tx) error {
		tag, err := tx.Exec(context.Background(), sql, args...)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != want {
			return fmt.Errorf("changed %d rows, want %d", tag.RowsAffected(), want)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("exec as tenant %q: %v", sql, err)
	}
}

func newRouter(t *testing.T) *gin.Engine {
	t.Helper()
	router, err := NewRouter(context.Background(), testPool, testSecret, testRegistry, testEdition, testStore, testHTTP)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	return router
}

func TestSiteIsolation(t *testing.T) {
	router := newRouter(t)

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
