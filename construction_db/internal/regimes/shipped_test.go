package regimes

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"testing"
)

// shippedKeys is one regime's entry in testdata/shipped.json: the keys that
// have shipped and so may already be stored in a customer's records. Item
// keys are written "<checklist_key>/<item_key>" because item keys are only
// unique inside their checklist.
//
// Keys are locked here. Checklist content is locked by ChecklistVersions
// (versions_test.go): every shipped version's hash, by key then version. A
// wording fix is a new version; a change in meaning needs a new key. New keys
// and versions are added to the lock by hand when they ship.
type shippedKeys struct {
	Checklists        []string                     `json:"checklists"`
	ChecklistItems    []string                     `json:"checklist_items"`
	Deadlines         []string                     `json:"deadlines"`
	Taxonomy          []string                     `json:"taxonomy"`
	ChecklistVersions map[string]map[string]string `json:"checklist_versions"`
}

// missingKeys returns one entry per locked key that reg no longer has, sorted
// by regime code.
func missingKeys(reg *Registry, lock map[string]shippedKeys) []string {
	var missing []string
	for _, code := range slices.Sorted(maps.Keys(lock)) {
		ds, ok := reg.Get(code)
		if !ok {
			missing = append(missing, fmt.Sprintf("regime %q", code))
			continue
		}

		checklists, items := map[string]bool{}, map[string]bool{}
		for _, c := range ds.Checklists {
			checklists[c.Key] = true
			for _, it := range c.Items {
				items[c.Key+"/"+it.Key] = true
			}
		}
		deadlines := map[string]bool{}
		for _, d := range ds.Deadlines {
			deadlines[d.Key] = true
		}
		taxonomy := map[string]bool{}
		for _, t := range ds.Taxonomy {
			taxonomy[t.Key] = true
		}

		report := func(kind string, want []string, have map[string]bool) {
			for _, k := range want {
				if !have[k] {
					missing = append(missing, fmt.Sprintf("%s %s %q", code, kind, k))
				}
			}
		}
		want := lock[code]
		report("checklist", want.Checklists, checklists)
		report("checklist item", want.ChecklistItems, items)
		report("deadline", want.Deadlines, deadlines)
		report("taxonomy", want.Taxonomy, taxonomy)
	}
	return missing
}

func TestShippedKeysStillExist(t *testing.T) {
	reg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var lock map[string]shippedKeys
	if err := decodeFile(os.DirFS("testdata"), "shipped.json", &lock); err != nil {
		t.Fatalf("read lock: %v", err)
	}

	for _, m := range missingKeys(reg, lock) {
		t.Errorf("shipped key removed or renamed: %s. Shipped keys never change meaning: "+
			"put it back, and add a new key for the new meaning.", m)
	}
}

func TestMissingKeysDetected(t *testing.T) {
	reg, err := LoadFS(validFS())
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	lock := map[string]shippedKeys{
		"test_regime": {
			Checklists:     []string{"scaffold_daily", "gone_checklist"},
			ChecklistItems: []string{"scaffold_daily/base_plates", "scaffold_daily/gone_item"},
			Deadlines:      []string{"test_rule", "gone_rule"},
			Taxonomy:       []string{"injury", "gone_type"},
		},
		"gone_regime": {},
	}

	got := missingKeys(reg, lock)
	want := []string{
		`regime "gone_regime"`,
		`test_regime checklist "gone_checklist"`,
		`test_regime checklist item "scaffold_daily/gone_item"`,
		`test_regime deadline "gone_rule"`,
		`test_regime taxonomy "gone_type"`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("missingKeys =\n%q\nwant\n%q", got, want)
	}
}
