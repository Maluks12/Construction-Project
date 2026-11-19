BEGIN;

DROP FUNCTION toolbox_attendance_person(uuid);
DROP TABLE toolbox_attendance_workers;
DROP TABLE worker_medicals;
DROP TABLE worker_competencies;
DROP TABLE workers;
DROP FUNCTION toolbox_attendance_workers_check();
DROP FUNCTION toolbox_attendance_workers_guard();
DROP FUNCTION worker_records_insert();
DROP FUNCTION worker_records_guard();
DROP FUNCTION workers_lead(uuid, uuid);
DROP FUNCTION workers_guard();
DROP FUNCTION workers_no_truncate();

COMMIT;
