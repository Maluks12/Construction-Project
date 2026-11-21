BEGIN;

-- REG4: deadlines get a second subject, site work (000045), beside incidents.
-- One nullable foreign key per subject, exactly one set (M8). Every rule that
-- named incidents now reads the subject's own version table.

ALTER TABLE deadline_duties ADD COLUMN site_work_id uuid;
ALTER TABLE deadline_duties
    ADD CONSTRAINT deadline_duties_site_work_fkey FOREIGN KEY (org_id, site_id, site_work_id) REFERENCES site_work (org_id, site_id, id),
    DROP CONSTRAINT deadline_duties_one_subject,
    ADD CONSTRAINT deadline_duties_one_subject CHECK (num_nonnulls(incident_id, site_work_id) = 1),
    ADD CONSTRAINT deadline_duties_once_site_work UNIQUE (site_work_id, rule_key),
    ADD CONSTRAINT deadline_duties_site_work_rule UNIQUE (org_id, site_id, id, site_work_id, rule_key);

CREATE FUNCTION deadline_proof_types_valid(types text[]) RETURNS boolean
    LANGUAGE sql IMMUTABLE
    AS $$
        SELECT coalesce((SELECT bool_and(t IS NOT NULL AND t ~ '^[a-z0-9_]+$') FROM unnest(types) AS t), true)
           AND (SELECT count(DISTINCT t) FROM unnest(types) AS t) = cardinality(types)
    $$;

-- A copy of the rule's proof types (REG4 D12), like every other rule field.
ALTER TABLE deadlines ADD COLUMN site_work_id uuid;
ALTER TABLE deadlines ADD COLUMN proof_document_types text[] NOT NULL DEFAULT '{}'
    CHECK (deadline_proof_types_valid(proof_document_types));
ALTER TABLE deadlines
    ADD CONSTRAINT deadlines_site_work_duty_fkey FOREIGN KEY (org_id, site_id, duty_id, site_work_id, rule_key)
        REFERENCES deadline_duties (org_id, site_id, id, site_work_id, rule_key),
    ADD CONSTRAINT deadlines_site_work_computed_fkey FOREIGN KEY (org_id, site_id, site_work_id, computed_in_version)
        REFERENCES site_work_versions (org_id, site_id, site_work_id, version),
    ADD CONSTRAINT deadlines_site_work_closed_fkey FOREIGN KEY (org_id, site_id, site_work_id, closed_in_version)
        REFERENCES site_work_versions (org_id, site_id, site_work_id, version),
    DROP CONSTRAINT deadlines_one_subject,
    ADD CONSTRAINT deadlines_one_subject CHECK (num_nonnulls(incident_id, site_work_id) = 1);
CREATE INDEX deadlines_site_work_idx ON deadlines (site_work_id);

-- Proof from the site's H&S file: one document version of the same site.
ALTER TABLE deadline_satisfactions
    ADD COLUMN document_id uuid,
    ADD COLUMN document_version integer,
    ADD CONSTRAINT deadline_satisfactions_document_fkey FOREIGN KEY (org_id, document_id, site_id) REFERENCES documents (org_id, id, site_id),
    ADD CONSTRAINT deadline_satisfactions_document_version_fkey FOREIGN KEY (document_id, document_version) REFERENCES document_versions (document_id, version),
    ADD CONSTRAINT deadline_satisfactions_document CHECK ((document_id IS NULL) = (document_version IS NULL));

-- As 000017, per subject. A site work deadline runs from its version's
-- starts_on at local midnight, and from nothing else.
CREATE OR REPLACE FUNCTION deadlines_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        r      record;
        latest integer;
        starts date;
    BEGIN
        IF NEW.closed_as IS NOT NULL OR NEW.closed_in_version IS NOT NULL OR NEW.closed_by IS NOT NULL
           OR NEW.closed_at IS NOT NULL OR NEW.closed_reason IS NOT NULL OR NEW.superseded_by IS NOT NULL THEN
            RAISE EXCEPTION 'deadline: a new deadline is live'
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.incident_id IS NOT NULL THEN
            SELECT latest_version INTO latest FROM incidents WHERE id = NEW.incident_id;
            IF FOUND AND NEW.computed_in_version <> latest THEN
                RAISE EXCEPTION 'deadline: computed in version % but the incident is at version %', NEW.computed_in_version, latest
                    USING ERRCODE = 'check_violation';
            END IF;
        ELSE
            SELECT latest_version INTO latest FROM site_work WHERE id = NEW.site_work_id;
            IF FOUND AND NEW.computed_in_version <> latest THEN
                RAISE EXCEPTION 'deadline: computed in version % but the site work is at version %', NEW.computed_in_version, latest
                    USING ERRCODE = 'check_violation';
            END IF;
            IF NEW.basis <> 'work_starts_at' THEN
                RAISE EXCEPTION 'deadline: a site work deadline runs from work_starts_at'
                    USING ERRCODE = 'check_violation';
            END IF;
            SELECT starts_on INTO starts FROM site_work_versions WHERE site_work_id = NEW.site_work_id AND version = NEW.computed_in_version;
            IF FOUND AND NEW.basis_at <> (starts::timestamp AT TIME ZONE NEW.time_zone) THEN
                RAISE EXCEPTION 'deadline: basis_at must be the start of starts_on (%) in %', starts, NEW.time_zone
                    USING ERRCODE = 'check_violation';
            END IF;
        END IF;
        IF NEW.added_by_satisfaction_id IS NOT NULL AND NOT EXISTS (
            SELECT 1 FROM deadline_satisfactions s JOIN deadline_duties du ON du.id = s.duty_id
            WHERE s.id = NEW.added_by_satisfaction_id AND s.recorded_at = now()
              AND (du.incident_id = NEW.incident_id OR du.site_work_id = NEW.site_work_id)) THEN
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

