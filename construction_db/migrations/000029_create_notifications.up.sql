BEGIN;

-- Reminders (M11 D22–D26) and the holiday recompute (BUILD_PLAN M11).
--
-- Redis decides when the worker runs; Postgres decides whether anything is
-- sent. A reminder is a notifications row, unique per (recipient, kind,
-- subject, threshold): a restart or a second worker inserting the same one
-- inserts nothing. Email goes through an outbox row that is claimed and
-- committed as 'sending' before the SMTP call, and is never sent again from
-- 'sending': at most once.

CREATE TABLE notifications (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       uuid NOT NULL,
    user_id      uuid NOT NULL,
    kind         text NOT NULL CHECK (kind IN ('incident_deadline', 'action_overdue', 'verification_stalled', 'competency_expiry',
                                               'medical_expiry', 'appointment_expiry', 'document_expiry')),
    subject_type text NOT NULL CHECK (subject_type IN ('deadline_duty', 'action', 'closeout', 'competency', 'medical', 'appointment', 'document')),
    subject_id   uuid NOT NULL,
    -- Which reminder of the subject: before_30d, on_day, overdue,
    -- escalation_2, stalled, ...; a document's carry its version (v2_on_day).
    threshold    text NOT NULL CHECK (threshold ~ '^[a-z0-9_]+$'),
    site_id      uuid,
    title        text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 300),
    due_on       date,
    due_at       timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    read_at      timestamptz,

    FOREIGN KEY (org_id, user_id) REFERENCES org_members (org_id, user_id),
    FOREIGN KEY (org_id, site_id) REFERENCES sites (org_id, id),
    UNIQUE (org_id, id),
    CONSTRAINT notifications_once UNIQUE (user_id, kind, subject_type, subject_id, threshold)
);

CREATE INDEX notifications_user_idx ON notifications (user_id, created_at DESC);

-- The one change: its recipient reads it, once.
CREATE FUNCTION notifications_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        rest notifications;
    BEGIN
        IF TG_OP = 'DELETE' THEN
            RAISE EXCEPTION 'notification %: never deleted', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        rest := NEW;
        rest.read_at := OLD.read_at;
        IF rest IS DISTINCT FROM OLD OR OLD.read_at IS NOT NULL OR NEW.read_at IS NULL THEN
            RAISE EXCEPTION 'notification %: the only change is reading it, once', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF app_user_id() IS DISTINCT FROM OLD.user_id THEN
            RAISE EXCEPTION 'notification %: only its recipient reads it', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER notifications_guard BEFORE UPDATE OR DELETE ON notifications
    FOR EACH ROW EXECUTE FUNCTION notifications_guard();

CREATE TABLE notification_emails (
    notification_id uuid PRIMARY KEY,
    org_id          uuid NOT NULL,
    to_address      text NOT NULL CHECK (char_length(to_address) BETWEEN 3 AND 320),
    status          text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sending', 'sent', 'failed', 'cancelled', 'unknown')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    claimed_at      timestamptz,
    finished_at     timestamptz,
    error           text CHECK (char_length(error) <= 1000),

    FOREIGN KEY (org_id, notification_id) REFERENCES notifications (org_id, id)
);

CREATE INDEX notification_emails_pending_idx ON notification_emails (created_at) WHERE status IN ('pending', 'sending');

