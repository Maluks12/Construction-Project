package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/evidence"
	"construction_db/internal/platform/storage"
)

type evidenceFixture struct {
	router                        http.Handler
	owner, supervisor, bookkeeper string // tokens
	other                         string // token of an owner in another org
	ownerMe                       auth.MeResponse
	siteA, siteB, otherSite       string // the supervisor is assigned to siteA only
}

func newEvidenceFixture(t *testing.T, name string) evidenceFixture {
	t.Helper()
	// Unique per run, so -count=N works against the shared test database.
	name += "-" + uuid.NewString()[:8]
	router := newRouter(t)
	f := evidenceFixture{router: router}

	f.owner = signup(t, router, name+" Co", "owner@"+name+".evidence.test")
	f.other = signup(t, router, name+" Other Co", "owner@"+name+".other.evidence.test")
	f.ownerMe = me(t, router, f.owner)
	f.siteA = listSites(t, router, f.owner)[0].ID
	f.siteB = createSite(t, router, f.owner, "Site B")
	f.otherSite = listSites(t, router, f.other)[0].ID

	supID := inviteMember(t, router, f.owner, "foreman@"+name+".evidence.test", auth.RoleSupervisor)
	if rec := do(t, router, http.MethodPut, "/v1/sites/"+f.siteA+"/members/"+supID, f.owner, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("assign supervisor: got %d (body: %s)", rec.Code, rec.Body.String())
	}
	f.supervisor = login(t, router, "foreman@"+name+".evidence.test")
	inviteMember(t, router, f.owner, "books@"+name+".evidence.test", auth.RoleBookkeeper)
	f.bookkeeper = login(t, router, "books@"+name+".evidence.test")
	return f
}

func declareBody(content string) map[string]any {
	sum := sha256.Sum256([]byte(content))
	return map[string]any{
		"content_sha256":     hex.EncodeToString(sum[:]),
		"size_bytes":         len(content),
		"content_type":       "image/jpeg",
		"client_captured_at": "2026-11-02T08:15:30.123456Z",
	}
}

func with(body map[string]any, key string, value any) map[string]any {
	out := map[string]any{}
	for k, v := range body {
		out[k] = v
	}
	if value == nil {
		delete(out, key)
	} else {
		out[key] = value
	}
	return out
}

func evidencePath(site, id string) string { return "/v1/sites/" + site + "/evidence/" + id }

func decodeEvidence(t *testing.T, rec *httptest.ResponseRecorder) evidence.Evidence {
	t.Helper()
	var ev evidence.Evidence
	if err := json.Unmarshal(rec.Body.Bytes(), &ev); err != nil {
		t.Fatalf("decode evidence: %v (body: %s)", err, rec.Body.String())
	}
	return ev
}

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("got %d, want %d (body: %s)", rec.Code, want, rec.Body.String())
	}
}

