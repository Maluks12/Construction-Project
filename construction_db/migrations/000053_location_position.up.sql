BEGIN;

-- ONBOARD D14: an optional order among a location's siblings (floors in
-- building order). Lists sort numbered first, then null, then by name.
ALTER TABLE site_locations
    ADD COLUMN position integer CONSTRAINT site_locations_position_range CHECK (position BETWEEN 0 AND 9999);

COMMIT;
