package evidence

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"construction_db/internal/core/auth"
	"construction_db/internal/core/sites"
	"construction_db/internal/platform/storage"
)

// vectorChain is CHAIN.md's three vectors as stored entries plus their head.
// Each call returns fresh copies, so a case can tamper freely.
func vectorChain(t *testing.T) ([]storedEntry, head) {
	t.Helper()
	var entries []storedEntry
	for _, v := range vectors(t) {
		entries = append(entries, storedEntry{
			SiteID:           v.SiteID,
			Seq:              v.Seq,
			ContentSHA256:    unhex(t, v.ContentSHA256),
			ServerReceivedAt: v.ServerReceivedAt,
			PrevChainHash:    unhex(t, v.PrevChainHash),
			MetadataHash:     unhex(t, v.MetadataHash),
			ChainHash:        unhex(t, v.ChainHash),
			Metadata:         v.metadata(),
			EvidenceStatus:   StatusVerified,
		})
	}
	last := entries[len(entries)-1]
	return entries, head{seq: last.Seq, hash: last.ChainHash, exists: true}
}

func walk(entries []storedEntry, h head, orphan string) *chainWalker {
	w := &chainWalker{prev: GenesisHash[:]}
	for _, e := range entries {
		w.add(e)
	}
	w.finish(h, orphan)
	return w
}

// reseal recomputes entries[i:]'s hashes and the head, as someone with write
// access to the database could after editing a sealed field.
func reseal(t *testing.T, entries []storedEntry, i int) head {
	t.Helper()
	for ; i < len(entries); i++ {
		e := &entries[i]
		if i > 0 {
			e.PrevChainHash = entries[i-1].ChainHash
		}
		meta, err := MetadataHash(e.Metadata)
		if err != nil {
			t.Fatal(err)
		}
		e.MetadataHash = meta[:]
		link, err := ChainHash(Link{ContentSHA256: e.ContentSHA256, ServerReceivedAt: e.ServerReceivedAt,
			SiteID: e.SiteID, PrevChainHash: e.PrevChainHash, MetadataHash: e.MetadataHash})
		if err != nil {
			t.Fatal(err)
		}
		e.ChainHash = link[:]
	}
	last := entries[len(entries)-1]
	return head{seq: last.Seq, hash: last.ChainHash, exists: true}
}