func TestEvidenceDeclare(t *testing.T) {
	f := newEvidenceFixture(t, "declare")
	const content = "photo bytes"
	body := declareBody(content)
	id := uuid.NewString()

	rec := do(t, f.router, http.MethodPut, evidencePath(f.siteA, id), f.owner, body)
	wantStatus(t, rec, http.StatusCreated)
	ev := decodeEvidence(t, rec)

	if ev.ID != id || ev.SiteID != f.siteA || ev.Status != evidence.StatusPending || ev.UploadedBy != f.ownerMe.UserID {
		t.Errorf("declared evidence = %+v", ev)
	}
	if ev.ContentSHA256 != body["content_sha256"] || ev.ClientCapturedAt.Format(time.RFC3339Nano) != "2026-11-02T08:15:30.123456Z" {
		t.Errorf("hash %s, captured %s; want the declared values", ev.ContentSHA256, ev.ClientCapturedAt.Format(time.RFC3339Nano))
	}
	if !strings.Contains(rec.Body.String(), `"chain":null`) {
		t.Errorf("want chain null until verified, body: %s", rec.Body.String())
	}
	up := ev.Upload
	if up == nil || up.Method != http.MethodPut || up.Headers["If-None-Match"] != "*" ||
		up.Headers["X-Amz-Checksum-Sha256"] == "" || up.Headers["Content-Type"] != "image/jpeg" {
		t.Fatalf("upload = %+v, want a PUT with the signed checksum and If-None-Match", up)
	}
	if left := time.Until(up.ExpiresAt); left < 14*time.Minute || left > 16*time.Minute {
		t.Errorf("upload expires in %s, want about 15 minutes", left)
	}

	t.Run("the upload URL accepts the declared bytes", func(t *testing.T) {
		req, err := http.NewRequest(up.Method, up.URL, bytes.NewReader([]byte(content)))
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range up.Headers {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("PUT to upload URL: %d", resp.StatusCode)
		}
		key := "orgs/" + f.ownerMe.OrgID + "/sites/" + f.siteA + "/evidence/" + id
		info, err := testStore.Stat(context.Background(), key)
		if err != nil || info.Size != int64(len(content)) {
			t.Errorf("stat %s = %+v, err %v", key, info, err)
		}
	})

	// The attack the signed length stops: a small declared size with the hash
	// of a larger file, which would then be kept forever under Object Lock.
	t.Run("a file larger than declared is refused", func(t *testing.T) {
		big := strings.Repeat("large file ", 100)
		sum := sha256.Sum256([]byte(big))
		id := uuid.NewString()
		small := with(body, "content_sha256", hex.EncodeToString(sum[:]))
		rec := do(t, f.router, http.MethodPut, evidencePath(f.siteA, id), f.owner, small)
		wantStatus(t, rec, http.StatusCreated)
		up := decodeEvidence(t, rec).Upload

		req, err := http.NewRequest(up.Method, up.URL, strings.NewReader(big))
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range up.Headers {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode < 400 || resp.StatusCode > 499 {
			t.Errorf("PUT of %d bytes declared as %d: status %d, want 4xx", len(big), len(content), resp.StatusCode)
		}
		key := "orgs/" + f.ownerMe.OrgID + "/sites/" + f.siteA + "/evidence/" + id
		if _, err := testStore.Stat(context.Background(), key); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("stat after refused upload: %v, want nothing stored", err)
		}
	})

	t.Run("repeating the declaration is idempotent", func(t *testing.T) {
		rec := do(t, f.router, http.MethodPut, evidencePath(f.siteA, id), f.owner, body)
		wantStatus(t, rec, http.StatusOK)
		if again := decodeEvidence(t, rec); again.Upload == nil || again.ID != id {
			t.Errorf("repeat = %+v, want the same evidence with a fresh upload", again)
		}
	})

	t.Run("an upper-case hash is the same declaration", func(t *testing.T) {
		upper := with(body, "content_sha256", strings.ToUpper(body["content_sha256"].(string)))
		wantStatus(t, do(t, f.router, http.MethodPut, evidencePath(f.siteA, id), f.owner, upper), http.StatusOK)
	})

	t.Run("a capture time is kept to the microsecond", func(t *testing.T) {
		id := uuid.NewString()
		ns := with(body, "client_captured_at", "2026-11-02T08:15:30.123456789Z")
		rec := do(t, f.router, http.MethodPut, evidencePath(f.siteA, id), f.owner, ns)
		wantStatus(t, rec, http.StatusCreated)
		if got := decodeEvidence(t, rec).ClientCapturedAt.Format(time.RFC3339Nano); got != "2026-11-02T08:15:30.123456Z" {
			t.Errorf("captured at %s, want it truncated to microseconds", got)
		}
		wantStatus(t, do(t, f.router, http.MethodPut, evidencePath(f.siteA, id), f.owner, body), http.StatusOK)
	})

	reused := map[string]struct {
		token, site string
		body        map[string]any
	}{
		"different hash":         {f.owner, f.siteA, declareBody("other bytes")},
		"capture time +1µs":      {f.owner, f.siteA, with(body, "client_captured_at", "2026-11-02T08:15:30.123457Z")},
		"added GPS":              {f.owner, f.siteA, with(with(body, "lat", -26.2), "lng", 28.0)},
		"added device":           {f.owner, f.siteA, with(body, "device_id", "pixel")},
		"different size":         {f.owner, f.siteA, with(body, "size_bytes", 12)},
		"another site":           {f.owner, f.siteB, body},
		"another uploader":       {f.supervisor, f.siteA, body},
		"another org's own site": {f.other, f.otherSite, body},
	}
	for name, tt := range reused {
		t.Run("reused id: "+name, func(t *testing.T) {
			rec := do(t, f.router, http.MethodPut, evidencePath(tt.site, id), tt.token, tt.body)
			wantStatus(t, rec, http.StatusConflict)
			if !strings.Contains(rec.Body.String(), "evidence id already used with different details") {
				t.Errorf("body: %s", rec.Body.String())
			}
		})
	}

	t.Run("rejected evidence needs a new id", func(t *testing.T) {
		id := uuid.NewString()
		wantStatus(t, do(t, f.router, http.MethodPut, evidencePath(f.siteA, id), f.owner, body), http.StatusCreated)
		execAsTenant(t, f.ownerMe.OrgID, 1, `UPDATE evidence SET status = 'rejected', rejected_reason = 'test' WHERE id = $1`, id)

		rec := do(t, f.router, http.MethodPut, evidencePath(f.siteA, id), f.owner, body)
		wantStatus(t, rec, http.StatusConflict)
		if want := `{"error":"evidence was rejected; declare a new evidence id"}`; rec.Body.String() != want {
			t.Errorf("body %s, want %s", rec.Body.String(), want)
		}
	})

	t.Run("verified evidence needs no upload", func(t *testing.T) {
		id := uuid.NewString()
		wantStatus(t, do(t, f.router, http.MethodPut, evidencePath(f.siteA, id), f.owner, body), http.StatusCreated)
		execAsTenant(t, f.ownerMe.OrgID, 1, `UPDATE evidence SET status = 'verified', object_version_id = 'v1' WHERE id = $1`, id)

		rec := do(t, f.router, http.MethodPut, evidencePath(f.siteA, id), f.owner, body)
		wantStatus(t, rec, http.StatusOK)
		if strings.Contains(rec.Body.String(), `"upload"`) {
			t.Errorf("verified evidence still offers an upload: %s", rec.Body.String())
		}
	})
}

