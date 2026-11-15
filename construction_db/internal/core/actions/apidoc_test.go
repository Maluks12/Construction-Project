package actions

import (
	"fmt"
	"strings"
	"testing"

	"construction_db/internal/core/checklists"
	"construction_db/internal/platform/apidoctest"
)

func TestAPIDoc(t *testing.T) {
	doc := apidoctest.Load(t, "actions")

	for name, v := range map[string]any{
		"ActionSummary": ActionHeader{}, "Action": Action{}, "ActionEvent": Event{}, "Closeout": Closeout{},
		"CloseoutEvidence": CloseoutEvidence{}, "Waiver": Waiver{},
	} {
		t.Run(name, func(t *testing.T) { doc.CheckStruct(t, name, v, false) })
	}
	for name, v := range map[string]any{
		"RaiseRequest": RaiseRequest{}, "FindingRef": FindingRef{}, "IncidentRef": IncidentRef{}, "EventRequest": EventRequest{},
		"CloseoutRequest": CloseoutRequest{}, "WaiverRequest": WaiverRequest{}, "RetractWaiverRequest": RetractWaiverRequest{},
	} {
		t.Run(name, func(t *testing.T) { doc.CheckStruct(t, name, v, true) })
	}
	// Built inline by the handler.
	doc.CheckFields(t, "ActionList", []string{"actions"})
	doc.CheckFields(t, "EvidenceProblem", []string{"error", "evidence_ids"})
	doc.CheckFields(t, "NotLatestVersion", []string{"error", "latest_version"})

	doc.CheckUnion(t, "ActionState", States)
	doc.CheckUnion(t, "ActionStatus", []string{StatusOpen, StatusAwaiting, StatusClosed, StatusCancelled})
	doc.CheckUnion(t, "ActionSource", Sources)
	doc.CheckUnion(t, "ActionEventKind", EventKinds)
	doc.CheckUnion(t, "ActionRequestKind", Requests)
	doc.CheckUnion(t, "ActionReview", Reviews)

	// Every error text the handlers send is quoted in the reference.
	for _, e := range sentinels {
		if quoted := `"` + e.Error() + `"`; !strings.Contains(doc.Text, quoted) {
			t.Errorf("the reference doesn't quote the error %s", quoted)
		}
	}
	// Every state and review flag is explained.
	for _, s := range append(append([]string{}, States...), Reviews...) {
		if row := "| `" + s + "` |"; !strings.Contains(doc.Text, row) {
			t.Errorf("the reference has no row for %s", s)
		}
	}
	// The default due table matches DefaultDueDays, for every severity.
	for _, l := range checklists.Severities {
		days, ok := DefaultDueDays[l.Key]
		if !ok {
			t.Errorf("DefaultDueDays has no %s", l.Key)
		}
		if row := fmt.Sprintf("| `%s` | %d |", l.Key, days); !strings.Contains(doc.Text, row) {
			t.Errorf("the reference has no row %q", row)
		}
	}
}
