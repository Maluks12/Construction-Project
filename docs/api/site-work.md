# Site work API (REG4)

For the dashboard and the mobile app: a site's planned construction work and
the notices it needs before it starts. CR 2014 reg 4 requires a notification
to the provincial director at least 7 days before some work starts; reg 3
(as exempted by GN 850 of 2018) a construction work permit application at
least 30 days before larger work. The backend keeps this file true:
`TestAPIDoc` (in `construction_db/internal/editions/construction/reg4`) fails
if the types below stop matching the Go code, or if an error text the
handlers send isn't quoted here.

All endpoints need `Authorization: Bearer <access token>` and are under
`/v1/construction`. A site whose regime isn't a construction one is 404
`"site not found"`. Errors are `{"error": "<message>"}`.

Roles: owner and SHE officer on any site of their organisation; supervisor
only on sites they've been added to (any other site is 404 `"site not
found"`). Other roles get 403 `"forbidden"`. These roles follow the incident
register's and are subject to the role matrix still being approved.

## How site work works

- **One record per piece of work.** A site may have several (phases: a
  demolition starting in March, the build in May). The phone makes up the
  record id (a UUID v4); retrying a submission is always safe.
- **Never edited.** To change the start date, the facts or the status,
  submit the next version: the whole record again, with an `amend_reason`.
  Every version stays readable.