// A client retrying before its first request finished must still get an
// idempotent answer, while a genuinely different declaration still conflicts.
func TestEvidenceConcurrentDeclare(t *testing.T) {
	f := newEvidenceFixture(t, "concurrent")
	// No more requests than pool connections: each holds at most one at a
	// time, so none queues for a connection. Queuing past RequireAuth's 2s
	// timeout is a 500 from auth, a separate issue from the declare race.
	n := int(testPool.Config().MaxConns)

	race := func(t *testing.T, bodyFor func(i int) map[string]any) map[int]int {
		t.Helper()
		id := uuid.NewString()
		codes := make([]int, n)
		var wg sync.WaitGroup
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				codes[i] = do(t, f.router, http.MethodPut, evidencePath(f.siteA, id), f.owner, bodyFor(i)).Code
			}()
		}
		wg.Wait()
		counts := map[int]int{}
		for _, c := range codes {
			counts[c]++
		}
		return counts
	}

	t.Run("identical declares", func(t *testing.T) {
		body := declareBody("same photo")
		got := race(t, func(int) map[string]any { return body })
		if got[http.StatusCreated] != 1 || got[http.StatusOK] != n-1 {
			t.Errorf("status counts %v, want one 201 and %d 200s", got, n-1)
		}
	})

	t.Run("different declares", func(t *testing.T) {
		got := race(t, func(i int) map[string]any { return declareBody(fmt.Sprintf("photo %d", i)) })
		if got[http.StatusCreated] != 1 || got[http.StatusConflict] != n-1 {
			t.Errorf("status counts %v, want one 201 and %d 409s", got, n-1)
		}
	})
}

