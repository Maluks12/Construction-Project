package members

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"construction_db/internal/platform/apidoctest"
)

func TestAPIDoc(t *testing.T) {
	doc := apidoctest.Load(t, "members")
	for name, v := range map[string]any{
		"Member": Member{}, "OpenAction": OpenAction{}, "OpenWork": OpenWork{}, "MemberDetail": MemberDetail{},
		"MemberChange": Change{}, "HistoryEntry": HistoryEntry{}, "SiteHistoryEntry": SiteHistoryEntry{},
		"MemberHistory": History{},
	} {
		t.Run(name, func(t *testing.T) { doc.CheckStruct(t, name, v, false) })
	}
	doc.CheckStruct(t, "UpdateRequest", UpdateRequest{}, true)
	doc.CheckFields(t, "MemberList", []string{"members"})
	doc.CheckUnion(t, "MemberStatus", Statuses)
	doc.CheckUnion(t, "PatchStatus", PatchStatuses)
	doc.CheckUnion(t, "SiteChange", SiteChanges)

	// The text writeError sends, for every sentinel.
	gin.SetMode(gin.TestMode)
	for _, e := range Sentinels {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		writeError(c, e)
		var body struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error != e.Error() {
			t.Fatalf("%v: sent %s (%v)", e, rec.Body.String(), err)
		}
		if quoted := `"` + body.Error + `"`; !strings.Contains(doc.Text, quoted) {
			t.Errorf("the reference doesn't quote the error %s", quoted)
		}
	}
}
