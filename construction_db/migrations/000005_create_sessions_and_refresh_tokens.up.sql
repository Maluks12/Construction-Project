BEGIN;

-- One row per logged-in device. Revoking a session (lost phone) cuts that
-- device off: its access token stops working on the next request and its
-- refresh token can no longer be used.
CREATE TABLE sessions (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users (id),
    org_id       uuid NOT NULL,
    device_name  text,
    user_agent   text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    revoked_at   timestamptz,

    -- The session's organisation must be one the user is a member of.
    FOREIGN KEY (org_id, user_id) REFERENCES org_members (org_id, user_id)
);

-- "Show me my devices" and "revoke all my sessions".
CREATE INDEX sessions_user_id_idx ON sessions (user_id);

-- Refresh tokens are never stored in plain text, only their SHA-256 hash,
-- so a database leak doesn't hand out working tokens.
-- Each refresh issues a new token and marks the old one used (rotation).
-- If a used token is ever presented again, it was stolen or replayed,
-- and the whole session is revoked (reuse detection).
CREATE TABLE refresh_tokens (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id uuid NOT NULL REFERENCES sessions (id),
    token_hash text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at    timestamptz
);

CREATE INDEX refresh_tokens_session_id_idx ON refresh_tokens (session_id);

COMMIT;