BEGIN;

-- The corrective action register (Rev 4 §2.5). Decisions D1–D8 (15 Nov) are
-- in construction_db/CLAUDE.md. Two tables:
--   - corrective_actions: what the action is and where it came from (never
--     changes), plus a copy of its current state (owner, due date, status).
--   - corrective_action_events: append-only. Each records who did what, when
--     and why, and the action's state after it. The header's state is always
--     its latest event's, so the history explains every value it ever had.
-- Ids are generated on the device so offline sync is idempotent.

-- Target of an action's finding key: the action pins the finding's severity.
ALTER TABLE inspection_findings ADD CONSTRAINT inspection_findings_severity_key
    UNIQUE (org_id, site_id, inspection_id, item_key, severity);

-- The order of checklists.Severities, for comparing a correction's finding
-- with the one it corrects (D7).
CREATE FUNCTION severity_rank(p_severity text) RETURNS integer
    LANGUAGE sql IMMUTABLE STRICT
    AS $$ SELECT CASE p_severity WHEN 'low' THEN 1 WHEN 'medium' THEN 2 WHEN 'high' THEN 3 WHEN 'critical' THEN 4 END $$;

-- Days from the local date an action is raised to its default due date (D2).
-- actions.DefaultDueDays mirrors it; a test compares them.
CREATE FUNCTION action_default_due_days(p_severity text) RETURNS integer
    LANGUAGE sql IMMUTABLE STRICT
    AS $$ SELECT CASE p_severity WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 7 WHEN 'low' THEN 14 END $$;

-- An unknown zone raises 22023.
CREATE FUNCTION action_local_date(p_at timestamptz, p_time_zone text) RETURNS date
    LANGUAGE sql STABLE STRICT
    AS $$ SELECT (p_at AT TIME ZONE p_time_zone)::date $$;

-- Due by the end of due_on, local time: overdue from the next local midnight
-- (D8, the M8 rule without the s4 extension).
CREATE FUNCTION action_due_at(p_due_on date, p_time_zone text) RETURNS timestamptz
    LANGUAGE sql STABLE STRICT
    AS $$ SELECT (p_due_on + 1)::timestamp AT TIME ZONE p_time_zone $$;

-- Who can see a site: an active owner or SHE officer, or an active supervisor
-- added to it. Raisers, owners (D3) and everyone recording an event must be.
CREATE FUNCTION action_site_member(p_org uuid, p_site uuid, p_user uuid) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
        SELECT EXISTS (
            SELECT 1 FROM org_members m
            WHERE m.org_id = p_org AND m.user_id = p_user AND m.status = 'active'
              AND (m.role IN ('owner', 'she_officer')
                   OR (m.role = 'supervisor' AND EXISTS (
                       SELECT 1 FROM site_members s WHERE s.org_id = p_org AND s.site_id = p_site AND s.user_id = p_user))))
    $$;

-- An active owner or SHE officer ("a lead").
CREATE FUNCTION action_lead(p_org uuid, p_user uuid) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
        SELECT EXISTS (
            SELECT 1 FROM org_members
            WHERE org_id = p_org AND user_id = p_user AND status = 'active' AND role IN ('owner', 'she_officer'))
    $$;

