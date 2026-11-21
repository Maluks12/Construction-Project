package incidents

import (
	"fmt"
	"strings"
	"testing"

	"construction_db/internal/platform/apidoctest"
	"construction_db/internal/regimes"
)

func TestAPIDoc(t *testing.T) {
	doc := apidoctest.Load(t, "incidents")

	for name, v := range map[string]any{
		"Incident": Incident{}, "Version": Version{}, "IncidentType": IncidentType{}, "Person": Person{},
		"AttachedEvidence": AttachedEvidence{}, "DeadlineChanges": DeadlineChanges{}, "WitnessStatement": WitnessStatement{},
		"IncidentSummary": IncidentSummary{},
	} {
		t.Run(name, func(t *testing.T) { doc.CheckStruct(t, name, v, false) })
	}
	for name, v := range map[string]any{
		"SubmitVersionRequest": SubmitVersionRequest{}, "PersonInput": PersonInput{}, "StatementRequest": StatementRequest{},
	} {
		t.Run(name, func(t *testing.T) { doc.CheckStruct(t, name, v, true) })
	}
	// Built inline by the handler.
	doc.CheckFields(t, "IncidentList", []string{"incidents"})
	doc.CheckFields(t, "EvidenceProblem", []string{"error", "evidence_ids"})
	doc.CheckFields(t, "UnknownTypes", []string{"error", "taxonomy_codes"})
	doc.CheckFields(t, "NotNextVersion", []string{"error", "latest_version"})

	doc.CheckUnion(t, "Treatment", Treatments)

	// Every error text the handlers send is quoted in the reference.
	for _, e := range sentinels {
		if quoted := `"` + e.Error() + `"`; !strings.Contains(doc.Text, quoted) {
			t.Errorf("the reference doesn't quote the error %s", quoted)
		}
	}
}

// The reference lists the construction regime's incident types for the
// frontend; it must match the regime data.
func TestIncidentTypesTable(t *testing.T) {
	doc := apidoctest.Load(t, "incidents")
	reg, err := regimes.Load()
	if err != nil {
		t.Fatal(err)
	}
	ds, _ := reg.Get("za_construction_2014")
	_, table, _ := strings.Cut(doc.Text, "## Incident types")
	table, _, _ = strings.Cut(table, "## Across the API")
	rows := 0
	for _, line := range strings.Split(table, "\n") {
		if strings.HasPrefix(line, "| `") {
			rows++
		}
	}
	if rows != len(ds.Taxonomy) {
		t.Errorf("the table has %d types, the regime %d", rows, len(ds.Taxonomy))
	}
	for _, ty := range ds.Taxonomy {
		yes := "no"
		if ty.Reportable {
			yes = "yes"
		}
		if row := fmt.Sprintf("| `%s` | %s | %s", ty.Key, ty.Label, yes); !strings.Contains(table, row) {
			t.Errorf("no row starting %q", row)
		}
	}
}
