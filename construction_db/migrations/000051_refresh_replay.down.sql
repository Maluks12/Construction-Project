BEGIN;

ALTER TABLE refresh_tokens
    DROP CONSTRAINT refresh_tokens_successor_needs_use,
    DROP CONSTRAINT refresh_tokens_replay_needs_successor,
    DROP COLUMN replay,
    DROP COLUMN replaced_by;

COMMIT;
