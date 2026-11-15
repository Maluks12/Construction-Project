BEGIN;

DROP FUNCTION deadlines_affected_by_day(char, date);
ALTER TABLE deadline_duties DROP CONSTRAINT deadline_duties_satisfaction_fkey;
ALTER TABLE deadlines DROP CONSTRAINT deadlines_added_by_satisfaction_fkey;
DROP TABLE deadline_satisfaction_retractions;
DROP TABLE deadline_satisfactions;
DROP TABLE deadlines;
DROP TABLE deadline_duties;
DROP FUNCTION deadlines_append_only();
DROP FUNCTION deadline_satisfaction_retractions_release();
DROP FUNCTION deadline_satisfaction_retractions_insert();
DROP FUNCTION deadline_satisfactions_take();
DROP FUNCTION deadline_satisfactions_insert();
DROP FUNCTION deadlines_weakening_check();
DROP FUNCTION deadlines_guard();
DROP FUNCTION deadlines_insert();
DROP FUNCTION deadline_duties_insert();
DROP FUNCTION deadline_duties_guard();

COMMIT;
