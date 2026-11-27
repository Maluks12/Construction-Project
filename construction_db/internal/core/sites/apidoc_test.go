package sites

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"construction_db/internal/platform/apidoctest"
)

func TestAPIDoc(t *testing.T) {
	doc := apidoctest.Load(t, "locations")
	doc.CheckStruct(t, "Location", Location{}, false)
	doc.CheckStruct(t, "CreateLocationRequest", CreateLocationRequest{}, true)
	doc.CheckStruct(t, "UpdateLocationRequest", UpdateLocationRequest{}, true)
	doc.CheckFields(t, "LocationList", []string{"locations"})
	doc.CheckUnion(t, "LocationKind", LocationKinds)

	// The text writeError actually sends, not the sentinel's own.
	gin.SetMode(gin.TestMode)
	for _, e := range LocationErrors {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		writeError(c, e)
		var body struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%v: %v", e, err)
		}
		if quoted := `"` + body.Error + `"`; !strings.Contains(doc.Text, quoted) {
			t.Errorf("the reference doesn't quote the error %s", quoted)
		}
	}
}
