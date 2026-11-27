package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"construction_db/internal/core/actions"
	"construction_db/internal/core/auth"
	"construction_db/internal/core/invites"
	"construction_db/internal/core/members"
	"construction_db/internal/core/notifications"
	"construction_db/internal/core/reports"
)

// teamFixture: an owner who signed up, a second owner, a SHE officer and a
// supervisor on site A, all active; site B beside it.
type teamFixture struct {
	router                           http.Handler
	owner, owner2, she, sup          string // tokens
	ownerID, owner2ID, sheID, supID  string
	org, siteA, siteB, emailSuffix   string
	ownerEmail, owner2Email, supMail string
}

func newTeamFixture(t *testing.T, name string) teamFixture {
	t.Helper()
	sfx := name + "-" + uuid.NewString()[:8] + ".team.test"
	r := newRouter(t)
	f := teamFixture{router: r, emailSuffix: sfx, ownerEmail: "owner@" + sfx, owner2Email: "owner2@" + sfx, supMail: "foreman@" + sfx}
	f.owner = signup(t, r, name+" Co", f.ownerEmail)
	me := me(t, r, f.owner)
	f.ownerID, f.org = me.UserID, me.OrgID
	f.siteA = listSites(t, r, f.owner)[0].ID
	f.siteB = createSite(t, r, f.owner, "Site B")
	f.owner2ID = inviteMember(t, r, f.owner, f.owner2Email, auth.RoleOwner)
	f.sheID = inviteMember(t, r, f.owner, "she@"+sfx, auth.RoleSHEOfficer)
	f.supID = inviteMember(t, r, f.owner, f.supMail, auth.RoleSupervisor)
	f.owner2, f.she, f.sup = login(t, r, f.owner2Email), login(t, r, "she@"+sfx), login(t, r, f.supMail)
	wantStatus(t, do(t, r, http.MethodPut, "/v1/sites/"+f.siteA+"/members/"+f.supID, f.owner, nil), http.StatusNoContent)
	return f
}

func memberPath(id string) string { return "/v1/members/" + id }

func (f teamFixture) patch(token, id string, body map[string]any) *httptest.ResponseRecorder {
	return serveJSON(f.router, http.MethodPatch, memberPath(id), token, body)
}

