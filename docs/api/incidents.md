# Incidents API

For the dashboard and the mobile app: the incident register, the deadlines
each incident starts (statutory reports, investigations), recording that a
deadline was met, and witness statements. The backend keeps this file true:
`TestAPIDoc` (in `construction_db/internal/core/incidents`) fails if the
types below stop matching the Go code, or if an error text the handlers send
isn't quoted here.

All endpoints need `Authorization: Bearer <access token>`. Errors are
`{"error": "<message>"}`.

Roles: owner and SHE officer on any site of their organisation; supervisor
only on sites they've been added to (any other site is 404 `"site not found"`). Other roles get 403 `"forbidden"`.

## How incidents work

- **Capture once, offline.** The phone keeps the draft. Nothing is on the
  server until the reporter submits **version 1**, under an incident id the
  phone made up (a UUID v4). Retrying a submission is always safe.
- **Never edited.** A submitted version never changes. To correct or add
  anything, submit the next version: the **whole** account again (types,
  times, people, photos), with an `amend_reason`. Every version stays
  readable as it was submitted.
- **Several types.** One incident can be several types at once, e.g.
  `pressure_release` and `unconsciousness`. Types come from the **site's**
  regime (`GET /v1/sites/:siteId/checklists` gives the regime; the type keys
  and labels are listed under [Incident types](#incident-types)).
- **Deadlines follow the facts.** Each version works out which of the
  regime's deadline rules apply. A rule that applies for the first time is
  added. One whose start moved (a corrected `occurred_at`) is
  **superseded** by a recomputed row. One that no longer applies is
  **withdrawn**. One that is unchanged is **carried**. Nothing is ever
  deleted: superseded and withdrawn rows stay, so the history shows what the
  system said, and when.
- **Deadlines are guidance, not legal advice.** Always show `source_ref`
  and `caveat` with a deadline, and never present one as authoritative.

## Endpoints

### `PUT /v1/sites/:siteId/incidents/:incidentId/versions/:version` — submit a version

Body: `SubmitVersionRequest`. Owner, SHE officer, supervisor. `version` is
1 for a new incident, then 2, 3, … Send the **latest** version + 1;
`GET` the incident first if unsure.

The response is the `Incident` as of this version, with `changes` saying
what it did to the deadlines.

- `occurred_at` is when it happened; the clocks run from it (OHSA and GAR:
  "within seven days of any incident"). A type added later still runs from
  `occurred_at`, so its deadline may already be past
  (`overdue_when_computed: true`).
- `employer_aware_at` is when the organisation learned of it. It is
  **required** when a person is `employee_of_org` and `injured`: COIDA's
  employer's report (W.Cl.2) runs from it. Default it to `occurred_at` on
  the phone.
- A person with `died_at` must be `injured`, and the version must include a
  reportable type.
- `people` and `evidence_ids` may be left out (treated as `[]`). Names are
  optional, so a near miss can stay no-blame.
- Photos must be **verified** evidence on this site (see
  `docs/api/evidence.md`); only their id and capture time are shown back.
- Times may be up to 5 minutes ahead of the server (device clocks drift);
  more than that is refused.
- Due dates are counted with the public holiday calendar, which starts on
  1 Jan 2024. An incident whose deadlines would fall before then (one from
  before about 25 Dec 2023) is refused with 400. An incident with no clocks,
  such as a near miss, is accepted.

Who may weaken a statutory clock: withdrawing a **report** deadline that
isn't met yet, or moving one later (by correcting `occurred_at` or
`employer_aware_at` later), takes an owner or SHE officer. Correcting a
time earlier, adding types, or withdrawing an investigation is open to
supervisors too.

| Status | When |
|---|---|
| 201 | Submitted. `Incident` |
| 200 | The same version again (every field identical, same person): the stored `Incident`, unchanged |
| 400 | `"incident id must be a UUID"`, `"version must be a whole number, 1 or more"`, `"taxonomy_codes must list at least one incident type"`, `"at most 10 incident types"`, `"taxonomy_codes must not repeat"`, `"taxonomy_codes must be incident types of the site's regime"` (as `UnknownTypes`, naming them), `"occurred_at is required"`, `"occurred_at is in the future; check the device clock"`, `"employer_aware_at must not be before occurred_at"`, `"employer_aware_at is in the future; check the device clock"`, `"employer_aware_at is required when an employee of the organisation is injured"`, `"client_submitted_at is required"`, `"client_submitted_at is in the future; check the device clock"`, `"location must be 1-500 characters"`, `"description must be 1-5000 characters"`, `"at most 50 people"`, `"people: name, occupation, employer_name and body_part must be 1-200 characters, injury_description 1-2000"`, `"treatment must be none, first_aid, medical_treatment or hospitalised"`, `"injury details are only given for someone injured"`, `"someone who died must be marked injured"`, `"employer_name is only given for someone not employed by the organisation"`, `"died_at must not be before occurred_at"`, `"died_at is in the future; check the device clock"`, `"a death must be classified with at least one reportable incident type"`, `"evidence_ids must be UUIDs"`, `"evidence_ids must not repeat"`, `"at most 20 photos"`, `"amend_reason is required for an amendment (1-500 characters)"`, `"version 1 has no amend_reason"`, `"device_id must be 1-200 bytes"`, `"deadlines can't be computed for this incident: the holiday calendar starts on 2024-01-01"`; photos: `"evidence not found on this site"` or `"evidence was rejected"` (as `EvidenceProblem`) |
| 403 | `"only an owner or a SHE officer can withdraw a report deadline or move it later"` |
| 404 | `"site not found"`; `"incident not found"` (version 2 or later of an incident that doesn't exist) |
| 409 | `"incident id already used with different details"` (version 1, or the id is on another site); `"this version was already submitted with different details"`; `"not the next version; get the incident and amend its latest version"` (as `NotNextVersion`, with `latest_version`); `"site is archived"` (the site was archived before `occurred_at`; an incident that happened earlier can still be reported); `"evidence not verified yet"` (as `EvidenceProblem`: still uploading, retry later) |

### `GET /v1/sites/:siteId/incidents/:incidentId?version=` — get an incident

Owner, SHE officer, supervisor. The `Incident` as of `version` (default the
latest), with every deadline row of the incident (all versions, so the
history is visible) and every witness statement. Works on an archived site.

| Status | When |
|---|---|
| 200 | `Incident` |
| 400 | `"version must be a whole number, 1 or more"` |
| 404 | `"site not found"`, `"incident not found"` (also for a malformed id), `"version not found"` |

### `GET /v1/sites/:siteId/incidents?type=&reportable=&open_deadlines=&limit=` — the incident register

Owner, SHE officer, supervisor. `IncidentList`, latest occurrence first,
each as of its latest version. `type` keeps incidents whose latest version
has that type; `reportable` and `open_deadlines` are `true` or `false`;
`limit` 1–200, default 50. Works on an archived site.

`open_deadlines` counts live deadlines not met yet; `overdue_deadlines` those
of them past due (a forthwith call is `due_now`, not overdue); `next_due_at`
is the soonest of them.

| Status | When |
|---|---|
| 200 | `IncidentList` |
| 400 | `"type must be an incident type of the site's regime"`, `"reportable must be true or false"`, `"open_deadlines must be true or false"`, `"limit must be 1-200"` |
| 404 | `"site not found"` |

### `PUT /v1/sites/:siteId/incidents/:incidentId/witness-statements/:statementId` — record a witness statement

Body: `StatementRequest`. Owner, SHE officer, supervisor. The phone makes up
`statementId`. A statement is never changed: to correct one, record a new
statement with `supersedes_id` (one correction per statement); the old one
then shows `superseded_by`. Works on an archived site.

| Status | When |
|---|---|
| 201 | Recorded. `WitnessStatement` |
| 200 | The same statement again (every field identical, same person) |
| 400 | `"statement id must be a UUID"`, `"witness_name must be 1-200 characters"`, `"witness_contact must be 1-200 characters"`, `"statement must be 1-20000 characters"`, `"taken_at is required"`, `"taken_at is in the future; check the device clock"`, `"device_id must be 1-200 bytes"`, `"evidence_id must be a UUID"`, `"supersedes_id must be a statement on this incident"`; photo: `"evidence not found on this site"`, `"evidence was rejected"` (as `EvidenceProblem`) |
| 404 | `"site not found"`, `"incident not found"` |
| 409 | `"statement id already used with different details"`, `"that statement already has a correction"`, `"evidence not verified yet"` |

### `GET /v1/sites/:siteId/deadlines?state=&limit=` — a site's live deadlines

Owner, SHE officer, supervisor. `DeadlineList`: live deadlines only
(superseded and withdrawn rows are on the incident), soonest `due_at`
first. `state` is one of `open`, `due_now`, `overdue`, `satisfied`,
`satisfied_late`; `limit` 1–200, default 50.

| Status | When |
|---|---|
| 200 | `DeadlineList` |
| 400 | `"state must be open, due_now, overdue, satisfied or satisfied_late"`, `"limit must be 1-200"` |
| 404 | `"site not found"` |

### `GET /v1/sites/:siteId/deadlines/:deadlineId` — one deadline

Owner, SHE officer, supervisor. `DeadlineDetail`: the row, with every
satisfaction of its duty (retracted ones too), oldest first.

| Status | When |
|---|---|
| 200 | `DeadlineDetail` |
| 404 | `"site not found"`, `"deadline not found"` |

### `PUT /v1/sites/:siteId/deadlines/:deadlineId/satisfactions/:satisfactionId` — record that a deadline was met

Body: `SatisfactionRequest`. Owner, SHE officer, supervisor (a forthwith
call is usually made from site). The phone makes up `satisfactionId`.
`satisfied_at` is when the call was made or the form sent, not when it is
recorded.

A satisfaction belongs to the **duty**, not the row: if the deadline is
later recomputed (a corrected `occurred_at`), the satisfaction carries over
and is judged against the new due time. You may name any row of the duty;
it is recorded against the live one, which the response returns. Recording
a call can start a further clock: a death after the notice needs its own
notice (GAR 8(2)). That row has `added_by_satisfaction_id` set. No version
lists it in `changes.added`; later versions list it as carried.

A phone that was offline may record a call days after it was made. When
`satisfied_at` is before `due_at` but the call was recorded at or after
`due_at`, the satisfaction has `recorded_after_due: true`. It still counts as
met in time. The dashboard shows both times ("called Tue 17 Nov 09:10,
recorded Mon 23 Nov 14:02"), because an overdue alert may have shown in the
meantime. A forthwith deadline never sets it.

| Status | When |
|---|---|
| 201 | Recorded. `DeadlineDetail` of the live row |
| 200 | The same satisfaction again (every field identical, same person): `DeadlineDetail` of the duty's live row now (the row it was recorded against, if the duty has been withdrawn since) |
| 400 | `"satisfaction id must be a UUID"`, `"satisfied_at is required"`, `"satisfied_at is in the future; check the device clock"`, `"satisfied_at must not be before the deadline's basis_at"`, `"method must be phone, email, fax, in_person, online, post or other"`, `"reference must be 1-200 characters"`, `"note must be 1-2000 characters"`, `"evidence_id must be a UUID"`; photo: `"evidence not found on this site"`, `"evidence was rejected"` (as `EvidenceProblem`) |
| 404 | `"site not found"`, `"deadline not found"` |
| 409 | `"satisfaction id already used with different details"`, `"deadline is already satisfied; retract that satisfaction first"`, `"deadline is withdrawn; its rule no longer applies"`, `"evidence not verified yet"` |

### `POST /v1/sites/:siteId/deadlines/:deadlineId/satisfactions/:satisfactionId/retract` — take a satisfaction back

Body: `RetractRequest`. **Owner or SHE officer** only. For a satisfaction
recorded in error: it stays on record with who retracted it, when and why,
and the duty is open again (its alerts come back).

Retracting a GAR 8(1) notice whose recording started the death notice
(GAR 8(2)) leaves the death notice live until the next amendment. That
amendment withdraws it, because the death no longer follows a recorded
notice. Withdrawing a report deadline that isn't met takes an owner or SHE
officer, so a supervisor's next amendment of that incident gets 403 until a
notice is recorded again or a lead amends it.

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
incident, from 16 Nov 10:00".

- **When it's due.** `due_at` is the first instant **after** the deadline:
  met in time means before `due_at`. For day-based rules, `due_on` is the
  last day (in `time_zone`, `Africa/Johannesburg` for South African sites):
  show "by the end of Mon 23 Nov". Days are counted from the day after the
  incident's date.
- **Forthwith** (`unit: "immediately"`) is a prompt to call now, not a
  countdown: `due_at` is the basis itself, the state is `due_now` until a
  call is recorded, and any recorded call meets it.
- **The legal latest date.** `due` is never pushed past a Sunday or public
  holiday, so it is the conservative date to act on and the one alerts use.
  When the last day is a Sunday or public holiday, the law (Interpretation
  Act s4) allows at least until the next day that is neither (a Saturday
  counts): that is `legal_latest_on`, with `legal_latest_source_ref`. One
  reading of s4 allows longer still; that is unsettled (see
  `SOURCES_za_construction_2014.md`). Show it beside the due date as "the law
  allows at least until …", never instead of it.
- `excluded_days` lists the days a working-day count skipped (weekends,
  public holidays), so users can see why a date fell where it did.
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

## Incident types

From the `za_construction_2014` regime (Construction Regulations 2014 sites).
`reportable` types start a report to the provincial director.

| key | label | reportable |
|---|---|---|
| `fatality` | Death | yes |
| `unconsciousness` | Person became unconscious | yes |
| `loss_of_limb` | Loss of a limb or part of a limb | yes |
| `injury_likely_fatal_or_permanent` | Injury or illness likely to cause death or a permanent physical defect | yes |
| `injury_14_days_unable_to_work` | Injury or illness likely to keep the person off work for at least 14 days | yes |
| `major_incident` | Major incident | yes |
| `dangerous_substance_spill` | Dangerous substance spilled, endangering health or safety | yes |
| `pressure_release` | Uncontrolled release of a substance under pressure, endangering health or safety | yes |
| `machinery_failure_flying_objects` | Machinery fractured or failed, causing flying, falling or uncontrolled moving objects | yes |
| `machinery_out_of_control` | Machinery ran out of control | yes |
| `medical_treatment` | Injury needing medical treatment beyond first aid | no (investigated) |
| `first_aid` | First aid only | no |
| `near_miss` | Near miss | no |
| `property_damage` | Property or equipment damage only | no |

## Across the API

- **503 `"server busy, try again"`** with a `Retry-After` header (seconds):
  the request ran out of time, or waited over 5 seconds for a record another
  request was changing (a burst of uploads to one site, say). Retry after
  that many seconds.
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
export type Treatment = "none" | "first_aid" | "medical_treatment" | "hospitalised";

export type DeadlineState = "open" | "due_now" | "overdue" | "satisfied" | "satisfied_late" | "superseded" | "withdrawn";

export type SatisfactionMethod = "phone" | "email" | "fax" | "in_person" | "online" | "post" | "other";

export interface SubmitVersionRequest {
  taxonomy_codes: string[];
  occurred_at: string;
  employer_aware_at?: string | null;
  location?: string | null;
  description?: string | null;
  people: PersonInput[];
  evidence_ids: string[];
  client_submitted_at: string;
  device_id?: string | null;
  amend_reason?: string | null;
}

export interface PersonInput {
  name?: string | null;
  occupation?: string | null;
  employee_of_org: boolean;
  employer_name?: string | null;
  injured: boolean;
  injury_description?: string | null;
  body_part?: string | null;
  treatment?: Treatment | null;
  died_at?: string | null;
}

export interface Incident {
  id: string;
  site_id: string;
  reported_by: string;
  created_at: string;
  latest_version: number;
  reportable: boolean;
  version: Version;
  changes: DeadlineChanges;
  deadlines: Deadline[];
  witness_statements: WitnessStatement[];
}

export interface Version {
  version: number;
  types: IncidentType[];
  occurred_at: string;
  employer_aware_at: string | null;
  location: string | null;
  description: string | null;
  people: Person[];
  evidence: AttachedEvidence[];
  submitted_by: string;
  client_submitted_at: string;
  submitted_at: string;
  device_id: string | null;
  amend_reason: string | null;
}

export interface IncidentType {
  key: string;
  label: string;
  reportable: boolean;
}

export interface Person {
  position: number;
  name: string | null;
  occupation: string | null;
  employee_of_org: boolean;
  employer_name: string | null;
  injured: boolean;
  injury_description: string | null;
  body_part: string | null;
  treatment: Treatment | null;
  died_at: string | null;
}

export interface AttachedEvidence {
  evidence_id: string;
  client_captured_at: string;
}

export interface DeadlineChanges {
  added: string[];
  superseded: string[];
  withdrawn: string[];
  carried: string[];
}

export interface WitnessStatement {
  id: string;
  witness_name: string;
  witness_contact: string | null;
  statement: string;
  taken_by: string;
  taken_at: string;
  recorded_at: string;
  device_id: string | null;
  evidence: AttachedEvidence | null;
  supersedes_id: string | null;
  superseded_by: string | null;
}

export interface IncidentSummary {
  id: string;
  site_id: string;
  latest_version: number;
  types: IncidentType[];
  reportable: boolean;
  occurred_at: string;
  location: string | null;
  reported_by: string;
  created_at: string;
  people_count: number;
  injured_count: number;
  death_count: number;
  open_deadlines: number;
  overdue_deadlines: number;
  next_due_at: string | null;
}

export interface IncidentList {
  incidents: IncidentSummary[];
}

export interface StatementRequest {
  witness_name: string;
  witness_contact?: string | null;
  statement: string;
  taken_at: string;
  device_id?: string | null;
  evidence_id?: string | null;
  supersedes_id?: string | null;
}

export interface Deadline {
  id: string;
  duty_id: string;
  incident_id: string | null;
  rule_key: string;
  label: string;
  kind: string;
  period: number;
  unit: string;
  direction: string;
  basis: string;
  source_ref: string;
  caveat: string | null;
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
  incident_id: string | null;
  rule_key: string;
  label: string;
  kind: string;
  period: number;
  unit: string;
  direction: string;
  basis: string;
  source_ref: string;
  caveat: string | null;
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
}

export interface RetractRequest {
  reason: string;
}

export interface EvidenceProblem {
  error: string;
  evidence_ids: string[];
}

export interface UnknownTypes {
  error: string;
  taxonomy_codes: string[];
}

export interface NotNextVersion {
  error: string;
  latest_version: number;
}
```
