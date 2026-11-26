package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/devicesync"
	"construction_db/internal/core/evidence"
)

func syncSitePath(site string) string { return "/v1/sync/sites/" + site }

func pullPage(t *testing.T, router http.Handler, token, path, cursor string, limit int) *httptest.ResponseRecorder {
	t.Helper()
	q := "?limit=" + strconv.Itoa(limit)
	if cursor != "" {
		q += "&cursor=" + url.QueryEscape(cursor)
	}
	return do(t, router, http.MethodGet, path+q, token, nil)
}

func mustBase64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func encodeCursor(t *testing.T, c map[string]any) string {
	t.Helper()
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodePull(t *testing.T, rec *httptest.ResponseRecorder) devicesync.Pull {
	t.Helper()
	wantStatus(t, rec, http.StatusOK)
	var p devicesync.Pull
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode pull: %v (body %s)", err, rec.Body.String())
	}
	return p
}

// pullSite reads every page from cursor; it returns the items, the last
// cursor and whether the first page was a reset.
func pullSite(t *testing.T, router http.Handler, token, site, cursor string, limit int) ([]devicesync.Item, string, bool) {
	t.Helper()
	var items []devicesync.Item
	reset := false
	for page := 0; ; page++ {
		p := decodePull(t, pullPage(t, router, token, syncSitePath(site), cursor, limit))
		if page == 0 {
			reset = p.Reset
		} else if p.Reset {
			t.Fatalf("page %d reset", page)
		}
		items = append(items, p.Items...)
		cursor = p.Cursor
		if !p.HasMore {
			return items, cursor, reset
		}
		if page > 200 {
			t.Fatal("pull never ends")
		}
	}
}

func pullOrg(t *testing.T, router http.Handler, token, cursor string) devicesync.OrgPull {
	t.Helper()
	rec := pullPage(t, router, token, "/v1/sync", cursor, 500)
	wantStatus(t, rec, http.StatusOK)
	var p devicesync.OrgPull
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode org pull: %v", err)
	}
	return p
}

// has reports whether items hold kind with id ("" for any id).
func has(items []devicesync.Item, kind, id string) bool {
	return slices.ContainsFunc(items, func(i devicesync.Item) bool { return i.Kind == kind && (id == "" || i.ID == id) })
}

func kindsOf(items []devicesync.Item) []string {
	var out []string
	for _, i := range items {
		out = append(out, i.Kind+":"+i.ID)
	}
	return out
}

func rec(method, path string, body any) map[string]any {
	r := map[string]any{"ref": method + " " + path, "method": method, "path": path}
	if body != nil {
		r["body"] = body
	}
	return r
}

func push(t *testing.T, router http.Handler, token string, records ...map[string]any) devicesync.PushResponse {
	t.Helper()
	res := do(t, router, http.MethodPost, "/v1/sync/push", token, map[string]any{"records": records})
	wantStatus(t, res, http.StatusOK)
	var out devicesync.PushResponse
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode push: %v", err)
	}
	return out
}

func bodies(r devicesync.PushResponse) []string {
	var out []string
	for _, x := range r.Results {
		out = append(out, string(x.Body))
	}
	return out
}

func statuses(r devicesync.PushResponse) []int {
	var out []int
	for _, x := range r.Results {
		out = append(out, x.Status)
	}
	return out
}

// shift is what a supervisor captures offline in one shift: a new worker
// signing in, a talk they sign, an incident, notice of site work and the
// day's diary.
type shift struct {
	worker, signIn, talk, attendance, incident, work, revision string
	records                                                    []map[string]any
}

