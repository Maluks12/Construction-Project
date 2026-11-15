BEGIN;

-- The deadline engine's records (Rev 4 §2.3, §5.3). Three tables:
--   - deadline_duties: one obligation, e.g. "GAR 8(1)(a) notice for incident
--     X". It lasts for as long as the incident does, through every version.
--   - deadlines: each computation of a duty's due date. A new basis (a
--     corrected occurred_at) supersedes the row with a new one; a rule that no
--     longer applies withdraws it. Rows are never edited or deleted, so what
--     the system said, and when, stays on record.
--   - deadline_satisfactions and their retractions: a satisfaction belongs to
--     the duty, not to a row, so it carries over a recomputation and is judged
--     against the duty's live due date.
-- Rev 4 §5.3's (subject_type, subject_id) can't have a foreign key, so each
-- subject gets its own column; incident_id is the only one so far.

CREATE TABLE deadline_duties (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          uuid NOT NULL,
    site_id         uuid NOT NULL,
    incident_id     uuid,
    rule_key        text NOT NULL CHECK (rule_key ~ '^[a-z0-9_]+$'),
    created_at      timestamptz NOT NULL DEFAULT now(),
    -- The satisfaction that currently meets the duty, or null. Maintained by
    -- the satisfaction and retraction triggers, under this row's lock.
    satisfaction_id uuid,

    FOREIGN KEY (org_id, site_id, incident_id) REFERENCES incidents (org_id, site_id, id),
    CONSTRAINT deadline_duties_one_subject CHECK (num_nonnulls(incident_id) = 1),
    CONSTRAINT deadline_duties_once UNIQUE (incident_id, rule_key),
    UNIQUE (org_id, site_id, id),
    UNIQUE (org_id, site_id, id, incident_id, rule_key)
);

CREATE TABLE deadlines (
    id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id                  uuid NOT NULL,
    site_id                 uuid NOT NULL,
    duty_id                 uuid NOT NULL,
    incident_id             uuid,
    regime_id               uuid NOT NULL REFERENCES regulatory_regimes (id),
    -- A copy of the regime rule (internal/regimes), as M7 copies template
    -- items: the row explains itself even if the data set changes.
    rule_key                text NOT NULL,
    rule_label              text NOT NULL CHECK (rule_label <> ''),
    kind                    text NOT NULL CHECK (kind IN ('report', 'investigation', 'notification')),
    period                  integer NOT NULL,
    unit                    text NOT NULL,
    direction               text NOT NULL,
    basis                   text NOT NULL CHECK (basis ~ '^[a-z0-9_]+$'),
    source_ref              text NOT NULL CHECK (source_ref <> ''),
    caveat                  text CHECK (caveat <> ''),
    basis_at                timestamptz NOT NULL,
    time_zone               text NOT NULL,
    jurisdiction            char(2) NOT NULL,
    -- Set by deadlines_insert from deadline_due (000015); whatever the
    -- client sent is overwritten.
    due_at                  timestamptz NOT NULL,
    due_on                  date,
    excluded_days           date[] NOT NULL,
    legal_latest_on         date,
    legal_latest_at         timestamptz,
    legal_latest_source_ref text,
    computed_in_version     integer NOT NULL,
    computed_at             timestamptz NOT NULL DEFAULT now(),
    -- Set when recording a satisfaction added the row (a death after a
    -- notice), so the row isn't shown as that version's doing.
    added_by_satisfaction_id uuid,
    -- How the row stopped being live: superseded by a recomputation, or
    -- withdrawn because the rule no longer applies. Who, when and why are
    -- copied from the version that did it.
    closed_as               text CHECK (closed_as IN ('superseded', 'withdrawn')),
    superseded_by           uuid,
    closed_in_version       integer,
    closed_by               uuid,
    closed_at               timestamptz,
    closed_reason           text,

    FOREIGN KEY (org_id, site_id, duty_id, incident_id, rule_key)
        REFERENCES deadline_duties (org_id, site_id, id, incident_id, rule_key),
    FOREIGN KEY (org_id, site_id, incident_id, computed_in_version)
        REFERENCES incident_versions (org_id, site_id, incident_id, version),
    FOREIGN KEY (org_id, site_id, incident_id, closed_in_version)
        REFERENCES incident_versions (org_id, site_id, incident_id, version),
    FOREIGN KEY (org_id, closed_by) REFERENCES org_members (org_id, user_id),
    UNIQUE (org_id, site_id, id),
    UNIQUE (org_id, site_id, id, duty_id),

    CONSTRAINT deadlines_one_subject CHECK (num_nonnulls(incident_id) = 1),
    CONSTRAINT deadlines_closed CHECK (
        (closed_as IS NULL) = (closed_in_version IS NULL)
        AND (closed_as IS NULL) = (closed_by IS NULL)
        AND (closed_as IS NULL) = (closed_at IS NULL)
        AND (closed_as IS NULL) = (closed_reason IS NULL)
        AND (closed_as IS NOT DISTINCT FROM 'superseded') = (superseded_by IS NOT NULL)),
    CONSTRAINT deadlines_legal_latest CHECK (
        (legal_latest_on IS NULL) = (legal_latest_at IS NULL)
        AND (legal_latest_on IS NULL) = (legal_latest_source_ref IS NULL))
);

