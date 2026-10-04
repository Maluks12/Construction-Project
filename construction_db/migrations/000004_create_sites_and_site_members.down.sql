BEGIN;

-- Children first: site_members references sites.
DROP TABLE IF EXISTS site_members;
DROP TABLE IF EXISTS sites;

COMMIT;