-- One way only: pending → sending or cancelled; sending → sent, failed or
-- unknown (a send whose result was never recorded). Nothing goes back to
-- pending, so nothing is sent twice.
CREATE FUNCTION notification_emails_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF TG_OP = 'DELETE' THEN
            RAISE EXCEPTION 'email %: never deleted', OLD.notification_id
                USING ERRCODE = 'check_violation';
        END IF;
        IF (NEW.notification_id, NEW.org_id, NEW.to_address, NEW.created_at)
           IS DISTINCT FROM (OLD.notification_id, OLD.org_id, OLD.to_address, OLD.created_at) THEN
            RAISE EXCEPTION 'email %: only its status changes', OLD.notification_id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.status = OLD.status THEN
            IF NEW IS DISTINCT FROM OLD THEN
                RAISE EXCEPTION 'email %: only its status changes', OLD.notification_id
                    USING ERRCODE = 'check_violation';
            END IF;
            RETURN NEW;
        END IF;
        IF NOT ((OLD.status = 'pending' AND NEW.status IN ('sending', 'cancelled'))
                OR (OLD.status = 'sending' AND NEW.status IN ('sent', 'failed', 'unknown'))) THEN
            RAISE EXCEPTION 'email %: % can''t become %', OLD.notification_id, OLD.status, NEW.status
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.status = 'sending' THEN
            NEW.claimed_at := clock_timestamp();
        ELSE
            NEW.finished_at := clock_timestamp();
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER notification_emails_guard BEFORE UPDATE OR DELETE ON notification_emails
    FOR EACH ROW EXECUTE FUNCTION notification_emails_guard();

-- Every run of a worker job (Rev 4 §2.3: alert on non-completion, not only
-- failure). System data: no org_id, on the RLS allowlist.
CREATE TABLE job_runs (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    job         text NOT NULL CHECK (job ~ '^[a-z0-9_]+$'),
    instance    text NOT NULL CHECK (char_length(instance) BETWEEN 1 AND 200),
    started_at  timestamptz NOT NULL DEFAULT clock_timestamp(),
    finished_at timestamptz,
    ok          boolean,
    error       text CHECK (char_length(error) <= 2000),
    counts      jsonb,
    CONSTRAINT job_runs_finished CHECK ((finished_at IS NULL) = (ok IS NULL))
);

CREATE INDEX job_runs_job_idx ON job_runs (job, started_at DESC);

CREATE FUNCTION job_runs_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF TG_OP = 'DELETE' THEN
            RAISE EXCEPTION 'job run %: never deleted', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF OLD.finished_at IS NOT NULL OR (NEW.id, NEW.job, NEW.instance, NEW.started_at) IS DISTINCT FROM (OLD.id, OLD.job, OLD.instance, OLD.started_at) THEN
            RAISE EXCEPTION 'job run %: finished once, nothing else changes', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        NEW.finished_at := clock_timestamp();
        RETURN NEW;
    END
    $$;

CREATE TRIGGER job_runs_guard BEFORE UPDATE OR DELETE ON job_runs
    FOR EACH ROW EXECUTE FUNCTION job_runs_guard();

-- The worker works organisation by organisation under RLS; this is the
-- only thing it reads across them.
CREATE FUNCTION worker_org_ids() RETURNS SETOF uuid
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path = pg_catalog, public
    AS $$ SELECT id FROM organisations ORDER BY id $$;

REVOKE ALL ON FUNCTION worker_org_ids() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION worker_org_ids() TO siteproof_app;

-- The latest lead time crossed: days_left at or under a lead; the smallest
-- such lead names the reminder (0 is "on the day"). NULL before the first.
CREATE FUNCTION notification_lead(p_days_left integer, p_leads integer[], p_prefix text) RETURNS text
    LANGUAGE sql IMMUTABLE
    AS $$
        SELECT CASE WHEN m IS NULL THEN NULL
                    WHEN m = 0 THEN p_prefix || 'on_day'
                    ELSE p_prefix || 'before_' || m || 'd' END
        FROM (SELECT min(l) AS m FROM unnest(p_leads) l WHERE p_days_left <= l) x
    $$;

-- Active owners and SHE officers of the tenant's organisation.
CREATE FUNCTION notification_leads() RETURNS SETOF uuid
    LANGUAGE sql STABLE
    AS $$ SELECT user_id FROM org_members WHERE org_id = app_org_id() AND status = 'active' AND role IN ('owner', 'she_officer') $$;