func TestEvidenceValidation(t *testing.T) {
	f := newEvidenceFixture(t, "validation")
	body := declareBody("photo bytes")
	hash := body["content_sha256"].(string)

	bad := map[string]map[string]any{
		"63-character hash":     with(body, "content_sha256", hash[:63]),
		"0x-prefixed hash":      with(body, "content_sha256", "0x"+hash[:62]),
		"non-hex hash":          with(body, "content_sha256", strings.Repeat("z", 64)),
		"no hash":               with(body, "content_sha256", nil),
		"size 0":                with(body, "size_bytes", 0),
		"size over 10 MiB":      with(body, "size_bytes", evidence.MaxSizeBytes+1),
		"gif":                   with(body, "content_type", "image/gif"),
		"lat without lng":       with(body, "lat", -26.2),
		"lng without lat":       with(body, "lng", 28.0),
		"lat 91":                with(with(body, "lat", 91), "lng", 28.0),
		"lng 181":               with(with(body, "lat", 0), "lng", 181),
		"empty device_id":       with(body, "device_id", ""),
		"device_id 202 bytes":   with(body, "device_id", strings.Repeat("Ü", 101)),
		"no client_captured_at": with(body, "client_captured_at", nil),
		"unparseable time":      with(body, "client_captured_at", "yesterday"),
	}
	for name, b := range bad {
		t.Run(name, func(t *testing.T) {
			wantStatus(t, do(t, f.router, http.MethodPut, evidencePath(f.siteA, uuid.NewString()), f.owner, b), http.StatusBadRequest)
		})
	}

	t.Run("malformed evidence id", func(t *testing.T) {
		rec := do(t, f.router, http.MethodPut, evidencePath(f.siteA, "not-a-uuid"), f.owner, body)
		wantStatus(t, rec, http.StatusBadRequest)
		if !strings.Contains(rec.Body.String(), "evidence id must be a UUID") {
			t.Errorf("body: %s", rec.Body.String())
		}
	})

	good := map[string]map[string]any{
		"device_id of 200 bytes": with(body, "device_id", strings.Repeat("Ü", 100)),
		"GPS at the edges":       with(with(body, "lat", -90), "lng", 180),
		"png at the size limit":  with(with(body, "content_type", "image/png"), "size_bytes", evidence.MaxSizeBytes),
	}
	for name, b := range good {
		t.Run(name, func(t *testing.T) {
			wantStatus(t, do(t, f.router, http.MethodPut, evidencePath(f.siteA, uuid.NewString()), f.owner, b), http.StatusCreated)
		})
	}
}

func TestEvidenceAccess(t *testing.T) {
	f := newEvidenceFixture(t, "access")
	body := declareBody("photo bytes")
	onA := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, evidencePath(f.siteA, onA), f.owner, body), http.StatusCreated)
	onB := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, evidencePath(f.siteB, onB), f.owner, body), http.StatusCreated)

	tests := []struct {
		name, token, method, path string
		body                      any
		want                      int
	}{
		{"no token", "", http.MethodGet, "/v1/sites/" + f.siteA + "/evidence", nil, http.StatusUnauthorized},
		{"bookkeeper cannot list", f.bookkeeper, http.MethodGet, "/v1/sites/" + f.siteA + "/evidence", nil, http.StatusForbidden},
		{"bookkeeper cannot get", f.bookkeeper, http.MethodGet, evidencePath(f.siteA, onA), nil, http.StatusForbidden},
		{"bookkeeper cannot declare", f.bookkeeper, http.MethodPut, evidencePath(f.siteA, uuid.NewString()), body, http.StatusForbidden},
		{"supervisor declares on own site", f.supervisor, http.MethodPut, evidencePath(f.siteA, uuid.NewString()), body, http.StatusCreated},
		{"supervisor reads on own site", f.supervisor, http.MethodGet, evidencePath(f.siteA, onA), nil, http.StatusOK},
		{"supervisor lists own site", f.supervisor, http.MethodGet, "/v1/sites/" + f.siteA + "/evidence", nil, http.StatusOK},
		{"supervisor cannot declare elsewhere", f.supervisor, http.MethodPut, evidencePath(f.siteB, uuid.NewString()), body, http.StatusNotFound},
		{"supervisor cannot read elsewhere", f.supervisor, http.MethodGet, evidencePath(f.siteB, onB), nil, http.StatusNotFound},
		{"supervisor cannot list elsewhere", f.supervisor, http.MethodGet, "/v1/sites/" + f.siteB + "/evidence", nil, http.StatusNotFound},
		{"other org cannot declare", f.other, http.MethodPut, evidencePath(f.siteA, uuid.NewString()), body, http.StatusNotFound},
		{"other org cannot read", f.other, http.MethodGet, evidencePath(f.siteA, onA), nil, http.StatusNotFound},
		{"other org cannot list", f.other, http.MethodGet, "/v1/sites/" + f.siteA + "/evidence", nil, http.StatusNotFound},
		{"other org can't reach it via its own site", f.other, http.MethodGet, evidencePath(f.otherSite, onA), nil, http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantStatus(t, do(t, f.router, tt.method, tt.path, tt.token, tt.body), tt.want)
		})
	}
}

