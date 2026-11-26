BEGIN;

DROP INDEX site_sign_ins_org_sync_idx;
DROP FUNCTION sync_oldest_write_age();

COMMIT;
