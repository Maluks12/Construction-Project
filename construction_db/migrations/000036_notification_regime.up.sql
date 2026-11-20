BEGIN;

-- The regime whose time zone a reminder's dates are told in: the site's,
-- else the record's own (org-level records name their regime, M11 D1).
CREATE FUNCTION notification_regime(n notifications) RETURNS text
    LANGUAGE sql STABLE
    AS $$
        SELECT coalesce(
            (SELECT g.code FROM sites s JOIN regulatory_regimes g ON g.id = s.regime_id WHERE s.id = n.site_id),
            CASE n.subject_type
                WHEN 'competency' THEN (SELECT c.regime_code FROM worker_competencies c WHERE c.id = n.subject_id)
                WHEN 'medical' THEN (SELECT m.regime_code FROM worker_medicals m WHERE m.id = n.subject_id)
                WHEN 'document' THEN (SELECT d.regime_code FROM documents d WHERE d.id = n.subject_id)
            END)
    $$;

COMMIT;
