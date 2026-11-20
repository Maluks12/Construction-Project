package auth

import "slices"

// Capabilities are display hints for the apps (M12 D16): what the caller's
// role lets them do, so a screen can hide a button instead of probing for a
// 403. No handler reads them; every route keeps its own guard, and
// TestCapabilitiesMatchRoutes holds this table to those guards. For a
// supervisor a capability means "on your own sites".
//
// The table encodes today's guards, not the role matrix awaiting approval.
// When the matrix is approved, this table and the guards change together.
const (
	CapCaptureRecords          = "capture_records"
	CapReadSafetyFile          = "read_safety_file"
	CapWriteSafetyFile         = "write_safety_file"
	CapManageSites             = "manage_sites"
	CapInviteMembers           = "invite_members"
	CapViewVault               = "view_vault"
	CapOpenMedicalCertificates = "open_medical_certificates"
	CapVerifyChain             = "verify_chain"
	CapWaiveFindings           = "waive_findings"
	CapVerifyActions           = "verify_actions"
	CapManageWorkers           = "manage_workers"
)

var (
	fieldRoles = []string{RoleOwner, RoleSHEOfficer, RoleSupervisor}
	leadRoles  = []string{RoleOwner, RoleSHEOfficer}
)

// capabilityRoles maps each capability to the roles whose guard admits it.
var capabilityRoles = map[string][]string{
	CapCaptureRecords:          fieldRoles,
	CapReadSafetyFile:          fieldRoles,
	CapWriteSafetyFile:         leadRoles,
	CapManageSites:             leadRoles,
	CapInviteMembers:           leadRoles,
	CapViewVault:               leadRoles,
	CapOpenMedicalCertificates: leadRoles,
	CapVerifyChain:             leadRoles,
	CapWaiveFindings:           leadRoles,
	CapVerifyActions:           leadRoles,
	CapManageWorkers:           leadRoles,
}

// AllCapabilities lists every capability, sorted.
func AllCapabilities() []string {
	out := make([]string, 0, len(capabilityRoles))
	for c := range capabilityRoles {
		out = append(out, c)
	}
	slices.Sort(out)
	return out
}

// Capabilities returns the role's capabilities, sorted; [] for none.
func Capabilities(role string) []string {
	out := []string{}
	for _, c := range AllCapabilities() {
		if slices.Contains(capabilityRoles[c], role) {
			out = append(out, c)
		}
	}
	return out
}

// Roles lists every organisation role.
var Roles = []string{RoleOwner, RoleSHEOfficer, RoleSupervisor, RoleClaimsHandler, RoleContractsManager, RoleBookkeeper}
