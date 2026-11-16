BEGIN;

-- M10 review (16 Nov):
--   1. Two people with the same name are told apart by their firm as well as
--      their clock number: identity_key is name|firm|clock_number.
--   2. A signature counts from the talk's start to its end, or to 12 hours
--      after the start while it has none, across midnight (night shifts).
--      The diary still counts a talk on the day it started (talk_date).

-- How a name, firm or clock number is compared: whitespace runs (space,
-- tab, newline, CR, FF, VT) become one space, ends are trimmed, case is
-- lowered. toolbox.normalise mirrors it; a test compares the two.
CREATE FUNCTION toolbox_norm(p text) RETURNS text
    LANGUAGE sql IMMUTABLE STRICT
    AS $$ SELECT lower(btrim(regexp_replace(p, '[\t\n\v\f\r ]+', ' ', 'g'))) $$;

ALTER TABLE toolbox_attendance DROP CONSTRAINT toolbox_attendance_once;
ALTER TABLE toolbox_attendance DROP COLUMN identity_key;
ALTER TABLE toolbox_attendance ADD COLUMN identity_key text GENERATED ALWAYS AS (
    toolbox_norm(name) || '|' || coalesce(toolbox_norm(firm), '') || '|' || coalesce(toolbox_norm(clock_number), '')
) STORED;
ALTER TABLE toolbox_attendance ADD CONSTRAINT toolbox_attendance_once UNIQUE (talk_id, identity_key);

-- How long after its start a talk without an end can be signed, and the
-- longest a talk can last. toolbox.SigningWindow mirrors it.
CREATE FUNCTION toolbox_signing_window() RETURNS interval
    LANGUAGE sql IMMUTABLE
    AS $$ SELECT interval '12 hours' $$;

ALTER TABLE toolbox_talks DROP CONSTRAINT toolbox_talks_ends_after_start;
ALTER TABLE toolbox_talks ADD CONSTRAINT toolbox_talks_ends_after_start CHECK (ended_at >= started_at);

CREATE OR REPLACE FUNCTION toolbox_talks_times_check(t toolbox_talks) RETURNS void
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF t.started_at > now() + interval '5 minutes' THEN
            RAISE EXCEPTION 'toolbox talk %: started_at is in the future', t.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF t.ended_at > now() + interval '5 minutes' THEN
            RAISE EXCEPTION 'toolbox talk %: ended_at is in the future', t.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF t.ended_at > t.started_at + toolbox_signing_window() THEN
            RAISE EXCEPTION 'toolbox talk %: ended_at is more than 12 hours after started_at', t.id
                USING ERRCODE = 'check_violation';
        END IF;
    END
    $$;

-- As 000020, plus: an end time can't come before a signature that stands
-- (retracted ones don't count).
CREATE OR REPLACE FUNCTION toolbox_talks_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF TG_OP = 'DELETE' THEN
            RAISE EXCEPTION 'toolbox talk %: talks are never deleted', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF (NEW.id, NEW.org_id, NEW.site_id, NEW.time_zone, NEW.recorded_by, NEW.device_id, NEW.created_at)
           IS DISTINCT FROM (OLD.id, OLD.org_id, OLD.site_id, OLD.time_zone, OLD.recorded_by, OLD.device_id, OLD.created_at) THEN
            RAISE EXCEPTION 'toolbox talk %: only its topic, presenter, notes and times can change', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF EXISTS (SELECT 1 FROM toolbox_attendance WHERE talk_id = OLD.id)
           AND ((NEW.topic_key, NEW.topic, NEW.presenter_name, NEW.notes, NEW.started_at)
                IS DISTINCT FROM (OLD.topic_key, OLD.topic, OLD.presenter_name, OLD.notes, OLD.started_at)
                OR (OLD.ended_at IS NOT NULL AND NEW.ended_at IS DISTINCT FROM OLD.ended_at)) THEN
            RAISE EXCEPTION 'toolbox talk %: people have signed it, so only its end time can be set, once', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.ended_at IS DISTINCT FROM OLD.ended_at AND EXISTS (
            SELECT 1 FROM toolbox_attendance a
            WHERE a.talk_id = OLD.id AND a.client_signed_at > NEW.ended_at
              AND NOT EXISTS (SELECT 1 FROM toolbox_attendance_retractions r WHERE r.attendance_id = a.id)) THEN
            RAISE EXCEPTION 'toolbox talk %: ended_at is before a signature', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        NEW.talk_date := action_local_date(NEW.started_at, NEW.time_zone);
        IF NEW IS DISTINCT FROM OLD THEN
            PERFORM toolbox_talks_times_check(NEW);
            NEW.updated_at := now();
        END IF;
        RETURN NEW;
    END
    $$;

-- The signing window replaces "on the talk's local date".
CREATE OR REPLACE FUNCTION toolbox_attendance_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        talk toolbox_talks;
    BEGIN
        NEW.signed_at := now();
        IF NEW.client_signed_at > NEW.signed_at + interval '5 minutes' THEN
            RAISE EXCEPTION 'attendance %: client_signed_at is in the future; check the device clock', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        SELECT * INTO talk FROM toolbox_talks WHERE id = NEW.talk_id FOR SHARE;
        IF NOT FOUND THEN
            RETURN NEW; -- the foreign key or the policy reports it
        END IF;
        IF NEW.client_signed_at < talk.started_at THEN
            RAISE EXCEPTION 'attendance %: signed before the talk started', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF talk.ended_at IS NOT NULL AND NEW.client_signed_at > talk.ended_at THEN
            RAISE EXCEPTION 'attendance %: signed after the talk ended', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF talk.ended_at IS NULL AND NEW.client_signed_at > talk.started_at + toolbox_signing_window() THEN
            RAISE EXCEPTION 'attendance %: signed more than 12 hours after the talk started', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

COMMIT;