-- As 000029: closed by the subject's latest version, whose submitter, time
-- and reason are copied.
CREATE OR REPLACE FUNCTION deadlines_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        rest   deadlines;
        h      public_holidays;
        closer record;
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

        IF OLD.incident_id IS NOT NULL THEN
            SELECT submitted_by, submitted_at, amend_reason INTO closer
            FROM incident_versions WHERE incident_id = OLD.incident_id AND version = NEW.closed_in_version;
            IF NOT FOUND OR NEW.closed_in_version <= OLD.computed_in_version
               OR NEW.closed_in_version <> (SELECT latest_version FROM incidents WHERE id = OLD.incident_id) THEN
                RAISE EXCEPTION 'deadline %: closed only by the incident''s latest version, after the one that computed it', OLD.id
                    USING ERRCODE = 'check_violation';
            END IF;
        ELSE
            SELECT submitted_by, submitted_at, amend_reason INTO closer
            FROM site_work_versions WHERE site_work_id = OLD.site_work_id AND version = NEW.closed_in_version;
            IF NOT FOUND OR NEW.closed_in_version <= OLD.computed_in_version
               OR NEW.closed_in_version <> (SELECT latest_version FROM site_work WHERE id = OLD.site_work_id) THEN
                RAISE EXCEPTION 'deadline %: closed only by the site work''s latest version, after the one that computed it', OLD.id
                    USING ERRCODE = 'check_violation';
            END IF;
        END IF;
        NEW.closed_by := closer.submitted_by;
        NEW.closed_at := closer.submitted_at;
        NEW.closed_reason := closer.amend_reason;
        RETURN NEW;
    END
    $$;

-- As 000029, for notifications too (REG4): withdrawing an unsatisfied
-- notification, or a later start moving it later, needs a lead.
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
                successor.time_zone, successor.jurisdiction, successor.regime_id, successor.proof_document_types)
               IS DISTINCT FROM (d.duty_id, d.rule_key, d.period, d.unit, d.direction, d.basis_at, d.time_zone, d.jurisdiction,
                                 d.regime_id, d.proof_document_types) THEN
                RAISE EXCEPTION 'deadline %: a recomputation keeps the rule and its inputs', d.id
                    USING ERRCODE = 'check_violation';
            END IF;
            RETURN NULL;
        END IF;
        IF d.closed_as IS NULL OR d.kind NOT IN ('report', 'notification') THEN
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
            RAISE EXCEPTION 'deadline %: only an owner or a SHE officer can withdraw a % deadline or move it later', d.id, d.kind
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

-- As 000017, but a before-deadline is met before its basis (the notice
-- precedes the work), so the lower bound is only for after-deadlines; and a
-- linked document must be one of the deadline's proof types, not retracted.
CREATE OR REPLACE FUNCTION deadline_satisfactions_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        d   deadlines;
        doc record;
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
        IF FOUND AND d.direction = 'after' AND NEW.satisfied_at < d.basis_at - interval '5 minutes' THEN
            RAISE EXCEPTION 'satisfaction %: satisfied_at is before the deadline''s basis', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF FOUND AND NEW.document_id IS NOT NULL THEN
            SELECT x.type_key, x.retracted_at AS doc_retracted, v.retracted_at AS version_retracted INTO doc
            FROM documents x JOIN document_versions v ON v.document_id = x.id AND v.version = NEW.document_version
            WHERE x.id = NEW.document_id;
            IF FOUND AND NOT (doc.type_key = ANY (d.proof_document_types)) THEN
                RAISE EXCEPTION 'satisfaction %: a % document is not proof of this deadline', NEW.id, doc.type_key
                    USING ERRCODE = 'check_violation';
            END IF;
            IF FOUND AND (doc.doc_retracted IS NOT NULL OR doc.version_retracted IS NOT NULL) THEN
                RAISE EXCEPTION 'satisfaction %: that document version is retracted', NEW.id
                    USING ERRCODE = 'check_violation';
            END IF;
        END IF;
        RETURN NEW;
    END
    $$;

-- As 000030: the replacement is computed in its subject's latest version and
-- copies the new columns.
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
            INSERT INTO deadlines (id, org_id, site_id, duty_id, incident_id, site_work_id, regime_id, rule_key, rule_label, kind, period,
                                   unit, direction, basis, source_ref, caveat, proof_document_types, basis_at, time_zone, jurisdiction,
                                   due_at, excluded_days, computed_in_version)
            VALUES (new_id, d.org_id, d.site_id, d.duty_id, d.incident_id, d.site_work_id, d.regime_id, d.rule_key, d.rule_label, d.kind,
                    d.period, d.unit, d.direction, d.basis, d.source_ref, d.caveat, d.proof_document_types, d.basis_at, d.time_zone,
                    d.jurisdiction, r.due_at, r.excluded_days,
                    CASE WHEN d.incident_id IS NOT NULL THEN (SELECT latest_version FROM incidents WHERE id = d.incident_id)
                         ELSE (SELECT latest_version FROM site_work WHERE id = d.site_work_id) END);
            n := n + 1;
        END LOOP;
        PERFORM set_config('app.recompute_holiday', '', true);
        INSERT INTO holiday_recomputations (org_id, jurisdiction, holiday_date, recomputed)
        VALUES (app_org_id(), p_jurisdiction, p_day, n);
        RETURN n;
    END
    $$;

COMMIT;
