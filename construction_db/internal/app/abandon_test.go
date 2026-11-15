package app

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/checklists"
)

func abandonPath(site, inspection string) string {
	return inspectionPath(site, inspection) + "/abandon"
}

func reason(r string) map[string]any { return map[string]any{"reason": r} }

func TestAbandonInspection(t *testing.T) {
	a := newAnswerFixture(t, "abandon")
	inviteMember(t, a.router, a.owner, "she@"+a.ownerMe.OrgID+".abandon.test", auth.RoleSHEOfficer)
	she := login(t, a.router, "she@"+a.ownerMe.OrgID+".abandon.test")
	supervisorID := me(t, a.router, a.supervisor).UserID
	key := a.tmpl.Items[0].Key

	var abandoned string
	t.Run("the inspector who started it can abandon it", func(t *testing.T) {
		wantStatus(t, do(t, a.router, http.MethodPut, itemPath(a.siteA, a.inspection, key), a.supervisor, answerBody("pass", "", t1)), http.StatusOK)
		rec := do(t, a.router, http.MethodPost, abandonPath(a.siteA, a.inspection), a.supervisor, reason("  Started the wrong checklist  "))
		wantStatus(t, rec, http.StatusCreated)
		abandoned = rec.Body.String()
		in := decodeInspection(t, rec)
		if in.Status != checklists.StatusAbandoned || in.AbandonedAt == nil || in.AbandonedBy == nil || *in.AbandonedBy != supervisorID ||
			in.AbandonReason == nil || *in.AbandonReason != "Started the wrong checklist" {
			t.Errorf("inspection %+v, want abandoned by the supervisor with the trimmed reason", in.InspectionHeader)
		}
		if it := in.Items[0]; it.Result == nil || *it.Result != "pass" {
			t.Errorf("first item %+v, want its answer kept", it)
		}
		if len(in.Findings) != 0 || in.CompletedAt != nil {
			t.Errorf("findings %v, completed_at %v; want none", in.Findings, in.CompletedAt)
		}
	})

	t.Run("abandoning again is 200 with the same inspection", func(t *testing.T) {
		rec := do(t, a.router, http.MethodPost, abandonPath(a.siteA, a.inspection), a.supervisor, reason("Another reason"))
		wantStatus(t, rec, http.StatusOK)
		if rec.Body.String() != abandoned {
			t.Errorf("repeat = %s\nfirst = %s", rec.Body.String(), abandoned)
		}
	})

	t.Run("an abandoned inspection can't be answered or completed", func(t *testing.T) {
		wantError(t, do(t, a.router, http.MethodPut, itemPath(a.siteA, a.inspection, key), a.supervisor, answerBody("na", "", t2)),
			http.StatusConflict, "inspection is abandoned")
		wantError(t, do(t, a.router, http.MethodPost, completePathFor(a.siteA, a.inspection), a.supervisor, completeBody()),
			http.StatusConflict, "inspection is abandoned")
	})

	t.Run("it stays in the list, marked abandoned", func(t *testing.T) {
		running := startInspection(t, a.router, a.supervisor, a.siteA, a.tmpl)
		all := listInspections(t, a.router, a.owner, a.siteA, "")
		if summaryIDs(all) != running+","+a.inspection || all[1].Status != checklists.StatusAbandoned || all[1].AnsweredCount != 1 {
			t.Errorf("list %s (status %s, answered %d), want both, the abandoned one with its answer", summaryIDs(all), all[1].Status, all[1].AnsweredCount)
		}
		if ids := summaryIDs(listInspections(t, a.router, a.owner, a.siteA, "?status=abandoned")); ids != a.inspection {
			t.Errorf("abandoned only: %s, want %s", ids, a.inspection)
		}
	})

	t.Run("with a fail, the supervisor who started it can't; a SHE officer can", func(t *testing.T) {
		failed := startInspection(t, a.router, a.supervisor, a.siteA, a.tmpl)
		wantStatus(t, do(t, a.router, http.MethodPut, itemPath(a.siteA, failed, key), a.supervisor, answerBody("fail", "high", t1)), http.StatusOK)
		wantError(t, do(t, a.router, http.MethodPost, abandonPath(a.siteA, failed), a.supervisor, reason("Not needed")),
			http.StatusForbidden, "an inspection with a failed item can only be abandoned by an owner or a SHE officer")
		rec := do(t, a.router, http.MethodPost, abandonPath(a.siteA, failed), she, reason("Inspector left site"))
		wantStatus(t, rec, http.StatusCreated)
		if in := decodeInspection(t, rec); in.Items[0].Result == nil || *in.Items[0].Result != "fail" || len(in.Findings) != 0 {
			t.Errorf("inspection %+v, want the fail kept and no findings", in)
		}
	})

	t.Run("with a fail, an owner can", func(t *testing.T) {
		failed := startInspection(t, a.router, a.supervisor, a.siteA, a.tmpl)
		wantStatus(t, do(t, a.router, http.MethodPut, itemPath(a.siteA, failed, key), a.supervisor, answerBody("fail", "low", t1)), http.StatusOK)
		wantStatus(t, do(t, a.router, http.MethodPost, abandonPath(a.siteA, failed), a.owner, reason("Duplicate")), http.StatusCreated)
	})

	t.Run("a supervisor can't abandon someone else's", func(t *testing.T) {
		owners := startInspection(t, a.router, a.owner, a.siteA, a.tmpl)
		wantError(t, do(t, a.router, http.MethodPost, abandonPath(a.siteA, owners), a.supervisor, reason("Not mine")),
			http.StatusForbidden, "only the inspector who started this inspection, an owner or a SHE officer can abandon it")
	})

	t.Run("a completed inspection can't be abandoned", func(t *testing.T) {
		done := startInspection(t, a.router, a.supervisor, a.siteA, a.tmpl)
		completeBySQL(t, a.ownerMe.OrgID, done, len(a.tmpl.Items))
		wantError(t, do(t, a.router, http.MethodPost, abandonPath(a.siteA, done), a.owner, reason("Too late")),
			http.StatusConflict, "inspection is completed; start a new inspection to correct it")
	})

	for name, body := range map[string]map[string]any{
		"no reason":      {},
		"a blank reason": reason("   "),
		"501 characters": reason(strings.Repeat("r", 501)),
	} {
		t.Run(name+" is 400", func(t *testing.T) {
			id := startInspection(t, a.router, a.supervisor, a.siteA, a.tmpl)
			wantError(t, do(t, a.router, http.MethodPost, abandonPath(a.siteA, id), a.supervisor, body), http.StatusBadRequest,
				"reason must be 1-500 characters")
		})
	}

	t.Run("scope", func(t *testing.T) {
		wantError(t, do(t, a.router, http.MethodPost, abandonPath(a.siteA, uuid.NewString()), a.owner, reason("x")), http.StatusNotFound, "inspection not found")
		wantStatus(t, do(t, a.router, http.MethodPost, abandonPath(a.siteA, a.inspection), a.bookkeeper, reason("x")), http.StatusForbidden)
		wantError(t, do(t, a.router, http.MethodPost, abandonPath(a.siteA, a.inspection), a.other, reason("x")), http.StatusNotFound, "site not found")
	})

	t.Run("an archived site still lets it be abandoned", func(t *testing.T) {
		id := startInspection(t, a.router, a.owner, a.siteB, a.tmpl)
		wantStatus(t, do(t, a.router, http.MethodDelete, "/v1/sites/"+a.siteB, a.owner, nil), http.StatusNoContent)
		wantStatus(t, do(t, a.router, http.MethodPost, abandonPath(a.siteB, id), a.owner, reason("Site closed")), http.StatusCreated)
	})
}

