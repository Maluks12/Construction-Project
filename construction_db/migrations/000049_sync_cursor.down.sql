BEGIN;

DROP FUNCTION sync_new_generation();
DROP FUNCTION sync_epoch();
DROP TABLE sync_generation;

DROP TRIGGER zz_sync_deleted ON inspection_response_evidence;

DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'inspections', 'inspection_responses', 'inspection_response_evidence', 'inspection_findings',
        'incidents', 'incident_versions', 'incident_people', 'incident_version_evidence', 'witness_statements',
        'deadline_duties', 'deadlines', 'deadline_satisfactions', 'deadline_satisfaction_retractions',
        'corrective_actions', 'corrective_action_events', 'corrective_action_closeouts',
        'corrective_action_closeout_evidence', 'finding_waivers',
        'toolbox_talks', 'toolbox_talk_evidence', 'toolbox_attendance', 'toolbox_attendance_retractions',
        'toolbox_attendance_workers',
        'site_diaries', 'site_diary_revisions', 'site_diary_revision_evidence', 'site_diary_addenda',
        'site_diary_addendum_evidence',
        'site_sign_ins', 'site_sign_in_sheets', 'site_roster', 'site_appointments', 'site_work',
        'site_work_versions', 'site_locations',
        'workers', 'worker_competencies', 'worker_medicals', 'documents', 'document_versions',
        'notifications'
    ] LOOP
        EXECUTE format('DROP TRIGGER zz_sync_stamp ON %I', t);
        EXECUTE format('ALTER TABLE %I DROP COLUMN sync_xid', t);
    END LOOP;
END
$$;

DROP TABLE sync_deletions;
DROP FUNCTION sync_deleted();
DROP FUNCTION sync_stamp();

COMMIT;
