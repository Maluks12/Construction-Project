BEGIN;

-- Toolbox talks with attendance (Rev 3 §2.3.4, Rev 4 A.5). Decisions D1–D3
-- and D10 (16 Nov) are in construction_db/CLAUDE.md. Four tables:
--   - toolbox_talks: what was talked about, by whom and when. It can be
--     edited until the first signature; after that only its end time can be
--     set, once.
--   - toolbox_talk_evidence: photos (verified, same site), append-only. A
--     photo of a signed paper register has kind 'paper_register'.
--   - toolbox_attendance: one row per person who signed, append-only.
--   - toolbox_attendance_retractions: at most one per signature, with a reason.
-- Ids are generated on the device so offline sync is idempotent.

CREATE TABLE toolbox_talks (
    id             uuid PRIMARY KEY,
    org_id         uuid NOT NULL,
    site_id        uuid NOT NULL,
    -- A key of the site regime's toolbox_topics, or null for a free-text
    -- topic. The API checks it against the regime; the database can't see
    -- regime data.
    topic_key      text CHECK (topic_key ~ '^[a-z0-9_]+$'),
    topic          text NOT NULL CHECK (char_length(btrim(topic)) BETWEEN 1 AND 200),
    presenter_name text NOT NULL CHECK (char_length(btrim(presenter_name)) BETWEEN 1 AND 200),
    notes          text CHECK (char_length(notes) BETWEEN 1 AND 4000),
    started_at     timestamptz NOT NULL,
    ended_at       timestamptz,
    -- The site regime's zone, copied at creation. talk_date is started_at's
    -- local date in it (set by the triggers): the diary's day.
    time_zone      text NOT NULL,
    talk_date      date NOT NULL,
    recorded_by    uuid NOT NULL,
    device_id      text CHECK (octet_length(device_id) BETWEEN 1 AND 200),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz,

    FOREIGN KEY (org_id, site_id) REFERENCES sites (org_id, id),
    FOREIGN KEY (org_id, recorded_by) REFERENCES org_members (org_id, user_id),
    UNIQUE (org_id, site_id, id),
    CONSTRAINT toolbox_talks_ends_after_start CHECK (ended_at > started_at)
);

CREATE INDEX toolbox_talks_site_date_idx ON toolbox_talks (site_id, talk_date);

CREATE TABLE toolbox_talk_evidence (
    org_id          uuid NOT NULL,
    site_id         uuid NOT NULL,
    talk_id         uuid NOT NULL,
    evidence_id     uuid NOT NULL,
    evidence_status text NOT NULL DEFAULT 'verified' CHECK (evidence_status = 'verified'),
    kind            text NOT NULL CHECK (kind IN ('photo', 'paper_register')),
    added_by        uuid NOT NULL,
    added_at        timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (talk_id, evidence_id),
    FOREIGN KEY (org_id, site_id, talk_id) REFERENCES toolbox_talks (org_id, site_id, id),
    -- Verified evidence from the talk's own site only (as 000012).
    FOREIGN KEY (org_id, site_id, evidence_id, evidence_status) REFERENCES evidence (org_id, site_id, id, status),
    FOREIGN KEY (org_id, added_by) REFERENCES org_members (org_id, user_id)
);

CREATE INDEX toolbox_talk_evidence_evidence_idx ON toolbox_talk_evidence (evidence_id);

-- A name as written at the talk (D1). M11 links a row to a worker through
-- its own append-only table; this row never changes.
CREATE TABLE toolbox_attendance (
    id               uuid PRIMARY KEY,
    org_id           uuid NOT NULL,
    site_id          uuid NOT NULL,
    talk_id          uuid NOT NULL,
    name             text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 200),
    firm             text CHECK (char_length(btrim(firm)) BETWEEN 1 AND 200),
    clock_number     text CHECK (char_length(btrim(clock_number)) BETWEEN 1 AND 50),
    -- Who this is, as far as a name and clock number can say: case and spacing
    -- don't count. "The same person twice" and the diary's headcount use it.
    identity_key     text GENERATED ALWAYS AS (
                         lower(regexp_replace(btrim(name), '\s+', ' ', 'g')) || '|' || coalesce(lower(btrim(clock_number)), '')
                     ) STORED,
    -- tap: the worker tapped their name on the recorder's phone (D2).
    -- paper: transcribed from a photographed signed register.
    method           text NOT NULL CHECK (method IN ('tap', 'paper')),
    recorded_by      uuid NOT NULL,
    client_signed_at timestamptz NOT NULL,
    signed_at        timestamptz NOT NULL DEFAULT now(),
    device_id        text CHECK (octet_length(device_id) BETWEEN 1 AND 200),

    FOREIGN KEY (org_id, site_id, talk_id) REFERENCES toolbox_talks (org_id, site_id, id),
    FOREIGN KEY (org_id, recorded_by) REFERENCES org_members (org_id, user_id),
    UNIQUE (org_id, site_id, id),
    CONSTRAINT toolbox_attendance_once UNIQUE (talk_id, identity_key)
);

