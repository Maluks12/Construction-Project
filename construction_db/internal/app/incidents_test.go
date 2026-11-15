package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"construction_db/internal/core/deadlines"
	"construction_db/internal/core/incidents"
	"construction_db/internal/platform/database"
)

const (
	gar8a = "ohsa_gar8_1a_written_notice"
	gar8b = "ohsa_gar8_1b_immediate_notice"
	gar82 = "ohsa_gar8_2_death_after_notice"
	gar92 = "ohsa_gar9_2_investigation"
	coida = "coida_s39_employer_report"
)

func incidentPath(site, id string) string { return "/v1/sites/" + site + "/incidents/" + id }
func versionPath(site, id string, n int) string {
	return fmt.Sprintf("%s/versions/%d", incidentPath(site, id), n)
}
func statementPath(site, incident, id string) string {
	return incidentPath(site, incident) + "/witness-statements/" + id
}
func deadlinePath(site, id string) string { return "/v1/sites/" + site + "/deadlines/" + id }
func satisfactionPath(site, deadline, id string) string {
	return deadlinePath(site, deadline) + "/satisfactions/" + id
}

func rfc(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// versionBody is a submission with the given types and occurrence time; the
// client time is a fixed point a minute ago so a repeat sends the same.
func versionBody(occurred time.Time, codes ...string) map[string]any {
	return map[string]any{
		"taxonomy_codes":      codes,
		"occurred_at":         rfc(occurred),
		"client_submitted_at": rfc(occurred.Add(time.Minute)),
	}
}

func decodeIncident(t *testing.T, rec *httptest.ResponseRecorder) incidents.Incident {
	t.Helper()
	var inc incidents.Incident
	if err := json.Unmarshal(rec.Body.Bytes(), &inc); err != nil {
		t.Fatalf("decode incident: %v (body: %s)", err, rec.Body.String())
	}
	return inc
}

func decodeDetail(t *testing.T, rec *httptest.ResponseRecorder) incidents.DeadlineDetail {
	t.Helper()
	var d incidents.DeadlineDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatalf("decode deadline: %v (body: %s)", err, rec.Body.String())
	}
	return d
}