CREATE FUNCTION notification_site_supervisors(p_site uuid) RETURNS SETOF uuid
    LANGUAGE sql STABLE
    AS $$
        SELECT sm.user_id FROM site_members sm
        JOIN org_members om ON om.org_id = sm.org_id AND om.user_id = sm.user_id
        WHERE sm.site_id = p_site AND om.role = 'supervisor' AND om.status = 'active'
    $$;

-- Everything due to be told at p_now, for the tenant's organisation and one
-- regime's subjects, with the regime's lead times (reminders.json). One row
-- per recipient and reminder; the worker inserts them ON CONFLICT DO NOTHING.
-- Only current subjects appear: a superseded or retracted record, a
-- satisfied deadline, a closed action or an earlier document version never
-- does (a renewal cancels the old one's reminders).
CREATE FUNCTION notifications_due(
    p_now timestamptz, p_regime text, p_time_zone text,
    p_worker_days integer[], p_company_days integer[], p_deadline_days integer[], p_escalation integer[], p_stalled_days integer)
    RETURNS TABLE (user_id uuid, kind text, subject_type text, subject_id uuid, threshold text, site_id uuid, title text,
                   due_on date, due_at timestamptz)
    LANGUAGE sql STABLE
    AS $$
    WITH today AS (SELECT (p_now AT TIME ZONE p_time_zone)::date AS d),
    dl AS (
        SELECT d.duty_id, d.site_id, d.rule_label, d.due_on, d.due_at,
               CASE WHEN d.unit = 'immediately' THEN 'forthwith'
                    WHEN p_now >= d.due_at THEN 'overdue'
                    WHEN d.due_on IS NOT NULL THEN notification_lead(d.due_on - (p_now AT TIME ZONE d.time_zone)::date, p_deadline_days, '')
                    ELSE 'open' END AS threshold,
               (SELECT v.submitted_by FROM incidents i JOIN incident_versions v ON v.incident_id = i.id AND v.version = i.latest_version
                WHERE i.id = d.incident_id) AS submitter
        FROM deadlines d
        JOIN deadline_duties du ON du.id = d.duty_id
        JOIN regulatory_regimes rr ON rr.id = d.regime_id
        WHERE d.closed_as IS NULL AND du.satisfaction_id IS NULL AND rr.code = p_regime
    ),
    act AS (
        SELECT a.id, a.site_id, a.title, a.due_on, a.due_at, a.owner_id, a.raised_by,
               (p_now AT TIME ZONE a.time_zone)::date - (a.due_on + 1) AS days_over
        FROM corrective_actions a
        JOIN sites s ON s.id = a.site_id
        JOIN regulatory_regimes rr ON rr.id = s.regime_id
        WHERE a.status = 'open' AND p_now >= a.due_at AND rr.code = p_regime
    ),
    stalled AS (
        SELECT a.pending_closeout_id, a.site_id, a.title
        FROM corrective_actions a
        JOIN sites s ON s.id = a.site_id
        JOIN regulatory_regimes rr ON rr.id = s.regime_id
        WHERE a.status = 'awaiting_verification' AND a.awaiting_since <= p_now - make_interval(days => p_stalled_days) AND rr.code = p_regime
    ),
    comp AS (
        SELECT c.id, c.worker_id, c.type_key, c.expires_on, notification_lead(c.expires_on - (SELECT d FROM today), p_worker_days, '') AS threshold
        FROM worker_competencies c
        WHERE c.regime_code = p_regime AND c.expires_on IS NOT NULL AND c.retracted_at IS NULL
          AND NOT EXISTS (SELECT 1 FROM worker_competencies s WHERE s.supersedes_id = c.id AND s.retracted_at IS NULL)
    ),
    med AS (
        SELECT m.id, m.worker_id, m.expires_on, notification_lead(m.expires_on - (SELECT d FROM today), p_worker_days, '') AS threshold
        FROM worker_medicals m
        WHERE m.regime_code = p_regime AND m.retracted_at IS NULL
          AND NOT EXISTS (SELECT 1 FROM worker_medicals s WHERE s.supersedes_id = m.id AND s.retracted_at IS NULL)
    ),
    appt AS (
        SELECT a.id, a.site_id, a.type_key, a.expires_on, notification_lead(a.expires_on - (SELECT d FROM today), p_worker_days, '') AS threshold
        FROM site_appointments a
        WHERE a.regime_code = p_regime AND a.expires_on IS NOT NULL AND a.retracted_at IS NULL
          AND NOT EXISTS (SELECT 1 FROM site_appointments s WHERE s.supersedes_id = a.id AND s.retracted_at IS NULL)
    ),
    doc AS (
        SELECT d.id, d.site_id, d.title, v.expires_on,
               notification_lead(v.expires_on - (SELECT d FROM today),
                                 CASE WHEN d.scope = 'org' THEN p_company_days ELSE p_worker_days END, 'v' || v.version || '_') AS threshold
        FROM documents d JOIN document_versions v ON v.document_id = d.id AND v.version = d.latest_version
        WHERE d.regime_code = p_regime AND v.expires_on IS NOT NULL
    ),
    -- Workers' supervisors: of every site where the worker is on the roster.
    worker_sups AS (
        SELECT DISTINCT r.worker_id, s.user_id
        FROM site_roster r, LATERAL notification_site_supervisors(r.site_id) s(user_id)
        WHERE r.removed_at IS NULL
    )
    SELECT r.user_id, 'incident_deadline', 'deadline_duty', dl.duty_id, dl.threshold, dl.site_id, dl.rule_label, dl.due_on, dl.due_at
    FROM dl, LATERAL (SELECT dl.submitter UNION SELECT notification_leads() UNION SELECT notification_site_supervisors(dl.site_id)) r(user_id)
    WHERE dl.threshold IS NOT NULL AND r.user_id IS NOT NULL
    UNION ALL
    SELECT r.user_id, 'action_overdue', 'action', act.id, 'escalation_' || e.step, act.site_id, act.title, act.due_on, act.due_at
    FROM act
    JOIN LATERAL (SELECT 1 AS step WHERE act.days_over >= p_escalation[1]
                  UNION ALL SELECT 2 WHERE act.days_over >= p_escalation[2]
                  UNION ALL SELECT 3 WHERE act.days_over >= p_escalation[3]) e ON true
    JOIN LATERAL (SELECT act.owner_id WHERE e.step = 1 UNION SELECT act.raised_by WHERE e.step = 1
                  UNION SELECT user_id FROM org_members WHERE e.step = 2 AND org_id = app_org_id() AND status = 'active' AND role = 'she_officer'
                  UNION SELECT user_id FROM org_members WHERE e.step = 3 AND org_id = app_org_id() AND status = 'active' AND role = 'owner') r(user_id) ON true
    UNION ALL
    SELECT r.user_id, 'verification_stalled', 'closeout', stalled.pending_closeout_id, 'stalled', stalled.site_id, stalled.title, NULL::date, NULL::timestamptz
    FROM stalled, LATERAL notification_leads() r(user_id)
    UNION ALL
    SELECT r.user_id, 'competency_expiry', 'competency', comp.id, comp.threshold, NULL::uuid, comp.type_key, comp.expires_on, NULL::timestamptz
    FROM comp, LATERAL (SELECT notification_leads() UNION SELECT ws.user_id FROM worker_sups ws WHERE ws.worker_id = comp.worker_id) r(user_id)
    WHERE comp.threshold IS NOT NULL
    UNION ALL
    SELECT r.user_id, 'medical_expiry', 'medical', med.id, med.threshold, NULL::uuid, 'Medical certificate of fitness', med.expires_on, NULL::timestamptz
    FROM med, LATERAL (SELECT notification_leads() UNION SELECT ws.user_id FROM worker_sups ws WHERE ws.worker_id = med.worker_id) r(user_id)
    WHERE med.threshold IS NOT NULL
    UNION ALL
    SELECT r.user_id, 'appointment_expiry', 'appointment', appt.id, appt.threshold, appt.site_id, appt.type_key, appt.expires_on, NULL::timestamptz
    FROM appt, LATERAL (SELECT notification_leads() UNION SELECT notification_site_supervisors(appt.site_id)) r(user_id)
    WHERE appt.threshold IS NOT NULL
    UNION ALL
    SELECT r.user_id, 'document_expiry', 'document', doc.id, doc.threshold, doc.site_id, doc.title, doc.expires_on, NULL::timestamptz
    FROM doc, LATERAL (SELECT notification_leads() UNION SELECT notification_site_supervisors(doc.site_id) WHERE doc.site_id IS NOT NULL) r(user_id)
    WHERE doc.threshold IS NOT NULL
    $$;

-- Whether a notification's subject still stands as it did: the sender
-- cancels the email of one that doesn't (a renewal, a retraction, a
-- satisfaction, a close), and the inbox shows it.
CREATE FUNCTION notification_subject_current(p_type text, p_id uuid, p_threshold text) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
        SELECT CASE p_type
            WHEN 'deadline_duty' THEN EXISTS (SELECT 1 FROM deadline_duties du JOIN deadlines d ON d.duty_id = du.id AND d.closed_as IS NULL
                                              WHERE du.id = p_id AND du.satisfaction_id IS NULL)
            WHEN 'action' THEN EXISTS (SELECT 1 FROM corrective_actions WHERE id = p_id AND status = 'open')
            WHEN 'closeout' THEN EXISTS (SELECT 1 FROM corrective_actions WHERE pending_closeout_id = p_id)
            WHEN 'competency' THEN EXISTS (SELECT 1 FROM worker_competencies c WHERE c.id = p_id AND c.retracted_at IS NULL
                                           AND NOT EXISTS (SELECT 1 FROM worker_competencies s WHERE s.supersedes_id = c.id AND s.retracted_at IS NULL))
            WHEN 'medical' THEN EXISTS (SELECT 1 FROM worker_medicals m WHERE m.id = p_id AND m.retracted_at IS NULL
                                        AND NOT EXISTS (SELECT 1 FROM worker_medicals s WHERE s.supersedes_id = m.id AND s.retracted_at IS NULL))
            WHEN 'appointment' THEN EXISTS (SELECT 1 FROM site_appointments a WHERE a.id = p_id AND a.retracted_at IS NULL
                                            AND NOT EXISTS (SELECT 1 FROM site_appointments s WHERE s.supersedes_id = a.id AND s.retracted_at IS NULL))
            WHEN 'document' THEN EXISTS (SELECT 1 FROM documents d WHERE d.id = p_id
                                         AND d.latest_version = substring(p_threshold FROM '^v([0-9]+)_')::integer)
        END
    $$;

