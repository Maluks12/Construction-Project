# SiteProof

South African safety and compliance platform. This repo builds the
Construction edition; the Mining edition is not being built yet.

Two areas, two people:
- `construction_db/`: Go API (backend). Details in `construction_db/CLAUDE.md`.
- `web/`: Next.js 16 dashboard for owners and SHE officers (frontend). Details
  in `web/CLAUDE.md`.

Source of truth for the product: SiteProof spec **Rev 4** (22 Sep 2026), in
`docs/spec/`. Rev 3 (SiteProof_Rev3_Safety_Led_...) has the detailed injury-on-duty (COIDA W.Cl.2–6)
lifecycle. The Implementation & Domain Reference (Rev 1) explains the construction
industry (JBCC, stakeholders, vocabulary); ignore its build order.
Read the relevant spec section before starting a milestone. The milestone plan
is `docs/BUILD_PLAN.md`. Everything is due **Sat 5 Dec 2026**.

## How we work (both areas)

- Claude writes the code; a person reviews every change before accepting it.
- Work one step at a time. Before writing, say in a few lines what you're about
  to change and why. Keep each change small enough to review in one sitting.
- Explain the reasoning behind non-obvious code so the reviewer understands it,
  not just accepts it.
- Run the area's checks after each change (listed in its CLAUDE.md) and report
  the results. A step isn't done until they pass.
- New behaviour gets a test in the same change.
- Verify against the migrations, the code and real library docs, not memory.
- Don't commit or push unless asked. When a step is done, suggest a commit
  message.
- Stay in your area. If a frontend task needs a backend change (or the other
  way round), say so and describe the change instead of making it.

Personal preferences (your name as reviewer, shell, how much explanation you
want) go in your own `CLAUDE.local.md` next to this file. It's git-ignored.

## The API contract between backend and frontend

The dashboard only talks to the API through `web/src/lib/api.ts`, and
`web/src/lib/types.ts` mirrors the `json` tags in
`construction_db/internal/core/*/model.go`. A change to an endpoint's path,
request body, response shape, status codes or error text is a contract change:

- Backend: call it out in the change summary and the commit message, and say
  which `types.ts` types or dashboard pages it affects.
- Frontend: never work around a missing field by guessing; ask for the
  endpoint change.

Shape rules both sides rely on:
- Single objects are returned unwrapped; lists are wrapped
  (`{"sites": [...]}`) and are `[]`, never `null`, when empty.
- Token responses (signup, login, refresh, invite accept) are a top-level
  `TokenPair`.
- Errors are `{"error": "<message>"}`. 400 validation, 401 bad or expired
  token, 403 wrong role, 404 not found (including someone else's or a
  malformed id), 409 conflict, 500 `"internal error"`.
- Phone numbers are E.164 (`+27820000001`).
- Access token 15 min; refresh token 30 days, rotated, with reuse detection (a
  refresh token used twice revokes the whole session).

## Run the whole thing locally (two terminals)

```
cd construction_db
docker compose up -d db minio        # Postgres 16 on localhost:5433, S3 (MinIO) on :9000
docker compose run --rm storage-init # once: create the evidence bucket
docker compose run --rm migrate up   # after pulling new migrations
go run ./cmd/api                     # API on :8080 (or: docker compose up -d --build api)
```
```
cd web
npm install                          # after package.json changes
npm run dev                          # dashboard on :3000
```

Secrets live in git-ignored files: `construction_db/.env` (copy
`construction_db/.env.example`) and `web/.env.local` (copy
`web/.env.example`).

## Architecture rules (Rev 4)

- Three layers: core (edition-agnostic), regimes (data: checklists, deadline
  rules, taxonomies, vocabulary), editions/{construction,mining}.
- Core never imports editions. No `if edition == ...` in core.
- The edition/regime belongs to the **site** (`sites.regime_id`), never the
  organisation. This applies to the dashboard too: vocabulary and checklists
  come from the site's regime.
- Stack: Go (Gin, pgx), PostgreSQL + RLS, Redis, Expo/React Native
  (offline-first SQLite), Next.js, S3 af-south-1 with Object Lock, Stitch
  DebiCheck behind a PaymentProvider interface.
- Cut from scope: Python AI, event bus, gRPC, microservices, OpenTelemetry,
  biometrics.

Roles: `owner`, `she_officer`, `supervisor`, `claims_handler`,
`contracts_manager`, `bookkeeper`. Owners and SHE officers manage sites and
invites; supervisors only see sites they've been added to.
