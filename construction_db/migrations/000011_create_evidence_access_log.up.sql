BEGIN;

-- Target for evidence_access_log's foreign key: a log row's evidence must be
-- on the site the row names.
ALTER TABLE evidence ADD CONSTRAINT evidence_org_site_id_unique UNIQUE (org_id, site_id, id);

-- Who saw which evidence, when and from where (Rev 4 §2.1 and A.2).
-- Append-only. A list request is one row with no evidence_id.
CREATE TABLE evidence_access_log (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id            uuid NOT NULL,
    site_id           uuid NOT NULL,
    evidence_id       uuid,
    user_id           uuid NOT NULL,
    action            text NOT NULL CHECK (action IN ('view_bytes', 'view_record', 'list', 'export', 'share')),
    -- The exact S3 version a view_bytes URL was signed for.
    object_version_id text,
    filter_status     text,
    filter_limit      integer,
    result_count      integer CHECK (result_count >= 0),
    ip                inet,
    user_agent        text CHECK (octet_length(user_agent) <= 512),
    -- clock_timestamp, not now(): the time of this row, not of the transaction's start.
    accessed_at       timestamptz NOT NULL DEFAULT clock_timestamp(),

    FOREIGN KEY (org_id, site_id) REFERENCES sites (org_id, id),
    FOREIGN KEY (org_id, site_id, evidence_id) REFERENCES evidence (org_id, site_id, id),
    FOREIGN KEY (org_id, user_id) REFERENCES org_members (org_id, user_id),

    CONSTRAINT evidence_access_log_list_has_no_evidence CHECK ((action = 'list') = (evidence_id IS NULL)),
    CONSTRAINT evidence_access_log_list_count CHECK ((action = 'list') = (result_count IS NOT NULL)),
    CONSTRAINT evidence_access_log_list_filters CHECK (action = 'list' OR (filter_status IS NULL AND filter_limit IS NULL)),
    CONSTRAINT evidence_access_log_bytes_version CHECK ((action = 'view_bytes') = (object_version_id IS NOT NULL))
);

CREATE INDEX evidence_access_log_evidence_idx ON evidence_access_log (evidence_id, accessed_at);
CREATE INDEX evidence_access_log_site_idx ON evidence_access_log (site_id, accessed_at);

-- Grants keep the app to INSERT and SELECT; this trigger stops everyone else,
-- the table owner and superusers included. Row triggers don't fire on
-- TRUNCATE, hence the statement trigger.
CREATE FUNCTION evidence_access_log_append_only() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        RAISE EXCEPTION 'evidence_access_log is append-only'
            USING ERRCODE = 'check_violation';
    END
    $$;

CREATE TRIGGER evidence_access_log_append_only BEFORE UPDATE OR DELETE ON evidence_access_log
    FOR EACH ROW EXECUTE FUNCTION evidence_access_log_append_only();
CREATE TRIGGER evidence_access_log_no_truncate BEFORE TRUNCATE ON evidence_access_log
    FOR EACH STATEMENT EXECUTE FUNCTION evidence_access_log_append_only();

ALTER TABLE evidence_access_log ENABLE ROW LEVEL SECURITY;
ALTER TABLE evidence_access_log FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON evidence_access_log FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

-- Migration 000008's default privileges grant UPDATE and DELETE on new tables.
REVOKE UPDATE, DELETE ON evidence_access_log FROM siteproof_app;

COMMIT;
