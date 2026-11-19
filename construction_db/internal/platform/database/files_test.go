package database_test

import (
	"fmt"
	"testing"
)

func fileID(n int) string { return fmt.Sprintf("40000000-0000-0000-0000-%012d", n) }

// $1 id, $2 org, $3 site, $4 purpose, $5 uploaded_by, $6 sha256, $7 status.
// A verified file has a version and a retention date; others have none.
const fileSQL = `
	INSERT INTO files (id, org_id, site_id, purpose, uploaded_by, content_sha256, size_bytes, content_type, object_key,
	                   object_version_id, retain_until, status)
	VALUES ($1::uuid, $2, $3, $4, $5, $6, 10, $$application/pdf$$, $$files/$$ || $1::uuid::text,
	        CASE WHEN $7::text = $$verified$$ THEN $$v1$$ END, CASE WHEN $7::text = $$verified$$ THEN now() + interval $$1 day$$ END, $7::text)`

// The seal on files (000024), for every role: the superuser's transaction
// bypasses RLS and grants, so only the triggers and constraints stop it.
func TestFileRules(t *testing.T) {
	seedRLS(t)
	tx := beginAs(t, admin, "")

	t.Run("purpose and site go together", func(t *testing.T) {
		refused(t, tx, "23514", "files_site_purpose", fileSQL, fileID(1), rlsOrgA, nil, "site_document", rlsUserA, hash32(1), "pending")
		refused(t, tx, "23514", "files_site_purpose", fileSQL, fileID(1), rlsOrgA, rlsSiteA, "medical", rlsUserA, hash32(1), "pending")
		refused(t, tx, "23514", "files_purpose_check", fileSQL, fileID(1), rlsOrgA, nil, "photo", rlsUserA, hash32(1), "pending")
	})

	must(t, tx, 1, fileSQL, fileID(2), rlsOrgA, nil, "medical", rlsUserA, hash32(2), "pending")
	t.Run("a pending file records its outcome, never what was declared", func(t *testing.T) {
		refused(t, tx, "23514", "what was declared never changes", `UPDATE files SET content_sha256 = $2 WHERE id = $1`, fileID(2), hash32(9))
		refused(t, tx, "23514", "what was declared never changes", `UPDATE files SET purpose = 'competency' WHERE id = $1`, fileID(2))
		must(t, tx, 1, `UPDATE files SET status = 'verified', object_version_id = 'v1', retain_until = now() + interval '1 day' WHERE id = $1`, fileID(2))
	})
	t.Run("a verified file never changes but its retention may grow", func(t *testing.T) {
		refused(t, tx, "23514", "only pending files can change", `UPDATE files SET object_version_id = 'v2' WHERE id = $1`, fileID(2))
		refused(t, tx, "23514", "only pending files can change", `UPDATE files SET status = 'rejected', rejected_reason = 'x' WHERE id = $1`, fileID(2))
		refused(t, tx, "23514", "retention can only be extended", `UPDATE files SET retain_until = now() WHERE id = $1`, fileID(2))
		refused(t, tx, "23514", "retention can only be extended", `UPDATE files SET retain_until = NULL WHERE id = $1`, fileID(2))
		must(t, tx, 1, `UPDATE files SET retain_until = retain_until + interval '1 year' WHERE id = $1`, fileID(2))
		refused(t, tx, "23514", "files are never deleted", `DELETE FROM files WHERE id = $1`, fileID(2))
	})
	t.Run("a verified file has a version", func(t *testing.T) {
		refused(t, tx, "23514", "files_verified_version", `
			INSERT INTO files (id, org_id, purpose, uploaded_by, content_sha256, size_bytes, content_type, object_key, status)
			VALUES ($1, $2, 'competency', $3, $4, 1, 'image/png', 'x', 'verified')`, fileID(3), rlsOrgA, rlsUserA, hash32(3))
	})
	t.Run("the access log is append-only", func(t *testing.T) {
		must(t, tx, 1, `INSERT INTO file_access_log (org_id, file_id, user_id, action, object_version_id) VALUES ($1, $2, $3, 'view_bytes', 'v1')`,
			rlsOrgA, fileID(2), rlsUserA)
		refused(t, tx, "23514", "file_access_log is append-only", `UPDATE file_access_log SET ip = '10.0.0.1' WHERE file_id = $1`, fileID(2))
		refused(t, tx, "23514", "file_access_log is append-only", `DELETE FROM file_access_log WHERE file_id = $1`, fileID(2))
	})
	t.Run("neither table can be truncated", func(t *testing.T) {
		refusedBatch(t, tx, "23514", "file_access_log is append-only", stmt{`TRUNCATE file_access_log`, nil})
		refusedBatch(t, tx, "23514", "files is append-only",
			stmt{`ALTER TABLE file_access_log DISABLE TRIGGER file_access_log_no_truncate`, nil}, stmt{`TRUNCATE files CASCADE`, nil})
	})
}

// The app may record a file's outcome and nothing else.
func TestFileGrants(t *testing.T) {
	seedRLS(t)
	tx := beginAs(t, pool, rlsOrgA)
	must(t, tx, 1, fileSQL, fileID(10), rlsOrgA, nil, "competency", rlsUserA, hash32(4), "pending")
	refused(t, tx, "42501", "", `UPDATE files SET file_name = 'x' WHERE id = $1`, fileID(10))
	refused(t, tx, "42501", "", `DELETE FROM files WHERE id = $1`, fileID(10))
	must(t, tx, 1, `UPDATE files SET status = 'rejected', rejected_reason = 'hash_mismatch' WHERE id = $1`, fileID(10))
}
