package reports

import (
	"strings"
	"testing"

	"construction_db/internal/platform/apidoctest"
)

func TestAPIDoc(t *testing.T) {
	doc := apidoctest.Load(t, "compliance")
	doc.CheckStruct(t, "ComplianceItem", Item{}, false)
	doc.CheckStruct(t, "ComplianceCategory", Category{}, false)
	doc.CheckStruct(t, "SiteSummary", SiteSummary{}, false)
	doc.CheckStruct(t, "SiteRank", SiteRank{}, false)
	doc.CheckStruct(t, "Rollup", Rollup{}, false)
	doc.CheckUnion(t, "Colour", []string{Red, Amber, Green})
	doc.CheckUnion(t, "CategoryKey", CategoryKeys)
	doc.CheckUnion(t, "ComplianceSubject", SubjectTypes)
	doc.CheckUnion(t, "ItemState", ItemStates)
	for _, e := range []string{ErrSiteNotFound.Error(), "forbidden"} {
		if quoted := `"` + e + `"`; !strings.Contains(doc.Text, quoted) {
			t.Errorf("the reference doesn't quote the error %s", quoted)
		}
	}
	// Every category key has a row in the rule table.
	for _, k := range CategoryKeys {
		if !strings.Contains(doc.Text, "| `"+k+"` |") {
			t.Errorf("the rule table has no row for %q", k)
		}
	}
}