func newShift(c sastClock, site string) shift {
	s := shift{worker: uuid.NewString(), signIn: uuid.NewString(), talk: uuid.NewString(), attendance: uuid.NewString(),
		incident: uuid.NewString(), work: uuid.NewString(), revision: uuid.NewString()}
	name := "Sipho " + s.worker[:6]
	s.records = []map[string]any{
		rec("PUT", workerPath(s.worker), map[string]any{"name": name, "firm": "ABC Scaffolding"}),
		rec("PUT", signInPath(site, s.signIn), signInBody(s.worker, c.Earlier(50*time.Minute), map[string]any{"device_id": "phone-1"})),
		rec("PUT", talkPath(site, s.talk), talkBody(map[string]any{"started_at": rfc(c.Earlier(40 * time.Minute)), "device_id": "phone-1"})),
		rec("PUT", signPath(site, s.talk, s.attendance), signBody(name, c.Earlier(30*time.Minute), nil)),
		rec("PUT", versionPath(site, s.incident, 1), with(versionBody(c.Earlier(55*time.Minute), "unconsciousness"), "device_id", "phone-1")),
		rec("PUT", workVersionPath(site, s.work, 1), workBody(c, c.Day(20), "excavation")),
		rec("PUT", revisionPath(site, c.Today(), s.revision), revisionBody(c.Earlier(5*time.Minute), nil)),
	}
	return s
}

func TestSyncPushThenPull(t *testing.T) {
	f := newEvidenceFixture(t, "syncshift")
	c := newSASTClock(t)

	_, cursor, _ := pullSite(t, f.router, f.supervisor, f.siteA, "", 200)
	s := newShift(c, f.siteA)

	first := push(t, f.router, f.supervisor, s.records...)
	if first.Processed != len(s.records) || !slices.Equal(statuses(first), []int{201, 201, 201, 201, 201, 201, 201}) {
		t.Fatalf("first push: processed %d, statuses %v (%v)", first.Processed, statuses(first), bodies(first))
	}
	for i, r := range first.Results {
		if r.Ref == nil || *r.Ref != s.records[i]["ref"] {
			t.Errorf("result %d ref %v", i, r.Ref)
		}
	}

	// The response was lost: the phone sends the whole batch again.
	again := push(t, f.router, f.supervisor, s.records...)
	if !slices.Equal(statuses(again), []int{200, 200, 200, 200, 200, 200, 200}) {
		t.Fatalf("repeat: %v", statuses(again))
	}
	for i := range again.Results {
		if i == 0 {
			continue // a worker carries no time that would differ
		}
		var a, b map[string]any
		_ = json.Unmarshal(first.Results[i].Body, &a)
		_ = json.Unmarshal(again.Results[i].Body, &b)
		if a["id"] != b["id"] {
			t.Errorf("record %d: repeat answered another record: %v vs %v", i, a["id"], b["id"])
		}
	}

	items, next, reset := pullSite(t, f.router, f.supervisor, f.siteA, cursor, 200)
	if reset {
		t.Fatal("delta reset")
	}
	for _, want := range [][2]string{
		{"sign_in_day", c.Today()}, {"toolbox_talk", s.talk}, {"incident", s.incident}, {"site_work", s.work},
		{"diary_day", c.Today()}, {"deadline", ""},
	} {
		if !has(items, want[0], want[1]) {
			t.Errorf("delta lacks %s %s; has %v", want[0], want[1], kindsOf(items))
		}
	}
	org := pullOrg(t, f.router, f.supervisor, "")
	if !has(org.Items, "worker", s.worker) {
		t.Errorf("org pull lacks the worker: %v", kindsOf(org.Items))
	}

	// Nothing new: an empty delta. One new record: just its aggregate.
	if items, _, _ := pullSite(t, f.router, f.supervisor, f.siteA, next, 200); len(items) != 0 {
		t.Errorf("empty delta has %v", kindsOf(items))
	}
	sign2 := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, signPath(f.siteA, s.talk, sign2), f.supervisor,
		signBody("Lerato K", c.Earlier(20*time.Minute), nil)), http.StatusCreated)
	// The talk, and the day's register and diary, whose headcounts count it.
	items, _, _ = pullSite(t, f.router, f.supervisor, f.siteA, next, 200)
	if len(items) != 3 || !has(items, "toolbox_talk", s.talk) || !has(items, "sign_in_day", c.Today()) || !has(items, "diary_day", c.Today()) {
		t.Errorf("delta after a signature: %v", kindsOf(items))
	}
}

