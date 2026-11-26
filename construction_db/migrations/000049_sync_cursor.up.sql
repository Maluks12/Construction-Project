BEGIN;

-- SYNC D1: a pull cursor that can't pass a slow commit.
--
-- Every synced row carries sync_xid, the id of the transaction that last
-- wrote it. A pull reads rows with lo <= sync_xid < hi, where hi is the
-- xmin of its own snapshot: every transaction below it has finished
-- (committed and visible, or rolled back), so a slow writer holds hi back
-- and is never passed. Server times (now()) are taken before commit and
-- can't do this.
--
-- zz_sync_stamp fires last among BEFORE triggers (they fire in name order),
-- so every guard comparing NEW with OLD sees sync_xid unchanged. A no-op
-- update keeps the old value.

CREATE FUNCTION sync_stamp() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF TG_OP = 'UPDATE' AND NEW IS NOT DISTINCT FROM OLD THEN
            RETURN NEW;
        END IF;
        NEW.sync_xid := pg_current_xact_id();
        RETURN NEW;
    END
    $$;

-- A deleted row's aggregate must still be re-sent: the deletion is recorded
-- here under the aggregate's root (TG_ARGV: kind, root column).
CREATE TABLE sync_deletions (
    id        bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id    uuid NOT NULL REFERENCES organisations (id),
    site_id   uuid NOT NULL,
    kind      text NOT NULL,
    root      text NOT NULL,
    sync_xid  xid8 NOT NULL DEFAULT pg_current_xact_id(),
    FOREIGN KEY (org_id, site_id) REFERENCES sites (org_id, id)
);

CREATE FUNCTION sync_deleted() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public
    AS $$
    DECLARE
        r text;
    BEGIN
        EXECUTE format('SELECT ($1).%I::text', TG_ARGV[1]) INTO r USING OLD;
        INSERT INTO sync_deletions (org_id, site_id, kind, root) VALUES (OLD.org_id, OLD.site_id, TG_ARGV[0], r);
        RETURN OLD;
    END
    $$;

ALTER TABLE sync_deletions ENABLE ROW LEVEL SECURITY;
ALTER TABLE sync_deletions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON sync_deletions FOR ALL TO siteproof_app
    USING (org_id = app_org_id()) WITH CHECK (org_id = app_org_id());
REVOKE INSERT, UPDATE, DELETE, TRUNCATE ON sync_deletions FROM siteproof_app;
CREATE TRIGGER sync_deletions_append_only BEFORE UPDATE OR DELETE ON sync_deletions
    FOR EACH ROW EXECUTE FUNCTION history_append_only();
CREATE TRIGGER sync_deletions_no_truncate BEFORE TRUNCATE ON sync_deletions
    FOR EACH STATEMENT EXECUTE FUNCTION history_append_only();

-- The synced tables and the column their pull index leads with. ADD COLUMN
-- with a volatile default rewrites each table once (no triggers fire), so
-- existing rows all carry this migration's xid: phones start with a full sync.
DO $$
DECLARE
    t record;
BEGIN
    FOR t IN SELECT * FROM (VALUES
        ('inspections', 'site_id'), ('inspection_responses', 'site_id'),
        ('inspection_response_evidence', 'site_id'), ('inspection_findings', 'site_id'),
        ('incidents', 'site_id'), ('incident_versions', 'site_id'), ('incident_people', 'site_id'),
        ('incident_version_evidence', 'site_id'), ('witness_statements', 'site_id'),
        ('deadline_duties', 'site_id'), ('deadlines', 'site_id'), ('deadline_satisfactions', 'site_id'),
        ('deadline_satisfaction_retractions', 'site_id'),
        ('corrective_actions', 'site_id'), ('corrective_action_events', 'site_id'),
        ('corrective_action_closeouts', 'site_id'), ('corrective_action_closeout_evidence', 'site_id'),
        ('finding_waivers', 'site_id'),
        ('toolbox_talks', 'site_id'), ('toolbox_talk_evidence', 'site_id'), ('toolbox_attendance', 'site_id'),
        ('toolbox_attendance_retractions', 'site_id'), ('toolbox_attendance_workers', 'site_id'),
        ('site_diaries', 'site_id'), ('site_diary_revisions', 'site_id'),
        ('site_diary_revision_evidence', 'site_id'), ('site_diary_addenda', 'site_id'),
        ('site_diary_addendum_evidence', 'site_id'),
        ('site_sign_ins', 'site_id'), ('site_sign_in_sheets', 'site_id'), ('site_roster', 'site_id'),
        ('site_appointments', 'site_id'), ('site_work', 'site_id'), ('site_work_versions', 'site_id'),
        ('site_locations', 'site_id'),
        ('workers', 'org_id'), ('worker_competencies', 'org_id'), ('worker_medicals', 'org_id'),
        ('documents', 'org_id'), ('document_versions', 'org_id'),
        ('notifications', 'user_id')
    ) AS v (name, lead) LOOP
        EXECUTE format('ALTER TABLE %I ADD COLUMN sync_xid xid8 NOT NULL DEFAULT pg_current_xact_id()', t.name);
        EXECUTE format('CREATE TRIGGER zz_sync_stamp BEFORE INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION sync_stamp()', t.name);
        EXECUTE format('CREATE INDEX %I ON %I (%I, sync_xid)', t.name || '_sync_idx', t.name, t.lead);
    END LOOP;
END
$$;
CREATE INDEX sync_deletions_sync_idx ON sync_deletions (site_id, sync_xid);
CREATE TRIGGER zz_sync_stamp BEFORE INSERT OR UPDATE ON sync_deletions FOR EACH ROW EXECUTE FUNCTION sync_stamp();

-- The one table the app deletes from: an answer's photos are replaced.
CREATE TRIGGER zz_sync_deleted AFTER DELETE ON inspection_response_evidence
    FOR EACH ROW EXECUTE FUNCTION sync_deleted('inspection', 'inspection_id');

-- SYNC D2: the server epoch. A cursor from another epoch is a reset.
--   system_identifier changes with a new cluster (logical restore, a
--   snapshot restored as a new instance); timeline_id after point-in-time
--   recovery or a promotion; the generation when the restore runbook says
--   so (a restore into the same cluster changes neither).
CREATE TABLE sync_generation (
    one        boolean PRIMARY KEY DEFAULT true CHECK (one),
    generation uuid NOT NULL DEFAULT gen_random_uuid(),
    changed_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO sync_generation DEFAULT VALUES;
REVOKE ALL ON sync_generation FROM siteproof_app;

CREATE FUNCTION sync_epoch() RETURNS text
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, public
    AS $$
    SELECT encode(sha256(convert_to(
        (SELECT system_identifier::text FROM pg_control_system()) || ':' ||
        (SELECT timeline_id::text FROM pg_control_checkpoint()) || ':' ||
        (SELECT generation::text FROM sync_generation), 'UTF8')), 'hex')
    $$;
REVOKE ALL ON FUNCTION sync_epoch() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION sync_epoch() TO siteproof_app;

-- Run by an operator after a restore (G38's runbook); never by the app.
CREATE FUNCTION sync_new_generation() RETURNS void
    LANGUAGE sql
    AS $$ UPDATE sync_generation SET generation = gen_random_uuid(), changed_at = now() $$;
REVOKE ALL ON FUNCTION sync_new_generation() FROM PUBLIC;

COMMIT;
