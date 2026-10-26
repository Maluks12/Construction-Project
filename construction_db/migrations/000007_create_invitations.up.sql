BEGIN;

-- An invitation to join an organisation. The invited person already has a
-- users row (created at invite time, with no password yet) and an
-- org_members row with status 'invited'. Accepting the invite sets the
-- password if needed and flips the membership to 'active'.
-- Only the SHA-256 hash of the token is stored, the same as refresh tokens.
CREATE TABLE invitations (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      uuid NOT NULL,
    user_id     uuid NOT NULL,
    token_hash  text NOT NULL UNIQUE,
    invited_by  uuid NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    accepted_at timestamptz,
    revoked_at  timestamptz,

    -- The invitee must be a member row of this organisation...
    FOREIGN KEY (org_id, user_id) REFERENCES org_members (org_id, user_id),
    -- ...and so must the person who sent it.
    FOREIGN KEY (org_id, invited_by) REFERENCES org_members (org_id, user_id),

    -- An invitation ends one way or the other, never both.
    CONSTRAINT invitations_accepted_or_revoked CHECK (accepted_at IS NULL OR revoked_at IS NULL)
);

-- At most one open invitation per person per organisation.
-- Re-inviting revokes the old one first.
CREATE UNIQUE INDEX invitations_one_pending
    ON invitations (org_id, user_id)
    WHERE accepted_at IS NULL AND revoked_at IS NULL;

CREATE INDEX invitations_org_id_idx ON invitations (org_id);

COMMIT;