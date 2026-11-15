BEGIN;

DROP FUNCTION deadline_due(timestamptz, integer, text, text, text, char);
DROP FUNCTION is_public_holiday(char, date);
DROP FUNCTION holiday_calendar_check(char, date);
DROP TABLE public_holidays;
DROP TABLE holiday_calendar_coverage;

COMMIT;
