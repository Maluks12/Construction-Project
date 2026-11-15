package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"construction_db/internal/core/checklists"
	"construction_db/internal/platform/database"
)

func completePathFor(site, inspection string) string {
	return inspectionPath(site, inspection) + "/complete"
}

func completeBody() map[string]any {
	return map[string]any{"client_completed_at": "2026-11-17T09:30:00.000001Z"}
}

// answerAllBySQL answers every item "pass" at an early device time, so an
// answer sent through the API afterwards counts as newer.
func answerAllBySQL(t *testing.T, orgID, inspection string, items int) {
	t.Helper()
	execAsTenant(t, orgID, int64(items), `UPDATE inspection_responses
		SET result = 'pass', client_answered_at = '2026-11-14T07:00:00Z', answered_at = now() WHERE inspection_id = $1`, inspection)
}

func decodeKeys(t *testing.T, rec *httptest.ResponseRecorder) (string, []string) {
	t.Helper()
	var body struct {
		Error    string   `json:"error"`
		ItemKeys []string `json:"item_keys"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (body: %s)", err, rec.Body.String())
	}
	return body.Error, body.ItemKeys
}

func TestCompleteInspection(t *testing.T) {
	a := newAnswerFixture(t, "complete")
	keys := []string{a.tmpl.Items[0].Key, a.tmpl.Items[1].Key, a.tmpl.Items[2].Key}

	t.Run("an unanswered item blocks it, named in template order", func(t *testing.T) {
		wantStatus(t, do(t, a.router, http.MethodPut, itemPath(a.siteA, a.inspection, keys[1]), a.supervisor, answerBody("pass", "", t1)), http.StatusOK)
		rec := do(t, a.router, http.MethodPost, completePathFor(a.siteA, a.inspection), a.supervisor, completeBody())
		msg, got := decodeKeys(t, rec)
		var want []string
		for _, it := range a.tmpl.Items {
			if it.Key != keys[1] {
				want = append(want, it.Key)
			}
		}
		if rec.Code != http.StatusConflict || msg != "items not answered" || !slices.Equal(got, want) {
			t.Errorf("got %d %q %v, want 409 items not answered %v", rec.Code, msg, got, want)
		}
	})

	answerAllBySQL(t, a.ownerMe.OrgID, a.inspection, len(a.tmpl.Items))
	wantStatus(t, do(t, a.router, http.MethodPut, itemPath(a.siteA, a.inspection, keys[2]), a.supervisor, answerBody("fail", "low", t1)), http.StatusOK)
	wantStatus(t, do(t, a.router, http.MethodPut, itemPath(a.siteA, a.inspection, keys[0]), a.supervisor, answerBody("fail", "critical", t1)), http.StatusOK)

	t.Run("a fail without a photo blocks it", func(t *testing.T) {
		rec := do(t, a.router, http.MethodPost, completePathFor(a.siteA, a.inspection), a.supervisor, completeBody())
		msg, got := decodeKeys(t, rec)
		if rec.Code != http.StatusConflict || msg != "a failed item needs a photo" || !slices.Equal(got, []string{keys[0], keys[2]}) {
			t.Errorf("got %d %q %v, want 409 a failed item needs a photo [%s %s]", rec.Code, msg, got, keys[0], keys[2])
		}
	})

	wantStatus(t, do(t, a.router, http.MethodPut, itemPath(a.siteA, a.inspection, keys[2]), a.supervisor, answerBody("fail", "low", t2, a.photo)), http.StatusOK)
	wantStatus(t, do(t, a.router, http.MethodPut, itemPath(a.siteA, a.inspection, keys[0]), a.supervisor, answerBody("fail", "critical", t2, a.photo2)), http.StatusOK)

	t.Run("someone else can't complete it", func(t *testing.T) {
		wantError(t, do(t, a.router, http.MethodPost, completePathFor(a.siteA, a.inspection), a.owner, completeBody()),
			http.StatusForbidden, "only the inspector who started this inspection can change it")
	})

	var completed string
	t.Run("completing raises one finding per fail, in template order", func(t *testing.T) {
		rec := do(t, a.router, http.MethodPost, completePathFor(a.siteA, a.inspection), a.supervisor, completeBody())
		wantStatus(t, rec, http.StatusCreated)
		completed = rec.Body.String()
		in := decodeInspection(t, rec)
		if in.Status != checklists.StatusCompleted || in.CompletedAt == nil || in.ClientCompletedAt == nil ||
			in.ClientCompletedAt.Format("2006-01-02T15:04:05.000000Z") != "2026-11-17T09:30:00.000001Z" {
			t.Errorf("inspection %+v, want completed with both completion times", in.InspectionHeader)
		}
		if len(in.Findings) != 2 || in.Findings[0].ItemKey != keys[0] || in.Findings[0].Severity != "critical" ||
			in.Findings[1].ItemKey != keys[2] || in.Findings[1].Severity != "low" || in.Findings[0].RaisedAt.IsZero() {
			t.Errorf("findings %+v, want %s critical then %s low", in.Findings, keys[0], keys[2])
		}
	})

	t.Run("completing again is 200 with the same inspection", func(t *testing.T) {
		rec := do(t, a.router, http.MethodPost, completePathFor(a.siteA, a.inspection), a.supervisor,
			map[string]any{"client_completed_at": "2026-11-18T00:00:00Z"})
		wantStatus(t, rec, http.StatusOK)
		if rec.Body.String() != completed {
			t.Errorf("repeat = %s\nfirst = %s", rec.Body.String(), completed)
		}
	})

	t.Run("get shows the findings", func(t *testing.T) {
		if in := decodeInspection(t, do(t, a.router, http.MethodGet, inspectionPath(a.siteA, a.inspection), a.owner, nil)); len(in.Findings) != 2 {
			t.Errorf("findings %+v, want 2", in.Findings)
		}
	})

	t.Run("an in-progress inspection has findings []", func(t *testing.T) {
		rec := do(t, a.router, http.MethodGet, inspectionPath(a.siteA, startInspection(t, a.router, a.supervisor, a.siteA, a.tmpl)), a.owner, nil)
		if !strings.Contains(rec.Body.String(), `"findings":[]`) {
			t.Errorf("body %s, want findings []", rec.Body.String())
		}
	})

	t.Run("validation and scope", func(t *testing.T) {
		wantStatus(t, do(t, a.router, http.MethodPost, completePathFor(a.siteA, a.inspection), a.supervisor, map[string]any{}), http.StatusBadRequest)
		wantError(t, do(t, a.router, http.MethodPost, completePathFor(a.siteA, uuid.NewString()), a.supervisor, completeBody()), http.StatusNotFound, "inspection not found")
		wantStatus(t, do(t, a.router, http.MethodPost, completePathFor(a.siteA, a.inspection), a.bookkeeper, completeBody()), http.StatusForbidden)
		wantError(t, do(t, a.router, http.MethodPost, completePathFor(a.siteA, a.inspection), a.other, completeBody()), http.StatusNotFound, "site not found")
	})

	t.Run("an archived site still lets it complete", func(t *testing.T) {
		pending := startInspection(t, a.router, a.supervisor, a.siteA, a.tmpl)
		answerAllBySQL(t, a.ownerMe.OrgID, pending, len(a.tmpl.Items))
		wantStatus(t, do(t, a.router, http.MethodDelete, "/v1/sites/"+a.siteA, a.owner, nil), http.StatusNoContent)
		wantStatus(t, do(t, a.router, http.MethodPost, completePathFor(a.siteA, pending), a.supervisor, completeBody()), http.StatusCreated)
	})
}

// Two completions of one inspection: the second waits on the row lock, then
// finds it completed and answers 200 with it, not an error.
func TestCompleteWaitsForCompletion(t *testing.T) {
	ctx := context.Background()
	a := newAnswerFixture(t, "completerace")
	answerAllBySQL(t, a.ownerMe.OrgID, a.inspection, len(a.tmpl.Items))

	first, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer first.Rollback(ctx)
	if err := database.SetTenant(ctx, first, database.Tenant{OrgID: a.ownerMe.OrgID}); err != nil {
		t.Fatalf("set tenant: %v", err)
	}
	if _, err := first.Exec(ctx, `UPDATE inspections SET status = 'completed', completed_at = now(), client_completed_at = now() WHERE id = $1`, a.inspection); err != nil {
		t.Fatalf("first completion: %v", err)
	}

	second := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		second <- do(t, a.router, http.MethodPost, completePathFor(a.siteA, a.inspection), a.supervisor, completeBody())
	}()
	waitForLock(t, "FOR UPDATE")
	if err := first.Commit(ctx); err != nil {
		t.Fatalf("commit first: %v", err)
	}
	rec := <-second
	wantStatus(t, rec, http.StatusOK)
	if in := decodeInspection(t, rec); in.Status != checklists.StatusCompleted {
		t.Errorf("status %s, want completed", in.Status)
	}
}

func listFindings(t *testing.T, router http.Handler, token, site, query string) []checklists.Finding {
	t.Helper()
	rec := do(t, router, http.MethodGet, "/v1/sites/"+site+"/findings"+query, token, nil)
	wantStatus(t, rec, http.StatusOK)
	var body struct {
		Findings []checklists.Finding `json:"findings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode findings: %v (body: %s)", err, rec.Body.String())
	}
	return body.Findings
}

