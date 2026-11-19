# Documents API: the vault and the H&S file

For the dashboard (and the mobile app, reading the H&S file on site):
- **the organisation's statutory vault:** Letter of Good Standing, COIDA
  registration, CIDB, tax and the other organisation document types of a
  regime;
- **each site's health and safety file:** CR 2014 reg 7(1)(b).

Both are versioned registers of sealed files. The backend keeps this file
true: `TestAPIDoc` (in `construction_db/internal/core/documents`) fails if
the types below stop matching the Go code, or if an error text the handlers
send isn't quoted here.

All endpoints need `Authorization: Bearer <access token>`. Errors are
`{"error": "<message>"}`.

| Register | Base path | Who |
|---|---|---|
| Vault | `/v1/vault/documents` | Owners and SHE officers only; others 403 `"forbidden"` |
| H&S file | `/v1/construction/sites/:siteId/safety-file` | Owners and SHE officers add; supervisors read their own sites; others 403. A site under another edition's regime is 404 `"site not found"` |

## How documents work (M11 D19, D20)

- **A document** is a type from the regime and a title. Several per type are
  fine (two risk assessments).
  - **Vault documents** name their regime (`regime_code`); a register
    belongs to the organisation, not a site.
  - **H&S file documents** take their site's regime: leave `regime_code`
    out.
- **Versions** are numbered from 1 and nothing is overwritten.
  - The document is created with version 1 (`PUT …/:documentId`).
  - A renewal or revision is the next number (`PUT …/:documentId/versions/2`
    and so on), with a file of its own.
  - Each version has an optional issuer, reference, issue date and expiry
    date (`expires_on` as the type says: `required`, `optional`, `none`).
  - The current version is the highest. Earlier ones stay readable and show
    `superseded`.
- **Files:** a vault version takes a verified `org_document` file; an H&S
  file version a `site_document` file of that site (files.md).
  - Each version has its own server-checked SHA-256 under Object Lock; it is
    not in the evidence chain.
  - Opening a version (`…/view`) is logged and returns a URL valid 60
    seconds.
- **Issuers.** A type that lists issuers takes only those. For a Letter of
  Good Standing they are `FEM`, `Compensation Fund` and `RMA` (Rev 4 §2.8).
- **State** of the current version: `expired`, `expiring` (within the first
  reminder lead time: 60 days for the vault, 30 for the H&S file) or
  `valid`.
- **Readiness** (Rev 4 "inspection readiness"): for each of the register's
  types, `present` (a current version not expired), `expired`, or `missing`.
  `required` marks the types every register must have.
- **Retraction** (M11 review): a mistake is retracted, never deleted, by an
  owner or SHE officer with a reason.
  - **A whole document:** it stays in the register, marked `retraction`.
    Readiness and reminders ignore it, and no version can be added to it.
  - **The current version:** the previous version is current again. Never
    the only standing version: retract the document instead.
  - **Numbers keep counting:** after a retracted version 3, the next upload
    is version 4.
- **Ids and numbers come from the device:** an identical repeat answers 200;
  the same number with different details is 409.

## Endpoints (the same under both base paths)

### `GET {base}?regime_code=` — the register

`Register`: every document with its current version, and readiness for the
regime's types. The vault takes `regime_code` (it may be left out while one
regime is loaded); the H&S file uses the site's. 400 `"regime_code is required for a vault document and must be a loaded regime"`.

### `PUT {base}/:documentId` — create a document with version 1

Body: `CreateRequest`. Owner or SHE officer.

| Status | When |
|---|---|
| 201 | Created. `Document` |
| 200 | The same again |
| 400 | `"document id must be a UUID"`, `"regime_code is required for a vault document and must be a loaded regime"`, `"a safety file document takes its site's regime; leave regime_code out"`, `"type_key is not a document type of this regime and scope"`, `"title must be 1-200 characters"`, and the version's texts below; the file's (files.md) |
| 403 | `"only an owner or SHE officer can add documents"` |
| 404 | `"site not found"` |
| 409 | `"document id already used with different details"`; `"file already attached to another document version"`; `"file not verified yet"` |

### `PUT {base}/:documentId/versions/:version` — the next version

Body: `VersionRequest`. Owner or SHE officer. `version` is 2 or more and
must be the next number.

