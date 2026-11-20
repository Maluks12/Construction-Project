BEGIN;

-- M12: audit pack exports (D5-D10).
--   - audit_exports: the request, append-only. as_of is the server's time
--     at insert; the pack holds what the server had recorded by then.
--   - audit_export_attempts: one row per build attempt, append-only except
--     its lease (moved forward while it builds) and its finish (set once).
--     A build may start only 2 minutes after as_of (the settle window), only
--     with no live lease on the export, and only after fewer than 3 failed
--     or lapsed attempts.
--   - audit_export_results: the finished pack's SHA-256, size and object,
--     one per export, accepted only from the export's newest attempt while
--     its lease is live (fencing: a worker whose lease lapsed and was
--     re-claimed can't record its pack). The attempt is finished in the
--     same statement.
--   - audit_export_evidence and audit_export_files: what the pack holds,
--     written in the result's transaction and checked at commit, so a
--     result is never half-recorded.
--   - audit_export_downloads: who downloaded a pack, when, from where.
--   - evidence_access_log and file_access_log gain export_id: a download
--     writes an 'export' row per photo and file in the pack.

CREATE TABLE audit_exports (
    id                  uuid PRIMARY KEY,
    org_id              uuid NOT NULL,
    site_id             uuid NOT NULL,
    requested_by        uuid NOT NULL,
    requester_role      text NOT NULL CHECK (requester_role IN ('owner', 'she_officer')),
    from_date           date NOT NULL,
    to_date             date NOT NULL,
    time_zone           text NOT NULL,
    as_of               timestamptz NOT NULL,
    pack_format         integer NOT NULL CHECK (pack_format > 0),
    regime_code         text NOT NULL,
    regime_version      text NOT NULL,
    regime_data_sha256  bytea NOT NULL CHECK (octet_length(regime_data_sha256) = 32),
    includes_health     boolean NOT NULL,
    client_requested_at timestamptz NOT NULL,
    device_id           text CHECK (octet_length(device_id) BETWEEN 1 AND 200),

    FOREIGN KEY (org_id, site_id) REFERENCES sites (org_id, id),
    FOREIGN KEY (org_id, requested_by) REFERENCES org_members (org_id, user_id),
    UNIQUE (org_id, id),
    CONSTRAINT audit_exports_range CHECK (to_date >= from_date AND to_date - from_date < 366),
    -- Health details only for the roles allowed them (D8).
    CONSTRAINT audit_exports_health CHECK (NOT includes_health OR requester_role IN ('owner', 'she_officer'))
);
CREATE INDEX audit_exports_site_idx ON audit_exports (site_id, as_of DESC);

CREATE FUNCTION audit_exports_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        NEW.as_of := clock_timestamp();
        RETURN NEW;
    END
    $$;
CREATE TRIGGER audit_exports_insert BEFORE INSERT ON audit_exports
    FOR EACH ROW EXECUTE FUNCTION audit_exports_insert();

-- The requester's role is their live membership's, not the client's word.
-- Checked AFTER the insert: RLS's WITH CHECK runs after BEFORE triggers, and
-- another organisation's members are invisible, so a BEFORE check would
-- answer a cross-tenant row with the wrong error.
CREATE FUNCTION audit_exports_requester() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        m org_members;
    BEGIN
        SELECT * INTO m FROM org_members WHERE org_id = NEW.org_id AND user_id = NEW.requested_by;
        IF m.status IS DISTINCT FROM 'active' OR m.role IS DISTINCT FROM NEW.requester_role THEN
            RAISE EXCEPTION 'audit export %: requester_role must be the requester''s active role', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;
CREATE TRIGGER audit_exports_requester AFTER INSERT ON audit_exports
    FOR EACH ROW EXECUTE FUNCTION audit_exports_requester();

-- The settle window: how long after as_of a build may start. 2 minutes
-- unless the claiming transaction says otherwise (tests use a shorter one).
CREATE FUNCTION audit_export_settle() RETURNS interval
    LANGUAGE sql STABLE
    AS $$ SELECT coalesce(nullif(current_setting('siteproof.export_settle', true), ''), '2 minutes')::interval $$;

CREATE TABLE audit_export_attempts (
    id          uuid PRIMARY KEY,
    org_id      uuid NOT NULL,
    export_id   uuid NOT NULL,
    attempt     integer NOT NULL CHECK (attempt BETWEEN 1 AND 3),
    instance    text NOT NULL CHECK (char_length(instance) BETWEEN 1 AND 200),
    claimed_at  timestamptz NOT NULL,
    lease_until timestamptz NOT NULL,
    finished_at timestamptz,
    outcome     text CHECK (outcome IN ('ready', 'failed', 'lapsed')),
    error       text CHECK (char_length(error) <= 2000),

    FOREIGN KEY (org_id, export_id) REFERENCES audit_exports (org_id, id),
    UNIQUE (export_id, attempt),
    UNIQUE (org_id, export_id, id),
    CONSTRAINT audit_export_attempts_finish CHECK ((finished_at IS NULL) = (outcome IS NULL)),
    CONSTRAINT audit_export_attempts_error CHECK (error IS NULL OR outcome IN ('failed', 'lapsed'))
);

CREATE TABLE audit_export_results (
    export_id         uuid PRIMARY KEY,
    org_id            uuid NOT NULL,
    attempt_id        uuid NOT NULL,
    sha256            bytea NOT NULL CHECK (octet_length(sha256) = 32),
    size_bytes        bigint NOT NULL CHECK (size_bytes > 0),
    object_key        text NOT NULL,
    object_version_id text NOT NULL,
    evidence_count    integer NOT NULL CHECK (evidence_count >= 0),
    file_count        integer NOT NULL CHECK (file_count >= 0),
    chain_length      bigint NOT NULL CHECK (chain_length >= 0),
    chain_head        bytea CHECK (octet_length(chain_head) = 32),
    chain_intact      boolean NOT NULL,
    finished_at       timestamptz NOT NULL,
    expires_at        timestamptz NOT NULL,

    FOREIGN KEY (org_id, export_id, attempt_id) REFERENCES audit_export_attempts (org_id, export_id, id),
    UNIQUE (org_id, export_id)
);

CREATE TABLE audit_export_evidence (
    org_id      uuid NOT NULL,
    export_id   uuid NOT NULL,
    site_id     uuid NOT NULL,
    evidence_id uuid NOT NULL,
    PRIMARY KEY (export_id, evidence_id),
    FOREIGN KEY (org_id, export_id) REFERENCES audit_export_results (org_id, export_id),
    FOREIGN KEY (org_id, site_id, evidence_id) REFERENCES evidence (org_id, site_id, id)
);

CREATE TABLE audit_export_files (
    org_id    uuid NOT NULL,
    export_id uuid NOT NULL,
    file_id   uuid NOT NULL,
    PRIMARY KEY (export_id, file_id),
    FOREIGN KEY (org_id, export_id) REFERENCES audit_export_results (org_id, export_id),
    FOREIGN KEY (org_id, file_id) REFERENCES files (org_id, id)
);

CREATE TABLE audit_export_downloads (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id        uuid NOT NULL,
    export_id     uuid NOT NULL,
    user_id       uuid NOT NULL,
    ip            inet,
    user_agent    text CHECK (octet_length(user_agent) <= 512),
    downloaded_at timestamptz NOT NULL DEFAULT clock_timestamp(),

    FOREIGN KEY (org_id, export_id) REFERENCES audit_export_results (org_id, export_id),
    FOREIGN KEY (org_id, user_id) REFERENCES org_members (org_id, user_id)
);

-- Attempts. Every check runs under the export row's lock, so two claims, or
-- a claim and a result, take turns.
CREATE FUNCTION audit_export_attempts_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        e     audit_exports;
        prior integer;
    BEGIN
        IF TG_OP = 'DELETE' THEN
            RAISE EXCEPTION 'audit export attempts are append-only' USING ERRCODE = 'check_violation';
        END IF;
        SELECT * INTO e FROM audit_exports WHERE id = NEW.export_id FOR UPDATE;

        IF TG_OP = 'INSERT' THEN
            NEW.claimed_at := clock_timestamp();
            NEW.finished_at := NULL;
            NEW.outcome := NULL;
            NEW.error := NULL;
            IF EXISTS (SELECT 1 FROM audit_export_results r WHERE r.export_id = NEW.export_id) THEN
                RAISE EXCEPTION 'audit export %: already built', NEW.export_id USING ERRCODE = 'check_violation';
            END IF;
            IF e.as_of > NEW.claimed_at - audit_export_settle() THEN
                RAISE EXCEPTION 'audit export %: not settled yet', NEW.export_id USING ERRCODE = 'check_violation';
            END IF;
            IF EXISTS (SELECT 1 FROM audit_export_attempts a
                       WHERE a.export_id = NEW.export_id AND a.finished_at IS NULL AND a.lease_until > NEW.claimed_at) THEN
                RAISE EXCEPTION 'audit export %: another attempt holds the lease', NEW.export_id USING ERRCODE = 'check_violation';
            END IF;
            IF EXISTS (SELECT 1 FROM audit_export_attempts a WHERE a.export_id = NEW.export_id AND a.finished_at IS NULL) THEN
                RAISE EXCEPTION 'audit export %: mark the lapsed attempt first', NEW.export_id USING ERRCODE = 'check_violation';
            END IF;
            SELECT count(*) INTO prior FROM audit_export_attempts a WHERE a.export_id = NEW.export_id;
            IF NEW.attempt <> prior + 1 THEN
                RAISE EXCEPTION 'audit export %: attempt must be %', NEW.export_id, prior + 1 USING ERRCODE = 'check_violation';
            END IF;
            IF NEW.lease_until <= NEW.claimed_at THEN
                RAISE EXCEPTION 'audit export %: a lease must end after the claim', NEW.export_id USING ERRCODE = 'check_violation';
            END IF;
            RETURN NEW;
        END IF;

        -- UPDATE: nothing but the lease (forward, while live and unfinished)
        -- or the finish (once).
        IF (NEW.id, NEW.org_id, NEW.export_id, NEW.attempt, NEW.instance, NEW.claimed_at)
           IS DISTINCT FROM (OLD.id, OLD.org_id, OLD.export_id, OLD.attempt, OLD.instance, OLD.claimed_at) THEN
            RAISE EXCEPTION 'audit export attempt %: only its lease and finish change', OLD.id USING ERRCODE = 'check_violation';
        END IF;
        IF OLD.finished_at IS NOT NULL THEN
            RAISE EXCEPTION 'audit export attempt %: already finished', OLD.id USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.finished_at IS NULL THEN
            IF NEW.lease_until <= OLD.lease_until OR OLD.lease_until <= clock_timestamp() THEN
                RAISE EXCEPTION 'audit export attempt %: a lease moves forward, and only while it is live', OLD.id
                    USING ERRCODE = 'check_violation';
            END IF;
            RETURN NEW;
        END IF;
        NEW.lease_until := OLD.lease_until;
        NEW.finished_at := clock_timestamp();
        IF NEW.outcome = 'lapsed' AND OLD.lease_until > NEW.finished_at THEN
            RAISE EXCEPTION 'audit export attempt %: its lease is still live', OLD.id USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.outcome = 'ready' AND NOT EXISTS (SELECT 1 FROM audit_export_results r WHERE r.attempt_id = OLD.id) THEN
            RAISE EXCEPTION 'audit export attempt %: ready only with its result', OLD.id USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;
CREATE TRIGGER audit_export_attempts_guard BEFORE INSERT OR UPDATE OR DELETE ON audit_export_attempts
    FOR EACH ROW EXECUTE FUNCTION audit_export_attempts_guard();

-- Results: only from the export's newest attempt, unfinished, lease live.
-- Recording it finishes the attempt as ready.
CREATE FUNCTION audit_export_results_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        a audit_export_attempts;
    BEGIN
        IF TG_OP <> 'INSERT' THEN
            RAISE EXCEPTION 'audit export results are append-only' USING ERRCODE = 'check_violation';
        END IF;
        PERFORM 1 FROM audit_exports WHERE id = NEW.export_id FOR UPDATE;
        SELECT * INTO a FROM audit_export_attempts WHERE id = NEW.attempt_id;
        NEW.finished_at := clock_timestamp();
        NEW.expires_at := NEW.finished_at + interval '7 days';
        IF a.finished_at IS NOT NULL OR a.lease_until <= NEW.finished_at
           OR EXISTS (SELECT 1 FROM audit_export_attempts x WHERE x.export_id = NEW.export_id AND x.attempt > a.attempt) THEN
            RAISE EXCEPTION 'audit export %: attempt % no longer holds the lease', NEW.export_id, a.attempt
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;
CREATE TRIGGER audit_export_results_guard BEFORE INSERT OR UPDATE OR DELETE ON audit_export_results
    FOR EACH ROW EXECUTE FUNCTION audit_export_results_guard();

CREATE FUNCTION audit_export_results_finish() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        UPDATE audit_export_attempts SET outcome = 'ready', finished_at = clock_timestamp() WHERE id = NEW.attempt_id;
        RETURN NULL;
    END
    $$;
CREATE TRIGGER audit_export_results_finish AFTER INSERT ON audit_export_results
    FOR EACH ROW EXECUTE FUNCTION audit_export_results_finish();

-- At commit, the pack's contents are exactly its counts; afterwards the
-- counts are reached, so nothing more can be added.
CREATE FUNCTION audit_export_contents_check() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        r audit_export_results;
    BEGIN
        SELECT * INTO r FROM audit_export_results WHERE export_id = NEW.export_id;
        IF (SELECT count(*) FROM audit_export_evidence WHERE export_id = NEW.export_id) <> r.evidence_count
           OR (SELECT count(*) FROM audit_export_files WHERE export_id = NEW.export_id) <> r.file_count THEN
            RAISE EXCEPTION 'audit export %: contents do not match the result''s counts', NEW.export_id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;
CREATE CONSTRAINT TRIGGER audit_export_results_contents AFTER INSERT ON audit_export_results
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION audit_export_contents_check();
CREATE CONSTRAINT TRIGGER audit_export_evidence_contents AFTER INSERT ON audit_export_evidence
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION audit_export_contents_check();
CREATE CONSTRAINT TRIGGER audit_export_files_contents AFTER INSERT ON audit_export_files
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION audit_export_contents_check();

-- Append-only for every role.
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['audit_exports', 'audit_export_evidence', 'audit_export_files', 'audit_export_downloads'] LOOP
        EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION history_append_only()', t || '_append_only', t);
    END LOOP;
    FOREACH t IN ARRAY ARRAY['audit_exports', 'audit_export_attempts', 'audit_export_results', 'audit_export_evidence',
                             'audit_export_files', 'audit_export_downloads'] LOOP
        EXECUTE format('CREATE TRIGGER %I BEFORE TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION history_append_only()', t || '_no_truncate', t);
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format('CREATE POLICY tenant ON %I FOR ALL TO siteproof_app USING (org_id = app_org_id()) WITH CHECK (org_id = app_org_id())', t);
        EXECUTE format('REVOKE DELETE, TRUNCATE ON %I FROM siteproof_app', t);
    END LOOP;
END
$$;
REVOKE UPDATE ON audit_exports, audit_export_results, audit_export_evidence, audit_export_files, audit_export_downloads FROM siteproof_app;
REVOKE UPDATE ON audit_export_attempts FROM siteproof_app;
-- SELECT ... FOR UPDATE needs an UPDATE privilege: the claim and the guards
-- lock the export row. UPDATE of id only, which the append-only trigger
-- refuses anyway, for every role.
GRANT UPDATE (id) ON audit_exports TO siteproof_app;
GRANT UPDATE (lease_until, finished_at, outcome, error) ON audit_export_attempts TO siteproof_app;

-- The access logs name the export a download came from.
ALTER TABLE evidence_access_log ADD COLUMN export_id uuid;
ALTER TABLE evidence_access_log ADD CONSTRAINT evidence_access_log_export
    CHECK ((action = 'export') = (export_id IS NOT NULL));
ALTER TABLE evidence_access_log ADD CONSTRAINT evidence_access_log_export_fkey
    FOREIGN KEY (org_id, export_id) REFERENCES audit_export_results (org_id, export_id);

ALTER TABLE file_access_log DROP CONSTRAINT file_access_log_action_check;
ALTER TABLE file_access_log ADD CONSTRAINT file_access_log_action_check CHECK (action IN ('view_bytes', 'export'));
ALTER TABLE file_access_log ADD COLUMN export_id uuid;
ALTER TABLE file_access_log ADD CONSTRAINT file_access_log_export
    CHECK ((action = 'export') = (export_id IS NOT NULL));
ALTER TABLE file_access_log ADD CONSTRAINT file_access_log_export_fkey
    FOREIGN KEY (org_id, export_id) REFERENCES audit_export_results (org_id, export_id);

COMMIT;
