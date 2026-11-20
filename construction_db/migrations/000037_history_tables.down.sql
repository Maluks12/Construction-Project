BEGIN;

DROP TRIGGER toolbox_talks_history ON toolbox_talks;
DROP TRIGGER users_member_history ON users;
DROP TRIGGER org_members_history ON org_members;
DROP TRIGGER sites_history ON sites;
DROP TRIGGER organisations_history ON organisations;
DROP FUNCTION toolbox_talks_history();
DROP FUNCTION users_member_history();
DROP FUNCTION org_members_history();
DROP FUNCTION sites_history();
DROP FUNCTION organisations_history();
DROP TABLE toolbox_talk_history;
DROP TABLE member_history;
DROP TABLE site_history;
DROP TABLE organisation_history;
DROP FUNCTION history_append_only();

COMMIT;