// D1's property through the API: a transaction that took its xid before a
// later commit, and commits after it, is still pulled. The pull waits for it
// rather than passing it.
func TestSyncPullsASlowCommit(t *testing.T) {
	f := newEvidenceFixture(t, "syncslow")
	c := newSASTClock(t)
	ctx := context.Background()
	_, cursor, _ := pullSite(t, f.router, f.owner, f.siteA, "", 200)

	slow := holdOpen(t, f)
	if _, err := slow.Exec(ctx, `SELECT pg_current_xact_id()`); err != nil {
		t.Fatal(err)
	}

	talk := uuid.NewString()
	wantStatus(t, do(t, f.router, http.MethodPut, talkPath(f.siteA, talk), f.owner,
		talkBody(map[string]any{"started_at": rfc(c.Earlier(10 * time.Minute))})), http.StatusCreated)

	items, held, _ := pullSite(t, f.router, f.owner, f.siteA, cursor, 200)
	if has(items, "toolbox_talk", talk) {
		t.Fatalf("the pull passed an open transaction: %v", kindsOf(items))
	}

	loc := uuid.NewString()
	if _, err := slow.Exec(ctx, `INSERT INTO site_locations (id, org_id, site_id, kind, name) VALUES ($1, $2, $3, 'zone', 'Slow zone')`,
		loc, f.ownerMe.OrgID, f.siteA); err != nil {
		t.Fatal(err)
	}
	if err := slow.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	items, _, _ = pullSite(t, f.router, f.owner, f.siteA, held, 200)
	if !has(items, "location_list", f.siteA) || !has(items, "toolbox_talk", talk) {
		t.Fatalf("after the slow commit the delta has %v; want the location list and the talk", kindsOf(items))
	}
}

func TestSyncCursorResets(t *testing.T) {
	f := newEvidenceFixture(t, "syncreset")
	ctx := context.Background()
	_, cursor, reset := pullSite(t, f.router, f.supervisor, f.siteA, "", 200)
	if reset {
		t.Fatal("a first pull isn't a reset")
	}

	t.Run("bad cursors are 400", func(t *testing.T) {
		for name, cur := range map[string]string{
			"garbage":        "not-a-cursor",
			"another site":   cursorFor(t, f, f.siteB),
			"org for a site": pullOrg(t, f.router, f.owner, "").Cursor,
		} {
			res := pullPage(t, f.router, f.owner, syncSitePath(f.siteA), cur, 10)
			if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), `"invalid cursor"`) {
				t.Errorf("%s: %d %s", name, res.Code, res.Body.String())
			}
		}
		res := pullPage(t, f.router, f.owner, syncSitePath(f.siteA), "", 501)
		if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), `"limit must be 1-500"`) {
			t.Errorf("limit 501: %d %s", res.Code, res.Body.String())
		}
	})

	t.Run("another person's cursor starts again", func(t *testing.T) {
		// Same role and site, so only the person differs.
		email := "second-" + uuid.NewString()[:8] + "@syncreset.test"
		second := inviteMember(t, f.router, f.owner, email, "supervisor")
		wantStatus(t, do(t, f.router, http.MethodPut, "/v1/sites/"+f.siteA+"/members/"+second, f.owner, nil), http.StatusNoContent)
		if _, _, reset := pullSite(t, f.router, login(t, f.router, email), f.siteA, cursor, 200); !reset {
			t.Error("another supervisor reused the first one's cursor without a reset")
		}
	})

	t.Run("a cursor from the future starts again", func(t *testing.T) {
		var c map[string]any
		raw := mustBase64(t, cursor)
		_ = json.Unmarshal(raw, &c)
		c["lo"] = "9000000000000000000"
		if _, _, reset := pullSite(t, f.router, f.supervisor, f.siteA, encodeCursor(t, c), 200); !reset {
			t.Error("no reset")
		}
	})

	t.Run("a new server epoch starts again", func(t *testing.T) {
		if _, err := testAdmin.Exec(ctx, `SELECT sync_new_generation()`); err != nil {
			t.Fatal(err)
		}
		items, after, reset := pullSite(t, f.router, f.supervisor, f.siteA, cursor, 200)
		if !reset {
			t.Fatal("no reset after a restore")
		}
		_ = items
		cursor = after
	})

	t.Run("a role change starts again", func(t *testing.T) {
		if _, _, reset := pullSite(t, f.router, f.supervisor, f.siteA, cursor, 200); reset {
			t.Fatal("reset without a change")
		}
		execAsTenant(t, f.ownerMe.OrgID, 1, `UPDATE org_members SET role = 'she_officer'
			WHERE org_id = $1 AND user_id = (SELECT user_id FROM org_members m JOIN users u ON u.id = m.user_id
			                                 WHERE m.org_id = $1 AND u.email LIKE 'foreman@%')`, f.ownerMe.OrgID)
		if _, _, reset := pullSite(t, f.router, f.supervisor, f.siteA, cursor, 200); !reset {
			t.Error("no reset after the role changed")
		}
	})
}

