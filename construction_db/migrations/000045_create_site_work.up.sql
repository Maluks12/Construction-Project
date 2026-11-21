BEGIN;

-- Planned construction work on a site (REG4): its start date and the facts
-- that decide CR 2014 reg 3 and reg 4 (site_work.json). Never edited: a changed
-- start, fact set or status is a new version with a reason, as for incidents
-- (000016). Ids and version numbers come from the device (SYNC).
CREATE TABLE site_work (
    id             uuid PRIMARY KEY,
    org_id         uuid NOT NULL,
    site_id        uuid NOT NULL,
    regime_id      uuid NOT NULL REFERENCES regulatory_regimes (id),
    created_by     uuid NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    latest_version integer NOT NULL DEFAULT 0 CHECK (latest_version >= 0),

    FOREIGN KEY (org_id, site_id) REFERENCES sites (org_id, id),
    FOREIGN KEY (org_id, created_by) REFERENCES org_members (org_id, user_id),
    UNIQUE (org_id, site_id, id)
);

CREATE INDEX site_work_site_idx ON site_work (site_id, created_at);

-- Fact keys as regimes.keyPattern restricts them, distinct. Whether they exist
-- in the site's regime is checked by the API. Empty is allowed: work that
-- triggers nothing.
CREATE FUNCTION site_work_facts_valid(facts text[]) RETURNS boolean
    LANGUAGE sql IMMUTABLE
    AS $$
        SELECT coalesce((SELECT bool_and(f IS NOT NULL AND f ~ '^[a-z0-9_]+$') FROM unnest(facts) AS f), true)
           AND (SELECT count(DISTINCT f) FROM unnest(facts) AS f) = cardinality(facts)
    $$;

CREATE TABLE site_work_versions (
    org_id              uuid NOT NULL,
    site_id             uuid NOT NULL,
    site_work_id        uuid NOT NULL,
    version             integer NOT NULL CHECK (version >= 1),
    description         text NOT NULL CHECK (char_length(description) BETWEEN 1 AND 500),
    starts_on           date NOT NULL,
    ends_on             date,
    facts               text[] NOT NULL CHECK (site_work_facts_valid(facts)),
    status              text NOT NULL CHECK (status IN ('planned', 'cancelled')),
    submitted_by        uuid NOT NULL,
    client_submitted_at timestamptz NOT NULL,
    submitted_at        timestamptz NOT NULL DEFAULT now(),
    device_id           text CHECK (octet_length(device_id) BETWEEN 1 AND 200),
    amend_reason        text CHECK (char_length(amend_reason) BETWEEN 1 AND 500),

    PRIMARY KEY (site_work_id, version),
    FOREIGN KEY (org_id, site_id, site_work_id) REFERENCES site_work (org_id, site_id, id),
    FOREIGN KEY (org_id, submitted_by) REFERENCES org_members (org_id, user_id),
    UNIQUE (org_id, site_id, site_work_id, version),

    CONSTRAINT site_work_versions_amend_reason CHECK ((version = 1) = (amend_reason IS NULL)),
    CONSTRAINT site_work_versions_first_planned CHECK (version > 1 OR status = 'planned'),
    CONSTRAINT site_work_versions_ends_after_start CHECK (ends_on >= starts_on)
);

-- Every guard below applies to every role, the table owner included.

CREATE FUNCTION site_work_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        rest site_work;
    BEGIN
        IF TG_OP = 'DELETE' THEN
            RAISE EXCEPTION 'site work %: site work is never deleted', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW IS NOT DISTINCT FROM OLD THEN
            RETURN NEW;
        END IF;
        rest := NEW;
        rest.latest_version := OLD.latest_version;
        IF rest IS DISTINCT FROM OLD
           OR NEW.latest_version <> OLD.latest_version + 1
           OR NOT EXISTS (SELECT 1 FROM site_work_versions WHERE site_work_id = OLD.id AND version = NEW.latest_version) THEN
            RAISE EXCEPTION 'site work %: site work changes only by a new version', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER site_work_guard BEFORE UPDATE OR DELETE ON site_work
    FOR EACH ROW EXECUTE FUNCTION site_work_guard();

