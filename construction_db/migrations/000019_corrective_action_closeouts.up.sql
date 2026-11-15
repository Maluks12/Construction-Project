BEGIN;

-- Close-outs, verification and waivers (Rev 4 §2.5; D1, D4, D5, D7, D8).
--   - corrective_action_closeouts: the owner's (or a lead's) claim that the
--     work is done, with at least one photo. Append-only.
--   - corrective_action_closeout_evidence: its photos: verified evidence from
--     the action's site, not older than the source, and not one of the
--     source's own photos.
--   - New events: closeout_submitted (open → awaiting_verification), verified
--     (→ closed), rejected (→ open), reopened (closed → open).
--   - finding_waivers: "no action needed" on a low or medium finding, by a
--     lead with a reason. Retracted at most once, never deleted.
--   - finding_coverage and action_review: the one implementation of D1/D7
--     coverage, which the API and the M12 summary read.

CREATE TABLE corrective_action_closeouts (
    id                  uuid PRIMARY KEY,
    org_id              uuid NOT NULL,
    site_id             uuid NOT NULL,
    action_id           uuid NOT NULL,
    note                text CHECK (char_length(note) BETWEEN 1 AND 2000),
    submitted_by        uuid NOT NULL,
    client_submitted_at timestamptz NOT NULL,
    submitted_at        timestamptz NOT NULL DEFAULT now(),
    device_id           text CHECK (octet_length(device_id) BETWEEN 1 AND 200),
    -- How many photos it has, checked at commit (as incident versions do).
    evidence_count      integer NOT NULL CHECK (evidence_count >= 1),

    FOREIGN KEY (org_id, site_id, action_id) REFERENCES corrective_actions (org_id, site_id, id),
    FOREIGN KEY (org_id, submitted_by) REFERENCES org_members (org_id, user_id),
    UNIQUE (org_id, site_id, id),
    UNIQUE (org_id, site_id, action_id, id)
);

CREATE INDEX corrective_action_closeouts_action_idx ON corrective_action_closeouts (action_id);

CREATE TABLE corrective_action_closeout_evidence (
    org_id          uuid NOT NULL,
    site_id         uuid NOT NULL,
    closeout_id     uuid NOT NULL,
    evidence_id     uuid NOT NULL,
    evidence_status text NOT NULL DEFAULT 'verified' CHECK (evidence_status = 'verified'),

    PRIMARY KEY (closeout_id, evidence_id),
    FOREIGN KEY (org_id, site_id, closeout_id) REFERENCES corrective_action_closeouts (org_id, site_id, id),
    FOREIGN KEY (org_id, site_id, evidence_id, evidence_status) REFERENCES evidence (org_id, site_id, id, status)
);

CREATE INDEX corrective_action_closeout_evidence_evidence_idx ON corrective_action_closeout_evidence (evidence_id);

