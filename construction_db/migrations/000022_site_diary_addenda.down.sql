BEGIN;

DROP TABLE site_diary_addendum_evidence;
DROP TABLE site_diary_addenda;
DROP FUNCTION site_diary_addendum_complete();
DROP FUNCTION site_diary_addenda_check();
DROP FUNCTION site_diary_addenda_insert();

COMMIT;
