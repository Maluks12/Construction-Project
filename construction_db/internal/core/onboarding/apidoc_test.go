package onboarding

import (
	"strings"
	"testing"

	"construction_db/internal/platform/apidoctest"
)

func TestAPIDoc(t *testing.T) {
	doc := apidoctest.Load(t, "onboarding")
	for name, v := range map[string]any{
		"ItemResult": ItemResult{}, "ItemProblem": ItemProblem{}, "DocumentsResult": DocumentsResult{},
		"Gap": Gap{}, "Step": Step{}, "Progress": Progress{},
	} {
		t.Run(name, func(t *testing.T) { doc.CheckStruct(t, name, v, false) })
	}
	doc.CheckStruct(t, "DocumentItem", DocumentItem{}, true)
	doc.CheckFields(t, "DocumentsRequest", []string{"items"})
	doc.CheckUnion(t, "DocumentKind", Kinds)
	doc.CheckUnion(t, "StepKey", StepKeys)
	for _, e := range Sentinels {
		if quoted := `"` + e.Error() + `"`; !strings.Contains(doc.Text, quoted) {
			t.Errorf("the reference doesn't quote the error %s", quoted)
		}
	}
}