func findingKeys(fs []checklists.Finding) string {
	out := []string{}
	for _, f := range fs {
		out = append(out, f.InspectionID[:8]+"/"+f.ItemKey+"/"+f.Severity)
	}
	return strings.Join(out, ",")
}

func TestSiteFindings(t *testing.T) {
	a := newAnswerFixture(t, "findings")
	org := a.ownerMe.OrgID
	k0, k1 := a.tmpl.Items[0].Key, a.tmpl.Items[1].Key

	// complete one inspection with fails on the given items, oldest first.
	complete := func(fails map[string]string) string {
		id := startInspection(t, a.router, a.supervisor, a.siteA, a.tmpl)
		answerAllBySQL(t, org, id, len(a.tmpl.Items))
		for key, sev := range fails {
			wantStatus(t, do(t, a.router, http.MethodPut, itemPath(a.siteA, id, key), a.supervisor, answerBody("fail", sev, t1, a.photo)), http.StatusOK)
		}
		wantStatus(t, do(t, a.router, http.MethodPost, completePathFor(a.siteA, id), a.supervisor, completeBody()), http.StatusCreated)
		return id
	}
	older := complete(map[string]string{k0: "high"})
	newer := complete(map[string]string{k0: "medium", k1: "critical"})
	complete(nil) // no fails, no findings

	t.Run("newest first, then template order, with context", func(t *testing.T) {
		got := listFindings(t, a.router, a.owner, a.siteA, "")
		want := strings.Join([]string{newer[:8] + "/" + k0 + "/medium", newer[:8] + "/" + k1 + "/critical", older[:8] + "/" + k0 + "/high"}, ",")
		if findingKeys(got) != want {
			t.Fatalf("findings %s, want %s", findingKeys(got), want)
		}
		f := got[0]
		if f.SiteID != a.siteA || f.TemplateKey != a.tmpl.Key || f.TemplateVersion != a.tmpl.Version || f.TemplateTitle != a.tmpl.Title ||
			f.Prompt != a.tmpl.Items[0].Prompt || f.RaisedAt.IsZero() {
			t.Errorf("finding %+v, want the template and prompt", f)
		}
	})

	queries := map[string]struct{ query, want string }{
		"one severity": {"?severity=critical", newer[:8] + "/" + k1 + "/critical"},
		"limit":        {"?limit=1", newer[:8] + "/" + k0 + "/medium"},
	}
	for name, q := range queries {
		t.Run(name, func(t *testing.T) {
			if got := findingKeys(listFindings(t, a.router, a.owner, a.siteA, q.query)); got != q.want {
				t.Errorf("findings %s, want %s", got, q.want)
			}
		})
	}

	t.Run("a site with none lists [] not null", func(t *testing.T) {
		rec := do(t, a.router, http.MethodGet, "/v1/sites/"+a.siteB+"/findings", a.owner, nil)
		wantStatus(t, rec, http.StatusOK)
		if rec.Body.String() != `{"findings":[]}` {
			t.Errorf("body %s", rec.Body.String())
		}
	})

	for query, msg := range map[string]string{
		"?severity=urgent": "severity must be low, medium, high or critical",
		"?limit=0":         "limit must be 1-200",
		"?limit=many":      "limit must be 1-200",
	} {
		t.Run("bad query "+query, func(t *testing.T) {
			wantError(t, do(t, a.router, http.MethodGet, "/v1/sites/"+a.siteA+"/findings"+query, a.owner, nil), http.StatusBadRequest, msg)
		})
	}

	access := []struct {
		name, token, site string
		want              int
	}{
		{"supervisor on own site", a.supervisor, a.siteA, http.StatusOK},
		{"supervisor elsewhere", a.supervisor, a.siteB, http.StatusNotFound},
		{"bookkeeper", a.bookkeeper, a.siteA, http.StatusForbidden},
		{"other org", a.other, a.siteA, http.StatusNotFound},
	}
	for _, tt := range access {
		t.Run(tt.name, func(t *testing.T) {
			wantStatus(t, do(t, a.router, http.MethodGet, "/v1/sites/"+tt.site+"/findings", tt.token, nil), tt.want)
		})
	}

	t.Run("an archived site still lists its findings", func(t *testing.T) {
		wantStatus(t, do(t, a.router, http.MethodDelete, "/v1/sites/"+a.siteA, a.owner, nil), http.StatusNoContent)
		if got := listFindings(t, a.router, a.owner, a.siteA, ""); len(got) != 3 {
			t.Errorf("%d findings, want 3", len(got))
		}
	})
}
