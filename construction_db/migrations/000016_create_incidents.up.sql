BEGIN;

-- The incident register (Rev 4 §2.6, A.6). An incident is never edited: each
-- submission is a whole new version, and every version stays readable as
-- submitted. There are no drafts on the server; the phone keeps those.
-- Ids are generated on the device so offline sync is idempotent.

-- The header holds only what never changes. latest_version is maintained by
-- the version trigger; it is also what serialises submissions (see
-- incident_versions_advance).
CREATE TABLE incidents (
    id             uuid PRIMARY KEY,
    org_id         uuid NOT NULL,
    site_id        uuid NOT NULL,
    regime_id      uuid NOT NULL REFERENCES regulatory_regimes (id),
    reported_by    uuid NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    latest_version integer NOT NULL DEFAULT 0 CHECK (latest_version >= 0),

    FOREIGN KEY (org_id, site_id) REFERENCES sites (org_id, id),
    FOREIGN KEY (org_id, reported_by) REFERENCES org_members (org_id, user_id),
    UNIQUE (org_id, site_id, id)
);

CREATE INDEX incidents_site_idx ON incidents (site_id, created_at);

-- Taxonomy keys as the regime data restricts them (regimes.keyPattern): one or
-- more, distinct. Whether they exist in the site's regime is checked by the
-- API, which has the regime data.
CREATE FUNCTION incident_codes_valid(codes text[]) RETURNS boolean
    LANGUAGE sql IMMUTABLE
    AS $$
        SELECT cardinality(codes) >= 1
           AND (SELECT bool_and(c IS NOT NULL AND c ~ '^[a-z0-9_]+$') FROM unnest(codes) AS c)
           AND (SELECT count(DISTINCT c) FROM unnest(codes) AS c) = cardinality(codes)
    $$;

-- One submitted account of the incident. Several types per version (one event
-- can fall under several paragraphs of OHSA s24(1)); its deadlines are the
-- union of their rules.
CREATE TABLE incident_versions (
    org_id              uuid NOT NULL,
    site_id             uuid NOT NULL,
    incident_id         uuid NOT NULL,
    version             integer NOT NULL CHECK (version >= 1),
    taxonomy_codes      text[] NOT NULL CHECK (incident_codes_valid(taxonomy_codes)),
    occurred_at         timestamptz NOT NULL,
    -- When the employer learned of it (COIDA s39(1)); required when an
    -- employee of the organisation is injured (checked at commit).
    employer_aware_at   timestamptz,
    location_text       text CHECK (char_length(location_text) BETWEEN 1 AND 500),
    description         text CHECK (char_length(description) BETWEEN 1 AND 5000),
    submitted_by        uuid NOT NULL,
    client_submitted_at timestamptz NOT NULL,
    submitted_at        timestamptz NOT NULL DEFAULT now(),
    device_id           text CHECK (octet_length(device_id) BETWEEN 1 AND 200),
    amend_reason        text CHECK (char_length(amend_reason) BETWEEN 1 AND 500),
    -- How many people and photos the version has. Checked at commit, so rows
    -- added later (in another transaction) are refused.
    people_count        integer NOT NULL CHECK (people_count >= 0),
    evidence_count      integer NOT NULL CHECK (evidence_count >= 0),

    PRIMARY KEY (incident_id, version),
    FOREIGN KEY (org_id, site_id, incident_id) REFERENCES incidents (org_id, site_id, id),
    FOREIGN KEY (org_id, submitted_by) REFERENCES org_members (org_id, user_id),
    UNIQUE (org_id, site_id, incident_id, version),

    CONSTRAINT incident_versions_amend_reason CHECK ((version = 1) = (amend_reason IS NULL)),
    CONSTRAINT incident_versions_aware_after_occurred CHECK (employer_aware_at >= occurred_at)
);

-- The people in one version. Names are optional, so a near miss stays
-- no-blame. A death is an injury.
CREATE TABLE incident_people (
    org_id             uuid NOT NULL,
    site_id            uuid NOT NULL,
    incident_id        uuid NOT NULL,
    version            integer NOT NULL,
    position           integer NOT NULL CHECK (position >= 1),
    name               text CHECK (char_length(name) BETWEEN 1 AND 200),
    occupation         text CHECK (char_length(occupation) BETWEEN 1 AND 200),
    employee_of_org    boolean NOT NULL,
    -- Who employs them, when it isn't the organisation (e.g. a subcontractor).
    employer_name      text CHECK (char_length(employer_name) BETWEEN 1 AND 200),
    injured            boolean NOT NULL,
    injury_description text CHECK (char_length(injury_description) BETWEEN 1 AND 2000),
    body_part          text CHECK (char_length(body_part) BETWEEN 1 AND 200),
    treatment          text CHECK (treatment IN ('none', 'first_aid', 'medical_treatment', 'hospitalised')),
    died_at            timestamptz,

    PRIMARY KEY (incident_id, version, position),
    FOREIGN KEY (org_id, site_id, incident_id, version)
        REFERENCES incident_versions (org_id, site_id, incident_id, version),

    CONSTRAINT incident_people_death_is_injury CHECK (died_at IS NULL OR injured),
    CONSTRAINT incident_people_injury_details CHECK (
        injured OR (injury_description IS NULL AND body_part IS NULL AND treatment IS NULL)),
    CONSTRAINT incident_people_employer CHECK (NOT employee_of_org OR employer_name IS NULL)
);

