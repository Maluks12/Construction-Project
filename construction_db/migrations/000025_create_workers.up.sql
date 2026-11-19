BEGIN;

-- The worker register (M11 D5–D9). Four tables:
--   - workers: who works for the organisation. Editable by the API; never
--     deleted, so every record that names a worker keeps its target.
--   - worker_competencies and worker_medicals: certificates and medical
--     fitness, each with a verified file of its own purpose. Append-only: a
--     renewal is a new row superseding the old (one live successor), and a
--     mistake is retracted once, by an owner or SHE officer, with a reason.
--   - toolbox_attendance_workers: links a toolbox signature to a worker
--     without touching the signature (M10 D1). One live link per signature;
--     a wrong link is unlinked once, with a reason.
-- Ids come from the device. Every guard applies to every role.

CREATE TABLE workers (
    id            uuid PRIMARY KEY,
    org_id        uuid NOT NULL,
    name          text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 200),
    firm          text CHECK (char_length(btrim(firm)) BETWEEN 1 AND 200),
    clock_number  text CHECK (char_length(btrim(clock_number)) BETWEEN 1 AND 50),
    trade         text CHECK (char_length(btrim(trade)) BETWEEN 1 AND 100),
    active        boolean NOT NULL DEFAULT true,
    -- The M10 identity key (000023): the register and toolbox signatures
    -- match on exactly this, never on a partial or fuzzy match.
    identity_key  text GENERATED ALWAYS AS (
                      toolbox_norm(name) || '|' || coalesce(toolbox_norm(firm), '') || '|' || coalesce(toolbox_norm(clock_number), '')
                  ) STORED,
    created_by    uuid NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_by    uuid,
    updated_at    timestamptz,

    FOREIGN KEY (org_id, created_by) REFERENCES org_members (org_id, user_id),
    FOREIGN KEY (org_id, updated_by) REFERENCES org_members (org_id, user_id),
    UNIQUE (org_id, id),
    CONSTRAINT workers_identity UNIQUE (org_id, identity_key)
);

CREATE FUNCTION workers_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF TG_OP = 'DELETE' THEN
            RAISE EXCEPTION 'worker %: workers are never deleted; mark them inactive', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF (NEW.id, NEW.org_id, NEW.created_by, NEW.created_at) IS DISTINCT FROM (OLD.id, OLD.org_id, OLD.created_by, OLD.created_at) THEN
            RAISE EXCEPTION 'worker %: only the name, firm, clock number, trade and active can change', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW IS DISTINCT FROM OLD THEN
            NEW.updated_at := now();
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER workers_guard BEFORE UPDATE OR DELETE ON workers
    FOR EACH ROW EXECUTE FUNCTION workers_guard();

-- An owner or SHE officer, active in the organisation.
CREATE FUNCTION workers_lead(p_org uuid, p_user uuid) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
        SELECT EXISTS (SELECT 1 FROM org_members
                       WHERE org_id = p_org AND user_id = p_user AND status = 'active' AND role IN ('owner', 'she_officer'))
    $$;

CREATE TABLE worker_competencies (
    id                 uuid PRIMARY KEY,
    org_id             uuid NOT NULL,
    worker_id          uuid NOT NULL,
    -- The regime the type was validated against (D1); the API checks the key.
    regime_code        text NOT NULL CHECK (regime_code ~ '^[a-z0-9_]+$'),
    type_key           text NOT NULL CHECK (type_key ~ '^[a-z0-9_]+$'),
    number             text CHECK (char_length(btrim(number)) BETWEEN 1 AND 100),
    issuer             text CHECK (char_length(btrim(issuer)) BETWEEN 1 AND 200),
    issued_on          date NOT NULL,
    expires_on         date CHECK (expires_on >= issued_on),
    file_id            uuid NOT NULL UNIQUE,
    file_purpose       text NOT NULL DEFAULT 'competency' CHECK (file_purpose = 'competency'),
    file_status        text NOT NULL DEFAULT 'verified' CHECK (file_status = 'verified'),
    supersedes_id      uuid,
    recorded_by        uuid NOT NULL,
    client_recorded_at timestamptz NOT NULL,
    recorded_at        timestamptz NOT NULL DEFAULT now(),
    device_id          text CHECK (octet_length(device_id) BETWEEN 1 AND 200),
    retracted_at       timestamptz,
    retracted_by       uuid,
    retract_reason     text CHECK (char_length(btrim(retract_reason)) BETWEEN 1 AND 500),

    FOREIGN KEY (org_id, worker_id) REFERENCES workers (org_id, id),
    FOREIGN KEY (org_id, file_id, file_purpose, file_status) REFERENCES files (org_id, id, purpose, status),
    FOREIGN KEY (org_id, recorded_by) REFERENCES org_members (org_id, user_id),
    FOREIGN KEY (org_id, retracted_by) REFERENCES org_members (org_id, user_id),
    UNIQUE (org_id, id),
    UNIQUE (org_id, id, worker_id, regime_code, type_key),
    -- A renewal is of the same worker's same type.
    FOREIGN KEY (org_id, supersedes_id, worker_id, regime_code, type_key)
        REFERENCES worker_competencies (org_id, id, worker_id, regime_code, type_key),
    CHECK (supersedes_id <> id),
    CONSTRAINT worker_competencies_retraction CHECK (
        (retracted_at IS NULL) = (retracted_by IS NULL) AND (retracted_at IS NULL) = (retract_reason IS NULL))
);

