# Backend: Go API

Read `../CLAUDE.md` first (shared rules and the API contract with `web/`).
Run everything from `construction_db/`; the Go module lives here, not at the
repo root.

Known risks (each has a DEPLOY task in `docs/BUILD_PLAN.md`):
- The API connects with the `POSTGRES_USER` superuser and drops privilege
  with `SET ROLE siteproof_app` on each connection. SQL running in the app
  could `RESET ROLE`. Production must use a LOGIN role instead.
- `users` has no RLS (it has no `org_id`). A buggy query could list every
  user's name, email and mobile across orgs.

Known spec issues:
- Rev 4 §2.1 says the hash chain is per project; everywhere else says per
  site. Use **per site**.
- The chain is ordered by server receipt, so concurrent offline uploads need a
  per-site chain-head lock or the chain forks.
- Rev 4 §2.1's chain formula has four fields and leaves the device timestamp,
  GPS, uploader and device unsealed, although Rev 4 presents dual timestamps
  as integrity evidence. We append a fifth field, `metadata_hash` (decided
  2 Nov 2026). Rev 4's four fields keep their order, so it's an extension,
  not a replacement. Corrections are new records, never edits to sealed
  fields.
- Phase 1 is about 105 features; section 6.D's "if it must shrink" list is the
  realistic scope.
- Rev 4 §1.4 keeps regime content in `regime_*` database tables. We embed
  JSON in the binary instead (`internal/regimes/data/`): it is reviewed in
  git and validated by tests, but a regulatory change needs a redeploy.
- The column lists in Rev 4 §1.4 are shifted down one row (deadline columns
  are printed against `regime_document_types`, and so on). Read correctly:
  deadline rule = `rule_code, period, unit, basis, source_ref`; vocabulary =
  `term_key, label`; incident taxonomy = `code, label, reportable,
  report_rule_code`.
- Three statements in Rev 4 contradict the law. The quotes and sources are in
  `internal/regimes/SOURCES_za_construction_2014.md`:
  - §3.2: Reg 4 is not "for all other work". It applies only to excavation,
    work at height, demolition, explosives, or a single-storey dwelling for
    the client.
  - §3.5: the s24 notice goes on WCL1/WCL2 (GAR 8(1)(a)), not "Annexure 1".
    Annexure 1 is the GAR 9 incident record.
  - §3.5: late COIDA reporting is no longer "an offence". Since 1 Apr 2026,
    s39(6) and (8) impose administrative penalties instead.

## Commands

```
docker compose up -d db minio           # start Postgres 16 and MinIO (S3) on :9000
docker compose run --rm storage-init    # once: create the Object Lock evidence bucket
docker compose run --rm migrate up      # apply migrations
docker compose up -d --build api        # build and run the API on :8080
gofmt -l .                              # must print nothing (CI fails otherwise)
go vet ./...
go test ./...                           # needs Docker Desktop running (testcontainers)
```

## Layout

- `cmd/api`: the server. `cmd/worker`: empty for now.
- `internal/core/{auth,sites,invites}`: features. Each has model.go, repo.go,
  service.go, handler.go, routes.go.
- `internal/core/evidence`: the evidence feature (model, repo, service,
  handler, routes; see Evidence engine for the endpoints), plus the chain
  encoding (`chain.go`: `MetadataHash`, `ChainHash`, `GenesisHash`) and its
  specification `CHAIN.md`. Site scoping goes through
  `sites.Service.GetInTx`, the same rule as `GET /v1/sites/:id`.
  `internal/core/evidence/evidencetest` reads `CHAIN.md` for any test:
  `Block(marker)` and `Recompute(ctx, db, siteID)`, which runs the
  published verification SQL.
  `chain_test.go` reads the test vectors and the SQL verification query
  from `CHAIN.md`, so the document can't drift from the code. Hash inputs
  must be read back from Postgres: the encoder refuses a time with
  sub-microsecond digits.
