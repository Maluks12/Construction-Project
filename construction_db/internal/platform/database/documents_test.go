package database_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
)

func docID(n int) string { return fmt.Sprintf("46000000-0000-0000-0000-%012d", n) }

const (
	// $1 id, $2 org, $3 site, $4 scope, $5 type, $6 created_by.
	docSQL = `
		INSERT INTO documents (id, org_id, site_id, scope, regime_code, type_key, title, created_by)
		VALUES ($1, $2, $3, $4, 'za_construction_2014', $5, 'A document', $6)`
	// $1 org, $2 site, $3 document, $4 version, $5 scope, $6 file, $7 uploaded_by.
	docVersionSQL = `
		INSERT INTO document_versions (org_id, site_id, document_id, version, scope, file_id, file_purpose, uploaded_by, client_uploaded_at)
		VALUES ($1, $2, $3, $4, $5, $6, $5::text || '_document', $7, now())`
)

func TestDocumentRules(t *testing.T) {
	seedRLS(t)
	ctx := context.Background()
	tx := beginAs(t, pool, rlsOrgA)
	orgFile := func(n int) string { return verifiedFile(t, tx, n, "org_document") }
	siteFile := func(n int) string {
		t.Helper()
		id := fileID(n)
		must(t, tx, 1, fileSQL, id, rlsOrgA, rlsSiteA, "site_document", rlsUserA, hash32(byte(n)), "verified")
		return id
	}
	latest := func(id string) int {
		t.Helper()
		var n int
		if err := tx.QueryRow(ctx, `SELECT latest_version FROM documents WHERE id = $1`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	t.Run("a document is saved with its first version", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "a document is saved with its first version",
			stmt{docSQL, []any{docID(1), rlsOrgA, nil, "org", "coida_letter_of_good_standing", rlsUserA}}, stmt{`SET CONSTRAINTS ALL IMMEDIATE`, nil})
		okBatch(t, tx, stmt{docSQL, []any{docID(1), rlsOrgA, nil, "org", "coida_letter_of_good_standing", rlsUserA}},
			stmt{docVersionSQL, []any{rlsOrgA, nil, docID(1), 1, "org", orgFile(301), rlsUserA}}, stmt{`SET CONSTRAINTS ALL IMMEDIATE`, nil})
		if n := latest(docID(1)); n != 1 {
			t.Errorf("latest_version %d", n)
		}
	})
	t.Run("versions come in order, never twice", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "version 3 is not the next version", stmt{docVersionSQL, []any{rlsOrgA, nil, docID(1), 3, "org", orgFile(302), rlsUserA}})
		refusedBatch(t, tx, "23505", "document_versions_pkey", stmt{docVersionSQL, []any{rlsOrgA, nil, docID(1), 1, "org", orgFile(303), rlsUserA}})
		okBatch(t, tx, stmt{docVersionSQL, []any{rlsOrgA, nil, docID(1), 2, "org", orgFile(304), rlsUserA}})
		if n := latest(docID(1)); n != 2 {
			t.Errorf("latest_version %d", n)
		}
	})
	t.Run("a version's file is a verified file of the document's own purpose and site", func(t *testing.T) {
		refusedBatch(t, tx, "23503", "", stmt{docVersionSQL, []any{rlsOrgA, nil, docID(1), 3, "org", siteFile(305), rlsUserA}})
		refusedBatch(t, tx, "23503", "", stmt{docVersionSQL, []any{rlsOrgA, nil, docID(1), 3, "org", verifiedFile(t, tx, 306, "medical"), rlsUserA}})
		// A site document's file is of the same site.
		okBatch(t, tx, stmt{docSQL, []any{docID(2), rlsOrgA, rlsSiteA, "site", "health_and_safety_plan", rlsUserA}},
			stmt{docVersionSQL, []any{rlsOrgA, rlsSiteA, docID(2), 1, "site", siteFile(307), rlsUserA}})
		refusedBatch(t, tx, "23514", "document_versions_site_scope", stmt{docVersionSQL, []any{rlsOrgA, nil, docID(2), 2, "site", siteFile(308), rlsUserA}})
		// A file belongs to one version.
		refusedBatch(t, tx, "23505", "document_versions_file_id_key", stmt{docVersionSQL, []any{rlsOrgA, nil, docID(1), 3, "org", fileID(304), rlsUserA}})
	})
	t.Run("nothing changes or goes", func(t *testing.T) {
		refused(t, tx, "42501", "", `UPDATE documents SET title = 'x' WHERE id = $1`, docID(1))
		refused(t, tx, "23514", "only its latest version moves on, by one", `UPDATE documents SET latest_version = 5 WHERE id = $1`, docID(1))
		refused(t, tx, "42501", "", `DELETE FROM document_versions WHERE document_id = $1`, docID(1))
		refused(t, tx, "42501", "", `DELETE FROM documents WHERE id = $1`, docID(1))
	})
}

func TestDocumentTablesRefuseChanges(t *testing.T) {
	seedRLS(t)
	tx := beginAs(t, admin, "")
	refused(t, tx, "23514", "document_versions is append-only", `UPDATE document_versions SET reference = 'x'`)
	refused(t, tx, "23514", "documents are never deleted", `DELETE FROM documents`)
	refuseTruncates(t, tx, "document_versions", "documents")
}

// Two version-2 saves at once: the second waits on the primary key and fails
// when the first commits. Forced: the first is held open.
func TestDocumentVersionRace(t *testing.T) {
	if err := seedOrgC(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	doc := fresh()
	files := []string{fresh(), fresh(), fresh()}
	if err := asC(ctx, func(tx pgx.Tx) error {
		for i, f := range files {
			if _, err := tx.Exec(ctx, fileSQL, f, orgC, nil, "org_document", userC, hash32(byte(200+i)), "verified"); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, docSQL, doc, orgC, nil, "org", "coida_letter_of_good_standing", userC); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, docVersionSQL, orgC, nil, doc, 1, "org", files[0], userC)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	first := tenantTx(t)
	defer first.Rollback(ctx)
	if _, err := first.Exec(ctx, docVersionSQL, orgC, nil, doc, 2, "org", files[1], userC); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- asC(ctx, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, docVersionSQL, orgC, nil, doc, 2, "org", files[2], userC)
			return err
		})
	}()
	waitingOn(t, "INSERT INTO document_versions")
	if err := first.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; pgCode(err) != "23505" {
		t.Fatalf("second version 2: %v, want 23505", err)
	}
}
