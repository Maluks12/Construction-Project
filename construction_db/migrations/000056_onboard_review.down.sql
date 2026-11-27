BEGIN;

DROP TRIGGER zz_sync_deleted ON site_members;
DROP FUNCTION site_members_sync_deleted();
DROP INDEX site_members_sync_idx;
DROP TRIGGER zz_sync_stamp ON site_members;
ALTER TABLE site_members DROP COLUMN sync_xid;

-- The three guards as they were.

CREATE OR REPLACE FUNCTION inspections_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        rest      inspections;
        responses integer;
        missing   text;
    BEGIN
        IF OLD.status = 'completed' THEN
            RAISE EXCEPTION 'inspection % is completed; start a new inspection to correct it', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF OLD.status = 'abandoned' THEN
            RAISE EXCEPTION 'inspection % is abandoned', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF TG_OP = 'DELETE' THEN
            RETURN OLD; -- in progress: privileges decide (the app has none)
        END IF;

        rest := NEW;
        rest.status := OLD.status;
        rest.completed_at := OLD.completed_at;
        rest.client_completed_at := OLD.client_completed_at;
        rest.abandoned_at := OLD.abandoned_at;
        rest.abandoned_by := OLD.abandoned_by;
        rest.abandon_reason := OLD.abandon_reason;
        IF NEW.status NOT IN ('completed', 'abandoned') OR rest IS DISTINCT FROM OLD THEN
            RAISE EXCEPTION 'inspection %: the only change allowed is completing or abandoning it', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;

        IF NEW.status = 'abandoned' THEN
            IF EXISTS (SELECT 1 FROM inspection_responses WHERE inspection_id = OLD.id AND result = 'fail')
               AND NOT EXISTS (SELECT 1 FROM org_members
                               WHERE org_id = NEW.org_id AND user_id = NEW.abandoned_by AND role IN ('owner', 'she_officer')) THEN
                RAISE EXCEPTION 'inspection %: an inspection with a failed item can only be abandoned by an owner or a SHE officer', OLD.id
                    USING ERRCODE = 'check_violation';
            END IF;
            RETURN NEW;
        END IF;

        -- These queries see every answer committed before this UPDATE got
        -- the row lock (READ COMMITTED; each statement takes a new snapshot).
        SELECT count(*) INTO responses FROM inspection_responses WHERE inspection_id = OLD.id;
        IF responses <> OLD.item_count THEN
            RAISE EXCEPTION 'inspection %: has % responses but its template version has % items', OLD.id, responses, OLD.item_count
                USING ERRCODE = 'check_violation';
        END IF;

        SELECT string_agg(item_key, ', ' ORDER BY position) INTO missing
        FROM inspection_responses WHERE inspection_id = OLD.id AND result IS NULL;
        IF missing IS NOT NULL THEN
            RAISE EXCEPTION 'inspection %: items not answered: %', OLD.id, missing
                USING ERRCODE = 'check_violation';
        END IF;

        SELECT string_agg(r.item_key, ', ' ORDER BY r.position) INTO missing
        FROM inspection_responses r
        WHERE r.inspection_id = OLD.id AND r.result = 'fail'
          AND NOT EXISTS (SELECT 1 FROM inspection_response_evidence e
                          WHERE e.inspection_id = r.inspection_id AND e.item_key = r.item_key);
        IF missing IS NOT NULL THEN
            RAISE EXCEPTION 'inspection %: a failed item needs a photo: %', OLD.id, missing
                USING ERRCODE = 'check_violation';
        END IF;

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

CREATE OR REPLACE FUNCTION deadline_satisfaction_retractions_release() RETURNS trigger
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

DROP TRIGGER org_members_insert_guard ON org_members;
DROP FUNCTION org_members_insert_guard();

COMMIT;
