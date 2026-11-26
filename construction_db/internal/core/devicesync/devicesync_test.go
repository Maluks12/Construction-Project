package devicesync

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestCursorRoundTrip(t *testing.T) {
	c := cursor{V: cursorVersion, Epoch: "e", Scope: "s", User: "u", Role: "supervisor", Lo: "10", Hi: "20", Bounded: true, Kind: "incident", After: "x"}
	got, err := decodeCursor(c.encode())
	if err != nil || got != c {
		t.Fatalf("%+v, %v", got, err)
	}
	done := cursor{V: cursorVersion, Epoch: "e", Scope: "s", Lo: "20"}
	if got, err := decodeCursor(done.encode()); err != nil || got.paging() {
		t.Fatalf("a finished pass decoded as %+v (%v)", got, err)
	}
}

func TestBadCursors(t *testing.T) {
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	for name, raw := range map[string]string{
		"not base64":        "!!!",
		"not json":          enc("nope"),
		"another version":   enc(`{"v":2,"e":"e","s":"s","lo":"1"}`),
		"no epoch":          enc(`{"v":1,"s":"s","lo":"1"}`),
		"no scope":          enc(`{"v":1,"e":"e","lo":"1"}`),
		"lo not an xid":     enc(`{"v":1,"e":"e","s":"s","lo":"-1"}`),
		"hi not an xid":     enc(`{"v":1,"e":"e","s":"s","lo":"1","hi":"x"}`),
		"a page with no hi": enc(`{"v":1,"e":"e","s":"s","lo":"1","k":"incident","a":"x"}`),
	} {
		if _, err := decodeCursor(raw); err != ErrBadCursor {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestRouteMatching(t *testing.T) {
	p := "/v1/sites/:id/incidents/:incidentId/versions/:version"
	for path, want := range map[string]bool{
		"/v1/sites/a/incidents/b/versions/1":   true,
		"/v1/sites/a/incidents/b/versions":     false,
		"/v1/sites/a/incidents/b/versions/1/x": false,
		"/v1/sites//incidents/b/versions/1":    false,
		"/v1/sites/a/incident/b/versions/1":    false,
	} {
		if got := matches(p, path); got != want {
			t.Errorf("%s: %v", path, got)
		}
	}
	for path, want := range map[string]bool{
		"/v1/sites/a/b":          true,
		"/v2/sites/a":            false,
		"/v1/sites/../a":         false,
		"/v1/sites/./a":          false,
		"/v1/sites//a":           false,
		"/v1/sites/a?x=1":        false,
		"/v1/sites/a#x":          false,
		"/v1/sites/a%2Fb":        false,
		"/v1/sites/a\\b":         false,
		"/v1/sites/a b":          false,
		"/v1/sites/a/":           false,
		strings.Repeat("/v1", 1): false,
	} {
		if got := cleanPath(path); got != want {
			t.Errorf("cleanPath(%q) = %v", path, got)
		}
	}
	s := &Service{push: CorePushRoutes()}
	if got := s.allowed("PUT", "/v1/sites/a/incidents/b/versions/2"); got != p {
		t.Errorf("allowed %q", got)
	}
	if got := s.allowed("POST", "/v1/sites/a/incidents/b/versions/2"); got != "" {
		t.Errorf("POST matched %q", got)
	}
}

func TestKindQueriesWindowEveryDep(t *testing.T) {
	for _, k := range CoreKinds() {
		for _, d := range k.Deps {
			if !strings.Contains(d.SQL, "$2::text::xid8") || !strings.Contains(d.SQL, "$3::text::xid8") {
				t.Errorf("%s: a dep on %s isn't windowed", k.Name, d.Table)
			}
			if !strings.Contains(d.SQL, d.Table) {
				t.Errorf("%s: a dep names %s but doesn't read it", k.Name, d.Table)
			}
		}
	}
}
