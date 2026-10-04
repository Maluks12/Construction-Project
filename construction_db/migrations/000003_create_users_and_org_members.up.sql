BEGIN;

-- A person's identity. Deliberately no org_id: a CHSO will later belong
-- to several client organisations (Rev 4, multi-organisation membership).
-- Membership lives in org_members instead.
CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email         text,
    mobile        text,
    full_name     text NOT NULL,
    -- NULL until an invited user sets a password.
    password_hash text,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    archived_at   timestamptz,

    -- Many foremen have no work email, but everyone needs one way to be reached.
    CONSTRAINT users_contact_required
        CHECK (email IS NOT NULL OR mobile IS NOT NULL)
);

-- Case-insensitive unique email: A@x.co and a@x.co are the same person.
CREATE UNIQUE INDEX users_email_unique ON users (lower(email));
CREATE UNIQUE INDEX users_mobile_unique ON users (mobile);

-- Which organisations a user belongs to, and their role in each.
-- Role decides capability; site assignment (site_members) decides scope.
CREATE TABLE org_members (
    org_id     uuid NOT NULL REFERENCES organisations (id),
    user_id    uuid NOT NULL REFERENCES users (id),
    role       text NOT NULL CHECK (role IN (
                   'owner',
                   'she_officer',
                   'supervisor',
                   'claims_handler',
                   'contracts_manager',
                   'bookkeeper'
               )),
    status     text NOT NULL DEFAULT 'active' CHECK (status IN (
                   'invited',
                   'active',
                   'suspended',
                   'removed'
               )),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    -- A user is a member of an organisation at most once.
    PRIMARY KEY (org_id, user_id)
);

-- "Which organisations does this user belong to?" (used at login).
CREATE INDEX org_members_user_id_idx ON org_members (user_id);

COMMIT;