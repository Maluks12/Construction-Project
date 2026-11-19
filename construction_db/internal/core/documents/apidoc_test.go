package documents

import (
	"strings"
	"testing"

	"construction_db/internal/platform/apidoctest"
)

func TestAPIDoc(t *testing.T) {
	doc := apidoctest.Load(t, "documents")
	for name, v := range map[string]any{
		"DocumentSummary": DocumentSummary{}, "Document": Document{}, "Version": Version{}, "Readiness": Readiness{}, "Register": Register{},
		"Retraction": Retraction{},
	} {
		t.Run(name, func(t *testing.T) { doc.CheckStruct(t, name, v, false) })
	}
	doc.CheckStruct(t, "CreateRequest", CreateRequest{}, true)
	doc.CheckStruct(t, "VersionRequest", VersionRequest{}, true)
	doc.CheckStruct(t, "RetractRequest", RetractRequest{}, true)
	doc.CheckUnion(t, "DocumentScope", []string{ScopeOrg, ScopeSite})
	doc.CheckUnion(t, "VersionState", VersionStates)
	doc.CheckUnion(t, "ReadinessStatus", ReadinessStates)
	if !strings.Contains(doc.Text, `"`+StateSuperseded+`"`) || !strings.Contains(doc.Text, `"`+StateRetracted+`"`) {
		t.Errorf("the reference doesn't name the %q state", StateSuperseded)
	}
	for _, e := range Sentinels {
		if quoted := `"` + e.Error() + `"`; !strings.Contains(doc.Text, quoted) {
			t.Errorf("the reference doesn't quote the error %s", quoted)
		}
	}
}
