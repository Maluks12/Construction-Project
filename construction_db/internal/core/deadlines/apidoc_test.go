package deadlines

import (
	"strings"
	"testing"

	"construction_db/internal/platform/apidoctest"
)

func TestAPIDoc(t *testing.T) {
	doc := apidoctest.Load(t, "deadlines")

	for name, v := range map[string]any{
		"Deadline": Deadline{}, "Satisfaction": Satisfaction{}, "Retraction": Retraction{}, "DeadlineDetail": DeadlineDetail{},
	} {
		t.Run(name, func(t *testing.T) { doc.CheckStruct(t, name, v, false) })
	}
	for name, v := range map[string]any{"SatisfactionRequest": SatisfactionRequest{}, "RetractRequest": RetractRequest{}} {
		t.Run(name, func(t *testing.T) { doc.CheckStruct(t, name, v, true) })
	}
	doc.CheckFields(t, "DeadlineList", []string{"deadlines"})
	doc.CheckFields(t, "EvidenceProblem", []string{"error", "evidence_ids"})
	doc.CheckUnion(t, "DeadlineState", States)
	doc.CheckUnion(t, "DeadlineSubjectType", SubjectKinds)
	doc.CheckUnion(t, "SatisfactionMethod", Methods)

	for _, e := range sentinels {
		if quoted := `"` + e.Error() + `"`; !strings.Contains(doc.Text, quoted) {
			t.Errorf("the reference doesn't quote the error %s", quoted)
		}
	}
	for _, s := range States {
		if row := "| `" + s + "` |"; !strings.Contains(doc.Text, row) {
			t.Errorf("the reference has no row for state %s", s)
		}
	}
}