- `internal/platform/{config,database,dbtest,health}`.
- `internal/platform/storage`: the `Store` interface for evidence objects and
  its S3 implementation (also used against MinIO). Presigned PUTs sign the
  SHA-256, the length and `If-None-Match: *`, so S3 refuses other bytes, any
  other size, and a second upload to the key. Without the length, a client
  could declare a small size with a huge file's hash and park that file
  under Object Lock. `Content-Length` and `Host` are signed but not returned
  in the headers: HTTP clients set them. `CheckObjectLock` runs at startup: the API exits
  unless the bucket's default retention is in `EVIDENCE_LOCK_MODE`.
  `storagetest.Start` runs MinIO (testcontainers) with a locked bucket.
- MinIO no longer publishes images. Dev and tests use Chainguard's build,
  pinned by digest in both `docker-compose.yml` and `storagetest.Image`; bump
  them together.
- `internal/archtest`: layer rules checked against the real import graph
  (core must not depend on editions; editions not on each other).
- `internal/core/edition`: the `Edition` interface, the only thing core and
  app know about an edition. `internal/editions/construction` implements it;
  core never imports `internal/editions`.
- `internal/regimes`: regime data sets embedded from
  `data/<regime code>/{regime,checklists,deadlines,taxonomies,vocabulary}.json`.
  `Load()` validates them; `CheckDatabase` runs at startup and the API exits
  if a `regulatory_regimes` row has no data set with the same version and
  edition. `testdata/shipped.json` locks shipped keys: a test fails if one
  disappears. Add new keys to it when they ship.
- `internal/app/router.go` wires everything; `internal/app/*_test.go` are the
  HTTP integration tests (testcontainers Postgres, migrations applied
  automatically).
- `migrations/`: golang-migrate files, `00000N_name.up.sql` / `.down.sql`.
- CI (`.github/workflows/ci.yml`): gofmt, vet, `go test -race`, build, Docker
  image to GHCR.

When showing a `routes.go`, always show the full file.

## Evidence engine (building, M6)

The client compresses to a 2048px long edge, hashes with SHA-256, and PUTs
the bytes straight to S3 with a presigned URL. The server then re-hashes the
exact stored version. Dual timestamps. Per-site chain:

`chain_hash = SHA256(content_sha256 || server_received_at || site_id || prev_chain_hash || metadata_hash)`

`metadata_hash` seals `evidence_id`, `client_captured_at`, `lat`/`lng`,
`uploaded_by` and `device_id` (see Known spec issues). The exact byte
encoding and test vectors are in `internal/core/evidence/CHAIN.md`.

- Endpoints (owner, SHE officer, supervisor on their own sites; others 403):
  - `PUT /v1/sites/:id/evidence/:evidenceId` declares evidence. 201 with an
    `upload` block (`method`, `url`, `headers` to send unchanged,
    `expires_at`, 15 min). The URL signs the length: the body must be
    exactly `size_bytes`, or S3 refuses it and stores nothing. Repeating the identical declaration is 200
    (pending: fresh URL; verified: no `upload`). 409 if the id was used with
    different details (any sealed field, including uploader and site) or was
    rejected. 400 for a malformed id or invalid fields.
  - `GET /v1/sites/:id/evidence?status=&limit=` lists newest first
    (`{"evidence": [...]}`, limit 1–200, default 50).
  - `GET /v1/sites/:id/evidence/:evidenceId`. `chain` is null until verified.
  - `POST /v1/sites/:id/evidence/:evidenceId/complete` (uploader only;
    anyone else 404). Re-checks the version S3 stored and, if it passes,
    seals it into the site's chain: 201 with `chain`. Already verified: 200
    (idempotent). No object yet: 409 `"file not uploaded yet"` (stays
    pending). A failed check is 400 and the evidence becomes `rejected`
    for good, with `rejected_reason` one of `size_mismatch`,
    `hash_mismatch`, `not_an_image`, `wrong_type`, `too_large_dimensions`.
    The 400 body carries the code too:
    `{"error": "...; declare a new evidence id", "rejected_reason": "..."}`.
    An object without Object Lock retention is a 500 (bucket fault; stays
    pending).