-- The holiday recompute. A recomputation closes a live deadline as
-- 'recomputed' (no incident version: the reason is the holiday) and inserts
-- its replacement computed by deadline_due with the same inputs. Closed and
-- satisfied deadlines never change.
ALTER TABLE deadlines DROP CONSTRAINT deadlines_closed_as_check;
ALTER TABLE deadlines ADD CONSTRAINT deadlines_closed_as_check CHECK (closed_as IN ('superseded', 'withdrawn', 'recomputed'));
ALTER TABLE deadlines ADD COLUMN recomputed_for date;
ALTER TABLE deadlines ADD CONSTRAINT deadlines_recomputed_for_fkey
    FOREIGN KEY (jurisdiction, recomputed_for) REFERENCES public_holidays (jurisdiction, holiday_date);
ALTER TABLE deadlines DROP CONSTRAINT deadlines_closed;
ALTER TABLE deadlines ADD CONSTRAINT deadlines_closed CHECK (
    (closed_as IS NULL) = (closed_at IS NULL)
    AND (closed_as IS NULL) = (closed_reason IS NULL)
    AND (coalesce(closed_as, '') IN ('superseded', 'withdrawn')) = (closed_in_version IS NOT NULL)
    AND (coalesce(closed_as, '') IN ('superseded', 'withdrawn')) = (closed_by IS NOT NULL)
    AND (coalesce(closed_as, '') = 'recomputed') = (recomputed_for IS NOT NULL)
    AND (coalesce(closed_as, '') IN ('superseded', 'recomputed')) = (superseded_by IS NOT NULL));

