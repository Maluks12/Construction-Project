package auth

import (
	"slices"
	"strings"
	"testing"

	"construction_db/internal/platform/apidoctest"
)

func TestAPIDoc(t *testing.T) {
	doc := apidoctest.Load(t, "me")
	doc.CheckStruct(t, "Me", MeResponse{}, false)
	doc.CheckUnion(t, "Capability", AllCapabilities())
	doc.CheckUnion(t, "Role", Roles)
	if quoted := `"unauthorized"`; !strings.Contains(doc.Text, quoted) {
		t.Errorf("the reference doesn't quote the error %s", quoted)
	}

	// The capability table: one row per capability, its ticks matching
	// Capabilities for each role column.
	columns := [][]string{{RoleOwner}, {RoleSHEOfficer}, {RoleSupervisor},
		{RoleClaimsHandler, RoleContractsManager, RoleBookkeeper}}
	for _, c := range AllCapabilities() {
		var row string
		for _, line := range strings.Split(doc.Text, "\n") {
			if strings.HasPrefix(line, "| `"+c+"` |") {
				row = line
			}
		}
		if row == "" {
			t.Errorf("no table row for capability %q", c)
			continue
		}
		cells := strings.Split(strings.Trim(row, "|"), "|")
		if len(cells) != 2+len(columns) {
			t.Errorf("row for %q has %d cells", c, len(cells))
			continue
		}
		for i, roles := range columns {
			ticked := strings.TrimSpace(cells[2+i]) == "✓"
			for _, role := range roles {
				if has := slices.Contains(Capabilities(role), c); has != ticked {
					t.Errorf("%s for %s: table says %v, Capabilities says %v", c, role, ticked, has)
				}
			}
		}
	}
}