CREATE TABLE toolbox_attendance_retractions (
    attendance_id       uuid PRIMARY KEY,
    org_id              uuid NOT NULL,
    site_id             uuid NOT NULL,
    reason              text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 1 AND 500),
    retracted_by        uuid NOT NULL,
    client_retracted_at timestamptz NOT NULL,
    retracted_at        timestamptz NOT NULL DEFAULT now(),

    FOREIGN KEY (org_id, site_id, attendance_id) REFERENCES toolbox_attendance (org_id, site_id, id),
    FOREIGN KEY (org_id, retracted_by) REFERENCES org_members (org_id, user_id)
);

-- Every guard below applies to every role, the table owner included. Each
-- raises check_violation with its own message.

-- Times from a device may run up to 5 minutes ahead of the server (clock
-- skew), as for inspection answers.
CREATE FUNCTION toolbox_talks_times_check(t toolbox_talks) RETURNS void
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF t.started_at > now() + interval '5 minutes' THEN
            RAISE EXCEPTION 'toolbox talk %: started_at is in the future', t.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF t.ended_at > now() + interval '5 minutes' THEN
            RAISE EXCEPTION 'toolbox talk %: ended_at is in the future', t.id
                USING ERRCODE = 'check_violation';
        END IF;
    END
    $$;

-- BEFORE triggers set the server's fields and check what the row says about
-- itself. Who may write, and the site's state, are checked AFTER the insert:
-- RLS's WITH CHECK runs after BEFORE triggers, and another organisation's
-- members and sites are invisible, so checking them first would answer a
-- cross-tenant insert with "can't see its site" instead of the policy error.

CREATE FUNCTION toolbox_talks_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        NEW.created_at := now();
        NEW.updated_at := NULL;
        NEW.talk_date := action_local_date(NEW.started_at, NEW.time_zone);
        PERFORM toolbox_talks_times_check(NEW);
        RETURN NEW;
    END
    $$;

CREATE TRIGGER toolbox_talks_insert BEFORE INSERT ON toolbox_talks
    FOR EACH ROW EXECUTE FUNCTION toolbox_talks_insert();

CREATE FUNCTION toolbox_talks_check() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        s sites;
    BEGIN
        IF NOT action_site_member(NEW.org_id, NEW.site_id, NEW.recorded_by) THEN
            RAISE EXCEPTION 'toolbox talk %: recorded_by can''t see its site', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        SELECT * INTO s FROM sites WHERE id = NEW.site_id;
        IF FOUND AND s.status = 'archived' AND NEW.started_at > s.archived_at THEN
            RAISE EXCEPTION 'toolbox talk %: started after its site was archived', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

CREATE TRIGGER toolbox_talks_check AFTER INSERT ON toolbox_talks
    FOR EACH ROW EXECUTE FUNCTION toolbox_talks_check();

-- Before the first signature anything but the talk's identity can change.
-- After it, the people who signed signed for this topic at this time, so
-- only ended_at may be set, once. A signature holds the talk FOR SHARE until
-- it commits, so an edit waits for it and then sees it here.
CREATE FUNCTION toolbox_talks_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF TG_OP = 'DELETE' THEN
            RAISE EXCEPTION 'toolbox talk %: talks are never deleted', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF (NEW.id, NEW.org_id, NEW.site_id, NEW.time_zone, NEW.recorded_by, NEW.device_id, NEW.created_at)
           IS DISTINCT FROM (OLD.id, OLD.org_id, OLD.site_id, OLD.time_zone, OLD.recorded_by, OLD.device_id, OLD.created_at) THEN
            RAISE EXCEPTION 'toolbox talk %: only its topic, presenter, notes and times can change', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF EXISTS (SELECT 1 FROM toolbox_attendance WHERE talk_id = OLD.id)
           AND ((NEW.topic_key, NEW.topic, NEW.presenter_name, NEW.notes, NEW.started_at)
                IS DISTINCT FROM (OLD.topic_key, OLD.topic, OLD.presenter_name, OLD.notes, OLD.started_at)
                OR (OLD.ended_at IS NOT NULL AND NEW.ended_at IS DISTINCT FROM OLD.ended_at)) THEN
            RAISE EXCEPTION 'toolbox talk %: people have signed it, so only its end time can be set, once', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        NEW.talk_date := action_local_date(NEW.started_at, NEW.time_zone);
        IF NEW IS DISTINCT FROM OLD THEN
            PERFORM toolbox_talks_times_check(NEW);
            NEW.updated_at := now();
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER toolbox_talks_guard BEFORE UPDATE OR DELETE ON toolbox_talks
    FOR EACH ROW EXECUTE FUNCTION toolbox_talks_guard();

