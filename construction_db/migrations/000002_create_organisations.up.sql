BEGIN;

-- The tenant boundary: one organisation is one contracting company.
CREATE TABLE organisations (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    trading_name     text NOT NULL,
    registered_name  text,
    reg_no           text,
    vat_no           text,
    -- The edition chosen at signup; decides which dashboard the user lands in.
    primary_edition  text NOT NULL CHECK (primary_edition IN ('construction', 'mining')),
    -- An organisation can hold both editions (Rev 4, section 1.3).
    enabled_editions text[] NOT NULL,
    -- Set during onboarding, so nullable at signup.
    coida_insurer    text CHECK (coida_insurer IN ('fem', 'rma', 'compensation_fund')),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    archived_at      timestamptz,

    -- Every enabled edition must be a known one.
    CONSTRAINT enabled_editions_valid
        CHECK (enabled_editions <@ ARRAY['construction', 'mining']::text[]),
    -- The primary edition must be one of the enabled ones.
    CONSTRAINT primary_edition_enabled
        CHECK (primary_edition = ANY (enabled_editions))
);

COMMIT;