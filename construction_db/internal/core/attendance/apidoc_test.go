package attendance

import (
	"strings"
	"testing"

	"construction_db/internal/platform/apidoctest"
)

func TestAPIDoc(t *testing.T) {
	doc := apidoctest.Load(t, "attendance")
	for name, v := range map[string]any{
		"SignIn": SignIn{}, "SignOut": SignOut{}, "Retraction": Retraction{}, "Sheet": Sheet{}, "Overlap": Overlap{}, "Day": Day{},
		"RosterEntry": RosterEntry{}, "AlreadySignedIn": AlreadySignedIn{},
	} {
		t.Run(name, func(t *testing.T) { doc.CheckStruct(t, name, v, false) })
	}
	for name, v := range map[string]any{
		"SignInRequest": SignInRequest{}, "SignOutRequest": SignOutRequest{}, "RetractRequest": RetractRequest{}, "RemoveRequest": RemoveRequest{},
	} {
		t.Run(name, func(t *testing.T) { doc.CheckStruct(t, name, v, true) })
	}
	doc.CheckFields(t, "RosterList", []string{"roster"})
	doc.CheckUnion(t, "SignInMethod", Methods)
	for _, e := range Sentinels {
		if quoted := `"` + e.Error() + `"`; !strings.Contains(doc.Text, quoted) {
			t.Errorf("the reference doesn't quote the error %s", quoted)
		}
	}
	// The 16-hour rule the reference states is the one in force.
	if OpenWindow.Hours() != 16 || !strings.Contains(doc.Text, "16 hours") {
		t.Errorf("OpenWindow %s; the reference must state it", OpenWindow)
	}
}
