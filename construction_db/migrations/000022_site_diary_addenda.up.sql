BEGIN;

-- Addenda to a closed site diary (D7): dated, append-only, with who, when,
-- why and what. The original never changes; an addendum is listed after it.

CREATE TABLE site_diary_addenda (
    id                 uuid PRIMARY KEY,
    org_id             uuid NOT NULL,
    site_id            uuid NOT NULL,
    diary_date         date NOT NULL,
    -- Why it wasn't in the original.
    reason             text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 1 AND 500),
    note               text CHECK (char_length(btrim(note)) BETWEEN 1 AND 4000),
    -- Only what the addendum adds or corrects, in the diary's sections.
    sections           jsonb NOT NULL DEFAULT '{}' CHECK (site_diary_sections_ok(sections)),
    evidence_count     integer NOT NULL DEFAULT 0 CHECK (evidence_count BETWEEN 0 AND 50),
    added_by           uuid NOT NULL,
    client_added_at    timestamptz NOT NULL,
    server_received_at timestamptz NOT NULL DEFAULT now(),
    device_id          text CHECK (octet_length(device_id) BETWEEN 1 AND 200),

    FOREIGN KEY (org_id, site_id, diary_date) REFERENCES site_diaries (org_id, site_id, diary_date),
    FOREIGN KEY (org_id, added_by) REFERENCES org_members (org_id, user_id),
    UNIQUE (org_id, site_id, diary_date, id),
    -- An addendum adds something.
    CONSTRAINT site_diary_addenda_adds CHECK (note IS NOT NULL OR sections <> '{}' OR evidence_count > 0)
);

CREATE INDEX site_diary_addenda_diary_idx ON site_diary_addenda (site_id, diary_date, server_received_at);

CREATE TABLE site_diary_addendum_evidence (
    org_id          uuid NOT NULL,
    site_id         uuid NOT NULL,
    diary_date      date NOT NULL,
    addendum_id     uuid NOT NULL,
    evidence_id     uuid NOT NULL,
    evidence_status text NOT NULL DEFAULT 'verified' CHECK (evidence_status = 'verified'),

    PRIMARY KEY (addendum_id, evidence_id),
    FOREIGN KEY (org_id, site_id, diary_date, addendum_id) REFERENCES site_diary_addenda (org_id, site_id, diary_date, id),
    FOREIGN KEY (org_id, site_id, evidence_id, evidence_status) REFERENCES evidence (org_id, site_id, id, status)
);

CREATE INDEX site_diary_addendum_evidence_evidence_idx ON site_diary_addendum_evidence (evidence_id);

-- Only once the day has closed, by the server's clock: before that, the
-- diary itself is edited. (closes_at never changes, so no lock is needed.)
CREATE FUNCTION site_diary_addenda_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        d site_diaries;
    BEGIN
        NEW.server_received_at := now();
        IF NEW.client_added_at > NEW.server_received_at + interval '5 minutes' THEN
            RAISE EXCEPTION 'addendum %: client_added_at is in the future; check the device clock', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        SELECT * INTO d FROM site_diaries WHERE site_id = NEW.site_id AND diary_date = NEW.diary_date;
        IF NOT FOUND THEN
            RETURN NEW; -- the foreign key or the policy reports it
        END IF;
        IF NEW.server_received_at < d.closes_at THEN
            RAISE EXCEPTION 'addendum %: site diary % is still open; edit it instead', NEW.id, NEW.diary_date
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER site_diary_addenda_insert BEFORE INSERT ON site_diary_addenda
    FOR EACH ROW EXECUTE FUNCTION site_diary_addenda_insert();

CREATE FUNCTION site_diary_addenda_check() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF NOT action_site_member(NEW.org_id, NEW.site_id, NEW.added_by) THEN
            RAISE EXCEPTION 'addendum %: added_by can''t see its site', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

CREATE TRIGGER site_diary_addenda_check AFTER INSERT ON site_diary_addenda
    FOR EACH ROW EXECUTE FUNCTION site_diary_addenda_check();

-- At commit an addendum has exactly evidence_count photos.
CREATE FUNCTION site_diary_addendum_complete() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        target uuid;
        want   integer;
        photos integer;
    BEGIN
        IF TG_TABLE_NAME = 'site_diary_addenda' THEN
            target := NEW.id;
        ELSE
            target := NEW.addendum_id;
        END IF;
        SELECT evidence_count INTO want FROM site_diary_addenda a WHERE a.id = target;
        IF NOT FOUND THEN
            RETURN NULL; -- the foreign key reports it
        END IF;
        SELECT count(*) INTO photos FROM site_diary_addendum_evidence e WHERE e.addendum_id = target;
        IF photos <> want THEN
            RAISE EXCEPTION 'addendum %: has % photos but evidence_count is %', target, photos, want
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

CREATE CONSTRAINT TRIGGER site_diary_addenda_complete AFTER INSERT ON site_diary_addenda
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION site_diary_addendum_complete();
CREATE CONSTRAINT TRIGGER site_diary_addendum_evidence_complete AFTER INSERT ON site_diary_addendum_evidence
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION site_diary_addendum_complete();

CREATE TRIGGER site_diary_addenda_append_only BEFORE UPDATE OR DELETE ON site_diary_addenda
    FOR EACH ROW EXECUTE FUNCTION site_diary_append_only();
CREATE TRIGGER site_diary_addendum_evidence_append_only BEFORE UPDATE OR DELETE ON site_diary_addendum_evidence
    FOR EACH ROW EXECUTE FUNCTION site_diary_append_only();
CREATE TRIGGER site_diary_addenda_no_truncate BEFORE TRUNCATE ON site_diary_addenda
    FOR EACH STATEMENT EXECUTE FUNCTION site_diary_append_only();
CREATE TRIGGER site_diary_addendum_evidence_no_truncate BEFORE TRUNCATE ON site_diary_addendum_evidence
    FOR EACH STATEMENT EXECUTE FUNCTION site_diary_append_only();

ALTER TABLE site_diary_addenda ENABLE ROW LEVEL SECURITY;
ALTER TABLE site_diary_addenda FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON site_diary_addenda FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE site_diary_addendum_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE site_diary_addendum_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON site_diary_addendum_evidence FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

REVOKE UPDATE, DELETE ON site_diary_addenda FROM siteproof_app;
REVOKE UPDATE, DELETE ON site_diary_addendum_evidence FROM siteproof_app;

COMMIT;
