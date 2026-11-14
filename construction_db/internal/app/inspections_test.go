package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"

	"construction_db/internal/core/checklists"
	"construction_db/internal/platform/database"
	"construction_db/internal/regimes"
)

func inspectionPath(site, id string) string { return "/v1/sites/" + site + "/inspections/" + id }

// startBody starts an inspection on tmpl as the catalogue offered it.
func startBody(tmpl checklists.ChecklistTemplate) map[string]any {
	return map[string]any{
		"template_key":      tmpl.Key,
		"template_version":  tmpl.Version,
		"template_sha256":   tmpl.SHA256,
		"client_started_at": "2026-11-14T06:30:00.123456Z",
	}
}

func decodeInspection(t *testing.T, rec *httptest.ResponseRecorder) checklists.Inspection {
	t.Helper()
	var in checklists.Inspection
	if err := json.Unmarshal(rec.Body.Bytes(), &in); err != nil {
		t.Fatalf("decode inspection: %v (body: %s)", err, rec.Body.String())
	}
	return in
}

func wantError(t *testing.T, rec *httptest.ResponseRecorder, status int, msg string) {
	t.Helper()
	if want := `{"error":"` + msg + `"}`; rec.Code != status || rec.Body.String() != want {
		t.Errorf("got %d %s, want %d %s", rec.Code, rec.Body.String(), status, want)
	}
}

// startInspection starts a new inspection on tmpl and returns its id.
func startInspection(t *testing.T, router http.Handler, token, site string, tmpl checklists.ChecklistTemplate) string {
	t.Helper()
	id := uuid.NewString()
	wantStatus(t, do(t, router, http.MethodPut, inspectionPath(site, id), token, startBody(tmpl)), http.StatusCreated)
	return id
}

// completeBySQL answers every item "pass" and completes the inspection with
// fixtures, until the answer and complete endpoints exist (7.6, 7.7).
func completeBySQL(t *testing.T, orgID, id string, items int) {
	t.Helper()
	execAsTenant(t, orgID, int64(items), `UPDATE inspection_responses SET result = 'pass', client_answered_at = now(), answered_at = now() WHERE inspection_id = $1`, id)
	execAsTenant(t, orgID, 1, `UPDATE inspections SET status = 'completed', completed_at = now(), client_completed_at = now() WHERE id = $1`, id)
}