CREATE TABLE corrective_actions (
    id                uuid PRIMARY KEY,
    org_id            uuid NOT NULL,
    site_id           uuid NOT NULL,
    -- The source: a finding, or an incident version (D6). Exactly one.
    inspection_id     uuid,
    item_key          text,
    incident_id       uuid,
    incident_version  integer,
    -- A finding's own severity (the foreign key pins it); chosen for an
    -- incident. Never changes.
    severity          text NOT NULL CHECK (severity IN ('low', 'medium', 'high', 'critical')),
    title             text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    description       text CHECK (char_length(description) BETWEEN 1 AND 2000),
    raised_by         uuid NOT NULL,
    client_raised_at  timestamptz NOT NULL,
    raised_at         timestamptz NOT NULL DEFAULT now(),
    device_id         text CHECK (octet_length(device_id) BETWEEN 1 AND 200),
    -- The site regime's time zone, copied as deadlines copy theirs.
    time_zone         text NOT NULL,
    -- Set by the insert trigger: when the failed item was answered or the
    -- incident occurred (close-out photos can't be older, D5), and the
    -- severity's default due date (D2).
    source_at         timestamptz NOT NULL,
    default_due_on    date NOT NULL,
    -- The current state: always the latest event's (corrective_actions_guard).
    status            text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'awaiting_verification', 'closed', 'cancelled')),
    owner_id          uuid NOT NULL,
    responsible_party text CHECK (char_length(responsible_party) BETWEEN 1 AND 200),
    due_on            date NOT NULL,
    due_at            timestamptz NOT NULL,
    event_count       integer NOT NULL DEFAULT 0 CHECK (event_count >= 0),

    FOREIGN KEY (org_id, site_id) REFERENCES sites (org_id, id),
    FOREIGN KEY (org_id, raised_by) REFERENCES org_members (org_id, user_id),
    FOREIGN KEY (org_id, owner_id) REFERENCES org_members (org_id, user_id),
    -- MATCH SIMPLE: the source not used is all null and skips its check.
    CONSTRAINT corrective_actions_finding_fkey FOREIGN KEY (org_id, site_id, inspection_id, item_key, severity)
        REFERENCES inspection_findings (org_id, site_id, inspection_id, item_key, severity),
    CONSTRAINT corrective_actions_incident_fkey FOREIGN KEY (org_id, site_id, incident_id, incident_version)
        REFERENCES incident_versions (org_id, site_id, incident_id, version),
    UNIQUE (org_id, site_id, id),

    CONSTRAINT corrective_actions_one_source CHECK (
        (inspection_id IS NOT NULL AND item_key IS NOT NULL AND incident_id IS NULL AND incident_version IS NULL)
        OR (inspection_id IS NULL AND item_key IS NULL AND incident_id IS NOT NULL AND incident_version IS NOT NULL))
);

CREATE INDEX corrective_actions_site_idx ON corrective_actions (site_id, raised_at);
CREATE INDEX corrective_actions_finding_idx ON corrective_actions (inspection_id, item_key) WHERE inspection_id IS NOT NULL;
CREATE INDEX corrective_actions_incident_idx ON corrective_actions (incident_id) WHERE incident_id IS NOT NULL;
CREATE INDEX corrective_actions_owner_idx ON corrective_actions (owner_id) WHERE status IN ('open', 'awaiting_verification');

CREATE TABLE corrective_action_events (
    -- Generated on the device; a repeat is recognised by it.
    id                uuid NOT NULL UNIQUE,
    org_id            uuid NOT NULL,
    site_id           uuid NOT NULL,
    action_id         uuid NOT NULL,
    seq               integer NOT NULL CHECK (seq >= 1),
    kind              text NOT NULL CHECK (kind IN ('raised', 'reassigned', 'due_changed', 'cancelled')),
    actor_id          uuid NOT NULL,
    client_at         timestamptz NOT NULL,
    recorded_at       timestamptz NOT NULL DEFAULT now(),
    device_id         text CHECK (octet_length(device_id) BETWEEN 1 AND 200),
    reason            text CHECK (char_length(reason) BETWEEN 1 AND 500),
    -- The action's state after this event.
    status            text NOT NULL CHECK (status IN ('open', 'awaiting_verification', 'closed', 'cancelled')),
    owner_id          uuid NOT NULL,
    responsible_party text CHECK (char_length(responsible_party) BETWEEN 1 AND 200),
    due_on            date NOT NULL,

    PRIMARY KEY (action_id, seq),
    FOREIGN KEY (org_id, site_id, action_id) REFERENCES corrective_actions (org_id, site_id, id),
    FOREIGN KEY (org_id, actor_id) REFERENCES org_members (org_id, user_id),
    FOREIGN KEY (org_id, owner_id) REFERENCES org_members (org_id, user_id),
    UNIQUE (org_id, site_id, action_id, id),

    CONSTRAINT corrective_action_events_raise_first CHECK ((kind = 'raised') = (seq = 1)),
    CONSTRAINT corrective_action_events_cancel_reason CHECK (kind <> 'cancelled' OR reason IS NOT NULL)
);

-- Every guard below applies to every role, the table owner included. Each
-- raises check_violation with its own message.

