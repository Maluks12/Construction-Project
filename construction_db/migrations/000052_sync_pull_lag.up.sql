BEGIN;

-- SYNC review 4: a pull reads up to the oldest transaction still holding an
-- xid anywhere on the server, so one left open (a psql session, a long
-- migration) holds every pull back. This is its age, for /healthz/jobs.
-- SECURITY DEFINER: the app role sees only its own sessions' xact_start.
-- Prepared transactions hold an xid too.
CREATE FUNCTION sync_oldest_write_age() RETURNS double precision
    LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, public
    AS $$
    SELECT coalesce(extract(epoch FROM clock_timestamp() - least(
        (SELECT min(xact_start) FROM pg_stat_activity WHERE backend_xid IS NOT NULL AND pid <> pg_backend_pid()),
        (SELECT min(prepared) FROM pg_prepared_xacts))), 0)::double precision
    $$;
REVOKE ALL ON FUNCTION sync_oldest_write_age() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION sync_oldest_write_age() TO siteproof_app;

-- The sign-in day's overlaps dep reads other sites' sign-ins of the org.
CREATE INDEX site_sign_ins_org_sync_idx ON site_sign_ins (org_id, sync_xid);

COMMIT;
