BEGIN;

-- A site belongs to one organisation and runs under one regime.
-- The regime is on the site, not the organisation, because one contractor
-- can have a building site and a mine site (Rev 4, section 1.3).
CREATE TABLE sites (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      uuid NOT NULL REFERENCES organisations (id),
    regime_id   uuid NOT NULL REFERENCES regulatory_regimes (id),
    name        text NOT NULL,
    address     text,
    lat         double precision,
    lng         double precision,
    status      text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'completed', 'archived')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz,

    -- Lets other tables reference (org_id, id) together, so the database
    -- itself guarantees a child row and its site share the same organisation.
    UNIQUE (org_id, id)
);

CREATE INDEX sites_org_id_idx ON sites (org_id);

-- Which users can see which sites.
-- Both foreign keys share org_id, so Postgres guarantees the site and the
-- member belong to the SAME organisation. A user from org B can never be
-- assigned to a site of org A, even if the Go code has a bug.
CREATE TABLE site_members (
    org_id     uuid NOT NULL,
    site_id    uuid NOT NULL,
    user_id    uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (site_id, user_id),
    FOREIGN KEY (org_id, site_id) REFERENCES sites (org_id, id),
    FOREIGN KEY (org_id, user_id) REFERENCES org_members (org_id, user_id)
);

-- "Which sites can this user see?"
CREATE INDEX site_members_user_id_idx ON site_members (user_id);

COMMIT;