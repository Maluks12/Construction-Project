BEGIN;

-- Target for inspection_response_evidence's foreign key. A link carries the
-- constant evidence_status 'verified', so it can only match a verified row,
-- and a verified row never changes (evidence_freeze, and this key itself).
ALTER TABLE evidence ADD CONSTRAINT evidence_org_site_id_status_unique UNIQUE (org_id, site_id, id, status);

-- An inspection is one filling-in of one checklist template version (Rev 4
-- §2.4; Rev 4 §5.3 calls it a checklist instance). The id is generated on
-- the device so offline sync is idempotent. Templates live in Go
-- (internal/regimes), so the row records which version was used, its content
-- hash, and copies of what the inspector read.
CREATE TABLE inspections (
    id                  uuid PRIMARY KEY,
    org_id              uuid NOT NULL,
    site_id             uuid NOT NULL,
    regime_id           uuid NOT NULL REFERENCES regulatory_regimes (id),
    template_key        text NOT NULL CHECK (template_key <> ''),
    template_version    integer NOT NULL CHECK (template_version >= 1),
    template_sha256     bytea NOT NULL CHECK (length(template_sha256) = 32),
    template_title      text NOT NULL,
    template_source_ref text NOT NULL,
    -- The template version's number of items. The database can't see the
    -- template, so completion checks the inspection has exactly this many
    -- responses: a response added after start makes completion fail.
    item_count          integer NOT NULL CHECK (item_count >= 1),
    status              text NOT NULL DEFAULT 'in_progress' CHECK (status IN ('in_progress', 'completed')),
    started_by          uuid NOT NULL,
    client_started_at   timestamptz NOT NULL,
    started_at          timestamptz NOT NULL DEFAULT now(),
    device_id           text CHECK (octet_length(device_id) BETWEEN 1 AND 200),
    -- A correction is a new inspection; this points at the one it corrects.
    corrects_id         uuid,
    client_completed_at timestamptz,
    completed_at        timestamptz,

    FOREIGN KEY (org_id, site_id) REFERENCES sites (org_id, id),
    FOREIGN KEY (org_id, started_by) REFERENCES org_members (org_id, user_id),
    -- Target of the child tables' foreign keys, so a child is always on its
    -- inspection's site and org.
    UNIQUE (org_id, site_id, id),

    CONSTRAINT inspections_not_self_correcting CHECK (corrects_id <> id),
    CONSTRAINT inspections_completion CHECK (
        (status = 'completed') = (completed_at IS NOT NULL)
        AND (completed_at IS NULL) = (client_completed_at IS NULL))
);

-- Self-referencing, so added once the unique key exists. Same site only.
ALTER TABLE inspections ADD CONSTRAINT inspections_corrects_fkey
    FOREIGN KEY (org_id, site_id, corrects_id) REFERENCES inspections (org_id, site_id, id);

CREATE INDEX inspections_site_idx ON inspections (site_id, started_at);

-- One row per item of the template version, inserted at start with a copy of
-- the item; the answer fills in result (and severity on a fail).
CREATE TABLE inspection_responses (
    org_id             uuid NOT NULL,
    site_id            uuid NOT NULL,
    inspection_id      uuid NOT NULL,
    item_key           text NOT NULL CHECK (item_key <> ''),
    position           integer NOT NULL CHECK (position >= 1),
    prompt             text NOT NULL CHECK (prompt <> ''),
    item_source_ref    text,
    -- The values of checklists.SupportedResponseTypes.
    response_type      text NOT NULL CHECK (response_type IN ('pass_fail_na')),
    result             text CHECK (result IN ('pass', 'fail', 'na')),
    -- The keys of checklists.Severities.
    severity           text CHECK (severity IN ('low', 'medium', 'high', 'critical')),
    comment            text CHECK (char_length(comment) <= 2000),
    client_answered_at timestamptz,
    answered_at        timestamptz,

    PRIMARY KEY (inspection_id, item_key),
    UNIQUE (inspection_id, position),
    FOREIGN KEY (org_id, site_id, inspection_id) REFERENCES inspections (org_id, site_id, id),
    -- Targets of inspection_response_evidence's and inspection_findings' keys.
    UNIQUE (org_id, site_id, inspection_id, item_key),
    UNIQUE (org_id, site_id, inspection_id, item_key, result, severity),

    CONSTRAINT inspection_responses_severity_on_fail CHECK ((result IS NOT DISTINCT FROM 'fail') = (severity IS NOT NULL)),
    CONSTRAINT inspection_responses_answered CHECK (
        (result IS NULL) = (answered_at IS NULL)
        AND (result IS NULL) = (client_answered_at IS NULL)
        AND (result IS NOT NULL OR comment IS NULL))
);

