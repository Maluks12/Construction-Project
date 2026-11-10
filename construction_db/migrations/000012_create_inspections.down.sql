BEGIN;

DROP TABLE inspection_findings;
DROP TABLE inspection_response_evidence;
DROP TABLE inspection_responses;
DROP TABLE inspections;
DROP FUNCTION inspections_no_truncate();
DROP FUNCTION inspection_findings_guard();
DROP FUNCTION inspection_response_evidence_guard();
DROP FUNCTION inspection_responses_guard();
DROP FUNCTION inspections_raise_findings();
DROP FUNCTION inspections_guard();
DROP FUNCTION inspections_insert();
ALTER TABLE evidence DROP CONSTRAINT evidence_org_site_id_status_unique;

COMMIT;
