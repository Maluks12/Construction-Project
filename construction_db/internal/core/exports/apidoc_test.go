package exports

import (
	"strings"
	"testing"

	"construction_db/internal/platform/apidoctest"
)

func TestAPIDoc(t *testing.T) {
	doc := apidoctest.Load(t, "exports")
	doc.CheckStruct(t, "ExportRequest", Request{}, true)
	doc.CheckStruct(t, "Export", Export{}, false)
	doc.CheckStruct(t, "Download", Download{}, false)
	doc.CheckFields(t, "ExportList", []string{"exports"})
	doc.CheckUnion(t, "ExportStatus", Statuses)
	for _, e := range append(Sentinels, errBody) {
		if quoted := `"` + e.Error() + `"`; !strings.Contains(doc.Text, quoted) {
			t.Errorf("the reference doesn't quote the error %s", quoted)
		}
	}
	if !strings.Contains(doc.Text, `"forbidden"`) {
		t.Error(`the reference doesn't quote "forbidden"`)
	}
}