-- The header gains the close-out awaiting verification, and since when (the
-- server's time it was submitted; D8). Both follow the events.
ALTER TABLE corrective_actions
    ADD COLUMN pending_closeout_id uuid,
    ADD COLUMN awaiting_since      timestamptz,
    ADD CONSTRAINT corrective_actions_pending_closeout_fkey FOREIGN KEY (org_id, site_id, id, pending_closeout_id)
        REFERENCES corrective_action_closeouts (org_id, site_id, action_id, id),
    ADD CONSTRAINT corrective_actions_awaiting CHECK (
        (status = 'awaiting_verification') = (pending_closeout_id IS NOT NULL)
        AND (pending_closeout_id IS NULL) = (awaiting_since IS NULL));

ALTER TABLE corrective_action_events DROP CONSTRAINT corrective_action_events_kind_check;
ALTER TABLE corrective_action_events DROP CONSTRAINT corrective_action_events_cancel_reason;
ALTER TABLE corrective_action_events
    ADD COLUMN closeout_id   uuid,
    ADD COLUMN self_verified boolean,
    ADD CONSTRAINT corrective_action_events_kind_check CHECK (kind IN (
        'raised', 'reassigned', 'due_changed', 'cancelled', 'closeout_submitted', 'verified', 'rejected', 'reopened')),
    ADD CONSTRAINT corrective_action_events_reason CHECK (kind NOT IN ('cancelled', 'rejected', 'reopened') OR reason IS NOT NULL),
    ADD CONSTRAINT corrective_action_events_closeout CHECK ((kind IN ('closeout_submitted', 'verified', 'rejected')) = (closeout_id IS NOT NULL)),
    ADD CONSTRAINT corrective_action_events_self_verified CHECK ((kind = 'verified') = (self_verified IS NOT NULL)),
    ADD CONSTRAINT corrective_action_events_closeout_fkey FOREIGN KEY (org_id, site_id, action_id, closeout_id)
        REFERENCES corrective_action_closeouts (org_id, site_id, action_id, id);

-- A close-out is submitted once and decided (verified or rejected) at most once.
CREATE UNIQUE INDEX corrective_action_events_submitted_once ON corrective_action_events (closeout_id) WHERE kind = 'closeout_submitted';
CREATE UNIQUE INDEX corrective_action_events_decided_once ON corrective_action_events (closeout_id) WHERE kind IN ('verified', 'rejected');

-- Every guard below applies to every role, the table owner included. Each
-- raises check_violation with its own message.

-- submitted_at is the server's time.
CREATE FUNCTION corrective_action_closeouts_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        NEW.submitted_at := now();
        IF NEW.client_submitted_at > NEW.submitted_at + interval '5 minutes' THEN
            RAISE EXCEPTION 'close-out %: client_submitted_at is in the future', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER corrective_action_closeouts_insert BEFORE INSERT ON corrective_action_closeouts
    FOR EACH ROW EXECUTE FUNCTION corrective_action_closeouts_insert();

-- At commit a close-out has exactly evidence_count photos and its
-- closeout_submitted event. Fired by inserts into both tables, so a photo
-- added to a committed close-out breaks its count and is refused.
CREATE FUNCTION corrective_action_closeout_complete() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        cid    uuid;
        c      corrective_action_closeouts;
        photos integer;
    BEGIN
        IF TG_TABLE_NAME = 'corrective_action_closeouts' THEN
            cid := NEW.id;
        ELSE
            cid := NEW.closeout_id;
        END IF;
        SELECT * INTO c FROM corrective_action_closeouts WHERE id = cid;
        IF NOT FOUND THEN
            RETURN NULL; -- the foreign key reports it
        END IF;
        SELECT count(*) INTO photos FROM corrective_action_closeout_evidence WHERE closeout_id = c.id;
        IF photos <> c.evidence_count THEN
            RAISE EXCEPTION 'close-out %: has % photos but evidence_count is %', c.id, photos, c.evidence_count
                USING ERRCODE = 'check_violation';
        END IF;
        IF NOT EXISTS (SELECT 1 FROM corrective_action_events WHERE closeout_id = c.id AND kind = 'closeout_submitted') THEN
            RAISE EXCEPTION 'close-out %: has no closeout_submitted event', c.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

CREATE CONSTRAINT TRIGGER corrective_action_closeouts_complete AFTER INSERT ON corrective_action_closeouts
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION corrective_action_closeout_complete();
CREATE CONSTRAINT TRIGGER corrective_action_closeout_evidence_complete AFTER INSERT ON corrective_action_closeout_evidence
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION corrective_action_closeout_complete();

-- A close-out photo must be able to show the fix (D5): not taken before the
-- source (5 minutes for clock skew), and not one of the source's own photos.
CREATE FUNCTION corrective_action_closeout_evidence_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        a        corrective_actions;
        captured timestamptz;
    BEGIN
        SELECT ca.* INTO a FROM corrective_action_closeouts c JOIN corrective_actions ca ON ca.id = c.action_id
        WHERE c.id = NEW.closeout_id;
        IF NOT FOUND THEN
            RETURN NEW; -- the foreign key (or RLS) reports it
        END IF;
        SELECT client_captured_at INTO captured FROM evidence WHERE id = NEW.evidence_id;
        IF captured < a.source_at - interval '5 minutes' THEN
            RAISE EXCEPTION 'close-out %: photo % was taken before the action''s source', NEW.closeout_id, NEW.evidence_id
                USING ERRCODE = 'check_violation';
        END IF;
        IF (a.inspection_id IS NOT NULL AND EXISTS (
                SELECT 1 FROM inspection_response_evidence
                WHERE inspection_id = a.inspection_id AND item_key = a.item_key AND evidence_id = NEW.evidence_id))
           OR (a.incident_id IS NOT NULL AND EXISTS (
                SELECT 1 FROM incident_version_evidence WHERE incident_id = a.incident_id AND evidence_id = NEW.evidence_id)) THEN
            RAISE EXCEPTION 'close-out %: photo % is one of the source''s own photos', NEW.closeout_id, NEW.evidence_id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER corrective_action_closeout_evidence_insert BEFORE INSERT ON corrective_action_closeout_evidence
    FOR EACH ROW EXECUTE FUNCTION corrective_action_closeout_evidence_insert();

-- The close-out a header awaits after an event: set by closeout_submitted,
-- kept by events that leave the action awaiting (a reassignment), cleared by
-- anything else.
CREATE FUNCTION corrective_action_pending_closeout(e corrective_action_events, previous uuid) RETURNS uuid
    LANGUAGE sql IMMUTABLE
    AS $$
        SELECT CASE
            WHEN e.status <> 'awaiting_verification' THEN NULL
            WHEN e.kind = 'closeout_submitted' THEN e.closeout_id
            ELSE previous
        END
    $$;

-- 000018's guard, now also checking the pending close-out and awaiting_since.
CREATE OR REPLACE FUNCTION corrective_actions_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        rest    corrective_actions;
        e       corrective_action_events;
        pending uuid;
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
        rest.pending_closeout_id := OLD.pending_closeout_id;
        rest.awaiting_since := OLD.awaiting_since;
        IF rest IS DISTINCT FROM OLD OR NEW.event_count <> OLD.event_count + 1 THEN
            RAISE EXCEPTION 'corrective action %: an action changes only by a new event', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        SELECT * INTO e FROM corrective_action_events WHERE action_id = OLD.id AND seq = NEW.event_count;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'corrective action %: an action changes only by a new event', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        pending := corrective_action_pending_closeout(e, OLD.pending_closeout_id);
        IF (NEW.status, NEW.owner_id, NEW.responsible_party, NEW.due_on, NEW.due_at, NEW.pending_closeout_id, NEW.awaiting_since)
           IS DISTINCT FROM (e.status, e.owner_id, e.responsible_party, e.due_on, action_due_at(e.due_on, OLD.time_zone), pending,
                             (SELECT submitted_at FROM corrective_action_closeouts WHERE id = pending)) THEN
            RAISE EXCEPTION 'corrective action %: an action changes only by a new event', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

-- 000018's event check, with the close-out kinds.
CREATE OR REPLACE FUNCTION corrective_action_events_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        a    corrective_actions;
        c    corrective_action_closeouts;
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

        IF a.status = 'cancelled' OR (a.status = 'closed' AND NEW.kind <> 'reopened') THEN
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
        WHEN 'closeout_submitted' THEN
            IF a.status <> 'open' THEN
                RAISE EXCEPTION 'corrective action %: a close-out is already awaiting verification', a.id
                    USING ERRCODE = 'check_violation';
            END IF;
            IF (NEW.status, NEW.owner_id, NEW.responsible_party, NEW.due_on)
               IS DISTINCT FROM ('awaiting_verification'::text, a.owner_id, a.responsible_party, a.due_on) THEN
                RAISE EXCEPTION 'corrective action %: a close-out changes the status to awaiting_verification, and nothing else', a.id
                    USING ERRCODE = 'check_violation';
            END IF;
            IF NOT lead AND NEW.actor_id <> a.owner_id THEN
                RAISE EXCEPTION 'corrective action %: only its owner, an owner or a SHE officer can close it out', a.id
                    USING ERRCODE = 'check_violation';
            END IF;
            SELECT * INTO c FROM corrective_action_closeouts WHERE id = NEW.closeout_id;
            IF FOUND AND (c.submitted_by <> NEW.actor_id OR c.client_submitted_at <> NEW.client_at) THEN
                RAISE EXCEPTION 'corrective action %: the close-out event must record its close-out as submitted', a.id
                    USING ERRCODE = 'check_violation';
            END IF;
        WHEN 'verified', 'rejected' THEN
            IF a.status <> 'awaiting_verification' THEN
                RAISE EXCEPTION 'corrective action %: has no close-out awaiting verification', a.id
                    USING ERRCODE = 'check_violation';
            END IF;
            IF NEW.closeout_id IS DISTINCT FROM a.pending_closeout_id THEN
                RAISE EXCEPTION 'corrective action %: only the close-out awaiting verification can be verified or rejected', a.id
                    USING ERRCODE = 'check_violation';
            END IF;
            IF (NEW.status, NEW.owner_id, NEW.responsible_party, NEW.due_on)
               IS DISTINCT FROM (CASE NEW.kind WHEN 'verified' THEN 'closed' ELSE 'open' END, a.owner_id, a.responsible_party, a.due_on) THEN
                RAISE EXCEPTION 'corrective action %: verifying closes it and rejecting reopens it, and nothing else', a.id
                    USING ERRCODE = 'check_violation';
            END IF;
            IF NOT lead THEN
                RAISE EXCEPTION 'corrective action %: only an owner or a SHE officer can verify or reject a close-out', a.id
                    USING ERRCODE = 'check_violation';
            END IF;
            IF NEW.kind = 'verified' THEN
                SELECT * INTO c FROM corrective_action_closeouts WHERE id = NEW.closeout_id;
                IF NEW.self_verified IS DISTINCT FROM (c.submitted_by = NEW.actor_id) THEN
                    RAISE EXCEPTION 'corrective action %: self_verified must say whether the verifier submitted the close-out', a.id
                        USING ERRCODE = 'check_violation';
                END IF;
            END IF;
        WHEN 'reopened' THEN
            IF a.status <> 'closed' THEN
                RAISE EXCEPTION 'corrective action %: only a closed action can be reopened', a.id
                    USING ERRCODE = 'check_violation';
            END IF;
            IF (NEW.status, NEW.owner_id, NEW.responsible_party, NEW.due_on)
               IS DISTINCT FROM ('open'::text, a.owner_id, a.responsible_party, a.due_on) THEN
                RAISE EXCEPTION 'corrective action %: reopening changes the status to open, and nothing else', a.id
                    USING ERRCODE = 'check_violation';
            END IF;
            IF NOT lead THEN
                RAISE EXCEPTION 'corrective action %: only an owner or a SHE officer can reopen it', a.id
                    USING ERRCODE = 'check_violation';
            END IF;
        END CASE;
        RETURN NEW;
    END
    $$;

-- 000018's advance, now also setting the pending close-out and awaiting_since.
CREATE OR REPLACE FUNCTION corrective_action_events_advance() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        UPDATE corrective_actions
        SET status = NEW.status, owner_id = NEW.owner_id, responsible_party = NEW.responsible_party,
            due_on = NEW.due_on, due_at = action_due_at(NEW.due_on, time_zone), event_count = NEW.seq,
            pending_closeout_id = corrective_action_pending_closeout(NEW, pending_closeout_id),
            awaiting_since = (SELECT submitted_at FROM corrective_action_closeouts
                              WHERE id = corrective_action_pending_closeout(NEW, pending_closeout_id))
        WHERE id = NEW.action_id AND event_count = NEW.seq - 1;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'corrective action %: event % is not the next event', NEW.action_id, NEW.seq
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

CREATE TRIGGER corrective_action_closeouts_append_only BEFORE UPDATE OR DELETE ON corrective_action_closeouts
    FOR EACH ROW EXECUTE FUNCTION corrective_actions_append_only();
CREATE TRIGGER corrective_action_closeout_evidence_append_only BEFORE UPDATE OR DELETE ON corrective_action_closeout_evidence
    FOR EACH ROW EXECUTE FUNCTION corrective_actions_append_only();
CREATE TRIGGER corrective_action_closeouts_no_truncate BEFORE TRUNCATE ON corrective_action_closeouts
    FOR EACH STATEMENT EXECUTE FUNCTION corrective_actions_append_only();
CREATE TRIGGER corrective_action_closeout_evidence_no_truncate BEFORE TRUNCATE ON corrective_action_closeout_evidence
    FOR EACH STATEMENT EXECUTE FUNCTION corrective_actions_append_only();

-- "No action needed" on a low or medium finding (D1): only by a lead, with a
-- reason; never on a finding superseded by a correction. A retraction (by a
-- lead, with a reason) is the only change, once.
CREATE TABLE finding_waivers (
    id              uuid PRIMARY KEY,
    org_id          uuid NOT NULL,
    site_id         uuid NOT NULL,
    inspection_id   uuid NOT NULL,
    item_key        text NOT NULL,
    severity        text NOT NULL CHECK (severity IN ('low', 'medium')),
    reason          text NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 500),
    waived_by       uuid NOT NULL,
    client_waived_at timestamptz NOT NULL,
    waived_at       timestamptz NOT NULL DEFAULT now(),
    device_id       text CHECK (octet_length(device_id) BETWEEN 1 AND 200),
    retracted_by    uuid,
    retracted_at    timestamptz,
    retract_reason  text CHECK (char_length(retract_reason) BETWEEN 1 AND 500),

    -- The finding, with its severity: a high or critical finding can't match.
    FOREIGN KEY (org_id, site_id, inspection_id, item_key, severity)
        REFERENCES inspection_findings (org_id, site_id, inspection_id, item_key, severity),
    FOREIGN KEY (org_id, waived_by) REFERENCES org_members (org_id, user_id),
    FOREIGN KEY (org_id, retracted_by) REFERENCES org_members (org_id, user_id),

    CONSTRAINT finding_waivers_retraction CHECK (
        (retracted_at IS NULL) = (retracted_by IS NULL)
        AND (retracted_at IS NULL) = (retract_reason IS NULL))
);

