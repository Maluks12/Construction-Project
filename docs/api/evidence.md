# Evidence API

For the dashboard and the mobile app: how to record evidence (photos), read
it back, and check a site's evidence chain. Built in M6. The backend keeps
this file true: `TestAPIDoc` (in `construction_db/internal/core/evidence`)
fails if the types below stop matching the Go code.

All endpoints need `Authorization: Bearer <access token>`. Errors are
`{"error": "<message>"}` unless stated otherwise.

## Recording evidence: three steps

The photo's bytes never pass through the API. They go straight to storage
(S3; MinIO locally) with a short-lived signed link.

1. **Declare** it: `PUT /v1/sites/:siteId/evidence/:evidenceId`. The client
   makes up `evidenceId` (a UUID v4) at capture time, so retrying after a
   lost connection is safe. The response includes an `upload` block.
2. **Upload** the bytes: send `upload.method` (`PUT`) to `upload.url` with
   every header in `upload.headers`, unchanged, and a body of **exactly**
   `size_bytes` bytes. Storage answers 200. Any other body, a missing or
   changed header, or a second upload to the same link is refused. The link
   expires at `upload.expires_at` (15 minutes); declaring again with the
   same details returns a fresh one.
3. **Complete** it: `POST /v1/sites/:siteId/evidence/:evidenceId/complete`.
   The server re-checks the stored file and, if it matches, seals it into
   the site's evidence chain (`status` becomes `verified`, `chain` is filled
   in).

If complete rejects the file, that evidence id is used up for good: declare
the photo again under a **new** id.

The client must compress the photo to at most 2048px on its long edge, as a
JPEG or PNG, **before** computing `content_sha256`, and upload exactly
those bytes.

## Endpoints

Roles: owner and SHE officer on any site of their organisation; supervisor
only on sites they've been added to (any other site is 404 `"site not
found"`). Other roles get 403 `"forbidden"`.

### `PUT /v1/sites/:siteId/evidence/:evidenceId` — declare

Body: `DeclareRequest`. Owner, SHE officer, supervisor.

| Status | When |
|---|---|
| 201 | Declared. `Evidence` with `status: "pending"`, `chain: null` and an `upload` block |
| 200 | The same declaration again (every field identical, same uploader and site). Pending: a fresh `upload`. Already verified: no `upload` |
| 400 | `"evidence id must be a UUID"`, `"content_sha256 must be 64 hex characters"`, `"lat and lng must both be set or both be absent"`, `"device_id must be 1-200 bytes"` (bytes, not characters), or a validator message for a missing or out-of-range field (`size_bytes` 1–10485760, `content_type` `image/jpeg` or `image/png`, `lat` −90–90, `lng` −180–180, `client_captured_at` required) |
| 404 | `"site not found"` |
| 409 | `"evidence id already used with different details"` (any field differs, or another site or uploader used it) or `"evidence was rejected; declare a new evidence id"` |

`client_captured_at` is kept to the microsecond.

### `POST /v1/sites/:siteId/evidence/:evidenceId/complete` — complete

No body. Only the person who declared it; anyone else gets 404.

| Status | When |
|---|---|
| 201 | Verified now. `Evidence` with `status: "verified"` and `chain` |
| 200 | Already verified (a retry is safe) |
| 400 | The stored file failed a check. Body: `EvidenceRejection`, e.g. `{"error": "uploaded file does not match content_sha256; declare a new evidence id", "rejected_reason": "hash_mismatch"}`. The evidence is now `rejected` |
| 404 | `"site not found"` or `"evidence not found"` |
| 409 | `"file not uploaded yet"` (upload, then complete again) or `"evidence was rejected; declare a new evidence id"` |

The checks, in order, and their `rejected_reason`:

| `rejected_reason` | `error` (each ends `"; declare a new evidence id"`) |
|---|---|
| `size_mismatch` | `uploaded file is not size_bytes long` |
| `hash_mismatch` | `uploaded file does not match content_sha256` |
| `not_an_image` | `uploaded file is not a JPEG or PNG` |
| `wrong_type` | `uploaded file is not the declared content_type` |
| `too_large_dimensions` | `image is larger than 2048px on its long edge` |