func TestChainWalker(t *testing.T) {
	seq := func(n int64) *int64 { return &n }

	tests := []struct {
		name    string
		tamper  func([]storedEntry, head) ([]storedEntry, head, string)
		reason  string // "" means intact
		atSeq   *int64
		orphan  bool
		wantLen int64
	}{
		{"as published", func(e []storedEntry, h head) ([]storedEntry, head, string) { return e, h, "" }, "", nil, false, 3},
		{"seq 2 deleted", func(e []storedEntry, h head) ([]storedEntry, head, string) {
			return []storedEntry{e[0], e[2]}, h, ""
		}, BreakSeqGap, seq(3), false, 2},
		{"prev of seq 2 changed", func(e []storedEntry, h head) ([]storedEntry, head, string) {
			e[1].PrevChainHash = GenesisHash[:]
			return e, h, ""
		}, BreakPrevMismatch, seq(2), false, 3},
		{"capture time of seq 2 moved 1µs", func(e []storedEntry, h head) ([]storedEntry, head, string) {
			e[1].Metadata.ClientCapturedAt = e[1].Metadata.ClientCapturedAt.Add(time.Microsecond)
			return e, h, ""
		}, BreakMetadataChanged, seq(2), false, 3},
		{"device of seq 1 changed", func(e []storedEntry, h head) ([]storedEntry, head, string) {
			d := "another-phone"
			e[0].Metadata.DeviceID = &d
			return e, h, ""
		}, BreakMetadataChanged, seq(1), false, 3},
		{"server time of seq 2 moved 1µs", func(e []storedEntry, h head) ([]storedEntry, head, string) {
			e[1].ServerReceivedAt = e[1].ServerReceivedAt.Add(time.Microsecond)
			return e, h, ""
		}, BreakChainHashMismatch, seq(2), false, 3},
		{"the earlier of two breaks wins", func(e []storedEntry, h head) ([]storedEntry, head, string) {
			e[2].ServerReceivedAt = e[2].ServerReceivedAt.Add(time.Microsecond)
			e[1].Metadata.ClientCapturedAt = e[1].Metadata.ClientCapturedAt.Add(time.Microsecond)
			return e, h, ""
		}, BreakMetadataChanged, seq(2), false, 3},
		{"evidence of seq 2 no longer verified", func(e []storedEntry, h head) ([]storedEntry, head, string) {
			e[1].EvidenceStatus = StatusPending
			return e, h, ""
		}, BreakEntryWithoutVerifiedEvidence, seq(2), false, 3},
		{"an unverified entry after a metadata break", func(e []storedEntry, h head) ([]storedEntry, head, string) {
			e[2].EvidenceStatus = StatusRejected
			e[1].Metadata.ClientCapturedAt = e[1].Metadata.ClientCapturedAt.Add(time.Microsecond)
			return e, h, ""
		}, BreakMetadataChanged, seq(2), false, 3},
		{"a hash break outranks status on the same entry", func(e []storedEntry, h head) ([]storedEntry, head, string) {
			e[1].EvidenceStatus = StatusPending
			e[1].ServerReceivedAt = e[1].ServerReceivedAt.Add(time.Microsecond)
			return e, h, ""
		}, BreakChainHashMismatch, seq(2), false, 3},
		{"seq 3 deleted, head left alone", func(e []storedEntry, h head) ([]storedEntry, head, string) {
			return e[:2], h, ""
		}, BreakHeadMismatch, seq(3), false, 2},
		{"head points past the last entry", func(e []storedEntry, h head) ([]storedEntry, head, string) {
			h.seq++
			return e, h, ""
		}, BreakHeadMismatch, seq(4), false, 3},
		{"head hash changed", func(e []storedEntry, h head) ([]storedEntry, head, string) {
			h.hash = e[1].ChainHash
			return e, h, ""
		}, BreakHeadMismatch, seq(3), false, 3},
		{"entries but no head", func(e []storedEntry, h head) ([]storedEntry, head, string) {
			return e, head{}, ""
		}, BreakHeadMismatch, seq(3), false, 3},
		{"a head but no entries", func(e []storedEntry, h head) ([]storedEntry, head, string) {
			return nil, h, ""
		}, BreakHeadMismatch, seq(3), false, 0},
		{"verified evidence missing from the chain", func(e []storedEntry, h head) ([]storedEntry, head, string) {
			return e, h, "orphan-id"
		}, BreakVerifiedWithoutEntry, nil, true, 3},
		{"empty site", func(e []storedEntry, h head) ([]storedEntry, head, string) {
			return nil, head{}, ""
		}, "", nil, false, 0},
		{"empty site with a genesis head", func(e []storedEntry, h head) ([]storedEntry, head, string) {
			return nil, head{seq: 0, hash: GenesisHash[:], exists: true}, ""
		}, "", nil, false, 0},
		// Known limits (CHAIN.md): undetectable without an external record of the head.
		{"truncated with the head rewound", func(e []storedEntry, h head) ([]storedEntry, head, string) {
			return e[:2], head{seq: 2, hash: e[1].ChainHash, exists: true}, ""
		}, "", nil, false, 2},
		{"rewritten from seq 2 on, every hash recomputed", func(e []storedEntry, h head) ([]storedEntry, head, string) {
			e[1].Metadata.ClientCapturedAt = e[1].Metadata.ClientCapturedAt.Add(time.Hour)
			return e, reseal(t, e, 1), ""
		}, "", nil, false, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries, h, orphan := tt.tamper(vectorChain(t))
			w := walk(entries, h, orphan)

			if w.length != tt.wantLen {
				t.Errorf("length %d, want %d", w.length, tt.wantLen)
			}
			if tt.reason == "" {
				if w.firstBreak != nil {
					t.Errorf("break %+v (seq %v), want intact", w.firstBreak, deref(w.firstBreak.Seq))
				}
				return
			}
			b := w.firstBreak
			if b == nil {
				t.Fatalf("intact, want %s", tt.reason)
			}
			if b.Reason != tt.reason || deref(b.Seq) != deref(tt.atSeq) || (b.Seq == nil) != (tt.atSeq == nil) {
				t.Errorf("break %s at seq %v, want %s at seq %v", b.Reason, deref(b.Seq), tt.reason, deref(tt.atSeq))
			}
			if tt.orphan && b.EvidenceID != "orphan-id" {
				t.Errorf("evidence id %q, want the orphan", b.EvidenceID)
			}
		})
	}
}

