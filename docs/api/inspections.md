# Inspections API

For the dashboard and the mobile app: the checklists a site's regulations
call for, and inspections against them: starting, reading, listing,
answering, completing and abandoning them, and the findings they raise.
The backend keeps this file true: `TestAPIDoc` (in
`construction_db/internal/core/checklists`) fails if the types below stop
matching the Go code, and `TestSeverityDefinitions` fails if the severity
table changes without the code.

All endpoints need `Authorization: Bearer <access token>`. Errors are
`{"error": "<message>"}`.

Roles: owner and SHE officer on any site of their organisation; supervisor
only on sites they've been added to (any other site is 404 `"site not found"`). Other roles get 403 `"forbidden"`.

## Endpoints

### `GET /v1/sites/:siteId/checklists` — the site's checklists

Owner, SHE officer, supervisor. Returns `ChecklistCatalog`: the checklist
templates of the **site's** regime (a site's regime decides its checklists,
never the organisation), in a stable order, and the severity scale for
findings. 404 `"site not found"`.

- Never hardcode a checklist: everything shown to an inspector comes from
  here.
- A template is identified by `key` **and** `version`. Wording only ever
  changes by publishing a new `version`, and every published version stays
  valid, so a stored inspection's version tells you exactly which prompts
  the inspector saw.
- The catalogue lists only the **latest** version of each template. An
  inspection started offline on an older version is still accepted after a
  newer one ships, and is shown with its own prompts.
- `sha256` identifies the exact wording of that version. Keep it with the
  template you display, and send it back unchanged when you start an
  inspection: the server refuses a start whose hash doesn't match the
  version named.
- `response_type` is `"pass_fail_na"` for every item today (pass, fail or
  not applicable). Other types will be added before any template uses them.
- `source_ref` is the regulation the template or item comes from, e.g.
  `"reg 13(2)(b)"`. Show it: it's what makes a checklist defensible.

### `PUT /v1/sites/:siteId/inspections/:inspectionId` — start an inspection

Body: `StartInspectionRequest`. Owner, SHE officer, supervisor. The client
makes up `inspectionId` (a UUID v4) when the inspector starts, so it works
offline and retrying after a lost connection is safe.

Send the `key`, `version` and `sha256` of the template **as the inspector
saw it**, and the device's clock time as `client_started_at`. The server
copies that version's title and items into the inspection, so it reads the
same forever, whatever is published later.

