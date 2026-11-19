BEGIN;

-- M11 review: one sign-in at a time per worker per site, whatever order
-- the phones' records arrive in. A sign-in's presence ends at the earliest
-- of its sign-out, 16 hours after it, or the worker's next standing sign-in
-- on the same site. So a 07:00 sign-in that arrives after an 08:00 one is
-- accepted (nothing covered 07:00) and simply ends at 08:00.
--
-- site_sign_in_covering (000026) refuses a sign-in inside another's
-- presence; it reads this function, as do the overlaps list (other sites
-- only) and toolbox not_signed. The sign-out guard (000026) already refuses
-- a sign-out after the next sign-in, so signing the 07:00 one out before
-- 08:00 still works.
CREATE OR REPLACE FUNCTION site_sign_in_ends_at(s site_sign_ins) RETURNS timestamptz
    LANGUAGE sql STABLE
    AS $$
        SELECT least(
            coalesce(s.client_signed_out_at, s.client_signed_in_at + site_sign_in_window()),
            (SELECT min(n.client_signed_in_at) FROM site_sign_ins n
             WHERE n.site_id = s.site_id AND n.worker_id = s.worker_id AND n.id <> s.id
               AND n.retracted_at IS NULL AND n.client_signed_in_at > s.client_signed_in_at))
    $$;

COMMIT;
