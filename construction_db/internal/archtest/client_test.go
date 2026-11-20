package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"slices"
	"strings"
	"testing"
)

// clientViolations: an access log's client comes from evidence.ClientOf,
// the one place that decides which X-Forwarded-For to believe (M12 review).
func clientViolations(files map[string]string) []string {
	var out []string
	fset := token.NewFileSet()
	for _, path := range slices.Sorted(maps.Keys(files)) {
		if strings.HasPrefix(path, "internal/core/evidence/") {
			continue
		}
		f, err := parser.ParseFile(fset, path, files[path], parser.SkipObjectResolution)
		if err != nil {
			out = append(out, fmt.Sprintf("%s: parse: %v", path, err))
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if cl, ok := n.(*ast.CompositeLit); ok {
				if s, ok := cl.Type.(*ast.SelectorExpr); ok && s.Sel.Name == "Client" {
					if x, ok := s.X.(*ast.Ident); ok && x.Name == "evidence" {
						out = append(out, fmt.Sprintf("%s:%d builds evidence.Client by hand; use evidence.ClientOf(c)", path, fset.Position(cl.Pos()).Line))
					}
				}
			}
			return true
		})
	}
	return out
}

func TestAccessLogClientFromClientOf(t *testing.T) {
	for _, v := range clientViolations(moduleSources(t)) {
		t.Error(v)
	}
	ok := map[string]string{
		"internal/core/evidence/handler.go": "package evidence\nvar _ = Client{IP: \"x\"}",
		"internal/core/files/handler.go":    "package files\nvar _ = evidence.ClientOf(c)",
	}
	if v := clientViolations(ok); len(v) != 0 {
		t.Fatalf("clean sources flagged: %v", v)
	}
	bad := map[string]string{"internal/core/exports/handler.go": "package exports\nvar _ = evidence.Client{IP: c.ClientIP()}"}
	if v := clientViolations(bad); len(v) != 1 {
		t.Errorf("hand-built client: %v", v)
	}
}
