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
- Found in M9 (Nov 2026):
  - **Every fail: generates, or can raise?** Rev 3 §2.3.2: "Every fail
    generates a corrective action". Rev 4 §2.4: "Every fail can raise a
    corrective action". Resolved by D1: raising is a person's choice, but
    a finding with no action and no waiver is unactioned and counted.
  - **No severity scale for actions.** Rev 4 §2.5 gives an action a
    "severity" with no scale. Actions use the finding severity scale; a
    finding's action takes the finding's, an incident's raiser picks one.
  - **One action per response.** Rev 3 §2.3.1's schema has
    `checklist_responses.corrective_action_id`. We allow several actions per
    finding, each pointing at its source.
  - **"Before and after photographs"** (Rev 4 §2.5): the before photo is
    the source's own (the fail photo or the incident's), not a separate
    upload; a close-out refuses those.
  - **Shifted rows in Rev 4 A.5**, as in §1.4: the "Corrective action
    register" row reads "Versioned". Read correctly: "Owner, due date,
    before/after photos, verification".
  - **Escalation isn't in M9.** Rev 3's "Open-items ageing: visible ageing
    and escalation of overdue actions" and Rev 4 A.4's "Escalation up the
    role hierarchy" are P1. M9 exposes `state=overdue` and
    `awaiting_since`; escalating is M11 worker work (added to its task).
  - **One table in §5.3.** Rev 4 §5.3 has one `corrective_actions` table.
    Append-only history needs events, close-outs and waivers beside it.
  - **Root cause is P2** (Rev 4 A.6 "Investigation workflow"). Incident
    actions don't model it, and an action doesn't satisfy GAR 9(2)'s
    investigation deadline.
  - **No law found for action deadlines.** Nothing in the regime's quoted
    sources sets them, so the severity defaults are a product choice, like
    the severity wording, and need the same practitioner review. Not
    researched beyond `SOURCES_za_construction_2014.md`.
- Found in M10 (Nov 2026):
  - **The diary is edition code.** Rev 4 §5.2 lists `/diary` under
    `/editions/construction`, so its endpoints are the first under
    `/v1/construction` (D4). Toolbox talks are core: both editions have them,
    with talk topics from each regime.
  - **Lock point.** Rev 3 A.8 locks the diary only "once linked to a
    submitted claim", and the Domain Reference agrees: "Once a diary entry is
    linked to a submitted claim, it should become read-only, with amendments
    recorded as separate, dated addenda". We close it at midnight (D6),
    which is stricter.
  - **Signature versus biometrics.** Rev 3 §2.3.4: "attendance by scan or
    signature"; Rev 4 §2.10: "Rule 1 -- no biometrics. Ever." A drawn
    signature captured with its stroke dynamics is behavioural
    characterisation (POPIA s1, quoted in SOURCES), so a signature is a tap
    (D2).
  - **Content library.** Rev 3: "Ships with a content library -- foremen will
    not write safety content"; Rev 4 6.B: "Construction toolbox talk
    content, P1, Activity and season based". M10 ships topic titles with
    sources only; the library is a proposed task (BUILD_PLAN, TALKS).
  - **Shifted rows in Rev 3 A.8**, as in Rev 4 §1.4 and A.5. Read correctly,
    e.g. "Weather with effect: Activities stopped, hours lost, critical path,
    delayed effect"; "Plant working vs standing: Recoverable standing time".
  - **Fields added by D10**, each optional:
    - Rev 4 §3.9: "Weather recorded by effect (activities stopped, hours
      lost, critical path, delayed effect), labour by trade and
      subcontractor, plant working versus standing, materials expected but
      not received";
    - Rev 3 §2.3.4: "Topic, presenter, duration, attendance by scan or
      signature, photograph";
    - Domain Reference §2.5: "Delayed-effect flag: Captures the 'ground was
      still wet on Tuesday from Sunday's rain' case, which is where most
      claims are lost";
    - Domain Reference §2.6: "Standing time on hired plant is a recoverable
      cost";
    - Domain Reference §2.7: "Deliveries expected but not received (a delay
      ground in its own right)".
  - **Not built in M10** (Rev 3 A.8, Domain Reference §2.7):
    - temperature and wind readings;
    - materials delivered with a delivery-note photo;
    - work completed by area;
    - drawings and revisions received;
    - missing-day detection;
    - diary reminders;
    - signature and countersignature;
    - the rainfall baseline;
    - two-tier escalation (phone UI).
  - **Records before photos.** Rev 4 §2.2 sends records before photos. A
    diary revision with photos waits for them, inside the 48-hour grace; if
    that passes, the photos go on an addendum.
  - **Law:** nothing found in Rev 4 on talks or the diary that contradicts
    it. A site diary is a contractual record (JBCC), not a statutory one.
    Not researched beyond the three documents.
