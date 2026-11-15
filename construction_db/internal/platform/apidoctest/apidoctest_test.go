package apidoctest

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"testing"
)

type inner struct {
	A string  `json:"a"`
	B *string `json:"b"`
}

type outer struct {
	inner          // unexported type: its exported fields are still sent
	C     int      `json:"c,omitempty"`
	D     string   // no tag: sent as "D"
	E     *float64 `json:"e,omitempty"`
	e     string   // unexported: never sent
	Skip  string   `json:"-"`
}

// The checker must see exactly the keys encoding/json sends.
func TestFieldsMatchEncodingJSON(t *testing.T) {
	b, e := "b", 1.5
	raw, err := json.Marshal(outer{inner: inner{A: "a", B: &b}, C: 1, D: "d", E: &e, e: "x", Skip: "s"})
	if err != nil {
		t.Fatal(err)
	}
	var sent map[string]any
	if err := json.Unmarshal(raw, &sent); err != nil {
		t.Fatal(err)
	}
	got := map[string]Field{}
	jsonFields(reflect.TypeOf(outer{}), false, got)
	if g, w := slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(sent)); !slices.Equal(g, w) {
		t.Errorf("checker sees %v, encoding/json sends %v", g, w)
	}
}

// recorder stands in for *testing.T so a failed check can be observed.
type recorder struct {
	testing.TB
	errs []string
}

func (r *recorder) Helper() {}
func (r *recorder) Errorf(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}

func TestCheckStruct(t *testing.T) {
	full := map[string]Field{
		"a": {},
		"b": {Nullable: true},
		"c": {Optional: true},
		"D": {},
		"e": {Optional: true}, // a pointer omitted when nil is never null on the wire
	}
	without := func(name string) map[string]Field {
		m := maps.Clone(full)
		delete(m, name)
		return m
	}
	with := func(name string, f Field) map[string]Field {
		m := maps.Clone(full)
		m[name] = f
		return m
	}

	tests := map[string]struct {
		doc     map[string]Field
		request bool
		ok      bool
	}{
		"matches":                             {full, false, true},
		"misses a field of the embedded type": {without("a"), false, false},
		"misses the untagged field":           {without("D"), false, false},
		"has a field Go doesn't send":         {with("extra", Field{}), false, false},
		"gets nullable wrong":                 {with("b", Field{}), false, false},
		"gets optional wrong":                 {with("c", Field{}), false, false},
		// Requests: optional exactly when the field is a pointer.
		"request rules": {map[string]Field{
			"a": {}, "b": {Optional: true, Nullable: true}, "c": {}, "D": {}, "e": {Optional: true},
		}, true, true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			doc := &Doc{Name: "test", Interfaces: map[string]map[string]Field{"Outer": tc.doc}}
			r := &recorder{}
			doc.CheckStruct(r, "Outer", outer{}, tc.request)
			if ok := len(r.errs) == 0; ok != tc.ok {
				t.Errorf("passed = %v, want %v (errors: %v)", ok, tc.ok, r.errs)
			}
		})
	}
}
