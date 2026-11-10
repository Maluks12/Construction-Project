package regimes

import (
	"fmt"
	"strings"
	"testing"
)

// entry is one checklist template version as JSON, with a single item.
func entry(key string, version int, prompt string) string {
	return fmt.Sprintf(`{"key": %q, "version": %d, "title": "T", "cadence": "daily", "source_ref": "r",
		"items": [{"key": "item", "prompt": %q, "response_type": "pass_fail_na"}]}`, key, version, prompt)
}

func entries(e ...string) string { return "[" + strings.Join(e, ",\n") + "]" }

func TestEveryVersionIsKept(t *testing.T) {
	fsys := with(validFS(), "test_regime/checklists.json", entries(
		entry("scaffold_daily", 1, "Base plates sound?"),
		entry("scaffold_daily", 2, "Base plates firm and level?"),
		entry("ladder_check", 1, "Ladder tied?"),
		entry("scaffold_daily_x", 3, "Only v3 shipped?"),
	))
	reg, err := LoadFS(fsys)
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	ds, _ := reg.Get("test_regime")

	t.Run("latest version of each key, in first-appearance order", func(t *testing.T) {
		var got []string
		for _, tmpl := range ds.Latest() {
			got = append(got, fmt.Sprintf("%s@%d", tmpl.Key, tmpl.Version))
		}
		if want := "scaffold_daily@2 ladder_check@1 scaffold_daily_x@3"; strings.Join(got, " ") != want {
			t.Errorf("Latest = %v, want %s", got, want)
		}
	})

	t.Run("an exact version, old ones included", func(t *testing.T) {
		for _, tc := range []struct {
			key     string
			version int
			prompt  string
		}{
			{"scaffold_daily", 1, "Base plates sound?"},
			{"scaffold_daily", 2, "Base plates firm and level?"},
			{"scaffold_daily_x", 3, "Only v3 shipped?"},
		} {
			tmpl, ok := ds.Checklist(tc.key, tc.version)
			if !ok || tmpl.Items[0].Prompt != tc.prompt {
				t.Errorf("Checklist(%s, %d) = %+v, %v; want prompt %q", tc.key, tc.version, tmpl, ok, tc.prompt)
			}
		}
		for _, tc := range []struct {
			key     string
			version int
		}{{"scaffold_daily", 3}, {"scaffold_daily_x", 1}, {"unknown", 1}} {
			if _, ok := ds.Checklist(tc.key, tc.version); ok {
				t.Errorf("Checklist(%s, %d) found, want not found", tc.key, tc.version)
			}
		}
	})

	t.Run("an empty data set has no latest", func(t *testing.T) {
		if got := (&DataSet{}).Latest(); got == nil || len(got) != 0 {
			t.Errorf("Latest of nothing = %#v, want []", got)
		}
	})
}

// ContentHash covers what an inspector reads, and only that.
func TestContentHash(t *testing.T) {
	base := ChecklistTemplate{Key: "k", Version: 1, Title: "T", Cadence: "daily", SourceRef: "r",
		Items: []ChecklistItem{{Key: "i", Prompt: "P?", ResponseType: "pass_fail_na", SourceRef: "r(a)"}}}
	h := base.ContentHash()
	if len(h) != 64 || strings.ToLower(h) != h {
		t.Fatalf("hash %q, want 64 lowercase hex characters", h)
	}
	if base.ContentHash() != h {
		t.Error("not deterministic")
	}

	changes := map[string]func(*ChecklistTemplate){
		"title":         func(c *ChecklistTemplate) { c.Title = "T2" },
		"cadence":       func(c *ChecklistTemplate) { c.Cadence = "weekly" },
		"source ref":    func(c *ChecklistTemplate) { c.SourceRef = "r2" },
		"prompt":        func(c *ChecklistTemplate) { c.Items[0].Prompt = "P2?" },
		"item key":      func(c *ChecklistTemplate) { c.Items[0].Key = "j" },
		"item source":   func(c *ChecklistTemplate) { c.Items[0].SourceRef = "r(b)" },
		"response type": func(c *ChecklistTemplate) { c.Items[0].ResponseType = "text" },
		"item added": func(c *ChecklistTemplate) {
			c.Items = append(c.Items, ChecklistItem{Key: "j", Prompt: "Q?", ResponseType: "pass_fail_na"})
		},
	}
	for name, change := range changes {
		c := base
		c.Items = append([]ChecklistItem(nil), base.Items...)
		change(&c)
		if c.ContentHash() == h {
			t.Errorf("changing the %s didn't change the hash", name)
		}
	}

	bumped := base
	bumped.Version = 9
	if bumped.ContentHash() != h {
		t.Error("the version changed the hash; it should cover content only")
	}
}
