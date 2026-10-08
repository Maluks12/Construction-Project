BEGIN;

-- Children first: refresh_tokens references sessions.
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS sessions;

COMMIT;