-- A new action is open with no events: its raise (event 1, in the same
-- transaction) is what makes it count. raised_at is the server's time.
CREATE FUNCTION corrective_actions_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        site_status       text;
        correction_status text;
        latest            integer;
        raised_on         date;
    BEGIN
        NEW.raised_at := now();
        -- A placeholder so a missing source is reported by its foreign key.
        NEW.source_at := NEW.client_raised_at;
        IF NEW.event_count <> 0 OR NEW.status <> 'open' THEN
            RAISE EXCEPTION 'corrective action %: a new action is open, with no events before its raise', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.client_raised_at > NEW.raised_at + interval '5 minutes' THEN
            RAISE EXCEPTION 'corrective action %: client_raised_at is in the future', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        SELECT status INTO site_status FROM sites WHERE id = NEW.site_id;
        IF site_status = 'archived' THEN
            RAISE EXCEPTION 'corrective action %: its site is archived', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;

        IF NEW.inspection_id IS NOT NULL THEN
            -- A finding superseded by a completed correction takes no new
            -- action (D7). FOR SHARE on the correction orders this against
            -- completing it: whichever commits first, the other sees it.
            SELECT status INTO correction_status FROM inspections
            WHERE corrects_id = NEW.inspection_id AND status <> 'abandoned'
            FOR SHARE;
            IF correction_status = 'completed' THEN
                RAISE EXCEPTION 'corrective action %: the finding is superseded by a correction', NEW.id
                    USING ERRCODE = 'check_violation';
            END IF;
            SELECT client_answered_at INTO NEW.source_at FROM inspection_responses
            WHERE inspection_id = NEW.inspection_id AND item_key = NEW.item_key;
        ELSIF NEW.incident_id IS NOT NULL THEN
            -- Raised against the latest version only (D6). FOR SHARE on the
            -- incident queues against an amendment, which updates it.
            SELECT latest_version INTO latest FROM incidents WHERE id = NEW.incident_id FOR SHARE;
            IF FOUND AND latest <> NEW.incident_version THEN
                RAISE EXCEPTION 'corrective action %: raised against version % but the incident is at version %', NEW.id, NEW.incident_version, latest
                    USING ERRCODE = 'check_violation';
            END IF;
            SELECT occurred_at INTO NEW.source_at FROM incident_versions
            WHERE incident_id = NEW.incident_id AND version = NEW.incident_version;
        END IF;
        IF NEW.source_at IS NULL THEN
            NEW.source_at := NEW.client_raised_at;
        END IF;

        raised_on := action_local_date(NEW.client_raised_at, NEW.time_zone);
        NEW.default_due_on := raised_on + action_default_due_days(NEW.severity);
        IF NEW.due_on < raised_on THEN
            RAISE EXCEPTION 'corrective action %: due_on is before the day it was raised', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        NEW.due_at := action_due_at(NEW.due_on, NEW.time_zone);
        RETURN NEW;
    END
    $$;

CREATE TRIGGER corrective_actions_insert BEFORE INSERT ON corrective_actions
    FOR EACH ROW EXECUTE FUNCTION corrective_actions_insert();

-- At commit a new action has its raise.
CREATE FUNCTION corrective_actions_has_raise() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF NOT EXISTS (SELECT 1 FROM corrective_action_events WHERE action_id = NEW.id AND seq = 1) THEN
            RAISE EXCEPTION 'corrective action %: has no raise; an action is created with its raise event', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

CREATE CONSTRAINT TRIGGER corrective_actions_has_raise AFTER INSERT ON corrective_actions
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION corrective_actions_has_raise();

-- The header's source never changes, and its state changes only to the
-- state its next event records. A no-op is allowed; nothing is deleted.
CREATE FUNCTION corrective_actions_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        rest corrective_actions;
        e    corrective_action_events;
    BEGIN
        IF TG_OP = 'DELETE' THEN
            RAISE EXCEPTION 'corrective action %: actions are never deleted; cancel it instead', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW IS NOT DISTINCT FROM OLD THEN
            RETURN NEW;
        END IF;
        rest := NEW;
        rest.status := OLD.status;
        rest.owner_id := OLD.owner_id;
        rest.responsible_party := OLD.responsible_party;
        rest.due_on := OLD.due_on;
        rest.due_at := OLD.due_at;
        rest.event_count := OLD.event_count;
        IF rest IS DISTINCT FROM OLD OR NEW.event_count <> OLD.event_count + 1 THEN
            RAISE EXCEPTION 'corrective action %: an action changes only by a new event', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        SELECT * INTO e FROM corrective_action_events WHERE action_id = OLD.id AND seq = NEW.event_count;
        IF NOT FOUND OR (NEW.status, NEW.owner_id, NEW.responsible_party, NEW.due_on, NEW.due_at)
                        IS DISTINCT FROM (e.status, e.owner_id, e.responsible_party, e.due_on, action_due_at(e.due_on, OLD.time_zone)) THEN
            RAISE EXCEPTION 'corrective action %: an action changes only by a new event', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER corrective_actions_guard BEFORE UPDATE OR DELETE ON corrective_actions
    FOR EACH ROW EXECUTE FUNCTION corrective_actions_guard();

