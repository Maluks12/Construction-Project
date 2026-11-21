BEGIN;

DROP TABLE site_work_versions;
DROP TABLE site_work;
DROP FUNCTION site_work_append_only();
DROP FUNCTION site_work_versions_advance();
DROP FUNCTION site_work_versions_insert();
DROP FUNCTION site_work_has_version();
DROP FUNCTION site_work_insert();
DROP FUNCTION site_work_guard();
DROP FUNCTION site_work_facts_valid(text[]);

COMMIT;