func countRows(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := testAdmin.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestInspectionStart(t *testing.T) {
	f := newEvidenceFixture(t, "start")
	catalog := decodeCatalog(t, f.router, f.siteA, f.owner)
	tmpl, other := catalog.Checklists[0], catalog.Checklists[1]
	id := uuid.NewString()
	// Nanoseconds: the server keeps microseconds.
	body := with(startBody(tmpl), "client_started_at", "2026-11-14T06:30:00.123456789Z")

	rec := do(t, f.router, http.MethodPut, inspectionPath(f.siteA, id), f.owner, body)
	wantStatus(t, rec, http.StatusCreated)
	created := rec.Body.String()
	in := decodeInspection(t, rec)

	t.Run("records the template version and who started it", func(t *testing.T) {
		if in.ID != id || in.SiteID != f.siteA || in.Status != checklists.StatusInProgress || in.StartedBy != f.ownerMe.UserID {
			t.Errorf("inspection %+v, want %s on %s, in progress, started by the owner", in, id, f.siteA)
		}
		if in.TemplateKey != tmpl.Key || in.TemplateVersion != tmpl.Version || in.TemplateSHA256 != tmpl.SHA256 ||
			in.TemplateTitle != tmpl.Title || in.TemplateSourceRef != tmpl.SourceRef || in.ItemCount != len(tmpl.Items) {
			t.Errorf("template fields %+v, want a copy of %s version %d", in, tmpl.Key, tmpl.Version)
		}
		if got := in.ClientStartedAt.Format("2006-01-02T15:04:05.000000000Z"); got != "2026-11-14T06:30:00.123456000Z" {
			t.Errorf("client_started_at %s, want it truncated to microseconds", got)
		}
		if in.StartedAt.IsZero() || in.DeviceID != nil || in.CorrectsID != nil || in.CompletedAt != nil || in.ClientCompletedAt != nil {
			t.Errorf("inspection %+v, want a server start time and no device, correction or completion", in)
		}
	})

	t.Run("has one unanswered item per template item, in order", func(t *testing.T) {
		if len(in.Items) != len(tmpl.Items) {
			t.Fatalf("%d items, want %d", len(in.Items), len(tmpl.Items))
		}
		for i, want := range tmpl.Items {
			got := in.Items[i]
			if got.Key != want.Key || got.Position != i+1 || got.Prompt != want.Prompt || got.ResponseType != want.ResponseType ||
				got.SourceRef == nil || *got.SourceRef != want.SourceRef {
				t.Errorf("item %d = %+v, want a copy of %+v at position %d", i, got, want, i+1)
			}
			if got.Result != nil || got.Severity != nil || got.Comment != nil || got.AnsweredAt != nil || got.ClientAnsweredAt != nil {
				t.Errorf("item %s is answered: %+v", got.Key, got)
			}
		}
	})

	t.Run("get returns the same inspection", func(t *testing.T) {
		rec := do(t, f.router, http.MethodGet, inspectionPath(f.siteA, id), f.owner, nil)
		wantStatus(t, rec, http.StatusOK)
		if rec.Body.String() != created {
			t.Errorf("get = %s\nstart = %s", rec.Body.String(), created)
		}
	})

	t.Run("the same start again is 200 with the same inspection", func(t *testing.T) {
		rec := do(t, f.router, http.MethodPut, inspectionPath(f.siteA, id), f.owner, body)
		wantStatus(t, rec, http.StatusOK)
		if rec.Body.String() != created {
			t.Errorf("repeat = %s\nfirst = %s", rec.Body.String(), created)
		}
		if n := countRows(t, `SELECT count(*) FROM inspection_responses WHERE inspection_id = $1`, id); n != len(tmpl.Items) {
			t.Errorf("%d responses after the repeat, want %d", n, len(tmpl.Items))
		}
	})

	const reused = "inspection id already used with different details"
	conflicts := []struct {
		name, token, site string
		body              map[string]any
	}{
		{"another start time", f.owner, f.siteA, with(body, "client_started_at", "2026-11-14T07:00:00Z")},
		{"another template", f.owner, f.siteA, startBody(other)},
		{"a device id added", f.owner, f.siteA, with(body, "device_id", "pixel-7a")},
		{"another user", f.supervisor, f.siteA, body},
		{"another site", f.owner, f.siteB, body},
	}
	for _, tt := range conflicts {
		t.Run("the id with "+tt.name+" is 409", func(t *testing.T) {
			wantError(t, do(t, f.router, http.MethodPut, inspectionPath(tt.site, id), tt.token, tt.body), http.StatusConflict, reused)
		})
	}
	t.Run("the id in another organisation is 409", func(t *testing.T) {
		theirs := decodeCatalog(t, f.router, f.otherSite, f.other).Checklists[0]
		wantError(t, do(t, f.router, http.MethodPut, inspectionPath(f.otherSite, id), f.other, startBody(theirs)), http.StatusConflict, reused)
	})
}

// A client retrying before its first request finished: one start wins, the
// rest answer as repeats, and only one inspection exists.
func TestInspectionConcurrentStart(t *testing.T) {
	f := newEvidenceFixture(t, "startrace")
	tmpl := decodeCatalog(t, f.router, f.siteA, f.owner).Checklists[0]
	id := uuid.NewString()

	const n = 5
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = do(t, f.router, http.MethodPut, inspectionPath(f.siteA, id), f.owner, startBody(tmpl)).Code
		}()
	}
	wg.Wait()

	created, repeats := 0, 0
	for _, c := range codes {
		switch c {
		case http.StatusCreated:
			created++
		case http.StatusOK:
			repeats++
		}
	}
	if created != 1 || repeats != n-1 {
		t.Errorf("statuses %v, want one 201 and %d 200s", codes, n-1)
	}
	if rows := countRows(t, `SELECT count(*) FROM inspection_responses WHERE inspection_id = $1`, id); rows != len(tmpl.Items) {
		t.Errorf("%d responses, want %d", rows, len(tmpl.Items))
	}
}

