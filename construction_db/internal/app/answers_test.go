package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"construction_db/internal/core/checklists"
	"construction_db/internal/platform/database"
)

func itemPath(site, inspection, key string) string {
	return inspectionPath(site, inspection) + "/items/" + key
}

// answerBody answers at a fixed device time; pass "" severity for none.
func answerBody(result, severity, at string, evidence ...string) map[string]any {
	b := map[string]any{"result": result, "client_answered_at": at, "evidence_ids": append([]string{}, evidence...)}
	if severity != "" {
		b["severity"] = severity
	}
	return b
}

const (
	t1 = "2026-11-14T08:00:00.000001Z"
	t2 = "2026-11-14T08:05:00Z"
)

func decodeItem(t *testing.T, rec *httptest.ResponseRecorder) checklists.InspectionItem {
	t.Helper()
	var it checklists.InspectionItem
	if err := json.Unmarshal(rec.Body.Bytes(), &it); err != nil {
		t.Fatalf("decode item: %v (body: %s)", err, rec.Body.String())
	}
	return it
}

func photoIDs(it checklists.InspectionItem) []string {
	ids := []string{}
	for _, e := range it.Evidence {
		ids = append(ids, e.EvidenceID)
	}
	slices.Sort(ids)
	return ids
}

// answerFixture: a supervisor's inspection on site A, with verified photos.
type answerFixture struct {
	evidenceFixture
	tmpl       checklists.ChecklistTemplate
	inspection string
	key, key2  string
	photo      string // verified, on site A
	photo2     string // verified, on site A
}

func newAnswerFixture(t *testing.T, name string) answerFixture {
	t.Helper()
	f := newEvidenceFixture(t, name)
	tmpl := decodeCatalog(t, f.router, f.siteA, f.owner).Checklists[0]
	a := answerFixture{evidenceFixture: f, tmpl: tmpl, key: tmpl.Items[0].Key, key2: tmpl.Items[1].Key}
	a.inspection = startInspection(t, f.router, f.supervisor, f.siteA, tmpl)
	a.photo, _ = verifiedJPEG(t, f, f.supervisor, f.siteA)
	a.photo2, _ = verifiedJPEG(t, f, f.supervisor, f.siteA)
	return a
}