-- One live waiver per finding.
CREATE UNIQUE INDEX finding_waivers_one_live ON finding_waivers (inspection_id, item_key) WHERE retracted_at IS NULL;
CREATE INDEX finding_waivers_site_idx ON finding_waivers (site_id);

CREATE FUNCTION finding_waivers_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        site_status       text;
        correction_status text;
    BEGIN
        NEW.waived_at := now();
        IF NEW.retracted_by IS NOT NULL OR NEW.retracted_at IS NOT NULL OR NEW.retract_reason IS NOT NULL THEN
            RAISE EXCEPTION 'waiver %: a new waiver is not retracted', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.client_waived_at > NEW.waived_at + interval '5 minutes' THEN
            RAISE EXCEPTION 'waiver %: client_waived_at is in the future', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        SELECT status INTO site_status FROM sites WHERE id = NEW.site_id;
        IF site_status = 'archived' THEN
            RAISE EXCEPTION 'waiver %: its site is archived', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        -- As for raising an action: FOR SHARE orders this against completing
        -- a correction of the finding's inspection.
        SELECT status INTO correction_status FROM inspections
        WHERE corrects_id = NEW.inspection_id AND status <> 'abandoned'
        FOR SHARE;
        IF correction_status = 'completed' THEN
            RAISE EXCEPTION 'waiver %: the finding is superseded by a correction', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER finding_waivers_insert BEFORE INSERT ON finding_waivers
    FOR EACH ROW EXECUTE FUNCTION finding_waivers_insert();

