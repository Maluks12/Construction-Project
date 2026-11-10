package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// logRow is one evidence_access_log row as these tests compare it.
type logRow struct {
	Action, UserID                          string
	EvidenceID, IP, UserAgent, FilterStatus *string
	FilterLimit, ResultCount                *int
}

// accessRows reads a site's log as superuser, oldest first.
func accessRows(t *testing.T, siteID string) []logRow {
	t.Helper()
	rows, err := testAdmin.Query(context.Background(), `
		SELECT action, user_id::text, evidence_id::text, host(ip), user_agent, filter_status, filter_limit, result_count
		FROM evidence_access_log WHERE site_id = $1 ORDER BY accessed_at, id`, siteID)
	if err != nil {
		t.Fatalf("read access log: %v", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (logRow, error) {
		var l logRow
		err := r.Scan(&l.Action, &l.UserID, &l.EvidenceID, &l.IP, &l.UserAgent, &l.FilterStatus, &l.FilterLimit, &l.ResultCount)
		return l, err
	})
	if err != nil {
		t.Fatalf("read access log: %v", err)
	}
	return out
}

// getWith is a GET with extra headers. httptest's peer address is 192.0.2.1.
func getWith(t *testing.T, router http.Handler, path, token string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func str(s string) *string { return &s }
func num(n int) *int       { return &n }

func equalRow(a, b logRow) bool {
	eqS := func(x, y *string) bool { return (x == nil && y == nil) || (x != nil && y != nil && *x == *y) }
	eqN := func(x, y *int) bool { return (x == nil && y == nil) || (x != nil && y != nil && *x == *y) }
	return a.Action == b.Action && a.UserID == b.UserID && eqS(a.EvidenceID, b.EvidenceID) && eqS(a.IP, b.IP) &&
		eqS(a.UserAgent, b.UserAgent) && eqS(a.FilterStatus, b.FilterStatus) &&
		eqN(a.FilterLimit, b.FilterLimit) && eqN(a.ResultCount, b.ResultCount)
}

// Every read of evidence details leaves exactly one row; a refused read leaves none.
func TestEvidenceReadsAreLogged(t *testing.T) {
	f := newEvidenceFixture(t, "accesslog")
	id := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, evidencePath(f.siteA, id), f.owner, declareBody("photo")), http.StatusCreated)
	if rows := accessRows(t, f.siteA); len(rows) != 0 {
		t.Fatalf("declare logged %d rows, want 0 (the uploader's own echo isn't a view)", len(rows))
	}

	ua := map[string]string{"User-Agent": "SiteProof-Test/1.0"}
	wantStatus(t, getWith(t, f.router, evidencePath(f.siteA, id), f.owner, ua), http.StatusOK)
	wantStatus(t, getWith(t, f.router, "/v1/sites/"+f.siteA+"/evidence", f.owner, ua), http.StatusOK)
	wantStatus(t, getWith(t, f.router, "/v1/sites/"+f.siteA+"/evidence?status=pending&limit=5", f.supervisor, ua), http.StatusOK)

	// Refused reads: nothing was seen, so nothing is logged.
	for name, req := range map[string]struct{ path, token string }{
		"unknown id":                    {evidencePath(f.siteA, uuid.NewString()), f.owner},
		"malformed id":                  {evidencePath(f.siteA, "not-a-uuid"), f.owner},
		"the wrong site":                {evidencePath(f.siteB, id), f.owner},
		"supervisor elsewhere":          {"/v1/sites/" + f.siteB + "/evidence", f.supervisor},
		"other org":                     {evidencePath(f.siteA, id), f.other},
		"other org lists":               {"/v1/sites/" + f.siteA + "/evidence", f.other},
		"bad status":                    {"/v1/sites/" + f.siteA + "/evidence?status=bogus", f.owner},
		"bad limit":                     {"/v1/sites/" + f.siteA + "/evidence?limit=0", f.owner},
		"bookkeeper (wrong role)":       {evidencePath(f.siteA, id), f.bookkeeper},
		"bookkeeper lists (wrong role)": {"/v1/sites/" + f.siteA + "/evidence", f.bookkeeper},
	} {
		if rec := getWith(t, f.router, req.path, req.token, ua); rec.Code < 400 {
			t.Fatalf("%s: got %d, want a refusal", name, rec.Code)
		}
	}

	supervisorID := me(t, f.router, f.supervisor).UserID
	want := []logRow{
		{Action: "view_record", UserID: f.ownerMe.UserID, EvidenceID: &id, IP: str("192.0.2.1"), UserAgent: str("SiteProof-Test/1.0")},
		{Action: "list", UserID: f.ownerMe.UserID, IP: str("192.0.2.1"), UserAgent: str("SiteProof-Test/1.0"), FilterLimit: num(50), ResultCount: num(1)},
		{Action: "list", UserID: supervisorID, IP: str("192.0.2.1"), UserAgent: str("SiteProof-Test/1.0"),
			FilterStatus: str("pending"), FilterLimit: num(5), ResultCount: num(1)},
	}
	got := accessRows(t, f.siteA)
	if len(got) != len(want) {
		t.Fatalf("site A has %d log rows, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if !equalRow(got[i], want[i]) {
			t.Errorf("row %d = %s, want %s", i, describe(got[i]), describe(want[i]))
		}
	}
	if rows := accessRows(t, f.siteB); len(rows) != 0 {
		t.Errorf("site B has %d log rows, want 0", len(rows))
	}
}

// The logged IP is the TCP peer unless the peer is a trusted proxy, so a
// client can't choose what the log says by sending X-Forwarded-For.
func TestAccessLogClientIP(t *testing.T) {
	f := newEvidenceFixture(t, "clientip")
	id := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, evidencePath(f.siteA, id), f.owner, declareBody("photo")), http.StatusCreated)
	forwarded := map[string]string{"X-Forwarded-For": "203.0.113.9"}

	behindProxy, err := NewRouter(context.Background(), testPool, testSecret, testRegistry, testEdition, testStore,
		HTTPOptions{RequestTimeout: testRequestTimeout, TrustedProxies: []string{"192.0.2.1"}})
	if err != nil {
		t.Fatalf("new router: %v", err)
	}

	wantStatus(t, getWith(t, f.router, evidencePath(f.siteA, id), f.owner, forwarded), http.StatusOK)
	wantStatus(t, getWith(t, behindProxy, evidencePath(f.siteA, id), f.owner, forwarded), http.StatusOK)

	rows := accessRows(t, f.siteA)
	if len(rows) != 2 {
		t.Fatalf("%d log rows, want 2", len(rows))
	}
	if ip := orNone(rows[0].IP); ip != "192.0.2.1" {
		t.Errorf("no trusted proxies: logged %s, want the peer 192.0.2.1", ip)
	}
	if ip := orNone(rows[1].IP); ip != "203.0.113.9" {
		t.Errorf("peer is a trusted proxy: logged %s, want the forwarded 203.0.113.9", ip)
	}
	if rows[0].UserAgent != nil {
		t.Errorf("no User-Agent sent: logged %q, want NULL", *rows[0].UserAgent)
	}
}

func orNone[T any](p *T) any {
	if p == nil {
		return "NULL"
	}
	return *p
}

func describe(r logRow) string {
	return fmt.Sprintf("{%s user=%s evidence=%v ip=%v ua=%v status=%v limit=%v count=%v}", r.Action, r.UserID,
		orNone(r.EvidenceID), orNone(r.IP), orNone(r.UserAgent), orNone(r.FilterStatus), orNone(r.FilterLimit), orNone(r.ResultCount))
}