- Complete reads the stored file as one stream in constant memory
  (`complete.go` `inspect`): `LimitReader(10 MiB + 1)` → `TeeReader` into
  `MultiWriter(sha256, counter)` → `bufio.Reader`. `image.DecodeConfig`
  reads the header from that stream, however much EXIF/ICC metadata comes
  first, then the rest is drained, so every byte is hashed exactly once.
  Checks run size, hash, then the decode result. A read error, even one
  the decoder swallowed, is a 500 and the evidence stays pending; it is
  never turned into a rejection that would burn the id.
- Complete reads exactly the version `Stat` saw: a version written in
  between can't swap what is checked and sealed
  (`TestEvidenceCompleteReadsTheStatVersion`).
- The chain append (`appendToChain`) runs after the S3 reads, in one short
  transaction: ensure and lock the site's head row (`FOR UPDATE`), lock the
  evidence row, take `clock_timestamp()`, compute the hashes from the rows
  as read from Postgres, insert the entry, advance the head, mark
  verified. Lock order is always head, then evidence.
- An evidence id is single-use. Its storage key is fixed and the upload URL
  allows one PUT, so after a rejection the client must declare a **new**
  evidence id; the rejected response says so.
- Migration 000010: `evidence` (declared metadata and status), append-only
  `evidence_chain`, and `site_chain_heads` (locked FOR UPDATE to serialise
  appends per site). The app role can update only an evidence row's outcome
  columns. The `evidence_freeze` trigger freezes verified and rejected rows
  for every role, except that `retain_until` may be extended (legal hold or
  claim), never shortened or cleared.
- Evidence rows pin their site and uploader's membership (foreign keys), so
  a future "remove member" must deactivate, not delete.
- Testing uploads from a real phone: set `S3_PUBLIC_ENDPOINT` to this
  machine's LAN address (e.g. `http://192.168.1.20:9000`). It is part of
  the presigned URL's signature and the phone can't reach `localhost`. Allow
  inbound TCP 9000 in Windows Firewall and keep the phone on the same Wi-Fi.

## Conventions (learned the hard way)

- **Tenant transactions (RLS):** every query on a tenant table runs inside
  `database.WithTenant(ctx, s.pool, database.Tenant{OrgID, UserID}, func(tx)
  …)`, with `tx` passed to every repo call. No single statements on the pool
  touch tenant tables: under RLS they see nothing. Inside `fn`, `s.pool` must
  not appear. Change the tenant mid-transaction with `database.SetTenant`
  (login, signup, invite accept). Only the token lookups
  (`RefreshTokenTenant`, `InvitationOrg`) run on the pool, before the tenant
  is known. Edition code follows the same rule; `Deps.Pool` alone sees
  nothing.
- `WithTenant` commits only if `fn` returns nil. To persist a write and still
  report an error (refresh reuse revokes the session), return nil from `fn`
  and return the error after it.
- Sentinels sent with `err.Error()` must reach the handler unwrapped; wrap
  only other errors (`invites.wrap` does this).
- Test fixtures that write raw SQL use `execAsTenant` (tenant tables) or
  `testAdmin` (`regulatory_regimes`, which the app role can only read) and
  assert the rows changed. Under RLS a mis-scoped write changes 0 rows
  silently.
- Repo functions take `db DBTX` so they work with the pool or a tx.
- `Scan` takes pointers (`&x.ID`), in exactly the same order as the SELECT
  list. `go vet` does not catch a missing `&`.
- Nullable columns scan into pointers (`*string`, `*time.Time`).
- List functions start from `items := []T{}` so JSON is `[]`, not `null`.
- Wrap errors with `fmt.Errorf("prefix: %w", err)`; one prefix per function.
- Service errors are sentinel `ErrX` values in model.go; each handler package
  has a `writeError` switch mapping them to status codes. Its default branch
  calls `httpapi.WriteUnexpected(c, "request failed", err)`, and so does any
  middleware with an unexpected error. Never write a 500 by hand:
  - out of time (`errors.Is(err, context.DeadlineExceeded)`, which includes
    waiting for a pool connection) is **503** `"server busy, try again"`
    with `Retry-After: 2`, logged at warn;
  - the client went away (`context.Canceled`) is 499 with no body, logged
    at info: not a server error, and nobody receives it;
  - anything else is 500 `"internal error"`, logged at error (never send
    err.Error() for a 500).
