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
				if k, ok := n.Key.(*ast.Ident); ok && k.Name == "Clock" && strings.HasPrefix(path, "cmd/") {
					out = append(out, fmt.Sprintf("%s sets a Clock; the API and worker run on the real clock", at(n)))
				}
			case *ast.AssignStmt:
				for _, l := range n.Lhs {
					if s, ok := l.(*ast.SelectorExpr); ok && s.Sel.Name == "Clock" && strings.HasPrefix(path, "cmd/") {
						out = append(out, fmt.Sprintf("%s sets a Clock; the API and worker run on the real clock", at(n)))
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
		"testcontainers":     {"internal/core/sites/x.go": "package sites\nimport _ \"github.com/testcontainers/testcontainers-go/modules/postgres\""},
	} {
		if v := clockViolations(bad); len(v) == 0 {
			t.Errorf("%s: not flagged", name)
		}
	}
}