// The start names the version the phone showed and the hash the catalogue
// gave for it; the server refuses anything that isn't its own.
func TestInspectionStartChecksTheTemplate(t *testing.T) {
	f := newEvidenceFixture(t, "starttmpl")
	tmpl := decodeCatalog(t, f.router, f.siteA, f.owner).Checklists[0]

	refused := []struct {
		name string
		body map[string]any
		msg  string
	}{
		{"a wrong hash", with(startBody(tmpl), "template_sha256", strings.Repeat("0", 64)), "checklist template does not match"},
		{"a malformed hash", with(startBody(tmpl), "template_sha256", "not-a-hash"), "checklist template does not match"},
		{"an unknown version", with(startBody(tmpl), "template_version", tmpl.Version+1), "unknown checklist template version"},
		{"an unknown key", with(startBody(tmpl), "template_key", "no_such_checklist"), "unknown checklist template version"},
	}
	for _, tt := range refused {
		t.Run(tt.name+" is 400 and stores nothing", func(t *testing.T) {
			id := uuid.NewString()
			wantError(t, do(t, f.router, http.MethodPut, inspectionPath(f.siteA, id), f.owner, tt.body), http.StatusBadRequest, tt.msg)
			wantStatus(t, do(t, f.router, http.MethodGet, inspectionPath(f.siteA, id), f.owner, nil), http.StatusNotFound)
			if n := countRows(t, `SELECT (SELECT count(*) FROM inspections WHERE id = $1) + (SELECT count(*) FROM inspection_responses WHERE inspection_id = $1)`, id); n != 0 {
				t.Errorf("%d rows stored for a refused start", n)
			}
		})
	}

	t.Run("the hash in upper case is accepted and stored in lower case", func(t *testing.T) {
		id := uuid.NewString()
		rec := do(t, f.router, http.MethodPut, inspectionPath(f.siteA, id), f.owner, with(startBody(tmpl), "template_sha256", strings.ToUpper(tmpl.SHA256)))
		wantStatus(t, rec, http.StatusCreated)
		if got := decodeInspection(t, rec).TemplateSHA256; got != tmpl.SHA256 {
			t.Errorf("stored hash %s, want %s", got, tmpl.SHA256)
		}
	})
}

// An inspection started on an older version (offline, before a newer one
// shipped) is still accepted, with that version's wording.
func TestInspectionStartOnAnOlderVersion(t *testing.T) {
	router := newRouter(t)
	token := signup(t, router, "Old Version Co", "owner@old-version-"+uuid.NewString()[:8]+".test")
	owner := me(t, router, token)
	siteA, _ := twoRegimeSites(t, owner.OrgID)
	// Runs before twoRegimeSites' cleanup, which can't delete a site or a
	// regime an inspection points at. In-progress rows can be deleted by a
	// superuser.
	t.Cleanup(func() {
		ctx := context.Background()
		for _, sql := range []string{
			`DELETE FROM inspection_responses WHERE site_id = $1`,
			`DELETE FROM inspections WHERE site_id = $1`,
		} {
			if _, err := testAdmin.Exec(ctx, sql, siteA); err != nil {
				t.Errorf("clean up inspections: %v", err)
			}
		}
	})

	version := func(v int, prompt string) string {
		return `{"key": "walk", "version": ` + string(rune('0'+v)) + `, "title": "Walk", "cadence": "daily", "source_ref": "test",
			"items": [{"key": "item", "prompt": "` + prompt + `", "response_type": "pass_fail_na"}]}`
	}
	fsys := regimeFS("test_regime_a", "test_regime_b")
	fsys["test_regime_a/checklists.json"] = &fstest.MapFile{Data: []byte("[" + version(1, "Old wording?") + "," + version(2, "New wording?") + "]")}
	reg, err := regimes.LoadFS(fsys)
	if err != nil {
		t.Fatalf("LoadFS: %v", err)
	}
	versioned, err := NewRouter(context.Background(), testPool, testSecret, reg,
		fakeEdition{name: "construction", regime: "test_regime_a"}, testStore, testHTTP)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	ds, _ := reg.Get("test_regime_a")
	v1, _ := ds.Checklist("walk", 1)

	latest := decodeCatalog(t, versioned, siteA, token).Checklists
	if len(latest) != 1 || latest[0].Version != 2 {
		t.Fatalf("catalogue %+v, want only version 2", latest)
	}

	t.Run("version 1 with its own hash", func(t *testing.T) {
		body := with(with(startBody(latest[0]), "template_version", 1), "template_sha256", v1.ContentHash())
		rec := do(t, versioned, http.MethodPut, inspectionPath(siteA, uuid.NewString()), token, body)
		wantStatus(t, rec, http.StatusCreated)
		in := decodeInspection(t, rec)
		if in.TemplateVersion != 1 || in.TemplateSHA256 != v1.ContentHash() || len(in.Items) != 1 || in.Items[0].Prompt != "Old wording?" {
			t.Errorf("inspection %+v, want version 1 with its own wording", in)
		}
		if in.Items[0].SourceRef != nil {
			t.Errorf("item source_ref %q, want null when the template has none", *in.Items[0].SourceRef)
		}
	})
	t.Run("version 2 with version 1's hash is refused", func(t *testing.T) {
		body := with(startBody(latest[0]), "template_sha256", v1.ContentHash())
		wantError(t, do(t, versioned, http.MethodPut, inspectionPath(siteA, uuid.NewString()), token, body), http.StatusBadRequest, "checklist template does not match")
	})
}