-- Each event is checked against the state the one before it left, under the
-- action's row lock (FOR NO KEY UPDATE, which the advance's UPDATE takes too,
-- so a close-out row's foreign-key check doesn't wait on it), so events of one
-- action are recorded one at a time.
-- recorded_at is the server's time.
CREATE FUNCTION corrective_action_events_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        a    corrective_actions;
        lead boolean;
    BEGIN
        NEW.recorded_at := now();
        SELECT * INTO a FROM corrective_actions WHERE id = NEW.action_id FOR NO KEY UPDATE;
        IF NOT FOUND THEN
            RETURN NEW; -- the foreign key (or RLS) reports it
        END IF;
        IF NEW.seq <> a.event_count + 1 THEN
            RAISE EXCEPTION 'corrective action %: event % is not the next event', a.id, NEW.seq
                USING ERRCODE = 'check_violation';
        END IF;
        IF (NEW.kind = 'raised') <> (NEW.seq = 1) THEN
            RAISE EXCEPTION 'corrective action %: its first event, and only its first, is its raise', a.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.client_at > NEW.recorded_at + interval '5 minutes' THEN
            RAISE EXCEPTION 'corrective action %: client_at is in the future', a.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NOT action_site_member(a.org_id, a.site_id, NEW.actor_id) THEN
            RAISE EXCEPTION 'corrective action %: % is not an owner, SHE officer or supervisor on its site', a.id, NEW.actor_id
                USING ERRCODE = 'check_violation';
        END IF;
        lead := action_lead(a.org_id, NEW.actor_id);

        IF NEW.kind = 'raised' THEN
            IF NEW.actor_id <> a.raised_by OR NEW.client_at <> a.client_raised_at
               OR (NEW.status, NEW.owner_id, NEW.responsible_party, NEW.due_on)
                  IS DISTINCT FROM ('open'::text, a.owner_id, a.responsible_party, a.due_on) THEN
                RAISE EXCEPTION 'corrective action %: the raise must record the action as it was raised', a.id
                    USING ERRCODE = 'check_violation';
            END IF;
            IF NOT action_site_member(a.org_id, a.site_id, NEW.owner_id) THEN
                RAISE EXCEPTION 'corrective action %: the owner must be an owner, SHE officer or supervisor on its site', a.id
                    USING ERRCODE = 'check_violation';
            END IF;
            IF NEW.due_on > a.default_due_on AND (NOT lead OR NEW.reason IS NULL) THEN
                RAISE EXCEPTION 'corrective action %: a due date later than the default needs an owner or SHE officer and a reason', a.id
                    USING ERRCODE = 'check_violation';
            END IF;
            RETURN NEW;
        END IF;

        IF a.status IN ('closed', 'cancelled') THEN
            RAISE EXCEPTION 'corrective action % is %', a.id, a.status
                USING ERRCODE = 'check_violation';
        END IF;

        CASE NEW.kind
        WHEN 'reassigned' THEN
            IF (NEW.status, NEW.due_on) IS DISTINCT FROM (a.status, a.due_on)
               OR (NEW.owner_id, NEW.responsible_party) IS NOT DISTINCT FROM (a.owner_id, a.responsible_party) THEN
                RAISE EXCEPTION 'corrective action %: a reassignment changes the owner or responsible party, and nothing else', a.id
                    USING ERRCODE = 'check_violation';
            END IF;
            IF NOT lead AND NEW.actor_id <> a.raised_by THEN
                RAISE EXCEPTION 'corrective action %: only its raiser, an owner or a SHE officer can reassign it', a.id
                    USING ERRCODE = 'check_violation';
            END IF;
            IF NOT action_site_member(a.org_id, a.site_id, NEW.owner_id) THEN
                RAISE EXCEPTION 'corrective action %: the owner must be an owner, SHE officer or supervisor on its site', a.id
                    USING ERRCODE = 'check_violation';
            END IF;
        WHEN 'due_changed' THEN
            IF (NEW.status, NEW.owner_id, NEW.responsible_party) IS DISTINCT FROM (a.status, a.owner_id, a.responsible_party)
               OR NEW.due_on = a.due_on THEN
                RAISE EXCEPTION 'corrective action %: a due date change changes the due date, and nothing else', a.id
                    USING ERRCODE = 'check_violation';
            END IF;
            IF NEW.due_on < action_local_date(NEW.client_at, a.time_zone) THEN
                RAISE EXCEPTION 'corrective action %: due_on is in the past', a.id
                    USING ERRCODE = 'check_violation';
            END IF;
            IF NEW.due_on > a.due_on AND (NOT lead OR NEW.reason IS NULL) THEN
                RAISE EXCEPTION 'corrective action %: moving a due date later needs an owner or SHE officer and a reason', a.id
                    USING ERRCODE = 'check_violation';
            END IF;
            IF NEW.due_on < a.due_on AND NOT lead AND NEW.actor_id <> a.raised_by AND NEW.actor_id <> a.owner_id THEN
                RAISE EXCEPTION 'corrective action %: only its raiser, its owner, an owner or a SHE officer can move its due date earlier', a.id
                    USING ERRCODE = 'check_violation';
            END IF;
        WHEN 'cancelled' THEN
            IF (NEW.status, NEW.owner_id, NEW.responsible_party, NEW.due_on)
               IS DISTINCT FROM ('cancelled'::text, a.owner_id, a.responsible_party, a.due_on) THEN
                RAISE EXCEPTION 'corrective action %: a cancellation changes the status to cancelled, and nothing else', a.id
                    USING ERRCODE = 'check_violation';
            END IF;
            IF NOT lead THEN
                RAISE EXCEPTION 'corrective action %: only an owner or a SHE officer can cancel it', a.id
                    USING ERRCODE = 'check_violation';
            END IF;
        END CASE;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER corrective_action_events_insert BEFORE INSERT ON corrective_action_events
    FOR EACH ROW EXECUTE FUNCTION corrective_action_events_insert();

