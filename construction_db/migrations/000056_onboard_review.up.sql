BEGIN;

-- ONBOARD review fixes (Thu 26 Nov). R3: a membership is created only as
-- an organisation's first member (its owner, at signup) or as an
-- invitation sent by someone allowed to grant the role. R2: the three guards
-- that asked whether someone is an owner or SHE officer without asking
-- whether they still are (status 'active') now ask action_lead(). R1: a
-- site membership's addition or removal re-sends the actions its member
-- owns on that site (owner_eligible).

-- R3. After the insert, so a cross-tenant insert is refused by RLS first;
-- org_members_guard (BEFORE) has already taken the organisation's lock, so
-- two "first" members queue and the second counts two.
CREATE FUNCTION org_members_insert_guard() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public
    AS $$
    DECLARE
        actor_role text;
    BEGIN
        IF NEW.status = 'active' THEN
            IF NEW.role = 'owner' AND (SELECT count(*) FROM org_members WHERE org_id = NEW.org_id) = 1 THEN
                RETURN NULL;
            END IF;
            RAISE EXCEPTION 'a membership starts as an invitation; only an organisation''s first member, its owner, starts active'
                USING ERRCODE = 'check_violation', CONSTRAINT = 'org_members_transition';
        END IF;
        IF NEW.status <> 'invited' THEN
            RAISE EXCEPTION 'a membership starts as an invitation, not %', NEW.status
                USING ERRCODE = 'check_violation', CONSTRAINT = 'org_members_transition';
        END IF;
        SELECT role INTO actor_role FROM org_members
        WHERE org_id = NEW.org_id AND user_id = app_user_id() AND status = 'active';
        IF actor_role IS NULL OR NOT member_change_allowed(actor_role, NEW.role, NEW.role) THEN
            RAISE EXCEPTION 'only an active owner or SHE officer invites, and only an owner invites an owner'
                USING ERRCODE = 'insufficient_privilege', CONSTRAINT = 'org_members_actor';
        END IF;
        RETURN NULL;
    END
    $$;
CREATE TRIGGER org_members_insert_guard AFTER INSERT ON org_members
    FOR EACH ROW EXECUTE FUNCTION org_members_insert_guard();

-- R2: from their latest versions (000014, 000046, 000017), each with its
-- role check replaced by action_lead() (000018: an active owner or SHE
-- officer).

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
               AND NOT action_lead(NEW.org_id, NEW.abandoned_by) THEN
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
        IF weakens AND NOT action_lead(d.org_id, d.closed_by) THEN
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
        IF NOT action_lead(NEW.org_id, NEW.retracted_by) THEN
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

-- R1, D1: additions are stamped; a removal (the site member endpoint, or
-- the 000054 trigger clearing a former supervisor's sites) records each
-- action the member owns on that site in sync_deletions, which the action
-- kind reads as "render it again" (a 404 there sends nothing).
ALTER TABLE site_members ADD COLUMN sync_xid xid8 NOT NULL DEFAULT pg_current_xact_id();
CREATE TRIGGER zz_sync_stamp BEFORE INSERT OR UPDATE ON site_members
    FOR EACH ROW EXECUTE FUNCTION sync_stamp();
CREATE INDEX site_members_sync_idx ON site_members (site_id, sync_xid);

CREATE FUNCTION site_members_sync_deleted() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public
    AS $$
    BEGIN
        INSERT INTO sync_deletions (org_id, site_id, kind, root)
        SELECT OLD.org_id, OLD.site_id, 'action', a.id::text FROM corrective_actions a
        WHERE a.org_id = OLD.org_id AND a.site_id = OLD.site_id AND a.owner_id = OLD.user_id;
        RETURN OLD;
    END
    $$;
CREATE TRIGGER zz_sync_deleted AFTER DELETE ON site_members
    FOR EACH ROW EXECUTE FUNCTION site_members_sync_deleted();

COMMIT;