-- Checked at commit, because a recomputation closes the old row before it
-- inserts the new one (the live-row index below allows one at a time).
ALTER TABLE deadlines ADD CONSTRAINT deadlines_superseded_by_fkey
    FOREIGN KEY (org_id, site_id, superseded_by, duty_id) REFERENCES deadlines (org_id, site_id, id, duty_id)
    DEFERRABLE INITIALLY DEFERRED;

-- One live computation per duty.
CREATE UNIQUE INDEX deadlines_one_live ON deadlines (duty_id) WHERE closed_as IS NULL;
CREATE INDEX deadlines_site_idx ON deadlines (site_id, due_at) WHERE closed_as IS NULL;
CREATE INDEX deadlines_incident_idx ON deadlines (incident_id);

-- That a duty was met: when (the client's time of the call or filing), how,
-- and a reference. Ids are generated on the device.
CREATE TABLE deadline_satisfactions (
    id              uuid PRIMARY KEY,
    org_id          uuid NOT NULL,
    site_id         uuid NOT NULL,
    duty_id         uuid NOT NULL,
    -- The live computation it was recorded against, for the record.
    deadline_id     uuid NOT NULL,
    satisfied_at    timestamptz NOT NULL,
    method          text NOT NULL CHECK (method IN ('phone', 'email', 'fax', 'in_person', 'online', 'post', 'other')),
    reference       text CHECK (char_length(reference) BETWEEN 1 AND 200),
    note            text CHECK (char_length(note) BETWEEN 1 AND 2000),
    recorded_by     uuid NOT NULL,
    recorded_at     timestamptz NOT NULL DEFAULT now(),
    evidence_id     uuid,
    evidence_status text NOT NULL DEFAULT 'verified' CHECK (evidence_status = 'verified'),

    FOREIGN KEY (org_id, site_id, deadline_id, duty_id) REFERENCES deadlines (org_id, site_id, id, duty_id),
    FOREIGN KEY (org_id, recorded_by) REFERENCES org_members (org_id, user_id),
    FOREIGN KEY (org_id, site_id, evidence_id, evidence_status) REFERENCES evidence (org_id, site_id, id, status),
    UNIQUE (org_id, site_id, id),
    UNIQUE (org_id, site_id, id, duty_id)
);

ALTER TABLE deadlines ADD CONSTRAINT deadlines_added_by_satisfaction_fkey
    FOREIGN KEY (org_id, site_id, added_by_satisfaction_id) REFERENCES deadline_satisfactions (org_id, site_id, id);

CREATE INDEX deadline_satisfactions_duty_idx ON deadline_satisfactions (duty_id);

-- A satisfaction recorded in error, taken back by an owner or SHE officer.
-- The satisfaction stays on record; the duty is open again.
CREATE TABLE deadline_satisfaction_retractions (
    satisfaction_id uuid PRIMARY KEY,
    org_id          uuid NOT NULL,
    site_id         uuid NOT NULL,
    duty_id         uuid NOT NULL,
    retracted_by    uuid NOT NULL,
    retracted_at    timestamptz NOT NULL DEFAULT now(),
    reason          text NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 500),

    FOREIGN KEY (org_id, site_id, satisfaction_id, duty_id) REFERENCES deadline_satisfactions (org_id, site_id, id, duty_id),
    FOREIGN KEY (org_id, retracted_by) REFERENCES org_members (org_id, user_id)
);

