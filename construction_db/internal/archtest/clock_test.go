package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The app tests can run as if at another hour: SITEPROOF_TEST_SAST, a moved
// clock (clock.Offset) and a libfaketime Postgres (dbtest.StartAt). None of
// that may reach the API or the worker, which always run on the real clock.
var (
	testHelperDirs = []string{"internal/platform/dbtest/", "internal/platform/storagetest/", "internal/platform/containertest/"}
	testHelperPkgs = []string{
		"construction_db/internal/platform/dbtest",
		"construction_db/internal/platform/storagetest",
		"construction_db/internal/platform/containertest",
		"github.com/testcontainers/testcontainers-go",
	}
)

// clockViolations checks non-test Go sources, keyed by slash path relative to
// the module root.
// testOnlyFields: fields the binaries never set; tests do, to move time or
// shorten waits (M12 review: the export builder's timings).
var testOnlyFields = map[string]string{
	"Clock":    "the API and worker run on the real clock",
	"Settle":   "the export settle window is the database default in production",
	"Lease":    "the export lease is the default in production",
	"Backoff":  "the export backoff is the database default in production",
	"OnlyOrgs": "the worker serves every organisation",
	// SYNC: tests shorten them.
	"PullLagLimit": "the pull lag limit is the default in production",
	"PullBudget":   "a pull page's time budget is the default in production",
	"PushReserve":  "a push's reserve before the deadline is the default in production",
	// CI run #90: tests lower it; the API refuses to start below the default.
	"PasswordCost": "passwords are hashed at bcrypt's default cost in production",
}

func clockViolations(files map[string]string) []string {
	var out []string
	fset := token.NewFileSet()
	for _, path := range slices.Sorted(maps.Keys(files)) {
		f, err := parser.ParseFile(fset, path, files[path], parser.SkipObjectResolution)
		if err != nil {
			out = append(out, fmt.Sprintf("%s: parse: %v", path, err))
			continue
		}
		at := func(n ast.Node) string { return fmt.Sprintf("%s:%d", path, fset.Position(n.Pos()).Line) }
		helper := hasAnyPrefix(path, testHelperDirs)

		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			for _, h := range testHelperPkgs {
				if (p == h || strings.HasPrefix(p, h+"/")) && !helper {
					out = append(out, fmt.Sprintf("%s imports %s, a test helper", at(imp), p))
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.BasicLit:
				if n.Kind != token.STRING {
					break
				}
				if strings.Contains(n.Value, "SITEPROOF_TEST_SAST") {
					out = append(out, fmt.Sprintf("%s names SITEPROOF_TEST_SAST; only tests may read it", at(n)))
				}
				if strings.Contains(strings.ToLower(n.Value), "faketime") && !strings.HasPrefix(path, "internal/platform/dbtest/") {
					out = append(out, fmt.Sprintf("%s mentions faketime; only dbtest may", at(n)))
				}
			case *ast.SelectorExpr:
				if x, ok := n.X.(*ast.Ident); ok && x.Name == "clock" && n.Sel.Name == "Offset" {
					out = append(out, fmt.Sprintf("%s uses clock.Offset; only tests move the clock", at(n)))
				}
			case *ast.KeyValueExpr:
				if k, ok := n.Key.(*ast.Ident); ok && strings.HasPrefix(path, "cmd/") {
					if why, test := testOnlyFields[k.Name]; test {
						out = append(out, fmt.Sprintf("%s sets %s; %s", at(n), k.Name, why))
					}
				}
			case *ast.AssignStmt:
				for _, l := range n.Lhs {
					if s, ok := l.(*ast.SelectorExpr); ok && strings.HasPrefix(path, "cmd/") {
						if why, test := testOnlyFields[s.Sel.Name]; test {
							out = append(out, fmt.Sprintf("%s sets %s; %s", at(n), s.Sel.Name, why))
						}
					}
				}
			}
			return true
		})
	}
	return out
}

func TestProductionUsesTheRealClock(t *testing.T) {
	files := moduleSources(t)
	for _, v := range clockViolations(files) {
		t.Error(v)
	}
	// Not vacuous: the binaries and the clock are where the guard looks.
	for _, f := range []string{"cmd/api/main.go", "cmd/worker/main.go", "internal/platform/clock/clock.go", "internal/platform/dbtest/dbtest.go"} {
		if _, ok := files[f]; !ok {
			t.Errorf("%s is gone; update the guard", f)
		}
	}
}

