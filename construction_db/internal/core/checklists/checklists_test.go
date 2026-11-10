package checklists

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"construction_db/internal/platform/apidoctest"
	"construction_db/internal/regimes"
)

// The TypeScript in docs/api/inspections.md must match the JSON the Go code sends.
func TestAPIDoc(t *testing.T) {
	doc := apidoctest.Load(t, "inspections")

	for name, v := range map[string]any{
		"ChecklistCatalog": ChecklistCatalog{}, "RegimeRef": RegimeRef{}, "ChecklistTemplate": ChecklistTemplate{},
		"ChecklistItem": ChecklistItem{}, "SeverityLevel": SeverityLevel{},
	} {
		t.Run(name, func(t *testing.T) { doc.CheckStruct(t, name, v, false) })
	}

	var keys []string
	for _, s := range Severities {
		keys = append(keys, s.Key)
	}
	doc.CheckUnion(t, "Severity", keys)
	doc.CheckUnion(t, "Cadence", regimes.Cadences)
	doc.CheckUnion(t, "ResponseType", SupportedResponseTypes)
}

// Each definition appears word for word in the API reference and the backend
// CLAUDE.md, so whoever reads either sees what the inspector sees.
func TestSeverityDefinitions(t *testing.T) {
	want := []string{SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical}
	var got []string
	for _, s := range Severities {
		got = append(got, s.Key)
		if s.Label == "" || !strings.HasSuffix(s.Definition, ".") || strings.Contains(s.Definition, "|") {
			t.Errorf("%s: a label and a one-sentence definition without | are required, got %+v", s.Key, s)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("severities %v, want %v (least severe first)", got, want)
	}

	_, thisFile, _, _ := runtime.Caller(0)
	claudeMD, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"docs/api/inspections.md":   apidoctest.Load(t, "inspections").Text,
		"construction_db/CLAUDE.md": strings.ReplaceAll(string(claudeMD), "\r\n", "\n"),
	}
	for name, text := range files {
		for _, s := range Severities {
			if row := "| `" + s.Key + "` | " + s.Label + " | " + s.Definition + " |"; !strings.Contains(text, row) {
				t.Errorf("%s has no row %q", name, row)
			}
		}
	}
}

// The engine records only the response types it supports. A shipped template
// using another would be startable but unanswerable, so it fails here first.
func TestShippedTemplatesUseSupportedResponseTypes(t *testing.T) {
	reg, err := regimes.Load()
	if err != nil {
		t.Fatalf("load regimes: %v", err)
	}
	for _, code := range reg.Codes() {
		ds, _ := reg.Get(code)
		for _, tmpl := range ds.Checklists {
			for _, it := range tmpl.Items {
				if !slices.Contains(SupportedResponseTypes, it.ResponseType) {
					t.Errorf("%s %s/%s: response type %q is not supported yet (supported: %v)",
						code, tmpl.Key, it.Key, it.ResponseType, SupportedResponseTypes)
				}
			}
		}
	}
}

// A data set with two versions of one template: what the tests below share.
func twoVersions() *regimes.DataSet {
	return &regimes.DataSet{
		Regime: regimes.Regime{Code: "r", Name: "Regime", Version: "2014"},
		Checklists: []regimes.ChecklistTemplate{
			{Key: "b", Version: 1, Title: "B", Cadence: "daily", SourceRef: "reg 2", Items: []regimes.ChecklistItem{
				{Key: "x", Prompt: "X?", ResponseType: "pass_fail_na", SourceRef: "reg 2(a)"},
			}},
			{Key: "b", Version: 2, Title: "B", Cadence: "daily", SourceRef: "reg 2", Items: []regimes.ChecklistItem{
				{Key: "x", Prompt: "X, and is it tagged?", ResponseType: "pass_fail_na", SourceRef: "reg 2(a)"},
			}},
			{Key: "a", Version: 1, Title: "A", Cadence: "weekly", SourceRef: "reg 1", Items: []regimes.ChecklistItem{}},
		},
	}
}

// The catalogue offers the latest version of each template, a field-for-field
// copy in key order, with that version's content hash.
func TestCatalogOf(t *testing.T) {
	ds := twoVersions()
	c := catalogOf(ds)
	if c.Regime != (RegimeRef{"r", "Regime", "2014"}) || len(c.Checklists) != 2 || len(c.Severities) != 4 {
		t.Fatalf("catalogue %+v, want 2 templates (latest versions only) and 4 severities", c)
	}
	b := c.Checklists[0]
	if b.Key != "b" || b.Version != 2 || b.SHA256 != ds.Checklists[1].ContentHash() || b.Title != "B" ||
		b.Cadence != "daily" || b.SourceRef != "reg 2" ||
		len(b.Items) != 1 || b.Items[0] != (ChecklistItem{"x", "X, and is it tagged?", "pass_fail_na", "reg 2(a)"}) {
		t.Errorf("first template %+v, want version 2 of b with its hash", b)
	}
	if a := c.Checklists[1]; a.Key != "a" || a.Items == nil || a.SHA256 != ds.Checklists[2].ContentHash() {
		t.Errorf("second template %+v, want a with items [] not null and its hash", a)
	}
}

// Start accepts any shipped version, but only with that version's own hash.
func TestResolveTemplate(t *testing.T) {
	ds := twoVersions()
	v1, v2 := ds.Checklists[0].ContentHash(), ds.Checklists[1].ContentHash()

	tests := []struct {
		name    string
		key     string
		version int
		sha     string
		want    error
	}{
		{"latest version, its hash", "b", 2, v2, nil},
		{"older version, its own hash (an offline start before v2 shipped)", "b", 1, v1, nil},
		{"hash in upper case", "b", 2, strings.ToUpper(v2), nil},
		{"v1's hash sent with v2", "b", 2, v1, ErrTemplateMismatch},
		{"wrong hash", "b", 2, strings.Repeat("0", 64), ErrTemplateMismatch},
		{"malformed hash", "b", 2, "not-hex", ErrTemplateMismatch},
		{"no hash", "b", 2, "", ErrTemplateMismatch},
		{"unknown version", "b", 3, v2, ErrUnknownTemplateVersion},
		{"unknown key", "c", 1, v1, ErrUnknownTemplateVersion},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpl, err := resolveTemplate(ds, tt.key, tt.version, tt.sha)
			if err != tt.want {
				t.Fatalf("err %v, want %v", err, tt.want)
			}
			if err == nil && (tmpl.Key != tt.key || tmpl.Version != tt.version) {
				t.Errorf("template %s@%d, want %s@%d", tmpl.Key, tmpl.Version, tt.key, tt.version)
			}
		})
	}
	if ErrUnknownTemplateVersion.Error() != "unknown checklist template version" || ErrTemplateMismatch.Error() != "checklist template does not match" {
		t.Error("the error texts are fixed: start (7.5) sends them as 400 bodies")
	}
}