-- One live renewal per record; a retracted renewal frees the place.
CREATE UNIQUE INDEX worker_competencies_one_successor ON worker_competencies (supersedes_id) WHERE retracted_at IS NULL;
CREATE INDEX worker_competencies_worker_idx ON worker_competencies (worker_id);
CREATE INDEX worker_competencies_expiry_idx ON worker_competencies (org_id, expires_on) WHERE retracted_at IS NULL;

-- Medical fitness (D6): the outcome, the dates, an optional practice number
-- and the certificate. No findings, diagnoses or restrictions: the only text
-- column is the practice number, limited to the characters one has.
CREATE TABLE worker_medicals (
    id                 uuid PRIMARY KEY,
    org_id             uuid NOT NULL,
    worker_id          uuid NOT NULL,
    regime_code        text NOT NULL CHECK (regime_code ~ '^[a-z0-9_]+$'),
    outcome            text NOT NULL CHECK (outcome IN ('fit', 'fit_with_restrictions', 'unfit')),
    examined_on        date NOT NULL,
    expires_on         date NOT NULL CHECK (expires_on >= examined_on),
    practice_number    text CHECK (practice_number ~ '^[A-Za-z0-9][A-Za-z0-9 ./-]{0,29}$'),
    file_id            uuid NOT NULL UNIQUE,
    file_purpose       text NOT NULL DEFAULT 'medical' CHECK (file_purpose = 'medical'),
    file_status        text NOT NULL DEFAULT 'verified' CHECK (file_status = 'verified'),
    supersedes_id      uuid,
    recorded_by        uuid NOT NULL,
    client_recorded_at timestamptz NOT NULL,
    recorded_at        timestamptz NOT NULL DEFAULT now(),
    device_id          text CHECK (octet_length(device_id) BETWEEN 1 AND 200),
    retracted_at       timestamptz,
    retracted_by       uuid,
    retract_reason     text CHECK (char_length(btrim(retract_reason)) BETWEEN 1 AND 500),

    FOREIGN KEY (org_id, worker_id) REFERENCES workers (org_id, id),
    FOREIGN KEY (org_id, file_id, file_purpose, file_status) REFERENCES files (org_id, id, purpose, status),
    FOREIGN KEY (org_id, recorded_by) REFERENCES org_members (org_id, user_id),
    FOREIGN KEY (org_id, retracted_by) REFERENCES org_members (org_id, user_id),
    UNIQUE (org_id, id),
    UNIQUE (org_id, id, worker_id),
    FOREIGN KEY (org_id, supersedes_id, worker_id) REFERENCES worker_medicals (org_id, id, worker_id),
    CHECK (supersedes_id <> id),
    CONSTRAINT worker_medicals_retraction CHECK (
        (retracted_at IS NULL) = (retracted_by IS NULL) AND (retracted_at IS NULL) = (retract_reason IS NULL))
);

CREATE UNIQUE INDEX worker_medicals_one_successor ON worker_medicals (supersedes_id) WHERE retracted_at IS NULL;
CREATE INDEX worker_medicals_worker_idx ON worker_medicals (worker_id);

-- Shared by both record tables (TG_TABLE_NAME picks the table): server
-- time, the device clock, no retraction at insert, and no renewal of a
-- retracted record.
CREATE FUNCTION worker_records_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        prev_retracted timestamptz;
    BEGIN
        NEW.recorded_at := now();
        IF NEW.client_recorded_at > NEW.recorded_at + interval '5 minutes' THEN
            RAISE EXCEPTION '%: client_recorded_at is in the future; check the device clock', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.retracted_at IS NOT NULL OR NEW.retracted_by IS NOT NULL OR NEW.retract_reason IS NOT NULL THEN
            RAISE EXCEPTION '%: a record is retracted after it is recorded, never at insert', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.supersedes_id IS NOT NULL THEN
            EXECUTE format('SELECT retracted_at FROM %I WHERE id = $1', TG_TABLE_NAME) INTO prev_retracted USING NEW.supersedes_id;
            IF prev_retracted IS NOT NULL THEN
                RAISE EXCEPTION '%: a retracted record can''t be renewed', NEW.id
                    USING ERRCODE = 'check_violation';
            END IF;
        END IF;
        RETURN NEW;
    END
    $$;

