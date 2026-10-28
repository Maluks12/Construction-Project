BEGIN;

-- Row-level security on every table that holds tenant data. The tenant comes
-- from app_org_id() / app_user_id() (migration 000008), set per transaction by
-- database.WithTenant. FORCE so it applies even to the table owner unless it
-- is a superuser or has BYPASSRLS.

ALTER TABLE organisations ENABLE ROW LEVEL SECURITY;
ALTER TABLE organisations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON organisations FOR ALL TO siteproof_app
    USING (id = app_org_id())
    WITH CHECK (id = app_org_id());

-- A user also sees their own memberships in every org, so login can list them.
ALTER TABLE org_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE org_members FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON org_members FOR ALL TO siteproof_app
    USING (org_id = app_org_id() OR user_id = app_user_id())
    WITH CHECK (org_id = app_org_id());

-- A user manages their own sessions in every org (list, logout, revoke).
-- The (org_id, user_id) foreign key to org_members still stops a session for
-- an org the user isn't in.
ALTER TABLE sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE sessions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON sessions FOR ALL TO siteproof_app
    USING (org_id = app_org_id() OR user_id = app_user_id())
    WITH CHECK (org_id = app_org_id() OR user_id = app_user_id());

-- No org_id: a refresh token is visible only when its session is (the EXISTS
-- is itself filtered by the sessions policy).
ALTER TABLE refresh_tokens ENABLE ROW LEVEL SECURITY;
ALTER TABLE refresh_tokens FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON refresh_tokens FOR ALL TO siteproof_app
    USING (EXISTS (SELECT 1 FROM sessions s WHERE s.id = refresh_tokens.session_id))
    WITH CHECK (EXISTS (SELECT 1 FROM sessions s WHERE s.id = refresh_tokens.session_id));

ALTER TABLE sites ENABLE ROW LEVEL SECURITY;
ALTER TABLE sites FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON sites FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE site_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE site_members FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON site_members FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE site_locations ENABLE ROW LEVEL SECURITY;
ALTER TABLE site_locations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON site_locations FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE invitations ENABLE ROW LEVEL SECURITY;
ALTER TABLE invitations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON invitations FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

-- Not here: users (no org_id; accepted risk, see BUILD_PLAN DEPLOY) and
-- regulatory_regimes (public system data, read-only to siteproof_app).

COMMIT;