// live returns the live row for rule, failing if there isn't exactly one.
func live(t *testing.T, inc incidents.Incident, rule string) deadlines.Deadline {
	t.Helper()
	var found []deadlines.Deadline
	for _, d := range inc.Deadlines {
		if d.RuleKey == rule && d.ClosedAs == nil {
			found = append(found, d)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d live %s rows, want 1: %+v", len(found), rule, inc.Deadlines)
	}
	return found[0]
}

// liveRules lists the live rows' rules and states, sorted.
func liveRules(inc incidents.Incident) []string {
	out := []string{}
	for _, d := range inc.Deadlines {
		if d.ClosedAs == nil {
			out = append(out, d.RuleKey+":"+d.State)
		}
	}
	slices.Sort(out)
	return out
}

func submitNew(t *testing.T, f evidenceFixture, token string, body map[string]any) incidents.Incident {
	t.Helper()
	rec := do(t, f.router, http.MethodPut, versionPath(f.siteA, uuid.NewString(), 1), token, body)
	wantStatus(t, rec, http.StatusCreated)
	return decodeIncident(t, rec)
}

func satisfy(t *testing.T, f evidenceFixture, token, deadline string, at time.Time, want int) incidents.DeadlineDetail {
	t.Helper()
	rec := do(t, f.router, http.MethodPut, satisfactionPath(f.siteA, deadline, uuid.NewString()), token,
		map[string]any{"satisfied_at": rfc(at), "method": "phone", "reference": "Call ref 123"})
	wantStatus(t, rec, want)
	return decodeDetail(t, rec)
}

func TestIncidentSubmitAndGet(t *testing.T) {
	f := newEvidenceFixture(t, "incident")
	now := time.Now().UTC().Truncate(time.Second)
	id := uuid.NewString()
	nearMiss := map[string]any{
		"taxonomy_codes": []string{"near_miss"}, "occurred_at": rfc(now.Add(-time.Hour)),
		"client_submitted_at": rfc(now), "description": "Pallet fell from the hoist; nobody below.",
	}

	t.Run("a near miss starts no clocks", func(t *testing.T) {
		rec := do(t, f.router, http.MethodPut, versionPath(f.siteA, id, 1), f.supervisor, nearMiss)
		wantStatus(t, rec, http.StatusCreated)
		inc := decodeIncident(t, rec)
		if inc.ID != id || inc.LatestVersion != 1 || inc.Reportable || len(inc.Deadlines) != 0 || inc.Version.Version != 1 ||
			len(inc.Version.Types) != 1 || inc.Version.Types[0].Label != "Near miss" || inc.Version.People == nil ||
			inc.Version.Evidence == nil || inc.WitnessStatements == nil {
			t.Errorf("incident %+v", inc)
		}
	})

	t.Run("an identical repeat is 200; other details are 409", func(t *testing.T) {
		wantStatus(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, id, 1), f.supervisor, nearMiss), http.StatusOK)
		wantError(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, id, 1), f.supervisor, with(nearMiss, "description", "Changed")),
			http.StatusConflict, "incident id already used with different details")
		wantError(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, id, 1), f.supervisor,
			with(nearMiss, "people", []map[string]any{{"employee_of_org": false, "injured": false}})),
			http.StatusConflict, "incident id already used with different details")
		// Another submitter counts as different details.
		wantError(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, id, 1), f.owner, nearMiss),
			http.StatusConflict, "incident id already used with different details")
		// The id on another site.
		wantError(t, do(t, f.router, http.MethodPut, versionPath(f.siteB, id, 1), f.owner, nearMiss),
			http.StatusConflict, "incident id already used with different details")
	})

	t.Run("versions come in order", func(t *testing.T) {
		rec := do(t, f.router, http.MethodPut, versionPath(f.siteA, id, 3), f.supervisor, with(nearMiss, "amend_reason", "Skip"))
		if rec.Code != http.StatusConflict || rec.Body.String() != `{"error":"not the next version; get the incident and amend its latest version","latest_version":1}` {
			t.Errorf("got %d %s", rec.Code, rec.Body.String())
		}
		wantError(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, uuid.NewString(), 2), f.supervisor, with(nearMiss, "amend_reason", "x")),
			http.StatusNotFound, "incident not found")
		v2 := with(with(nearMiss, "amend_reason", "Location added"), "location", "Hoist bay 2")
		wantStatus(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, id, 2), f.supervisor, v2), http.StatusCreated)
		wantStatus(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, id, 2), f.supervisor, v2), http.StatusOK)
		wantError(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, id, 2), f.supervisor, with(v2, "location", "Bay 3")),
			http.StatusConflict, "this version was already submitted with different details")
	})

	t.Run("get shows any version", func(t *testing.T) {
		inc := decodeIncident(t, do(t, f.router, http.MethodGet, incidentPath(f.siteA, id), f.owner, nil))
		if inc.LatestVersion != 2 || inc.Version.Version != 2 || inc.Version.Location == nil || *inc.Version.AmendReason != "Location added" {
			t.Errorf("latest %+v", inc.Version)
		}
		v1 := decodeIncident(t, do(t, f.router, http.MethodGet, incidentPath(f.siteA, id)+"?version=1", f.owner, nil))
		if v1.Version.Version != 1 || v1.Version.Location != nil || v1.LatestVersion != 2 {
			t.Errorf("version 1 %+v", v1.Version)
		}
		wantError(t, do(t, f.router, http.MethodGet, incidentPath(f.siteA, id)+"?version=3", f.owner, nil), http.StatusNotFound, "version not found")
		wantError(t, do(t, f.router, http.MethodGet, incidentPath(f.siteA, id)+"?version=0", f.owner, nil), http.StatusBadRequest,
			"version must be a whole number, 1 or more")
		wantError(t, do(t, f.router, http.MethodGet, incidentPath(f.siteA, uuid.NewString()), f.owner, nil), http.StatusNotFound, "incident not found")
		wantError(t, do(t, f.router, http.MethodGet, incidentPath(f.siteA, "not-a-uuid"), f.owner, nil), http.StatusNotFound, "incident not found")
	})

	t.Run("who", func(t *testing.T) {
		wantStatus(t, do(t, f.router, http.MethodGet, incidentPath(f.siteA, id), f.bookkeeper, nil), http.StatusForbidden)
		wantError(t, do(t, f.router, http.MethodGet, incidentPath(f.siteB, id), f.supervisor, nil), http.StatusNotFound, "site not found")
		wantError(t, do(t, f.router, http.MethodGet, incidentPath(f.siteA, id), f.other, nil), http.StatusNotFound, "site not found")
		wantError(t, do(t, f.router, http.MethodPut, versionPath(f.otherSite, id, 1), f.other, nearMiss),
			http.StatusConflict, "incident id already used with different details")
	})

	t.Run("photos", func(t *testing.T) {
		photo, _ := verifiedJPEG(t, f, f.supervisor, f.siteA)
		inc := submitNew(t, f, f.supervisor, with(nearMiss, "evidence_ids", []string{photo}))
		if len(inc.Version.Evidence) != 1 || inc.Version.Evidence[0].EvidenceID != photo || inc.Version.Evidence[0].ClientCapturedAt.IsZero() {
			t.Errorf("evidence %+v", inc.Version.Evidence)
		}
		unknown := uuid.NewString()
		rec := do(t, f.router, http.MethodPut, versionPath(f.siteA, uuid.NewString(), 1), f.supervisor, with(nearMiss, "evidence_ids", []string{unknown}))
		if rec.Code != http.StatusBadRequest || rec.Body.String() != `{"error":"evidence not found on this site","evidence_ids":["`+unknown+`"]}` {
			t.Errorf("unknown photo: %d %s", rec.Code, rec.Body.String())
		}
		pending := uuid.NewString()
		wantStatus(t, do(t, f.router, http.MethodPut, evidencePath(f.siteA, pending), f.supervisor, declareBody("pending photo")), http.StatusCreated)
		rec = do(t, f.router, http.MethodPut, versionPath(f.siteA, uuid.NewString(), 1), f.supervisor, with(nearMiss, "evidence_ids", []string{pending}))
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "evidence not verified yet") {
			t.Errorf("pending photo: %d %s", rec.Code, rec.Body.String())
		}
	})
}

