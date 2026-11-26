package toolbox

import (
	"strings"
	"testing"

	"construction_db/internal/platform/apidoctest"
	"construction_db/internal/regimes"
)

func TestAPIDoc(t *testing.T) {
	doc := apidoctest.Load(t, "toolbox")

	for name, v := range map[string]any{
		"ToolboxTopic": regimes.ToolboxTopic{}, "TalkSummary": TalkSummary{}, "Talk": Talk{}, "TalkPhoto": TalkPhoto{},
		"Attendance": Attendance{}, "Retraction": Retraction{}, "NotSigned": NotSigned{},
	} {
		t.Run(name, func(t *testing.T) { doc.CheckStruct(t, name, v, false) })
	}
	for name, v := range map[string]any{
		"TalkRequest": TalkRequest{}, "PhotoRequest": PhotoRequest{}, "SignRequest": SignRequest{}, "RetractRequest": RetractRequest{},
	} {
		t.Run(name, func(t *testing.T) { doc.CheckStruct(t, name, v, true) })
	}
	// Built inline by the handler.
	doc.CheckFields(t, "ToolboxTopicList", []string{"topics"})
	doc.CheckFields(t, "TalkList", []string{"talks"})
	doc.CheckFields(t, "AlreadySigned", []string{"error", "hint"})
	doc.CheckFields(t, "TalkChanged", []string{"error", "current"})
	if quoted := `"` + AlreadySignedHint + `"`; !strings.Contains(doc.Text, quoted) {
		t.Errorf("the reference doesn't quote the hint %s", quoted)
	}

	doc.CheckUnion(t, "AttendanceMethod", Methods)
	doc.CheckUnion(t, "TalkPhotoKind", Kinds)

	// Every error text the handlers send is quoted in the reference.
	for _, e := range sentinels {
		if quoted := `"` + e.Error() + `"`; !strings.Contains(doc.Text, quoted) {
			t.Errorf("the reference doesn't quote the error %s", quoted)
		}
	}
}
