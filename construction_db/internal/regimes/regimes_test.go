package regimes

import (
	"maps"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// A minimal valid data set. The content is made up; real legal content lives
// in data/ and is checked against its sources.
const (
	regimeJSON = `{"code": "test_regime", "name": "Test regime", "edition": "construction",
		"jurisdiction": "ZA", "version": "1", "effective_from": "2014-02-07"}`
	checklistsJSON = `[{"key": "scaffold_daily", "version": 1, "title": "Scaffold daily check",
		"cadence": "daily", "source_ref": "test ref",
		"items": [{"key": "base_plates", "prompt": "Base plates sound?", "response_type": "pass_fail_na"}]}]`
	deadlinesJSON = `[{"key": "test_rule", "label": "Report it", "period": 7, "unit": "calendar_days",
		"direction": "after", "basis": "incident_occurred_at", "source_ref": "test ref"},
		{"key": "test_now", "label": "Phone it in", "period": 0, "unit": "immediately",
		"direction": "after", "basis": "incident_occurred_at", "source_ref": "test ref"}]`
	taxonomyJSON = `[{"key": "injury", "label": "Injury", "reportable": true, "report_rules": ["test_rule", "test_now"]},
		{"key": "near_miss", "label": "Near miss", "reportable": false}]`
	vocabularyJSON = `[{"key": "site_diary", "label": "Site diary"}]`
)

func validFS() fstest.MapFS {
	return fstest.MapFS{
		"test_regime/regime.json":     {Data: []byte(regimeJSON)},
		"test_regime/checklists.json": {Data: []byte(checklistsJSON)},
		"test_regime/deadlines.json":  {Data: []byte(deadlinesJSON)},
		"test_regime/taxonomies.json": {Data: []byte(taxonomyJSON)},
		"test_regime/vocabulary.json": {Data: []byte(vocabularyJSON)},
	}
}

// with returns a copy of fsys with name set to content, or removed when
// content is empty.
func with(fsys fstest.MapFS, name, content string) fstest.MapFS {
	out := maps.Clone(fsys)
	if content == "" {
		delete(out, name)
	} else {
		out[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return out
}

func TestLoadEmbedded(t *testing.T) {
	reg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := reg.Codes(); !slices.Equal(got, []string{"za_construction_2014"}) {
		t.Errorf("Codes() = %v", got)
	}

	ds, ok := reg.Get("za_construction_2014")
	if !ok {
		t.Fatal("za_construction_2014 not loaded")
	}
	// Must match the seed row in migrations/000001.
	want := Regime{
		Code:          "za_construction_2014",
		Name:          "Construction Regulations, 2014",
		Edition:       "construction",
		Jurisdiction:  "ZA",
		Version:       "2014",
		EffectiveFrom: "2014-02-07",
	}
	if ds.Regime != want {
		t.Errorf("regime = %+v, want %+v", ds.Regime, want)
	}
}

func TestLoadFSValid(t *testing.T) {
	reg, err := LoadFS(validFS())
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	ds, ok := reg.Get("test_regime")
	if !ok {
		t.Fatal("test_regime not loaded")
	}
	if len(ds.Checklists) != 1 || len(ds.Checklists[0].Items) != 1 {
		t.Errorf("checklists = %+v", ds.Checklists)
	}
	if len(ds.Taxonomy) != 2 || !slices.Equal(ds.Taxonomy[0].ReportRules, []string{"test_rule", "test_now"}) {
		t.Errorf("taxonomy = %+v", ds.Taxonomy)
	}
	// Omitted report_rules must come back as [], not nil, so JSON shows [].
	if ds.Taxonomy[1].ReportRules == nil {
		t.Error("near_miss report_rules should be an empty list, not nil")
	}
	if len(ds.Deadlines) != 2 || ds.Deadlines[1].Unit != "immediately" || ds.Deadlines[1].Period != 0 {
		t.Errorf("deadlines = %+v", ds.Deadlines)
	}
}

func TestLoadFSNullListIsEmpty(t *testing.T) {
	reg, err := LoadFS(with(validFS(), "test_regime/vocabulary.json", "null"))
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	ds, _ := reg.Get("test_regime")
	if ds.Vocabulary == nil {
		t.Error("vocabulary should be an empty list, not nil")
	}
}

func TestLoadFSRejects(t *testing.T) {
	const (
		regimeFile     = "test_regime/regime.json"
		checklistsFile = "test_regime/checklists.json"
		deadlinesFile  = "test_regime/deadlines.json"
		taxonomyFile   = "test_regime/taxonomies.json"
		vocabularyFile = "test_regime/vocabulary.json"
	)

	tests := map[string]struct {
		fsys fstest.MapFS
		want string
	}{
		"no data sets": {fstest.MapFS{}, "no regime data sets found"},
		"stray top-level file": {
			with(validFS(), "README.md", "notes"),
			"README.md: unexpected file",
		},
		"missing file": {
			with(validFS(), vocabularyFile, ""),
			"test_regime/vocabulary.json",
		},
		"unknown field": {
			with(validFS(), regimeFile, strings.Replace(regimeJSON, `"version"`, `"colour": "red", "version"`, 1)),
			`unknown field "colour"`,
		},
		"trailing data": {
			with(validFS(), checklistsFile, checklistsJSON+"\n]"),
			"unexpected data after the JSON value",
		},
		"code does not match directory": {
			with(validFS(), regimeFile, strings.Replace(regimeJSON, "test_regime", "other_regime", 1)),
			`code "other_regime" does not match directory name`,
		},
		"bad date": {
			with(validFS(), regimeFile, strings.Replace(regimeJSON, "2014-02-07", "07/02/2014", 1)),
			`effective_from "07/02/2014" must be YYYY-MM-DD`,
		},
		"bad edition": {
			with(validFS(), regimeFile, strings.Replace(regimeJSON, `"construction"`, `"farming"`, 1)),
			`edition "farming" is not one of construction, mining`,
		},
		"bad cadence": {
			with(validFS(), checklistsFile, strings.Replace(checklistsJSON, `"daily"`, `"monthly"`, 1)),
			`checklists[0]: cadence "monthly" is not one of`,
		},
		"checklist without items": {
			with(validFS(), checklistsFile, `[{"key": "scaffold_daily", "version": 1, "title": "T",
				"cadence": "daily", "source_ref": "r", "items": []}]`),
			"checklists[0]: needs at least one item",
		},
		"key not snake case": {
			with(validFS(), vocabularyFile, `[{"key": "Site-Diary", "label": "Site diary"}]`),
			`key "Site-Diary" must be lower snake case`,
		},
		"duplicate key": {
			with(validFS(), vocabularyFile, `[{"key": "site_diary", "label": "A"}, {"key": "site_diary", "label": "B"}]`),
			`vocabulary[1]: duplicate key "site_diary"`,
		},
		"zero period": {
			with(validFS(), deadlinesFile, strings.Replace(deadlinesJSON, `"period": 7`, `"period": 0`, 1)),
			"deadlines[0]: period must be 1 or more",
		},
		"immediately with a period": {
			with(validFS(), deadlinesFile, strings.Replace(deadlinesJSON, `"period": 0`, `"period": 1`, 1)),
			`deadlines[1]: period must be 0 when unit is "immediately"`,
		},
		"bad direction": {
			with(validFS(), deadlinesFile, strings.Replace(deadlinesJSON, `"direction": "after"`, `"direction": "during"`, 1)),
			`deadlines[0]: direction "during" is not one of after, before`,
		},
		"missing direction": {
			with(validFS(), deadlinesFile, strings.Replace(deadlinesJSON, `"direction": "after", `, "", 1)),
			`deadlines[0]: direction "" is not one of after, before`,
		},
		"dangling report rule": {
			with(validFS(), taxonomyFile, `[{"key": "injury", "label": "Injury", "reportable": true, "report_rules": ["no_such_rule"]}]`),
			`report_rules entry "no_such_rule" is not a deadline rule`,
		},
		"duplicate report rule": {
			with(validFS(), taxonomyFile, `[{"key": "injury", "label": "Injury", "reportable": true, "report_rules": ["test_rule", "test_rule"]}]`),
			`report_rules lists "test_rule" twice`,
		},
		"reportable without report rules": {
			with(validFS(), taxonomyFile, `[{"key": "injury", "label": "Injury", "reportable": true, "report_rules": []}]`),
			"reportable types need at least one report_rules entry",
		},
		"report rules on a non-reportable type": {
			with(validFS(), taxonomyFile, `[{"key": "near_miss", "label": "Near miss", "reportable": false, "report_rules": ["test_rule"]}]`),
			"report_rules is set but reportable is false",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			reg, err := LoadFS(tc.fsys)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if reg != nil {
				t.Error("registry should be nil on error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// One run must report every problem, not stop at the first.
func TestLoadFSReportsEveryProblem(t *testing.T) {
	fsys := with(validFS(), "test_regime/regime.json", strings.Replace(regimeJSON, "2014-02-07", "soon", 1))
	fsys = with(fsys, "test_regime/vocabulary.json", `[{"key": "a", "label": ""}]`)

	_, err := LoadFS(fsys)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	for _, want := range []string{
		`test_regime/regime.json: regime: effective_from "soon"`,
		"test_regime/vocabulary.json: vocabulary[0]: label is required",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}