-- Photos in one version: only verified evidence from the incident's own site
-- (the M7 pattern: two foreign keys sharing site_id, one through the
-- constant evidence_status).
CREATE TABLE incident_version_evidence (
    org_id          uuid NOT NULL,
    site_id         uuid NOT NULL,
    incident_id     uuid NOT NULL,
    version         integer NOT NULL,
    evidence_id     uuid NOT NULL,
    evidence_status text NOT NULL DEFAULT 'verified' CHECK (evidence_status = 'verified'),

    PRIMARY KEY (incident_id, version, evidence_id),
    FOREIGN KEY (org_id, site_id, incident_id, version)
        REFERENCES incident_versions (org_id, site_id, incident_id, version),
    FOREIGN KEY (org_id, site_id, evidence_id, evidence_status)
        REFERENCES evidence (org_id, site_id, id, status)
);

CREATE INDEX incident_version_evidence_evidence_idx ON incident_version_evidence (evidence_id);

-- A witness's own account, taken any time after the incident. Never changed:
-- a correction is a further statement that supersedes it (at most one).
CREATE TABLE witness_statements (
    id              uuid PRIMARY KEY,
    org_id          uuid NOT NULL,
    site_id         uuid NOT NULL,
    incident_id     uuid NOT NULL,
    witness_name    text NOT NULL CHECK (char_length(witness_name) BETWEEN 1 AND 200),
    witness_contact text CHECK (char_length(witness_contact) BETWEEN 1 AND 200),
    statement       text NOT NULL CHECK (char_length(statement) BETWEEN 1 AND 20000),
    taken_by        uuid NOT NULL,
    taken_at        timestamptz NOT NULL,
    recorded_at     timestamptz NOT NULL DEFAULT now(),
    device_id       text CHECK (octet_length(device_id) BETWEEN 1 AND 200),
    -- A photo of the signed statement, if there is one.
    evidence_id     uuid,
    evidence_status text NOT NULL DEFAULT 'verified' CHECK (evidence_status = 'verified'),
    supersedes_id   uuid,

    FOREIGN KEY (org_id, site_id, incident_id) REFERENCES incidents (org_id, site_id, id),
    FOREIGN KEY (org_id, taken_by) REFERENCES org_members (org_id, user_id),
    -- MATCH SIMPLE: no photo (evidence_id null) skips the check.
    FOREIGN KEY (org_id, site_id, evidence_id, evidence_status) REFERENCES evidence (org_id, site_id, id, status),
    UNIQUE (org_id, site_id, incident_id, id),
    CONSTRAINT witness_statements_supersedes_once UNIQUE (supersedes_id),
    CONSTRAINT witness_statements_not_self CHECK (supersedes_id <> id)
);

-- Same incident only.
ALTER TABLE witness_statements ADD CONSTRAINT witness_statements_supersedes_fkey
    FOREIGN KEY (org_id, site_id, incident_id, supersedes_id) REFERENCES witness_statements (org_id, site_id, incident_id, id);

CREATE INDEX witness_statements_incident_idx ON witness_statements (incident_id, recorded_at);

-- Every guard below applies to every role, the table owner included. Each
-- raises check_violation with its own message.

-- The header never changes, except latest_version advancing by one to a
-- version that exists (incident_versions_advance does that). A no-op update is
-- allowed. Nothing is ever deleted.
CREATE FUNCTION incidents_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        rest incidents;
    BEGIN
        IF TG_OP = 'DELETE' THEN
            RAISE EXCEPTION 'incident %: incidents are never deleted', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW IS NOT DISTINCT FROM OLD THEN
            RETURN NEW;
        END IF;
        rest := NEW;
        rest.latest_version := OLD.latest_version;
        IF rest IS DISTINCT FROM OLD
           OR NEW.latest_version <> OLD.latest_version + 1
           OR NOT EXISTS (SELECT 1 FROM incident_versions WHERE incident_id = OLD.id AND version = NEW.latest_version) THEN
            RAISE EXCEPTION 'incident %: an incident changes only by a new version', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER incidents_guard BEFORE UPDATE OR DELETE ON incidents
    FOR EACH ROW EXECUTE FUNCTION incidents_guard();

