# Offline sync API (SYNC, Thu 26 Nov 2026)

For the mobile app. A phone works a whole shift offline, then pushes what it
captured in one batch and pulls what changed. Sync is a **transport over the
API, not new rules**:

- every pulled item is exactly the body of that record's own GET, for your
  role;
- every pushed record goes through its own endpoint (same validation, same
  errors, same idempotency), and its result is that endpoint's status and
  body.

So the per-feature references (`inspections.md`, `incidents.md`, …) stay the
source for what a record looks like and what each write may answer.
`TestAPIDoc` (`construction_db/internal/app/sync_apidoc_test.go`) fails if the
types below drift from the Go code or an error text the sync endpoints send
isn't quoted here.

Owners, SHE officers and supervisors may sync; any other role gets 403
`"forbidden"`. Supervisors see only the sites they have been added to, as
everywhere.

## Pull

| Call | What |
|---|---|
| `GET /v1/sync?cursor=&limit=` | Your organisation: the sites you can see now (always the whole list), `/v1/me`, and the changed workers, vault documents (owners and SHE officers) and your notifications |
| `GET /v1/sync/sites/:siteId?cursor=&limit=` | Everything changed on one site |
| `GET /v1/sync/sites/:siteId/reference` | The site's regime data, fetched again only when its `sha256` changes |

### Paging and the cursor

Leave `cursor` out for a full sync. Every response carries a `cursor`; send
it back on the next call. While `has_more` is true, call again at once with
the new cursor: the pass isn't finished. When it is false, store the cursor
and use it for the next delta.

- `limit` is 1–500 items, default 200. A page also stops after the item that
  takes it past 1 MiB, or after about 5 seconds (the request deadline is
  10). A page may hold fewer items than `limit` and still have more.
- The cursor is opaque: don't build or change one. It is per scope (the org,
  or one site), per organisation, per person and per role. Someone in two
  organisations keeps a set of cursors for each.
- An item can come more than once, across pages or passes: replace what you
  hold by `kind` and `id`.
