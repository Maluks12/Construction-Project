package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/files"
)

// GET /v1/me tells the dashboard who is signed in: role, name, organisation
// and capabilities, so it no longer guesses from 403s.
func TestMeShowsRoleAndNames(t *testing.T) {
	f := newEvidenceFixture(t, "me")

	got := me(t, f.router, f.owner)
	if got.Role != auth.RoleOwner || got.FullName != "Test Owner" || got.OrgName == "" {
		t.Errorf("owner: got role %q, name %q, org %q", got.Role, got.FullName, got.OrgName)
	}
	if got.SessionID == "" || got.UserID == "" || got.OrgID == "" {
		t.Errorf("owner: ids missing: %+v", got)
	}
	if !slices.Equal(got.Capabilities, auth.Capabilities(auth.RoleOwner)) {
		t.Errorf("owner capabilities %v", got.Capabilities)
	}

	sup := me(t, f.router, f.supervisor)
	if sup.Role != auth.RoleSupervisor || sup.FullName != "Test supervisor" || sup.OrgName != got.OrgName {
		t.Errorf("supervisor: got role %q, name %q, org %q", sup.Role, sup.FullName, sup.OrgName)
	}
	books := me(t, f.router, f.bookkeeper)
	if books.Capabilities == nil || len(books.Capabilities) != 0 {
		t.Errorf("bookkeeper capabilities: got %#v, want []", books.Capabilities)
	}
}

// Capabilities come from the live membership on every request: a role change
// shows on the next call, and a suspended member gets 401, not a stale list.
func TestMeFollowsLiveMembership(t *testing.T) {
	f := newEvidenceFixture(t, "melive")
	sup := me(t, f.router, f.supervisor)
	if slices.Contains(sup.Capabilities, auth.CapViewVault) {
		t.Fatalf("supervisor shows view_vault: %v", sup.Capabilities)
	}

	execAsTenant(t, sup.OrgID, 1, `UPDATE org_members SET role = 'she_officer' WHERE user_id = $1`, sup.UserID)
	now := me(t, f.router, f.supervisor)
	if now.Role != auth.RoleSHEOfficer || !slices.Equal(now.Capabilities, auth.Capabilities(auth.RoleSHEOfficer)) {
		t.Errorf("after the role change: role %q, capabilities %v", now.Role, now.Capabilities)
	}

	execAsTenant(t, sup.OrgID, 1, `UPDATE org_members SET status = 'suspended' WHERE user_id = $1`, sup.UserID)
	wantStatus(t, do(t, f.router, http.MethodGet, "/v1/me", f.supervisor, nil), http.StatusUnauthorized)
}

