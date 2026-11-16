BEGIN;

-- The daily site diary (Rev 4 §3.9, Construction edition). Decisions D4–D10
-- (16 Nov) are in construction_db/CLAUDE.md. Three tables:
--   - site_diaries: one per site per local day, created by its first
--     revision. Its head (the current revision), conflict and late flag are
--     always what site_diary_replay works out from the revisions.
--   - site_diary_revisions: append-only. Each save restates the whole diary.
--   - site_diary_revision_evidence: a revision's photos, frozen with it.
-- Addenda after closing are in 000022. Ids are generated on the device.

-- A save made on the device before midnight still counts as the original if
-- it reaches the server within this long of midnight (D6).
-- diary.GraceAfterClose mirrors it; a test compares them.
CREATE FUNCTION site_diary_grace() RETURNS interval
    LANGUAGE sql IMMUTABLE
    AS $$ SELECT interval '48 hours' $$;

-- The optional sections a revision or an addendum may carry. Their contents
-- are checked by the API (light rigour); the keys are checked here.
CREATE FUNCTION site_diary_sections_ok(p jsonb) RETURNS boolean
    LANGUAGE sql IMMUTABLE STRICT
    AS $$
        SELECT jsonb_typeof(p) = 'object'
           AND NOT EXISTS (
               SELECT 1 FROM jsonb_object_keys(p) k
               WHERE k NOT IN ('weather_times', 'labour', 'plant', 'instructions', 'delays', 'visitors', 'deliveries'))
    $$;

CREATE TABLE site_diaries (
    org_id                uuid NOT NULL,
    site_id               uuid NOT NULL,
    diary_date            date NOT NULL,
    -- The site regime's zone, copied at creation; the day runs from opens_at
    -- to closes_at (the next local midnight), both set by the trigger.
    time_zone             text NOT NULL,
    opens_at              timestamptz NOT NULL,
    closes_at             timestamptz NOT NULL,
    created_by            uuid NOT NULL,
    created_at            timestamptz NOT NULL DEFAULT now(),
    -- Changed only by a revision's trigger, to site_diary_replay's result.
    revision_count        integer NOT NULL DEFAULT 0,
    head_revision_id      uuid,
    conflict_revision_ids uuid[] NOT NULL DEFAULT '{}',
    late                  boolean NOT NULL DEFAULT false,

    PRIMARY KEY (site_id, diary_date),
    UNIQUE (org_id, site_id, diary_date),
    FOREIGN KEY (org_id, site_id) REFERENCES sites (org_id, id),
    FOREIGN KEY (org_id, created_by) REFERENCES org_members (org_id, user_id)
);

-- The minimum (weather, headcount, work done) is columns; everything else is
-- optional and sits in sections, with stable device-chosen row ids that
-- JBCC and the PC tier will link to (D8, D9).
CREATE TABLE site_diary_revisions (
    id                   uuid PRIMARY KEY,
    org_id               uuid NOT NULL,
    site_id              uuid NOT NULL,
    diary_date           date NOT NULL,
    -- Arrival order on the server, 1, 2, 3 … (set by the trigger).
    seq                  integer NOT NULL,
    -- The revision the phone last saw; null for one written from scratch.
    base_revision_id     uuid,
    weather_conditions   text[] NOT NULL CHECK (
                             cardinality(weather_conditions) BETWEEN 1 AND 9
                             AND weather_conditions <@ ARRAY['fine', 'cloudy', 'rain', 'heavy_rain', 'thunderstorm', 'wind', 'hot', 'cold', 'mist']),
    weather_note         text CHECK (char_length(btrim(weather_note)) BETWEEN 1 AND 500),
    headcount            integer NOT NULL CHECK (headcount BETWEEN 0 AND 10000),
    -- toolbox_headcount for the day when the server received it.
    headcount_suggested  integer NOT NULL DEFAULT 0,
    work_done            text NOT NULL CHECK (char_length(btrim(work_done)) BETWEEN 1 AND 4000),
    sections             jsonb NOT NULL DEFAULT '{}' CHECK (site_diary_sections_ok(sections)),
    evidence_count       integer NOT NULL DEFAULT 0 CHECK (evidence_count BETWEEN 0 AND 50),
    saved_by             uuid NOT NULL,
    client_saved_at      timestamptz NOT NULL,
    server_received_at   timestamptz NOT NULL DEFAULT now(),
    saved_after_close    boolean NOT NULL DEFAULT false,
    received_after_close boolean NOT NULL DEFAULT false,
    late_reason          text CHECK (char_length(btrim(late_reason)) BETWEEN 1 AND 500),
    device_id            text CHECK (octet_length(device_id) BETWEEN 1 AND 200),

    FOREIGN KEY (org_id, site_id, diary_date) REFERENCES site_diaries (org_id, site_id, diary_date),
    FOREIGN KEY (org_id, saved_by) REFERENCES org_members (org_id, user_id),
    UNIQUE (org_id, site_id, diary_date, id),
    UNIQUE (site_id, diary_date, seq),
    -- A late diary says why it was written after the day (D6).
    CONSTRAINT site_diary_revisions_late_reason CHECK ((late_reason IS NOT NULL) = saved_after_close)
);

