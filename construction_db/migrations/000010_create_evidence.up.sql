BEGIN;

-- Evidence declared by a client (Rev 4 §2.1). The id is generated on the
-- device so offline sync is idempotent. The row starts 'pending' and becomes
-- 'verified' once the server has re-hashed the stored bytes, or 'rejected'
-- (kept, never deleted) if they don't match.
CREATE TABLE evidence (
    id                 uuid PRIMARY KEY,
    org_id             uuid NOT NULL,
    site_id            uuid NOT NULL,
    uploaded_by        uuid NOT NULL,
    content_sha256     bytea NOT NULL CHECK (length(content_sha256) = 32),
    size_bytes         bigint NOT NULL CHECK (size_bytes > 0),
    content_type       text NOT NULL CHECK (content_type IN ('image/jpeg', 'image/png')),
    client_captured_at timestamptz NOT NULL,
    -- BETWEEN also rejects NaN and ±Infinity, which sort outside any range.
    lat                double precision CHECK (lat BETWEEN -90 AND 90),
    lng                double precision CHECK (lng BETWEEN -180 AND 180),
    device_id          text CHECK (octet_length(device_id) BETWEEN 1 AND 200),
    object_key         text NOT NULL UNIQUE,
    object_version_id  text,
    retain_until       timestamptz,
    status             text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'verified', 'rejected')),
    rejected_reason    text,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),

    FOREIGN KEY (org_id, site_id) REFERENCES sites (org_id, id),
    FOREIGN KEY (org_id, uploaded_by) REFERENCES org_members (org_id, user_id),

    -- Target of evidence_chain's foreign key, so a chain entry can only seal
    -- the hash declared for its evidence.
    UNIQUE (site_id, id, content_sha256),

    CONSTRAINT evidence_location_pair CHECK ((lat IS NULL) = (lng IS NULL)),
    CONSTRAINT evidence_rejected_reason CHECK ((status = 'rejected') = (rejected_reason IS NOT NULL)),
    CONSTRAINT evidence_verified_version CHECK (status <> 'verified' OR object_version_id IS NOT NULL)
);

CREATE INDEX evidence_site_id_idx ON evidence (site_id, created_at);

-- Only pending evidence may change. Applies to every role, including the
-- table owner. On verified or rejected evidence the one allowed change is
-- extending retain_until (a legal hold or claim), as S3 Object Lock allows.
CREATE FUNCTION evidence_freeze() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
        rest evidence;
    BEGIN
        IF OLD.status = 'pending' THEN
            RETURN NEW;
        END IF;

        IF NEW.retain_until IS DISTINCT FROM OLD.retain_until
           AND (NEW.retain_until IS NULL OR NEW.retain_until < OLD.retain_until) THEN
            RAISE EXCEPTION 'evidence %: retention can only be extended', OLD.id
                USING ERRCODE = 'check_violation';
        END IF;

        rest := NEW;
        rest.retain_until := OLD.retain_until;
        rest.updated_at := OLD.updated_at;
        IF rest IS DISTINCT FROM OLD THEN
            RAISE EXCEPTION 'evidence % is %; only pending evidence can change', OLD.id, OLD.status
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN NEW;
    END
    $$;

CREATE TRIGGER evidence_freeze BEFORE UPDATE ON evidence
    FOR EACH ROW EXECUTE FUNCTION evidence_freeze();

-- The newest chain entry per site. Appends lock this row (FOR UPDATE), which
-- serialises them per site. seq 0 and 32 zero bytes are the genesis link.
CREATE TABLE site_chain_heads (
    site_id    uuid PRIMARY KEY,
    org_id     uuid NOT NULL,
    seq        bigint NOT NULL DEFAULT 0 CHECK (seq >= 0),
    head_hash  bytea NOT NULL DEFAULT decode(repeat('00', 32), 'hex') CHECK (length(head_hash) = 32),
    updated_at timestamptz NOT NULL DEFAULT now(),

    FOREIGN KEY (org_id, site_id) REFERENCES sites (org_id, id)
);

-- One entry per verified evidence, append-only. The byte encoding of the
-- hashes is pinned in internal/core/evidence/CHAIN.md.
CREATE TABLE evidence_chain (
    evidence_id        uuid PRIMARY KEY,
    org_id             uuid NOT NULL,
    site_id            uuid NOT NULL,
    seq                bigint NOT NULL CHECK (seq >= 1),
    content_sha256     bytea NOT NULL CHECK (length(content_sha256) = 32),
    server_received_at timestamptz NOT NULL,
    prev_chain_hash    bytea NOT NULL CHECK (length(prev_chain_hash) = 32),
    metadata_hash      bytea NOT NULL CHECK (length(metadata_hash) = 32),
    chain_hash         bytea NOT NULL CHECK (length(chain_hash) = 32),

    FOREIGN KEY (org_id, site_id) REFERENCES sites (org_id, id),
    FOREIGN KEY (site_id, evidence_id, content_sha256) REFERENCES evidence (site_id, id, content_sha256),

    -- A fork (two entries after the same link) fails here even if the lock
    -- is bypassed.
    CONSTRAINT evidence_chain_site_seq_unique UNIQUE (site_id, seq),
    CONSTRAINT evidence_chain_site_prev_unique UNIQUE (site_id, prev_chain_hash),
    CONSTRAINT evidence_chain_site_hash_unique UNIQUE (site_id, chain_hash)
);

ALTER TABLE evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON evidence FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE site_chain_heads ENABLE ROW LEVEL SECURITY;
ALTER TABLE site_chain_heads FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON site_chain_heads FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

ALTER TABLE evidence_chain ENABLE ROW LEVEL SECURITY;
ALTER TABLE evidence_chain FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant ON evidence_chain FOR ALL TO siteproof_app
    USING (org_id = app_org_id())
    WITH CHECK (org_id = app_org_id());

-- Migration 000008's default privileges grant UPDATE and DELETE on new tables.
-- The app may only change an evidence row's outcome, never what was captured.
-- Column-level UPDATE still allows SELECT ... FOR UPDATE.
REVOKE UPDATE, DELETE ON evidence FROM siteproof_app;
GRANT UPDATE (status, rejected_reason, object_version_id, retain_until, updated_at) ON evidence TO siteproof_app;
REVOKE UPDATE, DELETE ON evidence_chain FROM siteproof_app;
REVOKE DELETE ON site_chain_heads FROM siteproof_app;

COMMIT;