ALTER TABLE deadline_duties ADD CONSTRAINT deadline_duties_satisfaction_fkey
    FOREIGN KEY (org_id, site_id, satisfaction_id, id) REFERENCES deadline_satisfactions (org_id, site_id, id, duty_id);

-- Every guard below applies to every role, the table owner included. Each
-- raises check_violation with its own message.

-- A duty changes only by its satisfaction_id being set (by a satisfaction) or
-- cleared (by a retraction); those triggers check the transition. A no-op is
-- allowed. Nothing is deleted.
CREATE FUNCTION deadline_duties_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        rest deadline_duties;
    BEGIN
        IF TG_OP = 'DELETE' THEN
            RAISE EXCEPTION 'deadline duty %: duties are never deleted', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        rest := NEW;
        rest.satisfaction_id := OLD.satisfaction_id;
        IF rest IS DISTINCT FROM OLD THEN
            RAISE EXCEPTION 'deadline duty %: only its satisfaction changes', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.satisfaction_id IS NOT DISTINCT FROM OLD.satisfaction_id THEN
            RETURN NEW;
        END IF;
        IF NEW.satisfaction_id IS NOT NULL AND OLD.satisfaction_id IS NOT NULL THEN
            RAISE EXCEPTION 'deadline duty %: already satisfied; retract that satisfaction first', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.satisfaction_id IS NULL AND NOT EXISTS (
            SELECT 1 FROM deadline_satisfaction_retractions WHERE satisfaction_id = OLD.satisfaction_id) THEN
            RAISE EXCEPTION 'deadline duty %: a satisfaction is cleared only by retracting it', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.satisfaction_id IS NOT NULL AND EXISTS (
            SELECT 1 FROM deadline_satisfaction_retractions WHERE satisfaction_id = NEW.satisfaction_id) THEN
            RAISE EXCEPTION 'deadline duty %: that satisfaction was retracted', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER deadline_duties_guard BEFORE UPDATE OR DELETE ON deadline_duties
    FOR EACH ROW EXECUTE FUNCTION deadline_duties_guard();

CREATE FUNCTION deadline_duties_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        NEW.created_at := now();
        IF NEW.satisfaction_id IS NOT NULL THEN
            RAISE EXCEPTION 'deadline duty %: a new duty is not satisfied yet', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER deadline_duties_insert BEFORE INSERT ON deadline_duties
    FOR EACH ROW EXECUTE FUNCTION deadline_duties_insert();

-- A new computation: due values come from deadline_due, never the client.
-- It belongs to the incident's latest version, and a new row is live.
-- legal_latest follows Interpretation Act s4, a South African rule: a
-- jurisdiction added later needs its own reading here.
CREATE FUNCTION deadlines_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        r      record;
        latest integer;
    BEGIN
        IF NEW.closed_as IS NOT NULL OR NEW.closed_in_version IS NOT NULL OR NEW.closed_by IS NOT NULL
           OR NEW.closed_at IS NOT NULL OR NEW.closed_reason IS NOT NULL OR NEW.superseded_by IS NOT NULL THEN
            RAISE EXCEPTION 'deadline: a new deadline is live'
                USING ERRCODE = 'check_violation';
        END IF;
        SELECT latest_version INTO latest FROM incidents WHERE id = NEW.incident_id;
        IF FOUND AND NEW.computed_in_version <> latest THEN
            RAISE EXCEPTION 'deadline: computed in version % but the incident is at version %', NEW.computed_in_version, latest
                USING ERRCODE = 'check_violation';
        END IF;
        -- The satisfaction must be this incident's, recorded in this
        -- transaction (now() is the transaction's start, as is recorded_at).
        IF NEW.added_by_satisfaction_id IS NOT NULL AND NOT EXISTS (
            SELECT 1 FROM deadline_satisfactions s JOIN deadline_duties du ON du.id = s.duty_id
            WHERE s.id = NEW.added_by_satisfaction_id AND du.incident_id = NEW.incident_id AND s.recorded_at = now()) THEN
            RAISE EXCEPTION 'deadline: added by a satisfaction of this incident recorded in the same transaction only'
                USING ERRCODE = 'check_violation';
        END IF;

        SELECT * INTO r FROM deadline_due(NEW.basis_at, NEW.period, NEW.unit, NEW.direction, NEW.time_zone, NEW.jurisdiction);
        NEW.due_at := r.due_at;
        NEW.due_on := r.due_on;
        NEW.excluded_days := r.excluded_days;
        NEW.legal_latest_on := r.legal_latest_on;
        NEW.legal_latest_at := r.legal_latest_at;
        NEW.legal_latest_source_ref := CASE WHEN r.legal_latest_on IS NOT NULL THEN 'Interpretation Act 33 of 1957, s4' END;
        NEW.computed_at := now();
        RETURN NEW;
    END
    $$;

CREATE TRIGGER deadlines_insert BEFORE INSERT ON deadlines
    FOR EACH ROW EXECUTE FUNCTION deadlines_insert();

-- A deadline changes once: it is closed by a later version of its incident,
-- as superseded or withdrawn. Who, when and why are copied from that version,
-- so they can't disagree with it. Nothing else ever changes, and nothing is
-- deleted.
CREATE FUNCTION deadlines_guard() RETURNS trigger
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

CREATE TRIGGER deadlines_guard BEFORE UPDATE OR DELETE ON deadlines
    FOR EACH ROW EXECUTE FUNCTION deadlines_guard();

-- At commit, a closed report deadline that weakened the duty needs an owner
-- or SHE officer to have submitted the version (decided 15 Nov): withdrawing
-- one that isn't satisfied, or replacing it with a later due time (satisfied
-- or not: a later date could turn a late report into an on-time one).
-- Deferred, because the replacement is inserted after this row is closed.
CREATE FUNCTION deadlines_weakening_check() RETURNS trigger
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