func TestEvidenceGetAndList(t *testing.T) {
	f := newEvidenceFixture(t, "list")
	list := func(t *testing.T, site, query string) []evidence.Evidence {
		t.Helper()
		rec := do(t, f.router, http.MethodGet, "/v1/sites/"+site+"/evidence"+query, f.owner, nil)
		wantStatus(t, rec, http.StatusOK)
		var resp struct {
			Evidence []evidence.Evidence `json:"evidence"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		return resp.Evidence
	}
	ids := func(items []evidence.Evidence) []string {
		out := []string{}
		for _, e := range items {
			out = append(out, e.ID)
		}
		return out
	}

	t.Run("an empty site lists [] not null", func(t *testing.T) {
		rec := do(t, f.router, http.MethodGet, "/v1/sites/"+f.siteB+"/evidence", f.owner, nil)
		wantStatus(t, rec, http.StatusOK)
		if rec.Body.String() != `{"evidence":[]}` {
			t.Errorf("body %s, want {\"evidence\":[]}", rec.Body.String())
		}
	})

	var declared []string
	for i := range 3 {
		id := uuid.NewString()
		wantStatus(t, do(t, f.router, http.MethodPut, evidencePath(f.siteA, id), f.owner, declareBody("photo "+string(rune('a'+i)))), http.StatusCreated)
		declared = append(declared, id)
	}
	execAsTenant(t, f.ownerMe.OrgID, 1, `UPDATE evidence SET status = 'rejected', rejected_reason = 'test' WHERE id = $1`, declared[0])

	newestFirst := []string{declared[2], declared[1], declared[0]}
	queries := map[string]struct {
		query string
		want  []string
	}{
		"all, newest first": {"", newestFirst},
		"limit":             {"?limit=2", newestFirst[:2]},
		"rejected":          {"?status=rejected", []string{declared[0]}},
		"pending":           {"?status=pending", newestFirst[:2]},
		"verified":          {"?status=verified", []string{}},
	}
	for name, q := range queries {
		t.Run(name, func(t *testing.T) {
			if got := ids(list(t, f.siteA, q.query)); strings.Join(got, ",") != strings.Join(q.want, ",") {
				t.Errorf("got %v, want %v", got, q.want)
			}
		})
	}

	for _, q := range []string{"?status=bogus", "?limit=0", "?limit=201", "?limit=many"} {
		t.Run("bad query "+q, func(t *testing.T) {
			wantStatus(t, do(t, f.router, http.MethodGet, "/v1/sites/"+f.siteA+"/evidence"+q, f.owner, nil), http.StatusBadRequest)
		})
	}

	t.Run("get", func(t *testing.T) {
		rec := do(t, f.router, http.MethodGet, evidencePath(f.siteA, declared[1]), f.owner, nil)
		wantStatus(t, rec, http.StatusOK)
		ev := decodeEvidence(t, rec)
		if ev.ID != declared[1] || ev.Chain != nil || strings.Contains(rec.Body.String(), `"upload"`) {
			t.Errorf("get = %s, want the evidence with no chain and no upload", rec.Body.String())
		}
		if rejected := decodeEvidence(t, do(t, f.router, http.MethodGet, evidencePath(f.siteA, declared[0]), f.owner, nil)); rejected.RejectedReason == nil || *rejected.RejectedReason != "test" {
			t.Errorf("rejected evidence = %+v, want its reason", rejected)
		}
	})

	for name, path := range map[string]string{
		"unknown id":     evidencePath(f.siteA, uuid.NewString()),
		"malformed id":   evidencePath(f.siteA, "not-a-uuid"),
		"the wrong site": evidencePath(f.siteB, declared[1]),
	} {
		t.Run("get "+name+" is 404", func(t *testing.T) {
			wantStatus(t, do(t, f.router, http.MethodGet, path, f.owner, nil), http.StatusNotFound)
		})
	}
}
