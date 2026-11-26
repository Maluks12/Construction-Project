BEGIN;

-- SYNC D7: a satisfaction is a capture like any other, so it names the
-- device it was recorded on, and an identical repeat compares it.
ALTER TABLE deadline_satisfactions
    ADD COLUMN device_id text CHECK (octet_length(device_id) BETWEEN 1 AND 200);

COMMIT;
