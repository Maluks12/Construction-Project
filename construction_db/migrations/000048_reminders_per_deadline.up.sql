BEGIN;

-- REG4 review R1: a reminder belongs to one computed deadline, not to its
-- duty. A moved start, a corrected occurred_at, a holiday recompute or a
-- reinstated record makes a new deadline row, whose thresholds are told
-- afresh; the old row's stop (subject_state 'superseded', queued emails
-- cancelled). Rows stored as 'deadline_duty' keep their meaning.
ALTER TABLE notifications DROP CONSTRAINT notifications_subject_type_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_subject_type_check
    CHECK (subject_type IN ('deadline', 'deadline_duty', 'action', 'closeout', 'competency', 'medical', 'appointment', 'document', 'export'));

-- As 000041, plus 'deadline': the row is live and its duty not met.
CREATE OR REPLACE FUNCTION notification_subject_current(p_type text, p_id uuid, p_threshold text) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$
        SELECT CASE p_type
            WHEN 'deadline_duty' THEN EXISTS (SELECT 1 FROM deadline_duties du JOIN deadlines d ON d.duty_id = du.id AND d.closed_as IS NULL
                                              WHERE du.id = p_id AND du.satisfaction_id IS NULL)
            WHEN 'deadline' THEN EXISTS (SELECT 1 FROM deadlines d JOIN deadline_duties du ON du.id = d.duty_id
                                         WHERE d.id = p_id AND d.closed_as IS NULL AND du.satisfaction_id IS NULL)
            WHEN 'action' THEN EXISTS (SELECT 1 FROM corrective_actions WHERE id = p_id AND status = 'open')
            WHEN 'closeout' THEN EXISTS (SELECT 1 FROM corrective_actions WHERE pending_closeout_id = p_id)
            WHEN 'competency' THEN EXISTS (SELECT 1 FROM worker_competencies c WHERE c.id = p_id AND c.retracted_at IS NULL
                                           AND NOT EXISTS (SELECT 1 FROM worker_competencies s WHERE s.supersedes_id = c.id AND s.retracted_at IS NULL))
            WHEN 'medical' THEN EXISTS (SELECT 1 FROM worker_medicals m WHERE m.id = p_id AND m.retracted_at IS NULL
                                        AND NOT EXISTS (SELECT 1 FROM worker_medicals s WHERE s.supersedes_id = m.id AND s.retracted_at IS NULL))
            WHEN 'appointment' THEN EXISTS (SELECT 1 FROM site_appointments a WHERE a.id = p_id AND a.retracted_at IS NULL
                                            AND NOT EXISTS (SELECT 1 FROM site_appointments s WHERE s.supersedes_id = a.id AND s.retracted_at IS NULL))
            WHEN 'export' THEN true -- an integrity alarm stays standing
            WHEN 'document' THEN EXISTS (SELECT 1 FROM documents d WHERE d.id = p_id AND d.retracted_at IS NULL
                                         AND document_current_version(d.id) = substring(p_threshold FROM '^v([0-9]+)_')::integer)
        END
    $$;

DROP FUNCTION notifications_due(timestamptz, text, text, integer[], integer[], integer[], integer[], integer, jsonb, integer, integer[]);

-- As 000047, but about the deadline row.
CREATE FUNCTION notifications_due(
    p_now timestamptz, p_regime text, p_time_zone text,
    p_worker_days integer[], p_company_days integer[], p_deadline_days integer[], p_escalation integer[], p_stalled_days integer, p_labels jsonb,
    p_lapse_days integer DEFAULT 14, p_notification_days integer[] DEFAULT NULL)
    RETURNS TABLE (user_id uuid, kind text, subject_type text, subject_id uuid, threshold text, site_id uuid, title text,
                   due_on date, due_at timestamptz)
    LANGUAGE sql STABLE
    AS $$
    WITH today AS (SELECT (p_now AT TIME ZONE p_time_zone)::date AS d),
    dl AS (
        SELECT d.id AS deadline_id, d.site_id, d.due_on, d.due_at,
               CASE WHEN d.site_work_id IS NOT NULL THEN 'site_work_deadline' ELSE 'incident_deadline' END AS kind,
               CASE WHEN d.site_work_id IS NOT NULL
                    THEN d.rule_label || ' (' || notification_site_name(d.site_id) || '; work starts '
                         || to_char((d.basis_at AT TIME ZONE d.time_zone)::date, 'FMDD Mon YYYY') || ')'
                    ELSE d.rule_label || ' (' || notification_site_name(d.site_id) || ')' END AS title,
               CASE WHEN d.unit = 'immediately' THEN 'forthwith'
                    WHEN p_now >= d.due_at THEN 'overdue'
                    WHEN d.due_on IS NOT NULL THEN notification_lead(d.due_on - (p_now AT TIME ZONE d.time_zone)::date,
                         CASE WHEN d.kind = 'notification' THEN coalesce(p_notification_days, p_deadline_days) ELSE p_deadline_days END, '')
                    ELSE 'open' END AS threshold,
               coalesce(
                   (SELECT v.submitted_by FROM incidents i JOIN incident_versions v ON v.incident_id = i.id AND v.version = i.latest_version
                    WHERE i.id = d.incident_id),
                   (SELECT v.submitted_by FROM site_work w JOIN site_work_versions v ON v.site_work_id = w.id AND v.version = w.latest_version
                    WHERE w.id = d.site_work_id)) AS submitter
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
    SELECT r.user_id, dl.kind, 'deadline', dl.deadline_id, dl.threshold, dl.site_id, left(dl.title, 300), dl.due_on, dl.due_at
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

COMMIT;