# ONBOARD handover for the dashboard (Thu 26 Nov 2026)

What the backend added in ONBOARD (onboarding) that the dashboard needs. The
details, status codes and exact error texts are in `members.md`,
`onboarding.md`, `locations.md`, `workers.md` (the import), `files.md` (the
batch declare), `actions.md` and `compliance.md`; their TypeScript blocks are
checked against the Go code, so copy types from there. Everything here is
for owners and SHE officers at the dashboard; none of it is in a sync push.

## What it is

The first-run set-up of a new organisation, and team management:

1. **Zones and floors** for each site (they existed; now ordered).
2. **The worker register from a spreadsheet**, all or nothing.
3. **Certificates, medicals and company documents in bulk**, all or nothing.
4. **A progress endpoint** for the set-up wizard.
5. **Team management**: list members; change a member's role or status, with
   a reason, recorded for good.

## New endpoints

| Endpoint | Reference |
|---|---|
| `GET /v1/members` (`?status=`) | `members.md` |
| `GET /v1/members/:userId` | `members.md` |
| `PATCH /v1/members/:userId` | `members.md` |
| `GET /v1/members/:userId/history` | `members.md` |
| `PUT /v1/worker-imports/:importId` (`?dry_run=true`; body `text/csv`) | `workers.md` |
| `POST /v1/files/batch` | `files.md` |
| `POST /v1/onboarding/documents` (`?dry_run=true`) | `onboarding.md` |
| `GET /v1/onboarding/progress` | `onboarding.md` |

`GET /v1/members?status=active` answers the earlier request for a list of
the organisation's active members (to add people to a site).

## Contract changes to existing endpoints

- **Locations** (`locations.md`, new reference): `Location.position`
  (number or null), `position` on create, and `PATCH` takes `name`,
  `position` or both (`"position": null` clears it; a field left out is
  kept; `name` is no longer required). The list is ordered by `position`
  (numbered first), then name; it was creation order. New 400s:
  `"position must be a whole number from 0 to 9999, or null"`,
  `"send name, position or both"`.
- **Actions** (`actions.md`): `owner_eligible` on every action (single, site
  register, organisation register): whether the owner can still act on it,
  the rule raise and reassign use (an active owner or SHE officer, or an
  active supervisor on its site). Named `owner_active` in the first ONBOARD
  build, which only read the status; renamed in review before any screen
  used it. The registers take
  `owner_eligible=true|false`; 400 `"owner_eligible must be true or false"`.
- **Compliance summary** (`compliance.md`): `ComplianceItem.owner_eligible`
  (boolean on the `actions` and `verifications` items, null elsewhere).
- **`/v1/me`** (`me.md`): a new capability, `manage_members` (owners and SHE
  officers).
- **Errors anywhere:** a deadlock Postgres breaks is now 503
  `"server busy, try again"` with `Retry-After`, like a lock timeout. It
  shouldn't happen (every writer of memberships locks in one order).

## Screens to build or change

- **Team page.** `GET /v1/members` (filter by status), each with role,
  status and site ids. A member's page: `GET /v1/members/:userId` (open
  work: their open actions and sites) and its history (who, when, from,
  to, why; site additions and removals).
- **Change role or status.** A form with the new role and/or status and a
  required reason (1–500 characters). Into supervisor, ask for their sites
  (`site_ids`, `[]` allowed). Show the answer's `cleared_site_ids` when
  someone stops being a supervisor or is removed. Hide what the caller
  can't do: a SHE officer can't touch an owner or make anyone an owner;
  nobody changes themselves. The API refuses these anyway (403).
- **After suspending or removing someone:** they're signed out everywhere in
  this organisation at once. Nothing they owned is reassigned: show their
  open actions (from the answer's `open_work`) with a "reassign" link.
- **Actions whose owner can no longer act on them:** `owner_eligible: false`
  (suspended, removed, moved to a role that can't own actions, or taken off
  the site). Show "needs a new owner" on the action and in the registers;
  `?owner_eligible=false` lists them all. The same flag is on the compliance
  summary's action items and on each of a member's `open_work` actions.
- **Two owners at once:** demoting the last active owner is refused (409
  `"an organisation must keep at least one active owner"`), even when two
  owners try to demote each other at the same moment.
- **Removed members** come back only by a new invite (same email or mobile):
  their history continues under the same user.
- **Import workers.** Upload the CSV as the request body with
  `Content-Type: text/csv` (or `application/vnd.ms-excel`, which Windows with
  Excel reports for a .csv). Set the header yourself: don't send
  `File.type`, which is empty or something else on some machines. under an import id you choose (a UUID per file).
  First `?dry_run=true`: show the rows as read and every problem with its row
  number (the header is row 1, as in Excel). When `encoding` is
  `windows-1252`, say "read as Windows-1252: check the names". Then the same
  request without `dry_run`. One bad row imports nothing; a repeat after a
  lost answer returns the first result. Accepted headers and their
  alternatives are in `workers.md`; semicolons and commas both work.
- **Load documents in bulk.** Declare the files in one request
  (`POST /v1/files/batch`), upload and complete each, then save every record
  in one request (`POST /v1/onboarding/documents`). Nothing is saved if any
  item has a problem; every item's problem is listed by index. A 409 means
  some files aren't verified yet: complete them and send the batch again.
  Expiry dates go on each record.
- **Set-up wizard.** `GET /v1/onboarding/progress`: steps in order, each
  `required` or not, `done`, counts, a `note` on the optional ones, and
  `gaps` (what's missing, by site and type). `complete` when every required
  step is done. Nothing to store or dismiss.
- **Locations.** Let people order floors (`position`); show the tree by
  `parent_id` in the list's order.

## Decisions that shape the screens

All in `construction_db/CLAUDE.md` (Status, ONBOARD D1–D14):

- The role rules are today's, until the role matrix is approved: owners
  change anyone; SHE officers change anyone but owners and can't grant
  owner; nobody changes themselves.
- Statuses: `invited`, `active`, `suspended`, `removed`. A PATCH swaps
  active and suspended, or removes; removed is final except by re-invite.
- No ID numbers anywhere in the import (M11).
- The CSV has no date columns; certificates and medicals go in through the
  bulk documents.

## Not built (noted)

- Moving an existing supervisor between sites by PATCH: use the site member
  endpoints (`PUT`/`DELETE /v1/sites/:id/members/:userId`).
- Per-user capability grants (G22) and the approved role matrix.
- SMS invites (G20).