func TestSyncAccessLoss(t *testing.T) {
	f := newEvidenceFixture(t, "syncloss")
	supID := memberID(t, f, "foreman@")

	sites := func(token string) []string {
		var out []string
		for _, s := range pullOrg(t, f.router, token, "").Sites {
			var site struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			}
			_ = json.Unmarshal(s.Site, &site)
			out = append(out, site.ID+":"+site.Status)
			if s.ReferenceSHA256 == "" || s.RegimeCode == "" {
				t.Errorf("site entry without its reference: %+v", s)
			}
		}
		return out
	}
	if got := sites(f.supervisor); !slices.Equal(got, []string{f.siteA + ":active"}) {
		t.Fatalf("supervisor's sites %v", got)
	}

	wantStatus(t, do(t, f.router, http.MethodDelete, "/v1/sites/"+f.siteB, f.owner, nil), http.StatusNoContent)
	if got := sites(f.owner); !slices.Contains(got, f.siteB+":archived") {
		t.Errorf("an archived site must stay listed (still readable): %v", got)
	}
	pullSite(t, f.router, f.owner, f.siteB, "", 200)

	wantStatus(t, do(t, f.router, http.MethodDelete, "/v1/sites/"+f.siteA+"/members/"+supID, f.owner, nil), http.StatusNoContent)
	if got := sites(f.supervisor); len(got) != 0 {
		t.Errorf("removed supervisor still lists %v", got)
	}
	res := pullPage(t, f.router, f.supervisor, syncSitePath(f.siteA), "", 10)
	if res.Code != http.StatusNotFound || !strings.Contains(res.Body.String(), `"site not found"`) {
		t.Errorf("removed supervisor's site pull: %d %s", res.Code, res.Body.String())
	}

	if res := pullPage(t, f.router, f.bookkeeper, "/v1/sync", "", 10); res.Code != http.StatusForbidden {
		t.Errorf("bookkeeper pull: %d", res.Code)
	}
}

func TestSyncPaging(t *testing.T) {
	f := newEvidenceFixture(t, "syncpaging")
	c := newSASTClock(t)
	var talks []string
	for i := range 5 {
		id := uuid.NewString()
		talks = append(talks, id)
		wantStatus(t, do(t, f.router, http.MethodPut, talkPath(f.siteA, id), f.owner,
			talkBody(map[string]any{"started_at": rfc(c.Earlier(time.Duration(10+i) * time.Minute))})), http.StatusCreated)
	}

	whole, _, _ := pullSite(t, f.router, f.owner, f.siteA, "", 500)

	// Page by two, and push more records between pages: the pass finishes,
	// and the next delta has what arrived meanwhile.
	var paged []devicesync.Item
	cursor := ""
	var late []string
	for page := 0; ; page++ {
		p := decodePull(t, pullPage(t, f.router, f.owner, syncSitePath(f.siteA), cursor, 2))
		if len(p.Items) > 2 {
			t.Fatalf("page of %d", len(p.Items))
		}
		paged = append(paged, p.Items...)
		cursor = p.Cursor
		if page == 1 {
			id := uuid.NewString()
			late = append(late, id)
			wantStatus(t, do(t, f.router, http.MethodPut, talkPath(f.siteA, id), f.owner,
				talkBody(map[string]any{"started_at": rfc(c.Earlier(5 * time.Minute))})), http.StatusCreated)
		}
		if !p.HasMore {
			break
		}
		if page > 50 {
			t.Fatalf("paging never ends: %d pages for %d items", page, len(whole))
		}
	}
	for _, w := range whole {
		if !has(paged, w.Kind, w.ID) {
			t.Errorf("paging lost %s %s", w.Kind, w.ID)
		}
	}
	delta, _, _ := pullSite(t, f.router, f.owner, f.siteA, cursor, 500)
	if !has(delta, "toolbox_talk", late[0]) {
		t.Errorf("the record pushed during the pass is not in the next delta: %v", kindsOf(delta))
	}
	for _, id := range talks {
		if !has(paged, "toolbox_talk", id) {
			t.Errorf("talk %s missing", id)
		}
	}
}

