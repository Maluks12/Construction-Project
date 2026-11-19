BEGIN;

-- Daily site sign-in and sign-out (M11 D11–D18). Four tables:
--   - site_sign_ins: one row per sign-in of a register worker on a site,
--     with an optional sign-out set once, and a retraction set once.
--   - site_sign_in_sheets: photos of signed paper sheets (verified evidence
--     of the site), per local day. Paper sign-ins need one first.
--   - site_roster: who is expected on a site (D16). A sign-in adds the
--     worker; leads and the site's supervisors add or remove (once).
-- And site_headcount(), which the diary's revision trigger now uses.
--
-- The rules, for every role:
--   - a sign-in counts on the local day of the phone's time (D15), from
--     that time until its sign-out, or for 16 hours without one (D13);
--   - one open sign-in per worker per site: a sign-in whose time falls
--     inside another of the worker's sign-ins on the site, open or closed,
--     is refused. Inserts and sign-outs lock the worker's row first, so two
--     phones tapping at once take turns and the second sees the first.

-- How long a sign-in without a sign-out lasts. attendance.OpenWindow
-- mirrors it; a test compares them.
CREATE FUNCTION site_sign_in_window() RETURNS interval
    LANGUAGE sql IMMUTABLE
    AS $$ SELECT interval '16 hours' $$;

CREATE TABLE site_sign_ins (
    id                     uuid PRIMARY KEY,
    org_id                 uuid NOT NULL,
    site_id                uuid NOT NULL,
    worker_id              uuid NOT NULL,
    -- The worker as the register named them at the sign-in (copied by the
    -- trigger), so a later edit of the register never changes this record.
    name                   text NOT NULL,
    firm                   text,
    clock_number           text,
    method                 text NOT NULL CHECK (method IN ('tap', 'paper')),
    -- On-the-spot entry that matched a worker already on the register (D11).
    matched_existing       boolean NOT NULL DEFAULT false,
    recorded_by            uuid NOT NULL,
    client_signed_in_at    timestamptz NOT NULL,
    received_at            timestamptz NOT NULL DEFAULT now(),
    -- The site regime's zone, copied by the API; sign_in_date is the local
    -- date of the phone's time in it (set by the trigger).
    time_zone              text NOT NULL,
    sign_in_date           date NOT NULL,
    received_after_close   boolean NOT NULL DEFAULT false,
    device_id              text CHECK (octet_length(device_id) BETWEEN 1 AND 200),
    client_signed_out_at   timestamptz,
    sign_out_method        text CHECK (sign_out_method IN ('tap', 'paper')),
    signed_out_by          uuid,
    sign_out_received_at   timestamptz,
    retracted_at           timestamptz,
    retracted_by           uuid,
    retract_reason         text CHECK (char_length(btrim(retract_reason)) BETWEEN 1 AND 500),

    FOREIGN KEY (org_id, site_id) REFERENCES sites (org_id, id),
    FOREIGN KEY (org_id, worker_id) REFERENCES workers (org_id, id),
    FOREIGN KEY (org_id, recorded_by) REFERENCES org_members (org_id, user_id),
    FOREIGN KEY (org_id, signed_out_by) REFERENCES org_members (org_id, user_id),
    FOREIGN KEY (org_id, retracted_by) REFERENCES org_members (org_id, user_id),
    UNIQUE (org_id, site_id, id),
    CONSTRAINT site_sign_ins_sign_out CHECK (
        (client_signed_out_at IS NULL) = (sign_out_method IS NULL)
        AND (client_signed_out_at IS NULL) = (signed_out_by IS NULL)
        AND (client_signed_out_at IS NULL) = (sign_out_received_at IS NULL)),
    CONSTRAINT site_sign_ins_retraction CHECK (
        (retracted_at IS NULL) = (retracted_by IS NULL) AND (retracted_at IS NULL) = (retract_reason IS NULL))
);