| Status | When |
|---|---|
| 201 | Added. `Document` |
| 200 | That version again, the same |
| 400 | `"version must be a whole number from 2"`, `"file_id must be a UUID"`, `"issuer must be 1-200 characters, and one of the type's issuers when it lists them"`, `"reference must be 1-200 characters"`, `"issued_on and expires_on must be YYYY-MM-DD, expiry not before issue"`, `"expires_on is required for this type"`, `"this type has no expiry; leave expires_on out"`, `"client_uploaded_at is required"`, `"client_uploaded_at is in the future; check the device clock"`, `"device_id must be 1-200 bytes"`; the file's |
| 403 | `"only an owner or SHE officer can add documents"` |
| 404 | `"document not found"`, `"regime not found"` |
| 409 | `"that version already exists with different details"`; `"version is not the next version of this document"`; `"document was retracted"`; `"file already attached to another document version"`; `"file not verified yet"` |

### `POST {base}/:documentId/retract` — retract a document

Body: `RetractRequest`. Owner or SHE officer. 201 retracted (`Document`),
200 the same again; 400 `"reason must be 1-500 characters"`; 403 `"only an owner or SHE officer can retract documents"`; 404 `"document not found"`; 409 `"already retracted with another reason"`.

### `POST {base}/:documentId/versions/:version/retract` — retract the current version

Body: `RetractRequest`. Owner or SHE officer. 201 retracted (`Document`,
with the previous version current), 200 the same again; 400, 403 as above;
404 `"document not found"`, `"version not found"`; 409 `"only the current version can be retracted"`, `"the only standing version can't be retracted; retract the document instead"`, `"document was retracted"`, `"already retracted with another reason"`.

### `GET {base}/:documentId` — a document with every version

`Document`, versions oldest first. 404 `"document not found"`.

### `POST {base}/:documentId/versions/:version/view` — open a version

`ViewURL` (files.md). 404 `"document not found"`, `"version not found"`.

## Not built (noted)

- **BILL** will gate the vault per plan: one middleware on the vault's route
  group.
- **PC** (December) will let a principal contractor read a linked
  subcontractor's vault: one more row-level security policy.

## Across the API

- **503 `"server busy, try again"`** with a `Retry-After` header (seconds).
- 401 `"authorization header required"`, `"invalid authorization header
  format"` or `"unauthorized"`.
- 400 with the JSON decoder's own text: a body that isn't valid JSON.
- 500 `"internal error"`.

## Types for `web/src/lib/types.ts`

Times are RFC 3339 strings in UTC; `_on` fields are dates. Ids are UUIDs. In
requests, a field typed `| null` may also be left out.

<!-- types -->
```ts
export type DocumentScope = "org" | "site";

export type VersionState = "valid" | "expiring" | "expired";

export type ReadinessStatus = "present" | "missing" | "expired";

export interface CreateRequest {
  regime_code?: string | null;
  type_key: string;
  title: string;
  version: VersionRequest;
}

export interface VersionRequest {
  file_id: string;
  issuer?: string | null;
  reference?: string | null;
  issued_on?: string | null;
  expires_on?: string | null;
  client_uploaded_at: string;
  device_id?: string | null;
}

export interface DocumentSummary {
  id: string;
  scope: DocumentScope;
  site_id: string | null;
  regime_code: string;
  type_key: string;
  title: string;
  created_by: string;
  created_at: string;
  latest_version: number;
  current: Version;
  retraction: Retraction | null;
}

export interface Document {
  id: string;
  scope: DocumentScope;
  site_id: string | null;
  regime_code: string;
  type_key: string;
  title: string;
  created_by: string;
  created_at: string;
  latest_version: number;
  current: Version;
  retraction: Retraction | null;
  versions: Version[];
}

export interface Version {
  version: number;
  file_id: string;
  issuer: string | null;
  reference: string | null;
  issued_on: string | null;
  expires_on: string | null;
  uploaded_by: string;
  client_uploaded_at: string;
  received_at: string;
  device_id: string | null;
  retraction: Retraction | null;
  // VersionState for the current version; "superseded" or "retracted" for the others.
  state: string;
}

export interface Retraction {
  reason: string;
  retracted_by: string;
  retracted_at: string;
}

export interface RetractRequest {
  reason: string;
}

export interface Readiness {
  type_key: string;
  label: string;
  required: boolean;
  status: ReadinessStatus;
  document_ids: string[];
}

export interface Register {
  regime_code: string;
  documents: DocumentSummary[];
  readiness: Readiness[];
}
```
