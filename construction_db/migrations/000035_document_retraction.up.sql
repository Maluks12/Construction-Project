BEGIN;

-- M11 review: documents can be retracted.
--   - A whole document, by an owner or SHE officer, with a reason, once:
--     it stays in the register, marked retracted, and is ignored by
--     readiness and reminders. No version can be added to it after.
--   - Its current version, the same way, so the previous one becomes current
--     again. Never the only standing version (retract the document instead).
--     Numbers keep counting: after a retracted v3 the next upload is v4.
-- The current version is the highest that isn't retracted.

ALTER TABLE documents
    ADD COLUMN retracted_at   timestamptz,
    ADD COLUMN retracted_by   uuid,
    ADD COLUMN retract_reason text CHECK (char_length(btrim(retract_reason)) BETWEEN 1 AND 500),
    ADD CONSTRAINT documents_retracted_by_fkey FOREIGN KEY (org_id, retracted_by) REFERENCES org_members (org_id, user_id),
    ADD CONSTRAINT documents_retraction CHECK (
        (retracted_at IS NULL) = (retracted_by IS NULL) AND (retracted_at IS NULL) = (retract_reason IS NULL));

ALTER TABLE document_versions
    ADD COLUMN retracted_at   timestamptz,
    ADD COLUMN retracted_by   uuid,
    ADD COLUMN retract_reason text CHECK (char_length(btrim(retract_reason)) BETWEEN 1 AND 500),
    ADD CONSTRAINT document_versions_retracted_by_fkey FOREIGN KEY (org_id, retracted_by) REFERENCES org_members (org_id, user_id),
    ADD CONSTRAINT document_versions_retraction CHECK (
        (retracted_at IS NULL) = (retracted_by IS NULL) AND (retracted_at IS NULL) = (retract_reason IS NULL));

CREATE FUNCTION document_current_version(p_document uuid) RETURNS integer
    LANGUAGE sql STABLE
    AS $$ SELECT max(version) FROM document_versions WHERE document_id = p_document AND retracted_at IS NULL $$;

-- As 000027, plus: never a version of a retracted document, nor one
-- arriving retracted.
CREATE OR REPLACE FUNCTION document_versions_insert() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        NEW.received_at := now();
        IF NEW.client_uploaded_at > NEW.received_at + interval '5 minutes' THEN
            RAISE EXCEPTION 'document %: client_uploaded_at is in the future; check the device clock', NEW.document_id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.retracted_at IS NOT NULL OR NEW.retracted_by IS NOT NULL OR NEW.retract_reason IS NOT NULL THEN
            RAISE EXCEPTION 'document %: a version is retracted after it is added, never at insert', NEW.document_id
                USING ERRCODE = 'check_violation';
        END IF;
        IF EXISTS (SELECT 1 FROM documents WHERE id = NEW.document_id AND retracted_at IS NOT NULL) THEN
            RAISE EXCEPTION 'document %: retracted; no version can be added', NEW.document_id
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

-- A document's header changes only by latest_version moving on by one, or
-- by its retraction: once, with who and why, by an owner or SHE officer.
CREATE OR REPLACE FUNCTION documents_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        rest_new jsonb;
        rest_old jsonb;
    BEGIN
        IF TG_OP = 'DELETE' THEN
            RAISE EXCEPTION 'document %: documents are never deleted', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW IS NOT DISTINCT FROM OLD THEN
            RETURN NEW;
        END IF;
        rest_new := to_jsonb(NEW) - ARRAY['latest_version', 'retracted_at', 'retracted_by', 'retract_reason'];
        rest_old := to_jsonb(OLD) - ARRAY['latest_version', 'retracted_at', 'retracted_by', 'retract_reason'];
        IF rest_new IS DISTINCT FROM rest_old OR NEW.latest_version NOT IN (OLD.latest_version, OLD.latest_version + 1) THEN
            RAISE EXCEPTION 'document %: only its latest version moves on, by one', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;
        IF (NEW.retracted_by, NEW.retract_reason) IS DISTINCT FROM (OLD.retracted_by, OLD.retract_reason)
           OR NEW.retracted_at IS DISTINCT FROM OLD.retracted_at THEN
            IF OLD.retracted_at IS NOT NULL THEN
                RAISE EXCEPTION 'document %: already retracted; it never changes again', OLD.id
                    USING ERRCODE = 'check_violation';
            END IF;
            IF NEW.retracted_by IS NULL OR NEW.retract_reason IS NULL THEN
                RAISE EXCEPTION 'document %: a retraction needs who and why', OLD.id
                    USING ERRCODE = 'check_violation';
            END IF;
            IF NOT workers_lead(OLD.org_id, NEW.retracted_by) THEN
                RAISE EXCEPTION 'document %: only an owner or SHE officer can retract it', OLD.id
                    USING ERRCODE = 'check_violation';
            END IF;
            NEW.retracted_at := now();
        END IF;
        RETURN NEW;
    END
    $$;