CREATE FUNCTION site_work_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        NEW.created_at := now();
        IF NEW.latest_version <> 0 THEN
            RAISE EXCEPTION 'site work %: new site work has no versions yet; submit version 1', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER site_work_insert BEFORE INSERT ON site_work
    FOR EACH ROW EXECUTE FUNCTION site_work_insert();

CREATE FUNCTION site_work_has_version() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF NOT EXISTS (SELECT 1 FROM site_work_versions WHERE site_work_id = NEW.id) THEN
            RAISE EXCEPTION 'site work %: has no version; site work is created by submitting version 1', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

CREATE CONSTRAINT TRIGGER site_work_has_version AFTER INSERT ON site_work
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION site_work_has_version();

-- submitted_at is the server's. A version can't claim to be from the future,
-- nor from after its site was archived (as incidents, 000016). starts_on must
-- be inside the holiday calendar (REG4 D4): a recompute may need its days.
CREATE FUNCTION site_work_versions_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        s            sites;
        jurisdiction char(2);
    BEGIN
        NEW.submitted_at := now();
        IF NEW.client_submitted_at > NEW.submitted_at + interval '5 minutes' THEN
            RAISE EXCEPTION 'site work % version %: client_submitted_at is in the future', NEW.site_work_id, NEW.version
                USING ERRCODE = 'check_violation';
        END IF;
        SELECT * INTO s FROM sites WHERE id = NEW.site_id;
        IF FOUND AND s.status = 'archived' AND NEW.client_submitted_at > s.archived_at THEN
            RAISE EXCEPTION 'site work % version %: submitted after its site was archived', NEW.site_work_id, NEW.version
                USING ERRCODE = 'check_violation';
        END IF;
        SELECT r.jurisdiction INTO jurisdiction FROM site_work w JOIN regulatory_regimes r ON r.id = w.regime_id
        WHERE w.id = NEW.site_work_id;
        IF FOUND THEN
            PERFORM holiday_calendar_check(jurisdiction, NEW.starts_on);
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER site_work_versions_insert BEFORE INSERT ON site_work_versions
    FOR EACH ROW EXECUTE FUNCTION site_work_versions_insert();

-- As incident_versions_advance: the UPDATE takes the header's row lock, so
-- submissions of one record (and their deadline writes) are serialised.
CREATE FUNCTION site_work_versions_advance() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        UPDATE site_work SET latest_version = NEW.version
        WHERE id = NEW.site_work_id AND latest_version = NEW.version - 1;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'site work %: version % is not the next version', NEW.site_work_id, NEW.version
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

CREATE TRIGGER site_work_versions_advance AFTER INSERT ON site_work_versions
    FOR EACH ROW EXECUTE FUNCTION site_work_versions_advance();

CREATE FUNCTION site_work_append_only() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        RAISE EXCEPTION '% is append-only; submit a new version instead', TG_TABLE_NAME
            USING ERRCODE = 'check_violation';
    END
    $$;

CREATE TRIGGER site_work_versions_append_only BEFORE UPDATE OR DELETE ON site_work_versions
    FOR EACH ROW EXECUTE FUNCTION site_work_append_only();
CREATE TRIGGER site_work_no_truncate BEFORE TRUNCATE ON site_work
    FOR EACH STATEMENT EXECUTE FUNCTION site_work_append_only();
CREATE TRIGGER site_work_versions_no_truncate BEFORE TRUNCATE ON site_work_versions
    FOR EACH STATEMENT EXECUTE FUNCTION site_work_append_only();

ALTER TABLE site_work ENABLE ROW LEVEL SECURITY;
ALTER TABLE site_work FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON site_work FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE site_work_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE site_work_versions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON site_work_versions FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

REVOKE UPDATE, DELETE ON site_work FROM siteproof_app;
GRANT UPDATE (latest_version) ON site_work TO siteproof_app;
REVOKE UPDATE, DELETE ON site_work_versions FROM siteproof_app;

COMMIT;
