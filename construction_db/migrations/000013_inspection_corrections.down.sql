BEGIN;

ALTER TABLE inspections DROP CONSTRAINT inspections_corrects_fkey;
ALTER TABLE inspections ADD CONSTRAINT inspections_corrects_fkey
    FOREIGN KEY (org_id, site_id, corrects_id) REFERENCES inspections (org_id, site_id, id);
ALTER TABLE inspections DROP CONSTRAINT inspections_org_site_id_template_unique;
ALTER TABLE inspections DROP CONSTRAINT inspections_corrects_once;

COMMIT;