-- Photos on an answer: only verified evidence from the inspection's own site,
-- enforced by the two foreign keys sharing site_id.
CREATE TABLE inspection_response_evidence (
    org_id          uuid NOT NULL,
    site_id         uuid NOT NULL,
    inspection_id   uuid NOT NULL,
    item_key        text NOT NULL,
    evidence_id     uuid NOT NULL,
    evidence_status text NOT NULL DEFAULT 'verified' CHECK (evidence_status = 'verified'),
    attached_at     timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (inspection_id, item_key, evidence_id),
    FOREIGN KEY (org_id, site_id, inspection_id, item_key)
        REFERENCES inspection_responses (org_id, site_id, inspection_id, item_key),
    FOREIGN KEY (org_id, site_id, evidence_id, evidence_status)
        REFERENCES evidence (org_id, site_id, id, status)
);

CREATE INDEX inspection_response_evidence_evidence_idx ON inspection_response_evidence (evidence_id);

-- One finding per failed item of a completed inspection, raised by the
-- completion trigger. The key makes a finding point at a fail with the same
-- severity. Corrective actions (M9) will reference it.
CREATE TABLE inspection_findings (
    org_id        uuid NOT NULL,
    site_id       uuid NOT NULL,
    inspection_id uuid NOT NULL,
    item_key      text NOT NULL,
    result        text NOT NULL DEFAULT 'fail' CHECK (result = 'fail'),
    severity      text NOT NULL,
    raised_at     timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (inspection_id, item_key),
    FOREIGN KEY (org_id, site_id, inspection_id, item_key, result, severity)
        REFERENCES inspection_responses (org_id, site_id, inspection_id, item_key, result, severity)
);

CREATE INDEX inspection_findings_site_idx ON inspection_findings (site_id, raised_at);

-- Every guard below applies to every role, the table owner included. Each
-- raises check_violation with its own message.

-- A new inspection starts in progress: inserting it completed would skip
-- every completion check. A correction must point at a completed one.
CREATE FUNCTION inspections_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF NEW.status <> 'in_progress' OR NEW.completed_at IS NOT NULL OR NEW.client_completed_at IS NOT NULL THEN
            RAISE EXCEPTION 'inspection %: a new inspection starts in progress', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.corrects_id IS NOT NULL
           AND NOT EXISTS (SELECT 1 FROM inspections WHERE id = NEW.corrects_id AND status = 'completed') THEN
            RAISE EXCEPTION 'inspection %: can only correct a completed inspection', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER inspections_insert BEFORE INSERT ON inspections
    FOR EACH ROW EXECUTE FUNCTION inspections_insert();

