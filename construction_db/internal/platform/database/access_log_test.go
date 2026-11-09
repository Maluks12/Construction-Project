package database_test

import (
	"context"
	"strings"
	"testing"

	"construction_db/internal/platform/database"
)

// The access log only grows. The app may insert and read; nobody, superusers
// included, may change or remove a row.
func TestAccessLogIsAppendOnly(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()

	t.Run("as the app", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback(ctx)
		if err := database.SetTenant(ctx, tx, database.Tenant{OrgID: rlsOrgA}); err != nil {
			t.Fatalf("set tenant: %v", err)
		}
		const otherSiteA = "22000000-0000-0000-0000-0000000000a2"
		if _, err := inSavepoint(ctx, tx, true, `INSERT INTO sites (id, org_id, regime_id, name) SELECT $1, $2, id, 'Second site' FROM regulatory_regimes LIMIT 1`,
			otherSiteA, rlsOrgA); err != nil {
			t.Fatalf("insert second site: %v", err)
		}

		const insert = `INSERT INTO evidence_access_log (org_id, site_id, evidence_id, user_id, action, object_version_id, filter_status, filter_limit, result_count, ip, user_agent)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
		type row struct {
			site, evidence, user, action string
			version, filterStatus        *string
			filterLimit, count           *int
			userAgent                    *string
		}
		insertArgs := func(r row) []any {
			var ev any
			if r.evidence != "" {
				ev = r.evidence
			}
			return []any{rlsOrgA, r.site, ev, r.user, r.action, r.version, r.filterStatus, r.filterLimit, r.count, "192.0.2.10", r.userAgent}
		}
		v1, verified, fifty, three, longUA := ptr("v1"), ptr("verified"), ptr(50), ptr(3), ptr(strings.Repeat("a", 513))

		tests := []struct {
			name     string
			row      row
			wantCode string // "" means the insert succeeds
		}{
			{"view_bytes", row{site: rlsSiteA, evidence: rlsEvidenceA, user: rlsUserA, action: "view_bytes", version: v1}, ""},
			{"view_record", row{site: rlsSiteA, evidence: rlsEvidenceA, user: rlsUserA, action: "view_record"}, ""},
			{"list", row{site: rlsSiteA, user: rlsUserA, action: "list", filterStatus: verified, filterLimit: fifty, count: three}, ""},
			{"export", row{site: rlsSiteA, evidence: rlsEvidenceA, user: rlsUserA, action: "export"}, ""},
			{"share", row{site: rlsSiteA, evidence: rlsEvidenceA, user: rlsUserA, action: "share"}, ""},
			{"unknown action", row{site: rlsSiteA, evidence: rlsEvidenceA, user: rlsUserA, action: "download"}, "23514"},
			{"list naming one evidence", row{site: rlsSiteA, evidence: rlsEvidenceA, user: rlsUserA, action: "list", count: three}, "23514"},
			{"list without a count", row{site: rlsSiteA, user: rlsUserA, action: "list"}, "23514"},
			{"view_record without evidence", row{site: rlsSiteA, user: rlsUserA, action: "view_record"}, "23514"},
			{"view_bytes without a version", row{site: rlsSiteA, evidence: rlsEvidenceA, user: rlsUserA, action: "view_bytes"}, "23514"},
			{"view_record with a version", row{site: rlsSiteA, evidence: rlsEvidenceA, user: rlsUserA, action: "view_record", version: v1}, "23514"},
			{"filters outside a list", row{site: rlsSiteA, evidence: rlsEvidenceA, user: rlsUserA, action: "view_record", filterStatus: verified}, "23514"},
			{"user agent over 512 bytes", row{site: rlsSiteA, evidence: rlsEvidenceA, user: rlsUserA, action: "view_record", userAgent: longUA}, "23514"},
			{"evidence from another site", row{site: otherSiteA, evidence: rlsEvidenceA, user: rlsUserA, action: "view_record"}, "23503"},
			{"user from another org", row{site: rlsSiteA, evidence: rlsEvidenceA, user: rlsUserB, action: "view_record"}, "23503"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				n, err := inSavepoint(ctx, tx, false, insert, insertArgs(tt.row)...)
				if tt.wantCode == "" {
					if err != nil || n != 1 {
						t.Errorf("inserted %d rows, err %v; want 1", n, err)
					}
					return
				}
				if got := pgCode(err); got != tt.wantCode {
					t.Errorf("got %v, want SQLSTATE %s", err, tt.wantCode)
				}
			})
		}

		for _, sql := range []string{
			`UPDATE evidence_access_log SET accessed_at = now()`,
			`DELETE FROM evidence_access_log`,
			`TRUNCATE evidence_access_log`,
		} {
			if _, err := inSavepoint(ctx, tx, false, sql); pgCode(err) != "42501" {
				t.Errorf("%s: got %v, want SQLSTATE 42501", sql, err)
			}
		}
	})

	t.Run("as a superuser", func(t *testing.T) {
		tx, err := admin.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback(ctx)

		for _, sql := range []string{
			`UPDATE evidence_access_log SET accessed_at = now()`,
			`DELETE FROM evidence_access_log`,
			`TRUNCATE evidence_access_log`,
		} {
			if _, err := inSavepoint(ctx, tx, false, sql); pgCode(err) != "23514" {
				t.Errorf("%s: got %v, want SQLSTATE 23514 from the append-only trigger", sql, err)
			}
		}

		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM evidence_access_log`).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		if n != 2 {
			t.Errorf("%d rows after the attempts, want the 2 seeded", n)
		}
	})
}

func ptr[T any](v T) *T { return &v }