func TestIncidentValidation(t *testing.T) {
	f := newEvidenceFixture(t, "incidentvalid")
	now := time.Now().UTC().Truncate(time.Second)
	base := versionBody(now.Add(-time.Hour), "first_aid")
	person := func(fields map[string]any) map[string]any { return with(base, "people", []map[string]any{fields}) }
	future := rfc(now.Add(time.Hour))

	tests := []struct {
		name, msg string
		version   int
		body      map[string]any
	}{
		{"no types", "taxonomy_codes must list at least one incident type", 1, with(base, "taxonomy_codes", []string{})},
		{"repeated types", "taxonomy_codes must not repeat", 1, with(base, "taxonomy_codes", []string{"first_aid", "first_aid"})},
		{"no occurred_at", "occurred_at is required", 1, with(base, "occurred_at", nil)},
		{"future occurred_at", "occurred_at is in the future; check the device clock", 1, with(base, "occurred_at", future)},
		{"aware before", "employer_aware_at must not be before occurred_at", 1, with(base, "employer_aware_at", rfc(now.Add(-2*time.Hour)))},
		{"aware future", "employer_aware_at is in the future; check the device clock", 1, with(base, "employer_aware_at", future)},
		{"no client time", "client_submitted_at is required", 1, with(base, "client_submitted_at", nil)},
		{"future client time", "client_submitted_at is in the future; check the device clock", 1, with(base, "client_submitted_at", future)},
		{"empty location", "location must be 1-500 characters", 1, with(base, "location", "")},
		{"long description", "description must be 1-5000 characters", 1, with(base, "description", strings.Repeat("x", 5001))},
		{"reason on version 1", "version 1 has no amend_reason", 1, with(base, "amend_reason", "Why")},
		{"bad treatment", "treatment must be none, first_aid, medical_treatment or hospitalised", 1,
			person(map[string]any{"employee_of_org": false, "injured": true, "treatment": "surgery"})},
		{"injury on someone uninjured", "injury details are only given for someone injured", 1,
			person(map[string]any{"employee_of_org": false, "injured": false, "body_part": "Hand"})},
		{"death not injured", "someone who died must be marked injured", 1,
			person(map[string]any{"employee_of_org": false, "injured": false, "died_at": rfc(now)})},
		{"employer of an employee", "employer_name is only given for someone not employed by the organisation", 1,
			person(map[string]any{"employee_of_org": true, "employer_name": "Sub (Pty) Ltd", "injured": false})},
		{"death before the incident", "died_at must not be before occurred_at", 1,
			person(map[string]any{"employee_of_org": false, "injured": true, "died_at": rfc(now.Add(-2 * time.Hour))})},
		{"death in the future", "died_at is in the future; check the device clock", 1,
			person(map[string]any{"employee_of_org": false, "injured": true, "died_at": future})},
		{"long name", "people: name, occupation, employer_name and body_part must be 1-200 characters, injury_description 1-2000", 1,
			person(map[string]any{"employee_of_org": false, "injured": false, "name": strings.Repeat("x", 201)})},
		// D11: OHSA s24(1)(a), "any person dies".
		{"death not reportable", "a death must be classified with at least one reportable incident type", 1,
			person(map[string]any{"employee_of_org": false, "injured": true, "died_at": rfc(now)})},
		// D2: COIDA's clock needs to know when the employer learned of it.
		{"injured employee without employer_aware_at", "employer_aware_at is required when an employee of the organisation is injured", 1,
			person(map[string]any{"employee_of_org": true, "injured": true})},
		{"bad photo id", "evidence_ids must be UUIDs", 1, with(base, "evidence_ids", []string{"x"})},
		{"bad device id", "device_id must be 1-200 bytes", 1, with(base, "device_id", "")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantError(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, uuid.NewString(), tt.version), f.owner, tt.body),
				http.StatusBadRequest, tt.msg)
		})
	}

	t.Run("unknown types are named", func(t *testing.T) {
		rec := do(t, f.router, http.MethodPut, versionPath(f.siteA, uuid.NewString(), 1), f.owner, with(base, "taxonomy_codes", []string{"first_aid", "alien_abduction"}))
		if rec.Code != http.StatusBadRequest || rec.Body.String() != `{"error":"taxonomy_codes must be incident types of the site's regime","taxonomy_codes":["alien_abduction"]}` {
			t.Errorf("got %d %s", rec.Code, rec.Body.String())
		}
	})
	t.Run("ids and versions", func(t *testing.T) {
		wantError(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, "nope", 1), f.owner, base), http.StatusBadRequest, "incident id must be a UUID")
		wantError(t, do(t, f.router, http.MethodPut, incidentPath(f.siteA, uuid.NewString())+"/versions/0", f.owner, base),
			http.StatusBadRequest, "version must be a whole number, 1 or more")
		wantError(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, uuid.NewString(), 2), f.owner, base),
			http.StatusBadRequest, "amend_reason is required for an amendment (1-500 characters)")
	})
}

// Reclassifying up adds clocks timed from the incident, which may already be
// overdue; the clocks already running carry on.
func TestIncidentReclassifiedUp(t *testing.T) {
	f := newEvidenceFixture(t, "incidentup")
	occurred := time.Now().UTC().Add(-10 * 24 * time.Hour).Truncate(time.Second)
	id := uuid.NewString()
	v1 := with(with(versionBody(occurred, "medical_treatment"), "employer_aware_at", rfc(occurred)),
		"people", []map[string]any{{"employee_of_org": true, "injured": true, "treatment": "medical_treatment", "body_part": "Left hand"}})

	inc := decodeIncident(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, id, 1), f.supervisor, v1))
	if got := liveRules(inc); !slices.Equal(got, []string{coida + ":overdue", gar92 + ":overdue"}) {
		t.Errorf("v1 clocks %v", got)
	}
	inv := live(t, inc, gar92)
	if !inv.OverdueWhenComputed || inv.DueOn == nil || inv.TimeZone != "Africa/Johannesburg" || inv.SourceRef == "" {
		t.Errorf("investigation %+v", inv)
	}

	rec := do(t, f.router, http.MethodPut, versionPath(f.siteA, id, 2), f.supervisor,
		with(with(v1, "taxonomy_codes", []string{"injury_14_days_unable_to_work"}), "amend_reason", "Off work for three weeks"))
	wantStatus(t, rec, http.StatusCreated)
	inc = decodeIncident(t, rec)
	notice := live(t, inc, gar8a)
	if notice.ComputedInVersion != 2 || !notice.OverdueWhenComputed || notice.State != deadlines.StateOverdue || notice.Caveat == nil {
		t.Errorf("written notice %+v, want added in v2, overdue from the start, with the s24(3) caveat", notice)
	}
	if len(inc.Changes.Added) != 1 || inc.Changes.Added[0] != notice.ID || len(inc.Changes.Carried) != 2 ||
		len(inc.Changes.Superseded) != 0 || len(inc.Changes.Withdrawn) != 0 {
		t.Errorf("changes %+v", inc.Changes)
	}
	if live(t, inc, gar92).ID != inv.ID {
		t.Error("the investigation should carry, not be recomputed")
	}
}