CREATE FUNCTION toolbox_talk_evidence_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        NEW.added_at := now();
        RETURN NEW;
    END
    $$;

CREATE TRIGGER toolbox_talk_evidence_insert BEFORE INSERT ON toolbox_talk_evidence
    FOR EACH ROW EXECUTE FUNCTION toolbox_talk_evidence_insert();

CREATE FUNCTION toolbox_talk_evidence_check() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF NOT action_site_member(NEW.org_id, NEW.site_id, NEW.added_by) THEN
            RAISE EXCEPTION 'toolbox talk %: added_by can''t see its site', NEW.talk_id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

CREATE TRIGGER toolbox_talk_evidence_check AFTER INSERT ON toolbox_talk_evidence
    FOR EACH ROW EXECUTE FUNCTION toolbox_talk_evidence_check();

-- A signature is on the talk's local date, not before it started and not in
-- the future. The talk is read FOR SHARE: an open header edit makes the
-- signature wait, then the signature checks the edited talk. (Another
-- organisation's talk is invisible here; the policy refuses the row.)
CREATE FUNCTION toolbox_attendance_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        talk toolbox_talks;
    BEGIN
        NEW.signed_at := now();
        IF NEW.client_signed_at > NEW.signed_at + interval '5 minutes' THEN
            RAISE EXCEPTION 'attendance %: client_signed_at is in the future; check the device clock', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        SELECT * INTO talk FROM toolbox_talks WHERE id = NEW.talk_id FOR SHARE;
        IF NOT FOUND THEN
            RETURN NEW; -- the foreign key or the policy reports it
        END IF;
        IF NEW.client_signed_at < talk.started_at THEN
            RAISE EXCEPTION 'attendance %: signed before the talk started', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF action_local_date(NEW.client_signed_at, talk.time_zone) <> talk.talk_date THEN
            RAISE EXCEPTION 'attendance %: signed on another day than the talk', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER toolbox_attendance_insert BEFORE INSERT ON toolbox_attendance
    FOR EACH ROW EXECUTE FUNCTION toolbox_attendance_insert();

CREATE FUNCTION toolbox_attendance_check() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF NOT action_site_member(NEW.org_id, NEW.site_id, NEW.recorded_by) THEN
            RAISE EXCEPTION 'attendance %: recorded_by can''t see its site', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

CREATE TRIGGER toolbox_attendance_check AFTER INSERT ON toolbox_attendance
    FOR EACH ROW EXECUTE FUNCTION toolbox_attendance_check();

-- Paper attendance rests on a photo of the signed register (D2). Checked at
-- commit, so the photo and the names can be written in either order. Photos
-- are append-only, so the rule can't be broken afterwards.
CREATE FUNCTION toolbox_attendance_paper_register() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF NEW.method = 'paper' AND NOT EXISTS (
            SELECT 1 FROM toolbox_talk_evidence WHERE talk_id = NEW.talk_id AND kind = 'paper_register') THEN
            RAISE EXCEPTION 'toolbox talk %: paper attendance needs a photo of the signed register', NEW.talk_id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

CREATE CONSTRAINT TRIGGER toolbox_attendance_paper_register AFTER INSERT ON toolbox_attendance
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION toolbox_attendance_paper_register();

CREATE FUNCTION toolbox_attendance_retractions_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        NEW.retracted_at := now();
        IF NEW.client_retracted_at > NEW.retracted_at + interval '5 minutes' THEN
            RAISE EXCEPTION 'retraction of attendance %: client_retracted_at is in the future; check the device clock', NEW.attendance_id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER toolbox_attendance_retractions_insert BEFORE INSERT ON toolbox_attendance_retractions
    FOR EACH ROW EXECUTE FUNCTION toolbox_attendance_retractions_insert();

-- A retraction: by the person who recorded the signature, or a lead, who can
-- see the site.
CREATE FUNCTION toolbox_attendance_retractions_check() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        recorder uuid;
    BEGIN
        SELECT recorded_by INTO recorder FROM toolbox_attendance WHERE id = NEW.attendance_id;
        IF NOT action_site_member(NEW.org_id, NEW.site_id, NEW.retracted_by)
           OR (NEW.retracted_by IS DISTINCT FROM recorder AND NOT action_lead(NEW.org_id, NEW.retracted_by)) THEN
            RAISE EXCEPTION 'retraction of attendance %: only its recorder or a lead on the site may retract it', NEW.attendance_id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

CREATE TRIGGER toolbox_attendance_retractions_check AFTER INSERT ON toolbox_attendance_retractions
    FOR EACH ROW EXECUTE FUNCTION toolbox_attendance_retractions_check();

CREATE FUNCTION toolbox_append_only() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        RAISE EXCEPTION '% is append-only', TG_TABLE_NAME
            USING ERRCODE = 'check_violation';
    END
    $$;

CREATE TRIGGER toolbox_talk_evidence_append_only BEFORE UPDATE OR DELETE ON toolbox_talk_evidence
    FOR EACH ROW EXECUTE FUNCTION toolbox_append_only();
CREATE TRIGGER toolbox_attendance_append_only BEFORE UPDATE OR DELETE ON toolbox_attendance
    FOR EACH ROW EXECUTE FUNCTION toolbox_append_only();
CREATE TRIGGER toolbox_attendance_retractions_append_only BEFORE UPDATE OR DELETE ON toolbox_attendance_retractions
    FOR EACH ROW EXECUTE FUNCTION toolbox_append_only();

-- Row triggers don't fire on TRUNCATE.
CREATE TRIGGER toolbox_talks_no_truncate BEFORE TRUNCATE ON toolbox_talks
    FOR EACH STATEMENT EXECUTE FUNCTION toolbox_append_only();
CREATE TRIGGER toolbox_talk_evidence_no_truncate BEFORE TRUNCATE ON toolbox_talk_evidence
    FOR EACH STATEMENT EXECUTE FUNCTION toolbox_append_only();
CREATE TRIGGER toolbox_attendance_no_truncate BEFORE TRUNCATE ON toolbox_attendance
    FOR EACH STATEMENT EXECUTE FUNCTION toolbox_append_only();
CREATE TRIGGER toolbox_attendance_retractions_no_truncate BEFORE TRUNCATE ON toolbox_attendance_retractions
    FOR EACH STATEMENT EXECUTE FUNCTION toolbox_append_only();

-- The diary's suggested headcount: distinct people who signed, and weren't
-- retracted, in the site's talks on a local day.
CREATE FUNCTION toolbox_headcount(p_site uuid, p_day date) RETURNS integer
    LANGUAGE sql STABLE
    AS $$
        SELECT count(DISTINCT a.identity_key)::integer
        FROM toolbox_attendance a
        JOIN toolbox_talks t ON t.id = a.talk_id
        WHERE t.site_id = p_site AND t.talk_date = p_day
          AND NOT EXISTS (SELECT 1 FROM toolbox_attendance_retractions r WHERE r.attendance_id = a.id)
    $$;

ALTER TABLE toolbox_talks ENABLE ROW LEVEL SECURITY;
ALTER TABLE toolbox_talks FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON toolbox_talks FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE toolbox_talk_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE toolbox_talk_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON toolbox_talk_evidence FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE toolbox_attendance ENABLE ROW LEVEL SECURITY;
ALTER TABLE toolbox_attendance FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON toolbox_attendance FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE toolbox_attendance_retractions ENABLE ROW LEVEL SECURITY;
ALTER TABLE toolbox_attendance_retractions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON toolbox_attendance_retractions FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

-- Migration 000008's default privileges grant UPDATE and DELETE on new tables.
-- The guard limits what may change once people have signed.
REVOKE UPDATE, DELETE ON toolbox_talks FROM siteproof_app;
GRANT UPDATE (topic_key, topic, presenter_name, notes, started_at, ended_at) ON toolbox_talks TO siteproof_app;
REVOKE UPDATE, DELETE ON toolbox_talk_evidence FROM siteproof_app;
REVOKE UPDATE, DELETE ON toolbox_attendance FROM siteproof_app;
REVOKE UPDATE, DELETE ON toolbox_attendance_retractions FROM siteproof_app;

COMMIT;
