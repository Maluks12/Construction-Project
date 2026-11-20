# Worker register API

For the dashboard and the mobile app: the organisation's workers, their
competencies (certificates, licences, training, registrations) and medical
fitness, the links from toolbox signatures to workers, and the regime type
lists. The backend keeps this file true: `TestAPIDoc` (in
`construction_db/internal/core/workers`) fails if the types below stop
matching the Go code, or if an error text the handlers send isn't quoted
here.

All endpoints need `Authorization: Bearer <access token>`. Errors are
`{"error": "<message>"}`; a problem with one field is `FieldProblem`
(`{"error": "...", "field": "..."}`).

Roles: owner, SHE officer and supervisor; others get 403 `"forbidden"`. The
register belongs to the organisation, not a site, so supervisors see all of
it: they sign people in. Editing a worker, retracting a record and linking a
signature by hand are for owners and SHE officers.

## How the register works

- **A worker** is a name, an optional firm and clock number, a trade and
  `active`. There's no ID number, photo or biometrics (POPIA; Rev 4 asks for
  an encrypted ID number, which SiteProof doesn't store).
- **One person, one entry.** Name, firm and clock number together identify a
  worker: case and spacing don't count, exactly as at a toolbox talk. A
  second worker with the same three is 409 `IdentityClash`, naming the one
  already there. Two people with the same name are told apart by firm or
  clock number.
- **Types come from a regime**, which each record names (`regime_code`):
  `GET /v1/regimes` lists the regimes, and `GET /v1/regimes/:code/types`
  gives their competency, appointment and document types, the medical rule
  and the reminder lead times. Each type says whether it has an expiry
  (`required`, `optional`, `none`). An appointment type with `required:
  true` is expected on every site (M12): the compliance summary shows a
  site without a live appointee, with the type's `required_note` saying when
  it may not apply.
- **Competencies** have a type, an issue date, an expiry as the type says,
  an optional number and issuer, and a certificate: a verified `competency`
  file (files.md). A supervisor can capture one on site.
- **Medical certificates of fitness** hold only the outcome (`fit`,
  `fit_with_restrictions`, `unfit`), the examination and expiry dates, an
  optional practice number and the certificate (a `medical` file).
  - They hold no findings, diagnoses or restrictions.
  - Everyone above sees the outcome and dates.
  - The certificate itself opens only for an owner or SHE officer, and every
    view is logged. A supervisor can capture it but not read it back
    (decided 18 Nov: inspectors and principal contractors ask for the
    certificate itself, so it is kept, behind that rule).
  - The regime proposes an expiry (`default_validity_months`, 12 for
    construction); the date on the certificate wins.
- **Nothing is overwritten.**
  - **A renewal** is a new record with `supersedes_id` set to the old one:
    the same worker (and, for competencies, the same type). The old record
    shows `superseded`. A record has at most one renewal; if a renewal is
    retracted, another can take its place.
  - **A mistake** is retracted by an owner or SHE officer, with a reason. It
    stays, marked `retracted`.
- **State:** `retracted`, `superseded`, then from the expiry date in the
  regime's time zone: `expired`, `expiring` (within the regime's first
  reminder lead time, 30 days for construction) or `valid`.
- **Toolbox signatures** (toolbox.md) show `worker_id`:
  - the worker the signature is linked to; or
  - the register worker with the same name, firm and clock number; or
  - null.

  A signature is linked automatically when it is given with the same three
  as a register worker. An owner or SHE officer can link one by hand, or
  unlink it ("different person"). The signature itself never changes.
- **Ids come from the device**: `PUT` with a UUID; an identical repeat
  answers 200.

## Endpoints

### `GET /v1/regimes` — the loaded regimes

`{"regimes": [RegimeSummary]}`.

### `GET /v1/regimes/:code/types` — a regime's types

`RegimeTypes`. 404 `"regime not found"`.

### `GET /v1/workers?active=&q=&limit=` — the register

`{"workers": [Worker]}`, by name. `active` is `true` or `false`; `q` searches
name, firm and clock number; `limit` 1–500, default 200. 400 `"active filter must be true or false"`, `"limit must be 1-500"`.

### `PUT /v1/workers/:workerId` — add or edit a worker

Body: `WorkerRequest` (`active` defaults to true).