CREATE TABLE holiday_recomputations (
    org_id       uuid NOT NULL,
    jurisdiction char(2) NOT NULL,
    holiday_date date NOT NULL,
    recomputed   integer NOT NULL CHECK (recomputed >= 0),
    done_at      timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (org_id, jurisdiction, holiday_date),
    FOREIGN KEY (org_id) REFERENCES organisations (id),
    FOREIGN KEY (jurisdiction, holiday_date) REFERENCES public_holidays (jurisdiction, holiday_date)
);

-- As 000017, plus the 'recomputed' close: only inside deadlines_recompute
-- for that holiday (it sets app.recompute_holiday), only of a live row of an
-- unsatisfied duty, with who empty and why the holiday.
CREATE OR REPLACE FUNCTION deadlines_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        rest deadlines;
        v    incident_versions;
        h    public_holidays;
    BEGIN
        IF TG_OP = 'DELETE' THEN
            RAISE EXCEPTION 'deadline %: deadlines are never deleted', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW IS NOT DISTINCT FROM OLD THEN
            RETURN NEW;
        END IF;
        IF OLD.closed_as IS NOT NULL THEN
            RAISE EXCEPTION 'deadline % is %; it never changes again', OLD.id, OLD.closed_as
                USING ERRCODE = 'check_violation';
        END IF;
        rest := NEW;
        rest.closed_as := OLD.closed_as;
        rest.superseded_by := OLD.superseded_by;
        rest.closed_in_version := OLD.closed_in_version;
        rest.closed_by := OLD.closed_by;
        rest.closed_at := OLD.closed_at;
        rest.closed_reason := OLD.closed_reason;
        rest.recomputed_for := OLD.recomputed_for;
        IF rest IS DISTINCT FROM OLD OR NEW.closed_as IS NULL THEN
            RAISE EXCEPTION 'deadline %: the only change allowed is closing it', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;

        IF NEW.closed_as = 'recomputed' THEN
            IF current_setting('app.recompute_holiday', true) IS DISTINCT FROM NEW.recomputed_for::text THEN
                RAISE EXCEPTION 'deadline %: recomputed only by deadlines_recompute for that holiday', OLD.id
                    USING ERRCODE = 'check_violation';
            END IF;
            IF EXISTS (SELECT 1 FROM deadline_duties WHERE id = OLD.duty_id AND satisfaction_id IS NOT NULL) THEN
                RAISE EXCEPTION 'deadline %: its duty is satisfied; a satisfied deadline is never recomputed', OLD.id
                    USING ERRCODE = 'check_violation';
            END IF;
            SELECT * INTO h FROM public_holidays WHERE jurisdiction = OLD.jurisdiction AND holiday_date = NEW.recomputed_for;
            NEW.closed_in_version := NULL;
            NEW.closed_by := NULL;
            NEW.closed_at := now();
            NEW.closed_reason := 'Recomputed for a new public holiday: ' || h.name || ' (' || h.source_ref || ')';
            RETURN NEW;
        END IF;

        SELECT * INTO v FROM incident_versions WHERE incident_id = OLD.incident_id AND version = NEW.closed_in_version;
        IF NOT FOUND OR NEW.closed_in_version <= OLD.computed_in_version
           OR NEW.closed_in_version <> (SELECT latest_version FROM incidents WHERE id = OLD.incident_id) THEN
            RAISE EXCEPTION 'deadline %: closed only by the incident''s latest version, after the one that computed it', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        NEW.closed_by := v.submitted_by;
        NEW.closed_at := v.submitted_at;
        NEW.closed_reason := v.amend_reason;
        RETURN NEW;
    END
    $$;

