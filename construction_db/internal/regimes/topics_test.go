package regimes

import (
	"os"
	"strings"
	"testing"
)

// Every shipped toolbox topic is quoted in SOURCES (D3): its key and its
// regulation appear on one row of the topics table.
func TestToolboxTopicsQuotedInSources(t *testing.T) {
	reg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ds, _ := reg.Get("za_construction_2014")
	if len(ds.ToolboxTopics) == 0 {
		t.Fatal("no toolbox topics")
	}
	src, err := os.ReadFile("SOURCES_za_construction_2014.md")
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]string{}
	for _, line := range strings.Split(string(src), "\n") {
		if cells := strings.Split(line, "|"); len(cells) > 3 {
			rows[strings.TrimSpace(cells[1])] = strings.TrimSpace(cells[2])
		}
	}
	for _, tp := range ds.ToolboxTopics {
		ref, ok := rows["`"+tp.Key+"`"]
		if !ok {
			t.Errorf("topic %q has no row in SOURCES", tp.Key)
			continue
		}
		if want := strings.TrimPrefix(tp.SourceRef, "CR 2014 "); ref != want {
			t.Errorf("topic %q: SOURCES says %q, data says %q", tp.Key, ref, tp.SourceRef)
		}
	}
	if _, ok := ds.Topic("scaffolding"); !ok {
		t.Error(`Topic("scaffolding") not found`)
	}
	if _, ok := ds.Topic("nope"); ok {
		t.Error(`Topic("nope") found`)
	}
}