| Status | When |
|---|---|
| 201 | Added. `Worker` |
| 200 | Edited, or the same again |
| 400 | `"worker id must be a UUID"`; `FieldProblem` with `"name must be 1-200 characters"`, `"firm must be 1-200 characters"`, `"clock_number must be 1-50 characters"`, `"trade must be 1-100 characters"` |
| 403 | `"only an owner or SHE officer can edit a worker"` |
| 409 | `IdentityClash`: `"a worker with this name, firm and clock number is already on the register"` with `worker_id`; `"id already used with different details"` |

### `GET /v1/workers/:workerId` — a worker with every record

`WorkerDetail`: competencies and medicals, renewals and retractions
included, oldest first, each with `state`. 404 `"worker not found"`.

### `PUT /v1/workers/:workerId/competencies/:recordId` — record a competency

Body: `CompetencyRequest`. Owner, SHE officer, supervisor.

| Status | When |
|---|---|
| 201 | Recorded. `Competency` |
| 200 | The same again |
| 400 | `"record id must be a UUID"`; `FieldProblem` with `"regime_code is not a loaded regime"`, `"type_key is not a competency type of that regime"`, `"number must be 1-100 characters"`, `"issuer must be 1-200 characters"`, `"issued_on must be YYYY-MM-DD"`, `"expires_on must be YYYY-MM-DD, not before the issue or examination date"`, `"expires_on is required for this type"`, `"this type has no expiry; leave expires_on out"`, `"file_id must be a UUID"`, `"supersedes_id must be a UUID"`, `"client_recorded_at is required"`, `"client_recorded_at is in the future; check the device clock"`, `"device_id must be 1-200 bytes"`; `"supersedes_id must be a live record of the same worker (and type)"`; the file's (files.md) |
| 404 | `"worker not found"` |
| 409 | `"id already used with different details"`; `"that record already has a renewal"`; `"file already attached to another record"`; `"file not verified yet"` |

### `PUT /v1/workers/:workerId/medicals/:recordId` — record a medical

Body: `MedicalRequest`. As competencies, with `FieldProblem`s
`"outcome must be fit, fit_with_restrictions or unfit"`, `"examined_on must be YYYY-MM-DD"` and `"practice_number must be 1-30 letters, digits, spaces, dots, slashes or dashes"`. The response is the `Medical`.

### `POST /v1/workers/:workerId/{competencies|medicals}/:recordId/retract` — retract

Body: `RetractRequest`. Owner or SHE officer. 201 retracted, 200 the same
again; 400 `"reason must be 1-500 characters"`; 403 `"only an owner or SHE officer can retract a record"`; 404 `"record not found"`; 409 `"record already retracted with another reason"`.

### `POST /v1/workers/:workerId/{competencies|medicals}/:recordId/view` — open the certificate

`ViewURL` (files.md), valid 60 seconds, after logging the view. A medical
certificate: owner or SHE officer only (403 `"only an owner or SHE officer
can open a medical certificate"`). 404 `"record not found"`.

### `PUT /v1/sites/:siteId/toolbox-talks/:talkId/attendance/:attendanceId/worker` — link a signature

Body: `LinkRequest`. Owner or SHE officer. 201 linked (`Link`), 200 already
linked to that worker; 403 `"only an owner or SHE officer can link or unlink a signature"`; 404 `"site not found"`, `"attendance not found"`, `"worker not
found"`; 409 `"signature already linked to a worker; unlink it first"`.

### `POST /v1/sites/:siteId/toolbox-talks/:talkId/attendance/:attendanceId/worker/unlink` — unlink

Body: `UnlinkRequest`. Owner or SHE officer. 200 with the `Link`, unlinked;
400 `"reason must be 1-500 characters"`; 409 `"signature is not linked to a worker"`.

## For ONBOARD's CSV import

The import runs the same checks row by row: `workers.ValidateWorker` and
`ValidateCompetency` (each `FieldProblem` above), then `CheckRows` for names
repeated within the file (`same name, firm and clock number as row N`) or
already on the register. One bad row writes nothing.

## Across the API

- **503 `"server busy, try again"`** with a `Retry-After` header (seconds).
- 401 `"authorization header required"`, `"invalid authorization header
  format"` or `"unauthorized"`.
- 400 with the JSON decoder's own text: a body that isn't valid JSON.
- 500 `"internal error"`.

## Types for `web/src/lib/types.ts`

Times are RFC 3339 strings in UTC; `_on` fields are dates (`YYYY-MM-DD`).
Ids are UUIDs. In requests, a field typed `| null` may also be left out.