-- The base is a revision of the same diary.
ALTER TABLE site_diary_revisions ADD CONSTRAINT site_diary_revisions_base_fkey
    FOREIGN KEY (org_id, site_id, diary_date, base_revision_id) REFERENCES site_diary_revisions (org_id, site_id, diary_date, id);

-- The header's revisions are the diary's own.
ALTER TABLE site_diaries ADD CONSTRAINT site_diaries_head_fkey
    FOREIGN KEY (org_id, site_id, diary_date, head_revision_id) REFERENCES site_diary_revisions (org_id, site_id, diary_date, id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE site_diary_revision_evidence (
    org_id          uuid NOT NULL,
    site_id         uuid NOT NULL,
    diary_date      date NOT NULL,
    revision_id     uuid NOT NULL,
    evidence_id     uuid NOT NULL,
    evidence_status text NOT NULL DEFAULT 'verified' CHECK (evidence_status = 'verified'),

    PRIMARY KEY (revision_id, evidence_id),
    FOREIGN KEY (org_id, site_id, diary_date, revision_id) REFERENCES site_diary_revisions (org_id, site_id, diary_date, id),
    -- Verified evidence from the diary's own site only (as 000012).
    FOREIGN KEY (org_id, site_id, evidence_id, evidence_status) REFERENCES evidence (org_id, site_id, id, status)
);

CREATE INDEX site_diary_revision_evidence_evidence_idx ON site_diary_revision_evidence (evidence_id);

-- Every guard below applies to every role, the table owner included. Each
-- raises check_violation with its own message. As in 000020, BEFORE triggers
-- check the row itself; who may write, and the site, are checked AFTER the
-- insert, so RLS answers a cross-tenant row first.

-- What the revisions say the diary is (D5, D6), replayed in arrival order:
--   - the first revision is the head; the diary is late if it was saved
--     after the day closed;
--   - a revision based on the head and newer than it (device time) becomes
--     the head and clears any conflict: whoever saved it saw the newest;
--   - any other revision is kept and joins the conflict (with the head it
--     didn't see, if the conflict is new), and becomes the head if it is the
--     newest. That includes one based on the head but older by device time,
--     so a skewed clock can't make a save vanish silently.
CREATE FUNCTION site_diary_replay(p_site uuid, p_date date,
                                  OUT head uuid, OUT conflicts uuid[], OUT late boolean, OUT revisions integer)
    LANGUAGE plpgsql STABLE
    AS $$
    DECLARE
        r       record;
        head_at timestamptz;
    BEGIN
        conflicts := '{}';
        late := false;
        revisions := 0;
        FOR r IN SELECT id, base_revision_id, client_saved_at, saved_after_close
                 FROM site_diary_revisions WHERE site_id = p_site AND diary_date = p_date ORDER BY seq LOOP
            revisions := revisions + 1;
            IF head IS NULL THEN
                head := r.id;
                head_at := r.client_saved_at;
                late := r.saved_after_close;
            ELSIF r.base_revision_id = head AND r.client_saved_at > head_at THEN
                head := r.id;
                head_at := r.client_saved_at;
                conflicts := '{}';
            ELSE
                IF cardinality(conflicts) = 0 THEN
                    conflicts := ARRAY[head];
                END IF;
                conflicts := conflicts || r.id;
                IF r.client_saved_at > head_at THEN
                    head := r.id;
                    head_at := r.client_saved_at;
                END IF;
            END IF;
        END LOOP;
    END
    $$;

CREATE FUNCTION site_diaries_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        NEW.created_at := now();
        NEW.opens_at := NEW.diary_date::timestamp AT TIME ZONE NEW.time_zone;
        NEW.closes_at := action_due_at(NEW.diary_date, NEW.time_zone);
        NEW.revision_count := 0;
        NEW.head_revision_id := NULL;
        NEW.conflict_revision_ids := '{}';
        NEW.late := false;
        IF NEW.opens_at > now() + interval '5 minutes' THEN
            RAISE EXCEPTION 'site diary %: the day hasn''t begun', NEW.diary_date
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER site_diaries_insert BEFORE INSERT ON site_diaries
    FOR EACH ROW EXECUTE FUNCTION site_diaries_insert();

CREATE FUNCTION site_diaries_check() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        s sites;
    BEGIN
        IF NOT action_site_member(NEW.org_id, NEW.site_id, NEW.created_by) THEN
            RAISE EXCEPTION 'site diary %: created_by can''t see its site', NEW.diary_date
                USING ERRCODE = 'check_violation';
        END IF;
        SELECT * INTO s FROM sites WHERE id = NEW.site_id;
        IF FOUND AND s.status = 'archived' AND NEW.opens_at > s.archived_at THEN
            RAISE EXCEPTION 'site diary %: the day began after its site was archived', NEW.diary_date
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

CREATE TRIGGER site_diaries_check AFTER INSERT ON site_diaries
    FOR EACH ROW EXECUTE FUNCTION site_diaries_check();

-- A diary is created by its first revision: at commit it has one.
CREATE FUNCTION site_diaries_has_revision() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF NOT EXISTS (SELECT 1 FROM site_diary_revisions WHERE site_id = NEW.site_id AND diary_date = NEW.diary_date) THEN
            RAISE EXCEPTION 'site diary %: has no revision', NEW.diary_date
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

CREATE CONSTRAINT TRIGGER site_diaries_has_revision AFTER INSERT ON site_diaries
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION site_diaries_has_revision();

-- A diary's identity never changes, and its state only ever moves on by one
-- revision, to exactly what the revisions replay to.
CREATE FUNCTION site_diaries_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        r record;
    BEGIN
        IF TG_OP = 'DELETE' THEN
            RAISE EXCEPTION 'site diary %: diaries are never deleted', OLD.diary_date
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW IS NOT DISTINCT FROM OLD THEN
            RETURN NEW; -- changes nothing
        END IF;
        IF (NEW.org_id, NEW.site_id, NEW.diary_date, NEW.time_zone, NEW.opens_at, NEW.closes_at, NEW.created_by, NEW.created_at)
           IS DISTINCT FROM (OLD.org_id, OLD.site_id, OLD.diary_date, OLD.time_zone, OLD.opens_at, OLD.closes_at, OLD.created_by, OLD.created_at) THEN
            RAISE EXCEPTION 'site diary %: only its revisions move it on', OLD.diary_date
                USING ERRCODE = 'check_violation';
        END IF;
        SELECT * INTO r FROM site_diary_replay(OLD.site_id, OLD.diary_date);
        IF NEW.revision_count <> OLD.revision_count + 1 OR NEW.revision_count <> r.revisions
           OR NEW.head_revision_id IS DISTINCT FROM r.head
           OR NEW.conflict_revision_ids IS DISTINCT FROM r.conflicts
           OR NEW.late IS DISTINCT FROM r.late THEN
            RAISE EXCEPTION 'site diary %: its state must be what its revisions replay to', OLD.diary_date
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER site_diaries_guard BEFORE UPDATE OR DELETE ON site_diaries
    FOR EACH ROW EXECUTE FUNCTION site_diaries_guard();

-- Closing (D6). The diary row is locked first (FOR NO KEY UPDATE, the lock
-- the advance's UPDATE takes), so a diary's revisions are numbered and
-- judged one at a time.
CREATE FUNCTION site_diary_revisions_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        d site_diaries;
    BEGIN
        NEW.server_received_at := now();
        SELECT * INTO d FROM site_diaries WHERE site_id = NEW.site_id AND diary_date = NEW.diary_date FOR NO KEY UPDATE;
        IF NOT FOUND THEN
            RETURN NEW; -- the foreign key or the policy reports it
        END IF;
        NEW.seq := d.revision_count + 1;
        NEW.headcount_suggested := toolbox_headcount(NEW.site_id, NEW.diary_date);
        NEW.saved_after_close := NEW.client_saved_at >= d.closes_at;
        NEW.received_after_close := NEW.server_received_at >= d.closes_at;
        IF NEW.client_saved_at > NEW.server_received_at + interval '5 minutes' THEN
            RAISE EXCEPTION 'site diary %: client_saved_at is in the future; check the device clock', NEW.diary_date
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.client_saved_at < d.opens_at THEN
            RAISE EXCEPTION 'site diary %: saved before the day began', NEW.diary_date
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.saved_after_close AND d.revision_count > 0 THEN
            RAISE EXCEPTION 'site diary %: closed at midnight; add an addendum', NEW.diary_date
                USING ERRCODE = 'check_violation';
        END IF;
        IF NOT NEW.saved_after_close AND NEW.server_received_at >= d.closes_at + site_diary_grace() THEN
            RAISE EXCEPTION 'site diary %: saved before midnight but received more than 48 hours after; add an addendum', NEW.diary_date
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER site_diary_revisions_insert BEFORE INSERT ON site_diary_revisions
    FOR EACH ROW EXECUTE FUNCTION site_diary_revisions_insert();

-- Moves the diary on to its revisions' replay. The WHERE fails if another
-- revision got this number first.
CREATE FUNCTION site_diary_revisions_advance() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        r record;
    BEGIN
        IF NOT action_site_member(NEW.org_id, NEW.site_id, NEW.saved_by) THEN
            RAISE EXCEPTION 'site diary %: saved_by can''t see its site', NEW.diary_date
                USING ERRCODE = 'check_violation';
        END IF;
        SELECT * INTO r FROM site_diary_replay(NEW.site_id, NEW.diary_date);
        UPDATE site_diaries
        SET revision_count = NEW.seq, head_revision_id = r.head, conflict_revision_ids = r.conflicts, late = r.late
        WHERE site_id = NEW.site_id AND diary_date = NEW.diary_date AND revision_count = NEW.seq - 1;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'site diary %: revision % is not the next revision', NEW.diary_date, NEW.seq
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

CREATE TRIGGER site_diary_revisions_advance AFTER INSERT ON site_diary_revisions
    FOR EACH ROW EXECUTE FUNCTION site_diary_revisions_advance();

-- At commit a revision has exactly evidence_count photos, so a photo can't
-- be added to a committed revision.
CREATE FUNCTION site_diary_revision_complete() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        want   integer;
        photos integer;
    BEGIN
        SELECT evidence_count INTO want FROM site_diary_revisions WHERE id = NEW.revision_id;
        IF NOT FOUND THEN
            RETURN NULL; -- the foreign key reports it
        END IF;
        SELECT count(*) INTO photos FROM site_diary_revision_evidence WHERE revision_id = NEW.revision_id;
        IF photos <> want THEN
            RAISE EXCEPTION 'site diary revision %: has % photos but evidence_count is %', NEW.revision_id, photos, want
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

-- Fired by the revision itself (as revision_id) and by each photo.
CREATE FUNCTION site_diary_revision_complete_self() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        photos integer;
    BEGIN
        SELECT count(*) INTO photos FROM site_diary_revision_evidence WHERE revision_id = NEW.id;
        IF photos <> NEW.evidence_count THEN
            RAISE EXCEPTION 'site diary revision %: has % photos but evidence_count is %', NEW.id, photos, NEW.evidence_count
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

CREATE CONSTRAINT TRIGGER site_diary_revisions_complete AFTER INSERT ON site_diary_revisions
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION site_diary_revision_complete_self();
CREATE CONSTRAINT TRIGGER site_diary_revision_evidence_complete AFTER INSERT ON site_diary_revision_evidence
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION site_diary_revision_complete();

CREATE FUNCTION site_diary_append_only() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        RAISE EXCEPTION '% is append-only', TG_TABLE_NAME
            USING ERRCODE = 'check_violation';
    END
    $$;

CREATE TRIGGER site_diary_revisions_append_only BEFORE UPDATE OR DELETE ON site_diary_revisions
    FOR EACH ROW EXECUTE FUNCTION site_diary_append_only();
CREATE TRIGGER site_diary_revision_evidence_append_only BEFORE UPDATE OR DELETE ON site_diary_revision_evidence
    FOR EACH ROW EXECUTE FUNCTION site_diary_append_only();

CREATE TRIGGER site_diaries_no_truncate BEFORE TRUNCATE ON site_diaries
    FOR EACH STATEMENT EXECUTE FUNCTION site_diary_append_only();
CREATE TRIGGER site_diary_revisions_no_truncate BEFORE TRUNCATE ON site_diary_revisions
    FOR EACH STATEMENT EXECUTE FUNCTION site_diary_append_only();
CREATE TRIGGER site_diary_revision_evidence_no_truncate BEFORE TRUNCATE ON site_diary_revision_evidence
    FOR EACH STATEMENT EXECUTE FUNCTION site_diary_append_only();

ALTER TABLE site_diaries ENABLE ROW LEVEL SECURITY;
ALTER TABLE site_diaries FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON site_diaries FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE site_diary_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE site_diary_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON site_diary_revisions FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE site_diary_revision_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE site_diary_revision_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON site_diary_revision_evidence FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

-- Migration 000008's default privileges grant UPDATE and DELETE on new tables.
-- The advance trigger runs as the app, so it needs the state columns.
REVOKE UPDATE, DELETE ON site_diaries FROM siteproof_app;
GRANT UPDATE (revision_count, head_revision_id, conflict_revision_ids, late) ON site_diaries TO siteproof_app;
REVOKE UPDATE, DELETE ON site_diary_revisions FROM siteproof_app;
REVOKE UPDATE, DELETE ON site_diary_revision_evidence FROM siteproof_app;

COMMIT;
