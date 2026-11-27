# Onboarding API: bulk documents and the set-up wizard

For the dashboard's first-run set-up (ONBOARD): loading workers'
certificates and medicals and the company documents in bulk, and the
wizard's progress. The backend keeps this file true: `TestAPIDoc` (in
`construction_db/internal/core/onboarding`) fails if the types below stop
matching the Go code, or if an error text these endpoints send isn't quoted
here.

All endpoints need `Authorization: Bearer <access token>` from an **owner or
SHE officer**; anyone else gets 403 `"forbidden"`. Errors are
`{"error": "<message>"}`. Both are online only (not in a sync push).

The rest of onboarding lives with its records: zones and floors in
`locations.md`, the worker register from a spreadsheet in `workers.md`
(`PUT /v1/worker-imports/:importId`), team members in `members.md`, invites
at `POST /v1/invites`.

## Loading documents in bulk

The flow, for (say) 40 certificates and a company's vault:

1. **Choose ids.** A UUID v4 per file, and one per record (each competency,
   medical or vault document).
2. **Declare the files** in one request: `POST /v1/files/batch`
   (`files.md`), purpose `competency`, `medical` or `org_document`. Each
   answer carries its `upload`.
3. **Upload each file** to its `upload` URL, then `complete` it
   (`POST /v1/files/:fileId/complete`). Do these in parallel, a few at a
   time. A file whose upload failed is still pending: declare it again for a
   fresh URL and upload it again. A `rejected` file needs a new file id (and
   its record a new `file_id`).
4. **Save the records** in one request: `POST /v1/onboarding/documents`.
   Every record names a verified file. **All or nothing:** if any item has a
   problem, nothing is saved and the answer lists every item's problem; fix
   them and send the whole batch again. Items already saved under their id
   (a repeat after a lost answer) count as `repeated`, not as problems.

Each item is checked exactly as its own endpoint checks it, with the same
texts: `PUT /v1/workers/:workerId/competencies/:recordId` and
`.../medicals/:recordId` (`workers.md`), `PUT /v1/vault/documents/:documentId`
(`documents.md`). Expiry dates go in each record (`expires_on`); a
type that needs one refuses an item without it.

### `POST /v1/onboarding/documents?dry_run=` — save many records, all or nothing

Body: `DocumentsRequest`, 1–100 items. Each item is one of:

- `kind: "competency"`: `worker_id` and `competency` (a `CompetencyRequest`);
- `kind: "medical"`: `worker_id` and `medical` (a `MedicalRequest`);
- `kind: "vault_document"`: `document` (a vault `CreateRequest`, with its
  first version).

`id` is the record's id. `dry_run=true` checks every item and saves
nothing.

| Status | When |
|---|---|
| 201 | Saved: `DocumentsResult` (`created` new, `repeated` already saved under their ids) |
| 200 | Nothing new: every item was already saved under its id, or a dry run (`problems` may list some) |
| 400 | `DocumentsResult` with `error` `"some documents have problems; nothing was saved"` and every item's problem in `problems` |
| 400 | `"items must hold 1-100 documents"`, `"dry_run must be true or false"`, or the JSON decoder's own text |
| 403 | `"forbidden"` |
| 409 | `DocumentsResult` with `error` `"some files aren't verified yet; complete them, then send the batch again"`: at least one item's file is `"file not verified yet"`; nothing was saved |

An item's own problems besides its endpoint's texts:
`"kind must be competency, medical or vault_document"`,
`"a competency item carries competency and worker_id, a medical item medical and worker_id, a vault_document item document; nothing else"`,
`"this id is used by another item of the batch"`.

## The set-up wizard

### `GET /v1/onboarding/progress` — how far set-up has got

200 `Progress`. Worked out on every call from the records themselves, so it
can't go stale and nothing needs saving or dismissing. `complete` is true
when every **required** step is done; the others are shown with a `note`
saying when they apply.

| `key` | Required | Done when | `count` / `total` |
|---|---|---|---|
| `sites` | yes | the organisation has a site that isn't archived (signing up creates one) | live sites |
| `team` | no | anyone besides the person who signed up is invited or active | those people |
| `company_documents` | yes | every required vault type of the sites' regimes is present and in date (`gaps`: the missing or expired ones) | present / required |
| `workers` | yes | the register has an active worker | active workers |
| `locations` | no | a live site has a zone or floor | live locations |
| `worker_documents` | no | every active worker has a competency or medical on record | workers with one / active workers |
| `hs_file` | yes (construction sites) | every live site's required H&S file types are present and in date (`gaps` name the site and type) | present / required, over the sites |
| `required_appointments` | no (construction sites) | every live site has a valid appointee of each required type (`gaps` carry each type's note: reg 8(1) and 8(7) bind the principal contractor) | filled / required, over the sites |
| `site_work` | no (construction sites) | site work is recorded on a live site (regulation 4) | site work records |

"In date" is the site regime's date (Africa/Johannesburg): a document
expiring today is still in date today.

## Across the API

- 400 `"text must not contain NUL characters"`: a NUL character (`\u0000`) anywhere in the JSON body (any value or key, however nested), the query string or the path, on every endpoint, checked before the token (REG4 review). Postgres text can't store one.
- 401 `"authorization header required"`, `"invalid authorization header
  format"` or `"unauthorized"`.
- 413 `"request body too large; at most 1 MiB"`: on every endpoint, checked before the token (SYNC review).
- **503 `"server busy, try again"`** with a `Retry-After` header (seconds):
  the request ran out of time, or waited over 5 seconds for a record another
  request was changing. Retry after that many seconds.
- 500 `"internal error"`.

## Types for `web/src/lib/types.ts`

Ids are UUIDs. `CompetencyRequest` and `MedicalRequest` are in `workers.md`,
`CreateRequest` in `documents.md`. In requests, a
field typed `| null` may also be left out.

<!-- types -->
```ts
export type DocumentKind = "competency" | "medical" | "vault_document";

export type StepKey = "sites" | "team" | "company_documents" | "workers" | "locations" | "worker_documents" | "hs_file" | "required_appointments" | "site_work";

export interface DocumentItem {
  kind: DocumentKind;
  id: string;
  worker_id?: string | null;
  competency?: CompetencyRequest | null;
  medical?: MedicalRequest | null;
  document?: CreateRequest | null;
}

export interface DocumentsRequest {
  items: DocumentItem[];
}

export interface ItemResult {
  index: number;
  kind: DocumentKind;
  id: string;
  created: boolean;
}

export interface ItemProblem {
  index: number;
  field: string | null;
  error: string;
}

export interface DocumentsResult {
  dry_run: boolean;
  created: number;
  repeated: number;
  items: ItemResult[];
  problems: ItemProblem[];
  error?: string;
}

export interface Gap {
  site_id: string | null;
  type_key: string | null;
  label: string;
  note: string | null;
}

export interface Step {
  key: StepKey;
  required: boolean;
  done: boolean;
  count: number;
  total: number | null;
  note: string | null;
  gaps: Gap[];
}

export interface Progress {
  complete: boolean;
  steps: Step[];
}
```
