BEGIN;

-- ONBOARD D10: a CSV import of the worker register, all or nothing, under an
-- id the dashboard chooses. Its row is written in the same transaction as the
-- workers it creates, so a repeat of the id (a lost response) answers with
-- the first result instead of "already on the register" for every row.
-- Append-only: an import is a fact.
CREATE TABLE worker_imports (
    id          uuid PRIMARY KEY,
    org_id      uuid NOT NULL REFERENCES organisations (id),
    imported_by uuid NOT NULL,
    imported_at timestamptz NOT NULL DEFAULT now(),
    file_sha256 text NOT NULL CHECK (file_sha256 ~ '^[0-9a-f]{64}$'),
    encoding    text NOT NULL CHECK (encoding IN ('utf-8', 'windows-1252')),
    row_count   integer NOT NULL CHECK (row_count BETWEEN 1 AND 5000),
    -- The workers created, in the file's order.
    worker_ids  uuid[] NOT NULL CHECK (cardinality(worker_ids) = row_count),

    FOREIGN KEY (org_id, imported_by) REFERENCES org_members (org_id, user_id),
    UNIQUE (org_id, id)
);

ALTER TABLE worker_imports ENABLE ROW LEVEL SECURITY;
ALTER TABLE worker_imports FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON worker_imports FOR ALL TO siteproof_app
    USING (org_id = app_org_id()) WITH CHECK (org_id = app_org_id());
REVOKE UPDATE, DELETE, TRUNCATE ON worker_imports FROM siteproof_app;
CREATE TRIGGER worker_imports_append_only BEFORE UPDATE OR DELETE ON worker_imports
    FOR EACH ROW EXECUTE FUNCTION history_append_only();
CREATE TRIGGER worker_imports_no_truncate BEFORE TRUNCATE ON worker_imports
    FOR EACH STATEMENT EXECUTE FUNCTION history_append_only();

COMMIT;