| Status | When |
|---|---|
| 201 | Started. `Inspection` with `status: "in_progress"` and one unanswered item per template item, in order |
| 200 | The same start again (every field identical, same person and site): the stored `Inspection`, unchanged |
| 400 | `"inspection id must be a UUID"`, `"unknown checklist template version"` (no such key or version in the site's regime), `"checklist template does not match"` (the hash isn't that version's), `"corrects_id must be a completed inspection on this site"`, `"a correction must use the same checklist as the inspection it corrects"`, `"inspection already has a correction"`, `"device_id must be 1-200 bytes"` (bytes, not characters), or a validator message for a missing field (`template_key`, `template_version` ≥ 1, `template_sha256`, `client_started_at`) |
| 404 | `"site not found"` |
| 409 | `"inspection id already used with different details"` (any field differs, or another person, site or organisation used it) or `"site is archived"` |

- Any published version is accepted with its own hash, not only the latest.
  An inspection started offline before a newer version shipped still syncs.
- **Archived sites** refuse new inspections. Reading still works, and a
  retry of a start that succeeded before the site was archived is still
  200. Sites with status `completed` accept inspections.
- **Corrections.** A completed inspection never changes. To correct one,
  start a new inspection with `corrects_id` set to it. The rules:
  - the corrected inspection must be completed and on the same site;
  - the correction must use the same checklist (`template_key`); a newer
    version of it is fine;
  - each inspection has at most one correction, and the first one started
    takes the place, even while it's still in progress;
  - a correction can itself be corrected once it's completed, so
    corrections form a chain.
- `client_started_at` is kept to the microsecond. `started_at` is the
  server's clock; show both when they differ noticeably.

### `GET /v1/sites/:siteId/inspections/:inspectionId` — get an inspection

Owner, SHE officer, supervisor. `Inspection` with its `items` in template
order. 404 `"site not found"` or `"inspection not found"` (including a
malformed id or the wrong site). Works on archived sites.

- An item's `result`, `severity`, `comment`, `client_answered_at` and
  `answered_at` are all `null` until it is answered. `evidence` is `[]`
  until a photo is attached.
- An item's `source_ref` is `null` when the template gives none.
- `client_completed_at` and `completed_at` are `null` while `status` is
  `"in_progress"`.

### `PUT /v1/sites/:siteId/inspections/:inspectionId/items/:itemKey` — answer an item

Body: `AnswerRequest`. **Only the inspector who started the inspection.**
Send the item's whole answer: `result`, a `severity` exactly when the
result is `"fail"`, an optional `comment`, the attached photos as
`evidence_ids` (send `[]` for none), and the device's clock time as
`client_answered_at`. Sending it again **replaces** the answer, photos
included. That's how to answer first and attach photos once they're
verified.

| Status | When |
|---|---|
| 200 | Answered. The `InspectionItem`, with its photos |
| 400 | `"severity is required for a fail"`, `"severity is only given for a fail"`, `"severity must be low, medium, high or critical"`, `"comment must be at most 2000 characters"`, `"evidence_ids must be UUIDs"`, `"evidence_ids must not repeat"`, `"at most 20 photos per item"`, `"client_answered_at is in the future; check the device clock"` (more than 5 minutes ahead of the server), a validator message for a missing or unknown field (`result` must be `pass`, `fail` or `na`; `client_answered_at` is required), or an `EvidenceProblem`: `"evidence not found on this site"` (unknown, or another site's) or `"evidence was rejected"` |
| 403 | `"only the inspector who started this inspection can change it"` |
| 404 | `"site not found"`, `"inspection not found"` or `"item not found"` |
| 409 | `"inspection is completed; start a new inspection to correct it"` or `"inspection is abandoned"`; an `EvidenceProblem` `"evidence not verified yet"` (the photo is still uploading: complete it, then answer again); or a `StaleAnswer` (below) |

- **Photos** must be verified evidence on the same site. An
  `EvidenceProblem` names the offending ids in `evidence_ids`.
- **Out-of-order answers.** Phones may send answers late or twice. The
  server keeps the newest by `client_answered_at`, and answers with a
  `StaleAnswer` (409, carrying the `stored` answer so you can show it):
  - `"a newer answer is already stored"`: this answer is older than the
    stored one;
  - `"a different answer is already stored for this time"`: the same
    `client_answered_at` but a different result, severity, comment or
    photo set.

  The exact same answer again is 200 and changes nothing, so a retry is
  safe.
- **Photo capture times.** Each photo shows `client_captured_at` and
  `captured_before_start`, which is true when the photo was taken before
  the inspection started (both by the device's clock). Show it: it's how a
  reviewer spots an old photo reused. Seeing the photo itself goes through
  `POST /v1/sites/:siteId/evidence/:evidenceId/view` (see evidence.md),
  which is logged; these times aren't.
- **Archived sites.** Answering still works on an archived site.

### `POST /v1/sites/:siteId/inspections/:inspectionId/complete` — complete an inspection

Body: `CompleteRequest` (the device's clock time as `client_completed_at`).
**Only the inspector who started the inspection.** Once completed, nothing
about the inspection changes: to correct it, start a new one with
`corrects_id` (above).

| Status | When |
|---|---|
| 201 | Completed. `Inspection` with `status: "completed"` and its `findings`, one per failed item |
| 200 | Already completed (a retry is safe): the stored `Inspection`, unchanged |
| 400 | A validator message (`client_completed_at` is required) |
| 403 | `"only the inspector who started this inspection can change it"` |
| 404 | `"site not found"` or `"inspection not found"` |
| 409 | An `IncompleteInspection`: `"items not answered"` or `"a failed item needs a photo"`, with the `item_keys` to fix in template order; or `"inspection is abandoned"` |

- An inspection is complete when every item has a result and every
  `"fail"` has at least one photo (Rev 4 §2.4: photo mandatory on fail).
  Offline, keep the complete request queued until the photos are uploaded
  and verified, and attached with an answer.
- Each `"fail"` raises one finding with the answer's severity, shown in
  `findings` in template order. Findings never change.
- Archived sites still accept completion.

### `POST /v1/sites/:siteId/inspections/:inspectionId/abandon` — abandon an inspection

Body: `AbandonRequest` with a `reason` (required, 1–500 characters, kept).
For an inspection started by mistake, or whose inspector has left.
Abandoning is final, like completing:
- **the inspection stays:** its answers are kept, it stays in lists and on
  get, marked `status: "abandoned"` with `abandoned_at`, `abandoned_by` and
  `abandon_reason`;
- **no findings:** it raises none;
- **corrections:** if it was a correction, another correction of the same
  inspection can now be started. An abandoned inspection can't itself be
  corrected.

**Who may abandon it:**
- the inspector who started it, an owner or a SHE officer;
- **if any item is answered `"fail"`, only an owner or a SHE officer**, so
  a failed item can't be made to disappear by abandoning instead of
  completing.

| Status | When |
|---|---|
| 201 | Abandoned. The `Inspection`, with its answers |
| 200 | Already abandoned (a retry is safe): the stored `Inspection`, unchanged |
| 400 | `"reason must be 1-500 characters"` (missing, blank or too long) |
| 403 | `"only the inspector who started this inspection, an owner or a SHE officer can abandon it"` or `"an inspection with a failed item can only be abandoned by an owner or a SHE officer"` |
| 404 | `"site not found"` or `"inspection not found"` |
| 409 | `"inspection is completed; start a new inspection to correct it"` |

Answering or completing an abandoned inspection is 409
`"inspection is abandoned"`. Archived sites still accept abandoning.

### `GET /v1/sites/:siteId/findings?severity=&coverage=&limit=` — a site's findings

Owner, SHE officer, supervisor. Newest first, then by inspection and
template order, as `FindingList` (`{"findings": []}` when empty, never
`null`). Each `Finding` carries its inspection's template and the item's
prompt, so it can be shown without fetching the inspection. Works on
archived sites.

- `severity` is optional and must be on the scale above. `limit` is
  1–200, default 50. There is no paging yet.
- 400 `"severity must be low, medium, high or critical"`, `"coverage must be unactioned, actioned, waived or superseded"` or `"limit must be 1-200"`;
  404 `"site not found"`.
- `corrected_by` is the id of the **completed** correction of the
  finding's inspection, or null. A finding with `corrected_by` set is
  superseded: the correction's findings are the current ones. A correction
  still in progress, or abandoned, supersedes nothing, so it leaves
  `corrected_by` null.
- `coverage` says how the finding is addressed (`docs/api/actions.md`):
  `actioned` (an action covers it: `covered_by` lists them, its own and any
  carried from the same item earlier in its correction chain at the same or
  a higher severity), `waived` (`waiver_id`), `superseded` (it has
  `corrected_by`), or `unactioned`. The `coverage` filter takes one of those;
  `coverage=unactioned` is the list of findings still needing an action.

### `GET /v1/sites/:siteId/inspections?status=&limit=` — list a site's inspections

Owner, SHE officer, supervisor. Newest started first, as `InspectionList`
(`{"inspections": []}` when empty, never `null`). Works on archived sites.

- `status` is optional: `in_progress`, `completed` or `abandoned`. Without it, every status is listed.
- `limit` is 1–200, default 50. There is no paging yet (it comes with
  sync).
- Each entry is an `InspectionSummary`: the inspection without its
  `items`, plus `answered_count` (items with a result) and `failed_count`
  (items whose result is `fail`). Get the inspection for its items.

| Status | When |
|---|---|
| 200 | `InspectionList` |
| 400 | `"status must be in_progress, completed or abandoned"` or `"limit must be 1-200"` (also for a `limit` that isn't a number) |
| 404 | `"site not found"` |

## Finding severity

A failed item raises a finding with one of these severities. Show the
**definition next to the choice**, every time: two people only grade
findings the same way if they read the same definitions, and findings are
compared across sites. The list comes from the catalogue's `severities`,
least severe first.

| key | label | definition |
|---|---|---|
| `low` | Low | A minor shortfall unlikely to cause injury (e.g. housekeeping or paperwork); fix at the next routine opportunity. |
| `medium` | Medium | Could cause an injury needing treatment, or breaks a regulation with no immediate danger; fix within days, not weeks. |
| `high` | High | Could cause serious injury or a reportable incident, but not imminently; fix before the affected work continues on the next shift. |
| `critical` | Critical | Imminent danger of death or serious injury; stop the affected work now and keep it stopped until it is made safe. |

The time hints guide the choice of severity. A corrective action's default
due date comes from it too (`docs/api/actions.md`).

## Across the API

- **503 `"server busy, try again"`** with a `Retry-After` header (seconds):
  the request ran out of time. Retry after that many seconds.
- 401 `"authorization header required"`, `"invalid authorization header
  format"` or `"unauthorized"`: missing, malformed, expired or revoked
  token.
- 500 `"internal error"`: a server fault.

## Types for `web/src/lib/types.ts`

Times are RFC 3339 strings in UTC with up to microsecond precision. Hashes
are lowercase hex (64 characters). Ids are UUIDs.

<!-- types -->
```ts
export type Severity = "low" | "medium" | "high" | "critical";

export type Cadence = "per_shift" | "daily" | "weekly" | "per_activity" | "ad_hoc";

export type ResponseType = "pass_fail_na";

export interface ChecklistCatalog {
  regime: RegimeRef;
  checklists: ChecklistTemplate[];
  severities: SeverityLevel[];
}

export interface RegimeRef {
  code: string;
  name: string;
  version: string;
}

export interface ChecklistTemplate {
  key: string;
  version: number;
  sha256: string;
  title: string;
  cadence: Cadence;
  source_ref: string;
  items: ChecklistItem[];
}

export interface ChecklistItem {
  key: string;
  prompt: string;
  response_type: ResponseType;
  source_ref: string;
}

export interface SeverityLevel {
  key: Severity;
  label: string;
  definition: string;
}

export type InspectionStatus = "in_progress" | "completed" | "abandoned";

export type Result = "pass" | "fail" | "na";

export interface StartInspectionRequest {
  template_key: string;
  template_version: number;
  template_sha256: string;
  client_started_at: string;
  device_id?: string | null;
  corrects_id?: string | null;
}

export interface Inspection {
  id: string;
  site_id: string;
  status: InspectionStatus;
  template_key: string;
  template_version: number;
  template_sha256: string;
  template_title: string;
  template_source_ref: string;
  item_count: number;
  started_by: string;
  client_started_at: string;
  started_at: string;
  device_id: string | null;
  corrects_id: string | null;
  client_completed_at: string | null;
  completed_at: string | null;
  abandoned_at: string | null;
  abandoned_by: string | null;
  abandon_reason: string | null;
  items: InspectionItem[];
  findings: InspectionFinding[];
}

export interface InspectionFinding {
  item_key: string;
  severity: Severity;
  raised_at: string;
}

export interface CompleteRequest {
  client_completed_at: string;
}

export interface AbandonRequest {
  reason: string;
}

export interface IncompleteInspection {
  error: string;
  item_keys: string[];
}

export interface Finding {
  inspection_id: string;
  site_id: string;
  item_key: string;
  severity: Severity;
  raised_at: string;
  template_key: string;
  template_version: number;
  template_title: string;
  prompt: string;
  corrected_by: string | null;
  coverage: Coverage;
  covered_by: string[];
  waiver_id: string | null;
}

export type Coverage = "unactioned" | "actioned" | "waived" | "superseded";

export interface FindingList {
  findings: Finding[];
}

export interface InspectionItem {
  key: string;
  position: number;
  prompt: string;
  source_ref: string | null;
  response_type: ResponseType;
  result: Result | null;
  severity: Severity | null;
  comment: string | null;
  client_answered_at: string | null;
  answered_at: string | null;
  evidence: AttachedEvidence[];
}

export interface AttachedEvidence {
  evidence_id: string;
  client_captured_at: string;
  captured_before_start: boolean;
  attached_at: string;
}

export interface AnswerRequest {
  result: Result;
  severity?: Severity | null;
  comment?: string | null;
  evidence_ids: string[];
  client_answered_at: string;
}

export interface EvidenceProblem {
  error: string;
  evidence_ids: string[];
}

export interface StaleAnswer {
  error: string;
  stored: InspectionItem;
}

export interface InspectionSummary {
  id: string;
  site_id: string;
  status: InspectionStatus;
  template_key: string;
  template_version: number;
  template_sha256: string;
  template_title: string;
  template_source_ref: string;
  item_count: number;
  started_by: string;
  client_started_at: string;
  started_at: string;
  device_id: string | null;
  corrects_id: string | null;
  client_completed_at: string | null;
  completed_at: string | null;
  abandoned_at: string | null;
  abandoned_by: string | null;
  abandon_reason: string | null;
  answered_count: number;
  failed_count: number;
}

export interface InspectionList {
  inspections: InspectionSummary[];
}
```
