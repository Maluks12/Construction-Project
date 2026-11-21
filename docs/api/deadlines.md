# Deadlines API

For the dashboard and the mobile app: a site's statutory deadlines, whatever
started them, and recording that one was met. Two kinds of record start
deadlines (`subject_type`):

- `incident`: reports and investigations after an incident
  (`docs/api/incidents.md`);
- `site_work`: notifications and permit applications before construction work
  starts (REG4, `docs/api/site-work.md`).

These endpoints were in the incidents reference until REG4; the paths, bodies
and error texts are unchanged. The backend keeps this file true: `TestAPIDoc`
(in `construction_db/internal/core/deadlines`) fails if the types below stop
matching the Go code, or if an error text the handlers send isn't quoted here.

All endpoints need `Authorization: Bearer <access token>`. Errors are
`{"error": "<message>"}`.

Roles: owner and SHE officer on any site of their organisation; supervisor
only on sites they've been added to (any other site is 404 `"site not
found"`). Other roles get 403 `"forbidden"`.

## Endpoints

### `GET /v1/sites/:siteId/deadlines?state=&subject_type=&limit=` — a site's live deadlines

Owner, SHE officer, supervisor. `DeadlineList`: live deadlines only
(superseded and withdrawn rows are on their incident or site work record),
soonest `due_at` first. `state` is one of `open`, `due_now`, `overdue`,
`satisfied`, `satisfied_late`; `subject_type` is `incident` or `site_work`;
`limit` 1–200, default 50.

| Status | When |
|---|---|
| 200 | `DeadlineList` |
| 400 | `"state must be open, due_now, overdue, satisfied or satisfied_late"`, `"subject_type must be incident or site_work"`, `"limit must be 1-200"` |
| 404 | `"site not found"` |

### `GET /v1/sites/:siteId/deadlines/:deadlineId` — one deadline

Owner, SHE officer, supervisor. `DeadlineDetail`: the row, with every
satisfaction of its duty (retracted ones too), oldest first.

| Status | When |
|---|---|
| 200 | `DeadlineDetail` |
| 404 | `"site not found"`, `"deadline not found"` |

### `PUT /v1/sites/:siteId/deadlines/:deadlineId/satisfactions/:satisfactionId` — record that a deadline was met

Body: `SatisfactionRequest`. Owner, SHE officer, supervisor. The phone makes
up `satisfactionId`. `satisfied_at` is when the call was made, the form sent
or the notice delivered, not when it is recorded.

A satisfaction belongs to the **duty**, not the row: if the deadline is
later recomputed (a corrected `occurred_at`, a moved start date), the
satisfaction carries over and is judged against the new due time. You may
name any row of the duty; it is recorded against the live one, which the
response returns. Recording a call can start a further clock: a death after
the notice needs its own notice (GAR 8(2)). That row has
`added_by_satisfaction_id` set.

- **After-deadlines** (`direction: "after"`, incidents): `satisfied_at` can't
  be before `basis_at` (5 minutes allowed for clock drift).
- **Before-deadlines** (`direction: "before"`, site work): the notice is sent
  before the work starts, so any past `satisfied_at` is accepted, however
  early.
- **A met notice whose start moves** (site work, REG4 review R2): the
  satisfaction carries over to the new row, because it belongs to the duty.
  But Annexure 2 states the expected commencement date. So when a
  satisfaction's `deadline_id` names a superseded row whose `basis_at`
  differs from the live row's, show "notice given for start <date>": that
  row's `basis_at` as a date in `time_zone`. The user can then decide
  whether to notify the provincial director of the new date; the law
  quoted in SOURCES doesn't say a new notice is needed.
- **Proof** (site work): `document_id` and `document_version` name a version
  of a document in the site's H&S file (`GET /v1/construction/sites/:siteId/safety-file`)
  whose type is in the deadline's `proof_document_types`: the Annexure 2
  notification for reg 4, the construction work permit for reg 3. Neither
  the document nor the version may be retracted. Both fields or neither.

A phone that was offline may record a call days after it was made. When
`satisfied_at` is before `due_at` but it was recorded at or after `due_at`,
the satisfaction has `recorded_after_due: true`. It still counts as met in
time. Show both times, because an overdue alert may have shown in the
meantime. A forthwith deadline never sets it.