// TestCapabilitiesMatchRoutes holds auth.Capabilities to the real guards:
// for every role and capability, a representative endpoint answers past its
// guard exactly when the role has the capability. A supervisor's
// capabilities hold on their own site and stop at another (404).
func TestCapabilitiesMatchRoutes(t *testing.T) {
	f := newActionFixture(t, "caps")
	r := f.router
	tag := uuid.NewString()[:8]
	tokens := map[string]string{
		auth.RoleOwner:      f.owner,
		auth.RoleSupervisor: f.supervisor,
		auth.RoleBookkeeper: f.bookkeeper,
	}
	for _, role := range []string{auth.RoleSHEOfficer, auth.RoleClaimsHandler, auth.RoleContractsManager} {
		email := role + "@" + tag + ".caps.test"
		inviteMember(t, r, f.owner, email, role)
		tokens[role] = login(t, r, email)
	}

	action := f.raise(t, f.owner, f.raiseBody(f.ownerID, testClock.Now().Add(-time.Minute))).ID
	worker := newWorker(t, r, f.supervisor, map[string]any{"name": "Caps Worker " + tag})
	medFile := uploadFile(t, r, f.supervisor, files.PurposeMedical, nil)
	medical := uuid.NewString()
	clk := newSASTClock(t)
	wantStatus(t, do(t, r, http.MethodPut, workerPath(worker)+"/medicals/"+medical, f.supervisor,
		medicalBody(clk, medFile, "fit", clk.Today(), clk.Day(300))), http.StatusCreated)

	type probe struct {
		site bool // takes a site: a supervisor is also tried on another site
		call func(token, site string) *httptest.ResponseRecorder
	}
	get := func(path string) func(string, string) *httptest.ResponseRecorder {
		return func(token, _ string) *httptest.ResponseRecorder { return do(t, r, http.MethodGet, path, token, nil) }
	}
	probes := map[string]probe{
		auth.CapCaptureRecords: {true, func(token, site string) *httptest.ResponseRecorder {
			return do(t, r, http.MethodGet, "/v1/sites/"+site+"/evidence", token, nil)
		}},
		auth.CapReadSafetyFile: {true, func(token, site string) *httptest.ResponseRecorder {
			return do(t, r, http.MethodGet, "/v1/construction/sites/"+site+"/safety-file", token, nil)
		}},
		auth.CapWriteSafetyFile: {true, func(token, site string) *httptest.ResponseRecorder {
			return do(t, r, http.MethodPost, "/v1/construction/sites/"+site+"/safety-file/"+uuid.NewString()+"/retract",
				token, map[string]any{"reason": "probe"})
		}},
		auth.CapManageSites: {true, func(token, site string) *httptest.ResponseRecorder {
			return do(t, r, http.MethodGet, "/v1/sites/"+site+"/members", token, nil)
		}},
		auth.CapInviteMembers: {false, get("/v1/invites")},
		auth.CapViewVault:     {false, get("/v1/vault/documents")},
		auth.CapOpenMedicalCertificates: {false, func(token, _ string) *httptest.ResponseRecorder {
			return do(t, r, http.MethodPost, workerPath(worker)+"/medicals/"+medical+"/view", token, nil)
		}},
		auth.CapVerifyChain: {true, func(token, site string) *httptest.ResponseRecorder {
			return do(t, r, http.MethodGet, chainPath(site), token, nil)
		}},
		auth.CapWaiveFindings: {true, func(token, site string) *httptest.ResponseRecorder {
			return do(t, r, http.MethodPut, waiverPath(site, uuid.NewString()), token, map[string]any{
				"inspection_id": f.inspection, "item_key": f.item, "reason": "probe", "client_waived_at": rfc(testClock.Now())})
		}},
		auth.CapVerifyActions: {true, func(token, site string) *httptest.ResponseRecorder {
			return do(t, r, http.MethodPut, eventPath(site, action, uuid.NewString()), token,
				eventBody("verify", testClock.Now(), map[string]any{"closeout_id": uuid.NewString()}))
		}},
		auth.CapExportAuditPack: {true, func(token, site string) *httptest.ResponseRecorder {
			return do(t, r, http.MethodGet, "/v1/sites/"+site+"/exports", token, nil)
		}},
		auth.CapViewCompliance: {true, func(token, site string) *httptest.ResponseRecorder {
			return do(t, r, http.MethodGet, "/v1/sites/"+site+"/compliance", token, nil)
		}},
		auth.CapManageWorkers: {false, func(token, _ string) *httptest.ResponseRecorder {
			return do(t, r, http.MethodPost, workerPath(worker)+"/competencies/"+uuid.NewString()+"/retract",
				token, map[string]any{"reason": "probe"})
		}},
	}

	for _, c := range auth.AllCapabilities() {
		if _, ok := probes[c]; !ok {
			t.Errorf("capability %q has no probe", c)
		}
	}
	for c, p := range probes {
		for role, token := range tokens {
			has := slices.Contains(auth.Capabilities(role), c)
			rec := p.call(token, f.siteA)
			if passed := pastGuard(rec); passed != has {
				t.Errorf("%s as %s on its site: got %d %s; capability listed: %v", c, role, rec.Code, rec.Body.String(), has)
			}
			if p.site && has && role == auth.RoleSupervisor {
				rec := p.call(token, f.siteB)
				if rec.Code != http.StatusNotFound {
					t.Errorf("%s as supervisor on another site: got %d %s, want 404", c, rec.Code, rec.Body.String())
				}
			}
		}
	}
}

// pastGuard: the request got past the role and site guards. Every role guard
// answers 403 and the site scope 404 "site not found"; anything else (200,
// a 400 on the probe's body, a 404 for the probe's made-up record, a 409)
// means the guard let it through.
func pastGuard(rec *httptest.ResponseRecorder) bool {
	if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
		return false
	}
	var body struct{ Error string }
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return !(rec.Code == http.StatusNotFound && body.Error == "site not found")
}