// The satisfaction belongs to the duty: when occurred_at is corrected
// earlier, the deadline is recomputed and the call is judged against the new
// date (change 1 on approval, 15 Nov).
func TestIncidentCorrectionJudgesTheSatisfactionAgain(t *testing.T) {
	f := newEvidenceFixture(t, "incidentcorrect")
	day := 24 * time.Hour
	occurred := time.Now().UTC().Add(-7 * day).Truncate(time.Second)
	id := uuid.NewString()
	v1 := versionBody(occurred, "loss_of_limb")

	inc := decodeIncident(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, id, 1), f.supervisor, v1))
	notice := live(t, inc, gar8a)
	// Written notice on day 6 of 7: on time.
	d := satisfy(t, f, f.supervisor, notice.ID, occurred.Add(6*day), http.StatusCreated)
	if d.State != deadlines.StateSatisfied || d.Satisfaction == nil {
		t.Fatalf("after the call: %+v", d.Deadline)
	}

	// The incident was really two days earlier: due on day 5 of the old count.
	rec := do(t, f.router, http.MethodPut, versionPath(f.siteA, id, 2), f.supervisor,
		with(with(v1, "occurred_at", rfc(occurred.Add(-2*day))), "amend_reason", "Date corrected from the site diary"))
	wantStatus(t, rec, http.StatusCreated)
	inc = decodeIncident(t, rec)
	now := live(t, inc, gar8a)
	if now.ID == notice.ID || now.State != deadlines.StateSatisfiedLate || now.Satisfaction == nil || now.Satisfaction.ID != d.Satisfaction.ID {
		t.Errorf("recomputed notice %+v, want a new row, satisfied late, by the same satisfaction", now)
	}
	old := inc.Deadlines[slices.IndexFunc(inc.Deadlines, func(x deadlines.Deadline) bool { return x.ID == notice.ID })]
	if old.State != deadlines.StateSuperseded || old.SupersededBy == nil || *old.SupersededBy != now.ID || old.ClosedBy == nil ||
		*old.ClosedReason != "Date corrected from the site diary" {
		t.Errorf("old notice %+v", old)
	}
	if !slices.Contains(inc.Changes.Superseded, notice.ID) {
		t.Errorf("changes %+v", inc.Changes)
	}
}

// Downgrading withdraws report clocks: only an owner or SHE officer may, and
// the rows stay with who, when and why.
func TestIncidentDowngrade(t *testing.T) {
	f := newEvidenceFixture(t, "incidentdown")
	occurred := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	id := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, id, 1), f.supervisor, versionBody(occurred, "unconsciousness")), http.StatusCreated)
	down := with(versionBody(occurred, "first_aid"), "amend_reason", "He fainted from the heat, not the fall")

	wantError(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, id, 2), f.supervisor, down),
		http.StatusForbidden, "only an owner or a SHE officer can withdraw a report deadline or move it later")

	rec := do(t, f.router, http.MethodPut, versionPath(f.siteA, id, 2), f.owner, down)
	wantStatus(t, rec, http.StatusCreated)
	inc := decodeIncident(t, rec)
	if got := liveRules(inc); len(got) != 0 {
		t.Errorf("live clocks %v, want none", got)
	}
	if len(inc.Changes.Withdrawn) != 3 || len(inc.Deadlines) != 3 {
		t.Errorf("changes %+v, deadlines %d", inc.Changes, len(inc.Deadlines))
	}
	for _, d := range inc.Deadlines {
		if d.State != deadlines.StateWithdrawn || d.ClosedBy == nil || *d.ClosedBy != f.ownerMe.UserID || d.ClosedAt == nil ||
			*d.ClosedReason != "He fainted from the heat, not the fall" {
			t.Errorf("withdrawn %+v", d)
		}
	}
}

// GAR 8(2): a death after the notice adds the death notice, whether the death
// comes in a later version or the call is recorded after the death was.
func TestDeathAfterNotice(t *testing.T) {
	f := newEvidenceFixture(t, "incidentdeath")
	occurred := time.Now().UTC().Add(-6 * time.Hour).Truncate(time.Second)

	t.Run("death in a later version", func(t *testing.T) {
		id := uuid.NewString()
		v1 := with(versionBody(occurred, "loss_of_limb"), "people", []map[string]any{{"employee_of_org": false, "injured": true}})
		inc := decodeIncident(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, id, 1), f.supervisor, v1))
		satisfy(t, f, f.supervisor, live(t, inc, gar8b).ID, occurred.Add(30*time.Minute), http.StatusCreated)

		died := occurred.Add(3 * time.Hour)
		v2 := with(with(v1, "amend_reason", "Died in hospital"), "people",
			[]map[string]any{{"employee_of_org": false, "injured": true, "died_at": rfc(died)}})
		inc = decodeIncident(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, id, 2), f.supervisor, v2))
		death := live(t, inc, gar82)
		if !death.BasisAt.Equal(died) || death.State != deadlines.StateDueNow || death.Basis != "person_died_at" {
			t.Errorf("death notice %+v", death)
		}
	})

	t.Run("call recorded after the death", func(t *testing.T) {
		died := occurred.Add(2 * time.Hour)
		body := with(versionBody(occurred, "loss_of_limb"), "people", []map[string]any{{"employee_of_org": false, "injured": true, "died_at": rfc(died)}})
		inc := submitNew(t, f, f.supervisor, body)
		if slices.ContainsFunc(inc.Deadlines, func(d deadlines.Deadline) bool { return d.RuleKey == gar82 }) {
			t.Fatal("no notice yet, so no death notice yet")
		}
		sat := satisfy(t, f, f.supervisor, live(t, inc, gar8b).ID, occurred.Add(time.Hour), http.StatusCreated).Satisfaction
		inc = decodeIncident(t, do(t, f.router, http.MethodGet, incidentPath(f.siteA, inc.ID), f.owner, nil))
		death := live(t, inc, gar82)
		if !death.BasisAt.Equal(died) || death.ComputedInVersion != 1 || death.AddedBySatisfactionID == nil ||
			*death.AddedBySatisfactionID != sat.ID {
			t.Errorf("death notice %+v", death)
		}
		// Recording the call added it, not version 1.
		if slices.Contains(inc.Changes.Added, death.ID) || slices.Contains(inc.Changes.Carried, death.ID) {
			t.Errorf("version 1's changes %+v include the death notice", inc.Changes)
		}

		// Retracting the call leaves the death notice live; the next
		// amendment withdraws it, which takes an owner or SHE officer.
		wantStatus(t, do(t, f.router, http.MethodPost, satisfactionPath(f.siteA, live(t, inc, gar8b).ID, sat.ID)+"/retract", f.owner,
			reason("Call was about another incident")), http.StatusCreated)
		v2 := with(body, "amend_reason", "Location added")
		wantError(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, inc.ID, 2), f.supervisor, v2),
			http.StatusForbidden, "only an owner or a SHE officer can withdraw a report deadline or move it later")
		rec := do(t, f.router, http.MethodPut, versionPath(f.siteA, inc.ID, 2), f.owner, v2)
		wantStatus(t, rec, http.StatusCreated)
		if inc = decodeIncident(t, rec); !slices.Contains(inc.Changes.Withdrawn, death.ID) {
			t.Errorf("version 2's changes %+v; want the death notice withdrawn", inc.Changes)
		}
	})
}

