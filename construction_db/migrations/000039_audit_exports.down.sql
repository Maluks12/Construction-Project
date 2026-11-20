BEGIN;

ALTER TABLE file_access_log DROP CONSTRAINT file_access_log_export_fkey;
ALTER TABLE file_access_log DROP CONSTRAINT file_access_log_export;
ALTER TABLE file_access_log DROP COLUMN export_id;
ALTER TABLE file_access_log DROP CONSTRAINT file_access_log_action_check;
ALTER TABLE file_access_log ADD CONSTRAINT file_access_log_action_check CHECK (action IN ('view_bytes'));
ALTER TABLE evidence_access_log DROP CONSTRAINT evidence_access_log_export_fkey;
ALTER TABLE evidence_access_log DROP CONSTRAINT evidence_access_log_export;
ALTER TABLE evidence_access_log DROP COLUMN export_id;

DROP TABLE audit_export_downloads;
DROP TABLE audit_export_files;
DROP TABLE audit_export_evidence;
DROP TABLE audit_export_results;
DROP TABLE audit_export_attempts;
DROP TABLE audit_exports;
DROP FUNCTION audit_export_contents_check();
DROP FUNCTION audit_export_results_finish();
DROP FUNCTION audit_export_results_guard();
DROP FUNCTION audit_export_attempts_guard();
DROP FUNCTION audit_export_settle();
DROP FUNCTION audit_exports_insert();
DROP FUNCTION audit_exports_requester();

COMMIT;
