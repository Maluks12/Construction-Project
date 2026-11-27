BEGIN;

-- ONBOARD team management (full rigour; D1-D7 in construction_db/CLAUDE.md).
-- Every rule lives here, on org_members itself, so it holds on every path:
-- the members PATCH, invites, signup, or SQL typed by hand.
--
-- Errors carry a CONSTRAINT name, which the API maps (members.writeError):
--   org_members_last_owner   23514  the organisation would lose its last active owner
--   org_members_actor        42501  who is acting may not make this change
--   org_members_self         42501  nobody changes their own membership
--   org_members_reason       23514  a change of an active or suspended membership needs a reason
--   org_members_transition   23514  not a status change PATCH or an invite makes
--   org_members_site_ids     23514  into supervisor: site_ids missing or not the org's live sites
--   org_members_no_delete    23514  memberships are deactivated, never deleted
--
-- Two transaction-local settings carry what the row can't:
--   siteproof.member_reason    why (1-500 characters after trimming)
--   siteproof.member_site_ids  a uuid[] literal: a new supervisor's sites

-- D1: who may change whom. auth.MemberChangeAllowed is its Go twin
-- (TestMemberRulesMatchDatabase compares every combination of roles).
CREATE FUNCTION member_change_allowed(p_actor text, p_from text, p_to text) RETURNS boolean
    LANGUAGE sql IMMUTABLE STRICT
    AS $$
    SELECT CASE p_actor
        WHEN 'owner' THEN true
        WHEN 'she_officer' THEN p_from <> 'owner' AND p_to <> 'owner'
        ELSE false
    END
    $$;

-- The rules, before the row changes. SECURITY DEFINER: the counts and the
-- actor's own row are read whatever the tenant (a missing tenant must not
-- make "no other owner" look true).
CREATE FUNCTION org_members_guard() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public
    AS $$
    DECLARE
        actor      uuid := app_user_id();
        actor_role text;
        reason     text := current_setting('siteproof.member_reason', true);
        owners     integer;
    BEGIN
        -- D3: one lock order for every writer, the organisation's row first.
        -- Two owners demoting each other queue here; the second then counts
        -- with a fresh snapshot (READ COMMITTED: a new one per statement) and
        -- sees the first's demotion. FOR NO KEY UPDATE doesn't conflict with
        -- the FOR KEY SHARE that foreign-key checks take.
        PERFORM 1 FROM organisations WHERE id = NEW.org_id FOR NO KEY UPDATE;

        IF TG_OP = 'INSERT' THEN
            RETURN NEW;
        END IF;
        IF (NEW.org_id, NEW.user_id) IS DISTINCT FROM (OLD.org_id, OLD.user_id) THEN
            RAISE EXCEPTION 'a membership''s organisation and user never change'
                USING ERRCODE = 'check_violation', CONSTRAINT = 'org_members_transition';
        END IF;
        IF (NEW.role, NEW.status) IS NOT DISTINCT FROM (OLD.role, OLD.status) THEN
            RETURN NEW;
        END IF;

        -- Accepting an invitation: the invitee, role unchanged.
        IF OLD.status = 'invited' AND NEW.status = 'active' THEN
            IF NEW.role <> OLD.role THEN
                RAISE EXCEPTION 'accepting an invitation keeps its role'
                    USING ERRCODE = 'check_violation', CONSTRAINT = 'org_members_transition';
            END IF;
            RETURN NEW;
        END IF;

        IF NOT ((OLD.status, NEW.status) IN (
                ('active', 'active'), ('suspended', 'suspended'),
                ('active', 'suspended'), ('suspended', 'active'),
                ('active', 'removed'), ('suspended', 'removed'), ('invited', 'removed'),
                ('removed', 'invited'), ('invited', 'invited'))) THEN
            RAISE EXCEPTION 'a membership can''t go from % to %', OLD.status, NEW.status
                USING ERRCODE = 'check_violation', CONSTRAINT = 'org_members_transition';
        END IF;

        -- D3: never the last active owner. Checked before who is acting: in
        -- two owners demoting each other, the second, once the lock lets it
        -- through, is refused here, by the rule the race is about.
        IF OLD.role = 'owner' AND OLD.status = 'active' AND NOT (NEW.role = 'owner' AND NEW.status = 'active') THEN
            SELECT count(*) INTO owners FROM org_members
            WHERE org_id = NEW.org_id AND user_id <> NEW.user_id AND role = 'owner' AND status = 'active';
            IF owners = 0 THEN
                RAISE EXCEPTION 'an organisation must keep at least one active owner'
                    USING ERRCODE = 'check_violation', CONSTRAINT = 'org_members_last_owner';
            END IF;
        END IF;

        -- Every other change has someone making it, an active member here: no
        -- app.user_id, or one who isn't active here, leaves actor_role NULL,
        -- and each branch below refuses that.
        SELECT role INTO actor_role FROM org_members
        WHERE org_id = NEW.org_id AND user_id = actor AND status = 'active';

        -- An invitation (sent again, or to a removed member): the sender may
        -- grant the role. Invites never touch active or suspended members.
        IF NEW.status = 'invited' THEN
            IF actor_role IS NULL OR NOT member_change_allowed(actor_role, NEW.role, NEW.role) THEN
                RAISE EXCEPTION 'only an owner can invite an owner'
                    USING ERRCODE = 'insufficient_privilege', CONSTRAINT = 'org_members_actor';
            END IF;
            RETURN NEW;
        END IF;

        -- The members PATCH (D1, D2, D6).
        IF actor = NEW.user_id THEN
            RAISE EXCEPTION 'nobody changes their own membership'
                USING ERRCODE = 'insufficient_privilege', CONSTRAINT = 'org_members_self';
        END IF;
        IF actor_role IS NULL OR NOT member_change_allowed(actor_role, OLD.role, NEW.role) THEN
            RAISE EXCEPTION 'a % may not change a % to %', coalesce(actor_role, 'non-member'), OLD.role, NEW.role
                USING ERRCODE = 'insufficient_privilege', CONSTRAINT = 'org_members_actor';
        END IF;
        IF reason IS NULL OR length(btrim(reason)) NOT BETWEEN 1 AND 500 THEN
            RAISE EXCEPTION 'a membership change needs a reason of 1-500 characters'
                USING ERRCODE = 'check_violation', CONSTRAINT = 'org_members_reason';
        END IF;

        RETURN NEW;
    END
    $$;
