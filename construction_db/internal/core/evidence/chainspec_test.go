package evidence

import (
	"strings"
	"testing"
)

// The pack's CHAIN.md keeps the specification and drops the repo's notes.
func TestChainSpec(t *testing.T) {
	spec := ChainSpec()
	for _, want := range []string{"## The formulas", "### Chain preimage: 120 bytes", "### Metadata preimage", "### Rules",
		"## Test vectors", `"chain_hash": "748d7329ca53f26b4f97fdc986a9b143d44e0214eea6b0e5f30a5b457fbfa82a"`,
		"## Verifying a site's chain in SQL", "tamper-proof"} {
		if !strings.Contains(spec, want) {
			t.Errorf("the spec lacks %q", want)
		}
	}
	for _, not := range []string{"chain_test.go", "TestRecomputeSQL", "CLAUDE.md", "<!--", "repo-only", "\n\n\n"} {
		if strings.Contains(spec, not) {
			t.Errorf("the spec still has %q", not)
		}
	}
}
