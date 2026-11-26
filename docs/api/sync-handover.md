# SYNC handover for the mobile app and the dashboard (Thu 26 Nov 2026)

## What it is

The phone can now work a whole shift offline:

- **push** everything it captured in one batch, each record through its own
  endpoint;
- **pull** what changed, per site and for the organisation, from a cursor
  that never skips a record;
- **refresh** its tokens safely even when a response is lost.

The reference is `docs/api/sync.md`. Each record's own reference
(`incidents.md`, `toolbox.md`, …) still says what it looks like and what its
endpoint may answer: sync adds no rules of its own.

## Contract changes

- **New endpoints:** `GET /v1/sync`, `GET /v1/sync/sites/:siteId`,
  `GET /v1/sync/sites/:siteId/reference`, `POST /v1/sync/push`.
- **`TokenPair`** gains `refresh_expires_at`, the session's end (signup,
  login, refresh, invite accept).
- **Refresh:** an exact retry of a refresh token within 5 minutes of its
  use, while its successor is unused, now returns the same pair. It used to
  revoke the session (401 `"session revoked"`). Reuse after that still
  revokes.
- **Satisfactions** take an optional `device_id` (request and
  `Satisfaction`); a repeat compares it. New 400 `"device_id must be 1-200 bytes"`.
- **Workers and talks:** `WorkerRequest` and `TalkRequest` take an optional
  `base_updated_at`; a stale one is 409 `WorkerChanged` / `TalkChanged`
  with `current`. Without it, nothing changes.
- **Roster removal:** a repeat is 200 with the removal (was 404); another
  reason is 409 `"worker already removed from the roster with another reason"`.
- **Inspections:** start and complete refuse a time more than 5 minutes
  ahead: 400 `"client_started_at is in the future; check the device clock"`,
  `"client_completed_at is in the future; check the device clock"`.
- **Findings:** `GET …/findings` takes `inspection_id`.

### Changed in the review (Thu 26 Nov)

- **Cursors are per organisation.** Someone in two organisations keeps
  separate cursors for each; one from the other organisation resets
  (`reset: true`, a full pass) instead of skipping records.
- **A NUL in one record** is that record's 400 `"text must not contain NUL characters"`
  in `results`; the rest of the batch runs. A control character in a
  record's `path` refuses the batch with that record's `index`: drop it.
- **Every endpoint** refuses a body over 1 MiB with 413
  `"request body too large; at most 1 MiB"`. A record too big to share a
  batch goes to its own endpoint.
- **More items come back when something they show changes**: a talk when
  that day's sign-ins change; a sign-in day when a worker's records, name or
  other sites' sign-ins, or that day's talk signatures, change; the roster on
  a sign-in; the diary on its incidents and signatures; the notification list
  when a subject it is about changes. Still as of `server_time`: `absent`,
  `overdue`, and "now" fields.
- **`/healthz/jobs`** has `oldest_write_seconds`, and `"sync_pull"` among
  `stale` past 5 minutes (for the uptime monitor, not the app).
- **The refresh grace's accepted risk** is written down in `sync.md`.

## Types to add

`web/src/lib/types.ts` and `mobile/src/lib/types.ts` (both have `TokenPair`):

- `TokenPair.refresh_expires_at: string`;
- `Satisfaction.device_id: string | null`, `SatisfactionRequest.device_id?`;
- `WorkerRequest.base_updated_at?`, `TalkRequest.base_updated_at?`,
  `WorkerChanged`, `TalkChanged` (`workers.md`, `toolbox.md`).

`mobile/src/lib/types.ts` only: everything in `sync.md`'s types block
(`SyncKind`, `PullItem`, `SyncPull`, `OrgPull`, `SyncSite`, `Reference`,
`PushRequest`, `PushRecord`, `PushResponse`, `PushResult`, `BatchProblem`).

## For the mobile app

### First sync on a phone

1. Sign in. Keep `refresh_expires_at`.
2. `GET /v1/sync` with no cursor, then again with each `cursor` while
   `has_more`. Keep `me` (role, capabilities), `sites`, and the items
   (workers, notifications; the vault for owners and SHE officers). Store the
   last cursor as the org cursor.
3. For each site in `sites`:
   - if you don't hold its `reference_sha256` yet, `GET /v1/sync/sites/:siteId/reference`
     and keep its parts by regime;
   - `GET /v1/sync/sites/:siteId` with no cursor, paging while `has_more`.
     Store the last cursor as that site's cursor.
4. Show progress by pages: a large site takes several.

### During the shift (offline)

- Capture as today: every record under its own UUID, with `device_id` and the
  device's times. Queue the records in the order they were made.
- Photos: queue the declaration (`PUT …/evidence/:id`), the upload and the
  `complete` for each. A record naming a photo goes after its declaration.
- Before going offline, if `refresh_expires_at` is less than 24 hours away,
  ask the person to sign in again.
- If the phone's clock is more than 5 minutes from the last `server_time`,
  warn before capturing: those times would be refused.

