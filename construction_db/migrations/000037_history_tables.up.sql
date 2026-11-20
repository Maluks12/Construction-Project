BEGIN;

-- M12 D10: an audit pack is rebuilt as of the time its export was queued,
-- so every column the pack reads that is edited in place needs its past
-- values. worker_history (000032) covers workers; these cover the rest:
--   - organisation_history: names, registration and VAT numbers, insurer;
--   - site_history: name, address, location, status, regime;
--   - member_history: each membership's role and status, with the user's
--     name, one row per membership (users has no org_id). ONBOARD's team
--     management adds its "why" here later;
--   - toolbox_talk_history: a talk's header, editable until its first
--     signature.
--
-- Rules for all four:
--   - written only by triggers on the source tables (SECURITY DEFINER, so a
--     name change reaches every organisation the user belongs to, whatever
--     the tenant); the app may only read them;
--   - append-only for every role: no UPDATE, DELETE or TRUNCATE;
--   - changed_at is clock_timestamp(): the as-of cut-off compares it with
--     the export's as_of. A build waits 2 minutes after as_of, so a
--     transaction stamped before it has committed by then;
--   - existing rows are backfilled with today's values from their creation
--     time: changes made before this migration are not recoverable;
--   - no foreign keys to the source rows, so history outlives them.

CREATE FUNCTION history_append_only() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        RAISE EXCEPTION '% is append-only', TG_TABLE_NAME USING ERRCODE = 'check_violation';
    END
    $$;

CREATE TABLE organisation_history (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id          uuid NOT NULL,
    trading_name    text NOT NULL,
    registered_name text,
    reg_no          text,
    vat_no          text,
    coida_insurer   text,
    archived_at     timestamptz,
    changed_at      timestamptz NOT NULL,
    changed_by      uuid
);
CREATE INDEX organisation_history_idx ON organisation_history (org_id, changed_at DESC, id DESC);

CREATE TABLE site_history (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id      uuid NOT NULL,
    site_id     uuid NOT NULL,
    regime_id   uuid NOT NULL,
    name        text NOT NULL,
    address     text,
    lat         double precision,
    lng         double precision,
    status      text NOT NULL,
    archived_at timestamptz,
    changed_at  timestamptz NOT NULL,
    changed_by  uuid
);
CREATE INDEX site_history_idx ON site_history (site_id, changed_at DESC, id DESC);

CREATE TABLE member_history (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id     uuid NOT NULL,
    user_id    uuid NOT NULL,
    role       text NOT NULL,
    status     text NOT NULL,
    full_name  text NOT NULL,
    changed_at timestamptz NOT NULL,
    changed_by uuid
);
CREATE INDEX member_history_idx ON member_history (org_id, user_id, changed_at DESC, id DESC);

CREATE TABLE toolbox_talk_history (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id         uuid NOT NULL,
    site_id        uuid NOT NULL,
    talk_id        uuid NOT NULL,
    topic_key      text,
    topic          text NOT NULL,
    presenter_name text NOT NULL,
    notes          text,
    started_at     timestamptz NOT NULL,
    ended_at       timestamptz,
    changed_at     timestamptz NOT NULL,
    changed_by     uuid
);
CREATE INDEX toolbox_talk_history_idx ON toolbox_talk_history (talk_id, changed_at DESC, id DESC);

-- The writers. Each skips an UPDATE that changes none of its columns.

CREATE FUNCTION organisations_history() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public
    AS $$
    BEGIN
        IF TG_OP = 'UPDATE' AND (NEW.trading_name, NEW.registered_name, NEW.reg_no, NEW.vat_no, NEW.coida_insurer, NEW.archived_at)
                                IS NOT DISTINCT FROM (OLD.trading_name, OLD.registered_name, OLD.reg_no, OLD.vat_no, OLD.coida_insurer, OLD.archived_at) THEN
            RETURN NULL;
        END IF;
        INSERT INTO organisation_history (org_id, trading_name, registered_name, reg_no, vat_no, coida_insurer, archived_at, changed_at, changed_by)
        VALUES (NEW.id, NEW.trading_name, NEW.registered_name, NEW.reg_no, NEW.vat_no, NEW.coida_insurer, NEW.archived_at,
                clock_timestamp(), app_user_id());
        RETURN NULL;
    END
    $$;
CREATE TRIGGER organisations_history AFTER INSERT OR UPDATE ON organisations
    FOR EACH ROW EXECUTE FUNCTION organisations_history();

