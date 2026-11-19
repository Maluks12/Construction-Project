BEGIN;

CREATE OR REPLACE FUNCTION deadlines_recompute(p_jurisdiction char(2), p_day date) RETURNS integer
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

COMMIT;
