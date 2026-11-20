# Audit pack exports API

For the dashboard: export one site's records over a period as an audit pack
for an inspector, a principal contractor or a court, and check a copy
someone holds later. The backend keeps this file true: `TestAPIDoc` (in
`construction_db/internal/core/exports`) fails if the types below stop
matching the Go code, or if an error text the handlers send isn't quoted
here.

All endpoints need `Authorization: Bearer <access token>`. **Owners and SHE
officers only** (M12 D9; Rev 4 §2.12 gives packs to the SHE officer role);
supervisors and every other role get 403 `"forbidden"`. Errors are
`{"error": "<message>"}`.

## How it works

1. **Queue** the export: `PUT /v1/sites/:id/exports/:exportId` with the
   period. The server records it with `as_of`, the moment it was queued.
   The pack holds **what the server had received by `as_of`**: a record that
   arrives later from an offline phone is not in it, even if it happened in
   the period.
2. The worker **builds** it, two minutes or more after `as_of` (so every
   change made before `as_of` has been saved). Poll `GET …/:exportId` until
   `status` is `ready`. A small site takes seconds; a busy site-year
   minutes.
3. **Download** it: `POST …/:exportId/download` gives a link that works for
   5 minutes. Every download is logged: who, when, from where, and an
   `export` row in the access logs for every photo and document file in the
   pack. Ask again for a fresh link; each request is logged again.
4. Packs are kept for **7 days** (`expires_at`), then deleted. The export
   record stays for good, with the pack's `sha256`.

**Checking a copy:** compute the SHA-256 of a ZIP someone holds (`sha256sum
pack.zip`, `certutil -hashfile pack.zip SHA256`) and compare it with the
export's `sha256`. Equal means not one byte changed. The pack's
`README.txt` says the same for its reader.

**The same data gives the same bytes.** A pack is a function of its export
record and the records received by `as_of`: names, roles and site details
are as they were then, not as they are now. Rebuilding an export (a crash
retry) gives the same `sha256`.

## What the pack holds (M12 D5–D8)

A ZIP (entries stored, not compressed):

- `CHAIN.md`: how the evidence chain is computed, to the byte, with test
  vectors and the SQL check: the same specification SiteProof's own tests
  check, so anyone can recompute the chain from the manifest alone.
- `README.txt`: what the pack is and how to check it.
- `index.html`: every record, readable in a browser with nothing else, in
  the order an inspector asks for (Rev 3 §2.3.10): statutory documents,
  appointments, the H&S file, workers with competencies and medicals,
  toolbox talks, inspections, corrective actions and waivers, the incident
  register with its deadlines, site diaries, the sign-in register, the
  evidence chain.
- `manifest.json`: the same as data, every file's SHA-256, and the site's
  evidence chain: every entry received by `as_of` with every input of its
  hash (`CHAIN.md`), the verification result, the head hash and length.
- `files/evidence/<id>.jpg|png`: the original photos received in the
  period, and those attached to a record in the pack.
- `files/documents/<id>.pdf|jpg|png`: document files: the H&S file and
  vault versions, appointment letters, competency certificates and medical
  certificates of fitness.

Each original is re-hashed against the SHA-256 sealed when it was received
as it is written into the pack. If one no longer matches, it is an
**integrity alarm**: the export fails at once and for good
(`integrity_alarm: true`, `last_error` names the photo or file), nothing is
recorded, and owners and SHE officers get an `integrity_alarm` notification
(notifications.md). It is never retried: the stored bytes won't change.

- **Inspections** in progress at `as_of` appear as their header only
  (`status: "in_progress_at_cut_off"`): their answers could still change.
  Abandoned ones are included and marked, with who, when and why.
- **Medical certificates and injury details** (`incident_people`: injured,
  injury, body part, treatment, died at) are included because only owners
  and SHE officers export (`includes_health: true`); the pack says so. This
  follows the decision on who may see them (BUILD_PLAN DEPLOY); if that
  narrows, the pack follows.

## Endpoints

### `PUT /v1/sites/:id/exports/:exportId`

Queue an export under an id the dashboard chose (UUID v4), with
`ExportRequest`. 201 with the `Export` (`status: "queued"`). The same
request again is 200 with the export as it stands now. Works on archived
sites.

- 400 `"export id must be a UUID"`;
  `"from and to must be YYYY-MM-DD, to not before from, at most 366 days"`
  (local dates in the site's `time_zone`);
  `"client_requested_at is required, and not in the future"`;
  `"device_id must be 1-200 bytes"`; `"invalid request body"`.
- 404 `"site not found"`.
- 409 `"export id already used with different details"`.

### `GET /v1/sites/:id/exports`

`{"exports": [Export]}`, newest first. 404 `"site not found"`.

### `GET /v1/sites/:id/exports/:exportId`

The `Export` with its `status`. 404 `"export not found"`.

- `queued`: waiting for the worker (at least 2 minutes after `as_of`).
- `building`: a worker holds it.
- `ready`: `sha256`, `size_bytes`, the counts and the chain's
  `chain_length`, `chain_head` and `chain_intact` are set.
- `failed`: three attempts failed (`last_error` says why), or the pack was
  refused for good: an integrity alarm, or larger than one object can hold
  (5 TiB; `last_error` says to export a shorter period). Queue a new
  export. Attempts back off: the second starts at least 1 minute after the
  first ended, the third 5 minutes after the second, so a failing export
  takes about 6 minutes to fail for good.
- `expired`: built, but more than 7 days ago; the pack is gone, the record
  and its `sha256` stay.

`attempts` counts build attempts; `failed_attempts` those that failed or
whose worker stopped.

### `POST /v1/sites/:id/exports/:exportId/download`

200 with `Download`: a URL that serves the ZIP for 5 minutes, with the
`sha256` and `size_bytes` to check it against. Logged (above), and the log
is written before the link is made: if logging fails, there is no link.

- 409 `"export not ready yet"`, `"export failed; request a new export"`,
  `"export expired; request a new export"`.
- 404 `"export not found"`, `"site not found"`.

## Types for `web/src/lib/types.ts`

<!-- types -->
```ts
export type ExportStatus = "queued" | "building" | "ready" | "failed" | "expired";

export interface ExportRequest {
  from: string;
  to: string;
  client_requested_at: string;
  device_id?: string | null;
}

export interface Export {
  id: string;
  site_id: string;
  from: string;
  to: string;
  time_zone: string;
  as_of: string;
  requested_by: string;
  requester_role: string;
  includes_health: boolean;
  pack_format: number;
  regime_code: string;
  regime_version: string;
  regime_data_sha256: string;
  client_requested_at: string;
  device_id: string | null;
  status: ExportStatus;
  attempts: number;
  failed_attempts: number;
  integrity_alarm: boolean;
  last_error: string | null;
  sha256: string | null;
  size_bytes: number | null;
  evidence_count: number | null;
  file_count: number | null;
  chain_length: number | null;
  chain_head: string | null;
  chain_intact: boolean | null;
  finished_at: string | null;
  expires_at: string | null;
}

export interface ExportList {
  exports: Export[];
}

export interface Download {
  method: string;
  url: string;
  expires_at: string;
  sha256: string;
  size_bytes: number;
}
```
