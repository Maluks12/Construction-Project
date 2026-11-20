package evidence

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"
)

// ChainRecord is one chain entry with every hash input (CHAIN.md), as an
// audit pack lists it so anyone can recompute the chain without us.
type ChainRecord struct {
	Seq              int64     `json:"seq"`
	EvidenceID       string    `json:"evidence_id"`
	ContentSHA256    string    `json:"content_sha256"`
	ServerReceivedAt time.Time `json:"server_received_at"`
	ClientCapturedAt time.Time `json:"client_captured_at"`
	Lat              *float64  `json:"lat"`
	Lng              *float64  `json:"lng"`
	UploadedBy       string    `json:"uploaded_by"`
	DeviceID         *string   `json:"device_id"`
	PrevChainHash    string    `json:"prev_chain_hash"`
	MetadataHash     string    `json:"metadata_hash"`
	ChainHash        string    `json:"chain_hash"`
}

// VerifyChainAsOf walks a site's chain in the caller's transaction (an audit
// pack's snapshot, M12 D10) as it stood at asOf: the entries the server had
// received by then, which are a prefix of the chain because entries are
// numbered in order of receipt. each gets every entry of that prefix.
//
// When nothing was received after asOf, the head row is checked as
// VerifyChain checks it. Otherwise the prefix's last entry is the head at
// asOf. Verified evidence with no entry is a break either way: it is wrong
// now, whenever it happened. CheckedAt is asOf, so the result is the same
// on every rebuild of the same data.
func VerifyChainAsOf(ctx context.Context, db DBTX, orgID, siteID string, asOf time.Time, each func(ChainRecord) error) (*ChainVerification, error) {
	w := &chainWalker{prev: GenesisHash[:]}
	later := false
	err := StreamChain(ctx, db, orgID, siteID, func(e storedEntry) error {
		if e.ServerReceivedAt.After(asOf) {
			later = true
			return nil
		}
		w.add(e)
		m := e.Metadata
		return each(ChainRecord{
			Seq: e.Seq, EvidenceID: m.EvidenceID, ContentSHA256: hex.EncodeToString(e.ContentSHA256),
			ServerReceivedAt: e.ServerReceivedAt.UTC(), ClientCapturedAt: m.ClientCapturedAt.UTC(),
			Lat: m.Lat, Lng: m.Lng, UploadedBy: m.UploadedBy, DeviceID: m.DeviceID,
			PrevChainHash: hex.EncodeToString(e.PrevChainHash), MetadataHash: hex.EncodeToString(e.MetadataHash),
			ChainHash: hex.EncodeToString(e.ChainHash),
		})
	})
	if err != nil {
		return nil, fmt.Errorf("verify chain as of: %w", err)
	}

	h := head{seq: 0, hash: GenesisHash[:], exists: true}
	if w.lastEntry != nil {
		h = head{seq: w.lastEntry.Seq, hash: w.lastEntry.ChainHash, exists: true}
	}
	if !later {
		seq, hash, ok, err := GetHead(ctx, db, orgID, siteID)
		if err != nil {
			return nil, fmt.Errorf("verify chain as of: %w", err)
		}
		h = head{seq: seq, hash: hash, exists: ok}
	}
	orphan, err := VerifiedWithoutEntry(ctx, db, orgID, siteID)
	if err != nil {
		return nil, fmt.Errorf("verify chain as of: %w", err)
	}
	w.finish(h, orphan)

	headSeq, headHash := int64(0), GenesisHash[:]
	if w.lastEntry != nil {
		headSeq, headHash = w.lastEntry.Seq, w.lastEntry.ChainHash
	}
	return &ChainVerification{
		SiteID:     siteID,
		Intact:     w.firstBreak == nil,
		Length:     w.length,
		HeadSeq:    headSeq,
		HeadHash:   hex.EncodeToString(headHash),
		FirstBreak: w.firstBreak,
		CheckedAt:  asOf.UTC(),
	}, nil
}
