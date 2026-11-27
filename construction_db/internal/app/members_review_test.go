package app

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"construction_db/internal/core/actions"
	"construction_db/internal/core/auth"
	"construction_db/internal/core/invites"
)

func loginRec(router http.Handler, email, orgID string) int {
	body := map[string]string{"email": email, "password": "correcthorse1", "device_name": "test"}
	if orgID != "" {
		body["org_id"] = orgID
	}
	return serveJSON(router, http.MethodPost, "/v1/auth/login", "", body).Code
}

// ONBOARD review R4: a suspended or removed member can't sign in to that
// organisation; someone suspended in A but active in B signs in to B only;
// a refresh after the suspension is refused.
func TestSuspendedMembersCantSignIn(t *testing.T) {
	f := newTeamFixture(t, "signin")
	r := f.router
	pair := loginTokens(t, r, f.supMail)

	// Active in a second organisation too.
	other := signup(t, r, "SignIn Other Co", "owner@other-"+f.emailSuffix)
	rec := do(t, r, http.MethodPost, "/v1/invites", other, map[string]string{"email": f.supMail, "full_name": "Two Orgs", "role": "supervisor"})
	wantStatus(t, rec, http.StatusCreated)
	inv := decodeMember[invites.InviteResponse](t, rec)
	wantStatus(t, do(t, r, http.MethodPost, "/v1/invites/accept", "", map[string]string{"token": inv.Token, "password": "correcthorse1"}), http.StatusOK)
	otherOrg := me(t, r, other).OrgID
	if got := loginRec(r, f.supMail, ""); got != http.StatusBadRequest {
		t.Fatalf("a member of two organisations without org_id: %d, want 400 (org_id is required)", got)
	}

	wantStatus(t, f.patch(f.owner, f.supID, map[string]any{"status": "suspended", "reason": "Investigation"}), 200)
	refresh(t, r, pair.RefreshToken, http.StatusUnauthorized)
	if got := loginRec(r, f.supMail, f.org); got != http.StatusBadRequest {
		t.Errorf("sign in to the organisation that suspended them: %d, want 400 (not a member)", got)
	}
	// Only B is left: signing in without naming it lands there.
	if got := me(t, r, loginTokens(t, r, f.supMail).AccessToken).OrgID; got != otherOrg {
		t.Errorf("signed in to %s, want the other organisation %s", got, otherOrg)
	}

	// Suspended or removed with no other organisation: 401, the same as a
	// wrong password, so the answer says nothing about the account.
	wantStatus(t, f.patch(f.owner, f.sheID, map[string]any{"status": "removed", "reason": "Left"}), 200)
	if got := loginRec(r, "she@"+f.emailSuffix, ""); got != http.StatusUnauthorized {
		t.Errorf("a removed member signing in: %d, want 401", got)
	}
	wantStatus(t, f.patch(f.owner, f.owner2ID, map[string]any{"status": "suspended", "reason": "Away"}), 200)
	if got := loginRec(r, f.owner2Email, ""); got != http.StatusUnauthorized {
		t.Errorf("a suspended member signing in: %d, want 401", got)
	}
}

// ONBOARD review R4: inviting someone who is already a member, suspended
// or active, is 409, never a 500 from the database's rules.
func TestInviteExistingMember(t *testing.T) {
	f := newTeamFixture(t, "reinvite")
	wantStatus(t, f.patch(f.owner, f.supID, map[string]any{"status": "suspended", "reason": "Away"}), 200)
	for _, email := range []string{f.supMail, "she@" + f.emailSuffix} {
		rec := do(t, f.router, http.MethodPost, "/v1/invites", f.owner, map[string]string{"email": email, "full_name": "Again", "role": "supervisor"})
		wantError(t, rec, http.StatusConflict, invites.ErrAlreadyMember.Error())
	}
}

// ONBOARD review R2: an action can't be raised for, or reassigned to, a
// member who was suspended or removed.
func TestActionOwnerMustBeActive(t *testing.T) {
	f := newActionFixture(t, "inactiveowner")
	r := f.router
	sfx := uuid.NewString()[:8]
	member := func(name string) string {
		id := inviteMember(t, r, f.owner, name+"-"+sfx+"@inactive.test", auth.RoleSupervisor)
		wantStatus(t, do(t, r, http.MethodPut, "/v1/sites/"+f.siteA+"/members/"+id, f.owner, nil), http.StatusNoContent)
		return id
	}
	suspended, removed := member("suspended"), member("removed")
	wantStatus(t, serveJSON(r, http.MethodPatch, memberPath(suspended), f.owner, map[string]any{"status": "suspended", "reason": "x"}), 200)
	wantStatus(t, serveJSON(r, http.MethodPatch, memberPath(removed), f.owner, map[string]any{"status": "removed", "reason": "x"}), 200)

	act := f.raise(t, f.owner, f.raiseBody(f.supervisorID, testClock.Now().Add(-time.Minute)))
	for _, who := range []string{suspended, removed} {
		wantError(t, do(t, r, http.MethodPut, actionPath(f.siteA, uuid.NewString()), f.owner, f.raiseBody(who, testClock.Now().Add(-time.Minute))),
			http.StatusBadRequest, actions.ErrOwnerNotEligible.Error())
		wantError(t, do(t, r, http.MethodPut, actionPath(f.siteA, act.ID)+"/events/"+uuid.NewString(), f.owner,
			eventBody("reassign", testClock.Now(), map[string]any{"owner_id": who})), http.StatusBadRequest, actions.ErrOwnerNotEligible.Error())
	}
}
