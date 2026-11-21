package reg4

import (
	"strings"
	"testing"

	"construction_db/internal/platform/apidoctest"
)

func TestAPIDoc(t *testing.T) {
	doc := apidoctest.Load(t, "site-work")
	for name, v := range map[string]any{
		"SiteWorkFact": Fact{}, "SiteWorkVersion": Version{}, "SiteWorkChanges": Changes{}, "SiteWork": SiteWork{},
		"SiteWorkSummary": Summary{}, "SiteWorkRule": Rule{}, "Catalogue": Catalogue{},
	} {
		t.Run(name, func(t *testing.T) { doc.CheckStruct(t, name, v, false) })
	}
	doc.CheckStruct(t, "VersionRequest", VersionRequest{}, true)
	doc.CheckFields(t, "SiteWorkList", []string{"site_work"})
	doc.CheckFields(t, "UnknownFacts", []string{"error", "facts"})
	doc.CheckFields(t, "NotNextVersion", []string{"error", "latest_version"})
	doc.CheckUnion(t, "SiteWorkStatus", Statuses)
	for _, e := range Sentinels {
		if quoted := `"` + e.Error() + `"`; !strings.Contains(doc.Text, quoted) {
			t.Errorf("the reference doesn't quote the error %s", quoted)
		}
	}
}