CREATE CONSTRAINT TRIGGER deadlines_weakening_check AFTER UPDATE ON deadlines
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION deadlines_weakening_check();

-- Recording a satisfaction: against the duty's live computation, at a time
-- not in the future and not before the clock's basis (5 minutes for clock
-- skew). FOR SHARE orders it against an amendment closing that row: whichever
-- comes second sees the other. The UPDATE in deadline_satisfactions_take takes
-- the duty's row lock, so two satisfactions of one duty queue and the second
-- fails: one live satisfaction per duty.
CREATE FUNCTION deadline_satisfactions_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        d deadlines;
    BEGIN
        NEW.recorded_at := now();
        SELECT * INTO d FROM deadlines WHERE id = NEW.deadline_id FOR SHARE;
        IF FOUND AND d.closed_as IS NOT NULL THEN
            RAISE EXCEPTION 'deadline % is %; record the satisfaction against the live deadline', d.id, d.closed_as
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.satisfied_at > NEW.recorded_at + interval '5 minutes' THEN
            RAISE EXCEPTION 'satisfaction %: satisfied_at is in the future', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF FOUND AND NEW.satisfied_at < d.basis_at - interval '5 minutes' THEN
            RAISE EXCEPTION 'satisfaction %: satisfied_at is before the deadline''s basis', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER deadline_satisfactions_insert BEFORE INSERT ON deadline_satisfactions
    FOR EACH ROW EXECUTE FUNCTION deadline_satisfactions_insert();

CREATE FUNCTION deadline_satisfactions_take() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        UPDATE deadline_duties SET satisfaction_id = NEW.id WHERE id = NEW.duty_id AND satisfaction_id IS NULL;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'deadline duty %: already satisfied; retract that satisfaction first', NEW.duty_id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

CREATE TRIGGER deadline_satisfactions_take AFTER INSERT ON deadline_satisfactions
    FOR EACH ROW EXECUTE FUNCTION deadline_satisfactions_take();

-- Retracting: an owner or SHE officer, and only the duty's current
-- satisfaction. Clearing satisfaction_id reopens the duty. The role is checked
-- after the insert, so another org's row fails its policy (42501) first.
CREATE FUNCTION deadline_satisfaction_retractions_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        NEW.retracted_at := now();
        RETURN NEW;
    END
    $$;

CREATE TRIGGER deadline_satisfaction_retractions_insert BEFORE INSERT ON deadline_satisfaction_retractions
    FOR EACH ROW EXECUTE FUNCTION deadline_satisfaction_retractions_insert();

