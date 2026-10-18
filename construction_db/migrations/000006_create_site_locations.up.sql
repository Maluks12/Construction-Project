BEGIN;

-- Places inside a site: zones and floors for construction (Rev 4, 5.3).
-- A tree: each location may sit under a parent location on the same site.
-- Mining kinds (shaft, level, section, working place) are added by the
-- Mining edition's own migration when it ships.
CREATE TABLE site_locations (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      uuid NOT NULL,
    site_id     uuid NOT NULL,
    parent_id   uuid,
    kind        text NOT NULL CHECK (kind IN ('zone', 'floor')),
    name        text NOT NULL CHECK (length(trim(name)) > 0),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz,

    -- The site must belong to the same organisation as the location.
    FOREIGN KEY (org_id, site_id) REFERENCES sites (org_id, id),

    -- Lets the parent check below, and later tables, reference (site_id, id)
    -- together.
    UNIQUE (site_id, id),

    -- A parent must be on the SAME site. With a NULL parent_id the check is
    -- skipped, so top-level locations are allowed.
    FOREIGN KEY (site_id, parent_id) REFERENCES site_locations (site_id, id),

    CONSTRAINT site_locations_not_own_parent CHECK (parent_id IS NULL OR parent_id <> id)
);

CREATE INDEX site_locations_site_id_idx ON site_locations (site_id);
CREATE INDEX site_locations_parent_id_idx ON site_locations (parent_id);

-- No two active locations with the same name under the same parent.
-- NULLS NOT DISTINCT makes two top-level "Block A"s count as duplicates too.
CREATE UNIQUE INDEX site_locations_sibling_name_unique
    ON site_locations (site_id, parent_id, lower(name)) NULLS NOT DISTINCT
    WHERE archived_at IS NULL;

COMMIT;