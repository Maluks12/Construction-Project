# Inspections API

For the dashboard and the mobile app: the checklists a site's regulations
call for, and inspections against them. Starting and reading an inspection
are here; listing, answering, completing and findings are coming in M7.
The backend keeps this file true: `TestAPIDoc` (in
`construction_db/internal/core/checklists`) fails if the types below stop
matching the Go code, and `TestSeverityDefinitions` fails if the severity
table changes without the code.

All endpoints need `Authorization: Bearer <access token>`. Errors are
`{"error": "<message>"}`.

Roles: owner and SHE officer on any site of their organisation; supervisor
only on sites they've been added to (any other site is 404 `"site not
found"`). Other roles get 403 `"forbidden"`.

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
| 400 | `"inspection id must be a UUID"`, `"unknown checklist template version"` (no such key or version in the site's regime), `"checklist template does not match"` (the hash isn't that version's), `"corrects_id must be a completed inspection on this site"`, `"device_id must be 1-200 bytes"` (bytes, not characters), or a validator message for a missing field (`template_key`, `template_version` ≥ 1, `template_sha256`, `client_started_at`) |
| 404 | `"site not found"` |
| 409 | `"inspection id already used with different details"` (any field differs, or another person, site or organisation used it) or `"site is archived"` |

- Any published version is accepted with its own hash, not only the latest.
  An inspection started offline before a newer version shipped still syncs.
- **Archived sites** refuse new inspections. Reading still works, and a
  retry of a start that succeeded before the site was archived is still
  200. Sites with status `completed` accept inspections.
- **Corrections.** A completed inspection never changes. To correct one,
  start a new inspection with `corrects_id` set to it. It must be completed
  and on the same site.
- `client_started_at` is kept to the microsecond. `started_at` is the
  server's clock; show both when they differ noticeably.

### `GET /v1/sites/:siteId/inspections/:inspectionId` — get an inspection

Owner, SHE officer, supervisor. `Inspection` with its `items` in template
order. 404 `"site not found"` or `"inspection not found"` (including a
malformed id or the wrong site). Works on archived sites.

- An item's `result`, `severity`, `comment`, `client_answered_at` and
  `answered_at` are all `null` until it is answered (answering is coming
  in M7).
- An item's `source_ref` is `null` when the template gives none.
- `client_completed_at` and `completed_at` are `null` while `status` is
  `"in_progress"`.

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

The time hints guide the choice of severity; corrective-action due dates
come later (M9).

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

export type InspectionStatus = "in_progress" | "completed";

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
  items: InspectionItem[];
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
}
```