// Archived sites refuse new inspections but keep their history readable;
// completed sites still take inspections (defects period).
func TestInspectionStartAndSiteStatus(t *testing.T) {
	f := newEvidenceFixture(t, "startstatus")
	tmpl := decodeCatalog(t, f.router, f.siteA, f.owner).Checklists[0]

	t.Run("a completed site accepts a start", func(t *testing.T) {
		execAsTenant(t, f.ownerMe.OrgID, 1, `UPDATE sites SET status = 'completed' WHERE id = $1`, f.siteA)
		startInspection(t, f.router, f.owner, f.siteA, tmpl)
	})

	before := uuid.NewString()
	body := with(startBody(tmpl), "device_id", "pixel-7a")
	wantStatus(t, do(t, f.router, http.MethodPut, inspectionPath(f.siteB, before), f.owner, body), http.StatusCreated)
	wantStatus(t, do(t, f.router, http.MethodDelete, "/v1/sites/"+f.siteB, f.owner, nil), http.StatusNoContent)

	t.Run("an archived site refuses a new start", func(t *testing.T) {
		id := uuid.NewString()
		wantError(t, do(t, f.router, http.MethodPut, inspectionPath(f.siteB, id), f.owner, startBody(tmpl)), http.StatusConflict, "site is archived")
		if n := countRows(t, `SELECT count(*) FROM inspections WHERE id = $1`, id); n != 0 {
			t.Errorf("%d rows stored for a refused start", n)
		}
	})
	t.Run("an inspection started before archiving is still readable", func(t *testing.T) {
		rec := do(t, f.router, http.MethodGet, inspectionPath(f.siteB, before), f.owner, nil)
		wantStatus(t, rec, http.StatusOK)
		if in := decodeInspection(t, rec); in.DeviceID == nil || *in.DeviceID != "pixel-7a" {
			t.Errorf("device_id %v, want pixel-7a", in.DeviceID)
		}
	})
	t.Run("retrying the earlier start is still 200", func(t *testing.T) {
		wantStatus(t, do(t, f.router, http.MethodPut, inspectionPath(f.siteB, before), f.owner, body), http.StatusOK)
	})
}

// A correction is a new inspection that points at the completed one it corrects.
func TestInspectionCorrections(t *testing.T) {
	f := newEvidenceFixture(t, "corrects")
	tmpl := decodeCatalog(t, f.router, f.siteA, f.owner).Checklists[0]
	const bad = "corrects_id must be a completed inspection on this site"
	original := startInspection(t, f.router, f.owner, f.siteA, tmpl)
	correcting := func(id string) map[string]any { return with(startBody(tmpl), "corrects_id", id) }

	t.Run("an inspection still in progress can't be corrected", func(t *testing.T) {
		wantError(t, do(t, f.router, http.MethodPut, inspectionPath(f.siteA, uuid.NewString()), f.owner, correcting(original)), http.StatusBadRequest, bad)
	})

	completeBySQL(t, f.ownerMe.OrgID, original, len(tmpl.Items))
	t.Run("get shows the completion", func(t *testing.T) {
		in := decodeInspection(t, do(t, f.router, http.MethodGet, inspectionPath(f.siteA, original), f.owner, nil))
		if in.Status != checklists.StatusCompleted || in.CompletedAt == nil || in.ClientCompletedAt == nil ||
			in.Items[0].Result == nil || *in.Items[0].Result != checklists.ResultPass || in.Items[0].AnsweredAt == nil {
			t.Errorf("inspection %+v, want completed with answered items", in)
		}
	})

	correction := uuid.NewString()
	t.Run("a completed inspection can", func(t *testing.T) {
		rec := do(t, f.router, http.MethodPut, inspectionPath(f.siteA, correction), f.owner, correcting(original))
		wantStatus(t, rec, http.StatusCreated)
		if in := decodeInspection(t, rec); in.CorrectsID == nil || *in.CorrectsID != original {
			t.Errorf("corrects_id %v, want %s", in.CorrectsID, original)
		}
	})
	t.Run("retrying with the id in upper case is 200", func(t *testing.T) {
		wantStatus(t, do(t, f.router, http.MethodPut, inspectionPath(f.siteA, correction), f.owner, correcting(strings.ToUpper(original))), http.StatusOK)
	})

	refused := map[string]struct{ site, corrects string }{
		"one on another site": {f.siteB, original},
		"an unknown id":       {f.siteA, uuid.NewString()},
		"a malformed id":      {f.siteA, "not-a-uuid"},
	}
	for name, tt := range refused {
		t.Run(name+" can't be corrected", func(t *testing.T) {
			wantError(t, do(t, f.router, http.MethodPut, inspectionPath(tt.site, uuid.NewString()), f.owner, correcting(tt.corrects)), http.StatusBadRequest, bad)
		})
	}
	t.Run("an inspection can't correct itself", func(t *testing.T) {
		id := uuid.NewString()
		wantError(t, do(t, f.router, http.MethodPut, inspectionPath(f.siteA, id), f.owner, correcting(id)), http.StatusBadRequest, bad)
	})
}

