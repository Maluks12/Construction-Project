// Package apidoctest checks the endpoint references in docs/api against the
// Go types the API sends, so the frontend can mirror them without guessing.
// Each reference has a "<!-- types -->" marker followed by TypeScript.
package apidoctest

import (
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

type Field struct{ Optional, Nullable bool }

// Doc is one parsed reference. Text is the whole file; Interfaces and Unions
// come from the types block only.
type Doc struct {
	Name       string
	Text       string
	Interfaces map[string]map[string]Field
	Unions     map[string][]string // literals, sorted
}

var (
	tsInterface = regexp.MustCompile(`(?s)export interface (\w+) \{\n(.*?)\n\}`)
	tsFieldLine = regexp.MustCompile(`(?m)^\s+(\w+)(\?)?: (.+);$`)
	tsUnion     = regexp.MustCompile(`(?s)export type (\w+) =([^;]+);`)
	tsLiteral   = regexp.MustCompile(`"([^"]+)"`)
)

// Load reads docs/api/<name>.md from the repository.
func Load(t testing.TB, name string) *Doc {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate apidoctest.go")
	}
	path := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "docs", "api", name+".md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := &Doc{
		Name:       name,
		Text:       strings.ReplaceAll(string(raw), "\r\n", "\n"),
		Interfaces: map[string]map[string]Field{},
		Unions:     map[string][]string{},
	}
	_, block, ok := strings.Cut(doc.Text, "<!-- types -->")
	if !ok {
		t.Fatalf("docs/api/%s.md has no <!-- types --> block", name)
	}
	for _, m := range tsInterface.FindAllStringSubmatch(block, -1) {
		fields := map[string]Field{}
		for _, f := range tsFieldLine.FindAllStringSubmatch(m[2], -1) {
			fields[f[1]] = Field{Optional: f[2] == "?", Nullable: strings.Contains(f[3], "| null")}
		}
		doc.Interfaces[m[1]] = fields
	}
	for _, m := range tsUnion.FindAllStringSubmatch(block, -1) {
		var lits []string
		for _, l := range tsLiteral.FindAllStringSubmatch(m[2], -1) {
			lits = append(lits, l[1])
		}
		slices.Sort(lits)
		doc.Unions[m[1]] = lits
	}
	return doc
}

// CheckStruct compares interface tsName with the JSON fields of v.
// Responses: a field is optional exactly when it is omitempty, and nullable
// when it is a pointer that isn't omitted (an omitted nil is never null on
// the wire). Requests (request true): optional exactly when it is a pointer,
// since only a pointer can tell "left out" from a zero value.
func (d *Doc) CheckStruct(t testing.TB, tsName string, v any, request bool) {
	t.Helper()
	got, ok := d.Interfaces[tsName]
	if !ok {
		t.Errorf("no interface %s in docs/api/%s.md", tsName, d.Name)
		return
	}
	want := map[string]Field{}
	typ := reflect.TypeOf(v)
	for i := range typ.NumField() {
		f := typ.Field(i)
		tag, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		if tag == "-" || tag == "" {
			continue
		}
		pointer := f.Type.Kind() == reflect.Pointer
		omitempty := strings.Contains(opts, "omitempty")
		optional := omitempty
		if request {
			optional = pointer
		}
		want[tag] = Field{Optional: optional, Nullable: pointer && !omitempty}
	}
	if !maps.Equal(got, want) {
		t.Errorf("%s: doc fields %v,\nGo sends %v", tsName, got, want)
	}
}

// CheckFields compares the field names of interface tsName, for bodies a
// handler builds inline.
func (d *Doc) CheckFields(t testing.TB, tsName string, want []string) {
	t.Helper()
	got := slices.Sorted(maps.Keys(d.Interfaces[tsName]))
	want = slices.Sorted(slices.Values(want))
	if !slices.Equal(got, want) {
		t.Errorf("%s fields %v, want %v", tsName, got, want)
	}
}

// CheckUnion compares the string literals of type tsName with values.
func (d *Doc) CheckUnion(t testing.TB, tsName string, values []string) {
	t.Helper()
	want := slices.Sorted(slices.Values(values))
	if got := d.Unions[tsName]; !slices.Equal(got, want) {
		t.Errorf("union %s is %v, want %v", tsName, got, want)
	}
}