func TestAnswerItem(t *testing.T) {
	a := newAnswerFixture(t, "answer")
	path := itemPath(a.siteA, a.inspection, a.key)

	t.Run("a pass with no photo", func(t *testing.T) {
		rec := do(t, a.router, http.MethodPut, itemPath(a.siteA, a.inspection, a.key2), a.supervisor, answerBody("pass", "", t1))
		wantStatus(t, rec, http.StatusOK)
		it := decodeItem(t, rec)
		if it.Result == nil || *it.Result != "pass" || it.Severity != nil || it.AnsweredAt == nil || it.ClientAnsweredAt == nil ||
			it.Evidence == nil || len(it.Evidence) != 0 {
			t.Errorf("item %+v, want pass, no severity, answered, evidence []", it)
		}
	})

	t.Run("a fail with a severity, a comment and photos", func(t *testing.T) {
		body := with(answerBody("fail", "high", t1, a.photo, a.photo2), "comment", "No shoring on the north face")
		rec := do(t, a.router, http.MethodPut, path, a.supervisor, body)
		wantStatus(t, rec, http.StatusOK)
		it := decodeItem(t, rec)
		want := []string{a.photo, a.photo2}
		slices.Sort(want)
		if *it.Result != "fail" || *it.Severity != "high" || *it.Comment != "No shoring on the north face" || !slices.Equal(photoIDs(it), want) {
			t.Errorf("item %+v, want fail/high with comment and both photos", it)
		}
		// The photos were captured on 6 Nov, before the inspection started (14 Nov).
		for _, e := range it.Evidence {
			if !e.CapturedBeforeStart || e.ClientCapturedAt.IsZero() || e.AttachedAt.IsZero() {
				t.Errorf("photo %+v, want captured before the start, with times", e)
			}
		}
	})

	t.Run("get shows the answer and its photos", func(t *testing.T) {
		in := decodeInspection(t, do(t, a.router, http.MethodGet, inspectionPath(a.siteA, a.inspection), a.owner, nil))
		if it := in.Items[0]; it.Result == nil || *it.Result != "fail" || len(it.Evidence) != 2 {
			t.Errorf("first item %+v, want the fail with two photos", it)
		}
		summary := listInspections(t, a.router, a.owner, a.siteA, "")[0]
		if summary.AnsweredCount != 2 || summary.FailedCount != 1 {
			t.Errorf("summary counts %d answered, %d failed; want 2 and 1", summary.AnsweredCount, summary.FailedCount)
		}
	})

	t.Run("an exact repeat is 200 and rewrites nothing", func(t *testing.T) {
		before := decodeItem(t, do(t, a.router, http.MethodPut, path, a.supervisor,
			with(answerBody("fail", "high", t1, a.photo2, a.photo), "comment", "No shoring on the north face")))
		after := decodeItem(t, do(t, a.router, http.MethodPut, path, a.supervisor,
			with(answerBody("fail", "high", t1, a.photo, a.photo2), "comment", "No shoring on the north face")))
		if !before.AnsweredAt.Equal(*after.AnsweredAt) {
			t.Errorf("answered_at moved on a repeat: %s then %s", before.AnsweredAt, after.AnsweredAt)
		}
	})

	stale := []struct {
		name, msg string
		body      map[string]any
	}{
		{"an older answer", "a newer answer is already stored", answerBody("pass", "", "2026-11-14T07:00:00Z")},
		{"the same time with another result", "a different answer is already stored for this time", answerBody("pass", "", t1)},
		{"the same time with another photo set", "a different answer is already stored for this time",
			with(answerBody("fail", "high", t1, a.photo), "comment", "No shoring on the north face")},
		{"the same time with another comment", "a different answer is already stored for this time",
			with(answerBody("fail", "high", t1, a.photo, a.photo2), "comment", "Fixed")},
	}
	for _, tt := range stale {
		t.Run(tt.name+" is 409 with the stored answer", func(t *testing.T) {
			rec := do(t, a.router, http.MethodPut, path, a.supervisor, tt.body)
			var body struct {
				Error  string                    `json:"error"`
				Stored checklists.InspectionItem `json:"stored"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v (body: %s)", err, rec.Body.String())
			}
			if rec.Code != http.StatusConflict || body.Error != tt.msg || body.Stored.Key != a.key ||
				body.Stored.Result == nil || *body.Stored.Result != "fail" || len(body.Stored.Evidence) != 2 {
				t.Errorf("got %d %s, want 409 %q with the stored fail", rec.Code, rec.Body.String(), tt.msg)
			}
		})
	}

	t.Run("a newer answer replaces the photos", func(t *testing.T) {
		rec := do(t, a.router, http.MethodPut, path, a.supervisor, answerBody("fail", "critical", t2, a.photo))
		wantStatus(t, rec, http.StatusOK)
		if it := decodeItem(t, rec); *it.Severity != "critical" || it.Comment != nil || !slices.Equal(photoIDs(it), []string{a.photo}) {
			t.Errorf("item %+v, want critical, no comment, one photo", it)
		}
		if n := countRows(t, `SELECT count(*) FROM inspection_response_evidence WHERE inspection_id = $1 AND item_key = $2`, a.inspection, a.key); n != 1 {
			t.Errorf("%d links stored, want 1", n)
		}
	})

	t.Run("a photo captured after the start isn't flagged", func(t *testing.T) {
		early := uuid.NewString()
		wantStatus(t, do(t, a.router, http.MethodPut, inspectionPath(a.siteA, early), a.supervisor,
			with(startBody(a.tmpl), "client_started_at", "2026-11-01T00:00:00Z")), http.StatusCreated)
		it := decodeItem(t, do(t, a.router, http.MethodPut, itemPath(a.siteA, early, a.key), a.supervisor, answerBody("fail", "low", t1, a.photo)))
		if len(it.Evidence) != 1 || it.Evidence[0].CapturedBeforeStart {
			t.Errorf("photo %+v, want captured_before_start false", it.Evidence)
		}
	})
}

func TestAnswerPhotos(t *testing.T) {
	a := newAnswerFixture(t, "answerphotos")
	path := itemPath(a.siteA, a.inspection, a.key)

	pending := declareAndUpload(t, a.router, a.supervisor, a.siteA, testImage(t, "jpeg", 12, 12), "image/jpeg")
	rejected := uuid.NewString()
	wantStatus(t, do(t, a.router, http.MethodPut, evidencePath(a.siteA, rejected), a.supervisor, declareBody("rejected")), http.StatusCreated)
	execAsTenant(t, a.ownerMe.OrgID, 1, `UPDATE evidence SET status = 'rejected', rejected_reason = 'test' WHERE id = $1`, rejected)
	onB, _ := verifiedJPEG(t, a.evidenceFixture, a.owner, a.siteB)
	unknown := uuid.NewString()

	refused := []struct {
		name   string
		photos []string
		status int
		msg    string
		ids    []string
	}{
		{"an unknown photo", []string{a.photo, unknown}, http.StatusBadRequest, "evidence not found on this site", []string{unknown}},
		{"a photo from another site", []string{onB}, http.StatusBadRequest, "evidence not found on this site", []string{onB}},
		{"a rejected photo", []string{rejected}, http.StatusBadRequest, "evidence was rejected", []string{rejected}},
		{"a photo still uploading", []string{a.photo, pending}, http.StatusConflict, "evidence not verified yet", []string{pending}},
	}
	for _, tt := range refused {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, a.router, http.MethodPut, path, a.supervisor, answerBody("fail", "high", t1, tt.photos...))
			var body struct {
				Error       string   `json:"error"`
				EvidenceIDs []string `json:"evidence_ids"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v (body: %s)", err, rec.Body.String())
			}
			if rec.Code != tt.status || body.Error != tt.msg || !slices.Equal(body.EvidenceIDs, tt.ids) {
				t.Errorf("got %d %s, want %d %q with %v", rec.Code, rec.Body.String(), tt.status, tt.msg, tt.ids)
			}
			if n := countRows(t, `SELECT count(*) FROM inspection_responses WHERE inspection_id = $1 AND result IS NOT NULL`, a.inspection); n != 0 {
				t.Errorf("%d answers stored after a refused one", n)
			}
		})
	}

	t.Run("an upper-case id is the same photo", func(t *testing.T) {
		rec := do(t, a.router, http.MethodPut, path, a.supervisor, answerBody("fail", "high", t1, strings.ToUpper(a.photo)))
		wantStatus(t, rec, http.StatusOK)
		if ids := photoIDs(decodeItem(t, rec)); !slices.Equal(ids, []string{a.photo}) {
			t.Errorf("photos %v, want %s", ids, a.photo)
		}
	})
}

