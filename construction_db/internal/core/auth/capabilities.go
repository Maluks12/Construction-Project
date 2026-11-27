package auth

import "slices"

// Capabilities are display hints (M12 D16): no handler reads them, and
// TestCapabilitiesMatchRoutes holds them to today's guards. Change both
// together when the role matrix is approved.
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
	CapViewCompliance          = "view_compliance"
	CapExportAuditPack         = "export_audit_pack"
	CapManageMembers           = "manage_members"
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
	CapViewCompliance:          fieldRoles,
	CapExportAuditPack:         leadRoles,
	CapManageMembers:           memberManagers(),
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