CREATE INDEX site_sign_ins_site_date_idx ON site_sign_ins (site_id, sign_in_date);
CREATE INDEX site_sign_ins_worker_idx ON site_sign_ins (worker_id, site_id, client_signed_in_at);

-- The end of a sign-in's presence: its sign-out, or the window without one.
CREATE FUNCTION site_sign_in_ends_at(s site_sign_ins) RETURNS timestamptz
    LANGUAGE sql STABLE
    AS $$ SELECT coalesce(s.client_signed_out_at, s.client_signed_in_at + site_sign_in_window()) $$;

-- The worker's other standing sign-in on the site that covers at, if any.
CREATE FUNCTION site_sign_in_covering(p_site uuid, p_worker uuid, p_at timestamptz, p_except uuid) RETURNS uuid
    LANGUAGE sql STABLE
    AS $$
        SELECT s.id FROM site_sign_ins s
        WHERE s.site_id = p_site AND s.worker_id = p_worker AND s.id <> p_except AND s.retracted_at IS NULL
          AND s.client_signed_in_at <= p_at AND p_at < site_sign_in_ends_at(s)
        ORDER BY s.client_signed_in_at LIMIT 1
    $$;

CREATE TABLE site_sign_in_sheets (
    org_id          uuid NOT NULL,
    site_id         uuid NOT NULL,
    sheet_date      date NOT NULL,
    evidence_id     uuid NOT NULL,
    evidence_status text NOT NULL DEFAULT 'verified' CHECK (evidence_status = 'verified'),
    added_by        uuid NOT NULL,
    added_at        timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (site_id, sheet_date, evidence_id),
    FOREIGN KEY (org_id, site_id) REFERENCES sites (org_id, id),
    FOREIGN KEY (org_id, site_id, evidence_id, evidence_status) REFERENCES evidence (org_id, site_id, id, status),
    FOREIGN KEY (org_id, added_by) REFERENCES org_members (org_id, user_id)
);

CREATE FUNCTION site_sign_ins_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        w     workers;
        other uuid;
    BEGIN
        NEW.received_at := now();
        IF NEW.client_signed_in_at > NEW.received_at + interval '5 minutes' THEN
            RAISE EXCEPTION 'sign-in %: client_signed_in_at is in the future; check the device clock', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.client_signed_out_at IS NOT NULL OR NEW.signed_out_by IS NOT NULL OR NEW.retracted_at IS NOT NULL OR NEW.retracted_by IS NOT NULL THEN
            RAISE EXCEPTION 'sign-in %: a sign-out or retraction comes after the sign-in, never with it', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        NEW.sign_in_date := action_local_date(NEW.client_signed_in_at, NEW.time_zone);
        NEW.received_after_close := NEW.received_at >= action_due_at(NEW.sign_in_date, NEW.time_zone) + site_diary_grace();
        -- Serialises a worker's sign-ins and sign-outs: a second phone's tap
        -- waits here for the first to commit, then sees it below.
        SELECT * INTO w FROM workers WHERE id = NEW.worker_id FOR NO KEY UPDATE;
        IF NOT FOUND THEN
            RETURN NEW; -- the foreign key or the policy reports it
        END IF;
        NEW.name := w.name;
        NEW.firm := w.firm;
        NEW.clock_number := w.clock_number;
        other := site_sign_in_covering(NEW.site_id, NEW.worker_id, NEW.client_signed_in_at, NEW.id);
        IF other IS NOT NULL THEN
            RAISE EXCEPTION 'sign-in %: already signed in at that time (sign-in %)', NEW.id, other
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.method = 'paper' AND NOT EXISTS (
            SELECT 1 FROM site_sign_in_sheets WHERE site_id = NEW.site_id AND sheet_date = NEW.sign_in_date) THEN
            RAISE EXCEPTION 'sign-in %: paper sign-in needs a photo of that day''s signed sheet', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER site_sign_ins_insert BEFORE INSERT ON site_sign_ins
    FOR EACH ROW EXECUTE FUNCTION site_sign_ins_insert();

