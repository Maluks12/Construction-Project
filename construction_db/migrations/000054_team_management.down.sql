BEGIN;

DROP INDEX org_members_sync_idx;
DROP TRIGGER zz_sync_stamp ON org_members;
ALTER TABLE org_members DROP COLUMN sync_xid;

DROP TRIGGER org_members_changed ON org_members;
DROP FUNCTION org_members_changed();

CREATE FUNCTION org_members_history() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public
    AS $$
    BEGIN
        IF TG_OP = 'UPDATE' AND (NEW.role, NEW.status) IS NOT DISTINCT FROM (OLD.role, OLD.status) THEN
            RETURN NULL;
        END IF;
        INSERT INTO member_history (org_id, user_id, role, status, full_name, changed_at, changed_by)
        SELECT NEW.org_id, NEW.user_id, NEW.role, NEW.status, u.full_name, clock_timestamp(), app_user_id()
        FROM users u WHERE u.id = NEW.user_id;
        RETURN NULL;
    END
    $$;
CREATE TRIGGER org_members_history AFTER INSERT OR UPDATE ON org_members
    FOR EACH ROW EXECUTE FUNCTION org_members_history();

DROP TRIGGER site_members_history ON site_members;
DROP FUNCTION site_members_history();
DROP TABLE site_member_history;

-- member_history is append-only: its new columns' values go with them.
ALTER TABLE member_history
    DROP COLUMN reason,
    DROP COLUMN prev_role,
    DROP COLUMN prev_status,
    DROP COLUMN site_ids,
    DROP COLUMN prev_site_ids;

GRANT DELETE ON org_members TO siteproof_app;
DROP TRIGGER org_members_no_truncate ON org_members;
DROP TRIGGER org_members_no_delete ON org_members;
DROP FUNCTION org_members_no_delete();

DROP TRIGGER org_members_guard ON org_members;
DROP FUNCTION org_members_guard();
DROP FUNCTION member_change_allowed(text, text, text);

COMMIT;
