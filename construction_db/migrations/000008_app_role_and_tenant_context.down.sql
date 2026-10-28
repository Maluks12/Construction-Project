BEGIN;

DROP FUNCTION IF EXISTS invitation_org(text);
DROP FUNCTION IF EXISTS refresh_token_tenant(text);
DROP FUNCTION IF EXISTS app_user_id();
DROP FUNCTION IF EXISTS app_org_id();

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'siteproof_app') THEN
        ALTER DEFAULT PRIVILEGES IN SCHEMA public
            REVOKE SELECT, INSERT, UPDATE, DELETE ON TABLES FROM siteproof_app;
        REVOKE ALL ON ALL TABLES IN SCHEMA public FROM siteproof_app;
        REVOKE USAGE ON SCHEMA public FROM siteproof_app;
        -- Fails if another database on the server still uses the role; drop it there first.
        DROP ROLE siteproof_app;
    END IF;
END
$$;

COMMIT;