func TestDeadlineSatisfactions(t *testing.T) {
	f := newEvidenceFixture(t, "incidentsat")
	occurred := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	inc := submitNew(t, f, f.supervisor, versionBody(occurred, "major_incident"))
	call := live(t, inc, gar8b)
	satID := uuid.NewString()
	body := map[string]any{"satisfied_at": rfc(occurred.Add(10 * time.Minute)), "method": "phone", "reference": "DoEL Gauteng, ref 55"}

	t.Run("record, repeat, conflict", func(t *testing.T) {
		rec := do(t, f.router, http.MethodPut, satisfactionPath(f.siteA, call.ID, satID), f.supervisor, body)
		wantStatus(t, rec, http.StatusCreated)
		d := decodeDetail(t, rec)
		if d.State != deadlines.StateSatisfied || d.Satisfaction.RecordedBy == "" || len(d.Satisfactions) != 1 {
			t.Errorf("detail %+v", d)
		}
		wantStatus(t, do(t, f.router, http.MethodPut, satisfactionPath(f.siteA, call.ID, satID), f.supervisor, body), http.StatusOK)
		wantError(t, do(t, f.router, http.MethodPut, satisfactionPath(f.siteA, call.ID, satID), f.supervisor, with(body, "method", "email")),
			http.StatusConflict, "satisfaction id already used with different details")
		wantError(t, do(t, f.router, http.MethodPut, satisfactionPath(f.siteA, call.ID, uuid.NewString()), f.supervisor, body),
			http.StatusConflict, "deadline is already satisfied; retract that satisfaction first")
	})

	t.Run("validation", func(t *testing.T) {
		for _, tt := range []struct {
			msg  string
			body map[string]any
		}{
			{"satisfied_at is required", with(body, "satisfied_at", nil)},
			{"satisfied_at is in the future; check the device clock", with(body, "satisfied_at", rfc(time.Now().Add(time.Hour)))},
			{"method must be phone, email, fax, in_person, online, post or other", with(body, "method", "pigeon")},
			{"reference must be 1-200 characters", with(body, "reference", "")},
			{"note must be 1-2000 characters", with(body, "note", "")},
			{"evidence_id must be a UUID", with(body, "evidence_id", "x")},
		} {
			wantError(t, do(t, f.router, http.MethodPut, satisfactionPath(f.siteA, call.ID, uuid.NewString()), f.supervisor, tt.body),
				http.StatusBadRequest, tt.msg)
		}
		wantError(t, do(t, f.router, http.MethodPut, satisfactionPath(f.siteA, call.ID, "x"), f.supervisor, body),
			http.StatusBadRequest, "satisfaction id must be a UUID")
		wantError(t, do(t, f.router, http.MethodPut, satisfactionPath(f.siteA, uuid.NewString(), uuid.NewString()), f.supervisor, body),
			http.StatusNotFound, "deadline not found")
		inv := live(t, inc, gar92)
		wantError(t, do(t, f.router, http.MethodPut, satisfactionPath(f.siteA, inv.ID, uuid.NewString()), f.supervisor,
			with(body, "satisfied_at", rfc(occurred.Add(-time.Hour)))), http.StatusBadRequest, "satisfied_at must not be before the deadline's basis_at")
	})

	t.Run("retract", func(t *testing.T) {
		path := satisfactionPath(f.siteA, call.ID, satID) + "/retract"
		wantError(t, do(t, f.router, http.MethodPost, path, f.supervisor, reason("Wrong incident")),
			http.StatusForbidden, "only an owner or a SHE officer can retract a satisfaction")
		wantError(t, do(t, f.router, http.MethodPost, path, f.owner, reason("")), http.StatusBadRequest, "reason must be 1-500 characters")
		rec := do(t, f.router, http.MethodPost, path, f.owner, reason("Wrong incident"))
		wantStatus(t, rec, http.StatusCreated)
		d := decodeDetail(t, rec)
		if d.State != deadlines.StateDueNow || d.Satisfaction != nil || len(d.Satisfactions) != 1 || d.Satisfactions[0].Retraction == nil {
			t.Errorf("after retraction %+v", d)
		}
		wantStatus(t, do(t, f.router, http.MethodPost, path, f.owner, reason("Wrong incident")), http.StatusOK)
		wantError(t, do(t, f.router, http.MethodPost, path, f.owner, reason("Other")), http.StatusConflict,
			"satisfaction was already retracted with a different reason")
		wantError(t, do(t, f.router, http.MethodPost, satisfactionPath(f.siteA, call.ID, uuid.NewString())+"/retract", f.owner, reason("x")),
			http.StatusNotFound, "satisfaction not found")
		// Open again: a new satisfaction may be recorded.
		d = satisfy(t, f, f.supervisor, call.ID, occurred.Add(20*time.Minute), http.StatusCreated)
		if d.State != deadlines.StateSatisfied || len(d.Satisfactions) != 2 {
			t.Errorf("after a new satisfaction %+v", d)
		}
	})

	t.Run("site list and filter", func(t *testing.T) {
		var out struct {
			Deadlines []deadlines.Deadline `json:"deadlines"`
		}
		rec := do(t, f.router, http.MethodGet, "/v1/sites/"+f.siteA+"/deadlines?state=satisfied", f.owner, nil)
		wantStatus(t, rec, http.StatusOK)
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Deadlines) != 1 || out.Deadlines[0].ID != call.ID {
			t.Errorf("satisfied list %+v, err %v", out.Deadlines, err)
		}
		wantError(t, do(t, f.router, http.MethodGet, "/v1/sites/"+f.siteA+"/deadlines?state=withdrawn", f.owner, nil),
			http.StatusBadRequest, "state must be open, due_now, overdue, satisfied or satisfied_late")
		wantError(t, do(t, f.router, http.MethodGet, "/v1/sites/"+f.siteA+"/deadlines?limit=0", f.owner, nil), http.StatusBadRequest, "limit must be 1-200")
		wantError(t, do(t, f.router, http.MethodGet, deadlinePath(f.siteA, uuid.NewString()), f.owner, nil), http.StatusNotFound, "deadline not found")
	})

	t.Run("a superseded id records against the live row", func(t *testing.T) {
		v2 := with(versionBody(occurred.Add(-time.Hour), "major_incident"), "amend_reason", "An hour earlier")
		inc2 := decodeIncident(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, inc.ID, 2), f.owner, v2))
		oldInv := live(t, inc, gar92)
		satID := uuid.NewString()
		body := map[string]any{"satisfied_at": rfc(occurred), "method": "phone"}
		rec := do(t, f.router, http.MethodPut, satisfactionPath(f.siteA, oldInv.ID, satID), f.supervisor, body)
		wantStatus(t, rec, http.StatusCreated)
		if d := decodeDetail(t, rec); d.ID != live(t, inc2, gar92).ID {
			t.Errorf("recorded against %s, want the live row %s", d.ID, live(t, inc2, gar92).ID)
		}

		// After a further correction, a repeat answers with the new live row.
		v3 := with(versionBody(occurred.Add(-2*time.Hour), "major_incident"), "amend_reason", "Two hours earlier")
		inc3 := decodeIncident(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, inc.ID, 3), f.owner, v3))
		rec = do(t, f.router, http.MethodPut, satisfactionPath(f.siteA, oldInv.ID, satID), f.supervisor, body)
		wantStatus(t, rec, http.StatusOK)
		if d := decodeDetail(t, rec); d.ID != live(t, inc3, gar92).ID || d.Satisfaction == nil || d.Satisfaction.ID != satID {
			t.Errorf("repeat answered with %s (%+v), want the live row %s", d.ID, d.Satisfaction, live(t, inc3, gar92).ID)
		}
	})

	t.Run("recorded after the due time", func(t *testing.T) {
		occurred := time.Now().UTC().Add(-10 * 24 * time.Hour).Truncate(time.Second)
		inc := submitNew(t, f, f.supervisor, versionBody(occurred, "injury_14_days_unable_to_work"))
		// Posted on day 2, recorded on day 10: in time, but an alert may have shown.
		d := satisfy(t, f, f.supervisor, live(t, inc, gar8a).ID, occurred.Add(24*time.Hour), http.StatusCreated)
		if d.State != deadlines.StateSatisfied || !d.Satisfaction.RecordedAfterDue || !d.Satisfactions[0].RecordedAfterDue {
			t.Errorf("late record %+v", d)
		}
		d = satisfy(t, f, f.supervisor, live(t, inc, gar92).ID, occurred.Add(24*time.Hour), http.StatusCreated)
		if d.State != deadlines.StateSatisfied || !d.Satisfaction.RecordedAfterDue {
			t.Errorf("late record %+v", d)
		}
		inc = submitNew(t, f, f.supervisor, versionBody(time.Now().UTC().Add(-time.Hour).Truncate(time.Second), "injury_14_days_unable_to_work"))
		d = satisfy(t, f, f.supervisor, live(t, inc, gar8a).ID, time.Now().UTC().Add(-time.Minute), http.StatusCreated)
		if d.Satisfaction.RecordedAfterDue || d.Satisfactions[0].RecordedAfterDue {
			t.Errorf("recorded in time but flagged %+v", d)
		}
	})
}

