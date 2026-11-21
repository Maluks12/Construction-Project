package app

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"construction_db/internal/platform/httpapi"
)

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