-- The one change: a retraction, once, by an owner or SHE officer.
CREATE FUNCTION worker_records_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        rest_new jsonb;
        rest_old jsonb;
    BEGIN
        IF TG_OP = 'DELETE' THEN
            RAISE EXCEPTION '%: records are never deleted; retract them', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW IS NOT DISTINCT FROM OLD THEN
            RETURN NEW;
        END IF;
        IF OLD.retracted_at IS NOT NULL THEN
            RAISE EXCEPTION '%: already retracted; it never changes again', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        rest_new := to_jsonb(NEW) - 'retracted_at' - 'retracted_by' - 'retract_reason';
        rest_old := to_jsonb(OLD) - 'retracted_at' - 'retracted_by' - 'retract_reason';
        IF rest_new IS DISTINCT FROM rest_old OR NEW.retracted_by IS NULL OR NEW.retract_reason IS NULL THEN
            RAISE EXCEPTION '%: the only change is a retraction, with who and why', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NOT workers_lead(NEW.org_id, NEW.retracted_by) THEN
            RAISE EXCEPTION '%: only an owner or SHE officer can retract it', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        NEW.retracted_at := now();
        RETURN NEW;
    END
    $$;

CREATE TRIGGER worker_competencies_insert BEFORE INSERT ON worker_competencies
    FOR EACH ROW EXECUTE FUNCTION worker_records_insert();
CREATE TRIGGER worker_competencies_guard BEFORE UPDATE OR DELETE ON worker_competencies
    FOR EACH ROW EXECUTE FUNCTION worker_records_guard();
CREATE TRIGGER worker_medicals_insert BEFORE INSERT ON worker_medicals
    FOR EACH ROW EXECUTE FUNCTION worker_records_insert();
CREATE TRIGGER worker_medicals_guard BEFORE UPDATE OR DELETE ON worker_medicals
    FOR EACH ROW EXECUTE FUNCTION worker_records_guard();

-- A link from a toolbox signature to a worker (D7). 'auto': made at signing
-- because the identity keys are equal (checked here); 'manual': a lead's.
CREATE TABLE toolbox_attendance_workers (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id         uuid NOT NULL,
    site_id        uuid NOT NULL,
    attendance_id  uuid NOT NULL,
    worker_id      uuid NOT NULL,
    method         text NOT NULL CHECK (method IN ('auto', 'manual')),
    linked_by      uuid NOT NULL,
    linked_at      timestamptz NOT NULL DEFAULT now(),
    unlinked_at    timestamptz,
    unlinked_by    uuid,
    unlink_reason  text CHECK (char_length(btrim(unlink_reason)) BETWEEN 1 AND 500),

    FOREIGN KEY (org_id, site_id, attendance_id) REFERENCES toolbox_attendance (org_id, site_id, id),
    FOREIGN KEY (org_id, worker_id) REFERENCES workers (org_id, id),
    FOREIGN KEY (org_id, linked_by) REFERENCES org_members (org_id, user_id),
    FOREIGN KEY (org_id, unlinked_by) REFERENCES org_members (org_id, user_id),
    CONSTRAINT toolbox_attendance_workers_unlink CHECK (
        (unlinked_at IS NULL) = (unlinked_by IS NULL) AND (unlinked_at IS NULL) = (unlink_reason IS NULL))
);

CREATE UNIQUE INDEX toolbox_attendance_workers_one_live ON toolbox_attendance_workers (attendance_id) WHERE unlinked_at IS NULL;
CREATE INDEX toolbox_attendance_workers_worker_idx ON toolbox_attendance_workers (worker_id);

-- Checked after the insert (the M9/M10 pattern): RLS's WITH CHECK runs after
-- BEFORE triggers, so a cross-tenant row is refused by the policy first.
CREATE FUNCTION toolbox_attendance_workers_check() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF NEW.unlinked_at IS NOT NULL OR NEW.unlinked_by IS NOT NULL OR NEW.unlink_reason IS NOT NULL THEN
            RAISE EXCEPTION 'link %: a link is unlinked after it is made, never at insert', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.method = 'auto' AND NOT EXISTS (
            SELECT 1 FROM toolbox_attendance a JOIN workers w ON w.id = NEW.worker_id
            WHERE a.id = NEW.attendance_id AND a.identity_key = w.identity_key) THEN
            RAISE EXCEPTION 'link %: an automatic link needs the same identity key', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.method = 'manual' AND NOT workers_lead(NEW.org_id, NEW.linked_by) THEN
            RAISE EXCEPTION 'link %: only an owner or SHE officer can link a signature by hand', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

CREATE CONSTRAINT TRIGGER toolbox_attendance_workers_check AFTER INSERT ON toolbox_attendance_workers
    FOR EACH ROW EXECUTE FUNCTION toolbox_attendance_workers_check();

