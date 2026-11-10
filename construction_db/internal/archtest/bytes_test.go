package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Rev 4 §2.1: every view of evidence is logged. The only code that may reach
// evidence bytes is where that is guaranteed, so a new endpoint can't serve
// them by accident. A tripwire for honest mistakes, not a sandbox: it matches
// names, not types.
var (
	// Each method that reaches bytes, and the one file allowed to use it.
	byteMethods = map[string]string{
		"PresignGet":  "internal/core/evidence/access.go",   // after logging the view
		"ReadVersion": "internal/core/evidence/complete.go", // server-side hashing; serves nothing
	}
	// Only these directories may talk to S3 directly.
	s3Importers = []string{"internal/platform/storage/", "internal/platform/storagetest/"}
	s3Packages  = []string{"github.com/aws/aws-sdk-go-v2/service/s3", "github.com/aws/aws-sdk-go-v2/feature/s3"}
	// storage.Store's methods. A new one fails the guard until someone
	// decides whether it reaches bytes and adds it here.
	storeFile    = "internal/platform/storage/storage.go"
	storeMethods = []string{"PresignGet", "PresignPut", "ReadVersion", "Stat"}
)

// byteViolations checks non-test Go sources, keyed by slash path relative to
// the module root. allowed counts uses in the allowed files, so a caller can
// tell the check didn't pass vacuously.
func byteViolations(files map[string]string) (violations []string, allowed map[string]int) {
	allowed = map[string]int{}
	fset := token.NewFileSet()
	storeFound := false

	for _, path := range slices.Sorted(maps.Keys(files)) {
		f, err := parser.ParseFile(fset, path, files[path], parser.SkipObjectResolution)
		if err != nil {
			violations = append(violations, fmt.Sprintf("%s: parse: %v", path, err))
			continue
		}

		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if isS3Package(p) && !hasAnyPrefix(path, s3Importers) {
				violations = append(violations, fmt.Sprintf("%s:%d imports %s; only %s may talk to S3",
					path, fset.Position(imp.Pos()).Line, p, strings.Join(s3Importers, " and ")))
			}
		}

		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			want, reaches := byteMethods[sel.Sel.Name]
			switch {
			case !reaches:
			case path == want:
				allowed[path]++
			default:
				violations = append(violations, fmt.Sprintf("%s:%d uses .%s, which reaches evidence bytes; only %s may (every view must be logged first)",
					path, fset.Position(sel.Pos()).Line, sel.Sel.Name, want))
			}
			return true
		})

		if path == storeFile {
			if methods, ok := interfaceMethods(f, "Store"); ok {
				storeFound = true
				if !slices.Equal(methods, storeMethods) {
					violations = append(violations, fmt.Sprintf("%s: storage.Store has methods %v, the guard knows %v; decide whether a new method reaches bytes, then update internal/archtest",
						path, methods, storeMethods))
				}
			}
		}
	}
	if !storeFound {
		violations = append(violations, storeFile+": no storage.Store interface found")
	}
	slices.Sort(violations)
	return violations, allowed
}

func interfaceMethods(f *ast.File, name string) ([]string, bool) {
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != name {
				continue
			}
			it, ok := ts.Type.(*ast.InterfaceType)
			if !ok {
				return nil, false
			}
			var out []string
			for _, m := range it.Methods.List {
				for _, n := range m.Names {
					out = append(out, n.Name)
				}
			}
			slices.Sort(out)
			return out, true
		}
	}
	return nil, false
}

func isS3Package(p string) bool {
	for _, pkg := range s3Packages {
		if p == pkg || strings.HasPrefix(p, pkg+"/") {
			return true
		}
	}
	return false
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// moduleSources reads every non-test .go file in the module.
func moduleSources(t *testing.T) map[string]string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate bytes_test.go")
	}
	root := filepath.Join(filepath.Dir(thisFile), "..", "..")
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); path != root && (strings.HasPrefix(name, ".") || name == "testdata" || name == "vendor" || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(src)
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}
	return files
}

func TestEvidenceBytesGuard(t *testing.T) {
	violations, allowed := byteViolations(moduleSources(t))
	for _, v := range violations {
		t.Error(v)
	}
	for method, file := range byteMethods {
		if allowed[file] == 0 {
			t.Errorf("%s no longer uses .%s; the guard would pass vacuously, so update it", file, method)
		}
	}
}

func TestByteViolations(t *testing.T) {
	const store = `package storage
type Store interface {
	PresignPut()
	Stat()
	ReadVersion()
	PresignGet()
}`
	base := func(extra map[string]string) map[string]string {
		files := map[string]string{
			storeFile:                             store,
			"internal/core/evidence/access.go":    "package evidence\nfunc f() { s.store.PresignGet() }",
			"internal/core/evidence/complete.go":  "package evidence\nfunc g() { s.store.ReadVersion() }",
			"internal/platform/storage/s3.go":     "package storage\nimport \"github.com/aws/aws-sdk-go-v2/service/s3\"\nvar _ = s3.New",
			"internal/platform/storagetest/st.go": "package storagetest\nimport \"github.com/aws/aws-sdk-go-v2/service/s3/types\"\nvar _ types.Tag",
		}
		for k, v := range extra {
			files[k] = v
		}
		return files
	}

	tests := map[string]struct {
		extra map[string]string
		want  string // a substring of the single violation; "" means none
	}{
		"allowed uses":               {nil, ""},
		"presign in another handler": {map[string]string{"internal/core/sites/handler.go": "package sites\nfunc h() { store.PresignGet() }"}, "internal/core/sites/handler.go:2 uses .PresignGet"},
		"read in evidence's handler": {map[string]string{"internal/core/evidence/handler.go": "package evidence\nfunc h() { s.store.ReadVersion() }"}, "internal/core/evidence/handler.go:2 uses .ReadVersion"},
		"method value, not a call":   {map[string]string{"internal/core/x/x.go": "package x\nvar f = store.PresignGet"}, "internal/core/x/x.go:2 uses .PresignGet"},
		"s3 client outside storage":  {map[string]string{"internal/core/x/x.go": "package x\nimport \"github.com/aws/aws-sdk-go-v2/service/s3\"\nvar _ = s3.New"}, "imports github.com/aws/aws-sdk-go-v2/service/s3;"},
		"s3 manager outside storage": {map[string]string{"cmd/api/x.go": "package main\nimport \"github.com/aws/aws-sdk-go-v2/feature/s3/manager\"\nvar _ = manager.New"}, "imports github.com/aws/aws-sdk-go-v2/feature/s3/manager"},
		"new Store method":           {map[string]string{storeFile: strings.Replace(store, "Stat()", "Stat()\n\tCopy()", 1)}, "storage.Store has methods [Copy PresignGet PresignPut ReadVersion Stat]"},
		"Store method removed":       {map[string]string{storeFile: strings.Replace(store, "\tStat()\n", "", 1)}, "storage.Store has methods [PresignGet PresignPut ReadVersion]"},
		"Store interface gone":       {map[string]string{storeFile: "package storage"}, "no storage.Store interface found"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, allowed := byteViolations(base(tc.extra))
			switch {
			case tc.want == "" && len(got) != 0:
				t.Errorf("violations %q, want none", got)
			case tc.want != "" && (len(got) != 1 || !strings.Contains(got[0], tc.want)):
				t.Errorf("violations %q, want exactly one containing %q", got, tc.want)
			}
			if allowed["internal/core/evidence/access.go"] != 1 || allowed["internal/core/evidence/complete.go"] != 1 {
				t.Errorf("allowed uses %v, want one in each allowed file", allowed)
			}
		})
	}
}
