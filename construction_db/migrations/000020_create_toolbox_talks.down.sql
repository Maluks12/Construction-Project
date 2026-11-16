BEGIN;

DROP FUNCTION toolbox_headcount(uuid, date);
-- Takes the talk row type, so it goes before the table.
DROP FUNCTION toolbox_talks_times_check(toolbox_talks);
DROP TABLE toolbox_attendance_retractions;
DROP TABLE toolbox_attendance;
DROP TABLE toolbox_talk_evidence;
DROP TABLE toolbox_talks;
DROP FUNCTION toolbox_append_only();
DROP FUNCTION toolbox_attendance_retractions_check();
DROP FUNCTION toolbox_attendance_check();
DROP FUNCTION toolbox_talk_evidence_check();
DROP FUNCTION toolbox_talks_check();
DROP FUNCTION toolbox_attendance_retractions_insert();
DROP FUNCTION toolbox_attendance_paper_register();
DROP FUNCTION toolbox_attendance_insert();
DROP FUNCTION toolbox_talk_evidence_insert();
DROP FUNCTION toolbox_talks_guard();
DROP FUNCTION toolbox_talks_insert();

COMMIT;
