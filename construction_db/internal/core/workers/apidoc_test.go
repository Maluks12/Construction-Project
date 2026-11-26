package workers

import (
	"strings"
	"testing"

	"construction_db/internal/platform/apidoctest"
	"construction_db/internal/regimes"
)

func TestAPIDoc(t *testing.T) {
	doc := apidoctest.Load(t, "workers")
	for name, v := range map[string]any{
		"Worker": Worker{}, "WorkerDetail": WorkerDetail{}, "Competency": Competency{}, "Medical": Medical{}, "Retraction": Retraction{},
		"Link": Link{}, "RegimeSummary": RegimeSummary{}, "RegimeTypes": RegimeTypes{}, "CompetencyType": regimes.CompetencyType{},
		"AppointmentType": regimes.AppointmentType{}, "MedicalRule": regimes.MedicalRule{}, "DocumentType": regimes.DocumentType{},
		"Reminders": regimes.Reminders{}, "FieldProblem": FieldProblem{}, "IdentityClash": IdentityClash{},
	} {
		t.Run(name, func(t *testing.T) { doc.CheckStruct(t, name, v, false) })
	}
	for name, v := range map[string]any{
		"WorkerRequest": WorkerRequest{}, "CompetencyRequest": CompetencyRequest{}, "MedicalRequest": MedicalRequest{},
		"RetractRequest": RetractRequest{}, "LinkRequest": LinkRequest{}, "UnlinkRequest": UnlinkRequest{},
	} {
		t.Run(name, func(t *testing.T) { doc.CheckStruct(t, name, v, true) })
	}
	doc.CheckFields(t, "WorkerList", []string{"workers"})
	doc.CheckFields(t, "WorkerChanged", []string{"error", "current"})
	doc.CheckFields(t, "RegimeList", []string{"regimes"})
	doc.CheckUnion(t, "RecordState", States)
	doc.CheckUnion(t, "MedicalOutcome", Outcomes)
	doc.CheckUnion(t, "SignInWarning", Warnings)

	var texts []string
	for _, e := range Sentinels {
		texts = append(texts, e.Error())
	}
	texts = append(texts, Messages...)
	for _, s := range texts {
		if quoted := `"` + s + `"`; !strings.Contains(doc.Text, quoted) {
			t.Errorf("the reference doesn't quote the error %s", quoted)
		}
	}
}