CREATE FUNCTION sites_history() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public
    AS $$
    BEGIN
        IF TG_OP = 'UPDATE' AND (NEW.regime_id, NEW.name, NEW.address, NEW.lat, NEW.lng, NEW.status, NEW.archived_at)
                                IS NOT DISTINCT FROM (OLD.regime_id, OLD.name, OLD.address, OLD.lat, OLD.lng, OLD.status, OLD.archived_at) THEN
            RETURN NULL;
        END IF;
        INSERT INTO site_history (org_id, site_id, regime_id, name, address, lat, lng, status, archived_at, changed_at, changed_by)
        VALUES (NEW.org_id, NEW.id, NEW.regime_id, NEW.name, NEW.address, NEW.lat, NEW.lng, NEW.status, NEW.archived_at,
                clock_timestamp(), app_user_id());
        RETURN NULL;
    END
    $$;
CREATE TRIGGER sites_history AFTER INSERT OR UPDATE ON sites
    FOR EACH ROW EXECUTE FUNCTION sites_history();

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

-- A name change is one new row for every membership the user has, in every
-- organisation: the definer reaches them all, whatever the tenant.
CREATE FUNCTION users_member_history() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public
    AS $$
    BEGIN
        IF NEW.full_name IS NOT DISTINCT FROM OLD.full_name THEN
            RETURN NULL;
        END IF;
        INSERT INTO member_history (org_id, user_id, role, status, full_name, changed_at, changed_by)
        SELECT m.org_id, m.user_id, m.role, m.status, NEW.full_name, clock_timestamp(), app_user_id()
        FROM org_members m WHERE m.user_id = NEW.id;
        RETURN NULL;
    END
    $$;
CREATE TRIGGER users_member_history AFTER UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION users_member_history();

CREATE FUNCTION toolbox_talks_history() RETURNS trigger
    LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public
    AS $$
    BEGIN
        IF TG_OP = 'UPDATE' AND (NEW.topic_key, NEW.topic, NEW.presenter_name, NEW.notes, NEW.started_at, NEW.ended_at)
                                IS NOT DISTINCT FROM (OLD.topic_key, OLD.topic, OLD.presenter_name, OLD.notes, OLD.started_at, OLD.ended_at) THEN
            RETURN NULL;
        END IF;
        INSERT INTO toolbox_talk_history (org_id, site_id, talk_id, topic_key, topic, presenter_name, notes, started_at, ended_at, changed_at, changed_by)
        VALUES (NEW.org_id, NEW.site_id, NEW.id, NEW.topic_key, NEW.topic, NEW.presenter_name, NEW.notes, NEW.started_at, NEW.ended_at,
                clock_timestamp(), app_user_id());
        RETURN NULL;
    END
    $$;
CREATE TRIGGER toolbox_talks_history AFTER INSERT OR UPDATE ON toolbox_talks
    FOR EACH ROW EXECUTE FUNCTION toolbox_talks_history();

-- Backfill: today's values, from each row's creation.
INSERT INTO organisation_history (org_id, trading_name, registered_name, reg_no, vat_no, coida_insurer, archived_at, changed_at)
SELECT id, trading_name, registered_name, reg_no, vat_no, coida_insurer, archived_at, created_at FROM organisations;
INSERT INTO site_history (org_id, site_id, regime_id, name, address, lat, lng, status, archived_at, changed_at)
SELECT org_id, id, regime_id, name, address, lat, lng, status, archived_at, created_at FROM sites;
INSERT INTO member_history (org_id, user_id, role, status, full_name, changed_at)
SELECT m.org_id, m.user_id, m.role, m.status, u.full_name, m.created_at FROM org_members m JOIN users u ON u.id = m.user_id;
INSERT INTO toolbox_talk_history (org_id, site_id, talk_id, topic_key, topic, presenter_name, notes, started_at, ended_at, changed_at)
SELECT org_id, site_id, id, topic_key, topic, presenter_name, notes, started_at, ended_at, coalesce(updated_at, created_at) FROM toolbox_talks;

DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['organisation_history', 'site_history', 'member_history', 'toolbox_talk_history'] LOOP
        EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION history_append_only()', t || '_append_only', t);
        EXECUTE format('CREATE TRIGGER %I BEFORE TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION history_append_only()', t || '_no_truncate', t);
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format('CREATE POLICY tenant ON %I FOR ALL TO siteproof_app USING (org_id = app_org_id()) WITH CHECK (org_id = app_org_id())', t);
        EXECUTE format('REVOKE INSERT, UPDATE, DELETE, TRUNCATE ON %I FROM siteproof_app', t);
    END LOOP;
END
$$;

COMMIT;
