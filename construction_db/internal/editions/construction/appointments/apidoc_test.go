package appointments

import (
	"strings"
	"testing"

	"construction_db/internal/platform/apidoctest"
)

func TestAPIDoc(t *testing.T) {
	doc := apidoctest.Load(t, "appointments")
	doc.CheckStruct(t, "Appointment", Appointment{}, false)
	doc.CheckStruct(t, "AppointmentRequest", Request{}, true)
	doc.CheckFields(t, "AppointmentList", []string{"appointments"})
	for _, e := range Sentinels {
		if quoted := `"` + e.Error() + `"`; !strings.Contains(doc.Text, quoted) {
			t.Errorf("the reference doesn't quote the error %s", quoted)
		}
	}
}
