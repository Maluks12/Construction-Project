package evidence

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

// apiDoc is docs/api/evidence.md, the reference the frontend mirrors.
func apiDoc(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate apidoc_test.go")
	}
	doc, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "docs", "api", "evidence.md"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(doc), "\r\n", "\n")
}

type tsField struct{ optional, nullable bool }

var (
	tsInterface = regexp.MustCompile(`(?s)export interface (\w+) \{\n(.*?)\n\}`)
	tsFieldLine = regexp.MustCompile(`(?m)^\s+(\w+)(\?)?: (.+);$`)
	tsUnion     = regexp.MustCompile(`(?s)export type (\w+) =([^;]+);`)
	tsLiteral   = regexp.MustCompile(`"([^"]+)"`)
)

// The TypeScript in docs/api/evidence.md must match the JSON the Go code
// sends, field for field, so the frontend can mirror it without guessing.
func TestAPIDoc(t *testing.T) {
	doc := apiDoc(t)
	_, block, ok := strings.Cut(doc, "<!-- types -->")
	if !ok {
		t.Fatal("docs/api/evidence.md has no <!-- types --> block")
	}

	interfaces := map[string]map[string]tsField{}
	for _, m := range tsInterface.FindAllStringSubmatch(block, -1) {
		fields := map[string]tsField{}
		for _, f := range tsFieldLine.FindAllStringSubmatch(m[2], -1) {
			fields[f[1]] = tsField{optional: f[2] == "?", nullable: strings.Contains(f[3], "| null")}
		}
		interfaces[m[1]] = fields
	}
	unions := map[string][]string{}
	for _, m := range tsUnion.FindAllStringSubmatch(block, -1) {
		var lits []string
		for _, l := range tsLiteral.FindAllStringSubmatch(m[2], -1) {
			lits = append(lits, l[1])
		}
		slices.Sort(lits)
		unions[m[1]] = lits
	}

	// Responses: optional exactly when omitempty. Requests: optional exactly
	// when the field may be left out (a pointer).
	for name, v := range map[string]any{
		"Evidence": Evidence{}, "ChainEntry": ChainEntry{}, "Upload": Upload{},
		"ChainVerification": ChainVerification{}, "ChainBreak": ChainBreak{}, "DeclareRequest": DeclareRequest{},
		"ViewURL": ViewURL{},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := interfaces[name]
			if !ok {
				t.Fatalf("no interface %s in the doc", name)
			}
			want := map[string]tsField{}
			typ := reflect.TypeOf(v)
			for i := range typ.NumField() {
				f := typ.Field(i)
				tag, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
				if tag == "-" || tag == "" {
					continue
				}
				pointer := f.Type.Kind() == reflect.Pointer
				optional := strings.Contains(opts, "omitempty")
				if name == "DeclareRequest" {
					optional = pointer
				}
				// A pointer that is omitted when nil is never null on the wire.
				want[tag] = tsField{optional: optional, nullable: pointer && !strings.Contains(opts, "omitempty")}
			}
			if !maps.Equal(got, want) {
				t.Errorf("doc fields %v,\nGo sends %v", got, want)
			}
		})
	}

	// Bodies the handlers build inline.
	for name, want := range map[string][]string{
		"EvidenceList":      {"evidence"},
		"EvidenceRejection": {"error", "rejected_reason"},
	} {
		got := slices.Sorted(maps.Keys(interfaces[name]))
		if !slices.Equal(got, want) {
			t.Errorf("%s fields %v, want %v", name, got, want)
		}
	}

	for name, want := range map[string][]string{
		"EvidenceStatus": {StatusPending, StatusVerified, StatusRejected},
		"RejectedReason": slices.Collect(maps.Keys(rejections)),
		"ChainBreakReason": {BreakSeqGap, BreakPrevMismatch, BreakMetadataChanged, BreakChainHashMismatch,
			BreakEntryWithoutVerifiedEvidence, BreakHeadMismatch, BreakVerifiedWithoutEntry},
	} {
		slices.Sort(want)
		if got := unions[name]; !slices.Equal(got, want) {
			t.Errorf("union %s is %v, want %v", name, got, want)
		}
	}

	// Each rejection's code and exact message appear in the doc's table.
	for reason, msg := range rejections {
		if row := "| `" + reason + "` | `" + msg + "` |"; !strings.Contains(doc, row) {
			t.Errorf("doc has no row %q", row)
		}
	}
}