-- Nothing about a completed inspection changes. An inspection in progress
-- changes only by being completed, and only if it is complete: every item
-- answered, exactly item_count responses, and a photo on every fail.
CREATE FUNCTION inspections_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        rest      inspections;
        responses integer;
        missing   text;
    BEGIN
        IF OLD.status = 'completed' THEN
            RAISE EXCEPTION 'inspection % is completed; start a new inspection to correct it', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF TG_OP = 'DELETE' THEN
            RETURN OLD; -- in progress: privileges decide (the app has none)
        END IF;

        rest := NEW;
        rest.status := OLD.status;
        rest.completed_at := OLD.completed_at;
        rest.client_completed_at := OLD.client_completed_at;
        IF NEW.status <> 'completed' OR rest IS DISTINCT FROM OLD THEN
            RAISE EXCEPTION 'inspection %: the only change allowed is completing it', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;

        -- These queries see every answer committed before this UPDATE got
        -- the row lock (READ COMMITTED; each statement takes a new snapshot).
        SELECT count(*) INTO responses FROM inspection_responses WHERE inspection_id = OLD.id;
        IF responses <> OLD.item_count THEN
            RAISE EXCEPTION 'inspection %: has % responses but its template version has % items', OLD.id, responses, OLD.item_count
                USING ERRCODE = 'check_violation';
        END IF;

        SELECT string_agg(item_key, ', ' ORDER BY position) INTO missing
        FROM inspection_responses WHERE inspection_id = OLD.id AND result IS NULL;
        IF missing IS NOT NULL THEN
            RAISE EXCEPTION 'inspection %: items not answered: %', OLD.id, missing
                USING ERRCODE = 'check_violation';
        END IF;

        SELECT string_agg(r.item_key, ', ' ORDER BY r.position) INTO missing
        FROM inspection_responses r
        WHERE r.inspection_id = OLD.id AND r.result = 'fail'
          AND NOT EXISTS (SELECT 1 FROM inspection_response_evidence e
                          WHERE e.inspection_id = r.inspection_id AND e.item_key = r.item_key);
        IF missing IS NOT NULL THEN
            RAISE EXCEPTION 'inspection %: a failed item needs a photo: %', OLD.id, missing
                USING ERRCODE = 'check_violation';
        END IF;

        RETURN NEW;
    END
    $$;

CREATE TRIGGER inspections_guard BEFORE UPDATE OR DELETE ON inspections
    FOR EACH ROW EXECUTE FUNCTION inspections_guard();

-- Completing raises one finding per fail, so findings and fails always match.
CREATE FUNCTION inspections_raise_findings() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF OLD.status = 'in_progress' AND NEW.status = 'completed' THEN
            INSERT INTO inspection_findings (org_id, site_id, inspection_id, item_key, severity)
            SELECT org_id, site_id, inspection_id, item_key, severity
            FROM inspection_responses
            WHERE inspection_id = NEW.id AND result = 'fail';
        END IF;
        RETURN NULL;
    END
    $$;

CREATE TRIGGER inspections_raise_findings AFTER UPDATE ON inspections
    FOR EACH ROW EXECUTE FUNCTION inspections_raise_findings();

-- Responses and photo links change only while their inspection is in
-- progress. FOR SHARE on the inspection row is what orders them against a
-- completion: completing (an UPDATE) waits for open answers to commit, and an
-- answer arriving after a completion committed re-reads the row, sees
-- 'completed' and fails. A missing parent is left to the foreign key (or RLS)
-- to report.
CREATE FUNCTION inspection_responses_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        parent_id     uuid;
        parent_status text;
        rest          inspection_responses;
    BEGIN
        IF TG_OP = 'DELETE' THEN
            parent_id := OLD.inspection_id;
        ELSE
            parent_id := NEW.inspection_id;
        END IF;
        SELECT status INTO parent_status FROM inspections WHERE id = parent_id FOR SHARE;
        IF FOUND AND parent_status <> 'in_progress' THEN
            RAISE EXCEPTION 'inspection % is completed; start a new inspection to correct it', parent_id
                USING ERRCODE = 'check_violation';
        END IF;

        IF TG_OP = 'UPDATE' THEN
            -- Only the answer changes; the copy of the item never does.
            rest := NEW;
            rest.result := OLD.result;
            rest.severity := OLD.severity;
            rest.comment := OLD.comment;
            rest.client_answered_at := OLD.client_answered_at;
            rest.answered_at := OLD.answered_at;
            IF rest IS DISTINCT FROM OLD THEN
                RAISE EXCEPTION 'inspection % item %: only the answer can change', OLD.inspection_id, OLD.item_key
                    USING ERRCODE = 'check_violation';
            END IF;
        END IF;

        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER inspection_responses_guard BEFORE INSERT OR UPDATE OR DELETE ON inspection_responses
    FOR EACH ROW EXECUTE FUNCTION inspection_responses_guard();

