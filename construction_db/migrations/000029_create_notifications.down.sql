BEGIN;

-- Back to 000017. Fails while any deadline is closed as recomputed.
DROP FUNCTION deadlines_recompute(char, date);
CREATE OR REPLACE FUNCTION deadlines_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        rest deadlines;
        v    incident_versions;
    BEGIN
        IF TG_OP = 'DELETE' THEN
            RAISE EXCEPTION 'deadline %: deadlines are never deleted', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW IS NOT DISTINCT FROM OLD THEN
            RETURN NEW;
        END IF;
        IF OLD.closed_as IS NOT NULL THEN
            RAISE EXCEPTION 'deadline % is %; it never changes again', OLD.id, OLD.closed_as
                USING ERRCODE = 'check_violation';
        END IF;
        rest := NEW;
        rest.closed_as := OLD.closed_as;
        rest.superseded_by := OLD.superseded_by;
        rest.closed_in_version := OLD.closed_in_version;
        rest.closed_by := OLD.closed_by;
        rest.closed_at := OLD.closed_at;
        rest.closed_reason := OLD.closed_reason;
        IF rest IS DISTINCT FROM OLD OR NEW.closed_as IS NULL THEN
            RAISE EXCEPTION 'deadline %: the only change allowed is closing it', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;

        SELECT * INTO v FROM incident_versions WHERE incident_id = OLD.incident_id AND version = NEW.closed_in_version;
        IF NOT FOUND OR NEW.closed_in_version <= OLD.computed_in_version
           OR NEW.closed_in_version <> (SELECT latest_version FROM incidents WHERE id = OLD.incident_id) THEN
            RAISE EXCEPTION 'deadline %: closed only by the incident''s latest version, after the one that computed it', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        NEW.closed_by := v.submitted_by;
        NEW.closed_at := v.submitted_at;
        NEW.closed_reason := v.amend_reason;
        RETURN NEW;
    END
    $$;

CREATE OR REPLACE FUNCTION deadlines_weakening_check() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        d         deadlines;
        successor deadlines;
        satisfied boolean;
        weakens   boolean;
    BEGIN
        SELECT * INTO d FROM deadlines WHERE id = NEW.id;
        IF d.closed_as IS NULL OR d.kind <> 'report' THEN
            RETURN NULL;
        END IF;
        IF d.closed_as = 'withdrawn' THEN
            SELECT satisfaction_id IS NOT NULL INTO satisfied FROM deadline_duties WHERE id = d.duty_id;
            weakens := NOT satisfied;
        ELSE
            SELECT * INTO successor FROM deadlines WHERE id = d.superseded_by;
            IF successor.computed_in_version <> d.closed_in_version THEN
                RAISE EXCEPTION 'deadline %: its replacement must be computed by the version that superseded it', d.id
                    USING ERRCODE = 'check_violation';
            END IF;
            weakens := successor.due_at > d.due_at;
        END IF;
        IF weakens AND NOT EXISTS (
            SELECT 1 FROM org_members WHERE org_id = d.org_id AND user_id = d.closed_by AND role IN ('owner', 'she_officer')) THEN
            RAISE EXCEPTION 'deadline %: only an owner or a SHE officer can withdraw a report deadline or move it later', d.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

REVOKE UPDATE (recomputed_for) ON deadlines FROM siteproof_app;
ALTER TABLE deadlines DROP CONSTRAINT deadlines_closed;
ALTER TABLE deadlines DROP CONSTRAINT deadlines_recomputed_for_fkey;
ALTER TABLE deadlines DROP COLUMN recomputed_for;
ALTER TABLE deadlines DROP CONSTRAINT deadlines_closed_as_check;
ALTER TABLE deadlines ADD CONSTRAINT deadlines_closed_as_check CHECK (closed_as IN ('superseded', 'withdrawn'));
ALTER TABLE deadlines ADD CONSTRAINT deadlines_closed CHECK (
        (closed_as IS NULL) = (closed_in_version IS NULL)
        AND (closed_as IS NULL) = (closed_by IS NULL)
        AND (closed_as IS NULL) = (closed_at IS NULL)
        AND (closed_as IS NULL) = (closed_reason IS NULL)
        AND (closed_as IS NOT DISTINCT FROM 'superseded') = (superseded_by IS NOT NULL));

DROP TABLE holiday_recomputations;
DROP FUNCTION notification_subject_current(text, uuid, text);
DROP FUNCTION notifications_due(timestamptz, text, text, integer[], integer[], integer[], integer[], integer);
DROP FUNCTION notification_site_supervisors(uuid);
DROP FUNCTION notification_leads();
DROP FUNCTION notification_lead(integer, integer[], text);
DROP FUNCTION worker_org_ids();
DROP TABLE job_runs;
DROP TABLE notification_emails;
DROP TABLE notifications;
DROP FUNCTION job_runs_guard();
DROP FUNCTION notification_emails_guard();
DROP FUNCTION notifications_guard();
DROP FUNCTION notifications_append_only();

COMMIT;