func TestAnswerValidation(t *testing.T) {
	a := newAnswerFixture(t, "answervalid")
	path := itemPath(a.siteA, a.inspection, a.key)
	twentyOne := make([]string, 21)
	for i := range twentyOne {
		twentyOne[i] = uuid.NewString()
	}

	bad := []struct {
		name string
		body map[string]any
		msg  string // "" for a validator message
	}{
		{"a fail without a severity", answerBody("fail", "", t1), "severity is required for a fail"},
		{"a pass with a severity", answerBody("pass", "high", t1), "severity is only given for a fail"},
		{"an n/a with a severity", answerBody("na", "low", t1), "severity is only given for a fail"},
		{"an unknown severity", answerBody("fail", "urgent", t1), "severity must be low, medium, high or critical"},
		{"a comment over 2000 characters", with(answerBody("pass", "", t1), "comment", strings.Repeat("é", 2001)), "comment must be at most 2000 characters"},
		{"a malformed photo id", answerBody("pass", "", t1, "not-a-uuid"), "evidence_ids must be UUIDs"},
		{"a photo twice", answerBody("pass", "", t1, a.photo, strings.ToUpper(a.photo)), "evidence_ids must not repeat"},
		{"21 photos", answerBody("pass", "", t1, twentyOne...), "at most 20 photos per item"},
		{"an unknown result", answerBody("maybe", "", t1), ""},
		{"no result", with(answerBody("pass", "", t1), "result", nil), ""},
		{"no time", with(answerBody("pass", "", t1), "client_answered_at", nil), ""},
		{"a time 10 minutes ahead of the server", answerBody("pass", "", ahead(10*time.Minute)), "client_answered_at is in the future; check the device clock"},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, a.router, http.MethodPut, path, a.supervisor, tt.body)
			if tt.msg != "" {
				wantError(t, rec, http.StatusBadRequest, tt.msg)
				return
			}
			wantStatus(t, rec, http.StatusBadRequest)
		})
	}
	t.Run("a comment of exactly 2000 characters", func(t *testing.T) {
		wantStatus(t, do(t, a.router, http.MethodPut, path, a.supervisor, with(answerBody("pass", "", t1), "comment", strings.Repeat("é", 2000))), http.StatusOK)
	})
	// Ordinary clock drift is fine: the limit is 5 minutes.
	t.Run("a time 4 minutes ahead of the server", func(t *testing.T) {
		wantStatus(t, do(t, a.router, http.MethodPut, path, a.supervisor, answerBody("na", "", ahead(4*time.Minute))), http.StatusOK)
	})
}