CREATE FUNCTION incidents_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        NEW.created_at := now();
        IF NEW.latest_version <> 0 THEN
            RAISE EXCEPTION 'incident %: a new incident has no versions yet; submit version 1', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER incidents_insert BEFORE INSERT ON incidents
    FOR EACH ROW EXECUTE FUNCTION incidents_insert();

-- At commit an incident has at least one version: it is created by
-- submitting version 1 in the same transaction.
CREATE FUNCTION incidents_has_version() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF NOT EXISTS (SELECT 1 FROM incident_versions WHERE incident_id = NEW.id) THEN
            RAISE EXCEPTION 'incident %: has no version; an incident is created by submitting version 1', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

CREATE CONSTRAINT TRIGGER incidents_has_version AFTER INSERT ON incidents
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION incidents_has_version();

-- A version's own checks. submitted_at is the server's time, whatever the
-- client sent. The incident can't be dated in the future (5 minutes for clock
-- skew, as for inspection answers), nor after its site was archived.
CREATE FUNCTION incident_versions_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        s sites;
    BEGIN
        NEW.submitted_at := now();
        IF NEW.occurred_at > NEW.submitted_at + interval '5 minutes' THEN
            RAISE EXCEPTION 'incident % version %: occurred_at is in the future', NEW.incident_id, NEW.version
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.employer_aware_at > NEW.submitted_at + interval '5 minutes' THEN
            RAISE EXCEPTION 'incident % version %: employer_aware_at is in the future', NEW.incident_id, NEW.version
                USING ERRCODE = 'check_violation';
        END IF;
        SELECT * INTO s FROM sites WHERE id = NEW.site_id;
        IF FOUND AND s.status = 'archived' AND NEW.occurred_at > s.archived_at THEN
            RAISE EXCEPTION 'incident % version %: occurred after its site was archived', NEW.incident_id, NEW.version
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER incident_versions_insert BEFORE INSERT ON incident_versions
    FOR EACH ROW EXECUTE FUNCTION incident_versions_insert();

-- Versions are numbered 1, 2, 3, ... with no gaps. The UPDATE takes the
-- incident's row lock, so two submissions of the same number queue: the
-- second re-reads the row after the first commits, finds latest_version
-- already advanced, updates nothing and fails here. The lock is held to
-- commit, so everything else the submission writes (its deadlines) is
-- serialised per incident too.
CREATE FUNCTION incident_versions_advance() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        UPDATE incidents SET latest_version = NEW.version
        WHERE id = NEW.incident_id AND latest_version = NEW.version - 1;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'incident %: version % is not the next version', NEW.incident_id, NEW.version
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

CREATE TRIGGER incident_versions_advance AFTER INSERT ON incident_versions
    FOR EACH ROW EXECUTE FUNCTION incident_versions_advance();

-- At commit a version is complete: exactly people_count people and
-- evidence_count photos, and employer_aware_at when an employee of the
-- organisation is injured. Fired by inserts into all three tables, so a row
-- added to a committed version breaks its count and is refused.
CREATE FUNCTION incident_version_complete() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        v       incident_versions;
        people  integer;
        photos  integer;
    BEGIN
        SELECT * INTO v FROM incident_versions WHERE incident_id = NEW.incident_id AND version = NEW.version;
        IF NOT FOUND THEN
            RETURN NULL; -- the foreign key reports it
        END IF;
        SELECT count(*) INTO people FROM incident_people WHERE incident_id = v.incident_id AND version = v.version;
        IF people <> v.people_count THEN
            RAISE EXCEPTION 'incident % version %: has % people but people_count is %', v.incident_id, v.version, people, v.people_count
                USING ERRCODE = 'check_violation';
        END IF;
        SELECT count(*) INTO photos FROM incident_version_evidence WHERE incident_id = v.incident_id AND version = v.version;
        IF photos <> v.evidence_count THEN
            RAISE EXCEPTION 'incident % version %: has % photos but evidence_count is %', v.incident_id, v.version, photos, v.evidence_count
                USING ERRCODE = 'check_violation';
        END IF;
        IF v.employer_aware_at IS NULL AND EXISTS (
            SELECT 1 FROM incident_people
            WHERE incident_id = v.incident_id AND version = v.version AND employee_of_org AND injured) THEN
            RAISE EXCEPTION 'incident % version %: an injured employee needs employer_aware_at', v.incident_id, v.version
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

CREATE CONSTRAINT TRIGGER incident_versions_complete AFTER INSERT ON incident_versions
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION incident_version_complete();
CREATE CONSTRAINT TRIGGER incident_people_complete AFTER INSERT ON incident_people
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION incident_version_complete();
CREATE CONSTRAINT TRIGGER incident_version_evidence_complete AFTER INSERT ON incident_version_evidence
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION incident_version_complete();

