BEGIN;

DROP TABLE document_versions;
DROP TABLE documents;
DROP FUNCTION document_versions_insert();
DROP FUNCTION document_versions_advance();
DROP FUNCTION documents_guard();
DROP FUNCTION documents_has_version();
DROP FUNCTION documents_append_only();

COMMIT;