// deadline_due refuses a day before the holiday calendar; the API says why.
// The error text names the calendar's first day, so it must match the table.
func TestIncidentBeforeTheCalendar(t *testing.T) {
	f := newEvidenceFixture(t, "incidentold")
	var first time.Time
	if err := testPool.QueryRow(context.Background(), `SELECT first_date FROM holiday_calendar_coverage WHERE jurisdiction = 'ZA'`).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(incidents.ErrBeforeCalendar.Error(), first.Format(time.DateOnly)) {
		t.Errorf("%q doesn't name the calendar's first day %s", incidents.ErrBeforeCalendar, first.Format(time.DateOnly))
	}
	old := time.Date(2023, 12, 1, 8, 0, 0, 0, time.UTC)
	wantError(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, uuid.NewString(), 1), f.owner, versionBody(old, "fatality")),
		http.StatusBadRequest, incidents.ErrBeforeCalendar.Error())
	// Its clocks end inside the calendar, so this one is fine.
	wantStatus(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, uuid.NewString(), 1), f.owner,
		versionBody(time.Date(2023, 12, 30, 8, 0, 0, 0, time.UTC), "fatality")), http.StatusCreated)
	// A non-reportable type has no clock, so no calendar is needed.
	wantStatus(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, uuid.NewString(), 1), f.owner, versionBody(old, "near_miss")),
		http.StatusCreated)
}

