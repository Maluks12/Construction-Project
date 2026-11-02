package database_test

import (
	"context"
	"testing"

	"construction_db/internal/platform/database"
)

// Captured evidence and chain entries can't be edited by the app, and once
// evidence is verified or rejected only its retention can grow.
func TestEvidenceIsAppendOnly(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if err := database.SetTenant(ctx, tx, database.Tenant{OrgID: rlsOrgA}); err != nil {
		t.Fatalf("set tenant: %v", err)
	}

	const pendingID = "29000000-0000-0000-0000-00000000000a"
	if _, err := tx.Exec(ctx, `INSERT INTO evidence (id, org_id, site_id, uploaded_by, content_sha256, size_bytes, content_type, client_captured_at, object_key)
		VALUES ($1, $2, $3, $4, $5, 1, 'image/jpeg', now(), 'rls/pending-a')`, pendingID, rlsOrgA, rlsSiteA, rlsUserA, hash32(7)); err != nil {
		t.Fatalf("insert pending evidence: %v", err)
	}

	tests := []struct {
		name     string
		sql      string
		args     []any
		wantCode string // "" means it succeeds and changes one row
	}{
		{"captured time", `UPDATE evidence SET client_captured_at = now() WHERE id = $1`, []any{pendingID}, "42501"},
		{"content hash", `UPDATE evidence SET content_sha256 = $2 WHERE id = $1`, []any{rlsEvidenceA, hash32(8)}, "42501"},
		{"delete evidence", `DELETE FROM evidence WHERE id = $1`, []any{pendingID}, "42501"},
		{"chain entry", `UPDATE evidence_chain SET chain_hash = $2 WHERE evidence_id = $1`, []any{rlsEvidenceA, hash32(8)}, "42501"},
		{"delete chain entry", `DELETE FROM evidence_chain WHERE evidence_id = $1`, []any{rlsEvidenceA}, "42501"},
		{"verified to rejected", `UPDATE evidence SET status = 'rejected', rejected_reason = 'x' WHERE id = $1`, []any{rlsEvidenceA}, "23514"},
		{"version of verified", `UPDATE evidence SET object_version_id = 'v2' WHERE id = $1`, []any{rlsEvidenceA}, "23514"},
		{"shorten retention", `UPDATE evidence SET retain_until = retain_until - interval '1 hour' WHERE id = $1`, []any{rlsEvidenceA}, "23514"},
		{"clear retention", `UPDATE evidence SET retain_until = NULL WHERE id = $1`, []any{rlsEvidenceA}, "23514"},
		{"extend retention", `UPDATE evidence SET retain_until = retain_until + interval '1 year', updated_at = now() WHERE id = $1`, []any{rlsEvidenceA}, ""},
		{"pending to verified", `UPDATE evidence SET status = 'verified', object_version_id = 'v1' WHERE id = $1`, []any{pendingID}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, err := inSavepoint(ctx, tx, false, tt.sql, tt.args...)
			if tt.wantCode == "" {
				if err != nil || n != 1 {
					t.Errorf("changed %d rows, err %v; want 1 row", n, err)
				}
				return
			}
			if got := pgCode(err); got != tt.wantCode {
				t.Errorf("got %v, want SQLSTATE %s", err, tt.wantCode)
			}
		})
	}

	t.Run("newly verified evidence is frozen", func(t *testing.T) {
		if _, err := inSavepoint(ctx, tx, true, `UPDATE evidence SET status = 'verified', object_version_id = 'v1' WHERE id = $1`, pendingID); err != nil {
			t.Fatalf("verify: %v", err)
		}
		_, err := inSavepoint(ctx, tx, false, `UPDATE evidence SET status = 'rejected', rejected_reason = 'x' WHERE id = $1`, pendingID)
		if pgCode(err) != "23514" {
			t.Errorf("got %v, want SQLSTATE 23514", err)
		}
	})
}
