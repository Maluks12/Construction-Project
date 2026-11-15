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
- Found in M7 (Nov 2026):
  - **Access log phase.** Rev 3 A.9 makes the evidence access log P2;
    Rev 4 §2.1 and A.2 make it P1. We follow Rev 4.
  - **Records before photos versus photo on fail.** Rev 4 §2.2 sends
    records before photos, while §2.4 makes a photo mandatory on a fail.
    Resolved: answers sync before photos; only completion waits for
    verified photos. The phone keeps "complete" queued until then.
  - **No severity scale.** Rev 4 §2.5 gives corrective actions a severity
    with no scale; Rev 3 put a severity on each template item. We use a
    fixed core scale with one-sentence definitions (Finding severity,
    below). A practitioner has yet to review the wording.
  - **No finding entity.** Rev 4 goes from a failed item straight to "can
    raise a corrective action"; Rev 3 says every fail generates one.
    BUILD_PLAN adds findings in between. M9 decides whether every finding
    needs an action.
  - **Naming.** Rev 4 §5.3 says `checklist_instances` and `responses`;
    BUILD_PLAN says inspections. The tables are `inspection*`, in package
    `internal/core/checklists` (Rev 4 §5.2).
  - **Template versions.** Rev 4 promises a 2027 checklist reproducible in
    2030. Satisfied: versions are append-only in the data and locked by
    hash, and each inspection copies its wording.
  - **Off-instance audit log.** Rev 4 §2.13 wants the audit log shipped
    off-instance. The access log is append-only against every role, but a
    superuser could still drop its trigger. Shipping it off-instance is
    DEPLOY/P2 work, like the chain anchor.
  - **Rev 4 §2.4 items not built in M7** (not in BUILD_PLAN):
    - reference photos per item;
    - scheduling and cadence enforcement;
    - organisation-specific templates and the custom builder (A.5, P1);
    - housekeeping "scored by zone" (§3.3);
    - response types other than pass/fail/N-A.
