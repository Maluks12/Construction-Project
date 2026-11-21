BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM notifications WHERE kind = 'site_work_deadline') THEN
        RAISE EXCEPTION 'site work reminders exist; they cannot be dropped';
    END IF;
END
$$;

DROP FUNCTION notifications_due(timestamptz, text, text, integer[], integer[], integer[], integer[], integer, jsonb, integer, integer[]);

-- 000042's.
CREATE FUNCTION notifications_due(
    p_now timestamptz, p_regime text, p_time_zone text,
    p_worker_days integer[], p_company_days integer[], p_deadline_days integer[], p_escalation integer[], p_stalled_days integer, p_labels jsonb,
    p_lapse_days integer DEFAULT 14)
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
        FROM sites st,
             LATERAL site_roster_on(st.id, (SELECT d FROM today), p_time_zone, p_lapse_days) r,
             LATERAL notification_site_supervisors(st.id) s(user_id)
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

ALTER TABLE notifications DROP CONSTRAINT notifications_kind_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_kind_check
    CHECK (kind IN ('incident_deadline', 'action_overdue', 'verification_stalled', 'competency_expiry',
                    'medical_expiry', 'appointment_expiry', 'document_expiry', 'integrity_alarm'));

COMMIT;