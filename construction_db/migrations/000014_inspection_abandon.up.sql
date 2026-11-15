BEGIN;

-- An inspection started by mistake, or whose inspector has left, can be
-- abandoned (decided 15 Nov). Abandoned is final like completed: nothing
-- about it changes, its answers stay, it raises no findings, and it stays in
-- lists and the audit pack, marked abandoned with who, when and why.
ALTER TABLE inspections DROP CONSTRAINT inspections_status_check;
ALTER TABLE inspections ADD CONSTRAINT inspections_status_check
    CHECK (status IN ('in_progress', 'completed', 'abandoned'));

ALTER TABLE inspections
    ADD COLUMN abandoned_at   timestamptz,
    ADD COLUMN abandoned_by   uuid,
    ADD COLUMN abandon_reason text CHECK (char_length(abandon_reason) BETWEEN 1 AND 500),
    ADD CONSTRAINT inspections_abandoned_by_fkey FOREIGN KEY (org_id, abandoned_by) REFERENCES org_members (org_id, user_id),
    ADD CONSTRAINT inspections_abandonment CHECK (
        (status = 'abandoned') = (abandoned_at IS NOT NULL)
        AND (abandoned_at IS NULL) = (abandoned_by IS NULL)
        AND (abandoned_at IS NULL) = (abandon_reason IS NULL));

-- Abandoning frees the correction slot: only one correction that isn't
-- abandoned per inspection. Same name, so the API's error mapping holds.
ALTER TABLE inspections DROP CONSTRAINT inspections_corrects_once;
CREATE UNIQUE INDEX inspections_corrects_once ON inspections (corrects_id) WHERE status <> 'abandoned';

-- A new inspection starts in progress, neither completed nor abandoned. A
-- correction must point at a completed inspection (not an abandoned one).
CREATE OR REPLACE FUNCTION inspections_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF NEW.status <> 'in_progress' OR NEW.completed_at IS NOT NULL OR NEW.client_completed_at IS NOT NULL
           OR NEW.abandoned_at IS NOT NULL OR NEW.abandoned_by IS NOT NULL OR NEW.abandon_reason IS NOT NULL THEN
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

-- Completed and abandoned inspections never change. One in progress changes
-- only by being completed (if complete) or abandoned. Abandoning one with a
-- failed item takes an owner or a SHE officer, so a fail can't be made to
-- disappear by the inspector abandoning instead of completing.
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

-- Responses and photo links change only while their inspection is in
-- progress; the message names what it is instead. The FOR SHARE lock is as in
-- 000012.
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
        IF FOUND AND parent_status = 'completed' THEN
            RAISE EXCEPTION 'inspection % is completed; start a new inspection to correct it', parent_id
                USING ERRCODE = 'check_violation';
        END IF;
        IF FOUND AND parent_status = 'abandoned' THEN
            RAISE EXCEPTION 'inspection % is abandoned', parent_id
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
        IF FOUND AND parent_status = 'completed' THEN
            RAISE EXCEPTION 'inspection % is completed; start a new inspection to correct it', parent_id
                USING ERRCODE = 'check_violation';
        END IF;
        IF FOUND AND parent_status = 'abandoned' THEN
            RAISE EXCEPTION 'inspection % is abandoned', parent_id
                USING ERRCODE = 'check_violation';
        END IF;
        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;
        RETURN NEW;
    END
    $$;

-- inspections_raise_findings fires only on completed, and inspection_findings_guard
-- needs a completed parent: abandoned inspections raise no findings, unchanged.

GRANT UPDATE (abandoned_at, abandoned_by, abandon_reason) ON inspections TO siteproof_app;

COMMIT;
