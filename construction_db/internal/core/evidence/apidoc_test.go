package evidence

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"construction_db/internal/platform/apidoctest"
)

// The TypeScript in docs/api/evidence.md must match the JSON the Go code
// sends, field for field, so the frontend can mirror it without guessing.
func TestAPIDoc(t *testing.T) {
	doc := apidoctest.Load(t, "evidence")

	for name, v := range map[string]any{
		"Evidence": Evidence{}, "ChainEntry": ChainEntry{}, "Upload": Upload{},
		"ChainVerification": ChainVerification{}, "ChainBreak": ChainBreak{},
		"ViewURL": ViewURL{},
	} {
		t.Run(name, func(t *testing.T) { doc.CheckStruct(t, name, v, false) })
	}
	t.Run("DeclareRequest", func(t *testing.T) { doc.CheckStruct(t, "DeclareRequest", DeclareRequest{}, true) })

	// Bodies the handlers build inline.
	doc.CheckFields(t, "EvidenceList", []string{"evidence"})
	doc.CheckFields(t, "EvidenceRejection", []string{"error", "rejected_reason"})

	doc.CheckUnion(t, "EvidenceStatus", []string{StatusPending, StatusVerified, StatusRejected})
	doc.CheckUnion(t, "RejectedReason", slices.Collect(maps.Keys(rejections)))
	doc.CheckUnion(t, "ChainBreakReason", []string{BreakSeqGap, BreakPrevMismatch, BreakMetadataChanged, BreakChainHashMismatch,
		BreakEntryWithoutVerifiedEvidence, BreakHeadMismatch, BreakVerifiedWithoutEntry})

	// Each rejection's code and exact message appear in the doc's table.
	for reason, msg := range rejections {
		if row := "| `" + reason + "` | `" + msg + "` |"; !strings.Contains(doc.Text, row) {
			t.Errorf("doc has no row %q", row)
		}
	}
}