- **One deadline per request:** `httpapi.Deadline(REQUEST_TIMEOUT)`, set in
  `NewRouter` right after Recovery. Pass `c.Request.Context()` down; don't
  add your own timeouts. A future long-running endpoint (e.g. the M12 audit
  pack export) needs its own, longer deadline.
- **Pool size:** `DB_MAX_CONNS` (default 20) per API instance. Keep the sum
  across instances below Postgres's `max_connections` (100 by default) minus
  headroom for `migrate`, psql and admin tools. `main.go` logs it at
  startup. Tests use `dbtest.MaxConns` (10).
- `isNotFound(err)` treats `pgx.ErrNoRows` and SQLSTATE `22P02` (malformed
  UUID) as not found. `isUniqueViolation(err, "constraint_name")` checks
  `23505`.
- "Did it change a row" updates return `(bool, error)` via
  `tag.RowsAffected() == 1`.
- An edition route that takes a site must confirm the site's regime belongs
  to that edition (via `sites.Service.Regime`) and return 404 if not.
  Mounting under `/v1/construction` doesn't guarantee that by itself.
- Org, user and role always come from the token (`auth.OrgID(c)` etc.), never
  the body.
- Emails compare with `lower()`.
- `.gitattributes` forces LF for Go files; gofmt in CI depends on it.

## Status

- Full plan: `docs/BUILD_PLAN.md`. Tick tasks there as they're done. When a
  step changes the plan or a decision, update this section.
- Foundation (M4, M5) due Sun 1 Nov. Safety core (M6–M12) due Sun 22 Nov,
  which is the checkpoint: if behind, cut JBCC delay claims, then
  principal-contractor tier, then IOD form assembly.
- **Done:** migrations 000001–000007; auth (email and mobile login, refresh
  rotation with reuse detection, sessions); sites, site members, supervisor
  scoping, locations; invites (send, list, revoke, preview, accept);
  integration tests use real invites.