// D3: every pulled item is what that role's own GET returns, so a pull never
// shows a field the GET wouldn't.
func TestPullMatchesGet(t *testing.T) {
	f := newEvidenceFixture(t, "syncmatch")
	c := newSASTClock(t)
	s := newShift(c, f.siteA)
	push(t, f.router, f.supervisor, s.records...)
	wantStatus(t, do(t, f.router, http.MethodPut, "/v1/sites/"+f.siteA+"/roster/"+s.worker, f.owner, nil), http.StatusOK) // the sign-in added them
	wantStatus(t, do(t, f.router, http.MethodPost, "/v1/sites/"+f.siteA+"/locations", f.owner,
		map[string]any{"kind": "zone", "name": "Block A"}), http.StatusCreated)

	kinds := map[string]devicesync.Kind{}
	for _, k := range append(devicesync.CoreKinds(), testEdition.SyncSources().Kinds...) {
		kinds[k.Name] = k
	}
	check := func(t *testing.T, token, scope string, items []devicesync.Item) map[string]bool {
		seen := map[string]bool{}
		for _, it := range items {
			seen[it.Kind] = true
			k := kinds[it.Kind]
			res := get(t, f.router, k.Path(scope, it.ID), token)
			wantStatus(t, res, http.StatusOK)
			var pulled, got any
			_ = json.Unmarshal(it.Data, &pulled)
			_ = json.Unmarshal(res.Body.Bytes(), &got)
			if !reflect.DeepEqual(pulled, got) {
				t.Errorf("%s %s: pulled\n%s\nGET\n%s", it.Kind, it.ID, it.Data, res.Body.String())
			}
		}
		return seen
	}
	for _, who := range []struct{ name, token string }{{"owner", f.owner}, {"supervisor", f.supervisor}} {
		t.Run(who.name, func(t *testing.T) {
			items, _, _ := pullSite(t, f.router, who.token, f.siteA, "", 500)
			seen := check(t, who.token, f.siteA, items)
			for _, k := range []string{"location_list", "toolbox_talk", "incident", "deadline", "sign_in_day", "roster", "site_work", "diary_day"} {
				if !seen[k] {
					t.Errorf("no %s item to compare", k)
				}
			}
			org := pullOrg(t, f.router, who.token, "")
			check(t, who.token, f.ownerMe.OrgID, org.Items)
			var me map[string]any
			_ = json.Unmarshal(org.Me, &me)
			var got map[string]any
			_ = json.Unmarshal(get(t, f.router, "/v1/me", who.token).Body.Bytes(), &got)
			if !reflect.DeepEqual(me, got) {
				t.Errorf("me %v, GET %v", me, got)
			}
		})
	}
}

func TestSyncReference(t *testing.T) {
	f := newEvidenceFixture(t, "syncref")
	res := get(t, f.router, syncSitePath(f.siteA)+"/reference", f.supervisor)
	wantStatus(t, res, http.StatusOK)
	var ref devicesync.Reference
	if err := json.Unmarshal(res.Body.Bytes(), &ref); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"checklists", "toolbox_topics", "document_types", "incident_types", "vocabulary", "site_work_facts"} {
		if len(ref.Parts[p]) < 3 {
			t.Errorf("part %s is empty: %s", p, ref.Parts[p])
		}
	}
	var cat any
	_ = json.Unmarshal(get(t, f.router, "/v1/sites/"+f.siteA+"/checklists", f.supervisor).Body.Bytes(), &cat)
	var part any
	_ = json.Unmarshal(ref.Parts["checklists"], &part)
	if !reflect.DeepEqual(cat, part) {
		t.Error("the checklists part is not GET …/checklists")
	}
	org := pullOrg(t, f.router, f.supervisor, "")
	if len(org.Sites) != 1 || org.Sites[0].ReferenceSHA256 != ref.SHA256 {
		t.Errorf("site entry sha %v, reference %s", org.Sites, ref.SHA256)
	}
	if res := get(t, f.router, syncSitePath(f.siteB)+"/reference", f.supervisor); res.Code != http.StatusNotFound {
		t.Errorf("another site's reference: %d", res.Code)
	}
}