CREATE FUNCTION deadline_satisfaction_retractions_release() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF NOT EXISTS (SELECT 1 FROM org_members
                       WHERE org_id = NEW.org_id AND user_id = NEW.retracted_by AND role IN ('owner', 'she_officer')) THEN
            RAISE EXCEPTION 'satisfaction %: only an owner or a SHE officer can retract a satisfaction', NEW.satisfaction_id
                USING ERRCODE = 'check_violation';
        END IF;
        UPDATE deadline_duties SET satisfaction_id = NULL WHERE id = NEW.duty_id AND satisfaction_id = NEW.satisfaction_id;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'satisfaction %: is not the duty''s current satisfaction', NEW.satisfaction_id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

CREATE TRIGGER deadline_satisfaction_retractions_release AFTER INSERT ON deadline_satisfaction_retractions
    FOR EACH ROW EXECUTE FUNCTION deadline_satisfaction_retractions_release();

CREATE FUNCTION deadlines_append_only() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        RAISE EXCEPTION '% is append-only', TG_TABLE_NAME
            USING ERRCODE = 'check_violation';
    END
    $$;

CREATE TRIGGER deadline_satisfactions_append_only BEFORE UPDATE OR DELETE ON deadline_satisfactions
    FOR EACH ROW EXECUTE FUNCTION deadlines_append_only();
CREATE TRIGGER deadline_satisfaction_retractions_append_only BEFORE UPDATE OR DELETE ON deadline_satisfaction_retractions
    FOR EACH ROW EXECUTE FUNCTION deadlines_append_only();

CREATE TRIGGER deadline_duties_no_truncate BEFORE TRUNCATE ON deadline_duties
    FOR EACH STATEMENT EXECUTE FUNCTION deadlines_append_only();
CREATE TRIGGER deadlines_no_truncate BEFORE TRUNCATE ON deadlines
    FOR EACH STATEMENT EXECUTE FUNCTION deadlines_append_only();
CREATE TRIGGER deadline_satisfactions_no_truncate BEFORE TRUNCATE ON deadline_satisfactions
    FOR EACH STATEMENT EXECUTE FUNCTION deadlines_append_only();
CREATE TRIGGER deadline_satisfaction_retractions_no_truncate BEFORE TRUNCATE ON deadline_satisfaction_retractions
    FOR EACH STATEMENT EXECUTE FUNCTION deadlines_append_only();

-- The live day-based deadlines whose window includes a given day: what a
-- newly proclaimed holiday could change. The M11 worker recomputes them.
CREATE FUNCTION deadlines_affected_by_day(p_jurisdiction char(2), p_day date) RETURNS SETOF deadlines
    LANGUAGE sql STABLE
    AS $$
        SELECT * FROM deadlines
        WHERE closed_as IS NULL AND jurisdiction = p_jurisdiction AND due_on IS NOT NULL
          AND p_day BETWEEN least((basis_at AT TIME ZONE time_zone)::date, due_on)
                        AND greatest((basis_at AT TIME ZONE time_zone)::date, coalesce(legal_latest_on, due_on))
    $$;

ALTER TABLE deadline_duties ENABLE ROW LEVEL SECURITY;
ALTER TABLE deadline_duties FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON deadline_duties FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE deadlines ENABLE ROW LEVEL SECURITY;
ALTER TABLE deadlines FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON deadlines FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE deadline_satisfactions ENABLE ROW LEVEL SECURITY;
ALTER TABLE deadline_satisfactions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON deadline_satisfactions FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE deadline_satisfaction_retractions ENABLE ROW LEVEL SECURITY;
ALTER TABLE deadline_satisfaction_retractions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON deadline_satisfaction_retractions FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

-- Migration 000008's default privileges grant UPDATE and DELETE on new tables.
-- The satisfaction triggers run as the app and set satisfaction_id; closing a
-- deadline sets three columns, and its trigger fills in who, when and why.
REVOKE UPDATE, DELETE ON deadline_duties FROM siteproof_app;
GRANT UPDATE (satisfaction_id) ON deadline_duties TO siteproof_app;
REVOKE UPDATE, DELETE ON deadlines FROM siteproof_app;
GRANT UPDATE (closed_as, superseded_by, closed_in_version) ON deadlines TO siteproof_app;
REVOKE UPDATE, DELETE ON deadline_satisfactions FROM siteproof_app;
REVOKE UPDATE, DELETE ON deadline_satisfaction_retractions FROM siteproof_app;

COMMIT;