-- As 000017, plus: a recomputed row's replacement has the same rule and
-- inputs (only the calendar moved it), and needs no lead.
CREATE OR REPLACE FUNCTION deadlines_weakening_check() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        d         deadlines;
        successor deadlines;
        satisfied boolean;
        weakens   boolean;
    BEGIN
        SELECT * INTO d FROM deadlines WHERE id = NEW.id;
        IF d.closed_as = 'recomputed' THEN
            SELECT * INTO successor FROM deadlines WHERE id = d.superseded_by;
            IF (successor.duty_id, successor.rule_key, successor.period, successor.unit, successor.direction, successor.basis_at,
                successor.time_zone, successor.jurisdiction, successor.regime_id)
               IS DISTINCT FROM (d.duty_id, d.rule_key, d.period, d.unit, d.direction, d.basis_at, d.time_zone, d.jurisdiction, d.regime_id) THEN
                RAISE EXCEPTION 'deadline %: a recomputation keeps the rule and its inputs', d.id
                    USING ERRCODE = 'check_violation';
            END IF;
            RETURN NULL;
        END IF;
        IF d.closed_as IS NULL OR d.kind <> 'report' THEN
            RETURN NULL;
        END IF;
        IF d.closed_as = 'withdrawn' THEN
            SELECT satisfaction_id IS NOT NULL INTO satisfied FROM deadline_duties WHERE id = d.duty_id;
            weakens := NOT satisfied;
        ELSE
            SELECT * INTO successor FROM deadlines WHERE id = d.superseded_by;
            IF successor.computed_in_version <> d.closed_in_version THEN
                RAISE EXCEPTION 'deadline %: its replacement must be computed by the version that superseded it', d.id
                    USING ERRCODE = 'check_violation';
            END IF;
            weakens := successor.due_at > d.due_at;
        END IF;
        IF weakens AND NOT EXISTS (
            SELECT 1 FROM org_members WHERE org_id = d.org_id AND user_id = d.closed_by AND role IN ('owner', 'she_officer')) THEN
            RAISE EXCEPTION 'deadline %: only an owner or a SHE officer can withdraw a report deadline or move it later', d.id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NULL;
    END
    $$;