| Status | When |
|---|---|
| 201 | Recorded. `DeadlineDetail` of the live row |
| 200 | The same satisfaction again (every field identical, same person): `DeadlineDetail` of the duty's live row now (the row it was recorded against, if the duty has been withdrawn since) |
| 400 | `"satisfaction id must be a UUID"`, `"satisfied_at is required"`, `"satisfied_at is in the future; check the device clock"`, `"satisfied_at must not be before the deadline's basis_at"`, `"method must be phone, email, fax, in_person, online, post or other"`, `"reference must be 1-200 characters"`, `"note must be 1-2000 characters"`, `"evidence_id must be a UUID"`, `"document_id must be a UUID"`, `"document_id and document_version are given together"`, `"document version not found in this site's health and safety file"`, `"that document type is not proof of this deadline"`, `"that document version is retracted"`, `"deadlines can't be computed for this incident: the holiday calendar starts on 2024-01-01"`; photo: `"evidence not found on this site"`, `"evidence was rejected"` (as `EvidenceProblem`) |
| 404 | `"site not found"`, `"deadline not found"` |
| 409 | `"satisfaction id already used with different details"`, `"deadline is already satisfied; retract that satisfaction first"`, `"deadline is withdrawn; its rule no longer applies"`, `"evidence not verified yet"` (as `EvidenceProblem`) |

### `POST /v1/sites/:siteId/deadlines/:deadlineId/satisfactions/:satisfactionId/retract` — take a satisfaction back

Body: `RetractRequest`. **Owner or SHE officer** only. For a satisfaction
recorded in error: it stays on record with who retracted it, when and why,
and the duty is open again (its alerts come back).

Retracting a GAR 8(1) notice whose recording started the death notice
(GAR 8(2)) leaves the death notice live until the incident's next amendment,
which withdraws it.

