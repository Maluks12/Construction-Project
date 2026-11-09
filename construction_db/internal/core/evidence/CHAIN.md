# Evidence chain encoding

How SiteProof seals evidence into a per-site hash chain, specified to the
byte, so that anyone (an auditor, an expert witness, a future version of this
code) can recompute every hash without our software.

The vectors and the SQL query below are not just examples: `chain_test.go`
reads them from this file and checks the Go code and Postgres against them.
If you change this file, the tests change with it.

## What the chain shows, and what it doesn't

Each verified piece of evidence on a site gets one chain entry. Every entry's
hash covers the previous entry's hash, so inserting, deleting, reordering or
editing a sealed field of any earlier entry breaks every hash after it.

This makes tampering **detectable**. It does not make it impossible, and the
product must never be described as "tamper-proof" or "court-admissible". It
is built to the reliability factors in section 15 of the Electronic
Communications and Transactions Act 25 of 2002.

**What it can't show on its own.** Two changes by someone with write access
to the database leave a chain that still verifies:

- **Truncation:** deleting the newest entries and their evidence rows, and
  rewinding `site_chain_heads` to match. Every remaining entry links
  correctly to the one before it. (Deleting only the chain entries is
  caught: their evidence is left `verified` with no entry.)
- **A consistent rewrite:** editing a sealed field, then recomputing that
  entry's hashes, every hash after it, and the head. Nothing about the hash
  function prevents this; the chain only makes it more work.

`TestRecomputeSQL`, `TestChainWalker` and `TestVerifyChain` demonstrate
both. Catching them needs a site's head hash and length recorded somewhere
outside the database, to compare against later. The verify endpoint returns
exactly these (`head_hash`, `length`):

- the daily signed chain anchor (Rev 4 §2.1, P2), written to a separate
  append-only store;
- the chain head and length included in every audit pack export (M12), so a
  pack someone already holds proves what the chain was on that day.

Until one of those exists, the chain shows that the entries present are
consistent with each other, not that none were removed from the end or
rewritten since.

## The formulas

```
chain_hash    = SHA-256( content_sha256 || server_received_at || site_id
                         || prev_chain_hash || metadata_hash )

metadata_hash = SHA-256( format || evidence_id || client_captured_at
                         || location || uploaded_by || device_id )
```

`||` is byte concatenation. Every field is raw bytes, never hex or text.