-- Checked after the insert, so a row for another organisation is refused by
-- RLS first (its members are invisible, so it would read as "not a lead").
CREATE FUNCTION finding_waivers_lead() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF NOT action_lead(NEW.org_id, NEW.waived_by) THEN
            RAISE EXCEPTION 'waiver %: only an owner or a SHE officer can waive a finding', NEW.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

CREATE TRIGGER finding_waivers_lead AFTER INSERT ON finding_waivers
    FOR EACH ROW EXECUTE FUNCTION finding_waivers_lead();

CREATE FUNCTION finding_waivers_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        rest finding_waivers;
    BEGIN
        IF TG_OP = 'DELETE' THEN
            RAISE EXCEPTION 'waiver %: waivers are never deleted; retract it instead', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW IS NOT DISTINCT FROM OLD THEN
            RETURN NEW;
        END IF;
        rest := NEW;
        rest.retracted_by := OLD.retracted_by;
        rest.retracted_at := OLD.retracted_at;
        rest.retract_reason := OLD.retract_reason;
        IF rest IS DISTINCT FROM OLD THEN
            RAISE EXCEPTION 'waiver %: the only change is retracting it', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF OLD.retracted_at IS NOT NULL THEN
            RAISE EXCEPTION 'waiver %: already retracted', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        NEW.retracted_at := now();
        IF NOT action_lead(OLD.org_id, NEW.retracted_by) THEN
            RAISE EXCEPTION 'waiver %: only an owner or a SHE officer can retract a waiver', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER finding_waivers_guard BEFORE UPDATE OR DELETE ON finding_waivers
    FOR EACH ROW EXECUTE FUNCTION finding_waivers_guard();

