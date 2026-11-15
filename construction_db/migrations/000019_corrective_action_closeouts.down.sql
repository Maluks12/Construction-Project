BEGIN;

-- Back to 000018: the functions 000019 replaced, as 000018 wrote them.
CREATE OR REPLACE FUNCTION corrective_actions_guard() RETURNS trigger
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

CREATE OR REPLACE FUNCTION corrective_action_events_insert() RETURNS trigger
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

CREATE OR REPLACE FUNCTION corrective_action_events_advance() RETURNS trigger
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

DROP FUNCTION action_review(uuid);
DROP FUNCTION finding_coverage(uuid, text);
DROP TABLE finding_waivers;
DROP FUNCTION finding_waivers_no_truncate();
DROP FUNCTION finding_waivers_guard();
DROP FUNCTION finding_waivers_lead();
DROP FUNCTION finding_waivers_insert();

DROP FUNCTION corrective_action_pending_closeout(corrective_action_events, uuid);
DROP INDEX corrective_action_events_decided_once;
DROP INDEX corrective_action_events_submitted_once;
ALTER TABLE corrective_action_events
    DROP CONSTRAINT corrective_action_events_closeout_fkey,
    DROP CONSTRAINT corrective_action_events_self_verified,
    DROP CONSTRAINT corrective_action_events_closeout,
    DROP CONSTRAINT corrective_action_events_reason,
    DROP CONSTRAINT corrective_action_events_kind_check,
    DROP COLUMN self_verified,
    DROP COLUMN closeout_id;
ALTER TABLE corrective_action_events
    ADD CONSTRAINT corrective_action_events_kind_check CHECK (kind IN ('raised', 'reassigned', 'due_changed', 'cancelled')),
    ADD CONSTRAINT corrective_action_events_cancel_reason CHECK (kind <> 'cancelled' OR reason IS NOT NULL);

ALTER TABLE corrective_actions
    DROP CONSTRAINT corrective_actions_awaiting,
    DROP CONSTRAINT corrective_actions_pending_closeout_fkey,
    DROP COLUMN awaiting_since,
    DROP COLUMN pending_closeout_id;

DROP TABLE corrective_action_closeout_evidence;
DROP TABLE corrective_action_closeouts;
DROP FUNCTION corrective_action_closeout_evidence_insert();
DROP FUNCTION corrective_action_closeout_complete();
DROP FUNCTION corrective_action_closeouts_insert();

COMMIT;