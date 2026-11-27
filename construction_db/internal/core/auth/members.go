package auth

import "slices"

// memberRule is what one role may do to other members (ONBOARD D1): change
// members and give them roles, and whether that reaches the owner role.
type memberRule struct {
	ChangesOwners bool
}

// memberRules is the one table of who may change whom, until the role
// matrix is approved. Its SQL twin is member_change_allowed (000054), which
// the org_members guard calls; TestMemberRulesMatchDatabase compares them
// for every combination of roles. A role not listed changes no one.
var memberRules = map[string]memberRule{
	RoleOwner:      {ChangesOwners: true},
	RoleSHEOfficer: {ChangesOwners: false},
}

// MemberChangeAllowed reports whether actor may change a member whose role
// is from, giving them to (from == to for a status change). Nobody changes
// themselves, which the caller checks.
func MemberChangeAllowed(actor, from, to string) bool {
	r, ok := memberRules[actor]
	if !ok {
		return false
	}
	return r.ChangesOwners || (from != RoleOwner && to != RoleOwner)
}

// memberManagers are the roles with any rule: the manage_members capability.
func memberManagers() []string {
	var out []string
	for _, role := range Roles {
		if _, ok := memberRules[role]; ok {
			out = append(out, role)
		}
	}
	return slices.Clip(out)
}
