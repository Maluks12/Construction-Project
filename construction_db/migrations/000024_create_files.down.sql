BEGIN;

DROP TABLE file_access_log;
DROP TABLE files;
DROP FUNCTION files_append_only();
DROP FUNCTION files_freeze();

COMMIT;
