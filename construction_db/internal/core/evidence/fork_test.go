package evidence

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"construction_db/internal/platform/storage"
)

// Anything that writes chain entries without the head lock (a bug, or a
// person with database access) must fail on a constraint, not fork the chain.
func TestForkFailsLoudly(t *testing.T) {
	ctx := context.Background()
	// Fresh ids each run, so -count=N works against the shared database.
	org, user, site, other := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	ids := map[string][]string{}
	for _, s := range []string{site, other} {
		for range 6 {
			ids[s] = append(ids[s], uuid.NewString())
		}
	}
	evidenceID := func(site string, n int) string { return ids[site][n] }
	sum := bytes.Repeat([]byte{5}, 32)

	exec := func(t *testing.T, sql string, args ...any) {
		t.Helper()
		if _, err := admin.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql, err)
		}
	}
	exec(t, `INSERT INTO organisations (id, trading_name, primary_edition, enabled_editions) VALUES ($1, 'Fork', 'construction', ARRAY['construction'])`, org)
	exec(t, `INSERT INTO users (id, email, full_name) VALUES ($1, $2, 'Fork')`, user, "fork-"+user+"@chain.test")
	exec(t, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner')`, org, user)
	for _, s := range []string{site, other} {
		exec(t, `INSERT INTO sites (id, org_id, regime_id, name) SELECT $1, $2, id, 'Fork' FROM regulatory_regimes LIMIT 1`, s, org)
		for n := range 6 {
			exec(t, `INSERT INTO evidence (id, org_id, site_id, uploaded_by, content_sha256, size_bytes, content_type, client_captured_at, object_key)
			         VALUES ($1, $2, $3, $4, $5, 1, 'image/jpeg', '2026-11-09T08:00:00Z', $6)`, evidenceID(s, n), org, s, user, sum, "fork/"+evidenceID(s, n))
		}
	}

	// A real seq 1 and seq 2 on the site, appended the normal way. A fork
	// needs a second entry after an entry that already has a successor.
	retain := time.Now().Add(24 * time.Hour)
	info := &storage.ObjectInfo{VersionID: "v1", LockMode: storage.LockGovernance, RetainUntil: &retain}
	s := &Service{pool: appPool}
	first, _, err := s.appendToChain(ctx, org, user, evidenceID(site, 0), info)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.appendToChain(ctx, org, user, evidenceID(site, 5), info); err != nil {
		t.Fatal(err)
	}
	firstHash := first.Chain.ChainHash

	insert := func(site string, n int, seq int64, prev []byte, chain string) error {
		_, err := admin.Exec(ctx, `
			INSERT INTO evidence_chain (evidence_id, org_id, site_id, seq, content_sha256, server_received_at, prev_chain_hash, metadata_hash, chain_hash)
			VALUES ($1, $2, $3, $4, $5, now(), $6, $7, decode($8, 'hex'))`,
			evidenceID(site, n), org, site, seq, sum, prev, bytes.Repeat([]byte{9}, 32), chain)
		return err
	}
	fresh := func(b byte) string { return fmt.Sprintf("%064x", bytes.Repeat([]byte{b}, 32)) }
	firstBytes := mustHex(t, firstHash)

	tests := []struct {
		name       string
		insert     func() error
		constraint string // "" means accepted
	}{
		// Unused seqs, so each case trips only the constraint it names.
		{"a second genesis entry", func() error { return insert(site, 1, 8, GenesisHash[:], fresh(0xA1)) }, "evidence_chain_site_prev_unique"},
		{"a second entry after seq 1", func() error { return insert(site, 2, 7, firstBytes, fresh(0xA2)) }, "evidence_chain_site_prev_unique"},
		{"another entry claiming seq 1", func() error { return insert(site, 3, 1, bytes.Repeat([]byte{0xB3}, 32), fresh(0xA3)) }, "evidence_chain_site_seq_unique"},
		{"an entry copying a chain hash", func() error { return insert(site, 4, 9, bytes.Repeat([]byte{0xB4}, 32), firstHash) }, "evidence_chain_site_hash_unique"},
		{"the same genesis on another site", func() error { return insert(other, 0, 1, GenesisHash[:], firstHash) }, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.insert()
			if tt.constraint == "" {
				if err != nil {
					t.Errorf("got %v, want it accepted: the constraints are per site", err)
				}
				return
			}
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23505" || pgErr.ConstraintName != tt.constraint {
				t.Errorf("got %v, want a unique violation on %s", err, tt.constraint)
			}
		})
	}

	var entries int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM evidence_chain WHERE site_id = $1`, site).Scan(&entries); err != nil || entries != 2 {
		t.Errorf("site has %d entries (err %v), want only the real seq 1 and 2", entries, err)
	}
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	return unhex(t, s)
}
