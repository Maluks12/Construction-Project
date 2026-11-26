BEGIN;

ALTER TABLE deadline_satisfactions DROP COLUMN device_id;

COMMIT;
