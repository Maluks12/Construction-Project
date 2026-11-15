BEGIN;

-- Fails if any inspection is abandoned or a site has an abandoned correction
-- next to another correction: the old constraints can't hold them.

-- The 000012 functions, verbatim.
CREATE OR REPLACE FUNCTION inspections_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF NEW.status <> 'in_progress' OR NEW.completed_at IS NOT NULL OR NEW.client_completed_at IS NOT NULL THEN
            RAISE EXCEPTION 'inspection %: a new inspection starts in progress', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.corrects_id IS NOT NULL
           AND NOT EXISTS (SELECT 1 FROM inspections WHERE id = NEW.corrects_id AND status = 'completed') THEN
            RAISE EXCEPTION 'inspection %: can only correct a completed inspection', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

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
        IF TG_OP = 'DELETE' THEN
            RETURN OLD; -- in progress: privileges decide (the app has none)
        END IF;

        rest := NEW;
        rest.status := OLD.status;
        rest.completed_at := OLD.completed_at;
        rest.client_completed_at := OLD.client_completed_at;
        IF NEW.status <> 'completed' OR rest IS DISTINCT FROM OLD THEN
            RAISE EXCEPTION 'inspection %: the only change allowed is completing it', OLD.id
                USING ERRCODE = 'check_violation';
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

CREATE OR REPLACE FUNCTION inspection_responses_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        parent_id     uuid;
        parent_status text;
        rest          inspection_responses;
    BEGIN
        IF TG_OP = 'DELETE' THEN
            parent_id := OLD.inspection_id;
        ELSE
            parent_id := NEW.inspection_id;
        END IF;
        SELECT status INTO parent_status FROM inspections WHERE id = parent_id FOR SHARE;
        IF FOUND AND parent_status <> 'in_progress' THEN
            RAISE EXCEPTION 'inspection % is completed; start a new inspection to correct it', parent_id
                USING ERRCODE = 'check_violation';
        END IF;

        IF TG_OP = 'UPDATE' THEN
            -- Only the answer changes; the copy of the item never does.
            rest := NEW;
            rest.result := OLD.result;
            rest.severity := OLD.severity;
            rest.comment := OLD.comment;
            rest.client_answered_at := OLD.client_answered_at;
            rest.answered_at := OLD.answered_at;
            IF rest IS DISTINCT FROM OLD THEN
                RAISE EXCEPTION 'inspection % item %: only the answer can change', OLD.inspection_id, OLD.item_key
                    USING ERRCODE = 'check_violation';
            END IF;
        END IF;

        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;
        RETURN NEW;
    END
    $$;

CREATE OR REPLACE FUNCTION inspection_response_evidence_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        parent_id     uuid;
        parent_status text;
    BEGIN
        IF TG_OP = 'UPDATE' THEN
            RAISE EXCEPTION 'a photo link never changes; remove it and add another'
                USING ERRCODE = 'check_violation';
        END IF;
        IF TG_OP = 'DELETE' THEN
            parent_id := OLD.inspection_id;
        ELSE
            parent_id := NEW.inspection_id;
        END IF;
        SELECT status INTO parent_status FROM inspections WHERE id = parent_id FOR SHARE;
        IF FOUND AND parent_status <> 'in_progress' THEN
            RAISE EXCEPTION 'inspection % is completed; start a new inspection to correct it', parent_id
                USING ERRCODE = 'check_violation';
        END IF;
        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;
        RETURN NEW;
    END
    $$;

DROP INDEX inspections_corrects_once;
ALTER TABLE inspections ADD CONSTRAINT inspections_corrects_once UNIQUE (corrects_id);

ALTER TABLE inspections
    DROP CONSTRAINT inspections_abandonment,
    DROP CONSTRAINT inspections_abandoned_by_fkey,
    DROP COLUMN abandon_reason,
    DROP COLUMN abandoned_by,
    DROP COLUMN abandoned_at;

ALTER TABLE inspections DROP CONSTRAINT inspections_status_check;
ALTER TABLE inspections ADD CONSTRAINT inspections_status_check CHECK (status IN ('in_progress', 'completed'));

COMMIT;
