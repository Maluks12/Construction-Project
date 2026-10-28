package archtest

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

const (
	module   = "construction_db"
	editions = module + "/internal/editions/"
)

// editionOf returns the edition a package belongs to, or "" if it isn't in one.
func editionOf(path string) string {
	rest, ok := strings.CutPrefix(path, editions)
	if !ok {
		return ""
	}
	name, _, _ := strings.Cut(rest, "/")
	return name
}

func mayImportEditions(path string) bool {
	return strings.HasPrefix(path, module+"/cmd/") ||
		path == module+"/internal/app" ||
		strings.HasPrefix(path, module+"/internal/app/")
}

func violations(graph map[string][]string) []string {
	var out []string
	for pkg, deps := range graph {
		own := editionOf(pkg)
		for _, dep := range deps {
			dep := editionOf(dep)
			switch {
			case dep == "":
			case own != "" && own != dep:
				out = append(out, fmt.Sprintf("%s depends on %s%s: editions must not depend on each other", pkg, editions, dep))
			case own == "" && !mayImportEditions(pkg):
				out = append(out, fmt.Sprintf("%s depends on %s%s: core must not depend on editions (Rev 4 §5.2)", pkg, editions, dep))
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// normalize folds test variants ("p [p.test]", "p.test") into their package.
func normalize(path string) string {
	path, _, _ = strings.Cut(path, " [")
	return strings.TrimSuffix(path, ".test")
}

// loadGraph lists every module package with its transitive deps, test code included.
func loadGraph(t *testing.T) map[string][]string {
	t.Helper()
	out, err := exec.Command("go", "list", "-test", "-f", `{{.ImportPath}}{{"\t"}}{{join .Deps ","}}`, module+"/...").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}

	graph := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pkg, deps, _ := strings.Cut(strings.TrimSpace(line), "\t")
		pkg = normalize(pkg)
		for _, d := range strings.Split(deps, ",") {
			if d = normalize(d); strings.HasPrefix(d, module+"/") && d != pkg {
				graph[pkg] = append(graph[pkg], d)
			}
		}
		if _, ok := graph[pkg]; !ok {
			graph[pkg] = nil
		}
	}
	return graph
}

func TestImportBoundary(t *testing.T) {
	graph := loadGraph(t)
	for _, want := range []string{module + "/internal/core/sites", editions + "construction"} {
		if _, ok := graph[want]; !ok {
			t.Fatalf("go list output is missing %s; the check would pass vacuously", want)
		}
	}
	for _, v := range violations(graph) {
		t.Error(v)
	}
}

func TestViolations(t *testing.T) {
	const (
		sites        = module + "/internal/core/sites"
		reg          = module + "/internal/regimes"
		app          = module + "/internal/app"
		api          = module + "/cmd/api"
		construction = editions + "construction"
		reg4         = editions + "construction/reg4"
		mining       = editions + "mining"
	)

	tests := map[string]struct {
		graph map[string][]string
		want  []string
	}{
		"core imports an edition": {
			map[string][]string{sites: {construction}},
			[]string{sites + " depends on " + construction + ": core must not depend on editions (Rev 4 §5.2)"},
		},
		"core reaches an edition transitively": {
			map[string][]string{sites: {app, construction}},
			[]string{sites + " depends on " + construction + ": core must not depend on editions (Rev 4 §5.2)"},
		},
		"regimes imports an edition": {
			map[string][]string{reg: {reg4}},
			[]string{reg + " depends on " + construction + ": core must not depend on editions (Rev 4 §5.2)"},
		},
		"edition imports another edition": {
			map[string][]string{construction: {mining}},
			[]string{construction + " depends on " + mining + ": editions must not depend on each other"},
		},
		"allowed": {
			map[string][]string{
				app:          {construction, sites},
				api:          {app, construction},
				construction: {sites},
				reg4:         {construction, sites},
			},
			nil,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := violations(tc.graph); !slices.Equal(got, tc.want) {
				t.Errorf("violations =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}
