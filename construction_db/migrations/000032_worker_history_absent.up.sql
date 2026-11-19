BEGIN;

-- M11 review: a day's register must be true for past days, and the roster
-- must not grow forever.
--   - worker_history: every state a worker has had (name, firm, clock
--     number, trade, active), append-only, written by a trigger on workers.
--     "Active on a date" is read from it, so a later edit or deactivation
--     never changes a past day.
--   - site_absent(site, day, zone, lapse_days): the roster entries live on
--     that day (added before it ended, not removed before it began) whose
--     worker was active then, and who didn't sign in. An entry a sign-in
--     added counts only while the worker signed in on the site in the
--     lapse_days before the day (roster_lapse_days in reminders.json, 14:
--     a product default); manual entries stay until removed.

CREATE TABLE worker_history (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id       uuid NOT NULL,
    worker_id    uuid NOT NULL,
    name         text NOT NULL,
    firm         text,
    clock_number text,
    trade        text,
    active       boolean NOT NULL,
    changed_at   timestamptz NOT NULL,
    changed_by   uuid NOT NULL,

    FOREIGN KEY (org_id, worker_id) REFERENCES workers (org_id, id),
    FOREIGN KEY (org_id, changed_by) REFERENCES org_members (org_id, user_id)
);

CREATE INDEX worker_history_worker_idx ON worker_history (worker_id, changed_at DESC, id DESC);

-- Each new state of a worker, as of when it was made (the insert's
-- created_at, an update's updated_at).
CREATE FUNCTION workers_history() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
        IF TG_OP = 'UPDATE' AND (NEW.name, NEW.firm, NEW.clock_number, NEW.trade, NEW.active)
                                IS NOT DISTINCT FROM (OLD.name, OLD.firm, OLD.clock_number, OLD.trade, OLD.active) THEN
            RETURN NULL;
        END IF;
        INSERT INTO worker_history (org_id, worker_id, name, firm, clock_number, trade, active, changed_at, changed_by)
        VALUES (NEW.org_id, NEW.id, NEW.name, NEW.firm, NEW.clock_number, NEW.trade, NEW.active,
                CASE WHEN TG_OP = 'INSERT' THEN NEW.created_at ELSE coalesce(NEW.updated_at, now()) END,
                coalesce(NEW.updated_by, NEW.created_by));
        RETURN NULL;
    END
    $$;

CREATE TRIGGER workers_history AFTER INSERT OR UPDATE ON workers
    FOR EACH ROW EXECUTE FUNCTION workers_history();

-- The workers already on registers start their history at creation.
INSERT INTO worker_history (org_id, worker_id, name, firm, clock_number, trade, active, changed_at, changed_by)
SELECT org_id, id, name, firm, clock_number, trade, active, coalesce(updated_at, created_at), coalesce(updated_by, created_by) FROM workers;

CREATE TRIGGER worker_history_append_only BEFORE UPDATE OR DELETE ON worker_history
    FOR EACH ROW EXECUTE FUNCTION workers_no_truncate();
CREATE TRIGGER worker_history_no_truncate BEFORE TRUNCATE ON worker_history
    FOR EACH STATEMENT EXECUTE FUNCTION workers_no_truncate();

ALTER TABLE worker_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE worker_history FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON worker_history FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

REVOKE UPDATE, DELETE ON worker_history FROM siteproof_app;

-- Who was expected on a site on a local day and didn't sign in. Names are
-- as of that day; today is as of now.
CREATE FUNCTION site_absent(p_site uuid, p_day date, p_time_zone text, p_lapse_days integer)
    RETURNS TABLE (roster_id uuid, worker_id uuid, name text, firm text, clock_number text, added_via text, added_by uuid,
                   added_at timestamptz, removed_by uuid, removed_at timestamptz, remove_reason text)
    LANGUAGE sql STABLE
    AS $$
        WITH b AS (SELECT p_day::timestamp AT TIME ZONE p_time_zone AS starts,
                          (p_day + 1)::timestamp AT TIME ZONE p_time_zone AS ends)
        SELECT r.id, r.worker_id, h.name, h.firm, h.clock_number, r.added_via, r.added_by, r.added_at,
               r.removed_by, r.removed_at, r.remove_reason
        FROM site_roster r
        CROSS JOIN b
        CROSS JOIN LATERAL (SELECT * FROM worker_history wh
                            WHERE wh.worker_id = r.worker_id AND wh.changed_at <= least(b.ends, now())
                            ORDER BY wh.changed_at DESC, wh.id DESC LIMIT 1) h
        WHERE r.site_id = p_site
          AND r.added_at < b.ends
          AND (r.removed_at IS NULL OR r.removed_at >= b.starts)
          AND h.active
          AND (r.added_via = 'manual' OR EXISTS (
                SELECT 1 FROM site_sign_ins x
                WHERE x.site_id = r.site_id AND x.worker_id = r.worker_id AND x.retracted_at IS NULL
                  AND x.sign_in_date < p_day AND x.sign_in_date >= p_day - p_lapse_days))
          AND NOT EXISTS (
                SELECT 1 FROM site_sign_ins s
                WHERE s.site_id = r.site_id AND s.worker_id = r.worker_id AND s.sign_in_date = p_day AND s.retracted_at IS NULL)
        ORDER BY lower(h.name), r.id
    $$;

COMMIT;