- Found in M11 (Nov 2026):
  - **ID numbers.** Rev 4 §2.7 ("Worker register with encrypted ID
    number") and §5.3 (`workers.id_number_enc`) store an ID number; R3
    §2.9.4 too. We store none (M10 D1, M11 brief): a worker is a name,
    optional firm and clock number, and trade.
  - **Medical certificates (decided 18 Nov).** Rev 4 §3.4: "Status only,
    never contents"; §2.12 lets a SHE officer see health documents only
    "with the claims-handler capability". We store the certificate itself,
    readable only by an owner or SHE officer, every view logged; a
    supervisor can capture it but not read it back. Reason: inspectors and
    principal contractors ask for the certificate. Everything else follows
    R3 §2.9.4: outcome, dates and practice number, "Not findings, diagnoses
    or restrictions". The change is also recorded in SOURCES and the M11
    handover.
  - **To check against the law in 11.1, not assumed:** Rev 4 §3.4's
    "Regulation 7(8)" and "valid for one year" for the medical certificate,
    and "tax" in the vault (the spec never names the document; SARS's
    current instrument is to be confirmed).
  - **Where things live.** Rev 4 §5.2 puts `/workers /attendance /vault
    /notifications` in core and `/safetyfile /appointments` under
    `editions/construction`. Rev 4 §5.3's `attendance_events` (direction
    in/out) becomes sign-ins with an optional sign-out (D12); its
    `worker_site_clearance` (induction, clearance) is not built.
  - **"Clock-in block"** (Rev 4 §2.7, P1, "when a required credential,
    medical or induction has lapsed"): a sign-in is never refused, it
    carries warnings (D18). A gate record must be complete.
  - **Expiry alerts.** Rev 4 §2.8 and A.8 give 60, 30 and 7 days for the
    vault only; nothing in the spec sets lead times for competencies,
    medicals or appointments. Ours are product defaults (D25).
  - **Shifted rows** again: Rev 4 A.3–A.5, 6.B (second half) and 6.C; Rev 3
    A.3, A.5 and A.8; the Domain Reference §3.6 and §4.1. Read in order,
    e.g. `regime_document_types (regime_id, doc_type, default_issuer,
    validity_days)`. Rev 4 §5.3 is aligned correctly.

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
SITEPROOF_TEST_SAST=00:30 go test -run '<tests>' ./internal/app/   # as if at 00:30 SAST (below)
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

- `cmd/api`: the server.
- `cmd/worker` (M11): the reminder jobs.
  - **Scheduling:** every 10 s it tries to claim each job in Redis
    (`internal/platform/cache`, `SET NX` for the job's interval); the
    instance that claims it runs it and records `job_runs`.
  - **Jobs:** `reminders` (15 min), `emails` (1 min), `holidays` (6 h); see
    `notifications.Schedule`.
  - **Config:** `REDIS_URL` (required), `SMTP_URL` (optional: without it,
    emails are logged), `WORKER_DB_MAX_CONNS` (default 5).
  - **Deployment:** the same Docker image with entrypoint `/worker`.
- M11 packages:
  - **`internal/core/files`:** sealed document files. As evidence (declare,
    presigned PUT, complete re-hashes the stored version) with no chain, and
    `purpose` pins what a file attaches to. Only `files/access.go` and
    `files/complete.go` reach bytes besides evidence's (`TestEvidenceBytesGuard`).
    `LogView` logs a view in the caller's transaction and enforces the
    medical rule; `Presign` signs it afterwards.
  - **`internal/core/workers`:** the register, competencies, medicals,
    signature links, the regime types endpoint, and the validators ONBOARD
    reuses (`ValidateWorker`, `ValidateCompetency`, `CheckRows`).
  - **`internal/core/attendance`:** sign-ins, sheets, roster, the day.
  - **`internal/core/documents`:** versioned documents. Mounted by
    `internal/core/vault` (org scope) and
    `internal/editions/construction/safetyfile` (site scope).
  - **`internal/core/notifications`:** inbox, the worker's jobs,
    `/healthz/jobs`.
  - **`internal/editions/construction/appointments`:** site appointments.
  - **Edition routes:** they reach core through `edition.Deps`, which gained
    `Files`, `Documents` and `Regimes`.
  - **`site_headcount(site, day)`** (000026) is the diary's suggestion now;
    `toolbox_headcount` stays for its tests.
  - **`attendance.OpenWindow`** mirrors `site_sign_in_window()`
    (`TestSignInWindowMatchesDatabase`).
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
- `internal/core/actions`: the corrective action register (Rev 4 §2.5):
  raise, events (reassign, move due, verify, reject, reopen, cancel),
  close-outs, waivers, and the registers (`docs/api/actions.md`). Every
  date rule calls the migration's SQL (`action_local_date`,
  `action_default_due_days`); `DefaultDueDays` mirrors it for the
  reference, and `TestActionRulesMatchDatabase` compares them. Finding
  coverage is `finding_coverage` (000019), which the findings list reads.
- `internal/core/toolbox`: toolbox talks, their photos and signatures
  (`docs/api/toolbox.md`). Topics come from the site regime
  (`toolbox_topics.json`, `DataSet.Topic`). `toolbox_headcount(site, day)`
  (000020) is the diary's suggested headcount.
- `internal/editions/construction/diary`: the site diary
  (`docs/api/diary.md`), the edition's first routes, mounted by
  `construction.Edition.RegisterRoutes` under `/v1/construction`. Every
  handler resolves the site through `sites.Service.RegimeInTx` and answers
  404 unless its regime's edition is construction. `GraceAfterClose`
  mirrors `site_diary_grace()` (000021); `TestDiaryGraceMatchesDatabase`
  compares them. The current revision and conflict are only ever
  `site_diary_replay`'s result (000021).
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

**Dates in tests (CI run #84, Fri 20 Nov).** Every day in the product is a
site's (or record's) regime date, Africa/Johannesburg for construction; CI
runs in UTC. Tests take dates and times from `sastClock`
(`internal/app/sastclock_test.go`): `Today()`, `Day(n)`, `Earlier(d)` (at
most an hour back, never before 00:00 SAST: squeezed in the first hour of
the day), `At(day, "23:35")` for past wall times. Day views pass
`?date=` explicitly. Database tests use
`action_local_date(now(), 'Africa/Johannesburg')`, never `current_date`.

To run the app tests as if at a given SAST time, set
`SITEPROOF_TEST_SAST=HH:MM`: TestMain picks the latest past HH:MM in
Johannesburg, starts Postgres with its clock moved by the same offset
(`dbtest.StartAt`: postgres:16-alpine plus libfaketime, built once from
`internal/platform/dbtest/faketime`, needs network the first time), and gives
the router and the tests the same clock (`HTTPOptions.Clock`,
`notifications.Jobs.Clock`). Only tests that take every time from the clock
are meaningful in that mode: the attendance, toolbox not_signed, reminders,
workers, documents, appointments and actions tests.

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
- **Forecast:** the one source is `docs/BUILD_PLAN.md` ("Forecast"): the
  dates, the reasons and the risks.
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
- **Milestone 8, Incidents and the deadline engine** (due Fri 13 Nov):
  **done Sun 15 Nov**, two days late against the due date, six days
  ahead of the forecast. Full rigour. Plan approved Sun 15 Nov.
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
  - 8.7 (Sun 15 Nov), **done**: `docs/api/incidents.md` with `TestAPIDoc`
    (every error text, every state, and the incident-type table against
    the regime data); `docs/api/m8-handover.md` for the frontend.
    - **End-to-end walk** on the real API, Postgres and MinIO (a separate
      compose project, `-p siteproof-m8walk`, removed afterwards): sign
      up and invite a supervisor → photo to MinIO and sealed → near miss
      (retry 200) → injury 9 days ago (COIDA and investigation overdue
      when computed) → reclassified to 14 days off (written notice added)
      → loss of a limb today (due end of Sun 22 Nov, legal latest Mon 23
      Nov) → forthwith call recorded → death after the call (GAR 8(2)
      added) → supervisor's downgrade 403 → owner's downgrade withdraws 4,
      kept with who, when and why → statement with the photo → register
      and overdue list → logged photo view. The script is not in the repo.
    - Making `time_zone` required meant adding it to the in-memory test
      regimes in `internal/app/regime_test.go`.
    - One API check is deliberately redundant: refusing a second
      correction of a witness statement. Without it the unique constraint
      gives the same 409 (its mutation survives; the constraint's is
      caught in 8.2a). Likewise the weakening and archived-site checks
      have database twins that answer the same.
  - Review fixes (Sun 15 Nov), each in the step it belongs to:
    - **Satisfy against Submit race** (a 500): both lock the incident; two
      forced tests (`TestSatisfactionAndAmendmentQueue`,
      `TestAmendmentWaitsForSatisfaction`) stop one request with a held
      lock while the other arrives.
    - **Incident before the calendar** (a 500): 400 with its own text,
      checked against `holiday_calendar_coverage`.
    - `recorded_after_due`; a repeated satisfaction answers with the live
      row; rows added by a satisfaction aren't in a version's `changes`.
    - Docs: retracting a GAR 8(1) satisfaction means the next amendment
      withdraws GAR 8(2) and needs a lead (also tested); the s4 open
      question in SOURCES; the forecast recomputed (v1 Mon 14 Dec).
    - Mutation checks: 5 of 5 caught (each lock, the 400 mapping, the
      distinct SQL message, the before-calendar check).

- **Milestone 9, Corrective actions** (due Sun 15 Nov): **done Sun 15
  Nov**, on its due date. Full rigour. Plan approved Sun 15 Nov.
  - **Decisions (15 Nov):**
    - **D1 Every finding is addressed, nothing automatic.** A person raises
      an action; a finding with neither an action nor a waiver is
      *unactioned* (the findings list shows `coverage` and filters on it).
      A lead may waive a **low or medium** finding with a reason
      (append-only; retracted once, by a lead, with a reason). Critical and
      high can't be waived: a fix on the spot is an action closed out with a
      photo. A cancelled action leaves its finding unactioned unless
      something else covers it. Several actions per finding.
    - **D2 Due dates.** A local date (`due_on`), default by severity: critical
      the day raised, high the next day, medium 7 days, low 14 (calendar days
      from the local date of `client_raised_at`). Later than the default
      needs a lead and a reason; afterwards the raiser, the owner or a lead
      move it earlier, only a lead later (with a reason).
    - **D3 Owner:** an active owner or SHE officer, or a supervisor added to
      the site (database, at raise and every reassignment), plus an optional
      free-text `responsible_party`. Reassigning: the raiser or a lead.
    - **D4 Statuses:** open → awaiting_verification (close-out by the owner
      or a lead) → closed (verified by a lead); reject → open; a lead may
      reopen a closed action or cancel one that isn't closed, with a
      reason. A lead verifying their own close-out is allowed and recorded
      `self_verified`.
    - **D5 Close-out photos:** verified, on the site, at least one; refused
      if taken more than 5 minutes before the source time or if one of the
      source's own photos; taken before the raise is accepted and flagged
      `captured_before_raised`.
    - **D6 Incident actions** link to the version they were raised against,
      which must be the latest then; later versions never touch them.
    - **D7 Corrections:** a finding is superseded once its inspection has a
      completed correction (`corrected_by`). New actions and waivers only on
      findings that aren't superseded. Old actions are kept and cover the
      correction's finding for the same item only if its severity is the
      same or lower; otherwise it is unactioned, a waiver never carries over,
      and the old action shows `review: severity_raised` (or
      `item_not_failed` if the correction passes the item).
    - **D8 Overdue:** the M8 rule without the s4 extension: due by the end of
      `due_on` in the regime's `time_zone`; open and past `due_at` is
      overdue; awaiting verification never is (`awaiting_since` says how
      long it has waited). Closed with the close-out submitted (device time)
      at or after `due_at` is `closed_late`.
    - **Defaults not put to a decision:** an archived site refuses new
      actions and waivers (409); events, close-outs and reads still work.
      Reassigning and moving a due date work while awaiting verification.
      An action's severity never changes.
  - 9.1 (Sun 15 Nov), **done**: `Finding.corrected_by` on `GET …/findings`:
    the completed correction's id, null while there's none or it's in
    progress or abandoned. **Contract change.**
  - 9.2a (Sun 15 Nov), **done**: migration 000018.
    - `corrective_actions` (source, severity, raiser, time zone; never
      change) with a copy of the current state, and `corrective_action_events`
      (append-only; each records who, when, why, and the state after it).
      The header's state can only become its next event's (the
      `incidents.latest_version` pattern: the advance trigger's
      `UPDATE … WHERE event_count = seq - 1`, under the row lock that
      serialises an action's events).
    - Functions: `severity_rank`, `action_default_due_days`,
      `action_local_date`, `action_due_at`, `action_site_member`,
      `action_lead`. Every check of D2, D3, D4 (open/cancel part) and D6 is
      a trigger, for every role; a raise takes `FOR SHARE` on the finding's
      correction and on the incident, so it queues against a completing
      correction or an amendment.
    - Tests: `internal/platform/database/actions_test.go`, with forced
      races (two events; a raise against a correction completing, both
      orders; a raise against an amendment, both orders).
    - Mutation checks: 48 of 50 caught on the first run. The two truncate
      mutations were wrong (an AFTER TRUNCATE trigger still refuses);
      dropping the triggers instead is caught. `action_lead`'s active check
      is redundant for events (the actor must be an active site member
      first) and is caught by the waiver test. A `due_at` computed only by
      the raise event was caught after adding a test of the header alone.
  - 9.2b (Sun 15 Nov), **done**: migration 000019.
    - `corrective_action_closeouts` and their photos (append-only; photo
      count and the closeout_submitted event checked at commit), the
      close-out event kinds, `pending_closeout_id` and `awaiting_since` on
      the header, `finding_waivers` (one live per finding; retraction a
      one-way change), `finding_coverage` (the only implementation of D1/D7
      coverage) and `action_review`.
    - The event trigger now takes `FOR NO KEY UPDATE` (as an amendment
      does), so a close-out row's foreign-key check doesn't wait on it.
    - The RLS fixtures' finding is medium (so it can be waived); its action
      has a close-out, so `corrective_action_events` has two rows per org
      (`rlsRows`).
    - Mutation checks: 61 on 000019 plus three re-runs for 000018 (the
      truncate triggers dropped, `action_lead`'s active check); 63 of 64
      caught. The survivor was the `corrective_actions_awaiting` CHECK,
      which the guard covers; it is now caught by
      `TestAwaitingCheckOnItsOwn` (guard off), so 64 of 64. Run in a scratch
      copy of `construction_db`, so other tests could run meanwhile.
    - Forced races: two close-outs; verify against cancel; two waivers; a
      waiver against a correction completing.
    - Up, down 2 and up again verified on a throwaway Postgres.
  - 9.3–9.5 (Sun 15 Nov), **done**: `internal/core/actions` and the
    findings list's `coverage`, `covered_by`, `waiver_id` and `?coverage=`.
    - Endpoints in `docs/api/actions.md`; every write is a `PUT` under a
      device id (a raise's event id is the action's, a close-out's event id
      the close-out's), and an identical repeat answers 200 before any
      other check.
    - `GET /v1/actions` is the first list across sites, scoped like
      `GET /v1/sites` (supervisors: their sites).
    - Tests: `internal/app/actions_test.go`, with forced races through the
      API (two close-outs; a close-out against a reassignment; a close-out
      while a correction completes, which share no lock; a raise losing the
      race on the primary key).
    - Mutation checks on the API's own checks: 31, plus the raced-raise
      retry and three on `TestAPIDoc`. All caught except two by design: the
      superseded-finding and archived-site checks on raise have database
      twins that answer the same 409 (the raise retries once and keeps the
      database's reason), as M8's weakening and archived checks do. The
      first run showed a test gap (no supervisor who was neither raiser nor
      owner moving a due date earlier), now covered.
    - A raise that loses a race to a correction, an amendment or an
      archiving is retried once, so the answer names it (with
      `latest_version` for an amendment): `TestRaiseRacesAmendment`,
      `TestRaiseRacesCorrection`.
    - **Contract change:** eight new endpoints, a changed findings list
      (see `docs/api/m9-handover.md`).
  - 9.6 (Sun 15 Nov), **done**: `docs/api/actions.md` with `TestAPIDoc`
    (types, every error text, every state and review flag, the default due
    table against `DefaultDueDays`), `docs/api/m9-handover.md`, and the
    end-to-end walk.
    - **End-to-end walk** on the real API, Postgres and MinIO (a separate
      compose project, `-p siteproof-m9walk`, removed afterwards), 31 steps,
      each with the expected status: sign up and invite a supervisor →
      five photos to MinIO and sealed → an inspection failing one item
      (high) with its photo → the finding unactioned → an action raised and
      assigned (repeat 200; a supervisor's later due date 403) → close-outs
      refused with no photo, with the fail's own photo and with a two-day-old
      photo → a close-out with a new photo (awaiting, `awaiting_since`) → the
      supervisor's verification 403, the owner's 201 (closed) → an action
      dated three days ago on the site's and the supervisor's overdue lists →
      a low finding waived, a high one refused → an incident action on v1,
      the incident amended to v2 (the action untouched, showing v1 against
      v2; a new raise on v1 is 409 with `latest_version`) → a correction at
      critical: the original superseded, the correction's finding
      unactioned, the old action flagged `severity_raised`, a raise on the
      superseded finding 409, one on the correction's 201 → no unactioned
      findings left → a logged photo view (`view_bytes` row). The script is
      not in the repo.
    - The walk's fail photo was taken before the item was answered, so step
      10 met the age rule first ("taken before the action's source"); the
      source-photo rule is tested in `internal/app`.
  - Review fixes (Sun 15 Nov), each in the step it belongs to:
    - **Action ids in another case.** Event and close-out read the action id
      from the URL as a UUID (malformed: 404 `"action not found"`) and use
      its canonical form, so a retry with an upper-case id is still a repeat
      (200), not 409. Tested for both; the mutation (raw id back) is caught.
    - **A supervisor taken off a site** can't record events or close-outs on
      its actions (404 `"site not found"`) and no longer sees them in
      `GET /v1/actions`; an action they owned stays theirs until a lead
      reassigns it (not back to them: they're no longer eligible).
      `TestRemovedSupervisor`.
    - **Docs:** `severity_raised` now says plainly that the correction's
      finding needs its own action (the old one doesn't cover it, even if
      re-dated), and that re-dating or cancelling the old one is separate.
    - **BUILD_PLAN:** the default due days join the severity wording in the
      practitioner review.
    - **Known flake, unexplained:** `TestIncidentSubmitAndGet` (M8) failed
      once in a full `go test -p 4 ./...` on Sun 15 Nov; its error text was
      lost (the command filtered it out). It then passed 96 times: 50 alone,
      40 alongside the evidence, storage and database packages, and 6 full
      `-p 4` runs. It sends one request at a time, so a race in the code is
      unlikely; the suspect is a MinIO or Docker Desktop stall under load
      (its photo upload fails on anything but 200, and Docker Desktop
      refused container starts twice that evening). `putUpload` now reports
      MinIO's status, headers, body and the elapsed time, so the next
      occurrence names its cause. Keep whole logs when hunting it.
    - **`go test -count=N` (N > 1)** failed from the second run in the
      database, storage and evidence packages: committed fixtures used fixed
      ids. Fixed in 10.0.

- **Milestone 10, Toolbox talks and site diary** (due Tue 17 Nov): **done
  Mon 16 Nov**, a day early, not yet reviewed. Plan
  approved Mon 16 Nov. Rigour: full for signed attendance, the paper rule,
  the diary's closing, conflicts and addenda; light for the rest.
  - **Decisions (16 Nov):**
    - **D1 Attendee identity:** a name as written at the talk (required),
      optional firm and clock number, no ID number. M11 links attendance to
      a worker through its own append-only table; the signed row never
      changes.
    - **D2 Signature:** a tap on the logged-in presenter's or supervisor's
      phone (`tap`), recording device time, server time, device and account.
      It proves the account recorded the name as present then, not identity.
      `paper`: names from a photographed signed register; a talk with any
      paper attendance needs a `paper_register` photo (database-enforced).
    - **D3 Topics:** `toolbox_topics` in the regime data (8–10, each with a
      CR 2014 `source_ref` quoted in SOURCES, locked in `shipped.json`); a
      talk has an optional `topic_key` and a required, editable title. No
      content library.
    - **D4 Diary home:** `internal/editions/construction/diary`, under
      `/v1/construction/sites/:id/diary`; another regime's site is 404.
      Toolbox talks are core (`internal/core/toolbox`).
    - **D5 Writers and conflicts:** owners, SHE officers and supervisors on
      the site. Each save restates the diary as an append-only revision; the
      newest by device time is current (5-minute future guard). A revision
      not based on the current one opens or extends a conflict (its ids
      shown); only a revision based on the newest of them clears it.
    - **D6 Midnight:** saved on the device before local midnight and
      received within 48 hours: part of the original, flagged
      `received_after_close`. Otherwise 409, add an addendum. A diary with no
      revision yet may get one saved after midnight (a late diary, with a
      reason), sealed on arrival. An on-time revision arriving in the grace
      is still accepted after a late one (a conflict that can no longer be
      cleared, left visible).
    - **D7 Addenda:** by diary writers, on a closed diary only, with a
      reason (1–500 characters), optional sections and photos; append-only.
    - **D8 Delays and instructions:** structured rows with stable
      device-chosen ids; JBCC links to them later by a person's choice.
      Nothing becomes a contract instruction automatically.
    - **D9 Labour:** `{id, firm, own_team, trade?, headcount}` rows, free-text
      firms; a read of the site's firm names for autocomplete.
    - **D10:** critical-path and delayed-effect flags on weather,
      `expected_not_received` deliveries, plant hours working and standing,
      a talk's end time; all optional.
    - **Defaults not put to a decision:** a talk's header (topic, notes,
      start) can change until its first signature, then only `ended_at` may
      be set, once, and photos added. A signature counts from the start to
      the end, or 12 hours after the start while there's none (000023,
      review). The same name, firm and clock number twice on one talk is
      409, with a hint (000023, review). A retraction: once, by the recorder or a lead, with a
      reason. Archived sites take talks and diaries only for days before
      the archive date; signatures, addenda and reads still work. Headcount
      suggestion: distinct signed, unretracted names (with clock number) in
      the day's talks, stored on each revision and shown live.
  - 10.0 (Mon 16 Nov), **done**: fresh fixture ids. Committed fixtures in
    the forced race tests take `fresh()` (a new UUID); org C's raise event
    takes the action's id (`raiseSeq` parsed a UUID's tail as a number).
    The chain vectors (`TestRecomputeSQL`) fix their ids, so they're seeded
    once per process; storage test objects and buckets get a per-run suffix.
    `go test -count=3` passes on the database, storage and evidence
    packages (the second run failed 15, 3 and 1 tests before).
  - 10.1 (Mon 16 Nov), **done**: the plan, D1–D10 and the Known spec
    issues (above); BUILD_PLAN's M10 items restated with the full rules.
  - 10.2 (Mon 16 Nov), **done**: `toolbox_topics.json`, a sixth regime
    file: 9 topics, each quoting its CR 2014 regulation in SOURCES (reg 10,
    13, 16, 23, 24, 25, 27, 28, 29; PPE and induction left out, with
    reasons), plus POPIA s1's "biometrics". Keys locked in `shipped.json`;
    `TestToolboxTopicsQuotedInSources` ties each key to its SOURCES row.
    Mutation checks: 3 of 3 caught.
  - 10.3 (Mon 16 Nov), **done**: migration 000020 (see Layout,
    `internal/core/toolbox`).
    - `toolbox_talks` (editable until the first signature, then only
      `ended_at`, once; `talk_date` from the copied zone),
      `toolbox_talk_evidence` (`photo` or `paper_register`, verified and
      same site by FK), `toolbox_attendance` (a stored `identity_key` makes
      the same name and clock number unique per talk),
      `toolbox_attendance_retractions` (one per signature: the recorder or a
      lead on the site). All append-only and untruncatable for every role.
    - Paper attendance needs a register photo, checked at commit.
    - **Who may write is checked AFTER the insert** (the M9 pattern): RLS's
      `WITH CHECK` runs after BEFORE triggers, and another org's members
      are invisible, so a BEFORE check would answer a cross-tenant row with
      the wrong error.
    - Forced races: two signatures of one name; a signature against a
      header edit, both orders (the signature holds the talk `FOR SHARE`);
      two retractions.
    - Mutation checks: 34 of 34 caught. The first run found one survivor
      (the retractor's site membership: every off-site retractor in the
      tests was also neither recorder nor lead), now covered by a recorder
      taken off the site. One mutation was invalid at first (a foreign key
      with no matching unique key, so the migration failed) and was redone.
  - 10.4 (Mon 16 Nov), **done**: `internal/core/toolbox`, endpoints in
    `docs/api/toolbox.md` (seven). An edit locks the talk row first, so it
    and a signature queue. A signature that loses its insert race (on
    either unique key) retries once and answers as a repeat.
    `TestToolboxSignLosesTheRace` forces it. **Contract change:** seven new
    endpoints.
  - 10.5 (Mon 16 Nov), **done**: migration 000021.
    - `site_diaries` (one per site and local day; `opens_at`, `closes_at`
      from the copied zone), `site_diary_revisions` (append-only, numbered
      by arrival), `site_diary_revision_evidence`.
    - **One definition of the current diary:** `site_diary_replay` replays
      the revisions in arrival order (D5: a revision based on the head and
      newer than it clears the conflict; anything else joins it, and the
      newest by device time is the head). The header guard refuses any
      state that isn't exactly the replay, moved on by one revision.
    - D6 in the revision trigger, under the diary row's lock: future and
      before-the-day times, the 48-hour grace (`site_diary_grace()`), and a
      save after midnight only as a diary's first revision, with a reason.
    - Forced races: two revisions on one base, both orders; two phones
      creating the day; a late diary against an on-time save, both orders.
      A second phone's `INSERT … ON CONFLICT` on the diary waits on the
      first's uncommitted update, before the revision's own lock.
    - Mutation checks: 37 of 37 caught. The first run left the guard's
      head, conflict, late and count comparisons uncaught: every test
      broke the count first. `TestSiteDiaryHeaderGuard` now holds the row
      back one revision and tries each wrong part on its own, and a
      two-revision jump.
  - 10.6 (Mon 16 Nov), **done**: migration 000022, addenda (only once the
    server's clock is past `closes_at`; reason required; must add a note,
    sections or photos; photo count frozen at commit; append-only).
    Mutation checks: 10 of 10 caught. Up, down 2, up verified on a
    throwaway Postgres for 000020–000022.
  - 10.7–10.8 (Mon 16 Nov), **done**: `internal/editions/construction/diary`
    (see Layout), endpoints in `docs/api/diary.md` (six). Section problems
    answer 400 with `section`, `row` and `field`. A revision that loses its
    insert race retries once (`TestDiaryRevisionLosesTheRace`);
    `TestDiaryOnlyOnConstructionSites` builds a registry with a mining test
    regime and checks its site is 404. **Contract change:** six new
    endpoints, the first under `/v1/construction`.
  - **API mutation checks** (10.4, 10.7, 10.8): toolbox 8 of 11, diary
    service 13 of 16, section validation 8 of 8 caught. The six survivors
    are by design: pre-checks with database twins (paper register photo,
    signing window, editing a signed talk, a closed diary, the grace, an
    addendum on an open diary) that answer the same through `mapRule`, as
    M8's and M9's twins do. Nine mutations first failed to compile (a
    condition replaced by `false` left a variable unused) and were redone
    as `false && …`; a run's results are now checked for build failures.
  - 10.9 (Mon 16 Nov), **done**: `docs/api/toolbox.md`, `docs/api/diary.md`
    (both under `TestAPIDoc`), `docs/api/m10-handover.md`, BUILD_PLAN ticked
    and the forecast recomputed (v1 Thu 10 Dec).
    - **End-to-end walk** on the real API, Postgres and MinIO (a separate
      compose project, `-p siteproof-m10walk`, removed afterwards), 45
      steps, each with its expected status: sign up and invite a supervisor
      → a talk on a regime topic with a sealed group photo → three people
      tap to sign, the phone's retry 200, the same person under a new id 409
      → a paper name before the register photo 409, after it 201 → a topic
      change after signatures 409, the end time 200 (4 signed, 2 photos) →
      today's diary with only the minimum, then every optional section and
      a photo → headcount 12 entered beside 4 suggested, the talk linked →
      an edit before midnight 201, an addendum on the open diary 409 →
      yesterday's diary saved 23:50 and received today 201
      (`received_after_close`) → an edit after midnight 409 → an addendum
      201, the original untouched → the supervisor on site B 404 (diary and
      talk) → the registers, revisions, firms → a logged photo view (one
      `view_bytes` row). The script (Node) is not in the repo.
    - `go test -p 4 ./...` passes; `go test -count=3` on the database
      package passes with the M10 tests.
  - **Review fixes (Mon 16 Nov), migration 000023:**
    - **Same-name workers.** `identity_key` is name|firm|clock number, each
      through `toolbox_norm` (whitespace runs to one space, trimmed,
      lowered). `toolbox.IdentityKey` is the same in Go, and
      `TestIdentityKeyMatchesDatabase` compares it with the generated column
      (case, tabs and newlines, a missing firm or clock number, accents). A
      second signature of the same person is 409 with a `hint` to add a firm
      or clock number. `toolbox_norm` lowers by the database's collation;
      the test database is en_US.utf8, and production must be a UTF-8 one
      too, or accented names would compare differently in Go.
    - **Night shifts.** A signature counts from `started_at` to `ended_at`,
      or to `started_at` + 12 hours (`toolbox_signing_window()`, mirrored by
      `toolbox.SigningWindow`) while there's no end, across midnight. An end
      time is at least the start, at most 12 hours after it, and not before
      a signature that stands. Each bound has its own message. The diary
      still counts a talk on `talk_date`. `TestSigningWindowAgrees` runs the
      same cases through the Go check, a direct insert (the trigger) and the
      API. Forced races: two identical signatures through the API (one 201,
      one 409, one row); an end time against a later signature, both
      orders.
    - 000023 supersedes 000020's signing-day rule, its key and its
      `ended_at > started_at` check; those mutation results of 000020 now
      describe the replaced code.
    - Not done: an addendum resolving a conflict (BUILD_PLAN, M10 open
      item); it would need addenda in `site_diary_replay` and the header
      guard.
    - Mutation checks: 000023 12 of 12 caught; Go 9 of 10 (the end-time
      pre-check survives by design: its database twin answers the same).
    - **Near-midnight flake fixed:** "the day hasn't begun" was tested with
      tomorrow, which falls inside the 5-minute clock leeway in the last 5
      minutes of a day (it failed at 23:57). Both tests now use the day
      after tomorrow. Other diary tests compute "today" and "yesterday" at
      call time, so a run that straddles midnight can still flake.

- **Milestone 11, People and documents** (due Fri 20 Nov; forecast Thu 19
  Nov). Plan approved Wed 18 Nov. Rigour: light for the worker register,
  vault, safety file and appointments; full for the file seal, the sign-in
  rules (one open sign-in, distinct counting, double taps, the identity-key
  match), reminders (once only, restart, two workers) and the holiday
  recompute.
  - **Decisions (18 Nov):**
    - **D1 Org-level types.** Competencies, medicals and company documents
      belong to the org, but a regime belongs to a site. Each record stores
      the `regime_code` it was typed against, validated against that data
      set (`GET /v1/regimes/:code/types`). Site-level records use the
      site's regime.
    - **D2 Homes** (Rev 4 §5.2): core `files`, `workers`, `attendance`,
      `vault`, `notifications`; edition `safetyfile`
      (`/v1/construction/sites/:id/safety-file`) and `appointments`
      (`/v1/construction/sites/:id/appointments`; CR 2014 reg 8
      appointments are per site). Versioned documents are core mechanics
      used by vault and safetyfile.
    - **D3 Rigour** as above.
    - **D4 Document versions have their own hash, not the evidence
      chain.** A `files` primitive copies evidence's flow (device id,
      presigned PUT signing hash, length and type, server re-hash of the
      stored S3 version, frozen, Object Lock, logged view). Org documents
      have no site (the chain is per site), documents are PDFs, and order
      comes from append-only version numbers; M12's pack lists the hashes.
      `application/pdf`, `image/jpeg`, `image/png`, at most 20 MiB.
    - **D5 Worker identity:** `identity_key` as M10's (`toolbox_norm` of
      name, firm, clock number), unique per org, active or not. Leads edit;
      supervisors create. No history table, no ID numbers, no photo.
    - **D6 Medicals:** outcome (`fit`, `fit_with_restrictions`, `unfit`),
      `examined_on`, `expires_on`, optional practice number, the document.
      No restriction text, findings or diagnoses. The document: owner and
      SHE officer only, every view logged (Known spec issues).
    - **D7 Signature links:** an append-only link table; automatic at
      signing when the identity key matches a worker, manual by a lead,
      unlinked by a row with a reason. No bulk backfill.
    - **D8 Diary labour rows:** optional `worker_ids` in new revisions
      (each once, at most `headcount`); the headcount is unchanged.
    - **D9 Competencies:** regime type, `issued_on`, `expires_on` as the
      type says, optional number and issuer, a required document. Renewal
      is a new record superseding the old (one successor, same worker and
      type); a mistake is retracted by a lead with a reason.
    - **D10 Validation** for ONBOARD's CSV: pure `ValidateWorker` and
      `ValidateCompetency` returning field problems, and `CheckRows` for
      identity-key clashes in the caller's transaction.
    - **D11 Every sign-in is a register worker.** A tap sends `worker_id`;
      on the spot sends a new worker, created in the same transaction, or,
      if its identity key exists, that worker is signed in with
      `matched_existing: true` and the worker shown. "Different person":
      retract that sign-in and sign in a new worker with a clock number. A
      retracted sign-in counts nowhere.
    - **D12 Sign-out is optional.**
    - **D13 Nothing happens at midnight.** A sign-in counts on the local day
      it began; a sign-out can be recorded up to 16 hours after it. One open
      sign-in per worker per site (no sign-out, not retracted, within 16
      hours), checked under the worker row's lock; a double tap is 409 with
      the open sign-in's id.
    - **D14 Two sites at once: allowed** and flagged. The gate's phone is
      often offline, so refusing on sync would throw away a presence record.
    - **D15 Late arrivals:** the phone's time decides the day and the order
      (5-minute future guard); accepted whenever they arrive, flagged
      `received_after_close` after the diary's 48-hour grace. No sign-in
      inside another recorded interval of the worker on the site.
    - **D16 Expected:** a site roster (a first sign-in adds the worker;
      leads and the site's supervisors add or remove, append-only).
    - **D17 Toolbox `not_signed`:** read only; workers signed in on the site
      that day before the talk ended and not linked to a signature of it.
    - **D18 Never refuse a sign-in;** carry warnings (no valid medical, a
      required competency expired).
    - **Headcount:** `site_headcount(site, day)`, distinct people among the
      day's sign-ins and talk signatures (unretracted); a person is the
      linked worker where there is one, else the identity key.
    - **D19 Documents:** (org [, site], type, title), several per type;
      append-only numbered versions with file, issuer, reference,
      `issued_on`, `expires_on`. The H&S file read shows each required type
      `present`, `missing` or `expired`.
    - **D20 Who:** vault, owner and SHE officer; safety file, leads write and
      supervisors read their sites. BILL's gate goes on the vault route
      group; the PC cross-org read is a later RLS policy. Neither is built.
    - **D21 Appointments** per site: a register worker, a type from the site
      regime, `appointed_on`, optional `expires_on`, a required document,
      supersede and retract as D9. No letters, no gap detection.
    - **D22 Channels:** in-app (the `notifications` row) and email (SMTP
      from `SMTP_URL`, Mailpit locally, log-only without it) behind one
      interface. SMS, WhatsApp and push are BUILD_PLAN tasks.
    - **D23 Recipients** (both channels):
      - incident deadlines: the version's submitter, owners, SHE officers,
        the site's supervisors;
      - overdue actions: the action's owner and raiser, then SHE officers
        at +1 day and owners at +3;
      - stalled verifications (more than 2 days): owners and SHE officers;
      - competency and medical expiries: owners, SHE officers and the
        supervisors of sites where the worker is on the roster;
      - appointment and H&S file expiries: owners, SHE officers and the
        site's supervisors;
      - company documents: owners and SHE officers;
      - a job that didn't complete: logged at error and `/health` reports
        `jobs: stale`, not a tenant notification.
    - **D24 Redis decides when, Postgres decides whether.** Redis holds the
      schedule, a per-job lock with a token and a heartbeat (go-redis).
      Notifications are unique on (recipient, kind, subject, threshold).
      The worker runs per org under RLS (`worker_org_ids()`, SECURITY
      DEFINER). Every run writes `job_runs`; a job with no success for 2×
      its interval is stale. The holiday recompute adds a `recomputed`
      close kind to `deadlines` (no incident version; why = the holiday's
      `source_ref`), once per org per holiday.
    - **D25 Lead times** (`reminders.json`, product defaults):
      - competencies, medicals, appointments and H&S file documents: 30, 14
        and 7 days before and on the day;
      - company documents: 60, 30 and 7 days before and on the day (Rev 4
        §2.8);
      - incident deadlines: 2 days before, on the due day and overdue
        (forthwith at once);
      - actions: escalation at 0, 1 and 3 days overdue; a verification
        stalls after 2 days.

      Per-org settings aren't built.
    - **D26 Once only.** A restart or a second worker inserts nothing new
      (`ON CONFLICT DO NOTHING`). An email outbox row goes pending →
      `sending` (claimed `FOR UPDATE SKIP LOCKED`, committed) → sent or
      failed; one left in `sending` is marked `unknown` and never resent
      (at most once). A superseded or retracted item leaves the due-item
      functions, its pending emails are cancelled, and its notifications
      show `subject_state: "superseded"`.
  - **Steps:**
    - 11.0 plan;
    - 11.1 regime data (`workforce.json`, `documents.json`,
      `reminders.json`) with SOURCES;
    - 11.2 files (000024);
    - 11.3 workers migration (000025);
    - 11.4 workers API;
    - 11.5 sign-in migration (000026);
    - 11.6 sign-in API, with the toolbox and diary changes;
    - 11.7 documents, vault and safety file (000027);
    - 11.8 appointments (000028);
    - 11.9 notifications migration and the holiday recompute (000029);
    - 11.10 `cmd/worker`, Redis, email;
    - 11.11 notifications API;
    - 11.12 handover, BUILD_PLAN and the end-to-end walk.

    If 11.9–11.10 run late, the holiday recompute is the piece to move
    after M12.
  - 11.0 (Wed 18 Nov), **done**: this plan, the Known spec issues, and
    BUILD_PLAN's M11 items restated.
  - 11.1 (Wed 18 Nov), **done**: `workforce.json` (7 competency types, 16
    appointment types, the medical rule), `documents.json` (7 site, 6 org
    types), `reminders.json`; loader and validation; keys locked in
    `shipped.json`; SOURCES quotes the CR 2014 text read from the PDF.
    - **Findings:** Rev 4's "reg 7(8)" is right; "valid for one year" is not
      in the law (Annexure 3 has one date), so it is a product default.
      Annexure 3 has an ID number field we don't store.
    - **Gaps:** the LGS, CIDB and SARS texts were not readable (SOURCES,
      Gaps M11).
    - `TestWorkforceTypesQuotedInSources` ties each key to its row (2 of 2
      mutations caught).
  - 11.2, **done**: migration 000024 and `internal/core/files`.
    - The freeze and append-only log hold for every role.
    - `complete` checks size, hash, then type (PDF `%PDF-`, images decode),
      max 20 MiB.
    - The archtest bytes guard now allows a list of files per method.
    - `evidence.Client` exports `LogIP` and `LogUserAgent`.
  - 11.3–11.4, **done**: migration 000025 and `internal/core/workers`.
    - **Records:** retraction is set-once columns, with a lead check in the
      guard. One live successor is a partial unique index, so a retracted
      renewal frees its place.
    - **`toolbox_attendance_person(attendance)`** is the one definition of
      who a signature is: its live link, else the same-key worker unless
      unlinked from them, else its key.
    - **Toolbox:** `Sign` auto-links in its transaction, and `Attendance`
      gains `worker_id`.
  - 11.5–11.6, **done**: migration 000026 and `internal/core/attendance`.
    - **One rule:** no sign-in at a time inside another of the worker's
      standing intervals on the site (open = 16 h without a sign-out),
      checked under the worker row's lock.
    - **Sign-outs:** after the sign-in, within 16 h, not after the next
      sign-in.
    - **Roster:** a sign-in adds the worker; `absent` ignores entries made
      only by retracted sign-ins (a "different person" mistake).
    - **Toolbox `not_signed`;** diary labour `worker_ids`, checked against
      the register.
    - **Forced races:** a double tap held open (DB and API); two phones
      adding the same person on the spot (the second matches the first).
  - 11.7, **done**: migration 000027, `internal/core/documents`, vault and
    safety file.
    - **Versions** use the `latest_version` advance pattern, and a document
      is saved with version 1 (checked at commit).
    - **Files** must be of the document's scope and site (two FKs, MATCH
      SIMPLE for the site one).
    - **Forced race:** two version-2 saves.
    - **Edition routes** check the site's edition in `construction.onSite`.
  - 11.8, **done**: migration 000028 and the edition's `appointments`,
    reusing 000025's record triggers.
  - 11.9–11.11, **done**: migration 000029, `internal/core/notifications`,
    `internal/platform/cache`, `internal/platform/mail` and `cmd/worker`.
    - **The reminder query:** `notifications_due(p_now, regime, zone, lead
      times…)` is the one definition of what is due, testable at any
      `p_now`. It only sees current subjects, so a renewal stops reminders.
    - **The recompute:** `deadlines_recompute` closes live, unsatisfied
      rows as `recomputed`. The guard allows that only under its setting,
      never for a satisfied duty; the replacement keeps the rule's inputs.
    - **Recompute records:** `holiday_recomputations`, one per org and
      holiday.
    - **System table:** `job_runs` is on the RLS allowlist.
    - **Email claim:** `SKIP LOCKED`, plus `status = 'pending'` re-checked
      outside the subquery.
    - **Versions:** go-redis is pinned to v9.22.0 (v9.23 needs Go 1.26), and
      the testcontainers redis module is v0.44.0.
  - **Mutation checks** (a scratch runner: exact-string mutation, the named
    tests, restore): **36 of 37 caught.**
    - **Caught, by area:** sign-in 8 (the covering check, the worker lock,
      retracted sign-ins standing, the window, sign-out after the next
      sign-in, the paper rule, the API pre-check, the on-the-spot retry);
      distinct counting 4; the identity-key match 4 (firm in the key, the
      auto-link key check, `MatchOrCreate`, toolbox's auto-link join); once
      only 6 of 7; the holiday guard 6; the file seal 7.
    - **First run:** `SKIP LOCKED` survived, a real gap. Without it, a second
      sender waited, then re-claimed a 'sending' row and sent it again. Fixed
      by the outer status re-check plus `TestEmailClaimSkipsLockedRows`
      (forced), and now caught.
    - **Survivor by design:** removing that outer re-check, which only
      matters without `SKIP LOCKED`.
    - Two mutations first failed to compile and were redone.
  - 11.12, **done**: docs and the end-to-end walk.
    - **Docs:** `docs/api/{files,workers,attendance,documents,appointments,notifications}.md`
      (each under `TestAPIDoc`); toolbox.md and diary.md updated;
      `docs/api/m11-handover.md`.
    - **Migrations:** up, down 6 and up again on a throwaway Postgres.
    - **End-to-end walk** on the real API, worker, Postgres, MinIO, Redis and
      Mailpit (compose project `-p siteproof-m11walk`, removed afterwards),
      49 steps:
      - a worker, a licence and a medical (PDFs to MinIO, sealed);
      - a sign-in, a double tap 409 naming the first, someone new on the
        spot with a warning, a sign-out, the day's headcount;
      - the vault's LGS, the H&S file's plan (present), an appointment;
      - Redis claims dropped to run the jobs now: two reminders and two
        emails in Mailpit, a second run adding nothing;
      - a renewal turning the old reminder superseded;
      - `/healthz/jobs` 200, then 503 `stale: ["emails"]` two minutes after
        the worker stopped.
    - **Found by the walk:** the worker ran its jobs in map order, so a new
      reminder's email could wait a minute. Now holidays, reminders, then
      emails, in one tick.
    - The script (Node) is not in the repo.
    - `go test -p 4 ./...` passes.

  - **Review fixes (Thu 19 Nov), migrations 000030-000035**, each its own
    commit after step 11.12:
    - **Email never recorded sent when it wasn't** (000033; full).
      - Without SMTP_URL the worker refuses to start unless EMAIL_MODE=log.
      - Log mode records 'logged', never 'sent' (sending -> logged in the
        guard).
    - **Reminders say who and what** (000034).
      - notifications_due takes p_labels (the regime's labels), and titles
        name the worker, type and site.
      - Emails are one plain sentence (`notifications.Render`): no codes,
        no medical outcome.
    - **The day register for past days** (000032).
      - `worker_history` (append-only, by trigger) and
        `site_absent(site, day, zone, lapse)`: the roster as it stood
        that day, the worker's state then.
      - Sign-in-added entries lapse after reminders.json's
        `roster_lapse_days` (14).
    - **One sign-in at a time, any arrival order** (000031; full).
      `site_sign_in_ends_at` is the earliest of the sign-out, 16 h or the
      next sign-in, and `SignIn.ends_at` shows it.
    - **Documents and their current version can be retracted** (000035).
      - `document_current_version` is the highest standing version.
      - Readiness and reminders ignore retracted documents.
    - **Holiday recompute** (000030).
      - The replacement row's dates were already recomputed by
        `deadlines_insert` (000017); the function now passes r's values,
        and the test compares them with deadline_due.
      - The closed row is unchanged, satisfied duties are untouched, and a
        second run changes nothing.
    - **Smaller:** not_signed skips sign-ins that ended before the talk; an
      on-the-spot worker_id with other details is 409.
    - **Verification:**
      - `go test -p 4 ./...` passes; the database package with `-count=3`.
      - Migrations 000030-000035 go up, down 6 and up again on a throwaway
        Postgres; the schema after down 6 matches a fresh up 29.
      - The round trip found 000032's down dropping `workers_history()`
        before its trigger; fixed.
      - **Mutation runs:** 30 mutations, 27 caught.
        - The first run caught 22 of 29.
        - Four survivors were real gaps: a sign-in on another site ending
          this one; a later sign-in, or a roster entry added later,
          changing a past day; and the medical title.
        - The medical title mutation had hit 000034, but 000035 re-issues
          `notifications_due`. It was redone on 000035, alongside a new
          competency-label mutation.
        - Tests were added for each gap, and all are now caught.
      - **The three survivors can't be caught by any test:**
        - E2: allowing `sending` -> `sending`; the guard returns early on an
          unchanged status.
        - E8: accepting a misspelt mode; unknown modes are already refused.
        - H7: the recompute passing the old dates; deadlines_insert
          recomputes them anyway.

- **CI dates fix (Fri 20 Nov, branch fix/ci-sast-dates).** CI run #84 failed
  at 23:35 UTC (01:35 SAST): three M11 tests mixed UTC and SAST dates.
  - Tests take every date and time from `sastClock` (Commands, above).
  - **Injected clock:** `internal/platform/clock`. Attendance, workers,
    documents, appointments, actions, `/healthz/jobs` and the worker's emails
    read now from it (nil is the real time). A sign-in's `lapsed` is now
    computed in SQL with the database's now().
  - **Product bug fixed:** reminder emails told dates in the first loaded
    regime's zone; now each email's own regime (`notification_regime`,
    000036: the site's regime, else the record's). Days in emails are counted
    by date, not 24-hour spans (a DST change made them one short).
  - **Decided (not_signed, night shifts):** a talk's `not_signed` includes a
    worker whose sign-in on the site was still standing at the talk's start
    (not signed out, not ended by a later sign-in, not retracted), even one
    made the previous date, as well as that day's sign-ins made before the
    talk ended. The day register, absent list and headcount stay by sign-in
    date. Test: `TestNotSignedNightShift` (23:35, then a 00:35 talk).

- **M12 answers (Thu 19 Nov), for the site compliance summary:**
  - **Expiring** means each record's own state (its reminder window).
  - **Counts against a site:**
    - missing or expired required H&S file documents;
    - expired required vault documents (shown on every site of the
      organisation);
    - unactioned findings;
    - overdue actions, with their escalation step;
    - stalled verifications;
    - today's sign-ins with warnings (no valid medical, unfit, an expired
      competency).
  - **Shown, not counted:** absent roster workers.
  - **Not shown:** reminders.

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

A corrective action's default due date (D2) follows the time hints: days
after the local date it is raised, low 14, medium 7, high 1, critical 0
(`action_default_due_days`, 000018; `actions.DefaultDueDays` mirrors it).
A practitioner has yet to review these too.

## Requested by the frontend

- `/v1/me` to include `role`, `full_name` and `org_name` (the dashboard
  currently guesses the role from 403s).
- An endpoint listing the organisation's active members, so the dashboard can
  add people to a site.