CREATE FUNCTION finding_waivers_no_truncate() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        RAISE EXCEPTION '% is append-only; retract a waiver instead', TG_TABLE_NAME
            USING ERRCODE = 'check_violation';
    END
    $$;

CREATE TRIGGER finding_waivers_no_truncate BEFORE TRUNCATE ON finding_waivers
    FOR EACH STATEMENT EXECUTE FUNCTION finding_waivers_no_truncate();

-- How a finding is addressed (D1, D7): superseded (its inspection has a
-- completed correction), actioned, waived, or unactioned. Actions and live
-- waivers count from this finding and from the same item earlier in its
-- correction chain, but an earlier one counts only if its severity is the
-- same or higher than this finding's: a correction that raises the severity
-- needs its own action. A cancelled action never counts. No row when the
-- item has no finding.
CREATE FUNCTION finding_coverage(p_inspection uuid, p_item text)
    RETURNS TABLE (coverage text, action_ids uuid[], waiver_id uuid)
    LANGUAGE sql STABLE
    AS $$
        WITH RECURSIVE chain (id) AS (
            SELECT p_inspection
            UNION ALL
            SELECT i.corrects_id FROM inspections i JOIN chain c ON i.id = c.id WHERE i.corrects_id IS NOT NULL
        ),
        this AS (
            SELECT severity FROM inspection_findings WHERE inspection_id = p_inspection AND item_key = p_item
        ),
        acts AS (
            SELECT array_agg(a.id ORDER BY a.raised_at, a.id) AS ids
            FROM corrective_actions a, this
            WHERE a.inspection_id IN (SELECT id FROM chain) AND a.item_key = p_item AND a.status <> 'cancelled'
              AND severity_rank(a.severity) >= severity_rank(this.severity)
        ),
        waiver AS (
            SELECT w.id
            FROM finding_waivers w, this
            WHERE w.inspection_id IN (SELECT id FROM chain) AND w.item_key = p_item AND w.retracted_at IS NULL
              AND severity_rank(w.severity) >= severity_rank(this.severity)
            ORDER BY w.waived_at DESC, w.id
            LIMIT 1
        )
        SELECT CASE
                   WHEN EXISTS (SELECT 1 FROM inspections WHERE corrects_id = p_inspection AND status = 'completed') THEN 'superseded'
                   WHEN acts.ids IS NOT NULL THEN 'actioned'
                   WHEN EXISTS (SELECT 1 FROM waiver) THEN 'waived'
                   ELSE 'unactioned'
               END,
               COALESCE(acts.ids, '{}'),
               (SELECT id FROM waiver)
        FROM this, acts
    $$;

