BEGIN;

-- The API runs as this role (database.Connect does SET ROLE), so row-level
-- security applies to it. It owns nothing and cannot bypass RLS.
-- Roles are cluster-wide, so another database on the server may have made it.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'siteproof_app') THEN
        CREATE ROLE siteproof_app NOLOGIN NOBYPASSRLS;
    END IF;
END
$$;

GRANT siteproof_app TO CURRENT_USER;

GRANT USAGE ON SCHEMA public TO siteproof_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO siteproof_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO siteproof_app;

-- Regime rows are system data; only migrations write them.
REVOKE INSERT, UPDATE, DELETE ON regulatory_regimes FROM siteproof_app;

-- The tenant for the current transaction, set by database.SetTenant with
-- set_config(..., true). NULLIF because a pooled connection reads '' (not
-- NULL) once an earlier transaction-local setting has ended.
CREATE FUNCTION app_org_id() RETURNS uuid
    LANGUAGE sql STABLE
    AS $$ SELECT NULLIF(current_setting('app.org_id', true), '')::uuid $$;

CREATE FUNCTION app_user_id() RETURNS uuid
    LANGUAGE sql STABLE
    AS $$ SELECT NULLIF(current_setting('app.user_id', true), '')::uuid $$;

-- Refresh and invite links arrive with only a secret token, before the org is
-- known. These return just the ids needed to set the tenant; every real read
-- then runs under RLS. SECURITY DEFINER runs them as the owner, which must be
-- a superuser or have BYPASSRLS once RLS is forced on these tables.
CREATE FUNCTION refresh_token_tenant(p_token_hash text)
    RETURNS TABLE (org_id uuid, user_id uuid)
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path = pg_catalog, public
    AS $$
        SELECT s.org_id, s.user_id
        FROM refresh_tokens r
        JOIN sessions s ON s.id = r.session_id
        WHERE r.token_hash = p_token_hash
    $$;

CREATE FUNCTION invitation_org(p_token_hash text)
    RETURNS uuid
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path = pg_catalog, public
    AS $$ SELECT org_id FROM invitations WHERE token_hash = p_token_hash $$;

REVOKE ALL ON FUNCTION refresh_token_tenant(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION invitation_org(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION refresh_token_tenant(text) TO siteproof_app;
GRANT EXECUTE ON FUNCTION invitation_org(text) TO siteproof_app;

COMMIT;
