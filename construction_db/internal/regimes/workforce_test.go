package regimes

import (
	"os"
	"strings"
	"testing"
)

// sourceRows maps each "| `key` | source_ref | quote |" row in SOURCES to its
// source_ref cell.
func sourceRows(t *testing.T) map[string]string {
	t.Helper()
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
	return rows
}

// Every competency, appointment and document type is quoted in SOURCES, on
// a row whose source_ref is exactly the data's.
func TestWorkforceTypesQuotedInSources(t *testing.T) {
	reg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ds, _ := reg.Get("za_construction_2014")
	if len(ds.Workforce.CompetencyTypes) == 0 || len(ds.Workforce.AppointmentTypes) == 0 || len(ds.DocumentTypes) == 0 {
		t.Fatal("a type list is empty")
	}
	rows := sourceRows(t)
	check := func(kind, key, ref string) {
		got, ok := rows["`"+key+"`"]
		switch {
		case !ok:
			t.Errorf("%s %q has no row in SOURCES", kind, key)
		case got != ref:
			t.Errorf("%s %q: SOURCES says %q, data says %q", kind, key, got, ref)
		}
	}
	for _, c := range ds.Workforce.CompetencyTypes {
		check("competency type", c.Key, c.SourceRef)
	}
	for _, a := range ds.Workforce.AppointmentTypes {
		check("appointment type", a.Key, a.SourceRef)
	}
	for _, d := range ds.DocumentTypes {
		check("document type", d.Key, d.SourceRef)
	}

	if _, ok := ds.CompetencyType("rope_access_licence"); !ok {
		t.Error(`CompetencyType("rope_access_licence") not found`)
	}
	if _, ok := ds.AppointmentType("construction_manager"); !ok {
		t.Error(`AppointmentType("construction_manager") not found`)
	}
	if _, ok := ds.DocumentType(ScopeOrg, "health_and_safety_plan"); ok {
		t.Error("a site document type was found in the org scope")
	}
	if _, ok := ds.DocumentType(ScopeSite, "health_and_safety_plan"); !ok {
		t.Error(`DocumentType(site, "health_and_safety_plan") not found`)
	}
}
