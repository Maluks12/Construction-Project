BEGIN;

-- Versioned documents (M11 D19, D20): the organisation's vault (scope
-- 'org') and each site's H&S file (scope 'site'). A document is a type and a
-- title; its versions are numbered from 1, append-only, each a verified file
-- of the document's own purpose with its own server-checked hash (D4). The
-- current version is the highest. Nothing is overwritten or deleted.
--
-- PC (Dec) will let a principal contractor read a linked subcontractor's
-- vault: rows are keyed by org_id, so that is one more RLS policy here,
-- not a reshape.

CREATE TABLE documents (
    id             uuid PRIMARY KEY,
    org_id         uuid NOT NULL,
    site_id        uuid,
    scope          text NOT NULL CHECK (scope IN ('org', 'site')),
    -- The regime the type was validated against: the record's own for the
    -- vault (D1), the site's for the H&S file. The API checks the key.
    regime_code    text NOT NULL CHECK (regime_code ~ '^[a-z0-9_]+$'),
    type_key       text NOT NULL CHECK (type_key ~ '^[a-z0-9_]+$'),
    title          text NOT NULL CHECK (char_length(btrim(title)) BETWEEN 1 AND 200),
    created_by     uuid NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    latest_version integer NOT NULL DEFAULT 0 CHECK (latest_version >= 0),

    FOREIGN KEY (org_id, site_id) REFERENCES sites (org_id, id),
    FOREIGN KEY (org_id, created_by) REFERENCES org_members (org_id, user_id),
    UNIQUE (org_id, id, scope),
    UNIQUE (org_id, id, site_id),
    CONSTRAINT documents_site_scope CHECK ((scope = 'site') = (site_id IS NOT NULL))
);

CREATE INDEX documents_org_idx ON documents (org_id, scope, type_key);
CREATE INDEX documents_site_idx ON documents (site_id) WHERE site_id IS NOT NULL;

CREATE TABLE document_versions (
    org_id             uuid NOT NULL,
    site_id            uuid,
    document_id        uuid NOT NULL,
    version            integer NOT NULL CHECK (version >= 1),
    scope              text NOT NULL CHECK (scope IN ('org', 'site')),
    file_id            uuid NOT NULL UNIQUE,
    file_purpose       text NOT NULL,
    file_status        text NOT NULL DEFAULT 'verified' CHECK (file_status = 'verified'),
    issuer             text CHECK (char_length(btrim(issuer)) BETWEEN 1 AND 200),
    reference          text CHECK (char_length(btrim(reference)) BETWEEN 1 AND 200),
    issued_on          date,
    expires_on         date CHECK (expires_on >= issued_on),
    uploaded_by        uuid NOT NULL,
    client_uploaded_at timestamptz NOT NULL,
    received_at        timestamptz NOT NULL DEFAULT now(),
    device_id          text CHECK (octet_length(device_id) BETWEEN 1 AND 200),

    PRIMARY KEY (document_id, version),
    FOREIGN KEY (org_id, document_id, scope) REFERENCES documents (org_id, id, scope),
    -- Checked only for site documents (site_id not null): same site.
    FOREIGN KEY (org_id, document_id, site_id) REFERENCES documents (org_id, id, site_id),
    FOREIGN KEY (org_id, file_id, file_purpose, file_status) REFERENCES files (org_id, id, purpose, status),
    -- A site document's file is of that site.
    FOREIGN KEY (org_id, site_id, file_id, file_purpose, file_status) REFERENCES files (org_id, site_id, id, purpose, status),
    FOREIGN KEY (org_id, uploaded_by) REFERENCES org_members (org_id, user_id),
    CONSTRAINT document_versions_site_scope CHECK ((scope = 'site') = (site_id IS NOT NULL)),
    CONSTRAINT document_versions_file_purpose CHECK (file_purpose = scope || '_document')
);

CREATE INDEX document_versions_expiry_idx ON document_versions (org_id, expires_on);

CREATE FUNCTION document_versions_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        NEW.received_at := now();
        IF NEW.client_uploaded_at > NEW.received_at + interval '5 minutes' THEN
            RAISE EXCEPTION 'document %: client_uploaded_at is in the future; check the device clock', NEW.document_id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

-- Moves the document on to its new version. The WHERE fails unless this is
-- the next number: a gap or a stale number is refused, and two saves of the
-- same number queue on the primary key.
CREATE FUNCTION document_versions_advance() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        UPDATE documents SET latest_version = NEW.version WHERE id = NEW.document_id AND latest_version = NEW.version - 1;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'document %: version % is not the next version', NEW.document_id, NEW.version
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

-- A document's header never changes, except latest_version moving on by one.
CREATE FUNCTION documents_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        rest documents;
    BEGIN
        IF TG_OP = 'DELETE' THEN
            RAISE EXCEPTION 'document %: documents are never deleted', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        rest := NEW;
        rest.latest_version := OLD.latest_version;
        IF rest IS DISTINCT FROM OLD OR NEW.latest_version NOT IN (OLD.latest_version, OLD.latest_version + 1) THEN
            RAISE EXCEPTION 'document %: only its latest version moves on, by one', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

-- At commit, every new document has a first version.
CREATE FUNCTION documents_has_version() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF (SELECT latest_version FROM documents WHERE id = NEW.id) = 0 THEN
            RAISE EXCEPTION 'document %: a document is saved with its first version', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

CREATE FUNCTION documents_append_only() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        RAISE EXCEPTION '% is append-only', TG_TABLE_NAME
            USING ERRCODE = 'check_violation';
    END
    $$;

CREATE TRIGGER document_versions_insert BEFORE INSERT ON document_versions
    FOR EACH ROW EXECUTE FUNCTION document_versions_insert();
CREATE TRIGGER document_versions_advance AFTER INSERT ON document_versions
    FOR EACH ROW EXECUTE FUNCTION document_versions_advance();
CREATE TRIGGER document_versions_append_only BEFORE UPDATE OR DELETE ON document_versions
    FOR EACH ROW EXECUTE FUNCTION documents_append_only();
CREATE TRIGGER documents_guard BEFORE UPDATE OR DELETE ON documents
    FOR EACH ROW EXECUTE FUNCTION documents_guard();
CREATE CONSTRAINT TRIGGER documents_has_version AFTER INSERT ON documents
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION documents_has_version();
CREATE TRIGGER documents_no_truncate BEFORE TRUNCATE ON documents
    FOR EACH STATEMENT EXECUTE FUNCTION documents_append_only();
CREATE TRIGGER document_versions_no_truncate BEFORE TRUNCATE ON document_versions
    FOR EACH STATEMENT EXECUTE FUNCTION documents_append_only();

ALTER TABLE documents ENABLE ROW LEVEL SECURITY;
ALTER TABLE documents FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON documents FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE document_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE document_versions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON document_versions FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

-- The app moves latest_version (through the trigger) and nothing else.
REVOKE UPDATE, DELETE ON documents FROM siteproof_app;
GRANT UPDATE (latest_version) ON documents TO siteproof_app;
REVOKE UPDATE, DELETE ON document_versions FROM siteproof_app;

COMMIT;