- A row is never lost to a slow write: a pull never passes a transaction that
  hasn't finished, it waits for it (one design for every table: each row
  carries the id of the transaction that last wrote it, and a pull reads up to
  the oldest one still running). A long write can delay a pull; it can't
  hide a record from one. That wait is server-wide: one write left open
  anywhere (an operator's session, a long migration) holds every pull back
  until it ends. `/healthz/jobs` reports the oldest open write as
  `oldest_write_seconds`, and `sync_pull` among its stale entries past 5
  minutes, so it is alerted on; pulls answer normally meanwhile, just
  without what was written after it.

### `reset`

`reset: true` means start again: drop everything you pulled for that scope
(the org, or that site) and keep the pages that follow as the full set. Your
**unsent** records are not affected: keep them and push them. A reset comes
when:

- the server was restored from a backup (the cursor names a server epoch);
- your role changed (fields visible to the old role must go);
- the cursor belongs to someone else, or to another of your organisations, or
  comes from the future;
- a new kind of record was added mid-pass.

After a reset, also push again the records you sent in the last 7 days: a
restore to an earlier point may have lost them, and a repeat of one the
server still has answers 200.

### Errors

| Status | When |
|---|---|
| 400 | `"invalid cursor"` (malformed, or another scope's), `"limit must be 1-500"` |
| 401 | Signed out: sign out on the phone |
| 403 | `"forbidden"`: a role that doesn't sync |
| 404 | `"site not found"`: you can no longer see this site (below) |
| 503 | `"server busy, try again"` with `Retry-After` |

### What a pull holds

`data` is the body of the GET in the last column, as your role would get it.
A field your role's GET doesn't show isn't in a pull either.

| `kind` | `id` | `data` is | Scope |
|---|---|---|---|
| `location_list` | site id | `GET /v1/sites/:siteId/locations` | site |
| `inspection` | inspection id | `GET /v1/sites/:siteId/inspections/:id` | site |
| `finding_list` | inspection id | `GET /v1/sites/:siteId/findings?inspection_id=:id&limit=200` | site |
| `action` | action id | `GET /v1/sites/:siteId/actions/:id` | site |
| `incident` | incident id | `GET /v1/sites/:siteId/incidents/:id` (latest version) | site |
| `deadline` | deadline row id | `GET /v1/sites/:siteId/deadlines/:id` (with every satisfaction) | site |
| `toolbox_talk` | talk id | `GET /v1/sites/:siteId/toolbox-talks/:id` | site |
| `sign_in_day` | `YYYY-MM-DD` | `GET /v1/sites/:siteId/sign-ins?date=:id` | site |
| `roster` | site id | `GET /v1/sites/:siteId/roster?all=true` | site |
| `diary_day` | `YYYY-MM-DD` | `GET /v1/construction/sites/:siteId/diary/:id` | site |
| `site_work` | site work id | `GET /v1/construction/sites/:siteId/site-work/:id` | site |
| `appointment_list` | site id | `GET /v1/construction/sites/:siteId/appointments` | site |
| `safety_file_document` | document id | `GET /v1/construction/sites/:siteId/safety-file/:id` | site |
| `worker` | worker id | `GET /v1/workers/:id` (competencies and medicals: outcome and dates, never the certificate) | org |
| `vault_document` | document id | `GET /v1/vault/documents/:id` (owners and SHE officers only) | org |
| `notification_list` | your user id | `GET /v1/notifications?limit=200` | org |

An item is sent again whenever a row its GET reads changes, including rows
of other records it shows:

- an action: its events and close-outs, its source's correction, an
  amendment of its incident;
- a finding list: an action or waiver anywhere in the inspection's
  correction chain;
- an incident: a version, statement, deadline or satisfaction;
- a talk: its signatures, a worker matched to a signature (by link or by
  name, firm and clock number), that day's sign-ins (`not_signed`);
- a sign-in day: its sign-ins and sheets, those workers' competencies,
  medicals and names (`warnings`), their sign-ins on other sites that day
  (`overlaps`), that day's talk signatures (`headcount`);
- a roster: a worker's name, a sign-in (`last_sign_in_on`);
- an appointment list: a worker's name;
- a diary day: that day's talks, signatures, incidents and sign-ins;
- your notification list: any record a notification of yours is about
  (`subject_state`).

`TestSyncDepsCoverWhatGETsRead` holds this to what each GET actually reads.

**As of `server_time`:** these fields can change without any record the item
shows changing. Work them out on the phone from the stored times, or treat
them as "as of `server_time`":

- deadline and action states (`overdue`): the clock;
- `absent` on a sign-in day: who was expected (the roster as it stood, and
  each worker's state that day); today's moves with the clock;
- `not_signed` on a talk still open, and `headcount_suggested_now` on
  today's diary: "now".

The site itself and your role come with the site list and `me`, not with
each item.

**Never in a pull:**

- photo and file bytes, and view links: open them through the logged
  `/view` endpoints, online;
- evidence and file records: the phone's own upload queue is the record.
  `complete` is idempotent and answers the status; a photo is shown in
  the records that name it (id and capture time);
- older incident and site work versions: `GET …?version=` online;
- the history tables, audit packs and exports (server-only).

### Access lost (D5)

- **Taken off a site:** the site drops out of `GET /v1/sync`'s `sites`, and
  its pull answers 404 `"site not found"`. Delete what you pulled for it and
  its cursor. Keep its **unsent** records and show them as "no access": they
  may be legal records. Pushing one answers 404 per record.
- **Role changed:** every cursor resets (above).
- **Site archived:** it stays in `sites` with `status: "archived"` and stays
  readable; stop offering new captures there (most refuse them with 409
  `"site is archived"`).
- **Suspended, removed, or the session ended:** 401: sign out. Keep the
  queue encrypted for that person to sign back in; never show it to someone
  else signing in on the phone.

### The reference bundle

Each entry in `sites` has `regime_code` and `reference_sha256`. When the
sha256 differs from the one you hold, `GET /v1/sync/sites/:siteId/reference`
and keep its `parts` for every site of that regime:

| Part | Is |
|---|---|
| `checklists` | `GET /v1/sites/:siteId/checklists` (catalogue and severity scale) |
| `toolbox_topics` | `GET /v1/sites/:siteId/toolbox-topics` |
| `document_types` | `GET /v1/regimes/:code/types` |
| `incident_types` | the regime's incident taxonomy (no other endpoint serves it) |
| `vocabulary` | the regime's terms |
| `site_work_facts` | `GET /v1/construction/sites/:siteId/site-work-facts` (construction sites) |

## Push

`POST /v1/sync/push` with `PushRequest`. Each record is a request to one
endpoint, as the phone would have sent it online: `method`, `path` (the
endpoint's path, no query) and `body`. `ref` is yours, echoed in the result.

- **In order, one at a time.** Put a record before what depends on it: the
  worker before their sign-in, version 1 before version 2, an action before
  its close-out, a photo's declaration before the record naming it.
- **Each in its own transaction.** One record failing doesn't stop the rest.
- **Each result is its endpoint's own**: `status`, `body`, and
  `retry_after` (seconds) when it sent `Retry-After`. 201 created, 200 the
  identical record again, 409 a conflict, 404 its parent isn't there (yet).
- **A record naming a photo still uploading** gets its endpoint's 409
  `"evidence not verified yet"` (or `"file not verified yet"`). Keep it
  queued; push it again after that photo's `complete`.
- **Declarations** (`PUT …/evidence/:id`, `PUT /v1/files/:id`) answer with a
  fresh 15-minute upload URL while pending. The upload itself and
  `complete` stay one request per file, outside the batch.
- **Stopping early.** The batch has the usual 10-second deadline. It starts
  no record with less than 2 seconds left, and answers 200 with `processed`:
  send `records[processed:]` again. It also stops after a 401 (sign out).
- **Lost response or a timeout:** send the whole batch again. Records already
  applied answer 200 (identical repeats); nothing is applied twice.
- **Limits:** 1–100 records and 1 MiB per batch, all in. A batch over 1 MiB
  is the push's own 400 `"batch too large; at most 1 MiB"` (`BatchProblem`,
  no `index`), not the 413 every other endpoint answers. A record too big to
  share a batch goes to its own endpoint, one request (every endpoint takes a
  body up to 1 MiB; above that, 413 `"request body too large; at most 1 MiB"`).
- **A `BatchProblem` with an `index`** names the one record that can never be
  accepted as it is: mark it failed, show it, and push the rest without it.
- **A NUL character** (`\u0000`) in a record's body is that record's own 400
  `"text must not contain NUL characters"`; the rest of the batch runs. In a
  record's `path` (or any control character) it is the batch's 400 `"route not allowed in a batch"`
  with its `index`: drop that record.
- **Rate limits** (DEPLOY) count records, not batches.

Records may go to these endpoints only (each one's own reference says what it
answers). Everything else is online-only: auth, invites, sites, locations and
members, `complete`, `/view`, exports, and linking signatures to workers.

- Evidence and files: `PUT /v1/sites/:siteId/evidence/:id`, `PUT /v1/files/:id`
- Inspections: `PUT …/inspections/:id`, `PUT …/inspections/:id/items/:itemKey`,
  `POST …/inspections/:id/complete`, `POST …/inspections/:id/abandon`
- Incidents: `PUT …/incidents/:id/versions/:version`,
  `PUT …/incidents/:id/witness-statements/:statementId`
- Deadlines: `PUT …/deadlines/:id/satisfactions/:satisfactionId`,
  `POST …/satisfactions/:satisfactionId/retract`
- Actions: `PUT …/actions/:id`, `PUT …/actions/:id/events/:eventId`,
  `PUT …/actions/:id/closeouts/:closeoutId`, `PUT …/waivers/:id`,
  `POST …/waivers/:id/retract`
- Talks: `PUT …/toolbox-talks/:id`, `PUT …/toolbox-talks/:id/photos/:evidenceId`,
  `PUT …/toolbox-talks/:id/attendance/:attendanceId`,
  `POST …/attendance/:attendanceId/retract`
- Sign-ins: `PUT …/sign-ins/:id`, `POST …/sign-ins/:id/sign-out`,
  `POST …/sign-ins/:id/retract`, `PUT …/sign-in-sheets/:date/:evidenceId`,
  `PUT …/roster/:workerId`, `POST …/roster/:workerId/remove`
- Workers: `PUT /v1/workers/:id`, `PUT …/competencies/:recordId`,
  `PUT …/medicals/:recordId`, and their `…/retract`
- Vault: `PUT /v1/vault/documents/:id`, `PUT …/versions/:version`, and their
  `…/retract`
- Notifications: `POST /v1/notifications/:id/read`
- Construction (`/v1/construction/sites/:siteId/…`): `diary/:date/revisions/:id`,
  `diary/:date/addenda/:id`, `site-work/:id/versions/:version`,
  `appointments/:id` and its `…/retract`, `safety-file/:id`,
  `safety-file/:id/versions/:version` and their `…/retract`

| Status | When |
|---|---|
| 200 | `PushResponse`: the batch ran (each record's own status is in `results`) |
| 400 | `BatchProblem`, and **nothing ran**: `"invalid batch"` (not JSON), `"batch too large; at most 1 MiB"`, `"records must hold 1-100 records"`, `"method must be PUT or POST"`, `"ref must be at most 200 bytes"`, `"route not allowed in a batch"` (`index` names the record) |
| 401, 403 | As for a pull |

### Conflict rules by record

| Record | Rule |
|---|---|
| Every capture (evidence, inspections, incidents, statements, satisfactions, actions, events, close-outs, waivers, signatures, sign-ins, competencies, medicals, documents, appointments, diary revisions and addenda, site work) | Created under your own UUID. Same id, same details: 200. Same id, different details: 409 `"… id already used with different details"`. Evidence is never overwritten |
| Versions (incidents, site work, documents) | Only the next version: otherwise 409 `NotNextVersion` with `latest_version` (documents: no `latest_version`; get the document) |
| Inspection answers | The newest `client_answered_at` wins (409 `StaleAnswer` with `stored` otherwise) |
| Diary | Revisions are kept, never lost; the newest by `client_saved_at` is current and a save not based on it shows in `conflict_revision_ids` (M10) |
| Workers and talks (edited in place) | Send `base_updated_at` (the `updated_at` you last pulled, or `created_at` if it was null). Changed since: 409 `WorkerChanged` / `TalkChanged` with `current`; show both and let the person edit again. Without it the last edit wins |
| Complete, abandon, sign-out, retractions, roster removal, notification read | A repeat answers 200 with what is stored |
| Sites, locations, members | Online only; last write wins |

Deadlines are worked out on the server: pulled, never pushed. Only a
satisfaction or its retraction is pushed.

## Signed in for a whole shift

The token pair (`TokenPair`, from signup, login, refresh and invite accept):

- the access token lasts 15 minutes; offline it doesn't matter, the phone
  refreshes when it reconnects;
- the session lasts 30 days from login and **refreshing never extends it**.
  `refresh_expires_at` says when it ends: before going offline for a shift,
  if less than 24 hours remain, ask the person to sign in again.

**A lost refresh response** (the refresh worked but the answer never
arrived): send the same refresh token again. Within 5 minutes, while the new
refresh token hasn't been used, you get **the same pair** back. Two refreshes
sent at once also both get the same pair. After 5 minutes, or once the new
token was used, presenting the old one again is reuse: the whole session is
revoked (401 `"session revoked"`), and the person signs in again; the queue
is kept.

**Accepted risk (recorded at the SYNC review):** someone who has stolen a
refresh token and presents it within those 5 minutes, before the phone has
used the new token, gets the same pair. The two can then share the session
in lockstep until one of them refreshes and the other presents the older
token, which revokes it. Reuse outside the grace still revokes at once. It is
the trade-off of Auth0's refresh token reuse interval, taken so that a lost
response doesn't sign a supervisor out mid-shift.

Capabilities (`/v1/me`, sent in every org pull as `me`) may be cached
offline: refresh them on reconnect and after any 403; sign out on a 401.

## Clocks

Every capture's own times are checked as before: more than 5 minutes ahead of
the server is 400 `"<field> is in the future; check the device clock"`
(inspection start and completion now too). Pulls and pushes carry
`server_time`: if the phone's clock is more than 5 minutes off, warn before
capturing. A photo's capture time is never refused: its dual timestamps are
the record.

## Types

Times are RFC 3339 strings in UTC. `data` and `body` are the endpoint's own
JSON (see its reference).

<!-- types -->
```ts
export type SyncKind =
  | "location_list" | "inspection" | "finding_list" | "action" | "incident" | "deadline"
  | "toolbox_talk" | "sign_in_day" | "roster" | "worker" | "vault_document" | "notification_list"
  | "diary_day" | "site_work" | "appointment_list" | "safety_file_document";

export interface PullItem {
  kind: SyncKind;
  id: string;
  data: unknown;
}

export interface SyncPull {
  server_time: string;
  reset: boolean;
  has_more: boolean;
  cursor: string;
  items: PullItem[];
}

export interface OrgPull {
  server_time: string;
  reset: boolean;
  has_more: boolean;
  cursor: string;
  items: PullItem[];
  me: unknown;
  sites: SyncSite[];
}

export interface SyncSite {
  site: unknown;
  regime_code: string;
  reference_sha256: string;
}

export interface Reference {
  site_id: string;
  regime_code: string;
  sha256: string;
  parts: Record<string, unknown>;
}

export interface PushRequest {
  records: PushRecord[];
}

export interface PushRecord {
  ref?: string | null;
  method: "PUT" | "POST";
  path: string;
  body: unknown;
}

export interface PushResponse {
  server_time: string;
  processed: number;
  results: PushResult[];
}

export interface PushResult {
  ref: string | null;
  status: number;
  body: unknown;
  retry_after?: number;
}

export interface BatchProblem {
  error: string;
  index: number | null;
}

export interface TokenPair {
  access_token: string;
  refresh_token: string;
  expires_in: number;
  token_type: "Bearer";
  refresh_expires_at: string;
}
```
