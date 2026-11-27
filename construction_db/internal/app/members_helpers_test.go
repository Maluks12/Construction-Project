package app

import (
	"net/http"
	"testing"
)

// changeMember PATCHes a member as token (an owner or SHE officer) and wants
// 200; body carries role and/or status, and the reason defaults to one.
func changeMember(t *testing.T, router http.Handler, token, userID string, body map[string]any) {
	t.Helper()
	if _, ok := body["reason"]; !ok {
		body["reason"] = "Test fixture"
	}
	wantStatus(t, do(t, router, http.MethodPatch, "/v1/members/"+userID, token, body), http.StatusOK)
}