### Back online

1. **Refresh** the tokens if the access token has expired (15 minutes). If
   the refresh response is lost, send the same refresh token again (below).
2. **Push** the queue as batches of up to 100 records (and 1 MiB), oldest
   first. For each result:
   - **201 or 200:** done; mark the record sent (keep it 7 days, below).
   - **409 `"evidence not verified yet"` / `"file not verified yet"`:** upload
     and `complete` its photos (one request per file), then push it again.
   - **409, anything else:** a conflict. Show the person both versions
     (`WorkerChanged`/`TalkChanged` carry `current`; a version conflict
     carries `latest_version`). Don't retry blindly.
   - **409 `AlreadySignedIn`:** drop the local copy (as before).
   - **404:** its parent isn't there. Push it again after the rest; if the
     site itself is gone (no longer in `sites`), it's "no access".
   - **400:** the record is wrong. Show it; don't retry.
   - **503:** wait `retry_after` seconds, push it again.
   - **401 for a record:** the batch stops; sign out.
   - **`processed` short of the batch:** send `records[processed:]` again.
   - **The batch itself refused (400 `BatchProblem`):** nothing ran.
     - **With an `index`** (a route not allowed in a batch, a bad method or
       path, a `ref` over 200 bytes): mark that record failed, show it, and
       push the rest without it. It will never be accepted as it is.
     - **Without one:** split the batch. `"batch too large; at most 1 MiB"`
       is the push's own 400, not the 413 other endpoints answer; then
       `"records must hold 1-100 records"`, or `"invalid batch"` (not JSON).
       A single record too big for any batch goes to its own endpoint,
       which takes up to 1 MiB (413 above).
   - **No answer at all (timeout, connection lost):** send the same batch
     again. Records already applied answer 200; nothing is applied twice.
3. **Upload** photos and `complete` each, then push the records that waited.
4. **Pull** `GET /v1/sync` from the org cursor, then each site from its own
   cursor, paging while `has_more`. Replace held items by `kind` and `id`.

### Losing a refresh response

Send the same refresh token again within 5 minutes and you get the same pair
back; two refreshes sent at once also both get it. After 5 minutes, or once
the new token was used, the old one counts as stolen: the session ends
(401). Sign in again; keep the queue.

### Conflicts

See `sync.md`, "Conflict rules by record". In short:

- captures are append-only: a repeat is 200, a different record under the
  same id is 409;
- versions must be the next one;
- the newest inspection answer wins;
- diary revisions are all kept;
- worker and talk edits send `base_updated_at`.

### Losing access

- **A site missing from `sites`, or its pull answering 404:** delete its
  pulled data and cursor. Keep its unsent records, shown as "no access".
- **`reset: true`:** drop that scope's pulled data and take the pages that
  follow as the full set. Then push again the records sent in the last 7
  days: a server restored from a backup may have lost them.
- **A role change** resets every cursor (that's how you learn of it, besides
  `me`).
- **An archived site** stays readable; stop offering captures there.
- **401:** sign out. Keep the queue encrypted for that person only.

### Capabilities

`me` arrives in every org pull. Cache it offline; refresh on reconnect and
after any 403; sign out on a 401 (decided 20 Nov).

## For the dashboard

- Optional: send `base_updated_at` when editing a worker
  (`people/forms.tsx`) or a talk, and show `WorkerChanged`/`TalkChanged`
  when someone else edited first. Without it, nothing changes.
- The satisfaction form may leave `device_id` out.
- Add `refresh_expires_at` to `TokenPair`; nothing else needs it.

## Running it (backend)

- Migrations 000049–000052 (`docker compose run --rm migrate up`). 000049
  rewrites every synced table once to add `sync_xid`.
- No new environment variables.
- **Restore runbook (G38):** after restoring a backup into the same cluster,
  run `SELECT sync_new_generation();` as the migration owner. Every phone then
  resets. A new cluster or a point-in-time recovery changes the epoch by
  itself.
- `sync_epoch()` reads `pg_control_system()` and `pg_control_checkpoint()` as
  the migration owner (SECURITY DEFINER). On a managed database where that
  role isn't a superuser, it needs `pg_monitor` or the equivalent (DEPLOY).
- `sync_oldest_write_age()` (000052, the pull lag on `/healthz/jobs`) reads
  every session's `backend_xid` and `xact_start` in `pg_stat_activity`, also
  as the migration owner. Without a superuser owner it needs
  `pg_read_all_stats` (part of `pg_monitor`): otherwise other sessions' rows
  hide those columns, it sees no open write, and the check always passes
  (DEPLOY).

## Open, not in SYNC

- `include_health` (REG4 review R3) isn't built: still awaiting confirmation,
  its own PR.
- Who may see injury details (DEPLOY): a pull shows exactly what the GET
  shows, so deciding it for the GET decides it for sync.
- Resumable multipart uploads (G26): one presigned PUT per file, as before.
- The phone's storage budget, Wi-Fi preference and per-record sync status
  are the app's (A.3).