CREATE FUNCTION inspection_response_evidence_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        parent_id     uuid;
        parent_status text;
    BEGIN
        IF TG_OP = 'UPDATE' THEN
            RAISE EXCEPTION 'a photo link never changes; remove it and add another'
                USING ERRCODE = 'check_violation';
        END IF;
        IF TG_OP = 'DELETE' THEN
            parent_id := OLD.inspection_id;
        ELSE
            parent_id := NEW.inspection_id;
        END IF;
        SELECT status INTO parent_status FROM inspections WHERE id = parent_id FOR SHARE;
        IF FOUND AND parent_status <> 'in_progress' THEN
            RAISE EXCEPTION 'inspection % is completed; start a new inspection to correct it', parent_id
                USING ERRCODE = 'check_violation';
        END IF;
        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER inspection_response_evidence_guard BEFORE INSERT OR UPDATE OR DELETE ON inspection_response_evidence
    FOR EACH ROW EXECUTE FUNCTION inspection_response_evidence_guard();

-- Findings are raised by completion and never change.
CREATE FUNCTION inspection_findings_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        parent_status text;
    BEGIN
        IF TG_OP <> 'INSERT' THEN
            RAISE EXCEPTION 'findings never change'
                USING ERRCODE = 'check_violation';
        END IF;
        SELECT status INTO parent_status FROM inspections WHERE id = NEW.inspection_id;
        IF FOUND AND parent_status <> 'completed' THEN
            RAISE EXCEPTION 'inspection %: findings are raised when it is completed', NEW.inspection_id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER inspection_findings_guard BEFORE INSERT OR UPDATE OR DELETE ON inspection_findings
    FOR EACH ROW EXECUTE FUNCTION inspection_findings_guard();

-- Row triggers don't fire on TRUNCATE.
CREATE FUNCTION inspections_no_truncate() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        RAISE EXCEPTION '% cannot be truncated', TG_TABLE_NAME
            USING ERRCODE = 'check_violation';
    END
    $$;

CREATE TRIGGER inspections_no_truncate BEFORE TRUNCATE ON inspections
    FOR EACH STATEMENT EXECUTE FUNCTION inspections_no_truncate();
CREATE TRIGGER inspection_responses_no_truncate BEFORE TRUNCATE ON inspection_responses
    FOR EACH STATEMENT EXECUTE FUNCTION inspections_no_truncate();
CREATE TRIGGER inspection_response_evidence_no_truncate BEFORE TRUNCATE ON inspection_response_evidence
    FOR EACH STATEMENT EXECUTE FUNCTION inspections_no_truncate();
CREATE TRIGGER inspection_findings_no_truncate BEFORE TRUNCATE ON inspection_findings
    FOR EACH STATEMENT EXECUTE FUNCTION inspections_no_truncate();

ALTER TABLE inspections ENABLE ROW LEVEL SECURITY;
ALTER TABLE inspections FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON inspections FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE inspection_responses ENABLE ROW LEVEL SECURITY;
ALTER TABLE inspection_responses FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON inspection_responses FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE inspection_response_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE inspection_response_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON inspection_response_evidence FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE inspection_findings ENABLE ROW LEVEL SECURITY;
ALTER TABLE inspection_findings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON inspection_findings FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

-- Migration 000008's default privileges grant UPDATE and DELETE on new tables.
-- Column-level UPDATE still allows SELECT ... FOR SHARE, which the guards use.
REVOKE UPDATE, DELETE ON inspections FROM siteproof_app;
GRANT UPDATE (status, completed_at, client_completed_at) ON inspections TO siteproof_app;
REVOKE UPDATE, DELETE ON inspection_responses FROM siteproof_app;
GRANT UPDATE (result, severity, comment, client_answered_at, answered_at) ON inspection_responses TO siteproof_app;
-- DELETE stays, so an answer can replace its photos while in progress.
REVOKE UPDATE ON inspection_response_evidence FROM siteproof_app;
-- INSERT stays: the completion trigger runs as the app.
REVOKE UPDATE, DELETE ON inspection_findings FROM siteproof_app;

COMMIT;