-- After the insert (RLS first): the recorder can see the site, the site
-- wasn't archived before the sign-in, and the worker joins the roster.
CREATE FUNCTION site_sign_ins_check() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        s sites;
    BEGIN
        IF NOT action_site_member(NEW.org_id, NEW.site_id, NEW.recorded_by) THEN
            RAISE EXCEPTION 'sign-in %: recorded_by can''t see its site', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        SELECT * INTO s FROM sites WHERE id = NEW.site_id;
        IF FOUND AND s.status = 'archived' AND NEW.client_signed_in_at > s.archived_at THEN
            RAISE EXCEPTION 'sign-in %: signed in after its site was archived', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        INSERT INTO site_roster (org_id, site_id, worker_id, added_via, added_by)
        VALUES (NEW.org_id, NEW.site_id, NEW.worker_id, 'sign_in', NEW.recorded_by)
        ON CONFLICT (site_id, worker_id) WHERE removed_at IS NULL DO NOTHING;
        RETURN NULL;
    END
    $$;

-- A sign-out is set once; a retraction once, by the recorder or a lead.
CREATE FUNCTION site_sign_ins_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        rest_new jsonb;
        rest_old jsonb;
        later    uuid;
    BEGIN
        IF TG_OP = 'DELETE' THEN
            RAISE EXCEPTION 'sign-in %: sign-ins are never deleted; retract them', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW IS NOT DISTINCT FROM OLD THEN
            RETURN NEW;
        END IF;
        rest_new := to_jsonb(NEW) - ARRAY['client_signed_out_at', 'sign_out_method', 'signed_out_by', 'sign_out_received_at',
                                            'retracted_at', 'retracted_by', 'retract_reason'];
        rest_old := to_jsonb(OLD) - ARRAY['client_signed_out_at', 'sign_out_method', 'signed_out_by', 'sign_out_received_at',
                                            'retracted_at', 'retracted_by', 'retract_reason'];
        IF rest_new IS DISTINCT FROM rest_old THEN
            RAISE EXCEPTION 'sign-in %: only a sign-out or a retraction can be added', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF OLD.retracted_at IS NOT NULL THEN
            RAISE EXCEPTION 'sign-in %: retracted; it never changes again', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;

        IF (NEW.client_signed_out_at, NEW.sign_out_method, NEW.signed_out_by)
           IS DISTINCT FROM (OLD.client_signed_out_at, OLD.sign_out_method, OLD.signed_out_by) THEN
            IF OLD.client_signed_out_at IS NOT NULL THEN
                RAISE EXCEPTION 'sign-in %: already signed out', OLD.id
                    USING ERRCODE = 'check_violation';
            END IF;
            NEW.sign_out_received_at := now();
            PERFORM 1 FROM workers WHERE id = OLD.worker_id FOR NO KEY UPDATE;
            IF NEW.client_signed_out_at > NEW.sign_out_received_at + interval '5 minutes' THEN
                RAISE EXCEPTION 'sign-in %: client_signed_out_at is in the future; check the device clock', OLD.id
                    USING ERRCODE = 'check_violation';
            END IF;
            IF NEW.client_signed_out_at < OLD.client_signed_in_at THEN
                RAISE EXCEPTION 'sign-in %: signed out before signing in', OLD.id
                    USING ERRCODE = 'check_violation';
            END IF;
            IF NEW.client_signed_out_at > OLD.client_signed_in_at + site_sign_in_window() THEN
                RAISE EXCEPTION 'sign-in %: signed out more than 16 hours after signing in', OLD.id
                    USING ERRCODE = 'check_violation';
            END IF;
            SELECT s.id INTO later FROM site_sign_ins s
            WHERE s.site_id = OLD.site_id AND s.worker_id = OLD.worker_id AND s.id <> OLD.id AND s.retracted_at IS NULL
              AND s.client_signed_in_at > OLD.client_signed_in_at AND s.client_signed_in_at < NEW.client_signed_out_at
            LIMIT 1;
            IF later IS NOT NULL THEN
                RAISE EXCEPTION 'sign-in %: signed out after a later sign-in (sign-in %)', OLD.id, later
                    USING ERRCODE = 'check_violation';
            END IF;
        ELSIF NEW.sign_out_received_at IS DISTINCT FROM OLD.sign_out_received_at THEN
            RAISE EXCEPTION 'sign-in %: only a sign-out or a retraction can be added', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;

        IF (NEW.retracted_by, NEW.retract_reason) IS DISTINCT FROM (OLD.retracted_by, OLD.retract_reason) THEN
            IF NEW.retracted_by IS NULL OR NEW.retract_reason IS NULL THEN
                RAISE EXCEPTION 'sign-in %: a retraction needs who and why', OLD.id
                    USING ERRCODE = 'check_violation';
            END IF;
            IF NEW.retracted_by <> OLD.recorded_by AND NOT workers_lead(OLD.org_id, NEW.retracted_by) THEN
                RAISE EXCEPTION 'sign-in %: only the person who recorded it, an owner or a SHE officer can retract it', OLD.id
                    USING ERRCODE = 'check_violation';
            END IF;
            NEW.retracted_at := now();
        ELSIF NEW.retracted_at IS DISTINCT FROM OLD.retracted_at THEN
            RAISE EXCEPTION 'sign-in %: a retraction needs who and why', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TABLE site_roster (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        uuid NOT NULL,
    site_id       uuid NOT NULL,
    worker_id     uuid NOT NULL,
    added_via     text NOT NULL CHECK (added_via IN ('sign_in', 'manual')),
    added_by      uuid NOT NULL,
    added_at      timestamptz NOT NULL DEFAULT now(),
    removed_at    timestamptz,
    removed_by    uuid,
    remove_reason text CHECK (char_length(btrim(remove_reason)) BETWEEN 1 AND 500),

    FOREIGN KEY (org_id, site_id) REFERENCES sites (org_id, id),
    FOREIGN KEY (org_id, worker_id) REFERENCES workers (org_id, id),
    FOREIGN KEY (org_id, added_by) REFERENCES org_members (org_id, user_id),
    FOREIGN KEY (org_id, removed_by) REFERENCES org_members (org_id, user_id),
    CONSTRAINT site_roster_removal CHECK (
        (removed_at IS NULL) = (removed_by IS NULL) AND (removed_at IS NULL) = (remove_reason IS NULL))
);