- Found in M8 (Nov 2026):
  - **Several incident types per version.** Rev 4 §5.3 gives `incidents` one
    `taxonomy_code`. One event can fall under several paragraphs of OHSA
    s24(1), e.g. a pressure release (s24(1)(c)(ii), forthwith notice only)
    that knocks someone unconscious (s24(1)(a), which also needs the 7-day
    written notice). A single type would drop a statutory clock when the
    wrong one is picked. So a version carries `taxonomy_codes` (one or
    more), and its clocks are the union of their rules (decided 15 Nov).
  - **Typed deadline subjects.** Rev 4 §5.3's `deadlines (subject_type,
    subject_id, ...)` can't have a foreign key. `deadlines` has one nullable
    foreign-key column per subject (`incident_id` now; site work and IOD
    claims later) with exactly one set.
  - **Counting days.** No source in the regime says how "within seven days"
    counts. Interpretation Act s4 does, and extends a last day on a Sunday or
    public holiday. We store both: `due` (no extension, used for alerts) and
    `legal_latest` (with s4). See `SOURCES_za_construction_2014.md`.
  - **Reg 4 has no task.** Rev 4's construction pack makes "Regulation 4
    notification" P1, but BUILD_PLAN had no task for it; one is now
    proposed there.

## Commands

```
docker compose up -d db minio           # start Postgres 16 and MinIO (S3) on :9000
docker compose run --rm storage-init    # once: create the Object Lock evidence bucket
docker compose run --rm migrate up      # apply migrations
docker compose up -d --build api        # build and run the API on :8080
gofmt -l .                              # must print nothing (CI fails otherwise)
go vet ./...
go test ./...                           # needs Docker Desktop running (testcontainers)
go test -p 4 ./...                      # locally on Windows: at most 4 packages at once
```

On Windows, run the tests locally with `-p 4`. Several packages each start
their own Postgres (and MinIO) containers. With every package at once,
Docker Desktop intermittently fails to start a container ("rootless Docker
is not supported on Windows", which really means it couldn't connect), or
stalls until requests time out. CI keeps the default. `dbtest.Start` and
`storagetest.Start` also retry a failed container start once, after 2s
(`containertest.StartWithRetry`). A real fault fails twice and reports
both errors.

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
  SHA-256, the length, the declared `Content-Type` and `If-None-Match: *`,
  so S3 refuses other bytes, any other size, any other type, and a second
  upload to the key. Without the length, a client
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
  disappears. Add new keys to it when they ship. **Checklist template
  versions are append-only.**
  - Never edit or remove a shipped version.
  - To change wording, copy the latest entry in `checklists.json`, edit it,
    give it the next `version`, and leave the old one in place. A key's
    versions sit together, oldest first.
  - `shipped.json` locks every shipped version's content hash, and
    `TestChecklistVersionsLocked` prints the entry to paste.
  - A change of meaning still needs a new key.
  - `DataSet.Latest()` is what the catalogue offers;
    `DataSet.Checklist(key, version)` finds any shipped version for the
    inspection engine.
- `internal/core/deadlines`: the deadline engine (Rev 4 §2.3). Pure Go
  rule selection and planning (`engine.go`), storage (`repo.go`). Due
  dates are computed only by Postgres's `deadline_due` (000015), from the
  `public_holidays` table. Tests here start their own Postgres.
- `internal/core/incidents`: the incident register, witness statements,
  and the deadline endpoints (`docs/api/incidents.md`).
- `internal/core/checklists`: the checklist engine (Rev 4 §2.4).
  `GET /v1/sites/:id/checklists` returns the site regime's templates
  (through `sites.Service.Regime`, never hardcoded) and the severity scale.
  `sites.Service.RegimeInTx` resolves a site's regime, with its
  `regulatory_regimes.id`, inside the caller's transaction.
  Inspections live here too (`inspections.go`, `repo.go`); their endpoints
  are in `docs/api/inspections.md`.
- `internal/platform/apidoctest`: checks a `docs/api/*.md` reference's
  TypeScript against the Go types (`Load`, `CheckStruct`, `CheckFields`,
  `CheckUnion`). Each feature's `TestAPIDoc` uses it. It reads fields by
  `encoding/json`'s rules: an untagged embedded struct is flattened, and
  an untagged exported field counts under its Go name
  (`TestFieldsMatchEncodingJSON`).
- `internal/app/router.go` wires everything; `internal/app/*_test.go` are the
  HTTP integration tests (testcontainers Postgres, migrations applied
  automatically).
- `migrations/`: golang-migrate files, `00000N_name.up.sql` / `.down.sql`.
- CI (`.github/workflows/ci.yml`): gofmt, vet, `go test -race`, build, Docker
  image to GHCR.

When showing a `routes.go`, always show the full file.

## Evidence engine (M6, done Mon 9 Nov)

The frontend's reference is `docs/api/evidence.md`: the upload flow, every
endpoint with its exact status codes and error texts, and the TypeScript
types. `TestAPIDoc` checks its types against the Go structs and constants,
so when an evidence endpoint changes, update the reference in the same
commit.

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
  - `POST /v1/sites/:id/evidence/:evidenceId/view` (M7) returns a
    60-second presigned GET of the sealed version, served as the verified
    `content_type`. The `view_bytes` row commits before the URL is signed.
    It is 409 `"evidence not verified"` for pending or rejected evidence.
    Only `evidence/access.go` may call `Store.PresignGet`, and only
    `complete.go` may call `Store.ReadVersion`; `internal/archtest`
    (`TestEvidenceBytesGuard`) enforces it.
  - `GET /v1/sites/:id/chain` (owner and SHE officer only; others 403)
    walks the site's chain as stored and returns `{site_id, intact, length,
    head_seq, head_hash, first_break, checked_at}`. `first_break` is null
    or `{seq, evidence_id, reason}`, with the reasons checked in this
    order: `seq_gap`, `prev_mismatch`, `metadata_changed`,
    `chain_hash_mismatch`, `entry_without_verified_evidence` (per entry;
    the last means the freeze trigger was switched off), then
    `head_mismatch`, then `verified_without_entry` (seq null). Walking a
    long chain can exceed `REQUEST_TIMEOUT`; the M12 audit pack may need to
    run it in `cmd/worker`. `intact` means no break found,
    never "tamper-proof": a consistent rewrite or a full truncation still
    verifies (`CHAIN.md`), so `head_hash` and `length` are what to record
    outside the database (M12 audit pack).
- `verify.go`: the walk reads one snapshot (`database.WithTenantSnapshot`)
  and streams entries through `chainWalker`, so memory doesn't grow with the
  chain. `afterHeadRead` is a test hook (no-op in production) that proves
  the snapshot (`TestVerifyChainReadsOneSnapshot`).
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

## Two levels of rigour (decided Sun 15 Nov)

Pick the level when planning a step, and say which in the plan.

**Full rigour** is for anything that seals, freezes or proves: evidence,
inspections, findings, incidents, deadlines, corrective actions, and the
audit pack. These records end up in front of an inspector or a court, so
a bug can't be fixed after the fact.
- The database enforces the rules (constraints, triggers, grants), not just
  the API.
- Mutation checks: break each rule on purpose, show a test fails, then
  restore it.
- Race tests wherever two requests can meet. Prefer a forced, deterministic
  collision (hold a transaction open) over firing requests and hoping.
- The API doc is checked by `TestAPIDoc`, including every error text.
- Full status notes in Status.

**Light rigour** is for ordinary CRUD: the worker register, toolbox talks,
the site diary, settings.
- Tests cover the happy path, scoping and RLS, and validation.
- The API doc is updated and checked by `TestAPIDoc`.
- No mutation checks unless something is subtle; say so in the plan when
  one is.
- Short status notes: what was added and any contract change.

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
- **Reads that must agree** (a head row and the entries it summarises) run
  in `database.WithTenantSnapshot`: read-only REPEATABLE READ, so commits
  made by others mid-way are invisible. Plain `WithTenant` reads can see a
  commit between two queries.
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
- **Evidence reads are logged.** These count as a view of evidence, and
  each writes an `evidence_access_log` row (`evidence.InsertAccess`) in the
  same transaction as the read, so a failed log write fails the read:
  - **the bytes:** `POST …/evidence/:evidenceId/view`, a `view_bytes` row;
  - **the full evidence record:** `GET …/evidence/:evidenceId`, a
    `view_record` row;
  - **a list:** `GET …/evidence`, one `list` row per request;
  - later, exports and shares (M12).

  **Not a view** (decided 15 Nov): an inspection showing an attached
  photo's `evidence_id`, `client_captured_at` and `captured_before_start`
  (`checklists.ListAttached`). The photo itself still goes through the
  logged `view`. Any new code that returns more of an evidence record than
  those three fields counts as a view and must log it.

  Take the client from `clientOf(c)`. `c.ClientIP()` believes
  `X-Forwarded-For` only from `TRUSTED_PROXIES` (default none; Gin's own
  default trusts every proxy).
- Emails compare with `lower()`.
- `.gitattributes` forces LF for Go files; gofmt in CI depends on it.

## Status

- Full plan: `docs/BUILD_PLAN.md`. Tick tasks there as they're done. When a
  step changes the plan or a decision, update this section.
- Foundation (M4, M5) due Sun 1 Nov. Safety core (M6–M12) due Sun 22 Nov,
  which is the checkpoint.
- **Forecast (Sun 15 Nov, revised after M7 finished that day; no cuts
  decided, full scope stays).** Sat 5 Dec is not reachable. The dates are
  recomputed from M8 starting Mon 16 Nov, with the per-milestone estimates
  unchanged. Reasons and risks are in `docs/BUILD_PLAN.md` ("Forecast").

  | Milestone | Rigour | Forecast finish | Was due |
  |---|---|---|---|
  | M7 Inspections & checklists | full | **done Sun 15 Nov** | Tue 10 Nov |
  | M8 Incidents (with the deadline engine) | full | Sat 21 Nov | Fri 13 Nov |
  | M9 Corrective actions | full | Tue 24 Nov | Sun 15 Nov |
  | M10 Toolbox talks & site diary | light | Thu 26 Nov | Tue 17 Nov |
  | M11 People & documents | light | Mon 30 Nov | Fri 20 Nov |
  | M12 Reports & audit pack | full | Thu 3 Dec | Sun 22 Nov |
  | SYNC Offline sync API | full | Mon 7 Dec | Wed 25 Nov |
  | BILL Billing | light; webhooks full | Thu 10 Dec | Fri 27 Nov |
  | DEPLOY | – | Sat 12 Dec | Sat 28 Nov |
  | IOD Injury on duty | full | Tue 15 Dec | Tue 1 Dec |
  | JBCC Delay claims | full | Thu 17 Dec | Wed 2 Dec |
  | PC Principal contractor tier | full | Sat 19 Dec | Fri 4 Dec |
  | V1 Release | – | **Sun 20 Dec** | Sat 5 Dec |
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
    healthcheck in compose. `storage.Open` (now `ReadVersion`, 7.2b) omits an empty version id.
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
  - 6.7 (Mon 9 Nov), **done**: `GET /v1/sites/:id/chain` (see Evidence
    engine). Every check is mutation-tested; `CHAIN.md` now documents the
    consistent-rewrite limit alongside truncation.
  - 6.8 (Mon 9 Nov), **done**: `docs/api/evidence.md` for the frontend,
    guarded by `TestAPIDoc`. **M6 is done**, three days after its Fri 6 Nov
    due date. M7 starts with the evidence access log (it was due Mon 9 Nov)
    and runs about three days behind; decide any cuts at the Sun 22 Nov
    checkpoint.
- **Milestone 7, Inspections and checklists** (due Tue 10 Nov): **done Sun
  15 Nov**, five days late.
  - **What it delivers:**
    - the evidence access log and logged photo viewing;
    - checklist templates from the site's regime, versioned append-only and
      locked by hash;
    - inspections that can be started, answered (photos attached),
      completed, corrected (once, on the same checklist) and abandoned;
    - findings with a severity, one per failed item, raised by the
      database at completion.
  - **The database enforces every rule.** Completed and abandoned
    inspections never change for any role. Mutation checks and forced race
    tests cover each step.
  - **End-to-end walk** on the real API, Postgres and MinIO passed twice
    (sign up → photo → inspection → refused completion → late resend
    refused → completion with a finding → photo view → abandon). The
    script is not in the repo; repeat it by hand as in the plan.
  - **Frontend handover:** `docs/api/m7-handover.md`.
  - Plan approved Mon 9 Nov. Decisions:
  - Access log rows for byte views, single-record reads, and one row per
    list request.
  - Fixed core severity list with one-sentence definitions.
  - Photo-on-fail enforced at completion.
  - Only verified, same-site evidence can be attached (foreign keys);
    old photos are flagged, not blocked.
  - Natural keys for responses; 403 for "not your inspection".
  - 7.1 (Mon 9 Nov), **done**: migration 000011 `evidence_access_log`.
    The app may only INSERT and SELECT. A trigger blocks UPDATE, DELETE
    and TRUNCATE for every role, superusers included. Foreign keys pin a
    row's evidence to its site and its user to the org. Nothing writes it
    yet; 7.2 does.
  - 7.2a (Mon 9 Nov), **done**: `GET` evidence writes a `view_record`
    row, and the list writes one `list` row per request (filters and
    count). Refused reads write nothing.
    - New env `TRUSTED_PROXIES` (default none), passed to
      `router.SetTrustedProxies`.
    - `NewRouter` now takes `app.HTTPOptions{RequestTimeout,
      TrustedProxies}`.
    - The user agent is cleaned to what Postgres accepts (valid UTF-8, no
      NUL, at most 512 bytes).
    - The IP is normalised: IPv4-mapped addresses are unmapped and IPv6
      zones dropped.
  - 7.2b (Tue 10 Nov), **done**: `POST …/evidence/:evidenceId/view` (see
    Evidence engine). **The access-log task is complete.**
    - `Store.Open` is renamed `ReadVersion`, and `Store.PresignGet` is new.
    - The presign forces `response-content-type`. The upload's
      `Content-Type` wasn't signed, so without the override a file stored
      as `text/html` was served as HTML (the mutation test showed it on
      MinIO).
    - The upload now signs `Content-Type` too: `PresignPut` takes the
      declared type, so S3 refuses any other and new objects carry the
      verified type. The override stays for objects uploaded before this
      change.
    - `TestEvidenceBytesGuard` is a name-based tripwire. It also fails on a
      new `Store` method or an S3 import outside `storage`.
    - `TestAccessLogFailureFailsTheRead`: with the log's INSERT revoked,
      get, list and view all answer 500 with nothing else.
    - Two new DEPLOY tasks in BUILD_PLAN: S3 access logs, and
      `TRUSTED_PROXIES`.
  - 7.3a (Tue 10 Nov), **done**: `internal/platform/apidoctest` extracted
    from evidence's `TestAPIDoc`, with the same rules (pulled forward from
    7.8). Removing a field, changing a union value or editing a rejection
    row in `evidence.md` still fails the test.
  - 7.3b (Tue 10 Nov), **done**: `GET /v1/sites/:id/checklists`,
    `sites.Service.RegimeInTx`, the severity scale (below), and
    `docs/api/inspections.md`. `regimes.Cadences` is exported for the API
    doc. A test fails if a shipped template uses a response type other than
    `pass_fail_na`.
  - 7.3c (Tue 10 Nov), **done**: template version lock in `shipped.json`
    (`checklist_versions`).
    - The hash covers what an inspector reads (key, title, cadence,
      source ref, items) through its own struct, so adding a field to
      `ChecklistTemplate` doesn't change every hash.
    - It fails on a content change without a version bump, a bump without
      a lock update, a version going down, or a template missing from the
      lock. Each message says what to do.
  - 7.3d (Tue 10 Nov), **done**: template versions are append-only (see
    Layout, `internal/regimes`).
    - A wording fix used to replace v1 with v2, so a stored v1 inspection
      could no longer be checked and an offline v1 start couldn't be
      validated.
    - The hash moved to `regimes.ChecklistTemplate.ContentHash`.
    - The catalogue offers only the latest version of each key, with its
      `sha256`.
    - `checklists.resolveTemplate(ds, key, version, sha256)` accepts any
      shipped version, but only with that version's hash: 400 `"unknown
      checklist template version"` or `"checklist template does not
      match"`. Start (7.5) uses it.
    - **Contract change:** `ChecklistTemplate.sha256`.
    - **7.3 is done.**
- **Decisions for inspections (10 Nov 2026):**
  - **Template hash.** Start sends `template_sha256` from the catalogue,
    and the server checks it against its own hash for (key, version). The
    inspection stores key, version and hash, and responses are created
    from that version's items.
  - **Site status.**
    - An **archived** site refuses new inspections with 409 `"site is
      archived"` (checked on `Site.Status`; `GetSite` doesn't filter
      archived sites).
    - An inspection already in progress when its site is archived can
      still be answered and completed.
    - Reads (list, get, findings) stay allowed on archived sites.
    - A **completed** site accepts new inspections (defects period).
  - 7.4 (Tue 10 Nov), **done**: migration 000012. It has four tables:
    `inspections`, `inspection_responses`, `inspection_response_evidence`
    and `inspection_findings`. It also adds `UNIQUE (org_id, site_id, id,
    status)` on `evidence`.
    - **The guards are triggers that apply to every role**, owner and
      superuser included:
      - a new inspection starts in progress;
      - a correction must point at a completed inspection on the same
        site;
      - a completed inspection changes in no way;
      - an inspection in progress changes only by being completed;
      - the copied item text never changes;
      - findings never change;
      - TRUNCATE is refused.
    - **Completion requires** every item answered, exactly `item_count`
      responses, and a photo on every fail. On completion,
      `inspections_raise_findings` inserts one finding per fail, copying
      its severity.
    - **Photos:** only verified evidence from the inspection's site can be
      attached, enforced by two FKs sharing `site_id`. One of them targets
      `evidence (…, status)` through the constant `evidence_status =
      'verified'`.
    - **Findings** have an FK to the response's `(…, result, severity)`,
      so a finding is always a fail with the same severity.
    - **Answers versus completion:** the response and photo guards lock
      the inspection `FOR SHARE`. A completion therefore waits for open
      answers, and a later answer fails.
      `TestAnswerWaitsForCompletion` and `TestCompletionWaitsForAnswer`
      prove it, and both fail without the lock.
      `TestAnswersRacingCompletion` runs 20 answers against one
      completion: no deadlock, and only successful answers stored.
    - **Grants:** the app can update only the completion columns of
      `inspections` and the answer columns of responses. It can delete
      photo links while in progress. No other UPDATE or DELETE.
    - **RLS:** `rls_test.go` has a new `deleteFrozen` rule. Org C in
      `inspections_test.go` holds the committed fixtures the
      two-transaction tests need.
  - 7.5a (Sat 14 Nov), **done**: start and get an inspection.
    - `PUT /v1/sites/:id/inspections/:inspectionId` starts one under an
      id the device chose. 201 when created; 200 for an identical repeat;
      409 for the id with different details.
    - **Order of checks in start:** site scope, then the existing id,
      then the archived site (409), then the template version and hash
      (400), then `corrects_id` (400). The id check comes before the
      archived check so a retry after archiving still answers 200.
    - It stores the server's own hash for the version, after checking the
      client's equals it, and one response per item of that version.
    - **Lost race:** an insert that hits the primary key retries once and
      answers as a repeat (`errIDTaken`, as in evidence).
      `TestInspectionStartLosesTheRace` forces it with an uncommitted
      identical start; firing concurrent requests alone rarely hits the
      window.
    - `GET /v1/sites/:id/inspections/:inspectionId` returns it with its
      items. It works on archived sites.
    - `sites.StatusArchived` is new. `checklists.NewService` now takes the
      pool.
    - `TestAPIDoc` also checks that the reference quotes every error text.
    - **Contract change:** two new endpoints.
  - 7.5b (Sun 15 Nov), **done**: `GET /v1/sites/:id/inspections?status=&limit=`.
    - Newest first; each summary has `answered_count` and `failed_count`,
      counted in one query. Works on archived sites. The query is
      validated before the site lookup, as evidence's list is.
    - `InspectionHeader` holds the fields `Inspection` and
      `InspectionSummary` share. It's embedded untagged in both, and its
      JSON is unchanged (7.5a's tests compare whole bodies).
    - **Doc-checker gap fixed.** `apidoctest.CheckStruct` used to skip
      every field without a `json` tag. So it would have skipped the
      embedded header, and also an exported field with a forgotten tag,
      which `encoding/json` sends under its Go name. It now follows
      `encoding/json`'s rules, and has its first unit tests.
    - **Contract change:** one new endpoint.
  - **Corrections tightened (Sun 15 Nov):** migration 000013.
    - **One correction per inspection:** `UNIQUE (corrects_id)`.
    - **Same checklist:** `template_key` joins the correction's foreign key
      (`inspections_corrects_fkey`), with a new
      `UNIQUE (org_id, site_id, id, template_key)` as its target.
    - The API checks both first: 400 `"inspection already has a
      correction"` and `"a correction must use the same checklist as the
      inspection it corrects"`. A lost race on the unique key gets the same
      400 (`TestInspectionCorrectionLosesTheRace`, forced with an
      uncommitted correction).
    - **Trade-off:** a correction started by mistake holds the place until
      it's completed, because in-progress inspections can't be abandoned
      yet.
    - Mutation checks: dropping either constraint, or either API check,
      fails a test.
    - **Contract change:** two new 400 texts on start.
  - **Evidence declare race test (Sun 15 Nov):**
    `TestEvidenceDeclareLosesTheRace` forces the lost race with an
    uncommitted identical declaration, as 7.5a does for inspections.
    - It's a test for existing code; no change to declare itself.
    - Removing declare's retry now fails it with 409. The existing
      concurrent-declare test still passed without the retry, so until now
      nothing covered it.
  - 7.6 (Sun 15 Nov), **done**: answer an item,
    `PUT …/inspections/:inspectionId/items/:itemKey`.
    - Only the starter may answer (403), while the inspection is in
      progress (409). An archived site doesn't stop it.
    - The request is the whole answer and replaces the photos. The
      inspection row is locked `FOR UPDATE` first, so answers and
      completion take turns.
    - **Newest answer wins** by `client_answered_at`:
      - older → 409 `"a newer answer is already stored"`;
      - same time, different content → 409 `"a different answer is
        already stored for this time"`;
      - both 409 bodies carry `stored`;
      - an exact repeat → 200, with nothing rewritten.
    - **Device clock:** a `client_answered_at` more than 5 minutes ahead of
      the server is 400 `"client_answered_at is in the future; check the
      device clock"`. Otherwise one phone with a fast clock would block
      every later correction of that item, since the newest answer wins.
    - **Photos** must be verified and on the site. The errors carry
      `evidence_ids`: unknown or another site's → 400, rejected → 400,
      pending → 409.
    - Items show `evidence: [{evidence_id, client_captured_at,
      captured_before_start, attached_at}]`, not access-logged (see
      Conventions).
    - Mutation checks: the starter check, the stale check, the photo check
      and the row lock are each caught.
    - **Contract change:** a new endpoint, `InspectionItem.evidence`, and
      the `AnswerRequest`, `AttachedEvidence`, `EvidenceProblem` and
      `StaleAnswer` types.
  - 7.7 (Sun 15 Nov), **done**: complete an inspection, and see findings.
    - `POST …/inspections/:inspectionId/complete`: only the starter. 201
      when completed, 200 if it already was.
    - Under the row lock it checks every item is answered and every fail
      has a photo. Otherwise 409 with `item_keys`, in template order. The
      000012 trigger checks the same and raises the findings.
    - `Inspection.findings` lists them in template order, `[]` until
      completed.
    - `GET /v1/sites/:id/findings?severity=&limit=`: newest first, each with
      its template and prompt. Works on archived sites.
    - Mutation checks: the completeness pre-check (without it the trigger
      gives a 500), the starter check, and the severity filter are each
      caught. A second completion waits on the lock, then answers 200.
    - **Contract change:** two new endpoints, `Inspection.findings`, and the
      `CompleteRequest`, `InspectionFinding`, `IncompleteInspection`,
      `Finding` and `FindingList` types.
  - 7.8 (Sun 15 Nov), **done**: abandon an inspection, migration 000014.
    - **Status `abandoned`:** final like completed, with
      `abandoned_at`/`abandoned_by`/`abandon_reason` (all set together;
      the reason is 1–500 characters). Answers are kept, no findings are
      raised, and it stays in lists and on get.
    - **Corrections:** `inspections_corrects_once` is now a partial unique
      index (`WHERE status <> 'abandoned'`), so abandoning a correction
      frees its place. An abandoned inspection can't be corrected.
    - **Who:** the starter, an owner or a SHE officer. With a fail, only an
      owner or a SHE officer. `inspections_guard` enforces the fail rule
      too, by reading the abandoner's role in `org_members`.
    - `POST …/abandon`: 201 when abandoned, 200 if it already was, 409 if
      completed. Answer and complete answer 409 `"inspection is
      abandoned"`.
    - Mutation checks: the partial index, the frozen guard, the trigger's
      fail rule, and the API's who-may and fail checks are each caught.
      `TestAPIDoc` caught an error text the doc had wrapped across two
      lines.
    - **Contract change:** a new endpoint; `abandoned` in
      `InspectionStatus` and the list filter (its bad-status text changed);
      `abandoned_at`, `abandoned_by` and `abandon_reason` on `Inspection`
      and `InspectionSummary`; the `AbandonRequest` type.
- **Milestone 8, Incidents and the deadline engine** (due Fri 13 Nov): in
  progress. Full rigour. Plan approved Sun 15 Nov.
  - **Decisions (15 Nov):**
    - **Counting:** "within N days" excludes the basis's local date and is
      due by the end of the last day in the regime's `time_zone`. `due` is
      never extended; `legal_latest` applies Interpretation Act s4 (a last
      day on a Sunday or public holiday moves on). Alerts use `due`. Sources
      in `SOURCES_za_construction_2014.md`. Whether s4 also excludes every
      Sunday and holiday inside the period is open (only commentary found,
      reading it our way); `legal_latest` is shown as "at least until".
    - **COIDA s39** attaches to any injured or dead employee of the
      organisation, first aid included, from `employer_aware_at`. IOD reuses
      that deadline.
    - **GAR 8(3) and s24(3)** are not modelled: clocks always start, with a
      caveat on the GAR 8(1) deadlines.
    - **Several types per version**, clocks are the union (Known spec issues).
    - **Who:** owners, SHE officers and supervisors on their sites submit,
      amend, take statements and record satisfactions; others 403.
    - **Weakening a clock** (withdrawing an open report deadline, or moving
      a report deadline later, satisfied or not) needs an owner or SHE
      officer and a reason; the deadline records who, when and why.
    - **A satisfaction belongs to the duty**, not the row: a basis change
      supersedes the deadline and the satisfaction carries over, judged
      against the corrected date. An owner or SHE officer can retract a
      satisfaction with a reason (append-only), which reopens the duty.
    - **No drafts on the server.** The phone keeps the draft; the server
      holds submitted versions only. Amending restates the whole version.
    - **New holidays** arrive by migration with the proclamation and Gazette
      number. A test fails when the calendar's coverage ends less than two
      years ahead.
    - **Archived sites** accept a new incident only if it occurred before
      the site was archived.
    - **A death** must be classified with at least one reportable type.
  - 8.0 (Sun 15 Nov), **done**: sources and regime data.
    - SOURCES quotes the Public Holidays Act (s2(1), Schedule 1), the two
      proclaimed days in range (29 May 2024, 4 Nov 2026) and Interpretation
      Act s4 and s1.
    - `regime.json` has `time_zone` (validated with `time.LoadLocation`;
      `time/tzdata` is embedded so the container image doesn't matter).
    - `DeadlineRule` has `applies_when` (`incident_type`,
      `employee_injured`, `person_died_after_notice`, `site_work`), each
      with one fixed basis; `after_satisfied` (the notices a later death
      follows) and `caveat`. An incident type may only list `incident_type`
      rules, so reg 3 and reg 4 can never attach to an incident.
    - Mutation checks: dropping the basis check or the taxonomy check each
      fails a test.
  - 8.1 (Sun 15 Nov), **done**: migration 000015, the holiday calendar.
    - `public_holidays` holds one row per day off from 2024 to 2030: 84
      statutory, 8 Sunday substitutes, 2 proclaimed. `kind` and
      `source_ref` say why. A substitute must be a Monday (CHECK).
      `holiday_calendar_coverage` says which dates are complete.
    - Both are read-only to the app and on the RLS allowlist (no `org_id`).
    - `deadline_due(basis_at, period, unit, direction, time_zone,
      jurisdiction)` is the engine's only date arithmetic. It returns
      `due_at`, `due_on`, `excluded_days`, `legal_latest_on` and
      `legal_latest_at`. A day outside the coverage raises `22000`, never
      "no holiday", with one message for a day before it (an old incident:
      the API answers 400) and another for a day after it (the calendar
      needs extending: 500). An unknown zone raises `22023`.
    - Tests in `internal/core/deadlines/calendar_test.go`:
      - 15 worked examples covering each unit and direction, the local
        date, Christmas, a Saturday, a Sunday, the proclaimed 4 Nov 2026,
        a Sunday holiday with its Monday, Easter, and both reg 3/4
        parameter sets.
      - `TestHolidayCalendarFollowsTheAct` regenerates Schedule 1, Easter
        and the Sunday rule in Go, and compares them with the table.
      - `TestHolidayCalendarCoverageAhead` fails when coverage ends less
        than two years from now.
    - Mutation checks: each of 10 is caught (first-day exclusion, Sunday,
      Saturday, holiday extension, coverage, working days, local date, a
      missing substitute row, the grant, the Monday CHECK). Up, down and
      up again verified on a throwaway Postgres.
  - 8.2a (Sun 15 Nov), **done**: migration 000016, incidents.
    - `incidents` (header: never changes except `latest_version`),
      `incident_versions`, `incident_people`, `incident_version_evidence`,
      `witness_statements`. All append-only for every role; TRUNCATE
      refused; RLS forced.
    - **Version order:** the version's AFTER INSERT trigger advances
      `incidents.latest_version` with `UPDATE … WHERE latest_version =
      version - 1`. A gap or a stale number fails. Two submissions of
      the same number queue on the primary key and the second gets 23505.
      A v3 while v2 is open fails at once: Postgres skips the row (its
      committed `latest_version` doesn't match) without waiting.
    - **Frozen at commit:** deferred constraint triggers check
      `people_count`, `evidence_count`, `employer_aware_at` for an
      injured employee, and that a new incident has a version. A row
      added to a committed version breaks its count.
    - Also enforced: `taxonomy_codes` 1+ distinct keys; `amend_reason`
      exactly when version > 1; times not ahead of the server by more
      than 5 minutes; `died_at` not before the incident; a death is an
      injury; no incident on an archived site dated after it was
      archived; photos verified and on the site (FKs); one correction per
      statement, on the same incident. `submitted_at`, `recorded_at`,
      `created_at` are the server's.
    - Tests: `internal/platform/database/incidents_test.go`, including
      forced races. **`SET CONSTRAINTS ALL IMMEDIATE` outlives a savepoint
      rollback**: the test helper resets it.
    - Mutation checks: all 21 caught, including each layer of a guard on its own
      (the advance trigger disabled inside a rolled-back test) and each
      table's truncate trigger with its neighbours' off.
  - 8.2b (Sun 15 Nov), **done**: migration 000017, deadlines.
    - `deadline_duties` (one per incident and rule, for life; holds the
      current `satisfaction_id`), `deadlines` (each computation),
      `deadline_satisfactions`, `deadline_satisfaction_retractions`.
    - **Due values come only from `deadline_due`**, set by the insert
      trigger; the client's are overwritten. A row must be computed in
      the incident's latest version, and starts live. One live row per
      duty (partial unique index).
    - **Closing** (superseded or withdrawn) happens once, only by the
      incident's latest version, later than the computing one. The
      trigger copies who, when and why from that version.
      `superseded_by` is a deferred FK to a row of the same duty.
    - **Weakening** (deferred, so the replacement exists): withdrawing an
      unsatisfied report deadline, or replacing a report deadline with a
      later due time, satisfied or not, needs the version's submitter to
      be an owner or SHE officer (`org_members`). The replacement must be
      computed by the closing version.
    - **Satisfactions:** against the live row, locked `FOR SHARE` so a
      concurrent close is seen. Not in the future, not before the basis.
      One unretracted per duty, held twice: the take trigger's guarded
      UPDATE, and the duty guard. Retraction: owner or SHE officer
      (checked after insert, so RLS answers first), the duty's current
      satisfaction only; it reopens the duty, and the satisfaction stays.
    - `added_by_satisfaction_id`: set on a row that recording a satisfaction
      added (GAR 8(2)); the insert trigger requires a satisfaction of the
      same incident recorded in the same transaction.
    - `deadlines_affected_by_day(jurisdiction, day)` lists live day-based
      deadlines whose window includes a day: what a new holiday could
      move (for the M11 worker).
    - Mutation checks: all 27 caught. Forced races: two satisfactions of one
      duty; a satisfaction against a row being withdrawn.
  - 8.3 (Sun 15 Nov), **done**: `internal/core/deadlines`.
    - `Applicable(ds, facts, duties)` reads `applies_when` (union of the
      types' rules; COIDA for an injured or dead employee, from
      `employer_aware_at`; GAR 8(2) for a death later than a met GAR 8(1)
      notice, from the earliest such death; never `site_work`).
    - `Plan(clocks, duties)`: carry (same basis at microseconds),
      supersede (new basis), withdraw, add (a new duty, or one withdrawn
      before, which keeps its satisfaction).
    - `State(...)`: open, due_now, overdue, satisfied, satisfied_late,
      superseded, withdrawn. A forthwith duty is met by any satisfaction.
    - `repo.go` stores plans (`Apply`) and reads rows with the duty's
      current satisfaction.
  - 8.4–8.6 (Sun 15 Nov), **done**: `internal/core/incidents`, endpoints
    in `docs/api/incidents.md`.
    - **Submit** `PUT …/incidents/:id/versions/:n`. Order of checks: site
      scope, then an identical repeat (200, before anything else, so a
      retry always succeeds), then not-found / not-next / regime, then
      archived, types, photos, then the plan. A non-lead's weakening plan
      is refused before writing (403), with `deadline_due` used to compare
      due times. An amendment locks the incident row (`FOR NO KEY UPDATE`,
      the lock a version insert takes) before reading its deadlines, and
      so does a satisfaction, so the two queue. A version 1 that loses the
      race on the incident's primary key retries once (repeat or 409).
    - **Get, register** (`?type=&reportable=&open_deadlines=&limit=`),
      **witness statements**, **site deadlines** (`?state=`), **deadline
      detail**, **satisfy** (any row of the duty; recorded against the
      live one; re-evaluates the latest version and adds new clocks, i.e.
      GAR 8(2) when the call is recorded after the death; an identical
      repeat answers with the duty's live row), **retract**. A satisfaction
      shows `recorded_after_due` when it was in time but recorded after
      the due time (an offline phone); it isn't refused.
    - Deadline endpoints live in the incidents package for now: every
      deadline's facts come from an incident. `internal/core/deadlines`
      stays regime-agnostic.
    - Tests: `internal/app/incidents_test.go`, 14 tests, including your
      change 1 (a call on day 6, `occurred_at` corrected 2 days earlier:
      satisfied late), a downgrade refused for a supervisor and allowed for
      an owner, GAR 8(2) both ways, and a forced lost race (repeat and
      conflict). Mutation checks on the API's own checks are caught.
    - **Contract change:** eight new endpoints and their types (see
      `docs/api/m8-handover.md`).

## Finding severity

A fixed product scale (decided 9 Nov 2026), not regime data, so findings
compare across sites and regimes. The source is `checklists.Severities`.
This table and the one in `docs/api/inspections.md` must match it word for
word (`TestSeverityDefinitions`). Fix wording only: a change of meaning
needs a new level, because past findings keep the old one. A practitioner
has yet to review the wording.

| key | label | definition |
|---|---|---|
| `low` | Low | A minor shortfall unlikely to cause injury (e.g. housekeeping or paperwork); fix at the next routine opportunity. |
| `medium` | Medium | Could cause an injury needing treatment, or breaks a regulation with no immediate danger; fix within days, not weeks. |
| `high` | High | Could cause serious injury or a reportable incident, but not imminently; fix before the affected work continues on the next shift. |
| `critical` | Critical | Imminent danger of death or serious injury; stop the affected work now and keep it stopped until it is made safe. |

## Requested by the frontend

- `/v1/me` to include `role`, `full_name` and `org_name` (the dashboard
  currently guesses the role from 403s).
- An endpoint listing the organisation's active members, so the dashboard can
  add people to a site.