func TestAnswerAccess(t *testing.T) {
	a := newAnswerFixture(t, "answeraccess")
	onB := startInspection(t, a.router, a.owner, a.siteB, a.tmpl)
	completed := startInspection(t, a.router, a.supervisor, a.siteA, a.tmpl)
	completeBySQL(t, a.ownerMe.OrgID, completed, len(a.tmpl.Items))
	body := answerBody("pass", "", t1)

	tests := []struct {
		name, token, path string
		want              int
		msg               string
	}{
		{"the owner on the supervisor's inspection", a.owner, itemPath(a.siteA, a.inspection, a.key), http.StatusForbidden,
			"only the inspector who started this inspection can change it"},
		{"the supervisor on another site", a.supervisor, itemPath(a.siteB, onB, a.key), http.StatusNotFound, "site not found"},
		{"a bookkeeper", a.bookkeeper, itemPath(a.siteA, a.inspection, a.key), http.StatusForbidden, ""},
		{"another org", a.other, itemPath(a.siteA, a.inspection, a.key), http.StatusNotFound, "site not found"},
		{"an unknown inspection", a.supervisor, itemPath(a.siteA, uuid.NewString(), a.key), http.StatusNotFound, "inspection not found"},
		{"a malformed inspection id", a.supervisor, itemPath(a.siteA, "not-a-uuid", a.key), http.StatusNotFound, "inspection not found"},
		{"an unknown item", a.supervisor, itemPath(a.siteA, a.inspection, "no_such_item"), http.StatusNotFound, "item not found"},
		{"a completed inspection", a.supervisor, itemPath(a.siteA, completed, a.key), http.StatusConflict,
			"inspection is completed; start a new inspection to correct it"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, a.router, http.MethodPut, tt.path, tt.token, body)
			if tt.msg != "" {
				wantError(t, rec, tt.want, tt.msg)
				return
			}
			wantStatus(t, rec, tt.want)
		})
	}

	t.Run("an archived site still takes answers", func(t *testing.T) {
		wantStatus(t, do(t, a.router, http.MethodDelete, "/v1/sites/"+a.siteA, a.owner, nil), http.StatusNoContent)
		wantStatus(t, do(t, a.router, http.MethodPut, itemPath(a.siteA, a.inspection, a.key), a.supervisor, body), http.StatusOK)
	})
}

// An answer that arrives while a completion is open waits on the inspection's
// row lock, then sees it completed and answers 409, not 500.
func TestAnswerWaitsForCompletion(t *testing.T) {
	ctx := context.Background()
	a := newAnswerFixture(t, "answerrace")
	execAsTenant(t, a.ownerMe.OrgID, int64(len(a.tmpl.Items)), `UPDATE inspection_responses
		SET result = 'pass', client_answered_at = '2026-11-14T07:00:00Z', answered_at = now() WHERE inspection_id = $1`, a.inspection)

	completing, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer completing.Rollback(ctx)
	if err := database.SetTenant(ctx, completing, database.Tenant{OrgID: a.ownerMe.OrgID}); err != nil {
		t.Fatalf("set tenant: %v", err)
	}
	if _, err := completing.Exec(ctx, `UPDATE inspections SET status = 'completed', completed_at = now(), client_completed_at = now() WHERE id = $1`, a.inspection); err != nil {
		t.Fatalf("complete: %v", err)
	}

	answered := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		answered <- do(t, a.router, http.MethodPut, itemPath(a.siteA, a.inspection, a.key), a.supervisor, answerBody("na", "", t1))
	}()
	waitForLock(t, "FOR UPDATE")
	if err := completing.Commit(ctx); err != nil {
		t.Fatalf("commit completion: %v", err)
	}
	wantError(t, <-answered, http.StatusConflict, "inspection is completed; start a new inspection to correct it")
}

// ahead is a device time d ahead of the real clock, as a phone with a fast
// clock would send.
func ahead(d time.Duration) string { return time.Now().UTC().Add(d).Format(time.RFC3339Nano) }