CREATE FUNCTION toolbox_attendance_workers_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF TG_OP = 'DELETE' THEN
            RAISE EXCEPTION 'link %: links are never deleted; unlink them', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW IS NOT DISTINCT FROM OLD THEN
            RETURN NEW;
        END IF;
        IF OLD.unlinked_at IS NOT NULL THEN
            RAISE EXCEPTION 'link %: already unlinked; it never changes again', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF (NEW.id, NEW.org_id, NEW.site_id, NEW.attendance_id, NEW.worker_id, NEW.method, NEW.linked_by, NEW.linked_at)
           IS DISTINCT FROM (OLD.id, OLD.org_id, OLD.site_id, OLD.attendance_id, OLD.worker_id, OLD.method, OLD.linked_by, OLD.linked_at)
           OR NEW.unlinked_by IS NULL OR NEW.unlink_reason IS NULL THEN
            RAISE EXCEPTION 'link %: the only change is unlinking it, with who and why', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NOT workers_lead(NEW.org_id, NEW.unlinked_by) THEN
            RAISE EXCEPTION 'link %: only an owner or SHE officer can unlink it', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        NEW.unlinked_at := now();
        RETURN NEW;
    END
    $$;

CREATE TRIGGER toolbox_attendance_workers_guard BEFORE UPDATE OR DELETE ON toolbox_attendance_workers
    FOR EACH ROW EXECUTE FUNCTION toolbox_attendance_workers_guard();

-- The person a signature counts as (D7 and the headcount): its live link's
-- worker; else the register worker with its identity key, unless that very
-- pairing was unlinked as a different person; else the identity key itself.
CREATE FUNCTION toolbox_attendance_person(p_attendance uuid) RETURNS text
    LANGUAGE sql STABLE
    AS $$
        SELECT coalesce(
            (SELECT 'w:' || l.worker_id FROM toolbox_attendance_workers l
             WHERE l.attendance_id = a.id AND l.unlinked_at IS NULL),
            (SELECT 'w:' || w.id FROM workers w
             WHERE w.org_id = a.org_id AND w.identity_key = a.identity_key
               AND NOT EXISTS (SELECT 1 FROM toolbox_attendance_workers u
                               WHERE u.attendance_id = a.id AND u.worker_id = w.id AND u.unlinked_at IS NOT NULL)),
            'k:' || a.identity_key)
        FROM toolbox_attendance a WHERE a.id = p_attendance
    $$;

CREATE FUNCTION workers_no_truncate() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        RAISE EXCEPTION '% is append-only', TG_TABLE_NAME
            USING ERRCODE = 'check_violation';
    END
    $$;

CREATE TRIGGER workers_no_truncate BEFORE TRUNCATE ON workers
    FOR EACH STATEMENT EXECUTE FUNCTION workers_no_truncate();
CREATE TRIGGER worker_competencies_no_truncate BEFORE TRUNCATE ON worker_competencies
    FOR EACH STATEMENT EXECUTE FUNCTION workers_no_truncate();
CREATE TRIGGER worker_medicals_no_truncate BEFORE TRUNCATE ON worker_medicals
    FOR EACH STATEMENT EXECUTE FUNCTION workers_no_truncate();
CREATE TRIGGER toolbox_attendance_workers_no_truncate BEFORE TRUNCATE ON toolbox_attendance_workers
    FOR EACH STATEMENT EXECUTE FUNCTION workers_no_truncate();

ALTER TABLE workers ENABLE ROW LEVEL SECURITY;
ALTER TABLE workers FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON workers FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE worker_competencies ENABLE ROW LEVEL SECURITY;
ALTER TABLE worker_competencies FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON worker_competencies FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE worker_medicals ENABLE ROW LEVEL SECURITY;
ALTER TABLE worker_medicals FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON worker_medicals FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE toolbox_attendance_workers ENABLE ROW LEVEL SECURITY;
ALTER TABLE toolbox_attendance_workers FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON toolbox_attendance_workers FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

-- Workers are edited, never deleted; records change only by a retraction;
-- links only by an unlink.
REVOKE DELETE ON workers FROM siteproof_app;
REVOKE UPDATE, DELETE ON worker_competencies FROM siteproof_app;
GRANT UPDATE (retracted_at, retracted_by, retract_reason) ON worker_competencies TO siteproof_app;
REVOKE UPDATE, DELETE ON worker_medicals FROM siteproof_app;
GRANT UPDATE (retracted_at, retracted_by, retract_reason) ON worker_medicals TO siteproof_app;
REVOKE UPDATE, DELETE ON toolbox_attendance_workers FROM siteproof_app;
GRANT UPDATE (unlinked_at, unlinked_by, unlink_reason) ON toolbox_attendance_workers TO siteproof_app;

COMMIT;
