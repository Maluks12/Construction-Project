BEGIN;

-- Written appointments per site (M11 D21; CR 2014 reg 8 "on a single site"
-- and the regulations listed in SOURCES): a register worker, a type from the
-- site regime, dates and the signed letter (a verified file of the site).
-- As competencies (000025): append-only, renewed by a new row superseding
-- the old (same site and type, one live successor; the appointee may
-- change), retracted once by an owner or SHE officer. Only owners and SHE
-- officers record them.

CREATE TABLE site_appointments (
    id                 uuid PRIMARY KEY,
    org_id             uuid NOT NULL,
    site_id            uuid NOT NULL,
    worker_id          uuid NOT NULL,
    regime_code        text NOT NULL CHECK (regime_code ~ '^[a-z0-9_]+$'),
    type_key           text NOT NULL CHECK (type_key ~ '^[a-z0-9_]+$'),
    appointed_on       date NOT NULL,
    expires_on         date CHECK (expires_on >= appointed_on),
    file_id            uuid NOT NULL UNIQUE,
    file_purpose       text NOT NULL DEFAULT 'appointment' CHECK (file_purpose = 'appointment'),
    file_status        text NOT NULL DEFAULT 'verified' CHECK (file_status = 'verified'),
    supersedes_id      uuid,
    recorded_by        uuid NOT NULL,
    client_recorded_at timestamptz NOT NULL,
    recorded_at        timestamptz NOT NULL DEFAULT now(),
    device_id          text CHECK (octet_length(device_id) BETWEEN 1 AND 200),
    retracted_at       timestamptz,
    retracted_by       uuid,
    retract_reason     text CHECK (char_length(btrim(retract_reason)) BETWEEN 1 AND 500),

    FOREIGN KEY (org_id, site_id) REFERENCES sites (org_id, id),
    FOREIGN KEY (org_id, worker_id) REFERENCES workers (org_id, id),
    FOREIGN KEY (org_id, site_id, file_id, file_purpose, file_status) REFERENCES files (org_id, site_id, id, purpose, status),
    FOREIGN KEY (org_id, recorded_by) REFERENCES org_members (org_id, user_id),
    FOREIGN KEY (org_id, retracted_by) REFERENCES org_members (org_id, user_id),
    UNIQUE (org_id, id),
    UNIQUE (org_id, site_id, id, type_key),
    FOREIGN KEY (org_id, site_id, supersedes_id, type_key) REFERENCES site_appointments (org_id, site_id, id, type_key),
    CHECK (supersedes_id <> id),
    CONSTRAINT site_appointments_retraction CHECK (
        (retracted_at IS NULL) = (retracted_by IS NULL) AND (retracted_at IS NULL) = (retract_reason IS NULL))
);

CREATE UNIQUE INDEX site_appointments_one_successor ON site_appointments (supersedes_id) WHERE retracted_at IS NULL;
CREATE INDEX site_appointments_site_idx ON site_appointments (site_id, type_key);

-- After the insert (RLS first): the recorder is an owner or SHE officer.
CREATE FUNCTION site_appointments_check() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF NOT workers_lead(NEW.org_id, NEW.recorded_by) THEN
            RAISE EXCEPTION 'appointment %: only an owner or SHE officer records appointments', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

CREATE TRIGGER site_appointments_insert BEFORE INSERT ON site_appointments
    FOR EACH ROW EXECUTE FUNCTION worker_records_insert();
CREATE TRIGGER site_appointments_check AFTER INSERT ON site_appointments
    FOR EACH ROW EXECUTE FUNCTION site_appointments_check();
CREATE TRIGGER site_appointments_guard BEFORE UPDATE OR DELETE ON site_appointments
    FOR EACH ROW EXECUTE FUNCTION worker_records_guard();
CREATE TRIGGER site_appointments_no_truncate BEFORE TRUNCATE ON site_appointments
    FOR EACH STATEMENT EXECUTE FUNCTION workers_no_truncate();

ALTER TABLE site_appointments ENABLE ROW LEVEL SECURITY;
ALTER TABLE site_appointments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON site_appointments FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

REVOKE UPDATE, DELETE ON site_appointments FROM siteproof_app;
GRANT UPDATE (retracted_at, retracted_by, retract_reason) ON site_appointments TO siteproof_app;

COMMIT;
