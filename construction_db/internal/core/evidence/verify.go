package evidence

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"construction_db/internal/core/sites"
	"construction_db/internal/platform/database"
)

// afterHeadRead does nothing; a test replaces it to commit an append between
// the walk's two reads, which only a snapshot hides.
var afterHeadRead = func() {}

// VerifyChain walks a site's chain as stored and reports the first break.
// It runs in one snapshot: an append committing mid-walk would otherwise look
// like an entry past the head.
func (s *Service) VerifyChain(ctx context.Context, orgID, userID, role, siteID string) (*ChainVerification, error) {
	var v *ChainVerification
	err := database.WithTenantSnapshot(ctx, s.pool, database.Tenant{OrgID: orgID, UserID: userID}, func(tx pgx.Tx) error {
		site, err := s.sites.GetInTx(ctx, tx, orgID, userID, role, siteID)
		if err != nil {
			return err
		}
		headSeq, headHash, hasHead, err := GetHead(ctx, tx, orgID, site.ID)
		if err != nil {
			return err
		}
		afterHeadRead()

		w := &chainWalker{prev: GenesisHash[:]}
		if err := StreamChain(ctx, tx, orgID, site.ID, func(e storedEntry) error {
			w.add(e)
			return nil
		}); err != nil {
			return err
		}
		orphan, err := VerifiedWithoutEntry(ctx, tx, orgID, site.ID)
		if err != nil {
			return err
		}
		w.finish(head{seq: headSeq, hash: headHash, exists: hasHead}, orphan)

		checked, err := Now(ctx, tx)
		if err != nil {
			return err
		}
		if !hasHead {
			headHash = GenesisHash[:]
		}
		v = &ChainVerification{
			SiteID:     site.ID,
			Intact:     w.firstBreak == nil,
			Length:     w.length,
			HeadSeq:    headSeq,
			HeadHash:   hex.EncodeToString(headHash),
			FirstBreak: w.firstBreak,
			CheckedAt:  checked,
		}
		return nil
	})
	if errors.Is(err, sites.ErrSiteNotFound) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("verify chain: %w", err)
	}
	return v, nil
}

type head struct {
	seq    int64
	hash   []byte
	exists bool
}

// chainWalker checks entries one at a time in seq order, keeping only what
// the next entry needs, so memory doesn't grow with the chain.
type chainWalker struct {
	length     int64
	prev       []byte // the previous entry's chain_hash; genesis at first
	lastEntry  *storedEntry
	firstBreak *ChainBreak
}

func (w *chainWalker) add(e storedEntry) {
	w.length++
	w.lastEntry = &e
	if w.firstBreak != nil {
		return // keep counting; only the first break is reported
	}
	if reason := w.check(e); reason != "" {
		seq := e.Seq
		w.firstBreak = &ChainBreak{Seq: &seq, EvidenceID: e.Metadata.EvidenceID, Reason: reason}
	}
	w.prev = e.ChainHash
}

// check returns why e breaks the chain, or "". The order is the documented one.
func (w *chainWalker) check(e storedEntry) string {
	if e.Seq != w.length {
		return BreakSeqGap
	}
	if !bytes.Equal(e.PrevChainHash, w.prev) {
		return BreakPrevMismatch
	}
	// A stored value that can't be encoded any more can't be what was sealed.
	meta, err := MetadataHash(e.Metadata)
	if err != nil || !bytes.Equal(meta[:], e.MetadataHash) {
		return BreakMetadataChanged
	}
	link, err := ChainHash(Link{
		ContentSHA256:    e.ContentSHA256,
		ServerReceivedAt: e.ServerReceivedAt,
		SiteID:           e.SiteID,
		PrevChainHash:    e.PrevChainHash,
		MetadataHash:     e.MetadataHash,
	})
	if err != nil || !bytes.Equal(link[:], e.ChainHash) {
		return BreakChainHashMismatch
	}
	if e.EvidenceStatus != StatusVerified {
		return BreakEntryWithoutVerifiedEvidence
	}
	return ""
}

// finish compares the head row with the last entry, then looks for verified
// evidence that never made it into the chain.
func (w *chainWalker) finish(h head, orphanEvidenceID string) {
	if w.firstBreak != nil {
		return
	}
	var headMatches bool
	var lastSeq int64
	evidenceID := ""
	if w.lastEntry == nil {
		// No entries: no head, or a head still at genesis.
		headMatches = !h.exists || (h.seq == 0 && bytes.Equal(h.hash, GenesisHash[:]))
	} else {
		lastSeq, evidenceID = w.lastEntry.Seq, w.lastEntry.Metadata.EvidenceID
		headMatches = h.exists && h.seq == lastSeq && bytes.Equal(h.hash, w.lastEntry.ChainHash)
	}
	if !headMatches {
		seq := max(h.seq, lastSeq)
		w.firstBreak = &ChainBreak{Seq: &seq, EvidenceID: evidenceID, Reason: BreakHeadMismatch}
		return
	}
	if orphanEvidenceID != "" {
		w.firstBreak = &ChainBreak{EvidenceID: orphanEvidenceID, Reason: BreakVerifiedWithoutEntry}
	}
}