- **Facts, not rules.** The user ticks what the work involves
  (`GET …/site-work-facts` lists them with their sources). The deadlines
  follow from the facts by the regime's rules, shown in the same response:
  - reg 4 notice: excavation, work at height with a risk of falling,
    demolition, explosives, or a single-storey dwelling for a client who will
    live in it;
  - reg 3 permit application: more than 365 days **and** more than 3600
    person-days, or a tender value limit of CIDB grade 7, 8 or 9;
  - a permit site needs no reg 4 notice (reg 4(1): "other than work
    contemplated in regulation 3(1)");
  - a single-storey dwelling never needs a permit (reg 2(2)).
- **Counting.** "At least 7 days before" is counted in clear days: neither
  the day of the notice nor the start day counts. Work starting Mon 30 Nov is
  due by the end of Sun 22 Nov. The deadline's `caveat` says this is our
  reading; show it.
- **Late or past starts.** Work recorded less than 7 clear days before it
  starts (or already started) is recorded anyway: its notice is overdue from
  the start (`overdue_when_computed` on the deadline, `late_when_recorded` on
  the version). Say so, so nobody reads it as missed while the system
  watched.
- **Who must do it.** Reg 4 binds the contractor who intends to carry out the
  work, so an overdue reg 4 notice is red. Reg 3 binds the client (or the
  client's agent), so an overdue permit application is amber with a caveat
  until SiteProof records the organisation's role on the site.
- **Met.** Record the notice with
  `PUT /v1/sites/:siteId/deadlines/:deadlineId/satisfactions/:satisfactionId`
  (`docs/api/deadlines.md`), linking the Annexure 2 (or the permit) from the
  site's H&S file as proof.
- **The start date** must fall inside the years the public holiday calendar
  covers (now 2024 to 2030); outside, 400.

## Endpoints

### `PUT /v1/construction/sites/:siteId/site-work/:workId/versions/:version` — submit a version

Body: `VersionRequest`. `version` is 1 for new work, then 2, 3, …; send the
latest + 1.

- `facts` may be left out or `[]` (work that triggers nothing).
- `status` defaults to `planned`; `cancelled` (version 2 or later) withdraws
  every deadline. A later `planned` version reinstates them; a satisfaction
  already recorded carries over.
- `client_submitted_at` is the device's time of submission (up to 5 minutes
  ahead of the server). An archived site takes nothing submitted after it was
  archived.

Who may weaken a statutory clock: withdrawing a notice or permit deadline
that isn't met yet (by removing a fact, adding a permit fact, or cancelling),
or moving one later (by a later start, met or not), takes an owner or SHE
officer. An earlier start, or adding a fact, is open to supervisors too.

The response is the `SiteWork` as of this version, with `changes` saying what
it did to the deadlines.

| Status | When |
|---|---|
| 201 | Submitted. `SiteWork` |
| 200 | The same version again (every field identical, same person) |
| 400 | `"site work id must be a UUID"`, `"version must be a whole number, 1 or more"`, `"description must be 1-500 characters"`, `"starts_on must be a date (YYYY-MM-DD)"`, `"ends_on must be a date (YYYY-MM-DD), not before starts_on"`, `"starts_on is outside the years the holiday calendar covers"`, `"facts must not repeat"`, `"facts must be site work facts of the site's regime"` (as `UnknownFacts`, naming them), `"status must be planned or cancelled"`, `"version 1 is planned; cancel in a later version"`, `"client_submitted_at is required"`, `"client_submitted_at is in the future; check the device clock"`, `"device_id must be 1-200 bytes"`, `"amend_reason is required for an amendment (1-500 characters)"`, `"version 1 has no amend_reason"` |
| 403 | `"only an owner or a SHE officer can withdraw a notification deadline or move it later"` |
| 404 | `"site not found"`; `"site work not found"` (version 2 or later of work that doesn't exist) |
| 409 | `"site work id already used with different details"` (version 1, or the id is on another site); `"this version was already submitted with different details"`; `"not the next version; get the site work and amend its latest version"` (as `NotNextVersion`, with `latest_version`); `"site is archived"` |

### `GET /v1/construction/sites/:siteId/site-work/:workId?version=` — get site work

The `SiteWork` as of `version` (default the latest), with every deadline row
(all versions). Works on an archived site.

| Status | When |
|---|---|
| 200 | `SiteWork` |
| 400 | `"version must be a whole number, 1 or more"` |
| 404 | `"site not found"`, `"site work not found"` (also for a malformed id), `"version not found"` |

### `GET /v1/construction/sites/:siteId/site-work?status=&limit=` — the site's work

`SiteWorkList`, the earliest start first, each as of its latest version.
`status` is `planned` or `cancelled`; `limit` 1–200, default 50.
`open_deadlines` counts live deadlines not met yet; `overdue_deadlines` those
past due; `next_due_at` the soonest.

| Status | When |
|---|---|
| 200 | `SiteWorkList` |
| 400 | `"status must be planned or cancelled"`, `"limit must be 1-200"` |
| 404 | `"site not found"` |

### `GET /v1/construction/sites/:siteId/site-work-facts` — the form

`Catalogue`: the site regime's facts and the rules they start, with each
rule's caveat and source.

| Status | When |
|---|---|
| 200 | `Catalogue` |
| 404 | `"site not found"` |

## Across the API

- 400 `"text must not contain NUL characters"`: a NUL character (`\u0000`) anywhere in the JSON body (any value or key, however nested), the query string or the path, on every endpoint, checked before the token (REG4 review). Postgres text can't store one.
- **503 `"server busy, try again"`** with a `Retry-After` header (seconds):
  the request ran out of time, or waited over 5 seconds for a record another
  request was changing (two versions of one site work record at once, say).
  Retry after that many seconds.
- 401: missing, malformed, expired or revoked token.
- 400 with the JSON decoder's own text: a body that isn't valid JSON or has
  a field of the wrong type.
- 500 `"internal error"`: a server fault.

## Types for `web/src/lib/types.ts`

`Deadline` is in `docs/api/deadlines.md`. Times are RFC 3339 in UTC; `_on`
fields are dates (`YYYY-MM-DD`) in the site's time zone.

<!-- types -->
```ts
export type SiteWorkStatus = "planned" | "cancelled";

export interface VersionRequest {
  description: string;
  starts_on: string;
  ends_on?: string | null;
  facts: string[];
  status: SiteWorkStatus;
  client_submitted_at: string;
  device_id?: string | null;
  amend_reason?: string | null;
}

export interface SiteWorkFact {
  key: string;
  label: string;
  source_ref: string;
}

export interface SiteWorkVersion {
  version: number;
  description: string;
  starts_on: string;
  ends_on: string | null;
  facts: SiteWorkFact[];
  status: SiteWorkStatus;
  submitted_by: string;
  client_submitted_at: string;
  submitted_at: string;
  device_id: string | null;
  amend_reason: string | null;
  late_when_recorded: boolean;
}

export interface SiteWorkChanges {
  added: string[];
  superseded: string[];
  withdrawn: string[];
  carried: string[];
}

export interface SiteWork {
  id: string;
  site_id: string;
  created_by: string;
  created_at: string;
  latest_version: number;
  version: SiteWorkVersion;
  changes: SiteWorkChanges;
  deadlines: Deadline[];
}

export interface SiteWorkSummary {
  id: string;
  site_id: string;
  latest_version: number;
  description: string;
  starts_on: string;
  ends_on: string | null;
  facts: string[];
  status: SiteWorkStatus;
  created_at: string;
  open_deadlines: number;
  overdue_deadlines: number;
  next_due_at: string | null;
}

export interface SiteWorkList {
  site_work: SiteWorkSummary[];
}

export interface SiteWorkRule {
  rule_key: string;
  label: string;
  period: number;
  unit: string;
  duty_holder: string;
  caveat: string | null;
  source_ref: string;
  when_any: string[][];
  unless_facts: string[];
  unless_rules: string[];
}

export interface Catalogue {
  facts: SiteWorkFact[];
  rules: SiteWorkRule[];
}

export interface UnknownFacts {
  error: string;
  facts: string[];
}

export interface NotNextVersion {
  error: string;
  latest_version: number;
}
```
