package files

import (
	"strings"
	"testing"

	"construction_db/internal/core/evidence"
	"construction_db/internal/platform/apidoctest"
)

func TestAPIDoc(t *testing.T) {
	doc := apidoctest.Load(t, "files")
	for name, v := range map[string]any{"File": File{}, "Upload": evidence.Upload{}, "ViewURL": evidence.ViewURL{}} {
		t.Run(name, func(t *testing.T) { doc.CheckStruct(t, name, v, false) })
	}
	doc.CheckStruct(t, "DeclareRequest", DeclareRequest{}, true)
	doc.CheckFields(t, "Rejected", []string{"error", "rejected_reason"})
	doc.CheckUnion(t, "FilePurpose", Purposes)
	doc.CheckUnion(t, "FileContentType", ContentTypes)
	doc.CheckUnion(t, "FileRejectedReason", RejectedReasons)
	for _, e := range append(Sentinels, error(&Rejected{})) {
		if quoted := `"` + e.Error() + `"`; !strings.Contains(doc.Text, quoted) {
			t.Errorf("the reference doesn't quote the error %s", quoted)
		}
	}
}