- **Milestone 5, Regimes and the edition boundary** (due Sun 1 Nov): **done**.
  - 5a (Wed 28 Oct), **done**: `internal/regimes` package (see Layout).
    Added `vocabulary.json` as a fifth file, and the keys-only lock in
    `testdata/shipped.json`. Rule: a shipped key never changes meaning.
  - 5b (Thu 29), **done**: `za_construction_2014` has 7 deadline rules, 14
    incident types, 5 checklists and 10 vocabulary terms, each quoted in
    `internal/regimes/SOURCES_za_construction_2014.md`. No scaffolding
    checklist yet (see Gaps there).
  - 5c (Thu 29), **done**: `sites.Service.Regime` is the only way to get a
    site's regime data (via `sites.regime_id`, same scoping as `GET
    /v1/sites/:id`). The default edition and regime for new orgs and sites
    come from the edition (see 5d). At startup, `Registry.CheckDefault`
    checks the data set and edition, and `regimes.CheckDefaultRow` checks the
    `regulatory_regimes` row.
  - 5d (Fri 30), **done**: `core/edition.Edition` (name, default regime,
    route hook) is implemented by `editions/construction` and passed in by
    `cmd/api/main.go`. Edition routes mount under an authenticated
    `/v1/<edition>` group and get `edition.Deps` (the pool and
    `*sites.Service`); construction registers none yet.
  - 5e (Fri 30), **done**: `internal/archtest` fails if anything outside
    `cmd/`, `internal/app` and `internal/editions` depends on an edition
    (transitively, test code included), or one edition depends on another.
    Runs in `go test ./...` and as its own CI step.
  - 5f (Sat 31), **done**: migration 000008 (`siteproof_app` role,
    `app_org_id()`/`app_user_id()`, SECURITY DEFINER token lookups) and
    000009 (RLS forced on organisations, org_members, sessions,
    refresh_tokens, sites, site_members, site_locations, invitations). Tenant
    set per transaction by `database.WithTenant` with `set_config(..., true)`.
  - 5g (Sun 1 Nov), **done**: `internal/platform/database/rls_test.go`
    runs unfiltered SELECT/INSERT/UPDATE/DELETE on all 8 RLS tables and
    checks that only the tenant's rows are reached. `TestRLSCoverage` fails
    the build if a table with `org_id`, or any public table not on its
    allowlist, lacks forced RLS and a policy. A new tenant table needs a
    policy in a migration, or an allowlist entry with a reason.
- **Milestone 6, Evidence engine** (due Fri 6 Nov). Plan approved Mon 2 Nov:
  presigned direct-to-S3 PUT; the server re-hashes the exact stored version;
  per-site chain appended under a `site_chain_heads` row lock;
  `metadata_hash` as a fifth chain field. Production lock mode and retention
  are decided at DEPLOY.
  - 6.1 (Mon 2 Nov), **done**: `internal/platform/storage` and `storagetest`
    (see Layout), MinIO in compose, S3 config, startup lock check. Tested on
    MinIO: other bytes → 400 and nothing stored; a missing signed header →
    4xx (AWS answers 403); a second PUT → 412. New env vars: `S3_BUCKET`,
    `EVIDENCE_LOCK_MODE` (both required), `S3_REGION` (default
    `af-south-1`), `S3_ENDPOINT`, `S3_PUBLIC_ENDPOINT`, `S3_ACCESS_KEY_ID`,
    `S3_SECRET_ACCESS_KEY`. All listed in `.env.example`.
  - 6.2 (Mon 2 Nov), **done**: migration 000010 (see Evidence engine), RLS
    on the three tables, and tests (`rls_test.go` now states each table's
    expected UPDATE and DELETE outcome; `TestEvidenceIsAppendOnly`). MinIO
    healthcheck in compose. `storage.Open` omits an empty version id.
  - 6.3 (Mon 2 Nov), **done**: chain encoding pinned in
    `internal/core/evidence/CHAIN.md` (see Layout). The three vectors were
    computed by Postgres, spot-checked with `sha256sum`, and reproduced by
    the Go encoder. `TestRecomputeSQL` runs the doc's SQL on the real schema
    and shows it catches a 1 µs edit and a deleted entry, even with the
    freeze trigger off.
  - 6.4 (Mon 2 Nov), **done**: declare, list and get evidence (see Evidence
    engine). `NewRouter` now takes a `storage.Store`; `internal/app` tests
    start MinIO too (`testStore`). First contract change of M6.
  - Interposed (Mon 2 Nov), **done**: busy is 503, not 500 (see
    Conventions). `RequireAuth`'s own 2s timeout is gone; one request
    deadline (`REQUEST_TIMEOUT`, default 10s) covers everything. Pool size
    from `DB_MAX_CONNS`. Found by commit 4's concurrency test: a burst larger
    than the pool made auth answer 500 after 2s. Contract change: 503.
  - 6.5 (Fri 6 Nov), **done**: complete endpoint (see Evidence engine):
    stream-check the stored bytes, then append to the site chain under the
    head-row lock.
  - 6.6 (Mon 9 Nov), **done**: `TestParallelUploadsGiveOneChain` fires 20
    completes on one site and 5 on another at once (a barrier after the
    uploads makes them contend for the lock) and checks each chain in Go
    and with `CHAIN.md`'s SQL. Without `FOR UPDATE` on the head, about 16
    of 20 fail with 500 on the unique constraints, and no fork is stored.
    `TestForkFailsLoudly` names the constraint that stops each kind of
    fork. The constraints don't catch a seq gap; the verify endpoint must.
  - Schedule: M6 was due Fri 6 Nov. The verify endpoint (6.7) and docs
    (6.8) land Tue 10 Nov, four days late; M7, including the access log
    due Mon 9 Nov, moves with it. Decide any cuts at the Sun 22 Nov
    checkpoint.

## Requested by the frontend

- `/v1/me` to include `role`, `full_name` and `org_name` (the dashboard
  currently guesses the role from 403s).
- An endpoint listing the organisation's active members, so the dashboard can
  add people to a site.
