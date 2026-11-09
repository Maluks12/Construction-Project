BEGIN;

DROP TABLE evidence_access_log;
DROP FUNCTION evidence_access_log_append_only();
ALTER TABLE evidence DROP CONSTRAINT evidence_org_site_id_unique;

COMMIT;