Act on `rejected_reason`, not the text.

### `GET /v1/sites/:siteId/evidence?status=&limit=` — list

Owner, SHE officer, supervisor. Newest first, as `EvidenceList`
(`{"evidence": []}` when empty, never `null`). `status` is optional
(`pending`, `verified` or `rejected`); `limit` is 1–200, default 50.
400 `"status must be pending, verified or rejected"` or `"limit must be
1-200"`; 404 `"site not found"`. There is no paging yet.

### `GET /v1/sites/:siteId/evidence/:evidenceId` — get

Owner, SHE officer, supervisor. `Evidence`, without `upload`. 404 `"site
not found"` or `"evidence not found"` (including a malformed id or the
wrong site).

### `GET /v1/sites/:siteId/chain` — check a site's chain

**Owner and SHE officer only.** Walks the site's evidence chain as stored
and returns `ChainVerification`. 404 `"site not found"`.

- `intact: true` means **no break was found**. Never present it as
  "tamper-proof" or "court-admissible".
- `first_break` is the first problem found, with `reason` (checked in this
  order): `seq_gap`, `prev_mismatch`, `metadata_changed`,
  `chain_hash_mismatch`, `entry_without_verified_evidence`, then
  `head_mismatch`, then `verified_without_entry` (its `seq` is `null`).
- `head_hash` and `length` are worth showing and keeping: someone with
  database access could shorten or rewrite the chain consistently, and only
  a record of these values kept elsewhere shows it.

## Across the API

- **503 `"server busy, try again"`** with a `Retry-After` header (seconds):
  the request ran out of time. Retry after that many seconds. Any endpoint
  can return it.
- 401 `"authorization header required"`, `"invalid authorization header
  format"` or `"unauthorized"`: missing, malformed, expired or revoked
  token.
- 500 `"internal error"`: a server fault.

## Not available yet

- **Viewing or downloading the photo itself.** Every view must be logged
  first (M7, access log); until then the API serves evidence details, not
  bytes.
- Paging through long evidence lists (comes with sync).

## Types for `web/src/lib/types.ts`

Times are RFC 3339 strings in UTC with up to microsecond precision. Hashes
are lowercase hex (64 characters). Ids are UUIDs.

<!-- types -->
```ts
export type EvidenceStatus = "pending" | "verified" | "rejected";

export type RejectedReason =
  | "size_mismatch"
  | "hash_mismatch"
  | "not_an_image"
  | "wrong_type"
  | "too_large_dimensions";

export type ChainBreakReason =
  | "seq_gap"
  | "prev_mismatch"
  | "metadata_changed"
  | "chain_hash_mismatch"
  | "entry_without_verified_evidence"
  | "head_mismatch"
  | "verified_without_entry";

export interface DeclareRequest {
  content_sha256: string;
  size_bytes: number;
  content_type: "image/jpeg" | "image/png";
  client_captured_at: string;
  lat?: number | null;
  lng?: number | null;
  device_id?: string | null;
}

export interface Evidence {
  id: string;
  site_id: string;
  status: EvidenceStatus;
  rejected_reason: RejectedReason | null;
  content_sha256: string;
  size_bytes: number;
  content_type: "image/jpeg" | "image/png";
  client_captured_at: string;
  lat: number | null;
  lng: number | null;
  device_id: string | null;
  uploaded_by: string;
  created_at: string;
  chain: ChainEntry | null;
  upload?: Upload;
}

export interface ChainEntry {
  seq: number;
  server_received_at: string;
  prev_chain_hash: string;
  metadata_hash: string;
  chain_hash: string;
}

export interface Upload {
  method: "PUT";
  url: string;
  headers: Record<string, string>;
  expires_at: string;
}

export interface EvidenceList {
  evidence: Evidence[];
}

export interface EvidenceRejection {
  error: string;
  rejected_reason: RejectedReason;
}

export interface ChainVerification {
  site_id: string;
  intact: boolean;
  length: number;
  head_seq: number;
  head_hash: string;
  first_break: ChainBreak | null;
  checked_at: string;
}

export interface ChainBreak {
  seq: number | null;
  evidence_id: string;
  reason: ChainBreakReason;
}
```