// Abandoning a correction frees the place for another (000014).
func TestAbandonFreesTheCorrection(t *testing.T) {
	a := newAnswerFixture(t, "abandoncorr")
	completeBySQL(t, a.ownerMe.OrgID, a.inspection, len(a.tmpl.Items))
	correcting := with(startBody(a.tmpl), "corrects_id", a.inspection)

	first := uuid.NewString()
	wantStatus(t, do(t, a.router, http.MethodPut, inspectionPath(a.siteA, first), a.supervisor, correcting), http.StatusCreated)
	wantError(t, do(t, a.router, http.MethodPut, inspectionPath(a.siteA, uuid.NewString()), a.supervisor, correcting),
		http.StatusBadRequest, "inspection already has a correction")

	wantStatus(t, do(t, a.router, http.MethodPost, abandonPath(a.siteA, first), a.supervisor, reason("Started by mistake")), http.StatusCreated)
	wantStatus(t, do(t, a.router, http.MethodPut, inspectionPath(a.siteA, uuid.NewString()), a.supervisor, correcting), http.StatusCreated)

	t.Run("an abandoned inspection can't be corrected", func(t *testing.T) {
		wantError(t, do(t, a.router, http.MethodPut, inspectionPath(a.siteA, uuid.NewString()), a.supervisor, with(startBody(a.tmpl), "corrects_id", first)),
			http.StatusBadRequest, "corrects_id must be a completed inspection on this site")
	})
}