func TestWitnessStatements(t *testing.T) {
	f := newEvidenceFixture(t, "incidentwitness")
	inc := submitNew(t, f, f.supervisor, versionBody(time.Now().UTC().Add(-time.Hour), "near_miss"))
	id := uuid.NewString()
	body := map[string]any{"witness_name": "T. Mokoena", "statement": "I saw the pallet slip.", "taken_at": rfc(time.Now().UTC().Truncate(time.Second))}

	rec := do(t, f.router, http.MethodPut, statementPath(f.siteA, inc.ID, id), f.supervisor, body)
	wantStatus(t, rec, http.StatusCreated)
	wantStatus(t, do(t, f.router, http.MethodPut, statementPath(f.siteA, inc.ID, id), f.supervisor, body), http.StatusOK)
	wantError(t, do(t, f.router, http.MethodPut, statementPath(f.siteA, inc.ID, id), f.supervisor, with(body, "statement", "Changed.")),
		http.StatusConflict, "statement id already used with different details")

	fix := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, statementPath(f.siteA, inc.ID, fix), f.supervisor,
		with(with(body, "statement", "The pallet slipped off the forks."), "supersedes_id", id)), http.StatusCreated)
	wantError(t, do(t, f.router, http.MethodPut, statementPath(f.siteA, inc.ID, uuid.NewString()), f.supervisor, with(body, "supersedes_id", id)),
		http.StatusConflict, "that statement already has a correction")
	other := submitNew(t, f, f.supervisor, versionBody(time.Now().UTC().Add(-time.Hour), "near_miss"))
	wantError(t, do(t, f.router, http.MethodPut, statementPath(f.siteA, other.ID, uuid.NewString()), f.supervisor, with(body, "supersedes_id", fix)),
		http.StatusBadRequest, "supersedes_id must be a statement on this incident")

	for _, tt := range []struct {
		msg  string
		body map[string]any
	}{
		{"witness_name must be 1-200 characters", with(body, "witness_name", "")},
		{"witness_contact must be 1-200 characters", with(body, "witness_contact", "")},
		{"statement must be 1-20000 characters", with(body, "statement", "")},
		{"taken_at is required", with(body, "taken_at", nil)},
		{"taken_at is in the future; check the device clock", with(body, "taken_at", rfc(time.Now().Add(time.Hour)))},
	} {
		wantError(t, do(t, f.router, http.MethodPut, statementPath(f.siteA, inc.ID, uuid.NewString()), f.supervisor, tt.body), http.StatusBadRequest, tt.msg)
	}
	wantError(t, do(t, f.router, http.MethodPut, statementPath(f.siteA, inc.ID, "x"), f.supervisor, body), http.StatusBadRequest, "statement id must be a UUID")
	wantError(t, do(t, f.router, http.MethodPut, statementPath(f.siteA, uuid.NewString(), uuid.NewString()), f.supervisor, body),
		http.StatusNotFound, "incident not found")

	got := decodeIncident(t, do(t, f.router, http.MethodGet, incidentPath(f.siteA, inc.ID), f.owner, nil))
	if len(got.WitnessStatements) != 2 || got.WitnessStatements[0].SupersededBy == nil || *got.WitnessStatements[0].SupersededBy != fix {
		t.Errorf("statements %+v", got.WitnessStatements)
	}
}

