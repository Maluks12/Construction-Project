BEGIN;

-- Children first: org_members references users.
DROP TABLE IF EXISTS org_members;
DROP TABLE IF EXISTS users;

COMMIT;