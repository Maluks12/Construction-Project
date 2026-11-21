# Document files API

For the mobile app and the dashboard: uploading the documents that M11's
records attach (certificates, medical certificates, appointment letters,
H&S file and vault documents). The backend keeps this file true:
`TestAPIDoc` (in `construction_db/internal/core/files`) fails if the types
below stop matching the Go code, or if an error text the handlers send isn't
quoted here.

All endpoints need `Authorization: Bearer <access token>`. Errors are
`{"error": "<message>"}`.

## How files work

- **The same seal as evidence photos, without the chain** (M11 D4). The
  phone or browser:
  1. chooses a file id (a UUID v4);
  2. declares the file's SHA-256, size and type (`PUT /v1/files/:fileId`);
  3. sends the bytes to the `upload` URL with every header in
     `upload.headers`;
  4. calls `complete`.

  The server re-reads the exact stored version and checks its size, hash
  and type. If they match, the file is `verified` and never changes again,
  under the bucket's Object Lock. If not, it is `rejected` for good: declare
  a new file id.
- **Types:** `application/pdf`, `image/jpeg`, `image/png`, up to 20 MiB
  (20971520 bytes). A PDF must start with `%PDF-`; an image must decode as the
  declared type. Unlike evidence photos, images have no size limit in pixels.
- **Purpose** says what the file may be attached to. A record refuses a file
  of another purpose, or of another site:
  - `competency` — a certificate (workers.md);
  - `medical` — a medical certificate of fitness (workers.md);
  - `appointment` — an appointment letter (needs `site_id`; appointments.md);
  - `site_document` — an H&S file document (needs `site_id`; documents.md);
  - `org_document` — a vault document (documents.md).
- **Who uploads:** owners and SHE officers any purpose; supervisors
  `competency` and `medical` only (they capture certificates on site). Other
  roles get 403 `"forbidden"`.
- **Opening a file** always goes through its record's `view` endpoint, which
  checks who may see the record, logs the view, and returns a URL valid for
  60 seconds. A medical certificate opens only for an owner or SHE officer,
  whatever record it is reached through.
- **Audit packs** (M12, `exports.md`) hold the files of the records they
  include. Each download of a pack logs an `export` row per file it holds,
  naming the export.

## Endpoints

### `PUT /v1/files/:fileId` — declare a file

Body: `DeclareRequest`. The response is the `File`; while it is pending it
carries `upload` (valid 15 minutes; repeat the declaration for a fresh one).

| Status | When |
|---|---|
| 201 | Declared. `File` with `upload` |
| 200 | The same declaration again: the `File` (with a fresh `upload` while pending) |
| 400 | `"file id must be a UUID"`, `"purpose must be competency, medical, appointment, site_document or org_document"`, `"site_id is required for appointment and site_document files, and only for them"`, `"content_sha256 must be 64 hex characters"`, `"size_bytes must be 1-20971520"`, `"content_type must be application/pdf, image/jpeg or image/png"`, `"file_name must be 1-200 characters"`, `"device_id must be 1-200 bytes"`; `"file was rejected; declare a new file id"` |
| 403 | `"only an owner or SHE officer can upload this kind of file"` |
| 404 | `"site not found"` |
| 409 | `"file id already used with different details"` |

### `POST /v1/files/:fileId/complete` — check the stored bytes

Only the uploader (anyone else: 404 `"file not found"`).

| Status | When |
|---|---|
| 201 | Verified now. `File` |
| 200 | Already verified |
| 400 | The stored bytes failed: `{"error": "stored file failed the checks; declare a new file id", "rejected_reason": "size_mismatch" \| "hash_mismatch" \| "wrong_type"}` (`Rejected`), and the file is rejected for good |
| 404 | `"file not found"` |
| 409 | `"file not uploaded yet"` |

### `GET /v1/files/:fileId` — a file's record

The uploader, an owner or a SHE officer. The `File` (never the bytes). 404
`"file not found"`.

### When a record attaches a file

The record's endpoint answers, with these texts:
- 400 `"file not found"` (the id isn't a file of the organisation), `"file was uploaded for another purpose"` (or another site), `"file was rejected;
  declare a new file id"`;
- 409 `"file not verified yet"` (complete it first, then send the record
  again).

Opening a medical certificate as anyone but an owner or SHE officer is 403
`"only an owner or SHE officer can open a medical certificate"`.

## Across the API

- **503 `"server busy, try again"`** with a `Retry-After` header (seconds):
  the request ran out of time, or waited over 5 seconds for a record another
  request was changing. Retry after that many seconds.
- 401 `"authorization header required"`, `"invalid authorization header
  format"` or `"unauthorized"`.
- 400 with the JSON decoder's own text: a body that isn't valid JSON.
- 500 `"internal error"`.

## Types for `web/src/lib/types.ts`

Times are RFC 3339 strings in UTC. Ids are UUIDs.

<!-- types -->
```ts
export type FilePurpose = "competency" | "medical" | "appointment" | "site_document" | "org_document";

export type FileContentType = "application/pdf" | "image/jpeg" | "image/png";

export type FileRejectedReason = "size_mismatch" | "hash_mismatch" | "wrong_type";

export interface DeclareRequest {
  purpose: FilePurpose;
  site_id?: string | null;
  content_sha256: string;
  size_bytes: number;
  content_type: FileContentType;
  file_name?: string | null;
  device_id?: string | null;
}

export interface Upload {
  method: string;
  url: string;
  headers: Record<string, string>;
  expires_at: string;
}

export interface File {
  id: string;
  purpose: FilePurpose;
  site_id: string | null;
  uploaded_by: string;
  content_sha256: string;
  size_bytes: number;
  content_type: FileContentType;
  file_name: string | null;
  device_id: string | null;
  status: "pending" | "verified" | "rejected";
  rejected_reason: FileRejectedReason | null;
  retain_until: string | null;
  created_at: string;
  upload?: Upload;
}

export interface Rejected {
  error: string;
  rejected_reason: FileRejectedReason;
}

export interface ViewURL {
  method: string;
  url: string;
  expires_at: string;
}
```
