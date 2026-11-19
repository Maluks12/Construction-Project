BEGIN;

-- Document files (M11 D4): certificates, appointment letters, H&S file and
-- vault documents. The same seal as evidence (000010), without the chain:
-- the device chooses the id, uploads straight to S3 with a URL that signs the
-- hash, length and type, and the server re-hashes the exact stored version.
-- A file starts 'pending' and becomes 'verified' or 'rejected', then never
-- changes (retain_until may only be extended).
--
-- purpose fixes what a file may be attached to: records reference
-- (id, purpose, status) with constant columns, so a medical certificate can
-- only ever be a medical record's, and the view rule for 'medical' (owner
-- and SHE officer only, D6) holds wherever it is attached. Site purposes
-- carry the site.
CREATE TABLE files (
    id                uuid PRIMARY KEY,
    org_id            uuid NOT NULL,
    site_id           uuid,
    purpose           text NOT NULL CHECK (purpose IN ('competency', 'medical', 'appointment', 'site_document', 'org_document')),
    uploaded_by       uuid NOT NULL,
    content_sha256    bytea NOT NULL CHECK (length(content_sha256) = 32),
    size_bytes        bigint NOT NULL CHECK (size_bytes BETWEEN 1 AND 20971520),
    content_type      text NOT NULL CHECK (content_type IN ('application/pdf', 'image/jpeg', 'image/png')),
    file_name         text CHECK (char_length(btrim(file_name)) BETWEEN 1 AND 200),
    device_id         text CHECK (octet_length(device_id) BETWEEN 1 AND 200),
    object_key        text NOT NULL UNIQUE,
    object_version_id text,
    retain_until      timestamptz,
    status            text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'verified', 'rejected')),
    rejected_reason   text,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    FOREIGN KEY (org_id, site_id) REFERENCES sites (org_id, id),
    FOREIGN KEY (org_id, uploaded_by) REFERENCES org_members (org_id, user_id),
    UNIQUE (org_id, id),
    UNIQUE (org_id, id, purpose, status),
    UNIQUE (org_id, site_id, id, purpose, status),

    CONSTRAINT files_site_purpose CHECK ((purpose IN ('appointment', 'site_document')) = (site_id IS NOT NULL)),
    CONSTRAINT files_rejected_reason CHECK ((status = 'rejected') = (rejected_reason IS NOT NULL)),
    CONSTRAINT files_verified_version CHECK (status <> 'verified' OR object_version_id IS NOT NULL)
);

CREATE INDEX files_org_idx ON files (org_id, created_at);

-- As evidence_freeze: only pending files change, for every role; a
-- verified or rejected file may only have its retention extended.
CREATE FUNCTION files_freeze() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        rest files;
    BEGIN
        IF TG_OP = 'DELETE' THEN
            RAISE EXCEPTION 'file %: files are never deleted', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF OLD.status = 'pending' THEN
            IF (NEW.id, NEW.org_id, NEW.site_id, NEW.purpose, NEW.uploaded_by, NEW.content_sha256, NEW.size_bytes,
                NEW.content_type, NEW.file_name, NEW.device_id, NEW.object_key, NEW.created_at)
               IS DISTINCT FROM (OLD.id, OLD.org_id, OLD.site_id, OLD.purpose, OLD.uploaded_by, OLD.content_sha256, OLD.size_bytes,
                OLD.content_type, OLD.file_name, OLD.device_id, OLD.object_key, OLD.created_at) THEN
                RAISE EXCEPTION 'file %: what was declared never changes', OLD.id
                    USING ERRCODE = 'check_violation';
            END IF;
            RETURN NEW;
        END IF;
        IF NEW.retain_until IS DISTINCT FROM OLD.retain_until
           AND (NEW.retain_until IS NULL OR NEW.retain_until < OLD.retain_until) THEN
            RAISE EXCEPTION 'file %: retention can only be extended', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        rest := NEW;
        rest.retain_until := OLD.retain_until;
        rest.updated_at := OLD.updated_at;
        IF rest IS DISTINCT FROM OLD THEN
            RAISE EXCEPTION 'file % is %; only pending files can change', OLD.id, OLD.status
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER files_freeze BEFORE UPDATE OR DELETE ON files
    FOR EACH ROW EXECUTE FUNCTION files_freeze();

-- Who opened which file, when and from where. Append-only for every role.
CREATE TABLE file_access_log (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id            uuid NOT NULL,
    file_id           uuid NOT NULL,
    user_id           uuid NOT NULL,
    action            text NOT NULL CHECK (action IN ('view_bytes')),
    object_version_id text NOT NULL,
    ip                inet,
    user_agent        text CHECK (octet_length(user_agent) <= 512),
    accessed_at       timestamptz NOT NULL DEFAULT clock_timestamp(),

    FOREIGN KEY (org_id, file_id) REFERENCES files (org_id, id),
    FOREIGN KEY (org_id, user_id) REFERENCES org_members (org_id, user_id)
);

CREATE INDEX file_access_log_file_idx ON file_access_log (file_id, accessed_at);

CREATE FUNCTION files_append_only() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        RAISE EXCEPTION '% is append-only', TG_TABLE_NAME
            USING ERRCODE = 'check_violation';
    END
    $$;

CREATE TRIGGER file_access_log_append_only BEFORE UPDATE OR DELETE ON file_access_log
    FOR EACH ROW EXECUTE FUNCTION files_append_only();
CREATE TRIGGER file_access_log_no_truncate BEFORE TRUNCATE ON file_access_log
    FOR EACH STATEMENT EXECUTE FUNCTION files_append_only();
CREATE TRIGGER files_no_truncate BEFORE TRUNCATE ON files
    FOR EACH STATEMENT EXECUTE FUNCTION files_append_only();

ALTER TABLE files ENABLE ROW LEVEL SECURITY;
ALTER TABLE files FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON files FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE file_access_log ENABLE ROW LEVEL SECURITY;
ALTER TABLE file_access_log FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON file_access_log FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

-- Migration 000008's default privileges grant UPDATE and DELETE on new
-- tables. The app may only record a file's outcome.
REVOKE UPDATE, DELETE ON files FROM siteproof_app;
GRANT UPDATE (status, rejected_reason, object_version_id, retain_until, updated_at) ON files TO siteproof_app;
REVOKE UPDATE, DELETE ON file_access_log FROM siteproof_app;

COMMIT;