| Status | When |
|---|---|
| 201 | Retracted. `DeadlineDetail` |
| 200 | Already retracted with the same reason |
| 400 | `"reason must be 1-500 characters"` |
| 403 | `"only an owner or a SHE officer can retract a satisfaction"` |
| 404 | `"site not found"`, `"deadline not found"`, `"satisfaction not found"` (not a satisfaction of this deadline's duty) |
| 409 | `"satisfaction was already retracted with a different reason"` |

## Deadlines

A `Deadline` is one computation of a duty: e.g. "the written notice for this
incident, from 16 Nov 10:00", or "the reg 4 notice for this work, starting
Mon 30 Nov".

- **Its subject.** `subject_type` says which record started it, and exactly
  one of `incident_id` and `site_work_id` is set.
- **When it's due.** `due_at` is the first instant **after** the deadline:
  met in time means before `due_at`. For day-based rules, `due_on` is the
  last day (in `time_zone`, `Africa/Johannesburg` for South African sites):
  show "by the end of Mon 23 Nov". After an event, days are counted from the
  day after the event's date.
- **Before work starts** (`direction: "before"`, `unit: "clear_days"`): "at
  least 7 days before" counts **clear days**: neither the day of the notice
  nor the start day counts. A Mon 30 Nov start is due by the end of Sun
  22 Nov. That is our reading (no court ruling was found), so the rule's
  `caveat` says so: show it.
- **Forthwith** (`unit: "immediately"`) is a prompt to call now, not a
  countdown: `due_at` is the basis itself, the state is `due_now` until a
  call is recorded, and any recorded call meets it.
- **The legal latest date.** `due` is never pushed past a Sunday or public
  holiday, so it is the conservative date to act on and the one alerts use.
  When the last day of an after-deadline is a Sunday or public holiday, the
  law (Interpretation Act s4) allows at least until the next day that is
  neither: that is `legal_latest_on`, with `legal_latest_source_ref`. Show
  it beside the due date as "the law allows at least until …", never instead
  of it. A before-deadline never has one: extending it would shorten the
  notice.
- `excluded_days` lists the days a working-day count skipped (weekends,
  public holidays), so users can see why a date fell where it did.
- `overdue_when_computed`: already past when the system learned it applied
  (a type added days later, or work recorded less than 7 clear days before
  it starts). Say so, so nobody reads it as missed while the system watched.
- `state`:

| state | meaning |
|---|---|
| `open` | not met; due in the future |
| `due_now` | a forthwith duty, not met yet |
| `overdue` | not met; past `due_at` |
| `satisfied` | met before `due_at` (or a forthwith duty met at all) |
| `satisfied_late` | met, after `due_at` |
| `superseded` | replaced by a recomputation (`superseded_by`) |
| `withdrawn` | the rule no longer applies after an amendment |

`closed_by`, `closed_at` and `closed_reason` say who submitted the version
that superseded or withdrew a row, when, and its `amend_reason`.
`added_by_satisfaction_id` is set on a row that recording a satisfaction
added, rather than a version.

## Across the API

- 400 `"text must not contain NUL characters"`: a NUL character (`\u0000`) anywhere in the JSON body (any value or key, however nested), the query string or the path, on every endpoint, checked before the token (REG4 review). Postgres text can't store one.
- **503 `"server busy, try again"`** with a `Retry-After` header (seconds):
  the request ran out of time, or waited over 5 seconds for a record another
  request was changing (a satisfaction and an amendment of one subject at
  once, say). Retry after that many seconds.
- 401 `"authorization header required"`, `"invalid authorization header
  format"` or `"unauthorized"`: missing, malformed, expired or revoked
  token.
- 400 with the JSON decoder's own text: a body that isn't valid JSON or has
  a field of the wrong type.
- 500 `"internal error"`: a server fault.

## Types for `web/src/lib/types.ts`

Times are RFC 3339 strings in UTC with up to microsecond precision; `due_on`
and similar `_on` fields are dates (`YYYY-MM-DD`). Ids are UUIDs.

<!-- types -->
```ts
export type DeadlineState = "open" | "due_now" | "overdue" | "satisfied" | "satisfied_late" | "superseded" | "withdrawn";

export type DeadlineSubjectType = "incident" | "site_work";

export type SatisfactionMethod = "phone" | "email" | "fax" | "in_person" | "online" | "post" | "other";

export interface Deadline {
  id: string;
  duty_id: string;
  subject_type: DeadlineSubjectType;
  incident_id: string | null;
  site_work_id: string | null;
  rule_key: string;
  label: string;
  kind: string;
  period: number;
  unit: string;
  direction: string;
  basis: string;
  source_ref: string;
  caveat: string | null;
  proof_document_types: string[];
  basis_at: string;
  time_zone: string;
  due_at: string;
  due_on: string | null;
  excluded_days: string[];
  legal_latest_on: string | null;
  legal_latest_at: string | null;
  legal_latest_source_ref: string | null;
  computed_in_version: number;
  computed_at: string;
  added_by_satisfaction_id: string | null;
  overdue_when_computed: boolean;
  state: DeadlineState;
  closed_as: string | null;
  superseded_by: string | null;
  closed_in_version: number | null;
  closed_by: string | null;
  closed_at: string | null;
  closed_reason: string | null;
  satisfaction: Satisfaction | null;
}

export interface Satisfaction {
  id: string;
  deadline_id: string;
  satisfied_at: string;
  method: SatisfactionMethod;
  reference: string | null;
  note: string | null;
  evidence_id: string | null;
  document_id: string | null;
  document_version: number | null;
  recorded_by: string;
  recorded_at: string;
  recorded_after_due: boolean;
  retraction: Retraction | null;
}

export interface Retraction {
  retracted_by: string;
  retracted_at: string;
  reason: string;
}

export interface DeadlineDetail {
  id: string;
  duty_id: string;
  subject_type: DeadlineSubjectType;
  incident_id: string | null;
  site_work_id: string | null;
  rule_key: string;
  label: string;
  kind: string;
  period: number;
  unit: string;
  direction: string;
  basis: string;
  source_ref: string;
  caveat: string | null;
  proof_document_types: string[];
  basis_at: string;
  time_zone: string;
  due_at: string;
  due_on: string | null;
  excluded_days: string[];
  legal_latest_on: string | null;
  legal_latest_at: string | null;
  legal_latest_source_ref: string | null;
  computed_in_version: number;
  computed_at: string;
  added_by_satisfaction_id: string | null;
  overdue_when_computed: boolean;
  state: DeadlineState;
  closed_as: string | null;
  superseded_by: string | null;
  closed_in_version: number | null;
  closed_by: string | null;
  closed_at: string | null;
  closed_reason: string | null;
  satisfaction: Satisfaction | null;
  satisfactions: Satisfaction[];
}

export interface DeadlineList {
  deadlines: Deadline[];
}

export interface SatisfactionRequest {
  satisfied_at: string;
  method: SatisfactionMethod;
  reference?: string | null;
  note?: string | null;
  evidence_id?: string | null;
  document_id?: string | null;
  document_version?: number | null;
}

export interface RetractRequest {
  reason: string;
}

export interface EvidenceProblem {
  error: string;
  evidence_ids: string[];
}
```
