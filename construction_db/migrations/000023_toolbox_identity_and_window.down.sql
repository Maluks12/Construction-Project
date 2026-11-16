BEGIN;

-- Back to 000020. Fails if two signatures of one talk differ only by firm:
-- the old key can't tell them apart.

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
        IF action_local_date(NEW.client_signed_at, talk.time_zone) <> talk.talk_date THEN
            RAISE EXCEPTION 'attendance %: signed on another day than the talk', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

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
        NEW.talk_date := action_local_date(NEW.started_at, NEW.time_zone);
        IF NEW IS DISTINCT FROM OLD THEN
            PERFORM toolbox_talks_times_check(NEW);
            NEW.updated_at := now();
        END IF;
        RETURN NEW;
    END
    $$;

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
    END
    $$;

ALTER TABLE toolbox_talks DROP CONSTRAINT toolbox_talks_ends_after_start;
ALTER TABLE toolbox_talks ADD CONSTRAINT toolbox_talks_ends_after_start CHECK (ended_at > started_at);
DROP FUNCTION toolbox_signing_window();

ALTER TABLE toolbox_attendance DROP CONSTRAINT toolbox_attendance_once;
ALTER TABLE toolbox_attendance DROP COLUMN identity_key;
ALTER TABLE toolbox_attendance ADD COLUMN identity_key text GENERATED ALWAYS AS (
    lower(regexp_replace(btrim(name), '\s+', ' ', 'g')) || '|' || coalesce(lower(btrim(clock_number)), '')
) STORED;
ALTER TABLE toolbox_attendance ADD CONSTRAINT toolbox_attendance_once UNIQUE (talk_id, identity_key);
DROP FUNCTION toolbox_norm(text);

COMMIT;
