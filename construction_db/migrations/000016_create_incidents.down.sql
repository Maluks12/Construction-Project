BEGIN;

DROP TABLE witness_statements;
DROP TABLE incident_version_evidence;
DROP TABLE incident_people;
DROP TABLE incident_versions;
DROP TABLE incidents;
DROP FUNCTION incidents_append_only();
DROP FUNCTION witness_statements_insert();
DROP FUNCTION incident_people_insert();
DROP FUNCTION incident_version_complete();
DROP FUNCTION incident_versions_advance();
DROP FUNCTION incident_versions_insert();
DROP FUNCTION incidents_has_version();
DROP FUNCTION incidents_insert();
DROP FUNCTION incidents_guard();
DROP FUNCTION incident_codes_valid(text[]);

COMMIT;
