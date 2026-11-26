BEGIN;

-- SYNC (A.3): a phone whose refresh succeeded but lost the response retries
-- with the same token. Within a short grace, while the new token is unused,
-- that exact retry gets the same pair back instead of revoking the session.
-- replay is that pair, encrypted with a key derived from the old token, which
-- only its holder has: the database alone can't reveal it.
ALTER TABLE refresh_tokens
    ADD COLUMN replaced_by uuid REFERENCES refresh_tokens (id),
    ADD COLUMN replay      bytea,
    ADD CONSTRAINT refresh_tokens_replay_needs_successor CHECK (replay IS NULL OR replaced_by IS NOT NULL),
    ADD CONSTRAINT refresh_tokens_successor_needs_use CHECK (replaced_by IS NULL OR used_at IS NOT NULL);

COMMIT;
