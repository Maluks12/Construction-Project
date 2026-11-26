package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"construction_db/internal/core/devicesync"
	"construction_db/internal/platform/httpapi"
)

// Every body the guard reads is bounded (SYNC review 2): one byte over
// MaxBody is 413 before any handler reads it, on any route.
func TestRequestBodyIsBounded(t *testing.T) {
	r := newRouter(t)
	token := signup(t, r, "Big Co", "owner@big-"+uuid.NewString()[:8]+".test")
	site := listSites(t, r, token)[0].ID
	want := `{"error":"` + httpapi.TooLargeText + `"}`
	for _, size := range []int{httpapi.MaxBody + 1, 4 * httpapi.MaxBody} {
		body := `{"notes":"` + strings.Repeat("n", size) + `"}`
		req := httptest.NewRequest(http.MethodPut, talkPath(site, uuid.NewString()), strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		res := httptest.NewRecorder()
		r.ServeHTTP(res, req)
		if res.Code != http.StatusRequestEntityTooLarge || res.Body.String() != want {
			t.Errorf("%d bytes: %d %s", size, res.Code, res.Body.String())
		}
	}
	// The guard stops reading at the bound: a huge body isn't taken into
	// memory first.
	huge := &countingReader{left: 64 * httpapi.MaxBody}
	req0 := httptest.NewRequest(http.MethodPut, talkPath(site, uuid.NewString()), huge)
	req0.Header.Set("Authorization", "Bearer "+token)
	res0 := httptest.NewRecorder()
	r.ServeHTTP(res0, req0)
	if res0.Code != http.StatusRequestEntityTooLarge || huge.read > httpapi.MaxBody+1 {
		t.Errorf("a 64 MiB body: %d, %d bytes read", res0.Code, huge.read)
	}
	// Just under is the handler's to judge.
	under := `{"notes":"` + strings.Repeat("n", httpapi.MaxBody-100) + `"}`
	req := httptest.NewRequest(http.MethodPut, talkPath(site, uuid.NewString()), strings.NewReader(under))
	req.Header.Set("Authorization", "Bearer "+token)
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	if res.Code == http.StatusRequestEntityTooLarge {
		t.Errorf("under the bound: %d", res.Code)
	}
}

// A path with a control character is the batch's 400, naming the record:
// no route takes one, so the phone drops that record.
func TestPushPathWithNUL(t *testing.T) {
	r := newRouter(t)
	token := signup(t, r, "NUL Path Co", "owner@nulpath-"+uuid.NewString()[:8]+".test")
	site := listSites(t, r, token)[0].ID
	res := do(t, r, http.MethodPost, "/v1/sync/push", token, map[string]any{"records": []any{
		rec("PUT", talkPath(site, uuid.NewString()), talkBody(nil)),
		rec("PUT", talkPath(site, "x\u0000y"), talkBody(nil)),
	}})
	if res.Code != http.StatusBadRequest || res.Body.String() != `{"error":"route not allowed in a batch","index":1}` {
		t.Errorf("%d %s", res.Code, res.Body.String())
	}
}

var routeParam = regexp.MustCompile(`:[A-Za-z]+`)

// fillParams gives each path parameter a value of its kind, so the request
// reaches the route rather than a 404.
func fillParams(path string) string {
	return routeParam.ReplaceAllStringFunc(path, func(p string) string {
		switch p {
		case ":date":
			return "2026-11-21"
		case ":version":
			return "1"
		case ":code":
			return "za_construction_2014"
		default:
			return "5e000000-0000-0000-0000-000000000001"
		}
	})
}

// Every registered route refuses a NUL in its query and, if it takes a body,
// in a JSON body nested two levels down (REG4 review R4). Routes skipped:
// none. The guard runs before auth, so no token is needed.
func TestEveryRouteRefusesNUL(t *testing.T) {
	r := newRouter(t)
	routes := r.Routes()
	if len(routes) < 80 {
		t.Fatalf("only %d routes registered; the walk would prove little", len(routes))
	}
	want := `{"error":"` + httpapi.NULText + `"}`
	token := signup(t, r, "NUL Co", "owner@nul-"+uuid.NewString()[:8]+".test")
	site := listSites(t, r, token)[0].ID
	for _, rt := range routes {
		path := fillParams(rt.Path)
		t.Run(rt.Method+" "+rt.Path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(rt.Method, path+"?q=a%00b", nil))
			if rec.Code != http.StatusBadRequest || rec.Body.String() != want {
				t.Errorf("query: got %d %s", rec.Code, rec.Body.String())
			}
			if rt.Method == http.MethodGet || rt.Method == http.MethodHead {
				return
			}
			if rt.Path == devicesync.PushRoute {
				// The batch's body is the records': a NUL in one is that
				// record's own 400, and the rest of the batch runs (SYNC
				// review 2).
				res := push(t, r, token,
					record("PUT", talkPath(site, uuid.NewString()), map[string]any{"a": map[string]any{"b": []any{"ok", "x\u0000y"}}}),
					record("PUT", talkPath(site, uuid.NewString()), talkBody(map[string]any{"started_at": rfc(time.Now().Add(-time.Minute))})))
				if len(res.Results) != 2 || res.Results[0].Status != http.StatusBadRequest || string(res.Results[0].Body) != want ||
					res.Results[1].Status != http.StatusCreated {
					t.Errorf("push: %v %v", statuses(res), bodies(res))
				}
				return
			}
			rec = httptest.NewRecorder()
			req := httptest.NewRequest(rt.Method, path, strings.NewReader(`{"a":{"b":["ok","x\u0000y"]}}`))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest || rec.Body.String() != want {
				t.Errorf("body: got %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// countingReader is left bytes of 'x', counting what was read.
type countingReader struct{ left, read int }

func (c *countingReader) Read(p []byte) (int, error) {
	if c.left == 0 {
		return 0, io.EOF
	}
	n := min(len(p), c.left)
	for i := range n {
		p[i] = 'x'
	}
	c.left -= n
	c.read += n
	return n, nil
}