-- A death can't come before the incident, nor in the future.
CREATE FUNCTION incident_people_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        occurred timestamptz;
    BEGIN
        IF NEW.died_at IS NOT NULL THEN
            SELECT occurred_at INTO occurred FROM incident_versions WHERE incident_id = NEW.incident_id AND version = NEW.version;
            IF FOUND AND NEW.died_at < occurred THEN
                RAISE EXCEPTION 'incident % version %: died_at is before the incident', NEW.incident_id, NEW.version
                    USING ERRCODE = 'check_violation';
            END IF;
            IF NEW.died_at > now() + interval '5 minutes' THEN
                RAISE EXCEPTION 'incident % version %: died_at is in the future', NEW.incident_id, NEW.version
                    USING ERRCODE = 'check_violation';
            END IF;
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER incident_people_insert BEFORE INSERT ON incident_people
    FOR EACH ROW EXECUTE FUNCTION incident_people_insert();

-- A statement can't be taken in the future. recorded_at is the server's time.
CREATE FUNCTION witness_statements_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        NEW.recorded_at := now();
        IF NEW.taken_at > NEW.recorded_at + interval '5 minutes' THEN
            RAISE EXCEPTION 'witness statement %: taken_at is in the future', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER witness_statements_insert BEFORE INSERT ON witness_statements
    FOR EACH ROW EXECUTE FUNCTION witness_statements_insert();

-- Versions, their people and photos, and statements are append-only.
CREATE FUNCTION incidents_append_only() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        RAISE EXCEPTION '% is append-only; submit a new version or statement instead', TG_TABLE_NAME
            USING ERRCODE = 'check_violation';
    END
    $$;

CREATE TRIGGER incident_versions_append_only BEFORE UPDATE OR DELETE ON incident_versions
    FOR EACH ROW EXECUTE FUNCTION incidents_append_only();
CREATE TRIGGER incident_people_append_only BEFORE UPDATE OR DELETE ON incident_people
    FOR EACH ROW EXECUTE FUNCTION incidents_append_only();
CREATE TRIGGER incident_version_evidence_append_only BEFORE UPDATE OR DELETE ON incident_version_evidence
    FOR EACH ROW EXECUTE FUNCTION incidents_append_only();
CREATE TRIGGER witness_statements_append_only BEFORE UPDATE OR DELETE ON witness_statements
    FOR EACH ROW EXECUTE FUNCTION incidents_append_only();

-- Row triggers don't fire on TRUNCATE.
CREATE TRIGGER incidents_no_truncate BEFORE TRUNCATE ON incidents
    FOR EACH STATEMENT EXECUTE FUNCTION incidents_append_only();
CREATE TRIGGER incident_versions_no_truncate BEFORE TRUNCATE ON incident_versions
    FOR EACH STATEMENT EXECUTE FUNCTION incidents_append_only();
CREATE TRIGGER incident_people_no_truncate BEFORE TRUNCATE ON incident_people
    FOR EACH STATEMENT EXECUTE FUNCTION incidents_append_only();
CREATE TRIGGER incident_version_evidence_no_truncate BEFORE TRUNCATE ON incident_version_evidence
    FOR EACH STATEMENT EXECUTE FUNCTION incidents_append_only();
CREATE TRIGGER witness_statements_no_truncate BEFORE TRUNCATE ON witness_statements
    FOR EACH STATEMENT EXECUTE FUNCTION incidents_append_only();

ALTER TABLE incidents ENABLE ROW LEVEL SECURITY;
ALTER TABLE incidents FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON incidents FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE incident_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE incident_versions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON incident_versions FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE incident_people ENABLE ROW LEVEL SECURITY;
ALTER TABLE incident_people FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON incident_people FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE incident_version_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE incident_version_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON incident_version_evidence FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE witness_statements ENABLE ROW LEVEL SECURITY;
ALTER TABLE witness_statements FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON witness_statements FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

-- Migration 000008's default privileges grant UPDATE and DELETE on new tables.
-- The version trigger runs as the app, so it needs UPDATE on latest_version;
-- incidents_guard allows nothing else.
REVOKE UPDATE, DELETE ON incidents FROM siteproof_app;
GRANT UPDATE (latest_version) ON incidents TO siteproof_app;
REVOKE UPDATE, DELETE ON incident_versions FROM siteproof_app;
REVOKE UPDATE, DELETE ON incident_people FROM siteproof_app;
REVOKE UPDATE, DELETE ON incident_version_evidence FROM siteproof_app;
REVOKE UPDATE, DELETE ON witness_statements FROM siteproof_app;

COMMIT;