<!-- types -->
```ts
export type RecordState = "valid" | "expiring" | "expired" | "superseded" | "retracted";

export type MedicalOutcome = "fit" | "fit_with_restrictions" | "unfit";

export type SignInWarning = "no_valid_medical" | "medical_unfit" | "competency_expired";

export interface WorkerRequest {
  name: string;
  firm?: string | null;
  clock_number?: string | null;
  trade?: string | null;
  active?: boolean | null;
}

export interface Worker {
  id: string;
  name: string;
  firm: string | null;
  clock_number: string | null;
  trade: string | null;
  active: boolean;
  created_by: string;
  created_at: string;
  updated_by: string | null;
  updated_at: string | null;
}

export interface WorkerList {
  workers: Worker[];
}

export interface WorkerDetail {
  id: string;
  name: string;
  firm: string | null;
  clock_number: string | null;
  trade: string | null;
  active: boolean;
  created_by: string;
  created_at: string;
  updated_by: string | null;
  updated_at: string | null;
  competencies: Competency[];
  medicals: Medical[];
}

export interface CompetencyRequest {
  regime_code: string;
  type_key: string;
  number?: string | null;
  issuer?: string | null;
  issued_on: string;
  expires_on?: string | null;
  file_id: string;
  supersedes_id?: string | null;
  client_recorded_at: string;
  device_id?: string | null;
}

export interface MedicalRequest {
  regime_code: string;
  outcome: MedicalOutcome;
  examined_on: string;
  expires_on: string;
  practice_number?: string | null;
  file_id: string;
  supersedes_id?: string | null;
  client_recorded_at: string;
  device_id?: string | null;
}

export interface Competency {
  id: string;
  worker_id: string;
  regime_code: string;
  file_id: string;
  supersedes_id: string | null;
  superseded_by: string | null;
  recorded_by: string;
  client_recorded_at: string;
  recorded_at: string;
  device_id: string | null;
  retraction: Retraction | null;
  state: RecordState;
  type_key: string;
  number: string | null;
  issuer: string | null;
  issued_on: string;
  expires_on: string | null;
}

export interface Medical {
  id: string;
  worker_id: string;
  regime_code: string;
  file_id: string;
  supersedes_id: string | null;
  superseded_by: string | null;
  recorded_by: string;
  client_recorded_at: string;
  recorded_at: string;
  device_id: string | null;
  retraction: Retraction | null;
  state: RecordState;
  outcome: MedicalOutcome;
  examined_on: string;
  expires_on: string;
  practice_number: string | null;
}

export interface Retraction {
  reason: string;
  retracted_by: string;
  retracted_at: string;
}

export interface RetractRequest {
  reason: string;
}

export interface LinkRequest {
  worker_id: string;
}

export interface UnlinkRequest {
  reason: string;
}

export interface Link {
  id: string;
  attendance_id: string;
  worker_id: string;
  method: "auto" | "manual";
  linked_by: string;
  linked_at: string;
  unlinked_by: string | null;
  unlinked_at: string | null;
  unlink_reason: string | null;
}

export interface RegimeSummary {
  code: string;
  name: string;
  edition: string;
  jurisdiction: string;
}

export interface RegimeList {
  regimes: RegimeSummary[];
}

export interface CompetencyType {
  key: string;
  label: string;
  kind: "certificate" | "licence" | "training" | "registration";
  expiry: "required" | "optional" | "none";
  source_ref: string;
}

export interface AppointmentType {
  key: string;
  label: string;
  expiry: "required" | "optional" | "none";
  required: boolean;
  required_note: string;
  source_ref: string;
}

export interface MedicalRule {
  default_validity_months: number;
  source_ref: string;
}

export interface DocumentType {
  key: string;
  label: string;
  scope: "org" | "site";
  required: boolean;
  expiry: "required" | "optional" | "none";
  issuers: string[];
  source_ref: string;
}

export interface Reminders {
  worker_expiry_days: number[];
  company_document_days: number[];
  deadline_days_before: number[];
  action_escalation_days: number[];
  verification_stalled_days: number;
  roster_lapse_days: number;
  source_ref: string;
}

export interface RegimeTypes {
  regime_code: string;
  competency_types: CompetencyType[];
  appointment_types: AppointmentType[];
  medical: MedicalRule;
  document_types: DocumentType[];
  reminders: Reminders;
}

export interface FieldProblem {
  error: string;
  field: string;
}

export interface IdentityClash {
  error: string;
  worker_id: string;
}
```