// serveJSON is do without a *testing.T, for goroutines.
func serveJSON(router http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func decodeMember[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return out
}

// Every rule and error text of team management through the API (D1-D8).
func TestMembersAPI(t *testing.T) {
	f := newTeamFixture(t, "members")
	r := f.router

	t.Run("list", func(t *testing.T) {
		list := decodeMember[struct{ Members []members.Member }](t, do(t, r, http.MethodGet, "/v1/members", f.owner, nil))
		if len(list.Members) != 4 {
			t.Fatalf("want 4 members, got %+v", list.Members)
		}
		for _, m := range list.Members {
			if m.UserID == f.supID && !slices.Equal(m.SiteIDs, []string{f.siteA}) {
				t.Errorf("supervisor's sites %v", m.SiteIDs)
			}
		}
		wantError(t, do(t, r, http.MethodGet, "/v1/members?status=gone", f.owner, nil), 400, members.ErrBadStatusFilter.Error())
		wantError(t, do(t, r, http.MethodGet, "/v1/members", f.sup, nil), 403, "forbidden")
		other := signup(t, r, "Members Other Co", "owner@other-"+f.emailSuffix)
		wantError(t, do(t, r, http.MethodGet, memberPath(f.supID), other, nil), 404, members.ErrMemberNotFound.Error())
		wantError(t, do(t, r, http.MethodGet, memberPath("not-a-uuid"), f.owner, nil), 404, members.ErrMemberNotFound.Error())
		wantError(t, do(t, r, http.MethodGet, memberPath(f.supID)+"/history", other, nil), 404, members.ErrMemberNotFound.Error())
	})

	t.Run("refused", func(t *testing.T) {
		why := "Testing"
		for _, tc := range []struct {
			name   string
			token  string
			id     string
			body   map[string]any
			status int
			err    error
		}{
			{"nothing to change", f.owner, f.sheID, map[string]any{"reason": why}, 400, members.ErrNothingToChange},
			{"no reason", f.owner, f.sheID, map[string]any{"status": "suspended"}, 400, members.ErrBadReason},
			{"blank reason", f.owner, f.sheID, map[string]any{"status": "suspended", "reason": "  "}, 400, members.ErrBadReason},
			{"long reason", f.owner, f.sheID, map[string]any{"status": "suspended", "reason": strings.Repeat("é", 501)}, 400, members.ErrBadReason},
			{"unknown role", f.owner, f.sheID, map[string]any{"role": "director", "reason": why}, 400, members.ErrBadRole},
			{"invited is not a PATCH status", f.owner, f.sheID, map[string]any{"status": "invited", "reason": why}, 400, members.ErrBadStatus},
			{"self", f.owner, f.ownerID, map[string]any{"role": "she_officer", "reason": why}, 403, members.ErrSelf},
			{"SHE officer demotes an owner", f.she, f.ownerID, map[string]any{"role": "she_officer", "reason": why}, 403, members.ErrOwnerOnly},
			{"SHE officer suspends an owner", f.she, f.owner2ID, map[string]any{"status": "suspended", "reason": why}, 403, members.ErrOwnerOnly},
			{"SHE officer grants owner", f.she, f.supID, map[string]any{"role": "owner", "reason": why}, 403, members.ErrOwnerOnly},
			{"into supervisor without sites", f.owner, f.sheID, map[string]any{"role": "supervisor", "reason": why}, 400, members.ErrSiteIDsRequired},
			{"sites without supervisor", f.owner, f.sheID, map[string]any{"status": "suspended", "reason": why, "site_ids": []string{f.siteA}}, 400, members.ErrSiteIDsUnexpected},
			{"a malformed site", f.owner, f.sheID, map[string]any{"role": "supervisor", "reason": why, "site_ids": []string{"x"}}, 400, members.ErrBadSiteIDs},
			{"another org's site", f.owner, f.sheID, map[string]any{"role": "supervisor", "reason": why, "site_ids": []string{uuid.NewString()}}, 400, members.ErrBadSiteIDs},
			{"unknown member", f.owner, uuid.NewString(), map[string]any{"status": "suspended", "reason": why}, 404, members.ErrMemberNotFound},
		} {
			t.Run(tc.name, func(t *testing.T) {
				wantError(t, do(t, r, http.MethodPatch, memberPath(tc.id), tc.token, tc.body), tc.status, tc.err.Error())
			})
		}
		wantError(t, do(t, r, http.MethodPatch, memberPath(f.sheID), f.sup, map[string]any{"status": "suspended", "reason": why}), 403, "forbidden")
	})

	t.Run("an archived site can't be named", func(t *testing.T) {
		gone := createSite(t, r, f.owner, "Gone")
		wantStatus(t, do(t, r, http.MethodDelete, "/v1/sites/"+gone, f.owner, nil), http.StatusNoContent)
		wantError(t, f.patch(f.owner, f.sheID, map[string]any{"role": "supervisor", "reason": "x", "site_ids": []string{gone}}),
			400, members.ErrBadSiteIDs.Error())
	})

	t.Run("into and out of supervisor", func(t *testing.T) {
		rec := do(t, r, http.MethodPatch, memberPath(f.sheID), f.owner, map[string]any{"role": "supervisor", "reason": "Runs site B", "site_ids": []string{f.siteB}})
		wantStatus(t, rec, 200)
		if c := decodeMember[members.Change](t, rec); !slices.Equal(c.SiteIDs, []string{f.siteB}) || len(c.ClearedSiteIDs) != 0 || c.Role != "supervisor" {
			t.Errorf("into supervisor: %+v", c)
		}
		// The site list now limits them.
		wantStatus(t, do(t, r, http.MethodGet, "/v1/sites/"+f.siteA, f.she, nil), http.StatusNotFound)
		wantStatus(t, do(t, r, http.MethodGet, "/v1/sites/"+f.siteB, f.she, nil), http.StatusOK)

		rec = do(t, r, http.MethodPatch, memberPath(f.sheID), f.owner, map[string]any{"role": "she_officer", "reason": "Back to safety"})
		wantStatus(t, rec, 200)
		if c := decodeMember[members.Change](t, rec); len(c.SiteIDs) != 0 || !slices.Equal(c.ClearedSiteIDs, []string{f.siteB}) {
			t.Errorf("out of supervisor: %+v", c)
		}

		h := decodeMember[members.History](t, do(t, r, http.MethodGet, memberPath(f.sheID)+"/history", f.owner, nil))
		var got []string
		for _, e := range h.History {
			got = append(got, strings.Join([]string{e.Role, deref(e.PrevRole), deref(e.Reason), ids(e.SiteIDs), ids(e.PrevSiteIDs), deref(e.ChangedByName)}, "|"))
		}
		want := []string{
			"she_officer|-|-|-|-|Test Owner",  // invited by the owner
			"she_officer|she_officer|-|-|-|-", // accepted (no one else acting)
			"supervisor|she_officer|Runs site B|" + f.siteB + "||Test Owner",
			"she_officer|supervisor|Back to safety||" + f.siteB + "|Test Owner",
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("history\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
		var sites []string
		for _, e := range h.SiteHistory {
			sites = append(sites, e.Change+":"+e.SiteID)
		}
		if want := []string{"added:" + f.siteB, "removed:" + f.siteB}; !slices.Equal(sites, want) {
			t.Errorf("site history %v, want %v", sites, want)
		}
	})

	t.Run("a change to nothing new records nothing", func(t *testing.T) {
		before := len(decodeMember[members.History](t, do(t, r, http.MethodGet, memberPath(f.sheID)+"/history", f.owner, nil)).History)
		stamp := decodeMember[members.MemberDetail](t, do(t, r, http.MethodGet, memberPath(f.sheID), f.owner, nil)).UpdatedAt
		wantStatus(t, f.patch(f.owner, f.sheID, map[string]any{"role": "she_officer", "reason": "Same"}), 200)
		after := len(decodeMember[members.History](t, do(t, r, http.MethodGet, memberPath(f.sheID)+"/history", f.owner, nil)).History)
		if after != before {
			t.Errorf("history grew from %d to %d", before, after)
		}
		if now := decodeMember[members.MemberDetail](t, do(t, r, http.MethodGet, memberPath(f.sheID), f.owner, nil)).UpdatedAt; !now.Equal(stamp) {
			t.Errorf("updated_at moved from %v to %v: the row was written", stamp, now)
		}
	})

	t.Run("invited and removed members", func(t *testing.T) {
		rec := do(t, r, http.MethodPost, "/v1/invites", f.owner, map[string]string{"email": "late@" + f.emailSuffix, "full_name": "Late Joiner", "role": "supervisor"})
		wantStatus(t, rec, http.StatusCreated)
		inv := decodeMember[invites.InviteResponse](t, rec)
		wantError(t, f.patch(f.owner, inv.UserID, map[string]any{"role": "she_officer", "reason": "x"}), 409, members.ErrInviteRole.Error())
		wantError(t, f.patch(f.owner, inv.UserID, map[string]any{"status": "suspended", "reason": "x"}), 409, members.ErrTransition.Error())
		wantStatus(t, f.patch(f.owner, inv.UserID, map[string]any{"status": "removed", "reason": "Not joining"}), 200)
		// The invitation went with them.
		wantStatus(t, do(t, r, http.MethodPost, "/v1/invites/accept", "", map[string]string{"token": inv.Token, "password": "correcthorse1"}), http.StatusBadRequest)
		wantError(t, f.patch(f.owner, inv.UserID, map[string]any{"status": "active", "reason": "Changed mind"}), 409, members.ErrTransition.Error())
		// Back by a new invite, under the same user.
		again := decodeMember[invites.InviteResponse](t, do(t, r, http.MethodPost, "/v1/invites", f.she, map[string]string{"email": "late@" + f.emailSuffix, "full_name": "Late Joiner", "role": "supervisor"}))
		if again.UserID != inv.UserID {
			t.Errorf("re-invited as %s, was %s", again.UserID, inv.UserID)
		}
		listed := decodeMember[struct{ Members []members.Member }](t, do(t, r, http.MethodGet, "/v1/members?status=invited", f.owner, nil))
		if len(listed.Members) != 1 || listed.Members[0].UserID != inv.UserID {
			t.Errorf("invited list %+v", listed.Members)
		}
	})

	t.Run("a SHE officer manages the rest", func(t *testing.T) {
		wantStatus(t, f.patch(f.she, f.supID, map[string]any{"status": "suspended", "reason": "Site incident"}), 200)
		wantStatus(t, f.patch(f.she, f.supID, map[string]any{"status": "active", "reason": "Cleared"}), 200)
		if m := decodeMember[members.MemberDetail](t, do(t, r, http.MethodGet, memberPath(f.supID), f.she, nil)); !slices.Equal(m.SiteIDs, []string{f.siteA}) {
			t.Errorf("reinstated supervisor's sites %v, want site A kept", m.SiteIDs)
		}
	})
}

func deref(s *string) string {
	if s == nil {
		return "-"
	}
	return *s
}

func ids(s *[]string) string {
	if s == nil {
		return "-"
	}
	return strings.Join(*s, ",")
}

// D4: suspension is refused on the very next request, not after the access
// token's 15 minutes, and the refresh token dies with the session.
func TestMemberSuspensionIsImmediate(t *testing.T) {
	f := newTeamFixture(t, "revoke")
	pair := loginTokens(t, f.router, f.supMail)
	wantStatus(t, do(t, f.router, http.MethodGet, "/v1/me", pair.AccessToken, nil), http.StatusOK)

	wantStatus(t, f.patch(f.owner, f.supID, map[string]any{"status": "suspended", "reason": "Investigation"}), 200)
	wantStatus(t, do(t, f.router, http.MethodGet, "/v1/me", pair.AccessToken, nil), http.StatusUnauthorized)
	refresh(t, f.router, pair.RefreshToken, http.StatusUnauthorized)

	// Reinstated: the old session stays ended; a new sign-in works.
	wantStatus(t, f.patch(f.owner, f.supID, map[string]any{"status": "active", "reason": "Cleared"}), 200)
	wantStatus(t, do(t, f.router, http.MethodGet, "/v1/me", pair.AccessToken, nil), http.StatusUnauthorized)
	wantStatus(t, do(t, f.router, http.MethodGet, "/v1/me", login(t, f.router, f.supMail), nil), http.StatusOK)

	// A role change signs no one out.
	tok := login(t, f.router, f.supMail)
	wantStatus(t, f.patch(f.owner, f.supID, map[string]any{"role": "she_officer", "reason": "Promoted"}), 200)
	if got := me(t, f.router, tok); got.Role != auth.RoleSHEOfficer {
		t.Errorf("after the role change: %q", got.Role)
	}
}

// D4, forced: a phone's push is refused from the first record after the
// suspension. The first record is held on a lock (an uncommitted worker
// with the same identity key) while the owner suspends the supervisor; it
// then completes, and the second record meets RequireAuth and stops the
// batch.
func TestMemberSuspendedMidPush(t *testing.T) {
	f := newTeamFixture(t, "midpush")
	ctx := context.Background()
	name := "Blocked " + uuid.NewString()[:8]

	hold, err := testAdmin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Rollback(ctx)
	if _, err := hold.Exec(ctx, `INSERT INTO workers (id, org_id, name, created_by) VALUES ($1, $2, $3, $4)`,
		uuid.NewString(), f.org, name, f.ownerID); err != nil {
		t.Fatal(err)
	}

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- serveJSON(f.router, http.MethodPost, "/v1/sync/push", f.sup, map[string]any{"records": []map[string]any{
			rec(http.MethodPut, workerPath(uuid.NewString()), map[string]any{"name": name}),
			rec(http.MethodPut, workerPath(uuid.NewString()), map[string]any{"name": "Second " + name}),
			rec(http.MethodPut, workerPath(uuid.NewString()), map[string]any{"name": "Third " + name}),
		}})
	}()
	waitForWaiters(t, "INSERT INTO workers", 1)
	wantStatus(t, f.patch(f.owner, f.supID, map[string]any{"status": "suspended", "reason": "Phone lost"}), 200)
	if err := hold.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	res := <-done
	wantStatus(t, res, http.StatusOK)
	var out struct {
		Processed int `json:"processed"`
		Results   []struct {
			Status int `json:"status"`
		} `json:"results"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	var got []int
	for _, x := range out.Results {
		got = append(got, x.Status)
	}
	if out.Processed != 2 || !slices.Equal(got, []int{http.StatusCreated, http.StatusUnauthorized}) {
		t.Fatalf("processed %d, statuses %v; want 2, [201 401]", out.Processed, got)
	}
}

// D3 through the API, forced: two owners demote each other while the test
// holds the organisation's lock, so both requests are past their guards and
// waiting. Released, one demotes the other; the second is 409.
func TestLastOwnerRaceAPI(t *testing.T) {
	f := newTeamFixture(t, "ownerrace")
	ctx := context.Background()
	hold, err := testAdmin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Rollback(ctx)
	if _, err := hold.Exec(ctx, `SELECT 1 FROM organisations WHERE id = $1 FOR NO KEY UPDATE`, f.org); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	codes := make([]*httptest.ResponseRecorder, 2)
	for i, p := range [][2]string{{f.owner, f.owner2ID}, {f.owner2, f.ownerID}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = serveJSON(f.router, http.MethodPatch, memberPath(p[1]), p[0], map[string]any{"role": "she_officer", "reason": "Stepping back"})
		}()
	}
	waitForWaiters(t, "FROM organisations WHERE id", 2)
	if err := hold.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	wg.Wait()

	got := []int{codes[0].Code, codes[1].Code}
	slices.Sort(got)
	if !slices.Equal(got, []int{200, 409}) {
		t.Fatalf("statuses %v (%s / %s), want one 200 and one 409", got, codes[0].Body.String(), codes[1].Body.String())
	}
	for _, c := range codes {
		if c.Code == 409 {
			wantError(t, c, 409, members.ErrLastOwner.Error())
		}
	}
	owners := decodeMember[struct{ Members []members.Member }](t, do(t, f.router, http.MethodGet, "/v1/members?status=active", f.she, nil))
	n := 0
	for _, m := range owners.Members {
		if m.Role == auth.RoleOwner {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d active owners after the race, want 1", n)
	}
}

// D3, forced: an invite accept and a PATCH removing that invitee meet. The
// accept takes the organisation's lock before its invitation's, so while
// it waits for the organisation it holds nothing the PATCH needs: no
// deadlock (40P01), whichever goes first.
func TestInviteAcceptRacesMemberPatch(t *testing.T) {
	f := newTeamFixture(t, "acceptrace")
	ctx := context.Background()
	rec := do(t, f.router, http.MethodPost, "/v1/invites", f.owner, map[string]string{"email": "racer@" + f.emailSuffix, "full_name": "Racer", "role": "supervisor"})
	wantStatus(t, rec, http.StatusCreated)
	inv := decodeMember[invites.InviteResponse](t, rec)

	hold, err := testAdmin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Rollback(ctx)
	if _, err := hold.Exec(ctx, `SELECT 1 FROM organisations WHERE id = $1 FOR NO KEY UPDATE`, f.org); err != nil {
		t.Fatal(err)
	}
	accepted := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		accepted <- serveJSON(f.router, http.MethodPost, "/v1/invites/accept", "", map[string]string{"token": inv.Token, "password": "correcthorse1"})
	}()
	waitForWaiters(t, "FROM organisations WHERE id", 1)
	// The waiting accept holds no lock on its invitation.
	probe, err := testAdmin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := probe.Exec(ctx, `SELECT 1 FROM invitations WHERE id = $1 FOR UPDATE NOWAIT`, inv.ID); err != nil {
		t.Errorf("the waiting accept already holds its invitation: %v", err)
	}
	_ = probe.Rollback(ctx)

	removed := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		removed <- serveJSON(f.router, http.MethodPatch, memberPath(inv.UserID), f.owner, map[string]any{"status": "removed", "reason": "Wrong person"})
	}()
	waitForWaiters(t, "FROM organisations WHERE id", 2)
	if err := hold.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	a, p := <-accepted, <-removed
	for _, x := range []*httptest.ResponseRecorder{a, p} {
		if x.Code >= 500 {
			t.Fatalf("a request failed: %d %s", x.Code, x.Body.String())
		}
	}
	// Whichever went first, the member ends removed and signed out.
	if p.Code != 200 {
		t.Fatalf("PATCH: %d %s", p.Code, p.Body.String())
	}
	m := decodeMember[members.MemberDetail](t, do(t, f.router, http.MethodGet, memberPath(inv.UserID), f.owner, nil))
	if m.Status != "removed" {
		t.Fatalf("member is %s", m.Status)
	}
	if a.Code == 200 {
		var pair auth.TokenPair
		_ = json.Unmarshal(a.Body.Bytes(), &pair)
		wantStatus(t, do(t, f.router, http.MethodGet, "/v1/me", pair.AccessToken, nil), http.StatusUnauthorized)
	}
}

// D1: auth.MemberChangeAllowed and member_change_allowed (000054) agree for
// every actor, from and to role.
func TestMemberRulesMatchDatabase(t *testing.T) {
	ctx := context.Background()
	for _, a := range auth.Roles {
		for _, from := range auth.Roles {
			for _, to := range auth.Roles {
				var db bool
				if err := testAdmin.QueryRow(ctx, `SELECT member_change_allowed($1, $2, $3)`, a, from, to).Scan(&db); err != nil {
					t.Fatal(err)
				}
				if goes := auth.MemberChangeAllowed(a, from, to); goes != db {
					t.Errorf("%s changing a %s to %s: Go %v, database %v", a, from, to, goes, db)
				}
			}
		}
	}
	if got := auth.Capabilities(auth.RoleSHEOfficer); !slices.Contains(got, auth.CapManageMembers) {
		t.Errorf("SHE officer lacks manage_members: %v", got)
	}
}

// ONBOARD review R1: owner_eligible is whether the owner can still act on
// the action (action_site_member on its site). Moved to another role, taken
// off the site or suspended, the owner stops being eligible: the action says
// so everywhere leads look, a phone's next pull re-sends it, and the owner
// gets no more reminders about it. Nothing is reassigned.
func TestOwnerEligible(t *testing.T) {
	f := newActionFixture(t, "ownereligible")
	r := f.router
	ctx := context.Background()
	sfx := uuid.NewString()[:8]
	onSite := func(name string) string {
		id := inviteMember(t, r, f.owner, name+"-"+sfx+"@eligible.test", auth.RoleSupervisor)
		wantStatus(t, do(t, r, http.MethodPut, "/v1/sites/"+f.siteA+"/members/"+id, f.owner, nil), http.StatusNoContent)
		return id
	}
	moved, offSite, suspended := f.supervisorID, onSite("offsite"), onSite("suspended")
	raised := testClock.Now().Add(-time.Minute)
	acts := map[string]actions.Action{}
	for _, who := range []string{moved, offSite, suspended} {
		a := f.raise(t, f.owner, f.raiseBody(who, raised))
		if !a.OwnerEligible {
			t.Fatalf("a new action's owner %s isn't eligible", who)
		}
		acts[who] = a
	}
	_, cursor, _ := pullSite(t, r, f.owner, f.siteA, "", 200)

	wantStatus(t, serveJSON(r, http.MethodPatch, memberPath(moved), f.owner, map[string]any{"role": "bookkeeper", "reason": "Office work"}), 200)
	wantStatus(t, do(t, r, http.MethodDelete, "/v1/sites/"+f.siteA+"/members/"+offSite, f.owner, nil), http.StatusNoContent)
	wantStatus(t, serveJSON(r, http.MethodPatch, memberPath(suspended), f.owner, map[string]any{"status": "suspended", "reason": "Left site"}), 200)

	for who, a := range acts {
		if got := decodeAction(t, do(t, r, http.MethodGet, actionPath(f.siteA, a.ID), f.owner, nil)); got.OwnerEligible {
			t.Errorf("%s's action still shows its owner eligible", who)
		}
	}
	items, _, _ := pullSite(t, r, f.owner, f.siteA, cursor, 200)
	for who, a := range acts {
		resent := false
		for _, it := range items {
			resent = resent || (it.Kind == "action" && it.ID == a.ID && strings.Contains(string(it.Data), `"owner_eligible":false`))
		}
		if !resent {
			t.Errorf("the pull didn't re-send %s's action with owner_eligible false (%d items)", who, len(items))
		}
	}

	ids := func(path string) []string {
		t.Helper()
		rec := do(t, r, http.MethodGet, path, f.owner, nil)
		wantStatus(t, rec, 200)
		var out struct{ Actions []actions.ActionHeader }
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, a := range out.Actions {
			got = append(got, a.ID)
		}
		slices.Sort(got)
		return got
	}
	var want []string
	for _, a := range acts {
		want = append(want, a.ID)
	}
	slices.Sort(want)
	if got := ids("/v1/actions?owner_eligible=false"); !slices.Equal(got, want) {
		t.Errorf("owner_eligible=false: %v, want %v", got, want)
	}
	if got := ids("/v1/sites/" + f.siteA + "/actions?owner_eligible=true"); slices.ContainsFunc(got, func(id string) bool { return slices.Contains(want, id) }) {
		t.Errorf("owner_eligible=true lists an orphaned action: %v", got)
	}
	wantError(t, do(t, r, http.MethodGet, "/v1/actions?owner_eligible=maybe", f.owner, nil), 400, actions.ErrBadOwnerEligible.Error())

	m := decodeMember[members.MemberDetail](t, do(t, r, http.MethodGet, memberPath(suspended), f.owner, nil))
	if len(m.OpenWork.Actions) != 1 || m.OpenWork.Actions[0].ID != acts[suspended].ID || m.OpenWork.Actions[0].OwnerEligible {
		t.Errorf("open work %+v", m.OpenWork)
	}

	// Days later the actions are overdue: the summary flags each, and the
	// reminders skip their owners but still reach the leads.
	later := testClock.Now().Add(6 * 24 * time.Hour)
	s := siteSummary(t, routerAt(t, func() time.Time { return later }), f.owner, f.siteA)
	flagged := 0
	for _, it := range category(t, s, reports.CatActions).Items {
		if it.ActionID != nil && slices.Contains(want, *it.ActionID) {
			flagged++
			if it.OwnerEligible == nil || *it.OwnerEligible {
				t.Errorf("summary item owner_eligible %v", it.OwnerEligible)
			}
		}
	}
	if flagged != 3 {
		t.Errorf("%d of the 3 overdue actions in the summary", flagged)
	}
	jobs := &notifications.Jobs{Pool: testPool, Registry: testRegistry, Mailer: &recordingMailer{}, OnlyOrgs: []string{f.ownerMe.OrgID}, Clock: func() time.Time { return later }}
	if _, err := jobs.Reminders(ctx, later); err != nil {
		t.Fatal(err)
	}
	for who, a := range acts {
		var toOwner, toOthers int
		if err := testAdmin.QueryRow(ctx, `SELECT count(*) FILTER (WHERE user_id = $2), count(*) FILTER (WHERE user_id <> $2)
			FROM notifications WHERE subject_id = $1`, a.ID, who).Scan(&toOwner, &toOthers); err != nil {
			t.Fatal(err)
		}
		if toOwner != 0 || toOthers == 0 {
			t.Errorf("%s's action: %d reminders to its owner (want 0), %d to the leads (want some)", who, toOwner, toOthers)
		}
	}
}
