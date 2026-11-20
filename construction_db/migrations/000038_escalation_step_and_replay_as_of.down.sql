BEGIN;

-- Put back 000021's own replay before dropping the as-of one it calls.
CREATE OR REPLACE FUNCTION site_diary_replay(p_site uuid, p_date date,
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
                 FROM site_diary_revisions WHERE site_id = p_site AND diary_date = p_date ORDER BY seq LOOP
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

DROP FUNCTION site_diary_replay_as_of(uuid, date, timestamptz);
DROP FUNCTION action_escalation_step(timestamptz, date, text, integer[]);

COMMIT;