CREATE TRIGGER org_members_guard BEFORE INSERT OR UPDATE ON org_members
    FOR EACH ROW EXECUTE FUNCTION org_members_guard();

-- D2: deactivated, never deleted (58 foreign keys pin memberships).
CREATE FUNCTION org_members_no_delete() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        RAISE EXCEPTION 'memberships are never deleted; set status removed'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'org_members_no_delete';
    END
    $$;
CREATE TRIGGER org_members_no_delete BEFORE DELETE ON org_members
    FOR EACH ROW EXECUTE FUNCTION org_members_no_delete();
CREATE TRIGGER org_members_no_truncate BEFORE TRUNCATE ON org_members
    FOR EACH STATEMENT EXECUTE FUNCTION org_members_no_delete();
REVOKE DELETE, TRUNCATE ON org_members FROM siteproof_app;

-- D6: who, when, from, to, why, and the sites a change cleared or set.
ALTER TABLE member_history
    ADD COLUMN reason        text,
    ADD COLUMN prev_role     text,
    ADD COLUMN prev_status   text,
    ADD COLUMN site_ids      uuid[],
    ADD COLUMN prev_site_ids uuid[];

-- D7b: every addition to and removal from a site, by any path.
CREATE TABLE site_member_history (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id     uuid NOT NULL,
    site_id    uuid NOT NULL,
    user_id    uuid NOT NULL,
    change     text NOT NULL CHECK (change IN ('added', 'removed')),
    changed_at timestamptz NOT NULL,
    changed_by uuid
);
CREATE INDEX site_member_history_idx ON site_member_history (org_id, user_id, changed_at DESC, id DESC);
CREATE TRIGGER site_member_history_append_only BEFORE UPDATE OR DELETE ON site_member_history
    FOR EACH ROW EXECUTE FUNCTION history_append_only();
CREATE TRIGGER site_member_history_no_truncate BEFORE TRUNCATE ON site_member_history
    FOR EACH STATEMENT EXECUTE FUNCTION history_append_only();
ALTER TABLE site_member_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE site_member_history FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON site_member_history FOR ALL TO siteproof_app
    USING (org_id = app_org_id()) WITH CHECK (org_id = app_org_id());
REVOKE INSERT, UPDATE, DELETE, TRUNCATE ON site_member_history FROM siteproof_app;

INSERT INTO site_member_history (org_id, site_id, user_id, change, changed_at)
SELECT org_id, site_id, user_id, 'added', created_at FROM site_members;

CREATE FUNCTION site_members_history() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public
    AS $$
    BEGIN
        IF TG_OP = 'INSERT' THEN
            INSERT INTO site_member_history (org_id, site_id, user_id, change, changed_at, changed_by)
            VALUES (NEW.org_id, NEW.site_id, NEW.user_id, 'added', clock_timestamp(), app_user_id());
        ELSE
            INSERT INTO site_member_history (org_id, site_id, user_id, change, changed_at, changed_by)
            VALUES (OLD.org_id, OLD.site_id, OLD.user_id, 'removed', clock_timestamp(), app_user_id());
        END IF;
        RETURN NULL;
    END
    $$;
CREATE TRIGGER site_members_history AFTER INSERT OR DELETE ON site_members
    FOR EACH ROW EXECUTE FUNCTION site_members_history();

-- What a change does, then its history row (one function: only here are the
-- sites before and after both known). Replaces 000037's writer.
DROP TRIGGER org_members_history ON org_members;
DROP FUNCTION org_members_history();

