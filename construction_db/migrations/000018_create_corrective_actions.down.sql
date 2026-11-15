BEGIN;

DROP TABLE corrective_action_events;
DROP TABLE corrective_actions;
DROP FUNCTION corrective_actions_append_only();
DROP FUNCTION corrective_action_events_advance();
DROP FUNCTION corrective_action_events_insert();
DROP FUNCTION corrective_actions_guard();
DROP FUNCTION corrective_actions_has_raise();
DROP FUNCTION corrective_actions_insert();
DROP FUNCTION action_lead(uuid, uuid);
DROP FUNCTION action_site_member(uuid, uuid, uuid);
DROP FUNCTION action_due_at(date, text);
DROP FUNCTION action_local_date(timestamptz, text);
DROP FUNCTION action_default_due_days(text);
DROP FUNCTION severity_rank(text);
ALTER TABLE inspection_findings DROP CONSTRAINT inspection_findings_severity_key;

COMMIT;
