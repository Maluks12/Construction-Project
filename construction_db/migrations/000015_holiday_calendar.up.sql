BEGIN;

-- The public holiday calendar (Rev 4 §2.3: "The calendar is a table, never a
-- constant"). One row per day off, Sunday substitutes and proclaimed days
-- included, so the table alone says which days are holidays. Sources are in
-- internal/regimes/SOURCES_za_construction_2014.md.
-- TestHolidayCalendarFollowsTheAct regenerates the statutory and substitute
-- rows from the Act and fails on any difference; a proclaimed day is added by
-- a new migration.
CREATE TABLE public_holidays (
    jurisdiction char(2) NOT NULL,
    holiday_date date NOT NULL,
    name         text NOT NULL CHECK (name <> ''),
    kind         text NOT NULL CHECK (kind IN ('statutory', 'sunday_substitute', 'proclaimed')),
    source_ref   text NOT NULL CHECK (source_ref <> ''),

    PRIMARY KEY (jurisdiction, holiday_date),
    -- PHA s2(1): "the following Monday shall be a public holiday".
    CONSTRAINT public_holidays_substitute_is_monday CHECK (kind <> 'sunday_substitute' OR extract(isodow FROM holiday_date) = 1)
);

-- The dates the calendar is complete for. A computation that needs a day
-- outside them fails rather than assume there is no holiday on it.
CREATE TABLE holiday_calendar_coverage (
    jurisdiction char(2) PRIMARY KEY,
    first_date   date NOT NULL,
    last_date    date NOT NULL,
    CHECK (first_date <= last_date)
);

-- Only migrations write the calendar.
REVOKE INSERT, UPDATE, DELETE ON public_holidays FROM siteproof_app;
REVOKE INSERT, UPDATE, DELETE ON holiday_calendar_coverage FROM siteproof_app;

CREATE FUNCTION holiday_calendar_check(p_jurisdiction char(2), p_day date) RETURNS void
    LANGUAGE plpgsql STABLE
    AS $$
    DECLARE
        c holiday_calendar_coverage;
    BEGIN
        SELECT * INTO c FROM holiday_calendar_coverage WHERE jurisdiction = p_jurisdiction;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'holiday calendar: no calendar for jurisdiction %', p_jurisdiction
                USING ERRCODE = 'data_exception';
        END IF;
        -- Two messages: a day before the calendar means an old incident (the
        -- API answers 400); a day after it means the calendar needs extending.
        IF p_day < c.first_date THEN
            RAISE EXCEPTION 'holiday calendar: % starts on %; % is before it', p_jurisdiction, c.first_date, p_day
                USING ERRCODE = 'data_exception';
        END IF;
        IF p_day > c.last_date THEN
            RAISE EXCEPTION 'holiday calendar: % ends on %; % is after it', p_jurisdiction, c.last_date, p_day
                USING ERRCODE = 'data_exception';
        END IF;
    END
    $$;

CREATE FUNCTION is_public_holiday(p_jurisdiction char(2), p_day date) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$ SELECT EXISTS (SELECT 1 FROM public_holidays WHERE jurisdiction = p_jurisdiction AND holiday_date = p_day) $$;

-- The deadline engine's only date arithmetic. The deadlines insert trigger
-- (000017) calls it, so every stored due value comes from here.
--
-- Day units count from the basis's local date, which is excluded (IA s4: the
-- first day is excluded, the last included), and are due by the end of the
-- last day: due_on is that day and due_at the first instant after it.
--   - calendar_days: due_on is period days away. "after" also sets
--     legal_latest_on when due_on is a Sunday or public holiday (IA s4): the
--     next day that is neither. due_on itself is never extended.
--   - working_days: counts only Monday to Friday days that aren't public
--     holidays; every day skipped is in excluded_days.
--   - hours: due_at is a moment; due_on is null.
--   - immediately: due_at is the basis itself (a call-now prompt).
-- Every day the result depends on must be inside the calendar's coverage.
CREATE FUNCTION deadline_due(
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
        IF p_unit <> 'immediately' AND p_period < 1 THEN
            RAISE EXCEPTION 'deadline_due: period must be 1 or more'
                USING ERRCODE = 'check_violation';
        END IF;

        step := CASE p_direction WHEN 'after' THEN 1 ELSE -1 END;
        excluded_days := '{}';
        -- An unknown zone name is an error here (22023), never UTC.
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

INSERT INTO holiday_calendar_coverage (jurisdiction, first_date, last_date) VALUES ('ZA', '2024-01-01', '2030-12-31');

INSERT INTO public_holidays (jurisdiction, holiday_date, name, kind, source_ref) VALUES
    ('ZA', '2024-01-01', 'New Year''s Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2024-03-21', 'Human Rights Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2024-03-29', 'Good Friday', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2024-04-01', 'Family Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2024-04-27', 'Freedom Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2024-05-01', 'Workers'' Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2024-05-29', 'Declared public holiday', 'proclaimed', 'PHA s2A; Proclamation Notice 157 of 2024, GG 50165'),
    ('ZA', '2024-06-16', 'Youth Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2024-06-17', 'Youth Day (Sunday substitute)', 'sunday_substitute', 'PHA s2(1): 2024-06-16 is a Sunday'),
    ('ZA', '2024-08-09', 'National Women''s Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2024-09-24', 'Heritage Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2024-12-16', 'Day of Reconciliation', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2024-12-25', 'Christmas Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2024-12-26', 'Day of Goodwill', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2025-01-01', 'New Year''s Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2025-03-21', 'Human Rights Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2025-04-18', 'Good Friday', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2025-04-21', 'Family Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2025-04-27', 'Freedom Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2025-04-28', 'Freedom Day (Sunday substitute)', 'sunday_substitute', 'PHA s2(1): 2025-04-27 is a Sunday'),
    ('ZA', '2025-05-01', 'Workers'' Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2025-06-16', 'Youth Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2025-08-09', 'National Women''s Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2025-09-24', 'Heritage Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2025-12-16', 'Day of Reconciliation', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2025-12-25', 'Christmas Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2025-12-26', 'Day of Goodwill', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2026-01-01', 'New Year''s Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2026-03-21', 'Human Rights Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2026-04-03', 'Good Friday', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2026-04-06', 'Family Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2026-04-27', 'Freedom Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2026-05-01', 'Workers'' Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2026-06-16', 'Youth Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2026-08-09', 'National Women''s Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2026-08-10', 'National Women''s Day (Sunday substitute)', 'sunday_substitute', 'PHA s2(1): 2026-08-09 is a Sunday'),
    ('ZA', '2026-09-24', 'Heritage Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2026-11-04', 'Local government elections (declared public holiday)', 'proclaimed', 'PHA s2A; Proclamation Notice 346 of 2026, GG 55352'),
    ('ZA', '2026-12-16', 'Day of Reconciliation', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2026-12-25', 'Christmas Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2026-12-26', 'Day of Goodwill', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2027-01-01', 'New Year''s Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2027-03-21', 'Human Rights Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2027-03-22', 'Human Rights Day (Sunday substitute)', 'sunday_substitute', 'PHA s2(1): 2027-03-21 is a Sunday'),
    ('ZA', '2027-03-26', 'Good Friday', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2027-03-29', 'Family Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2027-04-27', 'Freedom Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2027-05-01', 'Workers'' Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2027-06-16', 'Youth Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2027-08-09', 'National Women''s Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2027-09-24', 'Heritage Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2027-12-16', 'Day of Reconciliation', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2027-12-25', 'Christmas Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2027-12-26', 'Day of Goodwill', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2027-12-27', 'Day of Goodwill (Sunday substitute)', 'sunday_substitute', 'PHA s2(1): 2027-12-26 is a Sunday'),
    ('ZA', '2028-01-01', 'New Year''s Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2028-03-21', 'Human Rights Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2028-04-14', 'Good Friday', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2028-04-17', 'Family Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2028-04-27', 'Freedom Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2028-05-01', 'Workers'' Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2028-06-16', 'Youth Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2028-08-09', 'National Women''s Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2028-09-24', 'Heritage Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2028-09-25', 'Heritage Day (Sunday substitute)', 'sunday_substitute', 'PHA s2(1): 2028-09-24 is a Sunday'),
    ('ZA', '2028-12-16', 'Day of Reconciliation', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2028-12-25', 'Christmas Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2028-12-26', 'Day of Goodwill', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2029-01-01', 'New Year''s Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2029-03-21', 'Human Rights Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2029-03-30', 'Good Friday', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2029-04-02', 'Family Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2029-04-27', 'Freedom Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2029-05-01', 'Workers'' Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2029-06-16', 'Youth Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2029-08-09', 'National Women''s Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2029-09-24', 'Heritage Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2029-12-16', 'Day of Reconciliation', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2029-12-17', 'Day of Reconciliation (Sunday substitute)', 'sunday_substitute', 'PHA s2(1): 2029-12-16 is a Sunday'),
    ('ZA', '2029-12-25', 'Christmas Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2029-12-26', 'Day of Goodwill', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2030-01-01', 'New Year''s Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2030-03-21', 'Human Rights Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2030-04-19', 'Good Friday', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2030-04-22', 'Family Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2030-04-27', 'Freedom Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2030-05-01', 'Workers'' Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2030-06-16', 'Youth Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2030-06-17', 'Youth Day (Sunday substitute)', 'sunday_substitute', 'PHA s2(1): 2030-06-16 is a Sunday'),
    ('ZA', '2030-08-09', 'National Women''s Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2030-09-24', 'Heritage Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2030-12-16', 'Day of Reconciliation', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2030-12-25', 'Christmas Day', 'statutory', 'PHA s2(1), Schedule 1'),
    ('ZA', '2030-12-26', 'Day of Goodwill', 'statutory', 'PHA s2(1), Schedule 1');

COMMIT;