CREATE FUNCTION org_members_changed() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public
    AS $$
    DECLARE
        before_sites uuid[];
        after_sites  uuid[];
        wanted       uuid[];
        added        integer;
    BEGIN
        IF TG_OP = 'UPDATE' AND (NEW.role, NEW.status) IS NOT DISTINCT FROM (OLD.role, OLD.status) THEN
            RETURN NULL;
        END IF;

        IF TG_OP = 'UPDATE' THEN
            SELECT coalesce(array_agg(site_id ORDER BY site_id), '{}') INTO before_sites
            FROM site_members WHERE org_id = NEW.org_id AND user_id = NEW.user_id;

            -- D7: out of supervisor, or removed: their sites go.
            IF NEW.status = 'removed' OR (OLD.role = 'supervisor' AND NEW.role <> 'supervisor') THEN
                DELETE FROM site_members WHERE org_id = NEW.org_id AND user_id = NEW.user_id;
            END IF;

            -- D7: into supervisor (the PATCH): exactly the sites named.
            IF NEW.role = 'supervisor' AND OLD.role <> 'supervisor' AND OLD.status IN ('active', 'suspended')
               AND NEW.status <> 'removed' THEN
                wanted := nullif(current_setting('siteproof.member_site_ids', true), '')::uuid[];
                IF wanted IS NULL THEN
                    RAISE EXCEPTION 'site_ids is required when the role becomes supervisor'
                        USING ERRCODE = 'check_violation', CONSTRAINT = 'org_members_site_ids';
                END IF;
                DELETE FROM site_members WHERE org_id = NEW.org_id AND user_id = NEW.user_id;
                INSERT INTO site_members (org_id, site_id, user_id)
                SELECT NEW.org_id, s.id, NEW.user_id FROM sites s
                WHERE s.org_id = NEW.org_id AND s.id = ANY (wanted) AND s.status <> 'archived';
                GET DIAGNOSTICS added = ROW_COUNT;
                IF added <> (SELECT count(DISTINCT w) FROM unnest(wanted) w) THEN
                    RAISE EXCEPTION 'site_ids must name the organisation''s sites that aren''t archived'
                        USING ERRCODE = 'check_violation', CONSTRAINT = 'org_members_site_ids';
                END IF;
            END IF;

            SELECT coalesce(array_agg(site_id ORDER BY site_id), '{}') INTO after_sites
            FROM site_members WHERE org_id = NEW.org_id AND user_id = NEW.user_id;

            -- D4: suspended or removed: signed out everywhere in this org now,
            -- their refresh tokens with them; D5: nothing more is emailed.
            IF NEW.status IN ('suspended', 'removed') AND OLD.status <> NEW.status THEN
                UPDATE sessions SET revoked_at = now()
                WHERE org_id = NEW.org_id AND user_id = NEW.user_id AND revoked_at IS NULL;
                UPDATE notification_emails e SET status = 'cancelled'
                FROM notifications n
                WHERE n.org_id = e.org_id AND n.id = e.notification_id
                  AND n.org_id = NEW.org_id AND n.user_id = NEW.user_id AND e.status = 'pending';
            END IF;

            -- Removing an invited member withdraws the invitation.
            IF NEW.status = 'removed' AND OLD.status = 'invited' THEN
                UPDATE invitations SET revoked_at = now()
                WHERE org_id = NEW.org_id AND user_id = NEW.user_id AND accepted_at IS NULL AND revoked_at IS NULL;
            END IF;
        END IF;

        INSERT INTO member_history (org_id, user_id, role, status, full_name, changed_at, changed_by,
                                    reason, prev_role, prev_status, site_ids, prev_site_ids)
        SELECT NEW.org_id, NEW.user_id, NEW.role, NEW.status, u.full_name, clock_timestamp(), app_user_id(),
               nullif(btrim(current_setting('siteproof.member_reason', true)), ''),
               CASE WHEN TG_OP = 'UPDATE' THEN OLD.role END,
               CASE WHEN TG_OP = 'UPDATE' THEN OLD.status END,
               CASE WHEN before_sites IS DISTINCT FROM after_sites THEN after_sites END,
               CASE WHEN before_sites IS DISTINCT FROM after_sites THEN before_sites END
        FROM users u WHERE u.id = NEW.user_id;
        RETURN NULL;
    END
    $$;
CREATE TRIGGER org_members_changed AFTER INSERT OR UPDATE ON org_members
    FOR EACH ROW EXECUTE FUNCTION org_members_changed();

-- D5b: a member's change re-sends the actions they own (owner_eligible).
ALTER TABLE org_members ADD COLUMN sync_xid xid8 NOT NULL DEFAULT pg_current_xact_id();
CREATE TRIGGER zz_sync_stamp BEFORE INSERT OR UPDATE ON org_members
    FOR EACH ROW EXECUTE FUNCTION sync_stamp();
CREATE INDEX org_members_sync_idx ON org_members (org_id, sync_xid);

COMMIT;