-- Whether a lead should look at an action again because its finding was
-- corrected (D7): against the newest completed correction in the chain,
-- 'item_not_failed' if that correction doesn't fail the item, and
-- 'severity_raised' if it fails it at a higher severity. Null otherwise, and
-- for incident actions.
CREATE FUNCTION action_review(p_action uuid) RETURNS text
    LANGUAGE sql STABLE
    AS $$
        WITH RECURSIVE a AS (
            SELECT inspection_id, item_key, severity FROM corrective_actions WHERE id = p_action AND inspection_id IS NOT NULL
        ),
        down (id, depth) AS (
            SELECT i.id, 1 FROM inspections i, a WHERE i.corrects_id = a.inspection_id AND i.status = 'completed'
            UNION ALL
            SELECT i.id, d.depth + 1 FROM inspections i JOIN down d ON i.corrects_id = d.id WHERE i.status = 'completed'
        ),
        newest AS (
            SELECT id FROM down ORDER BY depth DESC LIMIT 1
        )
        SELECT CASE
                   WHEN r.result IS DISTINCT FROM 'fail' THEN 'item_not_failed'
                   WHEN severity_rank(r.severity) > severity_rank(a.severity) THEN 'severity_raised'
               END
        FROM a CROSS JOIN newest
        LEFT JOIN inspection_responses r ON r.inspection_id = newest.id AND r.item_key = a.item_key
    $$;

ALTER TABLE corrective_action_closeouts ENABLE ROW LEVEL SECURITY;
ALTER TABLE corrective_action_closeouts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON corrective_action_closeouts FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE corrective_action_closeout_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE corrective_action_closeout_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON corrective_action_closeout_evidence FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE finding_waivers ENABLE ROW LEVEL SECURITY;
ALTER TABLE finding_waivers FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON finding_waivers FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

GRANT UPDATE (pending_closeout_id, awaiting_since) ON corrective_actions TO siteproof_app;
REVOKE UPDATE, DELETE ON corrective_action_closeouts FROM siteproof_app;
REVOKE UPDATE, DELETE ON corrective_action_closeout_evidence FROM siteproof_app;
REVOKE UPDATE, DELETE ON finding_waivers FROM siteproof_app;
GRANT UPDATE (retracted_by, retracted_at, retract_reason) ON finding_waivers TO siteproof_app;

COMMIT;
