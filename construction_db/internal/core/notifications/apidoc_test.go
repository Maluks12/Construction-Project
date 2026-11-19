package notifications

import (
	"strings"
	"testing"

	"construction_db/internal/platform/apidoctest"
)

func TestAPIDoc(t *testing.T) {
	doc := apidoctest.Load(t, "notifications")
	doc.CheckStruct(t, "Notification", Notification{}, false)
	doc.CheckFields(t, "NotificationList", []string{"notifications"})
	doc.CheckUnion(t, "NotificationKind", Kinds)
	doc.CheckUnion(t, "NotificationSubject", SubjectTypes)
	doc.CheckUnion(t, "SubjectState", SubjectStates)
	for _, e := range []error{ErrNotFound, ErrBadLimit, ErrBadUnread} {
		if quoted := `"` + e.Error() + `"`; !strings.Contains(doc.Text, quoted) {
			t.Errorf("the reference doesn't quote the error %s", quoted)
		}
	}
}
