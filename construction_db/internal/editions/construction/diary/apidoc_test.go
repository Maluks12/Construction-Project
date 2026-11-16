package diary

import (
	"reflect"
	"strings"
	"testing"

	"construction_db/internal/platform/apidoctest"
)

func TestAPIDoc(t *testing.T) {
	doc := apidoctest.Load(t, "diary")

	// Responses. Sections and its rows are sent back as stored, so they are
	// checked as responses; a request may leave a null field out.
	for name, v := range map[string]any{
		"Weather": Weather{}, "Sections": Sections{}, "WeatherTimes": WeatherTimes{}, "LabourRow": LabourRow{}, "PlantRow": PlantRow{},
		"Instruction": Instruction{}, "Delay": Delay{}, "Visitor": Visitor{}, "Delivery": Delivery{},
		"Revision": Revision{}, "Addendum": Addendum{}, "DiarySummary": DiarySummary{}, "Diary": Diary{},
	} {
		t.Run(name, func(t *testing.T) { doc.CheckStruct(t, name, v, false) })
	}
	// The requests by field name: weather and headcount are pointers so a
	// missing one is told from a zero, yet required (the handler says so).
	doc.CheckFields(t, "RevisionRequest", jsonNames(RevisionRequest{}))
	doc.CheckFields(t, "AddendumRequest", jsonNames(AddendumRequest{}))
	// Built inline by the handler.
	doc.CheckFields(t, "DiaryList", []string{"diaries"})
	doc.CheckFields(t, "RevisionList", []string{"revisions"})
	doc.CheckFields(t, "FirmList", []string{"firms"})
	doc.CheckFields(t, "EvidenceProblem", []string{"error", "evidence_ids"})
	doc.CheckFields(t, "SectionProblem", []string{"error", "section", "row", "field"})

	doc.CheckUnion(t, "WeatherCondition", Conditions)
	doc.CheckUnion(t, "PlantState", PlantStates)
	doc.CheckUnion(t, "InstructionForm", InstructionForms)
	doc.CheckUnion(t, "DeliveryStatus", DeliveryStatuses)

	// Every error text the handlers send is quoted in the reference.
	for _, e := range sentinels {
		if quoted := `"` + e.Error() + `"`; !strings.Contains(strings.ReplaceAll(doc.Text, "\n", " "), quoted) {
			t.Errorf("the reference doesn't quote the error %s", quoted)
		}
	}
}

// jsonNames lists the JSON field names of a flat struct.
func jsonNames(v any) []string {
	var out []string
	typ := reflect.TypeOf(v)
	for i := range typ.NumField() {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		out = append(out, name)
	}
	return out
}