func TestPushBatchRules(t *testing.T) {
	f := newEvidenceFixture(t, "syncbatch")
	c := newSASTClock(t)
	talk := uuid.NewString()
	ok := rec("PUT", talkPath(f.siteA, talk), talkBody(map[string]any{"started_at": rfc(c.Earlier(10 * time.Minute))}))
	long := strings.Repeat("x", 201)

	for _, tt := range []struct {
		name    string
		body    any
		msg     string
		index   int
		noIndex bool
	}{
		{"no records", map[string]any{"records": []any{}}, "records must hold 1-100 records", 0, true},
		{"too many", map[string]any{"records": repeat(ok, 101)}, "records must hold 1-100 records", 0, true},
		{"not json", "nope", "invalid batch", 0, true},
		{"bad method", map[string]any{"records": []any{ok, rec("DELETE", talkPath(f.siteA, talk), nil)}}, "method must be PUT or POST", 1, false},
		{"online-only route", map[string]any{"records": []any{ok, rec("POST", "/v1/sites", map[string]any{"name": "x"})}}, "route not allowed in a batch", 1, false},
		{"a view", map[string]any{"records": []any{ok, rec("POST", evidencePath(f.siteA, uuid.NewString())+"/view", nil)}}, "route not allowed in a batch", 1, false},
		{"evidence complete", map[string]any{"records": []any{ok, rec("POST", completePath(f.siteA, uuid.NewString()), nil)}}, "route not allowed in a batch", 1, false},
		{"dot segments", map[string]any{"records": []any{ok, rec("PUT", "/v1/sites/"+f.siteA+"/toolbox-talks/../toolbox-talks/"+talk, nil)}}, "route not allowed in a batch", 1, false},
		{"a query", map[string]any{"records": []any{ok, rec("PUT", talkPath(f.siteA, talk)+"?x=1", nil)}}, "route not allowed in a batch", 1, false},
		{"escaped slash", map[string]any{"records": []any{ok, rec("PUT", "/v1/sites/"+f.siteA+"%2Ftoolbox-talks/"+talk, nil)}}, "route not allowed in a batch", 1, false},
		{"the push itself", map[string]any{"records": []any{ok, rec("POST", "/v1/sync/push", nil)}}, "route not allowed in a batch", 1, false},
		{"long ref", map[string]any{"records": []any{with(ok, "ref", long)}}, "ref must be at most 200 bytes", 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res := do(t, f.router, http.MethodPost, "/v1/sync/push", f.supervisor, tt.body)
			if s, isStr := tt.body.(string); isStr {
				req := httptest.NewRequest(http.MethodPost, "/v1/sync/push", strings.NewReader(s))
				req.Header.Set("Authorization", "Bearer "+f.supervisor)
				res = httptest.NewRecorder()
				f.router.ServeHTTP(res, req)
			}
			var p devicesync.BatchProblem
			_ = json.Unmarshal(res.Body.Bytes(), &p)
			if res.Code != http.StatusBadRequest || p.Error != tt.msg || (p.Index == nil) != tt.noIndex || (p.Index != nil && *p.Index != tt.index) {
				t.Errorf("%d %s", res.Code, res.Body.String())
			}
		})
	}
	if n := countRows(t, `SELECT count(*) FROM toolbox_talks WHERE id = $1`, talk); n != 0 {
		t.Errorf("a refused batch applied its records (%d talks)", n)
	}

	t.Run("too large", func(t *testing.T) {
		big := with(ok, "body", map[string]any{"notes": strings.Repeat("n", devicesync.MaxPushBytes)})
		res := do(t, f.router, http.MethodPost, "/v1/sync/push", f.supervisor, map[string]any{"records": []any{big}})
		if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "batch too large; at most 1 MiB") {
			t.Errorf("%d %s", res.Code, res.Body.String())
		}
	})
}

func repeat(r map[string]any, n int) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = r
	}
	return out
}