Rev 4 §2.1 specifies the first four fields of `chain_hash`. `metadata_hash`
is our fifth field: without it the device timestamp, GPS, uploader and device
could be edited without breaking the chain. Rev 4's four fields keep their
order, so this extends the spec rather than replacing it. (See "Known spec
issues" in `construction_db/CLAUDE.md`.)

### Chain preimage: 120 bytes

| Field              | Bytes | Encoding                                                     |
|--------------------|-------|--------------------------------------------------------------|
| content_sha256     | 32    | SHA-256 of the stored file's exact bytes                     |
| server_received_at | 8     | int64 big-endian, microseconds since 1970-01-01T00:00:00Z    |
| site_id            | 16    | UUID, raw bytes                                              |
| prev_chain_hash    | 32    | previous entry's `chain_hash`; 32 zero bytes for the first   |
| metadata_hash      | 32    | see below                                                    |

### Metadata preimage: 60 bytes plus the device id

| Field              | Bytes | Encoding                                                     |
|--------------------|-------|--------------------------------------------------------------|
| format             | 1     | `0x01`. A future change to this encoding gets a new number   |
| evidence_id        | 16    | UUID, raw bytes                                              |
| client_captured_at | 8     | int64 big-endian, microseconds since the epoch (device clock)|
| location flag      | 1     | `0x00` no GPS fix, `0x01` fix present                        |
| lat                | 8     | IEEE-754 float64 big-endian; 8 zero bytes if no fix          |
| lng                | 8     | IEEE-754 float64 big-endian; 8 zero bytes if no fix          |
| uploaded_by        | 16    | UUID, raw bytes                                              |
| device_id length   | 2     | uint16 big-endian, in bytes; 0 if there is no device id      |
| device_id          | 0–200 | UTF-8                                                        |

### Rules

- **Everything is big-endian.**
- **Times** are microseconds since the Unix epoch, which is UTC by definition.
  Postgres stores `timestamptz` to the microsecond, so the value hashed is
  exactly the value stored. Times before 1970 are negative (two's complement).
  The hash inputs are always read back from the database, never taken from a
  request, and the Go encoder refuses a time with sub-microsecond digits.
- **The first entry** of a site links to 32 zero bytes (`GenesisHash`), never
  NULL: a database unique constraint on `(site_id, prev_chain_hash)` then
  makes a second "first entry" fail.
- **No GPS fix** and **a fix at exactly (0, 0)** differ in the location flag.
  lat and lng are both present or both absent. NaN and infinities are refused.
  `-0.0` keeps its sign bit, exactly as Postgres `float8send` does.
- **No device id** has length 0. An empty device id is refused, so it can't
  be confused with none. The length counts bytes, not characters (`Ü` is 2).
- **Correcting evidence** means recording new evidence. Sealed fields are
  never edited; the database enforces this (migration 000010).

## Test vectors

One site's chain of three entries. seq 1 has GPS and a non-ASCII device id;
seq 2 has neither; seq 3 has a GPS fix at exactly (0, 0) and a capture time
one microsecond before the epoch (a device whose clock was reset). Content
hashes are the SHA-256 of the ASCII text `siteproof vector N`.

The expected values were computed by Postgres (the SQL encoder below), not by
the Go code they test, and spot-checked with `sha256sum`. To check one with
any tool: `printf '%s' <preimage hex> | xxd -r -p | sha256sum`.

<!-- vectors -->
```json
[
  {
    "seq": 1,
    "site_id": "33333333-3333-4333-8333-333333333333",
    "evidence_id": "11111111-1111-4111-8111-111111111111",
    "client_captured_at": "2026-11-02T08:15:30.123456Z",
    "lat": -26.204103,
    "lng": 28.047305,
    "uploaded_by": "22222222-2222-4222-8222-222222222222",
    "device_id": "pixel-7a-Ü001",
    "metadata_preimage": "011111111111114111811111111111111100065f46bfc74ec001c03a3440181e03f7403c0c1c2e33eff222222222222242228222222222222222000e706978656c2d37612dc39c303031",
    "metadata_hash": "fb002a7aa7630781018f724a03693cccddf0cce5a1cbfcad1db345e6c3e8a484",
    "content_sha256": "cab8f8b1e9bc620e5aee6c6e0d5a60127fc553ef78d1051b0c64322881d78fd8",
    "server_received_at": "2026-11-02T08:20:00.000001Z",
    "prev_chain_hash": "0000000000000000000000000000000000000000000000000000000000000000",
    "chain_preimage": "cab8f8b1e9bc620e5aee6c6e0d5a60127fc553ef78d1051b0c64322881d78fd800065f46cfdd4c01333333333333433383333333333333330000000000000000000000000000000000000000000000000000000000000000fb002a7aa7630781018f724a03693cccddf0cce5a1cbfcad1db345e6c3e8a484",
    "chain_hash": "748d7329ca53f26b4f97fdc986a9b143d44e0214eea6b0e5f30a5b457fbfa82a"
  },
  {
    "seq": 2,
    "site_id": "33333333-3333-4333-8333-333333333333",
    "evidence_id": "11111111-1111-4111-8111-111111111112",
    "client_captured_at": "2026-11-02T09:00:00Z",
    "lat": null,
    "lng": null,
    "uploaded_by": "22222222-2222-4222-8222-222222222222",
    "device_id": null,
    "metadata_preimage": "011111111111114111811111111111111200065f475eea64000000000000000000000000000000000000222222222222422282222222222222220000",
    "metadata_hash": "f770eb61659fffbc7a047980bc5cd09f26648e42ef7fa210c81e84f2c30f9d88",
    "content_sha256": "83b08628088e303cb8534021e1e4f8e4f7a1b526a3eba6db69db429102923f98",
    "server_received_at": "2026-11-02T09:00:05.5Z",
    "prev_chain_hash": "748d7329ca53f26b4f97fdc986a9b143d44e0214eea6b0e5f30a5b457fbfa82a",
    "chain_preimage": "83b08628088e303cb8534021e1e4f8e4f7a1b526a3eba6db69db429102923f9800065f475f3e506033333333333343338333333333333333748d7329ca53f26b4f97fdc986a9b143d44e0214eea6b0e5f30a5b457fbfa82af770eb61659fffbc7a047980bc5cd09f26648e42ef7fa210c81e84f2c30f9d88",
    "chain_hash": "1bae8a95b5e1f89ab38e2878810e91bf9ee944109ee729278673484a17a7d804"
  },
  {
    "seq": 3,
    "site_id": "33333333-3333-4333-8333-333333333333",
    "evidence_id": "11111111-1111-4111-8111-111111111113",
    "client_captured_at": "1969-12-31T23:59:59.999999Z",
    "lat": 0,
    "lng": 0,
    "uploaded_by": "22222222-2222-4222-8222-222222222222",
    "device_id": null,
    "metadata_preimage": "0111111111111141118111111111111113ffffffffffffffff0100000000000000000000000000000000222222222222422282222222222222220000",
    "metadata_hash": "05095884e80d3e6fab6190c971dcf9af6d0fbb60473fa5f0c1e45f1a89f36853",
    "content_sha256": "41ead027203c56f56c703869f02e5360d8e1b92cbe0d1643d1150b2992a398cb",
    "server_received_at": "2026-11-02T09:00:05.500001Z",
    "prev_chain_hash": "1bae8a95b5e1f89ab38e2878810e91bf9ee944109ee729278673484a17a7d804",
    "chain_preimage": "41ead027203c56f56c703869f02e5360d8e1b92cbe0d1643d1150b2992a398cb00065f475f3e5061333333333333433383333333333333331bae8a95b5e1f89ab38e2878810e91bf9ee944109ee729278673484a17a7d80405095884e80d3e6fab6190c971dcf9af6d0fbb60473fa5f0c1e45f1a89f36853",
    "chain_hash": "ab67ee9e8d8a6804843e3d233e73eef37dabd028f6213e51298e8a918b9820de"
  }
]
```

## Verifying a site's chain in SQL

Anyone with read access to the database can check a site's chain with this
query alone. `$1` is the site id. Each row must have all four columns true:

- `seq_ok`: entries are numbered 1, 2, 3, … with no gaps;
- `prev_ok`: each entry links to the one before it (the first to 32 zeros);
- `metadata_ok`: the evidence row still matches its sealed metadata;
- `chain_ok`: the entry's hash is correct.

It is also the reference encoder: the test vectors above were produced with
these same expressions.

<!-- recompute-sql -->
```sql
SELECT c.seq,
       c.evidence_id,
       c.seq = row_number() OVER w AS seq_ok,
       c.prev_chain_hash = coalesce(lag(c.chain_hash) OVER w, decode(repeat('00', 32), 'hex')) AS prev_ok,
       sha256(
           decode('01', 'hex')
           || uuid_send(e.id)
           || int8send((extract(epoch FROM e.client_captured_at) * 1000000)::bigint)
           || CASE WHEN e.lat IS NULL THEN decode(repeat('00', 17), 'hex')
                   ELSE decode('01', 'hex') || float8send(e.lat) || float8send(e.lng) END
           || uuid_send(e.uploaded_by)
           || int2send(coalesce(octet_length(e.device_id), 0)::int2)
           || coalesce(convert_to(e.device_id, 'UTF8'), ''::bytea)
       ) = c.metadata_hash AS metadata_ok,
       sha256(
           c.content_sha256
           || int8send((extract(epoch FROM c.server_received_at) * 1000000)::bigint)
           || uuid_send(c.site_id)
           || c.prev_chain_hash
           || c.metadata_hash
       ) = c.chain_hash AS chain_ok
FROM evidence_chain c
JOIN evidence e ON e.id = c.evidence_id
WHERE c.site_id = $1
WINDOW w AS (ORDER BY c.seq)
ORDER BY c.seq;
```

Needs PostgreSQL 14 or later, where `extract(epoch …)` returns an exact
`numeric`.