-- Copies the event's state to the header. The WHERE repeats the sequence
-- check, so a header that moved on can't be overwritten.
CREATE FUNCTION corrective_action_events_advance() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        UPDATE corrective_actions
        SET status = NEW.status, owner_id = NEW.owner_id, responsible_party = NEW.responsible_party,
            due_on = NEW.due_on, due_at = action_due_at(NEW.due_on, time_zone), event_count = NEW.seq
        WHERE id = NEW.action_id AND event_count = NEW.seq - 1;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'corrective action %: event % is not the next event', NEW.action_id, NEW.seq
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

CREATE TRIGGER corrective_action_events_advance AFTER INSERT ON corrective_action_events
    FOR EACH ROW EXECUTE FUNCTION corrective_action_events_advance();

CREATE FUNCTION corrective_actions_append_only() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        RAISE EXCEPTION '% is append-only; record a new event instead', TG_TABLE_NAME
            USING ERRCODE = 'check_violation';
    END
    $$;

CREATE TRIGGER corrective_action_events_append_only BEFORE UPDATE OR DELETE ON corrective_action_events
    FOR EACH ROW EXECUTE FUNCTION corrective_actions_append_only();

-- Row triggers don't fire on TRUNCATE.
CREATE TRIGGER corrective_actions_no_truncate BEFORE TRUNCATE ON corrective_actions
    FOR EACH STATEMENT EXECUTE FUNCTION corrective_actions_append_only();
CREATE TRIGGER corrective_action_events_no_truncate BEFORE TRUNCATE ON corrective_action_events
    FOR EACH STATEMENT EXECUTE FUNCTION corrective_actions_append_only();

ALTER TABLE corrective_actions ENABLE ROW LEVEL SECURITY;
ALTER TABLE corrective_actions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON corrective_actions FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE corrective_action_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE corrective_action_events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON corrective_action_events FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

-- Migration 000008's default privileges grant UPDATE and DELETE on new tables.
-- The advance trigger runs as the app, so it needs UPDATE on the state
-- columns (also what FOR UPDATE needs); corrective_actions_guard allows only
-- the next event's state.
REVOKE UPDATE, DELETE ON corrective_actions FROM siteproof_app;
GRANT UPDATE (status, owner_id, responsible_party, due_on, due_at, event_count) ON corrective_actions TO siteproof_app;
REVOKE UPDATE, DELETE ON corrective_action_events FROM siteproof_app;

COMMIT;
