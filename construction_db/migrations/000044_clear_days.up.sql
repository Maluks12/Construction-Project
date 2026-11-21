BEGIN;

-- REG4 D1: "at least N days before" counts neither the day of the act nor the
-- basis day, so clear_days before is due on start - (N + 1). Only "before":
-- nothing uses clear days after an event. No legal_latest: IA s4's extension
-- would shorten a notice. Everything else is 000015's function unchanged.
CREATE OR REPLACE FUNCTION deadline_due(
    p_basis_at      timestamptz,
    p_period        integer,
    p_unit          text,
    p_direction     text,
    p_time_zone     text,
    p_jurisdiction  char(2),
    OUT due_at          timestamptz,
    OUT due_on          date,
    OUT excluded_days   date[],
    OUT legal_latest_on date,
    OUT legal_latest_at timestamptz)
    LANGUAGE plpgsql STABLE
    AS $$
    DECLARE
        step    integer;
        d       date;
        counted integer := 0;
    BEGIN
        IF p_basis_at IS NULL OR p_period IS NULL OR p_unit IS NULL OR p_direction IS NULL
           OR p_time_zone IS NULL OR p_jurisdiction IS NULL THEN
            RAISE EXCEPTION 'deadline_due: every argument is required'
                USING ERRCODE = 'null_value_not_allowed';
        END IF;
        IF p_direction NOT IN ('after', 'before') THEN
            RAISE EXCEPTION 'deadline_due: unknown direction %', p_direction
                USING ERRCODE = 'check_violation';
        END IF;
        IF p_unit = 'immediately' AND (p_period <> 0 OR p_direction <> 'after') THEN
            RAISE EXCEPTION 'deadline_due: immediately takes period 0 and direction after'
                USING ERRCODE = 'check_violation';
        END IF;
        IF p_unit = 'clear_days' AND p_direction <> 'before' THEN
            RAISE EXCEPTION 'deadline_due: clear_days takes direction before'
                USING ERRCODE = 'check_violation';
        END IF;
        IF p_unit <> 'immediately' AND p_period < 1 THEN
            RAISE EXCEPTION 'deadline_due: period must be 1 or more'
                USING ERRCODE = 'check_violation';
        END IF;

        step := CASE p_direction WHEN 'after' THEN 1 ELSE -1 END;
        excluded_days := '{}';
        d := (p_basis_at AT TIME ZONE p_time_zone)::date;

        CASE p_unit
        WHEN 'immediately' THEN
            due_at := p_basis_at;
        WHEN 'hours' THEN
            due_at := p_basis_at + step * make_interval(hours => p_period);
        WHEN 'calendar_days' THEN
            due_on := d + step * p_period;
            IF p_direction = 'after' THEN
                d := due_on;
                PERFORM holiday_calendar_check(p_jurisdiction, d);
                WHILE extract(isodow FROM d) = 7 OR is_public_holiday(p_jurisdiction, d) LOOP
                    d := d + 1;
                    PERFORM holiday_calendar_check(p_jurisdiction, d);
                END LOOP;
                IF d <> due_on THEN
                    legal_latest_on := d;
                    legal_latest_at := (d + 1)::timestamp AT TIME ZONE p_time_zone;
                END IF;
            END IF;
        WHEN 'clear_days' THEN
            due_on := d - (p_period + 1);
        WHEN 'working_days' THEN
            WHILE counted < p_period LOOP
                d := d + step;
                PERFORM holiday_calendar_check(p_jurisdiction, d);
                IF extract(isodow FROM d) IN (6, 7) OR is_public_holiday(p_jurisdiction, d) THEN
                    excluded_days := excluded_days || d;
                ELSE
                    counted := counted + 1;
                END IF;
            END LOOP;
            due_on := d;
            excluded_days := ARRAY(SELECT x FROM unnest(excluded_days) AS x ORDER BY x);
        ELSE
            RAISE EXCEPTION 'deadline_due: unknown unit %', p_unit
                USING ERRCODE = 'check_violation';
        END CASE;

        IF due_on IS NOT NULL THEN
            due_at := (due_on + 1)::timestamp AT TIME ZONE p_time_zone;
        END IF;
    END
    $$;

COMMIT;
