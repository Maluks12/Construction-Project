# SiteProof: Construction backend

South African safety and compliance platform. This repo is the Go backend for the
Construction edition. The Mining edition is not being built yet.

Source of truth for the product: SiteProof spec **Rev 4** (22 Sep 2026), in `docs/spec/`.
Rev 3 (SiteProof_Rev3_Safety_Led_...) has the detailed injury-on-duty (COIDA W.Cl.2–6)
lifecycle. The Implementation & Domain Reference (Rev 1) explains the construction
industry (JBCC, stakeholders, vocabulary); ignore its build order. Read the relevant spec section before
starting a milestone. The full milestone plan is `docs/BUILD_PLAN.md`. Known spec issues:
- 2.1 says the hash chain is per project; everywhere else says per site. Use **per site**.
- The chain is ordered by server receipt, so concurrent offline uploads need a per-site
  chain-head lock or the chain forks.
- Phase 1 is about 105 features; section 6.D's "if it must shrink" list is the realistic scope.

## How we work

- **Claude writes the code. I (Maluks) review every change** before accepting it.
- Work one step at a time (e.g. 5a, then 5b). Before writing, say in a few lines what you're
  about to change and why. Keep each change small enough to review in one sitting.
- After each change: run `gofmt -l .`, `go vet ./...` and `go test ./...`, and report the
  results. Don't say a step is done until they pass.
- Explain the reasoning behind non-obvious code (why a transaction, why a lock, why this
  status code) so I can review it properly. I'm a software engineering student and want to
  understand what goes in, not just accept it.
- Tests are part of each step, not an afterthought: new behaviour gets a test in the same change.
- Don't commit or push unless I ask. When a step is done, suggest a commit message.
- When a step finishes, tick it in `docs/BUILD_PLAN.md`. When a step changes the plan or a
  decision, update the Status section below.
- Verify against the migrations and real library docs rather than memory.
- When showing a `routes.go`, always show the full file.

## Layout and commands

The Go module is in `construction_db/`, not the repo root. Run everything from there.

```
cd construction_db
docker compose up -d db                 # start Postgres 16
docker compose run --rm migrate up      # apply migrations
docker compose up -d --build api        # build and run the API on :8080
gofmt -l .                              # must print nothing (CI fails otherwise)
go vet ./...
go test ./...                           # needs Docker Desktop running (testcontainers)
```

- `cmd/api`: the server. `cmd/worker`: empty for now.
- `internal/core/{auth,sites,invites}`: features. Each has model.go, repo.go, service.go,
  handler.go, routes.go.
- `internal/platform/{config,database,dbtest,health}`.
- `internal/app/router.go` wires everything; `internal/app/*_test.go` are the HTTP
  integration tests (testcontainers Postgres, migrations applied automatically).
- `migrations/`: golang-migrate files, `00000N_name.up.sql` / `.down.sql`.
- CI (`.github/workflows/ci.yml`): gofmt, vet, `go test -race`, build, Docker image to GHCR.

## Architecture rules (Rev 4)

- Three layers: core (edition-agnostic), regimes (data: checklists, deadline rules,
  taxonomies, vocabulary), editions/{construction,mining}.
- Core must never import editions (CI check coming in M5). No `if edition == ...` in core.
- The edition/regime is attached to the **site** (`sites.regime_id`), never the organisation.
- Stack: Go (Gin, pgx), PostgreSQL + RLS, Redis, S3 af-south-1 with Object Lock,
  Stitch DebiCheck behind a PaymentProvider interface.
- Cut from scope: Python AI, event bus, gRPC, microservices, OpenTelemetry, biometrics.
- Evidence engine: compress to 2048px → SHA-256 → server re-verifies; dual timestamps;
  per-site chain `SHA256(content_hash || server_received_at || site_id || prev_chain_hash)`.

## Conventions (learned the hard way)

- **Transactions:** when several writes must all happen or none, use
  `tx, err := s.pool.Begin(ctx)`, `defer tx.Rollback(ctx)`, pass `tx` to every repo call,
  then `tx.Commit(ctx)`. After `Begin`, `s.pool` must not appear again in that function.
  Single statements and reads use the pool.
- Repo functions take `db DBTX` so they work with the pool or a tx.
- `Scan` takes pointers (`&x.ID`), in exactly the same order as the SELECT list.
  `go vet` does not catch a missing `&`.
- Nullable columns scan into pointers (`*string`, `*time.Time`).
- List functions start from `items := []T{}` so JSON is `[]`, not `null`.
- Wrap errors with `fmt.Errorf("prefix: %w", err)`; one prefix per function.
- Service errors are sentinel `ErrX` values in model.go; each handler package has a
  `writeError` switch mapping them to status codes. Unknown errors log and return 500
  with `"internal error"` (never send err.Error() for a 500).
- `isNotFound(err)` treats `pgx.ErrNoRows` and SQLSTATE `22P02` (malformed UUID) as not
  found. `isUniqueViolation(err, "constraint_name")` checks `23505`.
- "Did it change a row" updates return `(bool, error)` via `tag.RowsAffected() == 1`.
- Org, user and role always come from the token (`auth.OrgID(c)` etc.), never the body.
- Phone numbers are E.164 (`+27820000001`). Emails compare with `lower()`.
- Single objects are returned unwrapped; lists are wrapped (`{"invites": [...]}`).
  Token responses are always a top-level `TokenPair`.
- `.gitattributes` forces LF for Go files; gofmt in CI depends on it.

## Status

- Full plan: `docs/BUILD_PLAN.md`. Tick tasks there as they're done.
- Everything due **Sat 5 Dec 2026**. Foundation (M4, M5) due Sun 1 Nov. Safety core
  (M6–M12) due Sun 22 Nov, which is the checkpoint: if behind, cut JBCC delay claims,
  then principal-contractor tier, then IOD form assembly.
- **Done:** migrations 000001–000007; auth (email and mobile login, refresh rotation with
  reuse detection, sessions); sites, site members, supervisor scoping, locations;
  invites (send, list, revoke, preview, accept); integration tests use real invites.
- **Now: Milestone 5, Regimes and the edition boundary** (due Sun 1 Nov)
  - 5a (Wed 28 Oct): `internal/regimes` package. JSON data embedded with `go:embed` under
    `internal/regimes/data/<regime code>/` (regime.json, checklists.json, deadlines.json,
    taxonomies.json). `Load()`, `LoadFS(fs.FS)` for tests, `validate()` with
    `errors.Join`, `DisallowUnknownFields`, plus `CheckDatabase` at startup (every
    `regulatory_regimes` row needs a data set with the same version).
    Rule: a shipped key never changes meaning.
  - 5b (Thu 29): Construction data set (Construction Regulations 2014 checklists,
    incident taxonomy, deadline rules). Verify legal content against sources.
  - 5c (Thu 29): resolve the regime through the site, in one place.
  - 5d (Fri 30): `editions/construction` skeleton; core reaches it only via interfaces.
  - 5e (Fri 30): CI import-boundary check (fail if core imports editions).
  - 5f (Sat 31): RLS on tenant tables, `set_config(..., true)` inside each transaction.
  - 5g (Sun 1 Nov): tests that RLS blocks cross-org reads even from a buggy query.