func TestInspectionAccess(t *testing.T) {
	f := newEvidenceFixture(t, "inspaccess")
	tmpl := decodeCatalog(t, f.router, f.siteA, f.owner).Checklists[0]
	body := startBody(tmpl)
	onA := startInspection(t, f.router, f.supervisor, f.siteA, tmpl)
	onB := startInspection(t, f.router, f.owner, f.siteB, tmpl)

	tests := []struct {
		name, token, method, path string
		body                      any
		want                      int
		msg                       string // "" skips the body check
	}{
		{"supervisor starts on own site", f.supervisor, http.MethodPut, inspectionPath(f.siteA, uuid.NewString()), body, http.StatusCreated, ""},
		{"supervisor reads on own site", f.supervisor, http.MethodGet, inspectionPath(f.siteA, onA), nil, http.StatusOK, ""},
		{"owner reads the supervisor's inspection", f.owner, http.MethodGet, inspectionPath(f.siteA, onA), nil, http.StatusOK, ""},
		{"supervisor can't start elsewhere", f.supervisor, http.MethodPut, inspectionPath(f.siteB, uuid.NewString()), body, http.StatusNotFound, "site not found"},
		{"supervisor can't read elsewhere", f.supervisor, http.MethodGet, inspectionPath(f.siteB, onB), nil, http.StatusNotFound, "site not found"},
		{"bookkeeper can't start", f.bookkeeper, http.MethodPut, inspectionPath(f.siteA, uuid.NewString()), body, http.StatusForbidden, ""},
		{"bookkeeper can't read", f.bookkeeper, http.MethodGet, inspectionPath(f.siteA, onA), nil, http.StatusForbidden, ""},
		{"other org can't start", f.other, http.MethodPut, inspectionPath(f.siteA, uuid.NewString()), body, http.StatusNotFound, "site not found"},
		{"other org can't read", f.other, http.MethodGet, inspectionPath(f.siteA, onA), nil, http.StatusNotFound, "site not found"},
		{"other org can't reach it via its own site", f.other, http.MethodGet, inspectionPath(f.otherSite, onA), nil, http.StatusNotFound, "inspection not found"},
		{"the wrong site", f.owner, http.MethodGet, inspectionPath(f.siteB, onA), nil, http.StatusNotFound, "inspection not found"},
		{"an unknown id", f.owner, http.MethodGet, inspectionPath(f.siteA, uuid.NewString()), nil, http.StatusNotFound, "inspection not found"},
		{"a malformed id on get", f.owner, http.MethodGet, inspectionPath(f.siteA, "not-a-uuid"), nil, http.StatusNotFound, "inspection not found"},
		{"a malformed id on start", f.owner, http.MethodPut, inspectionPath(f.siteA, "not-a-uuid"), body, http.StatusBadRequest, "inspection id must be a UUID"},
		{"no token", "", http.MethodGet, inspectionPath(f.siteA, onA), nil, http.StatusUnauthorized, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, f.router, tt.method, tt.path, tt.token, tt.body)
			if tt.msg != "" {
				wantError(t, rec, tt.want, tt.msg)
				return
			}
			wantStatus(t, rec, tt.want)
		})
	}
}