// The API checks the bcrypt cost before anything else at startup.
func TestAPIChecksPasswordCost(t *testing.T) {
	src, ok := moduleSources(t)["cmd/api/main.go"]
	if !ok {
		t.Fatal("cmd/api/main.go is gone; update the guard")
	}
	if !callsSelector(t, src, "auth", "CheckPasswordCost") {
		t.Error("cmd/api/main.go must call auth.CheckPasswordCost at startup")
	}
}

// Each binary connects with its own lock limits: the worker never takes the
// API's 5s (the export builder waits longer).
func TestBinariesUseTheirBounds(t *testing.T) {
	files := moduleSources(t)
	for path, want := range map[string]string{"cmd/api/main.go": "APIBounds", "cmd/worker/main.go": "WorkerBounds"} {
		src, ok := files[path]
		if !ok {
			t.Fatalf("%s is gone; update the guard", path)
		}
		for _, b := range []string{"APIBounds", "WorkerBounds"} {
			if got := usesSelector(t, src, "database", b); got != (b == want) {
				t.Errorf("%s uses database.%s: %v; want only %s", path, b, got, want)
			}
		}
	}
}

func callsSelector(t *testing.T, src, pkg, name string) bool {
	t.Helper()
	return findSelector(t, src, pkg, name, true)
}

func usesSelector(t *testing.T, src, pkg, name string) bool {
	t.Helper()
	return findSelector(t, src, pkg, name, false)
}

func findSelector(t *testing.T, src, pkg, name string, call bool) bool {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "main.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		var s *ast.SelectorExpr
		if c, ok := n.(*ast.CallExpr); ok && call {
			s, _ = c.Fun.(*ast.SelectorExpr)
		} else if !call {
			s, _ = n.(*ast.SelectorExpr)
		}
		if s != nil {
			if x, ok := s.X.(*ast.Ident); ok && x.Name == pkg && s.Sel.Name == name {
				found = true
			}
		}
		return !found
	})
	return found
}

func TestClockViolations(t *testing.T) {
	ok := map[string]string{
		"cmd/api/main.go":                  "package main\nfunc main() { _ = app.HTTPOptions{RequestTimeout: 1} }",
		"internal/platform/clock/clock.go": "package clock\nfunc Offset() {}",
		"internal/platform/dbtest/db.go":   "package dbtest\nimport \"github.com/testcontainers/testcontainers-go\"\n// SITEPROOF_TEST_SAST\nvar _ = \"FAKETIME\"\nvar _ = testcontainers.WithEnv",
	}
	if v := clockViolations(ok); len(v) != 0 {
		t.Fatalf("clean sources flagged: %v", v)
	}
	for name, bad := range map[string]map[string]string{
		"env var read":       {"internal/platform/config/config.go": "package config\nimport \"os\"\nvar _ = os.Getenv(\"SITEPROOF_TEST_SAST\")"},
		"faketime in config": {"internal/platform/config/config.go": "package config\nvar _ = \"LD_PRELOAD=libfaketime\""},
		"moved clock":        {"internal/app/router.go": "package app\nvar _ = clock.Offset(5)"},
		"cmd sets Clock":     {"cmd/worker/main.go": "package main\nvar _ = notifications.Jobs{Clock: c}"},
		"cmd assigns Clock":  {"cmd/api/main.go": "package main\nfunc main() { opts.Clock = c }"},
		"cmd imports dbtest": {"cmd/api/main.go": "package main\nimport _ \"construction_db/internal/platform/dbtest\""},
		"cmd sets Settle":    {"cmd/worker/main.go": "package main\nvar _ = exports.Builder{Settle: time.Second}"},
		"cmd sets Lease":     {"cmd/worker/main.go": "package main\nvar _ = exports.Builder{Lease: time.Second}"},
		"cmd assigns Lease":  {"cmd/worker/main.go": "package main\nfunc main() { b.Lease = time.Second }"},
		"cmd sets Backoff":   {"cmd/worker/main.go": "package main\nvar _ = exports.Builder{Backoff: time.Second}"},
		"cmd sets OnlyOrgs":  {"cmd/worker/main.go": "package main\nvar _ = exports.Builder{OnlyOrgs: nil}"},
		"cmd lowers cost":    {"cmd/api/main.go": "package main\nfunc main() { auth.PasswordCost = 4 }"},
		"testcontainers":     {"internal/core/sites/x.go": "package sites\nimport _ \"github.com/testcontainers/testcontainers-go/modules/postgres\""},
	} {
		if v := clockViolations(bad); len(v) == 0 {
			t.Errorf("%s: not flagged", name)
		}
	}
}