CREATE UNIQUE INDEX site_roster_one_live ON site_roster (site_id, worker_id) WHERE removed_at IS NULL;

CREATE TRIGGER site_sign_ins_check AFTER INSERT ON site_sign_ins
    FOR EACH ROW EXECUTE FUNCTION site_sign_ins_check();
CREATE TRIGGER site_sign_ins_guard BEFORE UPDATE OR DELETE ON site_sign_ins
    FOR EACH ROW EXECUTE FUNCTION site_sign_ins_guard();

CREATE FUNCTION site_roster_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF TG_OP = 'DELETE' THEN
            RAISE EXCEPTION 'roster entry %: never deleted; remove it', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW IS NOT DISTINCT FROM OLD THEN
            RETURN NEW;
        END IF;
        IF OLD.removed_at IS NOT NULL THEN
            RAISE EXCEPTION 'roster entry %: already removed', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF (NEW.id, NEW.org_id, NEW.site_id, NEW.worker_id, NEW.added_via, NEW.added_by, NEW.added_at)
           IS DISTINCT FROM (OLD.id, OLD.org_id, OLD.site_id, OLD.worker_id, OLD.added_via, OLD.added_by, OLD.added_at)
           OR NEW.removed_by IS NULL OR NEW.remove_reason IS NULL THEN
            RAISE EXCEPTION 'roster entry %: the only change is removing it, with who and why', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NOT action_site_member(NEW.org_id, NEW.site_id, NEW.removed_by) THEN
            RAISE EXCEPTION 'roster entry %: removed_by can''t see its site', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        NEW.removed_at := now();
        RETURN NEW;
    END
    $$;

