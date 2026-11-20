BEGIN;

-- M12 review: a pack larger than one object can hold (S3: 5 TiB) is refused
-- for good (outcome 'refused'), like an integrity alarm: retrying can't
-- make it smaller. Below that, the upload's part size grows to fit 10,000
-- parts.

ALTER TABLE audit_export_attempts DROP CONSTRAINT audit_export_attempts_outcome_check;
ALTER TABLE audit_export_attempts ADD CONSTRAINT audit_export_attempts_outcome_check
    CHECK (outcome IN ('ready', 'failed', 'lapsed', 'integrity', 'refused'));
ALTER TABLE audit_export_attempts DROP CONSTRAINT audit_export_attempts_error;
ALTER TABLE audit_export_attempts ADD CONSTRAINT audit_export_attempts_error
    CHECK (error IS NULL OR outcome IN ('failed', 'lapsed', 'integrity', 'refused'));

CREATE OR REPLACE FUNCTION audit_export_retry_at(p_export uuid) RETURNS timestamptz
    LANGUAGE sql STABLE
    AS $$
    SELECT coalesce(
        (SELECT CASE WHEN a.outcome IN ('failed', 'ready', 'integrity', 'refused') THEN a.finished_at ELSE a.lease_until END
                + audit_export_backoff((SELECT count(*)::integer FROM audit_export_attempts x WHERE x.export_id = p_export))
         FROM audit_export_attempts a WHERE a.export_id = p_export ORDER BY a.attempt DESC LIMIT 1),
        '-infinity')
    $$;

CREATE OR REPLACE FUNCTION audit_export_attempts_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        e     audit_exports;
        prior integer;
    BEGIN
        IF TG_OP = 'DELETE' THEN
            RAISE EXCEPTION 'audit export attempts are append-only' USING ERRCODE = 'check_violation';
        END IF;
        SELECT * INTO e FROM audit_exports WHERE id = NEW.export_id FOR UPDATE;

        IF TG_OP = 'INSERT' THEN
            NEW.claimed_at := clock_timestamp();
            NEW.finished_at := NULL;
            NEW.outcome := NULL;
            NEW.error := NULL;
            IF EXISTS (SELECT 1 FROM audit_export_results r WHERE r.export_id = NEW.export_id) THEN
                RAISE EXCEPTION 'audit export %: already built', NEW.export_id USING ERRCODE = 'check_violation';
            END IF;
            IF EXISTS (SELECT 1 FROM audit_export_attempts a WHERE a.export_id = NEW.export_id AND a.outcome IN ('integrity', 'refused')) THEN
                RAISE EXCEPTION 'audit export %: stopped for good (an original no longer matches its seal, or the pack is too large)', NEW.export_id
                    USING ERRCODE = 'check_violation';
            END IF;
            IF e.as_of > NEW.claimed_at - audit_export_settle() THEN
                RAISE EXCEPTION 'audit export %: not settled yet', NEW.export_id USING ERRCODE = 'check_violation';
            END IF;
            IF EXISTS (SELECT 1 FROM audit_export_attempts a
                       WHERE a.export_id = NEW.export_id AND a.finished_at IS NULL AND a.lease_until > NEW.claimed_at) THEN
                RAISE EXCEPTION 'audit export %: another attempt holds the lease', NEW.export_id USING ERRCODE = 'check_violation';
            END IF;
            IF EXISTS (SELECT 1 FROM audit_export_attempts a WHERE a.export_id = NEW.export_id AND a.finished_at IS NULL) THEN
                RAISE EXCEPTION 'audit export %: mark the lapsed attempt first', NEW.export_id USING ERRCODE = 'check_violation';
            END IF;
            SELECT count(*) INTO prior FROM audit_export_attempts a WHERE a.export_id = NEW.export_id;
            IF NEW.attempt <> prior + 1 THEN
                RAISE EXCEPTION 'audit export %: attempt must be %', NEW.export_id, prior + 1 USING ERRCODE = 'check_violation';
            END IF;
            IF NEW.claimed_at < audit_export_retry_at(NEW.export_id) THEN
                RAISE EXCEPTION 'audit export %: the next attempt may start at %', NEW.export_id, audit_export_retry_at(NEW.export_id)
                    USING ERRCODE = 'check_violation';
            END IF;
            IF NEW.lease_until <= NEW.claimed_at THEN
                RAISE EXCEPTION 'audit export %: a lease must end after the claim', NEW.export_id USING ERRCODE = 'check_violation';
            END IF;
            RETURN NEW;
        END IF;

        -- UPDATE: nothing but the lease (forward, while live and unfinished)
        -- or the finish (once).
        IF (NEW.id, NEW.org_id, NEW.export_id, NEW.attempt, NEW.instance, NEW.claimed_at)
           IS DISTINCT FROM (OLD.id, OLD.org_id, OLD.export_id, OLD.attempt, OLD.instance, OLD.claimed_at) THEN
            RAISE EXCEPTION 'audit export attempt %: only its lease and finish change', OLD.id USING ERRCODE = 'check_violation';
        END IF;
        IF OLD.finished_at IS NOT NULL THEN
            RAISE EXCEPTION 'audit export attempt %: already finished', OLD.id USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.finished_at IS NULL THEN
            IF NEW.lease_until <= OLD.lease_until OR OLD.lease_until <= clock_timestamp() THEN
                RAISE EXCEPTION 'audit export attempt %: a lease moves forward, and only while it is live', OLD.id
                    USING ERRCODE = 'check_violation';
            END IF;
            RETURN NEW;
        END IF;
        NEW.lease_until := OLD.lease_until;
        NEW.finished_at := clock_timestamp();
        IF NEW.outcome = 'lapsed' AND OLD.lease_until > NEW.finished_at THEN
            RAISE EXCEPTION 'audit export attempt %: its lease is still live', OLD.id USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.outcome = 'ready' AND NOT EXISTS (SELECT 1 FROM audit_export_results r WHERE r.attempt_id = OLD.id) THEN
            RAISE EXCEPTION 'audit export attempt %: ready only with its result', OLD.id USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

COMMIT;
