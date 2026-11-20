# M12 handover for the dashboard and the mobile app (Fri 20 Nov 2026)

Everything the backend added in M12:
- who is signed in, with what their role lets them do (`GET /v1/me`);
- the site compliance summary and the organisation rollup;
- uploads still pending after 24 hours;
- audit pack exports.

The details, status codes and exact error texts are in `me.md`,
`compliance.md` and `exports.md`. Their TypeScript blocks are checked
against the Go code, so copy types from there.

## Types to add (`web/src/lib/types.ts`, `mobile/src/lib/types.ts`)

- **Me** (`me.md`): `Me`, and the unions `Role` and `Capability`. It
  replaces the old `MeResponse` (same three ids, plus `role`, `full_name`,
  `org_name`, `capabilities`).
- **Compliance** (`compliance.md`): `SiteSummary`, `ComplianceCategory`,
  `ComplianceItem`, `SiteRank`, `Rollup`, and the unions `Colour`,
  `CategoryKey`, `ComplianceSubject`, `ItemState`.
- **Exports** (`exports.md`): `ExportRequest`, `Export`, `ExportList`,
  `Download`, and the union `ExportStatus`.
- **Changed:** `AppointmentType` (`workers.md`) gains `required` and
  `required_note`.

## New endpoints

| Endpoint | For |
|---|---|
| `GET /v1/me` (changed) | Role, names and capabilities, read on every call |
| `GET /v1/sites/:id/compliance` | One site: every category with its items and colour |
| `GET /v1/compliance` | Every site, worst first, and the organisation's own items |
| `PUT /v1/sites/:id/exports/:exportId` | Queue an audit pack (owners and SHE officers) |
| `GET /v1/sites/:id/exports`, `GET …/:exportId` | Exports and their status |
| `POST /v1/sites/:id/exports/:exportId/download` | A 5-minute link to a ready pack, logged |

## What the dashboard should do

- **Stop guessing the role from 403s.** Call `GET /v1/me` after login and
  when the app comes back to the foreground. Use `capabilities` to hide
  buttons, never to grant anything: every endpoint still answers 403 or 404
  by its own rule. For a supervisor a capability means "on your own sites".
  A suspended member gets 401: sign them out.
- **Home screen: the rollup** (`GET /v1/compliance`). Sites come worst
  first; show each site's colour and its red and amber counts, and the
  organisation's own items (expired vault documents; for owners and SHE
  officers, files with no site still pending) once, at the top.
- **A site's compliance page** (`GET /v1/sites/:id/compliance`): one card
  per category, coloured; each item opens its record (the table in
  `compliance.md` says how). Categories with `counted: false` (absent
  workers, pending uploads) are shown without a colour.
  - Required appointments are **amber with a note**. Show the
    `note` beside it: whether reg 8(1) and 8(7) apply depends on this
    organisation's role on the site, which SiteProof doesn't record yet.
  - `date` and `time_zone` say which "today" the summary used.
  - Overdue actions show their `escalation_step` (1: owner and raiser,
    2: SHE officers, 3: owners).
- **Audit packs** (owners and SHE officers only; don't show the screen to
  others):
  1. Choose a period (at most 366 days, local dates) and queue it with a new
     UUID. Tell the user the pack holds what the server had received by
     now: anything a phone sends later won't be in it.
  2. Poll `GET …/:exportId` every few seconds while `queued` or `building`.
     It starts at least 2 minutes after queuing.
  3. On `ready`, show the `sha256`, the size, and the chain's length and
     head, with a "Download" button that calls `POST …/download` and opens
     the URL straight away (it lasts 5 minutes; every press is logged).
  4. `failed`: show `last_error` and offer a new export. `expired`: the pack
     is gone after 7 days; the record and its `sha256` stay, so a copy
     someone holds can still be checked.
  5. Say on the screen that the pack includes medical certificates and
     injury details, and is logged against the person who downloads it.
- **Pending uploads:** link each to the photo or file; ask whoever uploaded
  it to retry from their phone.

## What the mobile app should do

- **Call `GET /v1/me`** and hide what the role can't do (waivers,
  verification, the vault, medical certificates).
- **Retry pending uploads.** The dashboard now lists photos and files
  declared more than 24 hours ago and never completed. A phone with one in
  its queue should keep retrying it, or declare a new id if the old one was
  rejected.
- Nothing else changes for the phone in M12. Exports are the dashboard's.

## Running it (backend)

- **New environment variable `S3_EXPORT_BUCKET`** for the API and the worker
  (add `S3_EXPORT_BUCKET=exports` to `construction_db/.env`). Both refuse to
  start without it.
- **Create the bucket:** `docker compose run --rm storage-init` now creates
  it too, with versioning and a 7-day lifecycle rule.
- **The worker** builds packs. Locally: `docker compose up -d --build
  worker` (or `go run ./cmd/worker`). It needs the S3 variables as the API
  does. `/healthz/jobs` reports `exports` stale after 2 minutes with no
  finished pass, build or heartbeat (a long build heartbeats as it renews
  its lease).

## Open, not in M12

- **The role matrix** (awaiting approval) decides:
  - who sees the summary;
  - the expired vault items shown on supervisors' sites;
  - who exports, and who sees health details (Rev 4 §2.12 gives health
    documents to the claims handler);
  - the capability list.

  Today owners and SHE officers do everything here.
- **Required appointments** become red for a principal contractor, and
  uncounted for a subcontractor, in PC.
- **Customer branding and versioned templates** on packs, and a generated
  safety file, are gap tasks G18, G32 and G33 (BUILD_PLAN, M12 checkpoint).