func TestInspectionStartValidation(t *testing.T) {
	f := newEvidenceFixture(t, "startvalid")
	tmpl := decodeCatalog(t, f.router, f.siteA, f.owner).Checklists[0]
	body := startBody(tmpl)

	bad := map[string]map[string]any{
		"no template key":          with(body, "template_key", nil),
		"no template version":      with(body, "template_version", nil),
		"template version zero":    with(body, "template_version", 0),
		"no template hash":         with(body, "template_sha256", nil),
		"no start time":            with(body, "client_started_at", nil),
		"start time not a time":    with(body, "client_started_at", "this morning"),
		"empty device id":          with(body, "device_id", ""),
		"device id over 200 bytes": with(body, "device_id", strings.Repeat("d", 201)),
	}
	for name, b := range bad {
		t.Run(name, func(t *testing.T) {
			id := uuid.NewString()
			wantStatus(t, do(t, f.router, http.MethodPut, inspectionPath(f.siteA, id), f.owner, b), http.StatusBadRequest)
			if n := countRows(t, `SELECT count(*) FROM inspections WHERE id = $1`, id); n != 0 {
				t.Errorf("%d rows stored for a refused start", n)
			}
		})
	}
	t.Run("device id of 200 bytes", func(t *testing.T) {
		wantStatus(t, do(t, f.router, http.MethodPut, inspectionPath(f.siteA, uuid.NewString()), f.owner, with(body, "device_id", strings.Repeat("d", 200))), http.StatusCreated)
	})
	t.Run("device id error text", func(t *testing.T) {
		wantError(t, do(t, f.router, http.MethodPut, inspectionPath(f.siteA, uuid.NewString()), f.owner, with(body, "device_id", "")), http.StatusBadRequest, "device_id must be 1-200 bytes")
	})
}

// waitForLock waits until another session is blocked on a lock while running
// a statement containing fragment.
func waitForLock(t *testing.T, fragment string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		n := countRows(t, `SELECT count(*) FROM pg_stat_activity
			WHERE wait_event_type = 'Lock' AND query LIKE '%' || $1 || '%' AND pid <> pg_backend_pid()`, fragment)
		if n > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no session waited on a lock running %q", fragment)
}

// The race the retry exists for, made deterministic: an identical start is
// open but uncommitted, so this request doesn't see it, inserts, and blocks
// on the primary key. When the other commits, the insert fails; the request
// must look again and answer as a repeat, not as a conflict.
func TestInspectionStartLosesTheRace(t *testing.T) {
	ctx := context.Background()
	f := newEvidenceFixture(t, "startlost")
	tmpl := decodeCatalog(t, f.router, f.siteA, f.owner).Checklists[0]
	id := uuid.NewString()
	sum, err := hex.DecodeString(tmpl.SHA256)
	if err != nil {
		t.Fatal(err)
	}

	winner, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer winner.Rollback(ctx)
	if err := database.SetTenant(ctx, winner, database.Tenant{OrgID: f.ownerMe.OrgID}); err != nil {
		t.Fatalf("set tenant: %v", err)
	}
	// The same details startBody sends.
	tag, err := winner.Exec(ctx, `
		INSERT INTO inspections (id, org_id, site_id, regime_id, template_key, template_version, template_sha256,
		                         template_title, template_source_ref, item_count, started_by, client_started_at)
		SELECT $1, org_id, id, regime_id, $2, $3, $4, $5, $6, $7, $8, '2026-11-14T06:30:00.123456Z' FROM sites WHERE id = $9`,
		id, tmpl.Key, tmpl.Version, sum, tmpl.Title, tmpl.SourceRef, len(tmpl.Items), f.ownerMe.UserID, f.siteA)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("winner's insert: %v (%d rows)", err, tag.RowsAffected())
	}

	loser := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		loser <- do(t, f.router, http.MethodPut, inspectionPath(f.siteA, id), f.owner, startBody(tmpl))
	}()
	waitForLock(t, "INSERT INTO inspections")
	if err := winner.Commit(ctx); err != nil {
		t.Fatalf("commit winner: %v", err)
	}

	rec := <-loser
	wantStatus(t, rec, http.StatusOK)
	if in := decodeInspection(t, rec); in.ID != id || in.StartedBy != f.ownerMe.UserID {
		t.Errorf("inspection %+v, want the winner's", in)
	}
	if n := countRows(t, `SELECT count(*) FROM inspections WHERE id = $1`, id); n != 1 {
		t.Errorf("%d inspections with the id, want 1", n)
	}
}
