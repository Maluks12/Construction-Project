BEGIN;

-- Back to 000021: the diary suggests toolbox_headcount again.
CREATE OR REPLACE FUNCTION site_diary_revisions_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        d site_diaries;
    BEGIN
        NEW.server_received_at := now();
        SELECT * INTO d FROM site_diaries WHERE site_id = NEW.site_id AND diary_date = NEW.diary_date FOR NO KEY UPDATE;
        IF NOT FOUND THEN
            RETURN NEW; -- the foreign key or the policy reports it
        END IF;
        NEW.seq := d.revision_count + 1;
        NEW.headcount_suggested := toolbox_headcount(NEW.site_id, NEW.diary_date);
        NEW.saved_after_close := NEW.client_saved_at >= d.closes_at;
        NEW.received_after_close := NEW.server_received_at >= d.closes_at;
        IF NEW.client_saved_at > NEW.server_received_at + interval '5 minutes' THEN
            RAISE EXCEPTION 'site diary %: client_saved_at is in the future; check the device clock', NEW.diary_date
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.client_saved_at < d.opens_at THEN
            RAISE EXCEPTION 'site diary %: saved before the day began', NEW.diary_date
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.saved_after_close AND d.revision_count > 0 THEN
            RAISE EXCEPTION 'site diary %: closed at midnight; add an addendum', NEW.diary_date
                USING ERRCODE = 'check_violation';
        END IF;
        IF NOT NEW.saved_after_close AND NEW.server_received_at >= d.closes_at + site_diary_grace() THEN
            RAISE EXCEPTION 'site diary %: saved before midnight but received more than 48 hours after; add an addendum', NEW.diary_date
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

DROP FUNCTION site_headcount(uuid, date);
DROP FUNCTION site_sign_in_covering(uuid, uuid, timestamptz, uuid);
DROP FUNCTION site_sign_in_ends_at(site_sign_ins);
DROP TABLE site_roster;
DROP TABLE site_sign_in_sheets;
DROP TABLE site_sign_ins;
DROP FUNCTION site_roster_guard();
DROP FUNCTION site_sign_ins_insert();
DROP FUNCTION site_sign_ins_check();
DROP FUNCTION site_sign_ins_guard();
DROP FUNCTION site_sign_ins_append_only();
DROP FUNCTION site_sign_in_window();

COMMIT;
