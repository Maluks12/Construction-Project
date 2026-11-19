BEGIN;

-- M11 review: an email is never recorded as sent when it wasn't. In log
-- mode (EMAIL_MODE=log, no SMTP) the worker marks it 'logged'.
ALTER TABLE notification_emails DROP CONSTRAINT notification_emails_status_check;
ALTER TABLE notification_emails ADD CONSTRAINT notification_emails_status_check
    CHECK (status IN ('pending', 'sending', 'sent', 'logged', 'failed', 'cancelled', 'unknown'));

-- As 000029, plus sending -> logged.
CREATE OR REPLACE FUNCTION notification_emails_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF TG_OP = 'DELETE' THEN
            RAISE EXCEPTION 'email %: never deleted', OLD.notification_id
                USING ERRCODE = 'check_violation';
        END IF;
        IF (NEW.notification_id, NEW.org_id, NEW.to_address, NEW.created_at)
           IS DISTINCT FROM (OLD.notification_id, OLD.org_id, OLD.to_address, OLD.created_at) THEN
            RAISE EXCEPTION 'email %: only its status changes', OLD.notification_id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.status = OLD.status THEN
            IF NEW IS DISTINCT FROM OLD THEN
                RAISE EXCEPTION 'email %: only its status changes', OLD.notification_id
                    USING ERRCODE = 'check_violation';
            END IF;
            RETURN NEW;
        END IF;
        IF NOT ((OLD.status = 'pending' AND NEW.status IN ('sending', 'cancelled'))
                OR (OLD.status = 'sending' AND NEW.status IN ('sent', 'logged', 'failed', 'unknown'))) THEN
            RAISE EXCEPTION 'email %: % can''t become %', OLD.notification_id, OLD.status, NEW.status
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.status = 'sending' THEN
            NEW.claimed_at := clock_timestamp();
        ELSE
            NEW.finished_at := clock_timestamp();
        END IF;
        RETURN NEW;
    END
    $$;

COMMIT;
