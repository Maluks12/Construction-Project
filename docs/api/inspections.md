# Inspections API

For the dashboard and the mobile app: the checklists a site's regulations
call for, and (coming in M7) inspections against them and the findings
they raise. The backend keeps this file true: `TestAPIDoc` (in
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
  inspection (coming in M7): the server refuses a start whose hash doesn't
  match the version named.
- `response_type` is `"pass_fail_na"` for every item today (pass, fail or
  not applicable). Other types will be added before any template uses them.
- `source_ref` is the regulation the template or item comes from, e.g.
  `"reg 13(2)(b)"`. Show it: it's what makes a checklist defensible.

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

Hashes are lowercase hex (64 characters).

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
```
