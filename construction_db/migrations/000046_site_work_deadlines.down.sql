BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM deadline_duties WHERE site_work_id IS NOT NULL)
       OR EXISTS (SELECT 1 FROM deadline_satisfactions WHERE document_id IS NOT NULL) THEN
        RAISE EXCEPTION 'site work deadlines or proof documents exist; they cannot be dropped';
    END IF;
END
$$;

-- 000017's deadlines_insert and deadline_satisfactions_insert, 000029's guard
-- and weakening check, 000030's recompute.
CREATE OR REPLACE FUNCTION deadlines_insert() RETURNS trigger
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

CREATE OR REPLACE FUNCTION deadline_satisfactions_insert() RETURNS trigger
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

CREATE OR REPLACE FUNCTION deadlines_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        rest deadlines;
        v    incident_versions;
        h    public_holidays;
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
        rest.recomputed_for := OLD.recomputed_for;
        IF rest IS DISTINCT FROM OLD OR NEW.closed_as IS NULL THEN
            RAISE EXCEPTION 'deadline %: the only change allowed is closing it', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;

        IF NEW.closed_as = 'recomputed' THEN
            IF current_setting('app.recompute_holiday', true) IS DISTINCT FROM NEW.recomputed_for::text THEN
                RAISE EXCEPTION 'deadline %: recomputed only by deadlines_recompute for that holiday', OLD.id
                    USING ERRCODE = 'check_violation';
            END IF;
            IF EXISTS (SELECT 1 FROM deadline_duties WHERE id = OLD.duty_id AND satisfaction_id IS NOT NULL) THEN
                RAISE EXCEPTION 'deadline %: its duty is satisfied; a satisfied deadline is never recomputed', OLD.id
                    USING ERRCODE = 'check_violation';
            END IF;
            SELECT * INTO h FROM public_holidays WHERE jurisdiction = OLD.jurisdiction AND holiday_date = NEW.recomputed_for;
            NEW.closed_in_version := NULL;
            NEW.closed_by := NULL;
            NEW.closed_at := now();
            NEW.closed_reason := 'Recomputed for a new public holiday: ' || h.name || ' (' || h.source_ref || ')';
            RETURN NEW;
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
        IF d.closed_as = 'recomputed' THEN
            SELECT * INTO successor FROM deadlines WHERE id = d.superseded_by;
            IF (successor.duty_id, successor.rule_key, successor.period, successor.unit, successor.direction, successor.basis_at,
                successor.time_zone, successor.jurisdiction, successor.regime_id)
               IS DISTINCT FROM (d.duty_id, d.rule_key, d.period, d.unit, d.direction, d.basis_at, d.time_zone, d.jurisdiction, d.regime_id) THEN
                RAISE EXCEPTION 'deadline %: a recomputation keeps the rule and its inputs', d.id
                    USING ERRCODE = 'check_violation';
            END IF;
            RETURN NULL;
        END IF;
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

CREATE OR REPLACE FUNCTION deadlines_recompute(p_jurisdiction char(2), p_day date) RETURNS integer
    LANGUAGE plpgsql
    AS $$
    DECLARE
        d      deadlines;
        r      record;
        new_id uuid;
        n      integer := 0;
    BEGIN
        IF NOT EXISTS (SELECT 1 FROM public_holidays WHERE jurisdiction = p_jurisdiction AND holiday_date = p_day) THEN
            RAISE EXCEPTION 'recompute: % is not a public holiday in %', p_day, p_jurisdiction
                USING ERRCODE = 'check_violation';
        END IF;
        IF EXISTS (SELECT 1 FROM holiday_recomputations
                   WHERE org_id = app_org_id() AND jurisdiction = p_jurisdiction AND holiday_date = p_day) THEN
            RETURN NULL;
        END IF;
        PERFORM set_config('app.recompute_holiday', p_day::text, true);
        FOR d IN
            SELECT x.* FROM deadlines_affected_by_day(p_jurisdiction, p_day) x
            JOIN deadline_duties du ON du.id = x.duty_id
            WHERE du.satisfaction_id IS NULL
            ORDER BY x.id
        LOOP
            SELECT * INTO r FROM deadline_due(d.basis_at, d.period, d.unit, d.direction, d.time_zone, d.jurisdiction);
            CONTINUE WHEN (r.due_at, r.due_on, r.legal_latest_on) IS NOT DISTINCT FROM (d.due_at, d.due_on, d.legal_latest_on);
            new_id := gen_random_uuid();
            UPDATE deadlines SET closed_as = 'recomputed', superseded_by = new_id, recomputed_for = p_day WHERE id = d.id;
            INSERT INTO deadlines (id, org_id, site_id, duty_id, incident_id, regime_id, rule_key, rule_label, kind, period, unit, direction,
                                   basis, source_ref, caveat, basis_at, time_zone, jurisdiction, due_at, excluded_days, computed_in_version)
            VALUES (new_id, d.org_id, d.site_id, d.duty_id, d.incident_id, d.regime_id, d.rule_key, d.rule_label, d.kind, d.period, d.unit,
                    d.direction, d.basis, d.source_ref, d.caveat, d.basis_at, d.time_zone, d.jurisdiction, r.due_at, r.excluded_days,
                    (SELECT latest_version FROM incidents WHERE id = d.incident_id));
            n := n + 1;
        END LOOP;
        PERFORM set_config('app.recompute_holiday', '', true);
        -- Two workers recomputing at once: the second's closing UPDATE waits
        -- on the first's row, then the guard refuses a closed row and the
        -- second rolls back; the primary key also stops a second record.
        INSERT INTO holiday_recomputations (org_id, jurisdiction, holiday_date, recomputed)
        VALUES (app_org_id(), p_jurisdiction, p_day, n);
        RETURN n;
    END
    $$;

DROP INDEX deadlines_site_work_idx;
ALTER TABLE deadline_satisfactions
    DROP CONSTRAINT deadline_satisfactions_document,
    DROP CONSTRAINT deadline_satisfactions_document_version_fkey,
    DROP CONSTRAINT deadline_satisfactions_document_fkey,
    DROP COLUMN document_version,
    DROP COLUMN document_id;
ALTER TABLE deadlines
    DROP CONSTRAINT deadlines_one_subject,
    ADD CONSTRAINT deadlines_one_subject CHECK (num_nonnulls(incident_id) = 1),
    DROP CONSTRAINT deadlines_site_work_closed_fkey,
    DROP CONSTRAINT deadlines_site_work_computed_fkey,
    DROP CONSTRAINT deadlines_site_work_duty_fkey,
    DROP COLUMN proof_document_types,
    DROP COLUMN site_work_id;
ALTER TABLE deadline_duties
    DROP CONSTRAINT deadline_duties_site_work_rule,
    DROP CONSTRAINT deadline_duties_once_site_work,
    DROP CONSTRAINT deadline_duties_one_subject,
    ADD CONSTRAINT deadline_duties_one_subject CHECK (num_nonnulls(incident_id) = 1),
    DROP CONSTRAINT deadline_duties_site_work_fkey,
    DROP COLUMN site_work_id;
DROP FUNCTION deadline_proof_types_valid(text[]);

COMMIT;