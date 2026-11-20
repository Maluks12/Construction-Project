# SiteProof build plan

Go backend, Construction edition only (Mining is not in this build). Everything due **Sat 5 Dec 2026**. Dates are SAST.

Copied from the tickable plan page on claude.ai (https://claude.ai/artifact/XCsDqW7ag36Fqt3ugKVuUY) on 28 Oct 2026. M5 onwards were reconstructed from Rev 4, not copied from an original roadmap. Tick items here as they're done.

**Checkpoint, Sun 22 Nov:** if the safety core isn't done, cut from the end in this order: JBCC delay claims, then the principal-contractor tier, then IOD form assembly. Keep the IOD lifecycle.

**Decided Sun 15 Nov: no cuts.** The full scope stays. The forecast below (seventh revision, Fri 20 Nov, at the checkpoint) puts v1.0.0 on **Tue 22 Dec** with every gap the checkpoint found, or **Wed 9 Dec** without them. The due date stays Sat 5 Dec. The Sun 22 Nov checkpoint stays as a review point, not a cut.

**Decided Tue 17 Nov (the pricing, Enterprise and onboarding documents):**
- **Pricing is per active site, never per user.** Users are unlimited on every plan; plans limit active sites.
- **CHSO partners get a once-off referral payout**, not a recurring share. It's paid after the referred client has paid its first 3 months, or on its first annual prepay.

## Forecast (Fri 20 Nov, seventh revision: the checkpoint)

**Seventh revision (Fri 20 Nov, the M12 checkpoint).** M12 was built on Fri 20 Nov, in one day against the 4 days re-estimated that morning (it is not yet reviewed). REG4 is dated straight after it (D13, 1 day). The checkpoint (M12, below) went through Rev 4 §6.D's 105 P1 rows and found 38 tasks no milestone held; with no cuts (Sun 15 Nov) they are placed by D14: G35 joins IOD (2 days, was 1); G36–G38 join DEPLOY (3 days, was 2: G37 is code); the required-appointments task (D4) joins PC (4 tasks, 2 days); the other 34 make three new milestones after PC, GAPS-A (11 tasks, 4 days), GAPS-B (8, 3) and GAPS-C (16, 6). The rules are unchanged. **v1 moves from Sun 6 Dec to Tue 22 Dec**, 17 days after Sat 5 Dec. Without the gap milestones it would be **Wed 9 Dec**, 4 days late (REG4, and the extra days in IOD, DEPLOY and PC). The choice between the two is the user's: the gaps are Rev 4 P1, and Rev 4 itself rates JBCC and the PC compliance rollup P2 (Known spec issues, M12).

**Sixth revision (Fri 20 Nov, after M11's review and the CI date fix).** M11 was reviewed and merged on Thu 19 Nov. FIX is added before M12: CI run #84 failed on the M11 tests' date assumptions, and the fix, the email time zone bug it found and the not_signed night-shift decision were done on Fri 20 Nov; its one open task, branch protection, is a settings change. M12 grows from 4 tasks to 5 (the caller's role on GET /v1/me) and ONBOARD from 4 to 5 (team management, full rigour). By the existing rule (open tasks ÷ 3, rounded up) both stay at 2 days, so no date moves: v1 stays **Sun 6 Dec**. FIX took Fri 20 Nov, the first of M12's two forecast days; like review time, that isn't modelled.

**Fifth revision (Tue 17 Nov, after the product and billing decisions).** The estimates are now set from the pace M8–M10 actually had, not from the original plan. M8 (4 tasks), M9 (3) and M10 (4, with the fixture fix) were each built in one day at full rigour: 11 plan tasks in 3 days, about 3.5 a day, against forecasts of 6, 3 and 2 days. The rule below assumes 3 a day, to leave some room. ONBOARD (4 tasks) is added and BILL grew from 2 tasks to 9, 4 of them full rigour. SYNC is set to 3 days by hand: its pull and push, with conflict rules, must cover every record type built so far, which the task count undercounts. Every date is recomputed from M11 starting Tue 17 Nov: v1 moves from Thu 10 Dec to **Sun 6 Dec**, one day after Sat 5 Dec. Review time is still not modelled, and M7–M10 have not been reviewed yet; M6 and M7 ran slower than planned (8 days against 5, 7 against 2), so the faster pace is three milestones' worth of evidence.

**Fourth revision (after M10).** M10 was finished on Mon 16 Nov, against a 2-day estimate that had it on Tue 17 Nov. It grew in planning (signed attendance and the diary's closing, conflicts and addenda became full rigour), yet took one day. Every date below is recomputed from M11 starting Tue 17 Nov, with the same per-milestone estimates: v1 moves from Fri 11 Dec to Thu 10 Dec, 5 days after Sat 5 Dec. Review time is still not modelled, and M7–M10 have not been reviewed yet.

**Third revision (after M9).** M9 was also finished on Sun 15 Nov, against a 3-day estimate that had it on Wed 18 Nov. Every date below is recomputed from M10 starting Mon 16 Nov, with the same per-milestone estimates: v1 moves from Mon 14 Dec to Fri 11 Dec, still 6 days after Sat 5 Dec. Three full-rigour milestones in a day each is not a pace to plan on; the estimates stay as they are. Review time is still not modelled, and M7–M9 have not been reviewed yet.

**Second revision (after M8).** M8 was finished on Sun 15 Nov too, against a 6-day estimate that had it on Sat 21 Nov. Every date below is recomputed from M9 starting Mon 16 Nov, with the same per-milestone estimates: v1 moves from Sun 20 Dec to Mon 14 Dec. As before, the estimates aren't shrunk for two fast milestones. M9, M12 and SYNC are full rigour, and review time is still not modelled. REG4 is still unscheduled; it is dated at the Sun 22 Nov checkpoint.

**First revision.** The first forecast this morning had M7 on Wed 18 Nov, and abandoning (added to M7 the same day) would have pushed it to Thu 19. In fact M7's code was finished on Sun 15 Nov, including abandoning and an end-to-end walk on the real API. So every date below is recomputed from a Mon 16 Nov start for M8, with the same per-milestone estimates. That brings v1 forward from Wed 23 Dec to Sun 20 Dec. The estimates themselves are unchanged: one fast day doesn't justify shrinking them.

How the forecast is built (fifth revision; unchanged since):
- **Pace:** M8–M10 delivered about 3.5 plan tasks a day at full rigour; the forecast assumes 3. Effort days = the milestone's open tasks ÷ 3, rounded up. The rate was measured on full-rigour work, so light milestones get no discount and full ones no extra.
- **Plus one day** for new infrastructure (M11: `cmd/worker` and Redis) or an outside sandbox to integrate (BILL: Stitch).
- **DEPLOY** keeps its 2 days: its tasks are configuration and accounts, not code, so the task rate doesn't apply.
- **SYNC** is set to 3 days: pull and push with conflict rules must cover every record type built so far (inspections, findings, incidents and their versions, actions, toolbox talks and attendance, diary revisions and addenda, evidence); 3 open tasks would give 1.
- **Calendar:** one milestone after another, working every day, as so far.
- **Not modelled:** review time. Each step waits for review before the next builds on it.
- **Earlier revisions** estimated from the original plan, with full-rigour milestones at 1.5 times their planned days (M6 took 8 days against 5; M7 7 against 2).

| Milestone | Rigour | Days | Forecast finish | Was due | Why it takes longer than planned |
|---|---|---|---|---|---|
| M7 Inspections & checklists | full | – | **done Sun 15 Nov** | Tue 10 Nov | Grew by the access log, template versioning, corrections and abandoning |
| M8 Incidents | full | – | **done Sun 15 Nov** | Fri 13 Nov | Built in one day against a 6-day estimate; the dates below are recomputed from it |
| M9 Corrective actions | full | – | **done Sun 15 Nov** | Sun 15 Nov | Built in one day against a 3-day estimate; the dates below are recomputed from it |
| M10 Toolbox talks & site diary | light; full for attendance and closing | – | **done Mon 16 Nov** | Tue 17 Nov | Grew in planning (full rigour on signed attendance, closing, conflicts, addenda); built in one day |
| M11 People & documents | light; full for sign-in, reminders, the file seal and the holiday recompute | – | **done Thu 19 Nov** | Fri 20 Nov | On the forecast date; grew in planning (sign-in, email, the holiday recompute at full rigour); built Wed 18–Thu 19 Nov, reviewed and merged Thu 19 Nov. No later date moves |
| FIX CI dates & night shifts | full for the date fix | – | **done Fri 20 Nov** | Fri 20 Nov | Unplanned: CI run #84. Branch protection is still open (a settings change, not counted) |
| M12 Reports & audit pack | full | – | **done Fri 20 Nov** | Sun 22 Nov | Re-estimated at 4 days that morning (the rollup, the extra summary items, a rebuildable pack); built in one day, not yet reviewed |
| REG4 Regulation 4 notification | full | 1 | Sat 21 Nov | – | Dated at the checkpoint (D13): before SYNC, so sync covers its record |
| SYNC Offline sync API | full | 3 | Tue 24 Nov | Wed 25 Nov | Set by hand (the task count gives 1): pull and push with conflict rules across every record type built so far |
| ONBOARD Onboarding | light; full for team management; the CSV import's all-or-nothing tested | 2 | Thu 26 Nov | Thu 26 Nov | 5 tasks (team management added Fri 20 Nov); added Tue 17 Nov |
| BILL Billing | light; full for invoicing, price versions, metering and webhooks | 4 | Mon 30 Nov | Mon 30 Nov | 9 tasks (3 days, was 2 tasks), plus 1 for the Stitch sandbox (external) |
| DEPLOY | – | 3 | Thu 3 Dec | Mon 30 Nov | Configuration, not code, plus G37 (shipping the logs off-instance, 1 day of code); needs an AWS account and a practitioner's call on the Object Lock mode (external) |
| IOD Injury on duty | full | 2 | Sat 5 Dec | Tue 1 Dec | 4 tasks (G35, the doctor handout, added at the checkpoint) |
| JBCC Delay claims | full | 1 | Sun 6 Dec | Wed 2 Dec | 1 task |
| PC Principal contractor tier | full | 2 | Tue 8 Dec | Fri 4 Dec | 4 tasks (required appointments by role, M12 D4) |
| GAPS-A Safety records | full | 4 | Sat 12 Dec | – | 11 tasks from the checkpoint |
| GAPS-B Construction pack | full for s37(2) | 3 | Tue 15 Dec | – | 8 tasks from the checkpoint |
| GAPS-C Platform, evidence and reports | light; full for evidence | 6 | Mon 21 Dec | – | 16 tasks from the checkpoint |
| V1 Release | – | 1 | **Tue 22 Dec** | Sat 5 Dec | Wed 9 Dec without GAPS-A to C |

**Critical path:** every milestone follows the last, so all of them are on it, and there is no slack: the forecast is 17 days past Sat 5 Dec (4 without the gap milestones). The external dependencies fall due on it as follows:
- **Stitch sandbox credentials:** needed when BILL starts, Fri 27 Nov, and at the latest for the Stitch task, Mon 30 Nov.
- **AWS account:** needed when DEPLOY starts, Tue 1 Dec.
- **The Object Lock call (mode and retention, with a practitioner):** needed by Tue 1 Dec, before the bucket is created.

Any one of them arriving late moves the release later still, day for day.

**Lead times and decisions (Fri 20 Nov):**
- **AWS account with af-south-1:** request Fri 20 Nov.
- **SES production access:** Mon 23 Nov.
- **Role matrix approval:** Tue 24 Nov, before ONBOARD.
- **Stitch sandbox:** Wed 25 Nov; needed Fri 27 Nov.
- **Object Lock mode, with a practitioner:** by Mon 30 Nov, before the bucket.

**Risks to the forecast:**
- **External waits:** the three above, plus a practitioner reviewing the severity wording and the corrective-action default due days that follow it (low 14, medium 7, high 1, critical 0 days after the day raised; M9, product defaults, not law), which doesn't block a milestone.
- **The pace:** three milestones at about 3.5 tasks a day is the evidence. M6 and M7 ran slower than planned, and review time isn't modelled: if every milestone from M11 to the release took one day more, the release would be Wed 16 Dec.
- **Mobile sync:** the mobile app can't sync offline until SYNC lands (Tue 24 Nov, due Wed 25 Nov).
- **The gap milestones** are estimated from the checkpoint's one-line descriptions; each needs its own plan, and several (hazards, PPE, s37(2), branding) are new record types with their own decisions.
- **No contingency.** Anything that grows the way M7 did moves every date after it.

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

### M10: Toolbox talks & site diary (due Tue 17 Nov; done Mon 16 Nov)

- [x] First commit of the M10 session: M7–M9's committed test fixtures (the forced race tests in `internal/platform/database`, and the storage and evidence packages' fixed keys) use fresh ids per run, so `go test -count=N` works for hunting flakes. Today the second run collides with the first (found in the M9 review, Sun 15 Nov; CI runs once, so it isn't affected) (done Mon 16 Nov: `-count=3` passes on the database, storage and evidence packages)
- [x] Toolbox talks with worker attendance sign-off (due Mon 16 Nov; done Mon 16 Nov). Topic from the site regime's list (`toolbox_topics`, each with its CR 2014 source) or free text; presenter, start and optional end time, notes, photos (verified evidence, same site). Attendance is a name as written (optional firm and clock number; no ID number; one signature per name, firm and clock number), signed from the talk's start to its end, or up to 12 hours after the start without one (night shifts), signed by a tap on the presenter's phone, or `paper` from a photographed signed register (a talk with paper attendance must have a register photo; the database enforces it). Signed attendance is append-only; a retraction is a new row with a reason. M11 links attendance to workers through its own append-only table. **Rigour:** full for signed attendance and the paper rule; light for the rest
- [x] Daily site diary (Construction edition, `/v1/construction`), one per site per local day (the site regime's `time_zone`). **Required, and nothing else is:** weather, headcount and work done. **Optional sections:**
  - weather times: rain start and stop, work stopped and restarted, rain gauge mm, hours lost, critical path, delayed effect;
  - labour by firm (free text, our own team included, optional trade);
  - plant and its state (working, idle, broken down; hours working and standing);
  - instructions received (who, when, verbal or written, what);
  - delays;
  - visitors;
  - deliveries (received, or expected and not received);
  - photos.

  **Headcount** is suggested from that day's toolbox talk attendance (and the M11 site sign-ins later) and entered by the supervisor; both are stored, so a gap stays visible. Who wrote each revision, and when, is recorded. Every revision is kept, the newest by device time wins, and a conflict stays flagged until someone saves a revision based on the newest. That day's incidents and talks are linked by date, not copied in. **It closes at midnight:** a save made on the device before midnight counts if the server receives it within 48 hours (flagged `received_after_close`). After that, changes are dated addenda only, append-only (who, when, why, what), and the original never changes. A missed day can be written as a late diary, sealed on arrival. **Rigour:** full for closing, addenda and conflicts (database-enforced, mutation checks, forced races); light for the rest (due Tue 17 Nov)

Plan approved Mon 16 Nov (decisions D1–D10 in `construction_db/CLAUDE.md`, Status): 10.0 fresh fixture ids; 10.1 plan and decisions; 10.2 toolbox topics in the regime; 10.3 toolbox talks migration (000020); 10.4 toolbox API and `docs/api/toolbox.md`; 10.5 diaries and revisions migration (000021); 10.6 addenda migration (000022); 10.7 diary API, revisions and reads; 10.8 diary addenda API; 10.9 `docs/api/diary.md`, the M10 handover and the end-to-end walk. Review fixes (Mon 16 Nov, 000023): same-name workers told apart by firm, and the night-shift signing window.

- [ ] Open (from the M10 review): an addendum may resolve a diary conflict (`resolves_conflict_with: [revision ids]`, only while the day is closed and in conflict, each id one of `conflict_revision_ids`), and `site_diary_replay` then treats the conflict as resolved in favour of the current head. Not small: addenda would enter the replay and the header guard, and an in-grace revision arriving after such an addendum needs a rule. Until then a conflict open at midnight stays visible and an addendum can only say in words which revision is right

### TALKS: Toolbox talk content library (proposed Mon 16 Nov; not scheduled)

Rev 3 §2.3.4: "Ships with a content library -- foremen will not write safety content." Rev 4 6.B: "Construction toolbox talk content, P1, Activity and season based." M10 ships topic titles with their sources only.

- [ ] Talk content per topic (what the presenter reads out), versioned append-only in the regime data like checklists, chosen by activity and season

### M11: People & documents (due Fri 20 Nov; forecast Thu 19 Nov; done Thu 19 Nov; reviewed and merged Thu 19 Nov)

- [x] Worker register; competencies and appointments with expiry (due Wed 18 Nov; done Thu 19 Nov)
- [x] H&S file document register with versions in S3 (due Thu 19 Nov; done Thu 19 Nov)
- [x] Organisation-level company documents with expiry dates (the statutory vault): Letter of Good Standing, COIDA registration, CIDB, tax clearance. Separate from the site H&S file register; both feed the expiry reminders (due Thu 19 Nov; added Tue 17 Nov)
- [x] Daily site sign-in and sign-out from the worker register. A person taps their name on the supervisor's phone or a site tablet. Anyone not on the register yet is added on the spot (name, optional firm and clock number, no ID numbers), the same as at a toolbox talk. A photographed paper sign-in sheet is the fallback, like the paper talk register. The diary's headcount hint becomes the distinct people who signed in that day plus anyone who only signed a talk, matched on the same identity key. The supervisor's own number is still what's saved. With the register, the dashboard can show who was expected on site but didn't sign in. QR codes on workers' own phones are optional at most, never the main method. **Decisions to put before coding:** whether sign-out is required; what happens to people still signed in at midnight; whether a person can be signed in on two sites at once; and how a sign-in is matched to a worker register entry versus a name only (due Thu 19 Nov; decided Tue 17 Nov)
- [x] Deadline and expiry reminders in cmd/worker (Redis-backed) (due Fri 20 Nov; done Thu 19 Nov). Also corrective actions: escalate overdue ones up the role hierarchy (Rev 3 "visible ageing and escalation of overdue actions", Rev 4 A.4 "escalation up the role hierarchy", both P1), and flag stalled verifications from `awaiting_since` (added Sun 15 Nov, M9)
- [x] Recompute open deadlines after a holiday-calendar migration (a newly proclaimed day): each recompute is a new deadline row superseding the old, so what the system said before stays on record. M8 ships the SQL that lists the deadlines a given date affects. Alert on job non-completion, not only failure (Rev 4 §2.3) (added Sun 15 Nov)

Plan approved Wed 18 Nov. The decisions (D1–D26) are in `construction_db/CLAUDE.md` (Status). The items above, with the rules decided:

- **Workers.** A worker is a name, optional firm and clock number, and trade. They are active or not. There's no ID number, photo or biometrics, and one identity key per org (M10's name|firm|clock number).
  - **Competencies:** each has a type from the regime that the record names, an issue date, an expiry as the type says, and a required document.
  - **Medicals:** each has an outcome (fit, fit with restrictions, unfit), its dates, an optional practice number and the certificate. Only an owner or SHE officer can open the certificate, and every view is logged. There are no diagnoses or restrictions.
  - **Renewal** is a new record superseding the old.
  - **Links:** toolbox signatures link to workers through their own append-only table, and diary labour rows can name workers.
  - **Validation:** reusable by ONBOARD's CSV import.
- **Appointments** (Construction edition): per site, a register worker, a CR 2014 type from the site regime, dates, and a required document; superseded or retracted, never edited.
- **H&S file and vault.**
  - **Documents** have numbered, append-only versions. Each version is a file with its own server-checked SHA-256 (not the evidence chain), under Object Lock, with logged views.
  - **The H&S file** is per site, with types from the site regime. It shows each required type as present, missing or expired.
  - **The vault** is per organisation, for owners and SHE officers only, with types from the regime the record names. It's ready for BILL's gate and PC's cross-org read; neither is built.
- **Site sign-in.**
  - **Who signs in:** every sign-in is a register worker. Someone new is added on the spot, or matched to the worker with the same identity key; a wrong match is retracted and signed in again with a clock number.
  - **Sign-out** is optional, recordable up to 16 hours after the sign-in, and nothing happens at midnight.
  - **Open sign-ins:** one per worker per site. Two sites at once are allowed and flagged.
  - **Time:** the phone's time counts. A sign-in that arrives late is accepted, and flagged after the diary's 48-hour grace.
  - **Expected** means the site's roster.
  - **Toolbox talks** show who was signed in but didn't sign the talk.
    **Decided Fri 20 Nov (night shifts):** `not_signed` includes a worker
    whose sign-in on the site was still standing at the talk's start (not
    signed out, not ended by a later sign-in, not retracted), even one made
    the previous date, as well as that day's sign-ins before the talk ended.
    The day register, absent list and headcount stay by sign-in date.
  - **Lapsed credentials** produce warnings; a sign-in is never refused.
  - **Headcount hint:** distinct people among the day's sign-ins and talk signatures.
- **Reminders** (cmd/worker; Redis for timing, Postgres for once-only).
  - **What they cover:**
    - incident deadlines;
    - overdue actions, escalated to SHE officers after 1 day and to owners after 3;
    - stalled verifications;
    - expiries of competencies, medicals, appointments and H&S file documents (30, 14 and 7 days before, and on the day);
    - expiries of company documents (60, 30 and 7 days before, and on the day).
  - **Delivery:** in-app and email. Each reminder goes once per item and threshold, across restarts and two workers. A renewed item's reminders stop.
  - **Monitoring:** a job with no success for twice its interval is reported stale on `/health`.

Proposed Wed 18 Nov (not scheduled):

- [ ] Push notifications (Expo) to the mobile app (Rev 4 A.10, P1). Needs device-token registration and an Expo account
- [ ] SMS and WhatsApp reminders, metered with BILL (Rev 4 A.10, SMS P2 "paid add-on")
- [ ] Per-organisation reminder lead times (settings) overriding the regime defaults
- [ ] Appointment letters and appointment gap detection (Rev 3 §2.3.8, P1; Rev 4 6.D allows deferring gap detection to early P2); induction records and site clearance (Rev 4 A.5, §5.3 `worker_site_clearance`)

### FIX: CI dates and night shifts (due Fri 20 Nov)

- [x] Fix the SAST date assumptions in the tests behind CI run #84 (branch fix/ci-sast-dates); audit product code for dates not derived through Africa/Johannesburg. Found and fixed: reminder emails told dates in the first loaded regime's zone (now `notification_regime`, 000036), and days counted as hours ÷ 24 (done Fri 20 Nov, #14). CI runs the date-sensitive tests as if at 00:30 SAST, with a guard that production always uses the real clock (done Fri 20 Nov, chore/ci-midnight-and-plan)
- [x] A toolbox talk's `not_signed` lists workers whose sign-in on that site was still standing at the talk's start (night shifts across midnight), and workers who signed in on the talk's date after it started; the day view, absent list and headcount stay by sign-in date (done Fri 20 Nov, #14)
- [ ] Branch protection on main: changes by PR only, "Lint, test, build" required (due Fri 20 Nov)

### M12: Reports & audit pack (due Sun 22 Nov; done Fri 20 Nov, not yet reviewed)

- [x] Add the caller's role to GET /v1/me (due Sat 21 Nov; done Fri 20 Nov, with names and capabilities)
- [x] Site compliance summary endpoint (scope decided Thu 19 Nov; done Fri 20 Nov, with the organisation rollup). **Expiring** means each record's own state. **Counts against a site:** missing or expired required H&S file and vault documents, unactioned findings, overdue actions with their escalation step, stalled verifications, and today's sign-ins with warnings. **Shown but not counted:** absent workers. **Not shown:** reminders (due Sat 21 Nov)
- [x] In the compliance summary, list each site's evidence, and the organisation's document files (M11 `files`), still pending more than 24 hours after they were declared. A declared upload that's never completed stays pending forever, and any object it uploaded stays under Object Lock; this makes abandoned uploads visible rather than silent (due Sat 21 Nov) (done Fri 20 Nov)
- [x] Audit pack export with evidence manifest and chain verification result. Include each site's chain head hash and length: the chain alone can't detect its newest entries being deleted (with site_chain_heads rewound), but a pack someone already holds records how long it was (see internal/core/evidence/CHAIN.md). Until the daily signed anchor (P2) exists, this is the only external record. Verifying a long chain (it walks every entry) can take longer than REQUEST_TIMEOUT (default 10s), and a pack covers many sites, so the export may need to run in cmd/worker rather than inside a request. Include abandoned inspections, marked abandoned, with their answers and who abandoned them, when and why (decided 15 Nov) (due Sun 22 Nov) (done Fri 20 Nov, in cmd/worker)
- [x] Checkpoint: review the safety core against Rev 4 section 6.D; decide cuts here if behind (due Sun 22 Nov; done Fri 20 Nov: no cuts, 38 gap tasks, below)

Plan approved Fri 20 Nov (decisions D1–D16 in `construction_db/CLAUDE.md`, Status). **M12 no longer fits 2 days:** the org rollup, the extra summary items and an audit pack that can be rebuilt as of its queue time make it about 4 days (summary and rollup 1, history tables and as-of reads 0.5–1, export storage, worker, pack and races 2, checkpoint 0.5), finishing Tue 24 Nov. It was built in one day, Fri 20 Nov; the forecast is recomputed from that (seventh revision, above). The items above, with the rules decided:
- **GET /v1/me:** `role`, `full_name`, `org_name` and `capabilities`. Capabilities are display hints from one table of today's route guards; no handler reads them. For a supervisor they mean "on your own sites". The table and the guards change together when the role matrix is approved.
- **Compliance summary** per site and an organisation rollup, with counts, items by record id and red/amber/green per category; the site's status is its worst. "Today" is in the site regime's zone. Also open incident deadlines, expiring competencies, medicals and appointments of roster workers, and required appointments with no live appointee (amber, with a caveat; see PC).
- **Pending uploads:** evidence and files pending more than 24 hours after they were declared, per site; files with no site in the rollup, for leads only.
- **Audit pack:** built by `cmd/worker` as a deterministic ZIP (`manifest.json`, `index.html` in inspector order, `README.txt`, the original files), as of the time the export was queued, so a rebuild gives the same SHA-256. Each export is an append-only record with the pack's hash; downloads are logged. Packs live 7 days in their own bucket. Owners and SHE officers only.

#### Checkpoint: Rev 4 §6.D, line by line (Fri 20 Nov)

§6.D is a totals table (P1: shared core 85, construction 20) and a "keep / defer if it must shrink" list. Each line it counts is reviewed below: every P1 row of 6.A (A.1–A.10) and 6.B, read in order (the PDF's tables are shifted as noted in `construction_db/CLAUDE.md`; counted in order they give exactly 85 and 20), plus the P2 rows already built. Mining (6.C) is not in this build: not reviewed. **Decided Sun 15 Nov: no cuts**, so every gap is a task below, placed by D14 (new milestones after PC, before V1; a new record type includes its sync adapter).

**The keep list, first:**

| §6.D keep / defer | Status |
|---|---|
| Checklists with photo-on-fail | **built** (M7) |
| Corrective actions | **built** (M9; escalation M11) |
| Incidents with the reporting clock | **built** (M8; reminders M11) |
| Worker register with attendance and clearance | **partly**: register and attendance built (M11); site clearance and inductions not built (G5) |
| The statutory vault | **built** (M11); annual filing reminders not built (G27) |
| Offline capture | **partly**: device ids, late arrivals and the records-before-photos rule everywhere; the sync API is SYNC (next); the queue, storage budget and Wi-Fi preference are the mobile app's |
| The safety file | **partly**: the H&S file with readiness (M11) and the audit pack in inspector order (M12); a generated safety-file pack from versioned templates is G18 |
| Defer: housekeeping trends, appointment gap detection, s37(2) | **not built**; kept as G16, G14, G15 (no cuts) |

**6.A Shared core, P1 (85):**

| § | Feature | Status | Gap task |
|---|---|---|---|
| A.1 | Organisation accounts | partly: signup creates one; registration, VAT and COIDA insurer can't be edited | G19 |
| A.1 | Edition selection at signup | partly: the edition is the default one (Mining not built) | – (Mining) |
| A.1 | Edition attached to site | built (`sites.regime_id`, M5) | |
| A.1 | Regulatory regime model | built (`internal/regimes`, embedded JSON rather than `regime_*` tables) | |
| A.1 | Core/edition import boundary check | built (`internal/archtest`) | |
| A.1 | User accounts, JWT with refresh rotation | built (M4) | |
| A.1 | Device session revocation | built (sessions list and revoke) | |
| A.1 | Phone-number invitations | partly: invites by mobile; the SMS deep link isn't sent | G20 |
| A.1 | Roles and capability flags | partly: roles, and capability hints on `/v1/me` (M12); per-user grants not built | G22 |
| A.1 | Site scoping | built | |
| A.1 | Site location hierarchy | partly: `site_locations` (zones and floors to be checked in ONBOARD) | ONBOARD |
| A.1 | Onboarding wizard per edition | not built | ONBOARD |
| A.1 | Mining waitlist on signup | not built | G21 |
| A.2 | In-app capture with GPS | built server-side (lat/lng, device, sealed in `metadata_hash`); capture is the app's | |
| A.2 | On-device compression | built server-side (dimension and size limits refused); compression is the app's | |
| A.2 | SHA-256 at capture | built (re-hashed on receipt, M6) | |
| A.2 | Dual timestamps | built | |
| A.2 | Per-site hash chain | built (`CHAIN.md`) | |
| A.2 | Immutable originals, derived markup | partly: originals under Object Lock; no derived files | G23 |
| A.2 | Watermarking | not built | G23 |
| A.2 | Tagging and galleries | not built (a site's evidence list only) | G24 |
| A.2 | Access log | built (views M7, exports M12; no share feature yet) | |
| A.2 | Paper-to-photo capture | partly: paper talk registers and sign-in sheets; other completed forms not | G25 |
| A.3 | Full offline capture | partly: every write takes a device id and accepts late arrival | SYNC |
| A.3 | Full-shift offline tolerance | partly: as above; delta pull and batch push are SYNC | SYNC |
| A.3 | Client-generated UUIDs | built (PUT under device ids everywhere) | |
| A.3 | Separate metadata and media queues | built server-side (records before photos; completion waits) | |
| A.3 | Resumable multipart uploads | not built (one presigned PUT, 10–20 MiB caps) | G26 |
| A.3 | WiFi-preferred with data override | the mobile app's | mobile |
| A.3 | Local storage budget and warnings | the mobile app's | mobile |
| A.3 | Per-record sync status | partly: each record's server state; the app shows it | SYNC |
| A.4 | Calendar and working-day rules | built (M8) | |
| A.4 | SA public holiday calendar as data | built (`public_holidays`, M8; recompute M11) | |
| A.4 | Site shutdown periods | not built | G28 |
| A.4 | Immediate-report prompts | built (forthwith `due_now`, call recorded as a satisfaction) | |
| A.4 | Calendar inspector | partly: each deadline lists its excluded days; no calendar endpoint | G29 |
| A.4 | Computation and notification log | built (deadline rows with their inputs, recomputations, notifications, `job_runs`) | |
| A.4 | Escalation up the role hierarchy | built (M11) | |
| A.4 | Non-completion alerting | built (`/healthz/jobs`, M11) | |
| A.5 | Versioned checklist engine | built (M7) | |
| A.5 | Photo mandatory on fail | built | |
| A.5 | Reference photos | not built | G7 |
| A.5 | Per-shift, daily and per-activity scheduling | not built | G8 |
| A.5 | Corrective action register | built (M9) | |
| A.5 | Open-items ageing | partly: overdue with escalation step (M11, M12 summary); no ageing report | G9 |
| A.5 | Hazard identification | not built | G1 |
| A.5 | Near-miss one-tap capture | partly: `near_miss` in the incident taxonomy; no one-tap entry | G2 |
| A.5 | Toolbox talks with content library | partly: talks and signed attendance (M10); no content | G12 (TALKS) |
| A.5 | PPE checks and issue register | not built | G3 |
| A.5 | Risk assessments with review dates | partly: an H&S file document type; no review dates | G4 |
| A.5 | Induction records | not built | G5 |
| A.6 | Incident capture | partly: type, people, injury, witnesses, photos; no sketch | G10 |
| A.6 | Regime taxonomy | built | |
| A.6 | Obligation routing | built (every rule a version's types trigger) | |
| A.6 | Incident register | built | |
| A.7 | Worker register | partly: no ID number (decided M10/M11) and no photo (decided M11 D5) | – (decided) |
| A.7 | Credentials and competencies | built (M11) | |
| A.7 | Medical fitness status and expiry | built (M11; certificate stored, decided 18 Nov) | |
| A.7 | Site-specific clearance | not built | G5 |
| A.7 | QR/NFC worker cards | not built | G30 |
| A.7 | Roll-call fallback | built (tap a name; paper sheet) | |
| A.7 | Geofence check and headcount reconciliation | partly: headcount suggested vs entered (both stored); no geofence | G31 |
| A.7 | Append-only attendance | built | |
| A.7 | Clock-in block on lapsed requirements | partly: warnings, never a block (decided M11 D18) | – (decided) |
| A.8 | Organisation document vault | built (M11) | |
| A.8 | Issuer-aware Letter of Good Standing | built | |
| A.8 | Expiry alerting at 60/30/7 days | built | |
| A.8 | Annual filing reminders | not built | G27 |
| A.9 | Pre-filled employer's report | not built | IOD |
| A.9 | Doctor handout | not built | G35 (IOD) |
| A.9 | 7-day reporting clock | built (`coida_s39_employer_report`, M8) | |
| A.9 | Insurer routing | partly: the insurer column and LGS issuers; no routing | G19 |
| A.10 | Report engine with versioned templates | partly: the audit pack, versioned by `pack_format` (M12); no templates | G33 |
| A.10 | Customer branding | not built | G32 |
| A.10 | Asynchronous large-pack generation | built (M12, `cmd/worker`) | |
| A.10 | Push, email, in-app notifications | partly: email and in-app (M11); push not built | G34 |
| A.10 | Stitch DebiCheck with card fallback | not built | BILL |
| A.10 | Provider-agnostic billing layer | not built | BILL |
| A.10 | VAT invoices, annual prepay, dunning | not built | BILL |
| A.10 | Mandate revocation on cancel | not built | BILL |
| A.10 | Tenant isolation plus row-level security | built (M5; every table since) | |
| A.10 | Separate keys: evidence, app, health | not built | G36 (DEPLOY) |
| A.10 | S3 Object Lock | built (mode at DEPLOY) | |
| A.10 | Append-only audit log | partly: append-only in Postgres for every role; not shipped off-instance | G37 (DEPLOY) |
| A.10 | Tested quarterly restore | not built | G38 (DEPLOY) |

**6.B Construction edition, P1 (20):**

| Feature | Status | Gap task |
|---|---|---|
| Construction Regulations checklist pack | built (5 checklists; scaffolding is a SOURCES gap) | |
| Construction toolbox talk content | partly: 9 topics with sources; no content | G12 (TALKS) |
| Housekeeping walks by zone | not built | G6 |
| Housekeeping trends | not built | G16 |
| Appointments register and letters | partly: the register (M11); no letters | G13 |
| Appointment gap detection | not built (two required types flagged, M12 D4) | G14 |
| Section 37(2) mandatary agreements | not built | G15 |
| Regulation 4 notification | not built | REG4 |
| H&S plan and specification storage | built (H&S file, M11) | |
| MCoF Annexure 3 tracking | built (medicals, M11) | |
| OHSA s24 reportable incident flow | partly: the clocks (M8); no Annexure 1 preparation | G11 |
| W.Cl form set for IOD (P1–P2) | not built | IOD |
| FEM routing and LGS | partly: LGS with FEM as issuer; no routing | G19 |
| Safety file assembly (inspector order) | partly: the audit pack in inspector order (M12) | G18 |
| Inspection readiness view (present, missing, expired) | built (H&S file readiness M11; compliance summary M12) | |
| Site diary, two-tier (sixty-second default, escalation) | partly: the full diary (M10); no quick entry or missing-day escalation | G17 |
| Weather by effect | built (M10) | |
| Labour by trade and subcontractor | built (M10) | |
| Plant working vs standing | built (M10) | |
| Materials expected not received | built (M10) | |

**P2 rows already built:** the evidence export pack with hash manifest and chain verification (A.2, P2): the M12 audit pack. Not P1 in Rev 4 but in this build's plan: the JBCC delay claims register (6.B, P2) and the PC compliance rollup (6.B, P2) are JBCC and PC; the IOD claim stage tracker (A.9, P2) is IOD.

**Totals:** of 105 P1 rows, 45 built, 29 partly built, 29 not built, and 2 the mobile app's alone. What other milestones already hold (SYNC, ONBOARD, BILL, REG4, IOD) and two decided departures (no ID numbers or photos; warnings, not a clock-in block) aren't new tasks. The rest are 38 tasks, below.

**Gap tasks** (estimates in plan tasks at the forecast's 3 a day; milestones by D14):

- **GAPS-A: Safety records** (after PC; 11 tasks, 4 days; full rigour for records):
  - [ ] G1 Hazard identification register: hazard, location, risk before and after, mitigation, owner, close-out date, before and after photos (Rev 3 §2.3.5), with its sync adapter (1)
  - [ ] G2 Near-miss one-tap entry: a thirty-second, no-blame incident of type `near_miss` from one screen (1)
  - [ ] G3 PPE checks and issue register: required items per activity, worn confirmed, issues to named workers (Rev 3 §2.3.6) (1)
  - [ ] G4 Risk assessments, baseline and activity-specific, versioned with review dates; an activity started without a current one is flagged (Rev 3 §2.3.7) (1)
  - [ ] G5 Induction records and site-specific clearance (Rev 4 §5.3 `worker_site_clearance`), shown as a sign-in warning (1; the proposed task under M11)
  - [ ] G6 Housekeeping walks scored by zone, with photos (Rev 4 §3.3) (1)
  - [ ] G7 Reference photos per checklist item (what "correct" looks like), versioned with the template (1)
  - [ ] G8 Checklist scheduling: per shift, daily, weekly, per activity, with missed inspections shown (1)
  - [ ] G9 Open-items ageing report: actions by age band and severity per site (1)
  - [ ] G10 Incident sketch: a drawn sketch stored as evidence on a version (1)
  - [ ] G11 OHSA s24 Annexure 1 preparation: the form's data from the incident (1)
- **GAPS-B: Construction pack** (after GAPS-A; 8 tasks, 3 days):
  - [ ] G12 Toolbox talk content library (the TALKS task) (1)
  - [ ] G13 Appointment letters generated from the register (1)
  - [ ] G14 Appointment gap detection: an activity underway with no appointment for it (1)
  - [ ] G15 Section 37(2) mandatary agreements: register per contractor, generated agreement, signatures (2)
  - [ ] G16 Housekeeping trends per site (1)
  - [ ] G17 Site diary quick entry (the "sixty-second default") and missing-day escalation (1)
  - [ ] G18 Safety file assembly: a generated, versioned safety-file pack in inspector order, on the M12 engine (1)
- **GAPS-C: Platform, evidence and reports** (after GAPS-B; 16 tasks, 6 days):
  - [ ] G19 Organisation profile (registration, VAT, COIDA insurer) and insurer routing (FEM, RMA, Compensation Fund) (1)
  - [ ] G20 SMS deep-link invites (the SMS provider is BILL's metering) (1)
  - [ ] G21 Mining waitlist on signup (1)
  - [ ] G22 Per-user capability grants on top of role defaults (1; with the approved role matrix)
  - [ ] G23 Derived evidence files: markup and watermarks (company, site, reference, date, GPS), originals untouched (2)
  - [ ] G24 Evidence tags and galleries (1)
  - [ ] G25 Paper-to-photo capture of other completed forms into the chain (1)
  - [ ] G26 Resumable multipart evidence uploads (1)
  - [ ] G27 Annual filing reminders (Return of Earnings, renewals) in the vault (1)
  - [ ] G28 Site shutdown periods (the builders' holiday) in the deadline engine (1)
  - [ ] G29 Calendar inspector: the days a deadline excluded and why (1)
  - [ ] G30 QR/NFC worker cards scanned at the gate (1)
  - [ ] G31 Geofence check on sign-in, flagged (1)
  - [ ] G32 Customer branding on packs (1)
  - [ ] G33 Report templates, versioned, for packs and the safety file (1)
  - [ ] G34 Push notifications (the proposed task under M11) (1)
- **IOD** gains G35, the doctor handout (A.9 P1): 4 tasks, 2 days (was 1).
- **DEPLOY** gains G36 separate keys for evidence, application and health data; G37 the audit logs shipped off-instance (code: 1 day); G38 a tested restore: 3 days (was 2).
- **Mobile app** (not backend): on-device compression and capture, the offline queue's storage budget, Wi-Fi preference with a data override, per-record sync status.

## Sync, onboarding, billing & deploy (due Mon 30 Nov)

### SYNC: Offline sync API (due Wed 25 Nov; forecast Tue 24 Nov)

- [x] Request deadlines and pool limits: replace RequireAuth's own 2s timeout with one request-level deadline set in middleware; a request that hits it or can't get a pool connection gets 503 with Retry-After, never 500; pool size (pgxpool MaxConns) set from config and documented. Found in M6: a burst larger than the pool (default max(4, CPUs)) made auth answer 500 after 2s (due Mon 23 Nov)
- [ ] Client-generated UUIDs and idempotency keys on every create (due Mon 23 Nov)
- [ ] Delta pull per site from a cursor (GET /v1/sync?since=) (due Tue 24 Nov)
- [ ] Batch push with per-record results and conflict rules; evidence is never overwritten (due Wed 25 Nov)

### ONBOARD: Onboarding (due Thu 26 Nov; forecast Thu 26 Nov; added Tue 17 Nov)

**Rigour:** light, except that team management is full rigour and the CSV import needs a test proving that one bad row writes nothing.

- [ ] Site zones and floors: check whether `site_locations` already covers them; extend it only if not (due Wed 25 Nov)
- [ ] CSV import for the worker register: validate every row, report errors per row, all or nothing. Test: one bad row writes nothing (due Wed 25 Nov)
- [ ] Bulk document upload with expiry dates: worker certificates, medicals and company documents (due Thu 26 Nov)
- [ ] First-run setup progress endpoint for the wizard: sites added, team invited, company documents loaded, workers added (due Thu 26 Nov)
- [ ] Team management (due Thu 26 Nov). **Full rigour.** GET /v1/members (members with role and status) and PATCH /v1/members/:userId (role or status). An organisation always has at least one active owner, enforced in the database, with a forced-race test (two owners demoting each other). Suspending or removing a member revokes their sessions and refresh tokens immediately. A role change into or out of supervisor handles site scoping explicitly. Every change is recorded append-only: who, when, from, to, why. Owners and SHE officers may change roles, but only an owner may grant or change the owner role, matching invites. Follows the role matrix in the platform guide once approved

### BILL: Billing (due Mon 30 Nov; forecast Mon 30 Nov)

**Rigour:** full for invoicing, price versions (escalation, cap, rounding, prepay lock), metering and the webhooks; light for the rest.

- [ ] PaymentProvider interface, per-site plans (active-site limit and an extra-site price), subscription states (due Fri 27 Nov)
- [ ] Active-site counting. Archived sites are free and still readable. A lapsed account goes read-only and never loses its evidence (due Fri 27 Nov)
- [ ] Plan features switched on per plan: IOD claims, delay claims, the statutory vault and the subcontractor rollup now; SSO, API access and data export later (due Sat 28 Nov)
- [ ] Free subcontractor accounts under a principal contractor (due Sat 28 Nov)
- [ ] Invoicing: VAT invoices, EFT on 30-day terms, monthly, quarterly or annual billing, and annual prepay (two months free). **Full rigour** (due Sun 29 Nov)
- [ ] One-off and hourly charges on an invoice: done-for-you onboarding, and work beyond its limits (due Sun 29 Nov)
- [ ] Price versions with effective dates: yearly CPI escalation on a fixed date with a cap, 30 days' notice, rounding, and annual prepay locked until renewal. **Full rigour** (due Sun 29 Nov)
- [ ] SMS/WhatsApp metering per organisation, billed at cost plus a margin. **Full rigour** (due Mon 30 Nov)
- [ ] Stitch DebiCheck provider (sandbox) and verified webhooks. Declare the yearly increase on the mandate at signup, and set the maximum collection amount with headroom above the current invoice. Check Stitch's current documentation for both. **Full rigour** for the webhooks (due Mon 30 Nov)

### DEPLOY: Deploy (due Mon 30 Nov; forecast Wed 2 Dec)

- [ ] RLS on users: visible if it's you or shares an org with the tenant; login and invite lookups by email/mobile via SECURITY DEFINER functions (before the production deploy)
- [ ] Create a LOGIN role for siteproof_app with its own password; the production API connects with it, never superuser credentials. The migration owner must be a superuser or have BYPASSRLS (the token lookups are SECURITY DEFINER) (before the production deploy)
- [ ] Evidence bucket retention: choose the Object Lock mode (governance or compliance) and default retention with a practitioner. Compliance can be extended per object but never shortened, and blocks POPIA deletion requests. The same locked bucket holds document files (M11), medical certificates of fitness among them, so this retention decision covers POPIA for health data too: a certificate under compliance retention can't be erased before it ends. Set it as the bucket default and EVIDENCE_LOCK_MODE to match; the API refuses to start otherwise (before the production deploy)
- [ ] Evidence bucket in af-south-1: Object Lock and versioning on, SSE-KMS with its own evidence key (Rev 4 §2.13), all public access blocked (before the production deploy)
- [ ] API IAM role on the evidence bucket only: s3:PutObject, GetObject, GetObjectVersion, GetObjectRetention, GetBucketObjectLockConfiguration. No Delete* and no BypassGovernanceRetention (before the production deploy)
- [ ] Rate limiting on auth and evidence endpoints (Rev 3 security list, P1) (before the production deploy)
- [ ] Evidence downloads logged by S3: server access logging or CloudTrail data events on the evidence bucket, to a separate bucket. The API's access log records that a 60-second view link was issued, not that it was used; this records the actual GETs (before the production deploy)
- [ ] Set TRUSTED_PROXIES to the load balancer's addresses. Empty (the default) trusts no proxy, so behind a load balancer every access-log row would record the load balancer's IP; listing a range clients can reach lets them choose the logged IP (before the production deploy)
- [ ] Decide who may see injury and treatment details in `incident_people` (POPIA special personal information: health). Today every owner, SHE officer and supervisor on the site can (added Sun 15 Nov) (before launch)
- [ ] Worker email (M11): set SMTP_URL to SES SMTP (smtp://user:password@host:587?from=...) with a verified sender. The worker refuses to start without SMTP_URL unless EMAIL_MODE=log, which only logs emails and records them 'logged', never 'sent': never set EMAIL_MODE=log in production (before the production deploy)
- [ ] Worker deploy (M11): run the image with entrypoint /worker beside the API, with REDIS_URL (ElastiCache or similar), and point the uptime monitor at /healthz/jobs (503 when a job has gone twice its interval without completing) (before the production deploy)
- [ ] Export bucket (M12 D7) in af-south-1: versioning on, no Object Lock, SSE-KMS, all public access blocked, a lifecycle rule deleting current versions after 7 days and noncurrent versions after 1. Set `S3_EXPORT_BUCKET` for the API and the worker (both refuse to start without it). IAM: the worker may `s3:PutObject` (and the multipart actions: `CreateMultipartUpload`, `UploadPart`, `CompleteMultipartUpload`, `AbortMultipartUpload`) on this bucket only, plus `GetObject`/`GetObjectVersion` on the evidence bucket; the API `GetObject`/`GetObjectVersion` on the export bucket for download links. No `Delete*` anywhere (before the production deploy)
- [ ] G36 (M12 checkpoint, Rev 4 A.10 P1): separate KMS keys for evidence, application data and health data (medical certificates, injury details) (before the production deploy)
- [ ] G37 (M12 checkpoint, Rev 4 §2.13, A.10 P1): ship the append-only logs (`evidence_access_log`, `file_access_log`, `audit_export_downloads`) off-instance, as the chain anchor will be. 1 day of code (before the production deploy)
- [ ] G38 (M12 checkpoint, Rev 4 A.10 P1): a tested restore from backup, repeated quarterly (before launch)
- [ ] Production deploy: GHCR image, secrets, migrate step, health checks, backups (due Mon 30 Nov)
- [ ] Smoke test the deployed API. Use a dedicated smoke-test org and site: evidence it uploads can never be deleted (due Mon 30 Nov)

## Claims & contractor tier (due Sat 5 Dec)

### IOD: Injury on duty (COIDA) (due Tue 1 Dec; forecast Thu 3 Dec)

- [ ] IOD case linked to an incident, with the W.Cl.2–6 lifecycle (per Rev 3) (due Tue 1 Dec)
- [ ] IOD deadlines and reminders from regime rules. The 7-day W.Cl.2 clock already exists: M8 computes `coida_s39_employer_report` on the incident when an employee is injured. The IOD case reuses that deadline (its satisfaction is the W.Cl.2 being filed); it must not start a second one (decided Sun 15 Nov) (due Tue 1 Dec)
- [ ] Assemble form data for each stage (structured data first, PDF later) (due Tue 1 Dec)
- [ ] G35 (M12 checkpoint, Rev 4 A.9 P1): the doctor handout, shareable by WhatsApp or print, from the incident and worker

### JBCC: Delay claims (due Wed 2 Dec; forecast Fri 4 Dec)

- [ ] Delay event log, notice deadlines from regime data, evidence links (due Wed 2 Dec)

### PC: Principal contractor tier (due Fri 4 Dec; forecast Sat 5 Dec)

- [ ] Link subcontractor orgs to a site; the principal contractor sees their compliance (due Thu 3 Dec)
- [ ] Tests for cross-org visibility under RLS (due Fri 4 Dec)
- [ ] Required appointments by the organisation's role on the site (added Fri 20 Nov, M12 D4): once a site records whether this organisation is the principal contractor, a principal contractor's missing construction manager (CR 2014 reg 8(1)) or construction supervisor (reg 8(7)) becomes red in the compliance summary, and a subcontractor's is not counted. Until then both are amber with a caveat
- [ ] Quote and model GAR 8(3) (incidents to people other than employees: who reports) and OHSA s24(3) (exclusions, e.g. a traffic accident on a public road). Until then every reportable type starts its clocks and the GAR 8(1) deadlines carry a caveat (decided Sun 15 Nov; here because non-employees on a site are mostly subcontractors' workers)

### REG4: Regulation 4 notification (proposed Sun 15 Nov; dated at the checkpoint, Fri 20 Nov: straight after M12, before SYNC, M12 D13)

Rev 4's construction pack has "Regulation 4 notification, P1, 7-day deadline and tracking", but no task existed. `editions/construction/reg4` is empty.

- [ ] A site's planned construction work (start date, and which of reg 4(1)(a)–(d) or 4(2) applies), creating `cr2014_reg4_notification` (and, for reg 3 permits, `cr2014_reg3_permit_application`) deadlines through the M8 engine (`applies_when: site_work`). Decide whether "at least 7 days before" means clear days. Rev 4 §3.2 overstates reg 4's scope (see SOURCES)

### GAPS-A, GAPS-B, GAPS-C: the §6.D gaps (added Fri 20 Nov at the checkpoint; forecast Sat 12, Tue 15 and Mon 21 Dec)

The 34 tasks are listed, with their sources and estimates, under M12's checkpoint (G1–G34). Each milestone gets its own plan and decisions before code, like every milestone so far; a new record type includes its sync adapter (D14).

### V1: Release (due Sat 5 Dec; forecast Tue 22 Dec, or Wed 9 Dec without the gap milestones)

- [ ] Full regression, CI green, tag v1.0.0, deploy (due Sat 5 Dec)

## Enterprise & growth (Tue 8 – Fri 18 Dec; after v1, not part of it)

### ENT: Enterprise & growth (due Fri 18 Dec; added Tue 17 Nov)

- [ ] Custom plans per organisation: their own prices, site volume bands and contract dates, alongside the standard plans. No customer-specific code (due Tue 8 Dec)
- [ ] SSO, switched on per plan (due Thu 10 Dec)
- [ ] Public API with API keys, switched on per plan (due Fri 11 Dec)
- [ ] Custom reports and data export, switched on per plan (due Mon 14 Dec)
- [ ] Usage counts for the Enterprise quote triggers: active sites, office users, linked subcontractors (due Tue 15 Dec)
- [ ] CHSO partner programme: partner record, referral attribution, and the once-off payout (after the referred client has paid its first 3 months, or on its first annual prepay) (due Thu 17 Dec)
- [ ] A CHSO can work across several client organisations, with scoped access under RLS and tests (due Fri 18 Dec)