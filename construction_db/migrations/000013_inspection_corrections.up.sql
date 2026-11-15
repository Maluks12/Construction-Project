BEGIN;

-- A completed inspection has at most one correction. A correction is itself
-- an inspection, so it can be corrected in turn: the history is a chain, not
-- a tree. NULLs don't collide, so inspections that correct nothing are free.
ALTER TABLE inspections ADD CONSTRAINT inspections_corrects_once UNIQUE (corrects_id);

-- A correction uses the same checklist (template_key) as the inspection it
-- corrects; a newer version of it is fine. template_key joins the foreign
-- key, so the database refuses anything else. template_key never changes
-- (inspections_guard), so the key it points at never moves.
ALTER TABLE inspections ADD CONSTRAINT inspections_org_site_id_template_unique UNIQUE (org_id, site_id, id, template_key);
ALTER TABLE inspections DROP CONSTRAINT inspections_corrects_fkey;
ALTER TABLE inspections ADD CONSTRAINT inspections_corrects_fkey
    FOREIGN KEY (org_id, site_id, corrects_id, template_key) REFERENCES inspections (org_id, site_id, id, template_key);

COMMIT;
