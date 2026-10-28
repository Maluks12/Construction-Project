# Backend: Go API

Read `../CLAUDE.md` first (shared rules and the API contract with `web/`).
Run everything from `construction_db/`; the Go module lives here, not at the
repo root.

Known spec issues:
- Rev 4 §2.1 says the hash chain is per project; everywhere else says per
  site. Use **per site**.
- The chain is ordered by server receipt, so concurrent offline uploads need a
  per-site chain-head lock or the chain forks.
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
docker compose up -d db                 # start Postgres 16
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
- `internal/platform/{config,database,dbtest,health}`.
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

## Evidence engine (to build)

Compress to 2048px → SHA-256 → server re-verifies on the stored bytes; dual
timestamps; per-site chain
`SHA256(content_hash || server_received_at || site_id || prev_chain_hash)`.

## Conventions (learned the hard way)

- **Transactions:** when several writes must all happen or none, use
  `tx, err := s.pool.Begin(ctx)`, `defer tx.Rollback(ctx)`, pass `tx` to every
  repo call, then `tx.Commit(ctx)`. After `Begin`, `s.pool` must not appear
  again in that function. Single statements and reads use the pool.
- Repo functions take `db DBTX` so they work with the pool or a tx.
- `Scan` takes pointers (`&x.ID`), in exactly the same order as the SELECT
  list. `go vet` does not catch a missing `&`.
- Nullable columns scan into pointers (`*string`, `*time.Time`).
- List functions start from `items := []T{}` so JSON is `[]`, not `null`.
- Wrap errors with `fmt.Errorf("prefix: %w", err)`; one prefix per function.
- Service errors are sentinel `ErrX` values in model.go; each handler package
  has a `writeError` switch mapping them to status codes. Unknown errors log
  and return 500 with `"internal error"` (never send err.Error() for a 500).
- `isNotFound(err)` treats `pgx.ErrNoRows` and SQLSTATE `22P02` (malformed
  UUID) as not found. `isUniqueViolation(err, "constraint_name")` checks
  `23505`.
- "Did it change a row" updates return `(bool, error)` via
  `tag.RowsAffected() == 1`.
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
- **Now: Milestone 5, Regimes and the edition boundary** (due Sun 1 Nov)
  - 5a (Wed 28 Oct), **done**: `internal/regimes` package (see Layout).
    Added `vocabulary.json` as a fifth file, and the keys-only lock in
    `testdata/shipped.json`. Rule: a shipped key never changes meaning.
  - 5b (Thu 29), **done**: `za_construction_2014` has 7 deadline rules, 14
    incident types, 5 checklists and 10 vocabulary terms, each quoted in
    `internal/regimes/SOURCES_za_construction_2014.md`. No scaffolding
    checklist yet (see Gaps there).
  - 5c (Thu 29): resolve the regime through the site, in one place.
  - 5d (Fri 30): `editions/construction` skeleton; core reaches it only via
    interfaces.
  - 5e (Fri 30): CI import-boundary check (fail if core imports editions).
  - 5f (Sat 31): RLS on tenant tables, `set_config(..., true)` inside each
    transaction.
  - 5g (Sun 1 Nov): tests that RLS blocks cross-org reads even from a buggy
    query.

## Requested by the frontend

- `/v1/me` to include `role`, `full_name` and `org_name` (the dashboard
  currently guesses the role from 403s).
- An endpoint listing the organisation's active members, so the dashboard can
  add people to a site.