// A batch is the single endpoints in order: a version before its record,
// a record naming a photo still uploading, a conflict, each answered by its
// own endpoint, and one failure doesn't stop the rest.
func TestPushOrderAndPerRecordResults(t *testing.T) {
	f := newEvidenceFixture(t, "syncorder")
	c := newSASTClock(t)
	inc := uuid.NewString()
	occurred := c.Earlier(50 * time.Minute)
	v1 := versionBody(occurred, "near_miss")
	v2 := with(versionBody(occurred, "near_miss"), "amend_reason", "Corrected the place")

	img := testImage(t, "jpeg", 40, 30)
	photo := uuid.NewString()
	withPhoto := with(versionBody(occurred, "near_miss"), "evidence_ids", []string{photo})

	r := push(t, f.router, f.supervisor,
		rec("PUT", versionPath(f.siteA, inc, 2), v2),
		rec("PUT", versionPath(f.siteA, inc, 1), v1),
		rec("PUT", versionPath(f.siteA, inc, 2), v2),
		rec("PUT", evidencePath(f.siteA, photo), declareFor(img, "image/jpeg")),
		rec("PUT", versionPath(f.siteA, uuid.NewString(), 1), withPhoto),
		rec("PUT", versionPath(f.siteA, inc, 1), with(v1, "description", "different")),
	)
	if want := []int{404, 201, 201, 201, 409, 409}; !slices.Equal(statuses(r), want) {
		t.Fatalf("statuses %v, want %v (%v)", statuses(r), want, bodies(r))
	}
	if !strings.Contains(string(r.Results[4].Body), `"evidence not verified yet"`) || !strings.Contains(string(r.Results[4].Body), photo) {
		t.Errorf("photo still uploading: %s", r.Results[4].Body)
	}
	if !strings.Contains(string(r.Results[5].Body), `"incident id already used with different details"`) {
		t.Errorf("conflict: %s", r.Results[5].Body)
	}

	// The declaration's own answer carries the upload; upload and complete
	// stay one request per file, then the waiting record goes through.
	var ev evidence.Evidence
	if err := json.Unmarshal(r.Results[3].Body, &ev); err != nil || ev.Upload == nil {
		t.Fatalf("declare result %s", r.Results[3].Body)
	}
	putUpload(t, ev.Upload, img)
	wantStatus(t, do(t, f.router, http.MethodPost, completePath(f.siteA, photo), f.supervisor, nil), http.StatusCreated)
	again := push(t, f.router, f.supervisor, rec("PUT", evidencePath(f.siteA, photo), declareFor(img, "image/jpeg")),
		rec("PUT", versionPath(f.siteA, uuid.NewString(), 1), withPhoto))
	if !slices.Equal(statuses(again), []int{200, 201}) {
		t.Errorf("after the upload: %v (%v)", statuses(again), bodies(again))
	}
}

// The batch keeps the request's deadline: it starts no record with less than
// PushReserve (2 s) left, and says how far it got.
func TestPushStopsBeforeTheDeadline(t *testing.T) {
	f := newEvidenceFixture(t, "syncdeadline")
	c := newSASTClock(t)
	var records []any
	for i := range 3 {
		records = append(records, rec("PUT", talkPath(f.siteA, uuid.NewString()),
			talkBody(map[string]any{"started_at": rfc(c.Earlier(time.Duration(10+i) * time.Minute))})))
	}
	b, _ := json.Marshal(map[string]any{"records": records})
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/v1/sync/push", strings.NewReader(string(b))).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+f.supervisor)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	f.router.ServeHTTP(res, req)
	wantStatus(t, res, http.StatusOK)
	var out devicesync.PushResponse
	_ = json.Unmarshal(res.Body.Bytes(), &out)
	if out.Processed != 1 || len(out.Results) != 1 || out.Results[0].Status != http.StatusCreated {
		t.Errorf("processed %d, results %v", out.Processed, statuses(out))
	}
}