-- Recomputes the tenant's live, unsatisfied deadlines whose window includes
-- a public holiday (000017's deadlines_affected_by_day), once per holiday.
-- A row whose due dates the holiday doesn't change is left alone. Returns
-- how many were recomputed; NULL if this holiday was already done.
CREATE FUNCTION deadlines_recompute(p_jurisdiction char(2), p_day date) RETURNS integer
    LANGUAGE plpgsql
    AS $$
    DECLARE
        d      deadlines;
        r      record;
        new_id uuid;
        n      integer := 0;
    BEGIN
        IF NOT EXISTS (SELECT 1 FROM public_holidays WHERE jurisdiction = p_jurisdiction AND holiday_date = p_day) THEN
            RAISE EXCEPTION 'recompute: % is not a public holiday in %', p_day, p_jurisdiction
                USING ERRCODE = 'check_violation';
        END IF;
        IF EXISTS (SELECT 1 FROM holiday_recomputations
                   WHERE org_id = app_org_id() AND jurisdiction = p_jurisdiction AND holiday_date = p_day) THEN
            RETURN NULL;
        END IF;
        PERFORM set_config('app.recompute_holiday', p_day::text, true);
        FOR d IN
            SELECT x.* FROM deadlines_affected_by_day(p_jurisdiction, p_day) x
            JOIN deadline_duties du ON du.id = x.duty_id
            WHERE du.satisfaction_id IS NULL
            ORDER BY x.id
        LOOP
            SELECT * INTO r FROM deadline_due(d.basis_at, d.period, d.unit, d.direction, d.time_zone, d.jurisdiction);
            CONTINUE WHEN (r.due_at, r.due_on, r.legal_latest_on) IS NOT DISTINCT FROM (d.due_at, d.due_on, d.legal_latest_on);
            new_id := gen_random_uuid();
            UPDATE deadlines SET closed_as = 'recomputed', superseded_by = new_id, recomputed_for = p_day WHERE id = d.id;
            INSERT INTO deadlines (id, org_id, site_id, duty_id, incident_id, regime_id, rule_key, rule_label, kind, period, unit, direction,
                                   basis, source_ref, caveat, basis_at, time_zone, jurisdiction, due_at, excluded_days, computed_in_version)
            VALUES (new_id, d.org_id, d.site_id, d.duty_id, d.incident_id, d.regime_id, d.rule_key, d.rule_label, d.kind, d.period, d.unit,
                    d.direction, d.basis, d.source_ref, d.caveat, d.basis_at, d.time_zone, d.jurisdiction, d.due_at, d.excluded_days,
                    (SELECT latest_version FROM incidents WHERE id = d.incident_id));
            n := n + 1;
        END LOOP;
        PERFORM set_config('app.recompute_holiday', '', true);
        -- Two workers recomputing at once: the second's closing UPDATE waits
        -- on the first's row, then the guard refuses a closed row and the
        -- second rolls back; the primary key also stops a second record.
        INSERT INTO holiday_recomputations (org_id, jurisdiction, holiday_date, recomputed)
        VALUES (app_org_id(), p_jurisdiction, p_day, n);
        RETURN n;
    END
    $$;

CREATE FUNCTION notifications_append_only() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        RAISE EXCEPTION '% is append-only', TG_TABLE_NAME
            USING ERRCODE = 'check_violation';
    END
    $$;

CREATE TRIGGER notifications_no_truncate BEFORE TRUNCATE ON notifications
    FOR EACH STATEMENT EXECUTE FUNCTION notifications_append_only();
CREATE TRIGGER notification_emails_no_truncate BEFORE TRUNCATE ON notification_emails
    FOR EACH STATEMENT EXECUTE FUNCTION notifications_append_only();
CREATE TRIGGER job_runs_no_truncate BEFORE TRUNCATE ON job_runs
    FOR EACH STATEMENT EXECUTE FUNCTION notifications_append_only();
CREATE TRIGGER holiday_recomputations_append_only BEFORE UPDATE OR DELETE ON holiday_recomputations
    FOR EACH ROW
    EXECUTE FUNCTION notifications_append_only();
CREATE TRIGGER holiday_recomputations_no_truncate BEFORE TRUNCATE ON holiday_recomputations
    FOR EACH STATEMENT EXECUTE FUNCTION notifications_append_only();

ALTER TABLE notifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE notifications FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON notifications FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE notification_emails ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_emails FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON notification_emails FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE holiday_recomputations ENABLE ROW LEVEL SECURITY;
ALTER TABLE holiday_recomputations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON holiday_recomputations FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

REVOKE UPDATE, DELETE ON notifications FROM siteproof_app;
GRANT UPDATE (read_at) ON notifications TO siteproof_app;
REVOKE UPDATE, DELETE ON notification_emails FROM siteproof_app;
GRANT UPDATE (status, claimed_at, finished_at, error) ON notification_emails TO siteproof_app;
REVOKE UPDATE, DELETE ON job_runs FROM siteproof_app;
GRANT UPDATE (finished_at, ok, error, counts) ON job_runs TO siteproof_app;
REVOKE UPDATE, DELETE ON holiday_recomputations FROM siteproof_app;
-- The recompute closes deadlines: their close columns, and recomputed_for.
GRANT UPDATE (recomputed_for) ON deadlines TO siteproof_app;

COMMIT;