-- A version changes only by its retraction: once, by an owner or SHE
-- officer, the current version only, never the only standing one, and not
-- of a retracted document. The document's row is locked first, so a
-- retraction and a new version (which moves latest_version) take turns.
CREATE FUNCTION document_versions_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        doc      documents;
        standing integer;
    BEGIN
        IF TG_OP = 'DELETE' THEN
            RAISE EXCEPTION 'document_versions is append-only'
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW IS NOT DISTINCT FROM OLD THEN
            RETURN NEW;
        END IF;
        IF (to_jsonb(NEW) - ARRAY['retracted_at', 'retracted_by', 'retract_reason'])
           IS DISTINCT FROM (to_jsonb(OLD) - ARRAY['retracted_at', 'retracted_by', 'retract_reason']) THEN
            RAISE EXCEPTION 'document_versions is append-only'
                USING ERRCODE = 'check_violation';
        END IF;
        IF OLD.retracted_at IS NOT NULL THEN
            RAISE EXCEPTION 'document % version %: already retracted', OLD.document_id, OLD.version
                USING ERRCODE = 'check_violation';
        END IF;
        IF NEW.retracted_by IS NULL OR NEW.retract_reason IS NULL THEN
            RAISE EXCEPTION 'document % version %: a retraction needs who and why', OLD.document_id, OLD.version
                USING ERRCODE = 'check_violation';
        END IF;
        IF NOT workers_lead(OLD.org_id, NEW.retracted_by) THEN
            RAISE EXCEPTION 'document % version %: only an owner or SHE officer can retract it', OLD.document_id, OLD.version
                USING ERRCODE = 'check_violation';
        END IF;
        SELECT * INTO doc FROM documents WHERE id = OLD.document_id FOR UPDATE;
        IF doc.retracted_at IS NOT NULL THEN
            RAISE EXCEPTION 'document %: retracted; its versions don''t change', OLD.document_id
                USING ERRCODE = 'check_violation';
        END IF;
        IF OLD.version <> document_current_version(OLD.document_id) THEN
            RAISE EXCEPTION 'document % version %: only the current version can be retracted', OLD.document_id, OLD.version
                USING ERRCODE = 'check_violation';
        END IF;
        SELECT count(*) INTO standing FROM document_versions WHERE document_id = OLD.document_id AND retracted_at IS NULL;
        IF standing < 2 THEN
            RAISE EXCEPTION 'document % version %: the only standing version; retract the document instead', OLD.document_id, OLD.version
                USING ERRCODE = 'check_violation';
        END IF;
        NEW.retracted_at := now();
        RETURN NEW;
    END
    $$;

DROP TRIGGER document_versions_append_only ON document_versions;
CREATE TRIGGER document_versions_guard BEFORE UPDATE OR DELETE ON document_versions
    FOR EACH ROW EXECUTE FUNCTION document_versions_guard();

GRANT UPDATE (retracted_at, retracted_by, retract_reason) ON documents TO siteproof_app;
GRANT UPDATE (retracted_at, retracted_by, retract_reason) ON document_versions TO siteproof_app;