CREATE TRIGGER site_roster_guard BEFORE UPDATE OR DELETE ON site_roster
    FOR EACH ROW EXECUTE FUNCTION site_roster_guard();

CREATE FUNCTION site_sign_ins_append_only() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        RAISE EXCEPTION '% is append-only', TG_TABLE_NAME
            USING ERRCODE = 'check_violation';
    END
    $$;

CREATE TRIGGER site_sign_in_sheets_append_only BEFORE UPDATE OR DELETE ON site_sign_in_sheets
    FOR EACH ROW EXECUTE FUNCTION site_sign_ins_append_only();
CREATE TRIGGER site_sign_ins_no_truncate BEFORE TRUNCATE ON site_sign_ins
    FOR EACH STATEMENT EXECUTE FUNCTION site_sign_ins_append_only();
CREATE TRIGGER site_sign_in_sheets_no_truncate BEFORE TRUNCATE ON site_sign_in_sheets
    FOR EACH STATEMENT EXECUTE FUNCTION site_sign_ins_append_only();
CREATE TRIGGER site_roster_no_truncate BEFORE TRUNCATE ON site_roster
    FOR EACH STATEMENT EXECUTE FUNCTION site_sign_ins_append_only();

-- The diary's suggested headcount (M11): distinct people among the day's
-- standing sign-ins and the standing signatures of the day's talks. A
-- sign-in is its worker; a signature is toolbox_attendance_person (its
-- linked or same-key worker, else its identity key), so a worker who signed
-- in and signed a talk counts once.
CREATE FUNCTION site_headcount(p_site uuid, p_day date) RETURNS integer
    LANGUAGE sql STABLE
    AS $$
        SELECT count(DISTINCT person)::integer FROM (
            SELECT 'w:' || s.worker_id AS person
            FROM site_sign_ins s
            WHERE s.site_id = p_site AND s.sign_in_date = p_day AND s.retracted_at IS NULL
            UNION ALL
            SELECT toolbox_attendance_person(a.id)
            FROM toolbox_attendance a JOIN toolbox_talks t ON t.id = a.talk_id
            WHERE t.site_id = p_site AND t.talk_date = p_day
              AND NOT EXISTS (SELECT 1 FROM toolbox_attendance_retractions r WHERE r.attendance_id = a.id)
        ) people
    $$;

-- As 000021, with site_headcount in place of toolbox_headcount. Revisions
-- already stored keep the number they were given.
CREATE OR REPLACE FUNCTION site_diary_revisions_insert() RETURNS trigger
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
        NEW.headcount_suggested := site_headcount(NEW.site_id, NEW.diary_date);
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

ALTER TABLE site_sign_ins ENABLE ROW LEVEL SECURITY;
ALTER TABLE site_sign_ins FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON site_sign_ins FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE site_sign_in_sheets ENABLE ROW LEVEL SECURITY;
ALTER TABLE site_sign_in_sheets FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON site_sign_in_sheets FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE site_roster ENABLE ROW LEVEL SECURITY;
ALTER TABLE site_roster FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON site_roster FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

REVOKE UPDATE, DELETE ON site_sign_ins FROM siteproof_app;
GRANT UPDATE (client_signed_out_at, sign_out_method, signed_out_by, sign_out_received_at, retracted_at, retracted_by, retract_reason)
    ON site_sign_ins TO siteproof_app;
REVOKE UPDATE, DELETE ON site_sign_in_sheets FROM siteproof_app;
REVOKE UPDATE, DELETE ON site_roster FROM siteproof_app;
GRANT UPDATE (removed_at, removed_by, remove_reason) ON site_roster TO siteproof_app;

COMMIT;
