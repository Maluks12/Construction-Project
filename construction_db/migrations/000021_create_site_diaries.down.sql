BEGIN;

DROP TABLE site_diary_revision_evidence;
ALTER TABLE site_diaries DROP CONSTRAINT site_diaries_head_fkey;
DROP TABLE site_diary_revisions;
DROP TABLE site_diaries;
DROP FUNCTION site_diary_append_only();
DROP FUNCTION site_diary_revision_complete_self();
DROP FUNCTION site_diary_revision_complete();
DROP FUNCTION site_diary_revisions_advance();
DROP FUNCTION site_diary_revisions_insert();
DROP FUNCTION site_diaries_guard();
DROP FUNCTION site_diaries_has_revision();
DROP FUNCTION site_diaries_check();
DROP FUNCTION site_diaries_insert();
DROP FUNCTION site_diary_replay(uuid, date);
DROP FUNCTION site_diary_sections_ok(jsonb);
DROP FUNCTION site_diary_grace();

COMMIT;
