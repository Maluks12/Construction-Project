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

// The catalogue is a field-for-field copy of the regime data, in data order.
func TestCatalogOf(t *testing.T) {
	ds := &regimes.DataSet{
		Regime: regimes.Regime{Code: "r", Name: "Regime", Version: "2014"},
		Checklists: []regimes.ChecklistTemplate{
			{Key: "b", Version: 2, Title: "B", Cadence: "daily", SourceRef: "reg 2", Items: []regimes.ChecklistItem{
				{Key: "x", Prompt: "X?", ResponseType: "pass_fail_na", SourceRef: "reg 2(a)"},
			}},
			{Key: "a", Version: 1, Title: "A", Cadence: "weekly", SourceRef: "reg 1", Items: []regimes.ChecklistItem{}},
		},
	}
	c := catalogOf(ds)
	if c.Regime != (RegimeRef{"r", "Regime", "2014"}) || len(c.Checklists) != 2 || len(c.Severities) != 4 {
		t.Fatalf("catalogue %+v", c)
	}
	b := c.Checklists[0]
	if b.Key != "b" || b.Version != 2 || b.Title != "B" || b.Cadence != "daily" || b.SourceRef != "reg 2" ||
		len(b.Items) != 1 || b.Items[0] != (ChecklistItem{"x", "X?", "pass_fail_na", "reg 2(a)"}) {
		t.Errorf("first template %+v, want a copy of b in data order", b)
	}
	if a := c.Checklists[1]; a.Key != "a" || a.Items == nil {
		t.Errorf("second template %+v, want a with items [] not null", a)
	}
}