// An append that commits between reading the head and reading the entries
// must not show up as a break: VerifyChain reads one snapshot. Without it,
// the walk sees the new entry but the old head (head_mismatch).
func TestVerifyChainReadsOneSnapshot(t *testing.T) {
	ctx := context.Background()
	org, user, site := uuid.NewString(), uuid.NewString(), uuid.NewString()
	ids := []string{uuid.NewString(), uuid.NewString()}
	stmts := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO organisations (id, trading_name, primary_edition, enabled_editions) VALUES ($1, 'Snapshot', 'construction', ARRAY['construction'])`, []any{org}},
		{`INSERT INTO users (id, email, full_name) VALUES ($1, $2, 'Snapshot')`, []any{user, "snapshot-" + user + "@chain.test"}},
		{`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner')`, []any{org, user}},
		{`INSERT INTO sites (id, org_id, regime_id, name) SELECT $1, $2, id, 'Snapshot' FROM regulatory_regimes LIMIT 1`, []any{site, org}},
	}
	for _, id := range ids {
		stmts = append(stmts, struct {
			sql  string
			args []any
		}{`INSERT INTO evidence (id, org_id, site_id, uploaded_by, content_sha256, size_bytes, content_type, client_captured_at, object_key)
		   VALUES ($1, $2, $3, $4, $5, 1, 'image/jpeg', '2026-11-09T08:00:00Z', $6)`, []any{id, org, site, user, bytes.Repeat([]byte{3}, 32), "snapshot/" + id}})
	}
	for _, s := range stmts {
		if _, err := admin.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("fixture %q: %v", s.sql, err)
		}
	}

	s := &Service{pool: appPool, sites: sites.NewService(appPool, nil, "")}
	retain := time.Now().Add(24 * time.Hour)
	info := &storage.ObjectInfo{VersionID: "v1", LockMode: storage.LockGovernance, RetainUntil: &retain}
	if _, _, err := s.appendToChain(ctx, org, user, ids[0], info); err != nil {
		t.Fatal(err)
	}

	// Mid-walk, another connection appends the second entry and commits.
	afterHeadRead = func() {
		if _, _, err := s.appendToChain(ctx, org, user, ids[1], info); err != nil {
			t.Errorf("append mid-walk: %v", err)
		}
	}
	t.Cleanup(func() { afterHeadRead = func() {} })

	v, err := s.VerifyChain(ctx, org, user, auth.RoleOwner, site)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Intact || v.Length != 1 || v.HeadSeq != 1 {
		t.Errorf("got %+v (break %+v), want the chain as it was when the walk began: intact, length 1", v, v.FirstBreak)
	}

	afterHeadRead = func() {}
	if v, err := s.VerifyChain(ctx, org, user, auth.RoleOwner, site); err != nil || !v.Intact || v.Length != 2 {
		t.Errorf("after the append: %+v, err %v; want intact, length 2", v, err)
	}
}

func deref(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}
