package devicesync

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// A sync request is a transport over the API (D3, D6): every pulled item is
// its GET's body and every pushed record its endpoint's status and body,
// because each is served by the router itself, as a sub-request carrying
// the caller's own headers.

type expectKey struct{}

// expected is the one route a sub-request may reach. GuardSubRequests
// refuses anything else before a handler runs.
type expected struct {
	method, pattern string
	reached         bool
}

// GuardSubRequests is global middleware (after routing, before auth): a
// sub-request reaches only the route it was checked against. A request from
// a client never carries the context value, so it passes untouched.
func GuardSubRequests() gin.HandlerFunc {
	return func(c *gin.Context) {
		e, ok := c.Request.Context().Value(expectKey{}).(*expected)
		if !ok {
			c.Next()
			return
		}
		if c.Request.Method != e.method || c.FullPath() != e.pattern {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": ErrRouteNotAllowed.Error()})
			return
		}
		e.reached = true
		c.Next()
	}
}

// response is what a sub-request answered.
type response struct {
	status     int
	body       json.RawMessage
	retryAfter *int
}

type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *recorder) Header() http.Header { return r.header }
func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
}
func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(b)
}

// forwarded are the caller's headers a sub-request keeps: who they are, and
// what the access logs record (evidence.ClientOf reads the address and the
// user agent).
var forwarded = []string{"Authorization", "User-Agent", "X-Forwarded-For", "X-Real-Ip"}

// serve runs method path through h as the caller of parent, allowed only to
// reach pattern.
func serve(ctx context.Context, h http.Handler, parent *http.Request, method, path, pattern string, body []byte) response {
	e := &expected{method: method, pattern: pattern}
	req, err := http.NewRequestWithContext(context.WithValue(ctx, expectKey{}, e), method, path, bytes.NewReader(body))
	if err != nil {
		return response{status: http.StatusBadRequest, body: errorBody(ErrRouteNotAllowed)}
	}
	for _, k := range forwarded {
		if v := parent.Header.Values(k); len(v) > 0 {
			req.Header[http.CanonicalHeaderKey(k)] = v
		}
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.RemoteAddr = parent.RemoteAddr

	rec := &recorder{header: http.Header{}}
	h.ServeHTTP(rec, req)
	out := response{status: rec.status, body: json.RawMessage(bytes.TrimSpace(rec.body.Bytes()))}
	if out.status == 0 {
		out.status = http.StatusOK
	}
	if !e.reached && out.status == http.StatusNotFound && len(out.body) == 0 {
		// The router found no route at all (gin's bare 404).
		out = response{status: http.StatusBadRequest, body: errorBody(ErrRouteNotAllowed)}
	}
	if len(out.body) == 0 {
		out.body = json.RawMessage("null")
	}
	if s := rec.header.Get("Retry-After"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			out.retryAfter = &n
		}
	}
	return out
}

func errorBody(err error) json.RawMessage {
	b, _ := json.Marshal(gin.H{"error": err.Error()})
	return b
}

// matches reports whether path fits a route pattern: the same number of
// segments, literals equal, every :param a non-empty segment.
func matches(pattern, path string) bool {
	ps, xs := strings.Split(pattern, "/"), strings.Split(path, "/")
	if len(ps) != len(xs) {
		return false
	}
	for i, p := range ps {
		switch {
		case strings.HasPrefix(p, ":"):
			if xs[i] == "" {
				return false
			}
		case p != xs[i]:
			return false
		}
	}
	return true
}

// cleanPath refuses anything a router or proxy might read differently from
// the pattern check: escapes, queries, fragments, dot or empty segments.
func cleanPath(path string) bool {
	if !strings.HasPrefix(path, "/v1/") || strings.ContainsAny(path, "%?#\\ ") {
		return false
	}
	// Control characters (NUL among them): no route takes one.
	if strings.ContainsFunc(path, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return false
	}
	for _, s := range strings.Split(path[1:], "/") {
		if s == "" || s == "." || s == ".." {
			return false
		}
	}
	return true
}
