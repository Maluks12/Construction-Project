BEGIN;

-- M12:
--   - action_escalation_step: an overdue action's escalation step, 1-3, as
--     notifications_due (000035) escalates it: days overdue counted in the
--     action's own zone from the day after due_on, against the regime's
--     action_escalation_days ([0, 1, 3]). 0 when not overdue at p_now. The
--     compliance summary shows it; TestEscalationStepMatchesReminders
--     compares it with notifications_due.
--   - site_diary_replay_as_of: site_diary_replay over the revisions the
--     server had received by p_as_of, for the audit pack (D10).
--     site_diary_replay is now the as-of replay at 'infinity', so there is
--     still one definition of the current diary.

CREATE FUNCTION action_escalation_step(p_now timestamptz, p_due_on date, p_time_zone text, p_escalation integer[])
    RETURNS integer
    LANGUAGE sql STABLE
    AS $$
    SELECT CASE WHEN p_now < action_due_at(p_due_on, p_time_zone) THEN 0
                ELSE (SELECT count(*)::integer FROM unnest(p_escalation) AS e(days)
                      WHERE (p_now AT TIME ZONE p_time_zone)::date - (p_due_on + 1) >= e.days)
           END
    $$;

CREATE FUNCTION site_diary_replay_as_of(p_site uuid, p_date date, p_as_of timestamptz,
                                        OUT head uuid, OUT conflicts uuid[], OUT late boolean, OUT revisions integer)
    LANGUAGE plpgsql STABLE
    AS $$
    DECLARE
        r       record;
        head_at timestamptz;
    BEGIN
        conflicts := '{}';
        late := false;
        revisions := 0;
        FOR r IN SELECT id, base_revision_id, client_saved_at, saved_after_close
                 FROM site_diary_revisions
                 WHERE site_id = p_site AND diary_date = p_date AND server_received_at <= p_as_of
                 ORDER BY seq LOOP
            revisions := revisions + 1;
            IF head IS NULL THEN
                head := r.id;
                head_at := r.client_saved_at;
                late := r.saved_after_close;
            ELSIF r.base_revision_id = head AND r.client_saved_at > head_at THEN
                head := r.id;
                head_at := r.client_saved_at;
                conflicts := '{}';
            ELSE
                IF cardinality(conflicts) = 0 THEN
                    conflicts := ARRAY[head];
                END IF;
                conflicts := conflicts || r.id;
                IF r.client_saved_at > head_at THEN
                    head := r.id;
                    head_at := r.client_saved_at;
                END IF;
            END IF;
        END LOOP;
    END
    $$;

CREATE OR REPLACE FUNCTION site_diary_replay(p_site uuid, p_date date,
                                             OUT head uuid, OUT conflicts uuid[], OUT late boolean, OUT revisions integer)
    LANGUAGE sql STABLE
    AS $$
    SELECT * FROM site_diary_replay_as_of(p_site, p_date, 'infinity')
    $$;

COMMIT;