// Two phones push the same incident at once. Forced: an uncommitted row with
// its id holds both inserts; when it goes, one batch creates the incident
// and the other's insert waits for it and answers as an identical repeat.
func TestPushSameRecordTwice(t *testing.T) {
	f := newEvidenceFixture(t, "synctwice")
	c := newSASTClock(t)
	ctx := context.Background()
	inc := uuid.NewString()
	body := versionBody(c.Earlier(30*time.Minute), "near_miss")

	blocker := holdOpen(t, f)
	if _, err := blocker.Exec(ctx, `INSERT INTO incidents (id, org_id, site_id, regime_id, reported_by)
		SELECT $1, $2, $3, regime_id, $4 FROM sites WHERE id = $3`, inc, f.ownerMe.OrgID, f.siteA, f.ownerMe.UserID); err != nil {
		t.Fatal(err)
	}
	results := make(chan devicesync.PushResponse, 2)
	for range 2 {
		go func() { results <- push(t, f.router, f.supervisor, rec("PUT", versionPath(f.siteA, inc, 1), body)) }()
	}
	waitForWaiters(t, "INSERT INTO incidents", 2)
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	got := []int{(<-results).Results[0].Status, (<-results).Results[0].Status}
	slices.Sort(got)
	if !slices.Equal(got, []int{200, 201}) {
		t.Errorf("statuses %v, want one 201 and one 200", got)
	}
	if n := countRows(t, `SELECT count(*) FROM incident_versions WHERE incident_id = $1`, inc); n != 1 {
		t.Errorf("%d versions", n)
	}
}

func memberID(t *testing.T, f evidenceFixture, emailPrefix string) string {
	t.Helper()
	var id string
	if err := testAdmin.QueryRow(context.Background(), `SELECT m.user_id::text FROM org_members m JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1 AND u.email LIKE $2 || '%'`, f.ownerMe.OrgID, emailPrefix).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func cursorFor(t *testing.T, f evidenceFixture, site string) string {
	t.Helper()
	return decodePull(t, pullPage(t, f.router, f.owner, syncSitePath(site), "", 1)).Cursor
}

// record is rec, for tests whose own rec is a recorder.
func record(method, path string, body any) map[string]any { return rec(method, path, body) }

// SYNC review 1: one person in two organisations. The org cursor names its
// organisation; used in the other, it resets and the full set comes,
// rather than a delta from the first organisation's place that skips
// everything older.
func TestSyncOrgCursorIsTheOrgs(t *testing.T) {
	router := newRouter(t)
	email := "u@twoorgs-" + uuid.NewString()[:8] + ".test"
	inB := signup(t, router, "Two Orgs B", email)
	orgB := me(t, router, inB).OrgID
	workerB := newWorker(t, router, inB, map[string]any{"name": "Older In B " + uuid.NewString()[:6]})

	ownerA := signup(t, router, "Two Orgs A", "owner@twoorgs-a-"+uuid.NewString()[:8]+".test")
	orgA := me(t, router, ownerA).OrgID
	invite := sendInvite(t, router, ownerA, map[string]any{"full_name": "U", "email": email, "role": auth.RoleOwner})
	acceptInvite(t, router, invite.Token, "correcthorse1", http.StatusOK)
	inA := loginTokensFor(t, router, email, orgA).AccessToken
	inB = loginTokensFor(t, router, email, orgB).AccessToken

	fromA := pullOrg(t, router, inA, "")
	if has(fromA.Items, "worker", workerB) {
		t.Fatal("org A's pull shows org B's worker")
	}
	inBWithA := pullOrg(t, router, inB, fromA.Cursor)
	if !inBWithA.Reset || !has(inBWithA.Items, "worker", workerB) {
		t.Errorf("org A's cursor in org B: reset %v, items %v; want a reset with B's worker", inBWithA.Reset, kindsOf(inBWithA.Items))
	}
	// And the cursor B hands back is B's: no reset on its own.
	if again := pullOrg(t, router, inB, inBWithA.Cursor); again.Reset {
		t.Error("org B's own cursor reset")
	}
}

// SYNC review 6: the org pull renders every site the caller can see on every
// page, inside the page's time budget. At 50 sites it stays far inside the
// 10-second deadline.
func TestOrgPullFiftySites(t *testing.T) {
	router := newRouter(t)
	token := signup(t, router, "Fifty Co", "owner@fifty-"+uuid.NewString()[:8]+".test")
	for i := range 49 {
		createSite(t, router, token, "Site "+strconv.Itoa(i))
	}
	start := time.Now()
	p := pullOrg(t, router, token, "")
	took := time.Since(start)
	if len(p.Sites) != 50 {
		t.Fatalf("%d sites", len(p.Sites))
	}
	if took > 4*time.Second {
		t.Errorf("an org pull of 50 sites took %s", took)
	}
	t.Logf("50 sites in %s", took)
}