func TestIncidentRegister(t *testing.T) {
	f := newEvidenceFixture(t, "incidentlist")
	now := time.Now().UTC().Truncate(time.Second)
	near := submitNew(t, f, f.supervisor, versionBody(now.Add(-3*time.Hour), "near_miss"))
	death := submitNew(t, f, f.supervisor, with(versionBody(now.Add(-2*time.Hour), "fatality"),
		"people", []map[string]any{{"employee_of_org": false, "injured": true, "died_at": rfc(now.Add(-2 * time.Hour))}}))

	list := func(query string) []incidents.IncidentSummary {
		t.Helper()
		rec := do(t, f.router, http.MethodGet, "/v1/sites/"+f.siteA+"/incidents"+query, f.owner, nil)
		wantStatus(t, rec, http.StatusOK)
		var out struct {
			Incidents []incidents.IncidentSummary `json:"incidents"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out.Incidents
	}
	ids := func(s []incidents.IncidentSummary) []string {
		out := []string{}
		for _, x := range s {
			out = append(out, x.ID)
		}
		return out
	}

	all := list("")
	if !slices.Equal(ids(all), []string{death.ID, near.ID}) {
		t.Fatalf("register %v, want the death then the near miss", ids(all))
	}
	if d := all[0]; !d.Reportable || d.DeathCount != 1 || d.InjuredCount != 1 || d.OpenDeadlines != 3 || d.NextDueAt == nil {
		t.Errorf("death summary %+v", d)
	}
	for query, want := range map[string][]string{
		"?type=near_miss":           {near.ID},
		"?reportable=true":          {death.ID},
		"?open_deadlines=false":     {near.ID},
		"?reportable=false&limit=1": {near.ID},
	} {
		if got := ids(list(query)); !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", query, got, want)
		}
	}
	for query, msg := range map[string]string{
		"?type=nope":        "type must be an incident type of the site's regime",
		"?reportable=yes":   "reportable must be true or false",
		"?open_deadlines=1": "open_deadlines must be true or false",
		"?limit=201":        "limit must be 1-200",
	} {
		wantError(t, do(t, f.router, http.MethodGet, "/v1/sites/"+f.siteA+"/incidents"+query, f.owner, nil), http.StatusBadRequest, msg)
	}
	// A supervisor sees only their own sites.
	wantStatus(t, do(t, f.router, http.MethodGet, "/v1/sites/"+f.siteB+"/incidents", f.supervisor, nil), http.StatusNotFound)
}

func TestIncidentOnArchivedSite(t *testing.T) {
	f := newEvidenceFixture(t, "incidentarchived")
	now := time.Now().UTC().Truncate(time.Second)
	execAsTenant(t, f.ownerMe.OrgID, 1, `UPDATE sites SET status = 'archived', archived_at = now() - interval '1 day' WHERE id = $1`, f.siteB)
	wantError(t, do(t, f.router, http.MethodPut, versionPath(f.siteB, uuid.NewString(), 1), f.owner, versionBody(now.Add(-time.Hour), "near_miss")),
		http.StatusConflict, "site is archived")
	// Reported late, but it happened before the site was archived.
	wantStatus(t, do(t, f.router, http.MethodPut, versionPath(f.siteB, uuid.NewString(), 1), f.owner, versionBody(now.Add(-48*time.Hour), "near_miss")),
		http.StatusCreated)
}

// Two submissions of version 2, made deterministic: one is open but
// uncommitted, so the request queues on the incident's lock. When the other
// commits, the request reads it and answers as a repeat; with other details,
// as a conflict.
func TestIncidentVersionLosesTheRace(t *testing.T) {
	f := newEvidenceFixture(t, "incidentrace")
	occurred := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)

	for _, tt := range []struct {
		name       string
		winnerNote string
		want       int
	}{
		{"identical: a repeat", "Same note", http.StatusOK},
		{"different: a conflict", "Other note", http.StatusConflict},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			id := uuid.NewString()
			wantStatus(t, do(t, f.router, http.MethodPut, versionPath(f.siteA, id, 1), f.owner, versionBody(occurred, "near_miss")), http.StatusCreated)
			v2 := with(versionBody(occurred, "near_miss"), "amend_reason", "Same note")

			winner, err := testPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer winner.Rollback(ctx)
			if err := database.SetTenant(ctx, winner, database.Tenant{OrgID: f.ownerMe.OrgID}); err != nil {
				t.Fatal(err)
			}
			_, err = winner.Exec(ctx, `
				INSERT INTO incident_versions (org_id, site_id, incident_id, version, taxonomy_codes, occurred_at, submitted_by,
				                               client_submitted_at, amend_reason, people_count, evidence_count)
				VALUES ($1, $2, $3, 2, '{near_miss}', $4, $5, $6, $7, 0, 0)`,
				f.ownerMe.OrgID, f.siteA, id, occurred, f.ownerMe.UserID, occurred.Add(time.Minute), tt.winnerNote)
			if err != nil {
				t.Fatalf("winner's version: %v", err)
			}

			loser := make(chan *httptest.ResponseRecorder, 1)
			go func() { loser <- do(t, f.router, http.MethodPut, versionPath(f.siteA, id, 2), f.owner, v2) }()
			waitForLock(t, "FOR NO KEY UPDATE")
			if err := winner.Commit(ctx); err != nil {
				t.Fatalf("commit winner: %v", err)
			}
			rec := <-loser
			wantStatus(t, rec, tt.want)
			if tt.want == http.StatusConflict && !strings.Contains(rec.Body.String(), "this version was already submitted with different details") {
				t.Errorf("body %s", rec.Body.String())
			}
			if n := countRows(t, `SELECT count(*) FROM incident_versions WHERE incident_id = $1`, id); n != 2 {
				t.Errorf("%d versions, want 2", n)
			}
		})
	}
}

// holdOpen starts a transaction in the fixture's org that the test commits or
// rolls back, to stop a request at a chosen point.
func holdOpen(t *testing.T, f evidenceFixture) pgx.Tx {
	t.Helper()
	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tx.Rollback(ctx) })
	if err := database.SetTenant(ctx, tx, database.Tenant{OrgID: f.ownerMe.OrgID}); err != nil {
		t.Fatal(err)
	}
	return tx
}

// Recording a call can add the death notice, computed in the incident's
// latest version, so a satisfaction and an amendment of one incident must
// queue. Made deterministic: an uncommitted duty row stops the satisfaction
// after it has read the latest version, and the amendment arrives meanwhile.
// It must wait for the satisfaction, not commit a version 2 under it (which
// made the satisfaction's insert fail with a 500).
func TestSatisfactionAndAmendmentQueue(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFixture(t, "incidentqueue")
	occurred := time.Now().UTC().Add(-6 * time.Hour).Truncate(time.Second)
	people := []map[string]any{{"employee_of_org": false, "injured": true, "died_at": rfc(occurred.Add(2 * time.Hour))}}
	v1 := with(versionBody(occurred, "loss_of_limb"), "people", people)
	inc := submitNew(t, f, f.supervisor, v1)

	hold := holdOpen(t, f)
	if _, err := hold.Exec(ctx, `INSERT INTO deadline_duties (org_id, site_id, incident_id, rule_key) VALUES ($1, $2, $3, $4)`,
		f.ownerMe.OrgID, f.siteA, inc.ID, gar82); err != nil {
		t.Fatal(err)
	}

	notice := live(t, inc, gar8b).ID
	call := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		call <- do(t, f.router, http.MethodPut, satisfactionPath(f.siteA, notice, uuid.NewString()), f.supervisor,
			map[string]any{"satisfied_at": rfc(occurred.Add(time.Hour)), "method": "phone"})
	}()
	waitForLock(t, "INSERT INTO deadline_duties")

	amend := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		amend <- do(t, f.router, http.MethodPut, versionPath(f.siteA, inc.ID, 2), f.supervisor, with(v1, "amend_reason", "Location added"))
	}()
	waitForLock(t, "FOR NO KEY UPDATE")
	if err := hold.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	wantStatus(t, <-call, http.StatusCreated)
	rec := <-amend
	wantStatus(t, rec, http.StatusCreated)
	v2 := decodeIncident(t, rec)
	death := live(t, v2, gar82)
	if death.ComputedInVersion != 1 || !slices.Contains(v2.Changes.Carried, death.ID) {
		t.Errorf("death notice %+v; version 2's changes %+v", death, v2.Changes)
	}
}

// The other order: a satisfaction is in progress (stopped by a lock on its
// duty) when a supervisor's downgrade arrives. The downgrade must wait and see
// the call as made, so withdrawing the met notice is allowed; reading the
// duty before the satisfaction committed answered 403.
func TestAmendmentWaitsForSatisfaction(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFixture(t, "incidentwaits")
	occurred := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	inc := submitNew(t, f, f.supervisor, versionBody(occurred, "major_incident"))
	notice := live(t, inc, gar8b)

	hold := holdOpen(t, f)
	if _, err := hold.Exec(ctx, `SELECT 1 FROM deadline_duties WHERE id = $1 FOR UPDATE`, notice.DutyID); err != nil {
		t.Fatal(err)
	}

	call := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		call <- do(t, f.router, http.MethodPut, satisfactionPath(f.siteA, notice.ID, uuid.NewString()), f.supervisor,
			map[string]any{"satisfied_at": rfc(occurred.Add(10 * time.Minute)), "method": "phone"})
	}()
	waitForLock(t, "INSERT INTO deadline_satisfactions")

	down := with(versionBody(occurred, "near_miss"), "amend_reason", "Nobody was near the crane")
	amend := make(chan *httptest.ResponseRecorder, 1)
	go func() { amend <- do(t, f.router, http.MethodPut, versionPath(f.siteA, inc.ID, 2), f.supervisor, down) }()
	waitForLock(t, "FOR NO KEY UPDATE")
	if err := hold.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	wantStatus(t, <-call, http.StatusCreated)
	rec := <-amend
	wantStatus(t, rec, http.StatusCreated)
	if got := liveRules(decodeIncident(t, rec)); len(got) != 0 {
		t.Errorf("live clocks %v after the downgrade, want none", got)
	}
}