-- Reminders: only a document's current version, never a retracted
-- document. Otherwise as 000034.
CREATE OR REPLACE FUNCTION notifications_due(
    p_now timestamptz, p_regime text, p_time_zone text,
    p_worker_days integer[], p_company_days integer[], p_deadline_days integer[], p_escalation integer[], p_stalled_days integer, p_labels jsonb)
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
        SELECT a.id, a.site_id, a.worker_id, a.type_key, a.expires_on, notification_lead(a.expires_on - (SELECT d FROM today), p_worker_days, '') AS threshold
        FROM site_appointments a
        WHERE a.regime_code = p_regime AND a.expires_on IS NOT NULL AND a.retracted_at IS NULL
          AND NOT EXISTS (SELECT 1 FROM site_appointments s WHERE s.supersedes_id = a.id AND s.retracted_at IS NULL)
    ),
    doc AS (
        SELECT d.id, d.site_id, d.title, v.expires_on,
               notification_lead(v.expires_on - (SELECT d FROM today),
                                 CASE WHEN d.scope = 'org' THEN p_company_days ELSE p_worker_days END, 'v' || v.version || '_') AS threshold
        FROM documents d JOIN document_versions v ON v.document_id = d.id AND v.version = document_current_version(d.id)
        WHERE d.regime_code = p_regime AND v.expires_on IS NOT NULL AND d.retracted_at IS NULL
    ),
    -- Workers' supervisors: of every site where the worker is on the roster.
    worker_sups AS (
        SELECT DISTINCT r.worker_id, s.user_id
        FROM site_roster r, LATERAL notification_site_supervisors(r.site_id) s(user_id)
        WHERE r.removed_at IS NULL
    )
    SELECT r.user_id, 'incident_deadline', 'deadline_duty', dl.duty_id, dl.threshold, dl.site_id,
           left(dl.rule_label || ' (' || notification_site_name(dl.site_id) || ')', 300), dl.due_on, dl.due_at
    FROM dl, LATERAL (SELECT dl.submitter UNION SELECT notification_leads() UNION SELECT notification_site_supervisors(dl.site_id)) r(user_id)
    WHERE dl.threshold IS NOT NULL AND r.user_id IS NOT NULL
    UNION ALL
    SELECT r.user_id, 'action_overdue', 'action', act.id, 'escalation_' || e.step, act.site_id,
           left(act.title || ' (' || notification_site_name(act.site_id) || ')', 300), act.due_on, act.due_at
    FROM act
    JOIN LATERAL (SELECT 1 AS step WHERE act.days_over >= p_escalation[1]
                  UNION ALL SELECT 2 WHERE act.days_over >= p_escalation[2]
                  UNION ALL SELECT 3 WHERE act.days_over >= p_escalation[3]) e ON true
    JOIN LATERAL (SELECT act.owner_id WHERE e.step = 1 UNION SELECT act.raised_by WHERE e.step = 1
                  UNION SELECT user_id FROM org_members WHERE e.step = 2 AND org_id = app_org_id() AND status = 'active' AND role = 'she_officer'
                  UNION SELECT user_id FROM org_members WHERE e.step = 3 AND org_id = app_org_id() AND status = 'active' AND role = 'owner') r(user_id) ON true
    UNION ALL
    SELECT r.user_id, 'verification_stalled', 'closeout', stalled.pending_closeout_id, 'stalled', stalled.site_id,
           left('Verify the close-out: ' || stalled.title || ' (' || notification_site_name(stalled.site_id) || ')', 300), NULL::date, NULL::timestamptz
    FROM stalled, LATERAL notification_leads() r(user_id)
    UNION ALL
    SELECT r.user_id, 'competency_expiry', 'competency', comp.id, comp.threshold, NULL::uuid,
           left(notification_worker_name(comp.worker_id) || ': ' || coalesce(p_labels -> 'competency' ->> comp.type_key, comp.type_key), 300),
           comp.expires_on, NULL::timestamptz
    FROM comp, LATERAL (SELECT notification_leads() UNION SELECT ws.user_id FROM worker_sups ws WHERE ws.worker_id = comp.worker_id) r(user_id)
    WHERE comp.threshold IS NOT NULL
    UNION ALL
    SELECT r.user_id, 'medical_expiry', 'medical', med.id, med.threshold, NULL::uuid,
           left(notification_worker_name(med.worker_id) || ': medical certificate of fitness', 300), med.expires_on, NULL::timestamptz
    FROM med, LATERAL (SELECT notification_leads() UNION SELECT ws.user_id FROM worker_sups ws WHERE ws.worker_id = med.worker_id) r(user_id)
    WHERE med.threshold IS NOT NULL
    UNION ALL
    SELECT r.user_id, 'appointment_expiry', 'appointment', appt.id, appt.threshold, appt.site_id,
           left(notification_worker_name(appt.worker_id) || ': ' || coalesce(p_labels -> 'appointment' ->> appt.type_key, appt.type_key)
                || ' (' || notification_site_name(appt.site_id) || ')', 300),
           appt.expires_on, NULL::timestamptz
    FROM appt, LATERAL (SELECT notification_leads() UNION SELECT notification_site_supervisors(appt.site_id)) r(user_id)
    WHERE appt.threshold IS NOT NULL
    UNION ALL
    SELECT r.user_id, 'document_expiry', 'document', doc.id, doc.threshold, doc.site_id,
           left(doc.title || coalesce(' (' || notification_site_name(doc.site_id) || ')', ''), 300), doc.expires_on, NULL::timestamptz
    FROM doc, LATERAL (SELECT notification_leads() UNION SELECT notification_site_supervisors(doc.site_id) WHERE doc.site_id IS NOT NULL) r(user_id)
    WHERE doc.threshold IS NOT NULL
    $$;

CREATE OR REPLACE FUNCTION notification_subject_current(p_type text, p_id uuid, p_threshold text) RETURNS boolean
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
            WHEN 'document' THEN EXISTS (SELECT 1 FROM documents d WHERE d.id = p_id AND d.retracted_at IS NULL
                                         AND document_current_version(d.id) = substring(p_threshold FROM '^v([0-9]+)_')::integer)
        END
    $$;

COMMIT;
