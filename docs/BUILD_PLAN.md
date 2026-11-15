# SiteProof build plan

Go backend, Construction edition only (Mining is not in this build). Everything due **Sat 5 Dec 2026**. Dates are SAST.

Copied from the tickable plan page on claude.ai (https://claude.ai/artifact/XCsDqW7ag36Fqt3ugKVuUY) on 28 Oct 2026. M5 onwards were reconstructed from Rev 4, not copied from an original roadmap. Tick items here as they're done.

**Checkpoint, Sun 22 Nov:** if the safety core isn't done, cut from the end in this order: JBCC delay claims, then the principal-contractor tier, then IOD form assembly. Keep the IOD lifecycle.

**Decided Sun 15 Nov: no cuts.** The full scope stays, so **Sat 5 Dec is not reachable**. The forecast below puts v1.0.0 on **Fri 11 Dec**, 6 days late. The Sun 22 Nov checkpoint stays as a review point, not a cut.

## Forecast (Sun 15 Nov, revised three times the same day)

**Third revision (after M9).** M9 was also finished on Sun 15 Nov, against a 3-day estimate that had it on Wed 18 Nov. Every date below is recomputed from M10 starting Mon 16 Nov, with the same per-milestone estimates: v1 moves from Mon 14 Dec to Fri 11 Dec, still 6 days after Sat 5 Dec. Three full-rigour milestones in a day each is not a pace to plan on; the estimates stay as they are. Review time is still not modelled, and M7–M9 have not been reviewed yet.

**Second revision (after M8).** M8 was finished on Sun 15 Nov too, against a 6-day estimate that had it on Sat 21 Nov. Every date below is recomputed from M9 starting Mon 16 Nov, with the same per-milestone estimates: v1 moves from Sun 20 Dec to Mon 14 Dec. As before, the estimates aren't shrunk for two fast milestones. M9, M12 and SYNC are full rigour, and review time is still not modelled. REG4 is still unscheduled; it is dated at the Sun 22 Nov checkpoint.

**First revision.** The first forecast this morning had M7 on Wed 18 Nov, and abandoning (added to M7 the same day) would have pushed it to Thu 19. In fact M7's code was finished on Sun 15 Nov, including abandoning and an end-to-end walk on the real API. So every date below is recomputed from a Mon 16 Nov start for M8, with the same per-milestone estimates. That brings v1 forward from Wed 23 Dec to Sun 20 Dec. The estimates themselves are unchanged: one fast day doesn't justify shrinking them.

How the forecast is built:
- **Recent pace:** M6 took 8 days against a planned 5. M7 grew from a 2-day plan to 7 days (Mon 9 to Sun 15 Nov), mostly from scope added along the way (the access log, template versioning, corrections and abandoning).
- **Full-rigour milestones** (anything that seals, freezes or proves; see `construction_db/CLAUDE.md`) are estimated at about 1.5 times their planned days. **Light ones** (ordinary CRUD) are estimated as planned.
- **Calendar:** one milestone after another, working every day, as so far.
- **Not modelled:** review time. Each step waits for review before the next builds on it.

| Milestone | Rigour | Days | Forecast finish | Was due | Why it takes longer than planned |
|---|---|---|---|---|---|
| M7 Inspections & checklists | full | – | **done Sun 15 Nov** | Tue 10 Nov | Grew by the access log, template versioning, corrections and abandoning |
| M8 Incidents | full | – | **done Sun 15 Nov** | Fri 13 Nov | Built in one day against a 6-day estimate; the dates below are recomputed from it |
| M9 Corrective actions | full | – | **done Sun 15 Nov** | Sun 15 Nov | Built in one day against a 3-day estimate; the dates below are recomputed from it |
| M10 Toolbox talks & site diary | light | 2 | Tue 17 Nov | Tue 17 Nov | |
| M11 People & documents | light, plus the worker | 4 | Sat 21 Nov | Fri 20 Nov | Reminders need `cmd/worker` and Redis, which are new infrastructure; now also action escalation |
| M12 Reports & audit pack | full | 3 | Tue 24 Nov | Sun 22 Nov | The audit pack may need to run in the worker |
| SYNC Offline sync API | full | 4 | Sat 28 Nov | Wed 25 Nov | Conflict rules across every record type built by then |
| BILL Billing | light; webhooks full | 3 | Tue 1 Dec | Fri 27 Nov | Needs Stitch sandbox access (external) |
| DEPLOY | – | 2 | Thu 3 Dec | Sat 28 Nov | Needs an AWS account and a practitioner's call on the Object Lock mode (external) |
| IOD Injury on duty | full | 3 | Sun 6 Dec | Tue 1 Dec | |
| JBCC Delay claims | full | 2 | Tue 8 Dec | Wed 2 Dec | |
| PC Principal contractor tier | full | 2 | Thu 10 Dec | Fri 4 Dec | |
| V1 Release | – | 1 | **Fri 11 Dec** | Sat 5 Dec | |

**Risks to the forecast:**
- **External waits:** Stitch sandbox credentials, the AWS account, and a practitioner reviewing the Object Lock mode, the severity wording, and the corrective-action default due days that follow it (low 14, medium 7, high 1, critical 0 days after the day raised; M9, product defaults, not law). Each one can stall a milestone on its own.
- **Mobile sync:** the mobile app can't sync offline until SYNC lands (Sat 28 Nov, was Wed 25 Nov).
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

### M8: Incidents (due Fri 13 Nov; done Sun 15 Nov)

- [x] Incident register, classified from the regime taxonomy (due Wed 11 Nov; done Sun 15 Nov). Several types per incident
- [x] Reporting deadlines computed from regime deadline rules (OHS Act s24 reportable incidents) (due Thu 12 Nov; done Sun 15 Nov). Plus the holiday calendar, working days, COIDA s39 and GAR 8(2), recording and retracting satisfactions
- [x] Witness statements and evidence links; submitted incidents are amended by new version only (due Thu 12 Nov; done Sun 15 Nov)
- [x] Incident tests (due Fri 13 Nov; done Sun 15 Nov). Plus mutation checks, forced races and an end-to-end walk on the real API, Postgres and MinIO

Plan approved Sun 15 Nov (steps and decisions in `construction_db/CLAUDE.md`, Status): 8.0 sources and regime data; 8.1 holiday calendar and due-date function (000015); 8.2a incidents migration (000016); 8.2b deadlines migration (000017); 8.3 deadline engine; 8.4 submit and get; 8.5 register and witness statements; 8.6 deadlines API with satisfactions and retractions; 8.7 `docs/api/incidents.md` and the end-to-end walk.

### M9: Corrective actions (due Sun 15 Nov; done Sun 15 Nov)

- [x] Actions from findings and incidents: owner, due date, status (due Sat 14 Nov; done Sun 15 Nov). Plus waivers of low and medium findings, and finding coverage
- [x] Close-out requires evidence; overdue query (due Sun 15 Nov; done Sun 15 Nov). Plus verification by a lead, rejection, reopening and cancelling, all append-only
- [x] Decide how findings of a corrected inspection are treated: which inspection's findings drive corrective actions (the original's, the correction's, or both), and what happens to actions already raised on a finding the correction supersedes. Show `corrected_by` (the id of the inspection that corrects it, or null) on each entry of the findings list, so the dashboard can tell a superseded finding apart (added Sun 15 Nov; done Sun 15 Nov: D7 in `construction_db/CLAUDE.md`)

Plan approved Sun 15 Nov (steps and decisions D1–D8 in `construction_db/CLAUDE.md`, Status): 9.1 `corrected_by` on findings; 9.2a actions and events (000018); 9.2b close-outs, verification, waivers and coverage (000019); 9.3 raise, get and registers; 9.4 events and close-outs API; 9.5 waivers and finding coverage; 9.6 `docs/api/actions.md`, the M9 handover and the end-to-end walk.

### M10: Toolbox talks & site diary (due Tue 17 Nov; forecast Tue 17 Nov)

- [ ] First commit of the M10 session: M7–M9's committed test fixtures (the forced race tests in `internal/platform/database`, and the storage and evidence packages' fixed keys) use fresh ids per run, so `go test -count=N` works for hunting flakes. Today the second run collides with the first (found in the M9 review, Sun 15 Nov; CI runs once, so it isn't affected)
- [ ] Toolbox talks with worker attendance sign-off (due Mon 16 Nov)
- [ ] Daily site diary: headcount, weather, notes, evidence (due Tue 17 Nov)

### M11: People & documents (due Fri 20 Nov; forecast Sat 21 Nov)

- [ ] Worker register; competencies and appointments with expiry (due Wed 18 Nov)
- [ ] H&S file document register with versions in S3 (due Thu 19 Nov)
- [ ] Deadline and expiry reminders in cmd/worker (Redis-backed) (due Fri 20 Nov). Also corrective actions: escalate overdue ones up the role hierarchy (Rev 3 "visible ageing and escalation of overdue actions", Rev 4 A.4 "escalation up the role hierarchy", both P1), and flag stalled verifications from `awaiting_since` (added Sun 15 Nov, M9)
- [ ] Recompute open deadlines after a holiday-calendar migration (a newly proclaimed day): each recompute is a new deadline row superseding the old, so what the system said before stays on record. M8 ships the SQL that lists the deadlines a given date affects. Alert on job non-completion, not only failure (Rev 4 §2.3) (added Sun 15 Nov)

### M12: Reports & audit pack (due Sun 22 Nov; forecast Tue 24 Nov)

- [ ] Site compliance summary endpoint: open findings, overdue actions, expiring competencies (due Sat 21 Nov)
- [ ] In the compliance summary, list each site's evidence still pending more than 24 hours after it was declared. A declared upload that's never completed stays pending forever, and any object it uploaded stays under Object Lock; this makes abandoned uploads visible rather than silent (due Sat 21 Nov)
- [ ] Audit pack export with evidence manifest and chain verification result. Include each site's chain head hash and length: the chain alone can't detect its newest entries being deleted (with site_chain_heads rewound), but a pack someone already holds records how long it was (see internal/core/evidence/CHAIN.md). Until the daily signed anchor (P2) exists, this is the only external record. Verifying a long chain (it walks every entry) can take longer than REQUEST_TIMEOUT (default 10s), and a pack covers many sites, so the export may need to run in cmd/worker rather than inside a request. Include abandoned inspections, marked abandoned, with their answers and who abandoned them, when and why (decided 15 Nov) (due Sun 22 Nov)
- [ ] Checkpoint: review the safety core against Rev 4 section 6.D; decide cuts here if behind (due Sun 22 Nov)

## Sync, billing & deploy (due Sat 28 Nov)

### SYNC: Offline sync API (due Wed 25 Nov; forecast Sat 28 Nov)

- [x] Request deadlines and pool limits: replace RequireAuth's own 2s timeout with one request-level deadline set in middleware; a request that hits it or can't get a pool connection gets 503 with Retry-After, never 500; pool size (pgxpool MaxConns) set from config and documented. Found in M6: a burst larger than the pool (default max(4, CPUs)) made auth answer 500 after 2s (due Mon 23 Nov)
- [ ] Client-generated UUIDs and idempotency keys on every create (due Mon 23 Nov)
- [ ] Delta pull per site from a cursor (GET /v1/sync?since=) (due Tue 24 Nov)
- [ ] Batch push with per-record results and conflict rules; evidence is never overwritten (due Wed 25 Nov)

### BILL: Billing (due Fri 27 Nov; forecast Tue 1 Dec)

- [ ] PaymentProvider interface, plans, seat limits, subscription states (due Thu 26 Nov)
- [ ] Stitch DebiCheck provider (sandbox) and verified webhooks (due Fri 27 Nov)

### DEPLOY: Deploy (due Sat 28 Nov; forecast Thu 3 Dec)

- [ ] RLS on users: visible if it's you or shares an org with the tenant; login and invite lookups by email/mobile via SECURITY DEFINER functions (before the production deploy)
- [ ] Create a LOGIN role for siteproof_app with its own password; the production API connects with it, never superuser credentials. The migration owner must be a superuser or have BYPASSRLS (the token lookups are SECURITY DEFINER) (before the production deploy)
- [ ] Evidence bucket retention: choose the Object Lock mode (governance or compliance) and default retention with a practitioner. Compliance can be extended per object but never shortened, and blocks POPIA deletion requests. Set it as the bucket default and EVIDENCE_LOCK_MODE to match; the API refuses to start otherwise (before the production deploy)
- [ ] Evidence bucket in af-south-1: Object Lock and versioning on, SSE-KMS with its own evidence key (Rev 4 §2.13), all public access blocked (before the production deploy)
- [ ] API IAM role on the evidence bucket only: s3:PutObject, GetObject, GetObjectVersion, GetObjectRetention, GetBucketObjectLockConfiguration. No Delete* and no BypassGovernanceRetention (before the production deploy)
- [ ] Rate limiting on auth and evidence endpoints (Rev 3 security list, P1) (before the production deploy)
- [ ] Evidence downloads logged by S3: server access logging or CloudTrail data events on the evidence bucket, to a separate bucket. The API's access log records that a 60-second view link was issued, not that it was used; this records the actual GETs (before the production deploy)
- [ ] Set TRUSTED_PROXIES to the load balancer's addresses. Empty (the default) trusts no proxy, so behind a load balancer every access-log row would record the load balancer's IP; listing a range clients can reach lets them choose the logged IP (before the production deploy)
- [ ] Decide who may see injury and treatment details in `incident_people` (POPIA special personal information: health). Today every owner, SHE officer and supervisor on the site can (added Sun 15 Nov) (before launch)
- [ ] Production deploy: GHCR image, secrets, migrate step, health checks, backups (due Sat 28 Nov)
- [ ] Smoke test the deployed API. Use a dedicated smoke-test org and site: evidence it uploads can never be deleted (due Sat 28 Nov)

## Claims & contractor tier (due Sat 5 Dec)

### IOD: Injury on duty (COIDA) (due Tue 1 Dec; forecast Sun 6 Dec)

- [ ] IOD case linked to an incident, with the W.Cl.2–6 lifecycle (per Rev 3) (due Sun 29 Nov)
- [ ] IOD deadlines and reminders from regime rules. The 7-day W.Cl.2 clock already exists: M8 computes `coida_s39_employer_report` on the incident when an employee is injured. The IOD case reuses that deadline (its satisfaction is the W.Cl.2 being filed); it must not start a second one (decided Sun 15 Nov) (due Mon 30 Nov)
- [ ] Assemble form data for each stage (structured data first, PDF later) (due Tue 1 Dec)

### JBCC: Delay claims (due Wed 2 Dec; forecast Tue 8 Dec)

- [ ] Delay event log, notice deadlines from regime data, evidence links (due Wed 2 Dec)

### PC: Principal contractor tier (due Fri 4 Dec; forecast Thu 10 Dec)

- [ ] Link subcontractor orgs to a site; the principal contractor sees their compliance (due Thu 3 Dec)
- [ ] Tests for cross-org visibility under RLS (due Fri 4 Dec)
- [ ] Quote and model GAR 8(3) (incidents to people other than employees: who reports) and OHSA s24(3) (exclusions, e.g. a traffic accident on a public road). Until then every reportable type starts its clocks and the GAR 8(1) deadlines carry a caveat (decided Sun 15 Nov; here because non-employees on a site are mostly subcontractors' workers)

### REG4: Regulation 4 notification (proposed Sun 15 Nov; not yet scheduled, date it at the Sun 22 Nov checkpoint)

Rev 4's construction pack has "Regulation 4 notification, P1, 7-day deadline and tracking", but no task existed. `editions/construction/reg4` is empty.

- [ ] A site's planned construction work (start date, and which of reg 4(1)(a)–(d) or 4(2) applies), creating `cr2014_reg4_notification` (and, for reg 3 permits, `cr2014_reg3_permit_application`) deadlines through the M8 engine (`applies_when: site_work`). Decide whether "at least 7 days before" means clear days. Rev 4 §3.2 overstates reg 4's scope (see SOURCES)

### V1: Release (due Sat 5 Dec; forecast Fri 11 Dec)

- [ ] Full regression, CI green, tag v1.0.0, deploy (due Sat 5 Dec)