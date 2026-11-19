BEGIN;

CREATE OR REPLACE FUNCTION site_sign_in_ends_at(s site_sign_ins) RETURNS timestamptz
    LANGUAGE sql STABLE
    AS $$ SELECT coalesce(s.client_signed_out_at, s.client_signed_in_at + site_sign_in_window()) $$;

COMMIT;
