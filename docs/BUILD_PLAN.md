# SiteProof build plan

Go backend, Construction edition only (Mining is not in this build). Everything due **Sat 5 Dec 2026**. Dates are SAST.

Copied from the tickable plan page on claude.ai (https://claude.ai/artifact/XCsDqW7ag36Fqt3ugKVuUY) on 28 Oct 2026. M5 onwards were reconstructed from Rev 4, not copied from an original roadmap. Tick items here as they're done.

**Checkpoint, Sun 22 Nov:** if the safety core isn't done, cut from the end in this order: JBCC delay claims, then the principal-contractor tier, then IOD form assembly. Keep the IOD lifecycle.

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
- [ ] RLS on tenant tables; set org/site settings per transaction (set_config(..., true) inside the tx, safe with pgx pool reuse) (due Sat 31 Oct)
- [ ] Test that RLS blocks cross-org reads even from a buggy query (due Sun 1 Nov)

## Safety core (due Sun 22 Nov (checkpoint))

### M6: Evidence engine (due Fri 6 Nov)

- [ ] S3 storage behind an interface; presigned upload URLs (af-south-1, Object Lock bucket) (due Mon 2 Nov)
- [ ] Migration: evidence + site_chain_heads (content_hash, client_captured_at, server_received_at, prev_chain_hash, chain_hash) (due Mon 2 Nov)
- [ ] Server re-hashes the stored bytes and rejects mismatches (client compresses to 2048px before hashing) (due Tue 3 Nov)
- [ ] Per-site chain append under a lock (pg_advisory_xact_lock or FOR UPDATE; UNIQUE(site_id, prev_chain_hash)) (due Wed 4 Nov)
- [ ] Pin the chain input encoding and document it (bytes vs hex, UTC timestamp precision) (due Wed 4 Nov)
- [ ] Chain verification endpoint: walk a site's chain, report the first break (due Thu 5 Nov)
- [ ] Concurrency test: parallel uploads to one site give one unforked chain (due Fri 6 Nov)

### M7: Inspections & checklists (due Tue 10 Nov)

- [ ] Migration: inspections, responses, findings; templates come from the regime (due Sat 7 Nov)
- [ ] Start and complete inspections, answer items, attach evidence (due Mon 9 Nov)
- [ ] A failed item raises a finding with severity (due Mon 9 Nov)
- [ ] Tests, including supervisor scoping (due Tue 10 Nov)

### M8: Incidents (due Fri 13 Nov)

- [ ] Incident register, classified from the regime taxonomy (due Wed 11 Nov)
- [ ] Reporting deadlines computed from regime deadline rules (OHS Act s24 reportable incidents) (due Thu 12 Nov)
- [ ] Witness statements and evidence links; submitted incidents are amended by new version only (due Thu 12 Nov)
- [ ] Incident tests (due Fri 13 Nov)

### M9: Corrective actions (due Sun 15 Nov)

- [ ] Actions from findings and incidents: owner, due date, status (due Sat 14 Nov)
- [ ] Close-out requires evidence; overdue query (due Sun 15 Nov)

### M10: Toolbox talks & site diary (due Tue 17 Nov)

- [ ] Toolbox talks with worker attendance sign-off (due Mon 16 Nov)
- [ ] Daily site diary: headcount, weather, notes, evidence (due Tue 17 Nov)

### M11: People & documents (due Fri 20 Nov)

- [ ] Worker register; competencies and appointments with expiry (due Wed 18 Nov)
- [ ] H&S file document register with versions in S3 (due Thu 19 Nov)
- [ ] Deadline and expiry reminders in cmd/worker (Redis-backed) (due Fri 20 Nov)

### M12: Reports & audit pack (due Sun 22 Nov)

- [ ] Site compliance summary endpoint: open findings, overdue actions, expiring competencies (due Sat 21 Nov)
- [ ] Audit pack export with evidence manifest and chain verification result (due Sun 22 Nov)
- [ ] Checkpoint: review the safety core against Rev 4 section 6.D; decide cuts here if behind (due Sun 22 Nov)

## Sync, billing & deploy (due Sat 28 Nov)

### SYNC: Offline sync API (due Wed 25 Nov)

- [ ] Client-generated UUIDs and idempotency keys on every create (due Mon 23 Nov)
- [ ] Delta pull per site from a cursor (GET /v1/sync?since=) (due Tue 24 Nov)
- [ ] Batch push with per-record results and conflict rules; evidence is never overwritten (due Wed 25 Nov)

### BILL: Billing (due Fri 27 Nov)

- [ ] PaymentProvider interface, plans, seat limits, subscription states (due Thu 26 Nov)
- [ ] Stitch DebiCheck provider (sandbox) and verified webhooks (due Fri 27 Nov)

### DEPLOY: Deploy (due Sat 28 Nov)

- [ ] Production deploy: GHCR image, secrets, migrate step, health checks, backups (due Sat 28 Nov)
- [ ] Smoke test the deployed API (due Sat 28 Nov)

## Claims & contractor tier (due Sat 5 Dec)

### IOD: Injury on duty (COIDA) (due Tue 1 Dec)

- [ ] IOD case linked to an incident, with the W.Cl.2–6 lifecycle (per Rev 3) (due Sun 29 Nov)
- [ ] IOD deadlines and reminders from regime rules (due Mon 30 Nov)
- [ ] Assemble form data for each stage (structured data first, PDF later) (due Tue 1 Dec)

### JBCC: Delay claims (due Wed 2 Dec)

- [ ] Delay event log, notice deadlines from regime data, evidence links (due Wed 2 Dec)

### PC: Principal contractor tier (due Fri 4 Dec)

- [ ] Link subcontractor orgs to a site; the principal contractor sees their compliance (due Thu 3 Dec)
- [ ] Tests for cross-org visibility under RLS (due Fri 4 Dec)

### V1: Release (due Sat 5 Dec)

- [ ] Full regression, CI green, tag v1.0.0, deploy (due Sat 5 Dec)