# SiteProof build plan

Go backend, Construction edition only (Mining is not in this build). Everything due **Sat 5 Dec 2026**. Dates are SAST.

Copied from the tickable plan page on claude.ai (https://claude.ai/artifact/XCsDqW7ag36Fqt3ugKVuUY) on 28 Oct 2026. M5 onwards were reconstructed from Rev 4, not copied from an original roadmap. Tick items here as they're done.

**Checkpoint, Sun 22 Nov:** if the safety core isn't done, cut from the end in this order: JBCC delay claims, then the principal-contractor tier, then IOD form assembly. Keep the IOD lifecycle.

**Decided Sun 15 Nov: no cuts.** The full scope stays, so **Sat 5 Dec is not reachable**. The forecast below puts v1.0.0 on **Sun 20 Dec**, 15 days late. The Sun 22 Nov checkpoint stays as a review point, not a cut.

## Forecast (Sun 15 Nov, revised the same day)

**Revision.** The first forecast this morning had M7 on Wed 18 Nov, and abandoning (added to M7 the same day) would have pushed it to Thu 19. In fact M7's code was finished on Sun 15 Nov, including abandoning and an end-to-end walk on the real API. So every date below is recomputed from a Mon 16 Nov start for M8, with the same per-milestone estimates. That brings v1 forward from Wed 23 Dec to Sun 20 Dec. The estimates themselves are unchanged: one fast day doesn't justify shrinking them.

How the forecast is built:
- **Recent pace:** M6 took 8 days against a planned 5. M7 grew from a 2-day plan to 7 days (Mon 9 to Sun 15 Nov), mostly from scope added along the way (the access log, template versioning, corrections and abandoning).
- **Full-rigour milestones** (anything that seals, freezes or proves; see `construction_db/CLAUDE.md`) are estimated at about 1.5 times their planned days. **Light ones** (ordinary CRUD) are estimated as planned.
- **Calendar:** one milestone after another, working every day, as so far.
- **Not modelled:** review time. Each step waits for review before the next builds on it.

| Milestone | Rigour | Days | Forecast finish | Was due | Why it takes longer than planned |
|---|---|---|---|---|---|
| M7 Inspections & checklists | full | – | **done Sun 15 Nov** | Tue 10 Nov | Grew by the access log, template versioning, corrections and abandoning |
| M8 Incidents | full | 6 | Sat 21 Nov | Fri 13 Nov | Includes the deadline engine: holiday calendar table, working days, the Sunday-to-Monday rule (Rev 4 §2.3) |
| M9 Corrective actions | full | 3 | Tue 24 Nov | Sun 15 Nov | |
| M10 Toolbox talks & site diary | light | 2 | Thu 26 Nov | Tue 17 Nov | |
| M11 People & documents | light, plus the worker | 4 | Mon 30 Nov | Fri 20 Nov | Reminders need `cmd/worker` and Redis, which are new infrastructure |
| M12 Reports & audit pack | full | 3 | Thu 3 Dec | Sun 22 Nov | The audit pack may need to run in the worker |
| SYNC Offline sync API | full | 4 | Mon 7 Dec | Wed 25 Nov | Conflict rules across every record type built by then |
| BILL Billing | light; webhooks full | 3 | Thu 10 Dec | Fri 27 Nov | Needs Stitch sandbox access (external) |
| DEPLOY | – | 2 | Sat 12 Dec | Sat 28 Nov | Needs an AWS account and a practitioner's call on the Object Lock mode (external) |
| IOD Injury on duty | full | 3 | Tue 15 Dec | Tue 1 Dec | |
| JBCC Delay claims | full | 2 | Thu 17 Dec | Wed 2 Dec | |
| PC Principal contractor tier | full | 2 | Sat 19 Dec | Fri 4 Dec | |
| V1 Release | – | 1 | **Sun 20 Dec** | Sat 5 Dec | |

**Risks to the forecast:**
- **External waits:** Stitch sandbox credentials, the AWS account, and a practitioner reviewing the Object Lock mode and the severity wording. Each one can stall a milestone on its own.
- **Mobile sync:** the mobile app can't sync offline until SYNC lands (Mon 7 Dec, was Wed 25 Nov).
- **No contingency:** the forecast has none. Anything that grows the way M7 did moves every date after it.

**Done before this plan:** migrations 000001–000006, auth (signup, login, refresh with reuse detection, sessions), sites (CRUD, members, supervisor scoping, locations), integration tests, CI.

## Foundation (due Sun 1 Nov)

### M4: Orgs, members & invites (due Tue 27 Oct)

- [x] Migration 000007, invites model, repo, service, handler, routes; accept; login by mobile; integration tests; addMember replaced

### M5: Regimes & the edition boundary (due Sun 1 Nov)

- [x] Load regime data: checklists, deadline rules, taxonomies, vocabulary (internal/regimes, embedded JSON, versioned) (due Wed 28 Oct)
- [x] Construction regime data set: Construction Regulations 2014 checklists, incident taxonomy, deadline rules (due Thu 29 Oct)
- [x] Resolve the regime through the site in one place (sites.regime_id, never the org) (due Thu 29 Oct)
- [x] editions/construction skeleton; core reaches it only through interfaces (due Fri 30 Oct)
- [x] CI import-boundary check: fail if core imports editions (due Fri 30 Oct)
- [x] RLS on tenant tables; set org/site settings per transaction (set_config(..., true) inside the tx, safe with pgx pool reuse) (due Sat 31 Oct)
- [x] Test that RLS blocks cross-org reads even from a buggy query (due Sun 1 Nov)

## Safety core (due Sun 22 Nov (checkpoint))

### M6: Evidence engine (due Fri 6 Nov; done Mon 9 Nov)

- [x] S3 storage behind an interface; presigned upload URLs (af-south-1, Object Lock bucket) (due Mon 2 Nov)
- [x] Migration: evidence + site_chain_heads (content_hash, client_captured_at, server_received_at, prev_chain_hash, chain_hash) (due Mon 2 Nov)
- [x] Server re-hashes the stored bytes and rejects mismatches (client compresses to 2048px before hashing) (due Tue 3 Nov)
- [x] Per-site chain append under a lock (pg_advisory_xact_lock or FOR UPDATE; UNIQUE(site_id, prev_chain_hash)) (due Wed 4 Nov)
- [x] Pin the chain input encoding and document it (bytes vs hex, UTC timestamp precision) (due Wed 4 Nov)
- [x] Chain verification endpoint: walk a site's chain, report the first break (due Thu 5 Nov)
- [x] Concurrency test: parallel uploads to one site give one unforked chain (due Fri 6 Nov)

### M7: Inspections & checklists (due Tue 10 Nov; done Sun 15 Nov)

- [x] Evidence access log: append-only (who, when, which evidence, from where; every view, export and share, Rev 4 §2.1 and A.2, P1). Then a short-lived presigned GET for evidence bytes that writes a log entry. No endpoint may serve evidence bytes before this (due Mon 9 Nov; done Tue 10 Nov)
- [x] Migration: inspections, responses, findings; templates come from the regime (due Sat 7 Nov; done Tue 10 Nov)
- [x] Start and complete inspections, answer items, attach evidence (due Mon 9 Nov; done Sun 15 Nov). Also abandoning (added Sun 15 Nov)
- [x] A failed item raises a finding with severity (due Mon 9 Nov; done Sun 15 Nov)
- [x] Tests, including supervisor scoping (due Tue 10 Nov; done Sun 15 Nov). Plus an end-to-end walk on the real API, Postgres and MinIO

### M8: Incidents (due Fri 13 Nov; forecast Sat 21 Nov)

- [ ] Incident register, classified from the regime taxonomy (due Wed 11 Nov)
- [ ] Reporting deadlines computed from regime deadline rules (OHS Act s24 reportable incidents) (due Thu 12 Nov)
- [ ] Witness statements and evidence links; submitted incidents are amended by new version only (due Thu 12 Nov)
- [ ] Incident tests (due Fri 13 Nov)

### M9: Corrective actions (due Sun 15 Nov; forecast Tue 24 Nov)

- [ ] Actions from findings and incidents: owner, due date, status (due Sat 14 Nov)
- [ ] Close-out requires evidence; overdue query (due Sun 15 Nov)
- [ ] Decide how findings of a corrected inspection are treated: which inspection's findings drive corrective actions (the original's, the correction's, or both), and what happens to actions already raised on a finding the correction supersedes. Show `corrected_by` (the id of the inspection that corrects it, or null) on each entry of the findings list, so the dashboard can tell a superseded finding apart (added Sun 15 Nov)

### M10: Toolbox talks & site diary (due Tue 17 Nov; forecast Thu 26 Nov)

- [ ] Toolbox talks with worker attendance sign-off (due Mon 16 Nov)
- [ ] Daily site diary: headcount, weather, notes, evidence (due Tue 17 Nov)

### M11: People & documents (due Fri 20 Nov; forecast Mon 30 Nov)

- [ ] Worker register; competencies and appointments with expiry (due Wed 18 Nov)
- [ ] H&S file document register with versions in S3 (due Thu 19 Nov)
- [ ] Deadline and expiry reminders in cmd/worker (Redis-backed) (due Fri 20 Nov)

### M12: Reports & audit pack (due Sun 22 Nov; forecast Thu 3 Dec)

- [ ] Site compliance summary endpoint: open findings, overdue actions, expiring competencies (due Sat 21 Nov)
- [ ] In the compliance summary, list each site's evidence still pending more than 24 hours after it was declared. A declared upload that's never completed stays pending forever, and any object it uploaded stays under Object Lock; this makes abandoned uploads visible rather than silent (due Sat 21 Nov)
- [ ] Audit pack export with evidence manifest and chain verification result. Include each site's chain head hash and length: the chain alone can't detect its newest entries being deleted (with site_chain_heads rewound), but a pack someone already holds records how long it was (see internal/core/evidence/CHAIN.md). Until the daily signed anchor (P2) exists, this is the only external record. Verifying a long chain (it walks every entry) can take longer than REQUEST_TIMEOUT (default 10s), and a pack covers many sites, so the export may need to run in cmd/worker rather than inside a request. Include abandoned inspections, marked abandoned, with their answers and who abandoned them, when and why (decided 15 Nov) (due Sun 22 Nov)
- [ ] Checkpoint: review the safety core against Rev 4 section 6.D; decide cuts here if behind (due Sun 22 Nov)

## Sync, billing & deploy (due Sat 28 Nov)

### SYNC: Offline sync API (due Wed 25 Nov; forecast Mon 7 Dec)

- [x] Request deadlines and pool limits: replace RequireAuth's own 2s timeout with one request-level deadline set in middleware; a request that hits it or can't get a pool connection gets 503 with Retry-After, never 500; pool size (pgxpool MaxConns) set from config and documented. Found in M6: a burst larger than the pool (default max(4, CPUs)) made auth answer 500 after 2s (due Mon 23 Nov)
- [ ] Client-generated UUIDs and idempotency keys on every create (due Mon 23 Nov)
- [ ] Delta pull per site from a cursor (GET /v1/sync?since=) (due Tue 24 Nov)
- [ ] Batch push with per-record results and conflict rules; evidence is never overwritten (due Wed 25 Nov)

### BILL: Billing (due Fri 27 Nov; forecast Thu 10 Dec)

- [ ] PaymentProvider interface, plans, seat limits, subscription states (due Thu 26 Nov)
- [ ] Stitch DebiCheck provider (sandbox) and verified webhooks (due Fri 27 Nov)

### DEPLOY: Deploy (due Sat 28 Nov; forecast Sat 12 Dec)

- [ ] RLS on users: visible if it's you or shares an org with the tenant; login and invite lookups by email/mobile via SECURITY DEFINER functions (before the production deploy)
- [ ] Create a LOGIN role for siteproof_app with its own password; the production API connects with it, never superuser credentials. The migration owner must be a superuser or have BYPASSRLS (the token lookups are SECURITY DEFINER) (before the production deploy)
- [ ] Evidence bucket retention: choose the Object Lock mode (governance or compliance) and default retention with a practitioner. Compliance can be extended per object but never shortened, and blocks POPIA deletion requests. Set it as the bucket default and EVIDENCE_LOCK_MODE to match; the API refuses to start otherwise (before the production deploy)
- [ ] Evidence bucket in af-south-1: Object Lock and versioning on, SSE-KMS with its own evidence key (Rev 4 §2.13), all public access blocked (before the production deploy)
- [ ] API IAM role on the evidence bucket only: s3:PutObject, GetObject, GetObjectVersion, GetObjectRetention, GetBucketObjectLockConfiguration. No Delete* and no BypassGovernanceRetention (before the production deploy)
- [ ] Rate limiting on auth and evidence endpoints (Rev 3 security list, P1) (before the production deploy)
- [ ] Evidence downloads logged by S3: server access logging or CloudTrail data events on the evidence bucket, to a separate bucket. The API's access log records that a 60-second view link was issued, not that it was used; this records the actual GETs (before the production deploy)
- [ ] Set TRUSTED_PROXIES to the load balancer's addresses. Empty (the default) trusts no proxy, so behind a load balancer every access-log row would record the load balancer's IP; listing a range clients can reach lets them choose the logged IP (before the production deploy)
- [ ] Production deploy: GHCR image, secrets, migrate step, health checks, backups (due Sat 28 Nov)
- [ ] Smoke test the deployed API. Use a dedicated smoke-test org and site: evidence it uploads can never be deleted (due Sat 28 Nov)

## Claims & contractor tier (due Sat 5 Dec)

### IOD: Injury on duty (COIDA) (due Tue 1 Dec; forecast Tue 15 Dec)

- [ ] IOD case linked to an incident, with the W.Cl.2–6 lifecycle (per Rev 3) (due Sun 29 Nov)
- [ ] IOD deadlines and reminders from regime rules (due Mon 30 Nov)
- [ ] Assemble form data for each stage (structured data first, PDF later) (due Tue 1 Dec)

### JBCC: Delay claims (due Wed 2 Dec; forecast Thu 17 Dec)

- [ ] Delay event log, notice deadlines from regime data, evidence links (due Wed 2 Dec)

### PC: Principal contractor tier (due Fri 4 Dec; forecast Sat 19 Dec)

- [ ] Link subcontractor orgs to a site; the principal contractor sees their compliance (due Thu 3 Dec)
- [ ] Tests for cross-org visibility under RLS (due Fri 4 Dec)

### V1: Release (due Sat 5 Dec; forecast Sun 20 Dec)

- [ ] Full regression, CI green, tag v1.0.0, deploy (due Sat 5 Dec)