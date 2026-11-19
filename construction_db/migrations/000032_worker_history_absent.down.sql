BEGIN;

DROP FUNCTION site_absent(uuid, date, text, integer);
DROP TABLE worker_history;
DROP TRIGGER workers_history ON workers;
DROP FUNCTION workers_history();

COMMIT;
