# Site diary API (Construction edition)

For the mobile app and the dashboard: the daily site diary. The backend
keeps this file true: `TestAPIDoc` (in
`construction_db/internal/editions/construction/diary`) fails if the types
below stop matching the Go code, or if an error text the handlers send isn't
quoted here.

All endpoints need `Authorization: Bearer <access token>`. Errors are
`{"error": "<message>"}`.

The diary belongs to the Construction edition, so its endpoints are under
`/v1/construction`. A site whose regime isn't a construction regime has no
diary: 404 `"site not found"`, as for a site you can't see.

Roles: owner and SHE officer ("leads") on any site of their organisation;
supervisor only on sites they've been added to (any other site is 404
`"site not found"`). Other roles get 403 `"forbidden"`.

## How the site diary works

- **One diary per site per day**, the local day in the site regime's
  `time_zone` (`diary_date`, `YYYY-MM-DD`). The first save creates it.
- **Three things are required, nothing else:** `weather` (at least one
  condition), `headcount` (the number the supervisor enters; 0 is a
  number) and `work_done`. Everything in `sections` and every photo is
  optional.
- **Each save restates the whole diary** as a new revision, under an id the
  phone made up (`PUT …/revisions/:revisionId`). Every revision is kept. The
  **current** diary is the newest by device time (`client_saved_at`).
- **Two phones.** Send `base_revision_id`: the revision the phone last saw
  (`current.id` from the last `GET`), or null when writing from scratch. A
  save that wasn't based on the current revision, or is older than it,
  can't silently replace or vanish: both are kept and the diary shows
  `conflict_revision_ids`. `GET …/revisions` returns them all, to show side
  by side. The conflict clears when someone saves a revision based on the
  current one (the newest): they've seen both.
- **Midnight.** The diary closes at the end of its day (`closes_at`):
  - A revision **saved on the phone before midnight** still counts as the
    original if the server receives it by `grace_ends_at` (48 hours after
    midnight). It shows `received_after_close: true`.
  - A save **made after midnight** is refused once the diary has a
    revision; add an addendum instead.
  - A day with **no diary at all** can still be written after it: a **late
    diary**, with a `late_reason`. Its single revision shows
    `saved_after_close: true`, the diary shows `late: true`, and from then
    on only addenda. An on-time save still arriving inside the grace is
    accepted too, and shows as a conflict that can't be cleared after
    midnight.
- **Addenda.** After `closes_at`, changes are dated addenda: who, when (phone
  and server time), why (`reason`, required) and what (a note, sections,
  photos: at least one). The original never changes; addenda are listed
  after it.
- **Headcount.** `current.headcount_suggested` is the number of distinct
  people, counted when the revision arrived:
  - who signed in on site that day (attendance.md: not retracted, on the
    day of the phone's time);
  - plus anyone who only signed that day's toolbox talks (not retracted).

  Each person counts once. A sign-in is its register worker. A signature is
  the worker it is linked to or matches by name, firm and clock number, else
  that name, firm and clock number. So a worker who signed in and signed a
  talk counts once.

  A talk counts on the day it started (`talk_date`), with every signature it
  got: a night-shift talk at 23:40 counts on that day, even for people who
  signed after midnight.

  `headcount_suggested_now` is the same count now, including sign-ins that
  arrived late from an offline phone. Revisions saved before M11 kept the
  talk-only count they were given. Both sit beside the entered `headcount`,
  so a gap stays visible.
- **Labour rows can name workers (M11).** A `labour` row's optional
  `worker_ids` are register workers counted in it: each once, at most its
  `headcount`. It doesn't change any count.
- **Linked, not copied.** `toolbox_talk_ids` and `incident_ids` are that
  day's talks and incidents (by the incident's latest version's
  `occurred_at`). Fetch them from `docs/api/toolbox.md` and
  `docs/api/incidents.md`.
- **Section rows have ids** the phone makes up (a UUID v4), kept the same in
  every revision that restates the row. Later records point at them: JBCC
  delay events and contract instructions, the principal-contractor tier's
  firms. An instruction recorded here is a record of what was said or
  written; it never becomes a contract instruction by itself.
- **Times inside sections** must be on the diary's day.
- **Photos** must be verified evidence of the site. A photo still uploading
  is 409 with its id: keep the revision queued and send it again once the
  photo is complete, within the grace.
- **Archived sites** keep their diaries. A diary is accepted only for a day
  that began before the site was archived; addenda always.

## Endpoints

### `PUT /v1/construction/sites/:siteId/diary/:date/revisions/:revisionId` — save the diary

Body: `RevisionRequest`. Owner, SHE officer, supervisor. The response is the
`Diary` after the save. Order of checks: the body, the site, an identical
repeat (200, before anything else), the day and the closing rules, the
base, the photos.

| Status | When |
|---|---|
| 201 | Saved. `Diary` |
| 200 | The same revision again (same person, same fields): the `Diary` now |
| 400 | `"revision id must be a UUID"`, `"date must be YYYY-MM-DD"`, `"base_revision_id must be a UUID"`, `"weather is required, with at least one condition"`, `"weather conditions must be fine, cloudy, rain, heavy_rain, thunderstorm, wind, hot, cold or mist, each once"`, `"weather note must be 1-500 characters"`, `"headcount is required (0-10000)"`, `"work_done must be 1-4000 characters"`, `"client_saved_at is required"`, `"client_saved_at is in the future; check the device clock"`, `"evidence_ids must be UUIDs, each once, at most 50"`, `"device_id must be 1-200 bytes"`, `"the diary's day hasn't begun"`, `"client_saved_at is before the diary's day"`, `"a diary saved after its day closed is a late diary and needs late_reason (1-500 characters)"`, `"late_reason is only for a diary saved after its day closed"`, `"base_revision_id is not a revision of this diary"`; a section problem (`SectionProblem`, below); `"evidence not found on this site"` or `"evidence was rejected"` (as `EvidenceProblem`) |
| 404 | `"site not found"` |
| 409 | `"revision id already used with different details"`; `"site diary closed at midnight; add an addendum"`; `"saved before midnight but received more than 48 hours after; add an addendum"`; `"the site was archived before this day"`; `"evidence not verified yet"` (as `EvidenceProblem`: retry once the upload is complete) |

A section problem names where it is:
`{"error": "...", "section": "labour", "row": 1, "field": "own_team"}`
(`row` is 0-based; -1 for `weather_times`). Its `error` is one of
`"row id must be a UUID, used once in its section"`, `"text is required and
must be 1-500 characters (descriptions 1-2000)"`, `"number out of range (hours
0-24, rain gauge 0-1000 mm, headcount 0-10000)"`, `"value is not one of the
allowed choices"`, `"time must be on the diary's day"`, `"only one labour row
can be own_team"`, `"at most 200 rows in a section"`, `"worker_ids must be register workers, each once, at most the row's headcount"`, `"received_at is
required"`, `"an end time must not be before its start"`.

### `GET /v1/construction/sites/:siteId/diary/:date` — a day's diary

Owner, SHE officer, supervisor. The `Diary`: the current revision, the
conflict, the addenda (oldest first), the headcount suggestion and the day's
talks and incidents. Works on an archived site. 400 `"date must be
YYYY-MM-DD"`; 404 `"site not found"` or `"site diary not found"`. Photos show
only their id, so this isn't a view of the evidence; open a photo with the
evidence `view` endpoint (logged).

### `GET /v1/construction/sites/:siteId/diary/:date/revisions` — every revision

Owner, SHE officer, supervisor. `{"revisions": [Revision]}` in the order the
server received them (`seq`). 404 `"site not found"` or `"site diary not
found"`.

### `GET /v1/construction/sites/:siteId/diary?from=&to=&limit=` — the register

Owner, SHE officer, supervisor. `{"diaries": [DiarySummary]}`, newest day
first, between `from` and `to` (inclusive dates, both optional). `limit`
1–200, default 50. 400 `"from and to must be YYYY-MM-DD, from not after to"`
or `"limit must be 1-200"`; 404 `"site not found"`.

### `GET /v1/construction/sites/:siteId/diary-firms` — firm names for autocomplete

Owner, SHE officer, supervisor. `{"firms": [string]}`: the firm names used
on the site before, in diary labour rows and toolbox signatures, sorted. 404
`"site not found"`.

### `PUT /v1/construction/sites/:siteId/diary/:date/addenda/:addendumId` — add to a closed diary

Body: `AddendumRequest`. Owner, SHE officer, supervisor. The response is the
`Addendum`. An identical repeat answers 200.

| Status | When |
|---|---|
| 201 | Added. `Addendum` |
| 200 | The same addendum again (same person, same fields) |
| 400 | `"addendum id must be a UUID"`, `"date must be YYYY-MM-DD"`, `"reason must be 1-500 characters"`, `"note must be 1-4000 characters"`, `"client_added_at is required"`, `"client_added_at is in the future; check the device clock"`, `"device_id must be 1-200 bytes"`, `"evidence_ids must be UUIDs, each once, at most 50"`, `"an addendum needs a note, a section or a photo"`; a section problem; `"evidence not found on this site"` or `"evidence was rejected"` (as `EvidenceProblem`) |
| 404 | `"site not found"`, `"site diary not found"` (no diary that day: write a late diary instead) |
| 409 | `"addendum id already used with different details"`; `"site diary is still open; save a revision instead"`; `"evidence not verified yet"` (as `EvidenceProblem`) |

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

Times are RFC 3339 strings in UTC with up to microsecond precision; `_date`
fields are dates (`YYYY-MM-DD`). Ids are UUIDs. In requests, a field typed
`| null` may also be left out.

<!-- types -->
```ts
export type WeatherCondition = "fine" | "cloudy" | "rain" | "heavy_rain" | "thunderstorm" | "wind" | "hot" | "cold" | "mist";

export type PlantState = "working" | "idle" | "broken_down";

export type InstructionForm = "verbal" | "written";

export type DeliveryStatus = "received" | "expected_not_received";

export interface Weather {
  conditions: WeatherCondition[];
  note: string | null;
}

export interface Sections {
  weather_times?: WeatherTimes;
  labour?: LabourRow[];
  plant?: PlantRow[];
  instructions?: Instruction[];
  delays?: Delay[];
  visitors?: Visitor[];
  deliveries?: Delivery[];
}

export interface WeatherTimes {
  rain_started_at: string | null;
  rain_stopped_at: string | null;
  work_stopped_at: string | null;
  work_restarted_at: string | null;
  rain_gauge_mm: number | null;
  hours_lost: number | null;
  activities_affected: string | null;
  critical_path: boolean | null;
  delayed_effect: boolean | null;
}

export interface LabourRow {
  id: string;
  firm: string;
  own_team: boolean;
  trade: string | null;
  headcount: number;
  worker_ids?: string[];
}

export interface PlantRow {
  id: string;
  description: string;
  ref: string | null;
  state: PlantState;
  hours_working: number | null;
  hours_standing: number | null;
}

export interface Instruction {
  id: string;
  from_name: string;
  from_role: string | null;
  received_at: string;
  form: InstructionForm;
  description: string;
  reference: string | null;
}

export interface Delay {
  id: string;
  description: string;
  started_at: string | null;
  ended_at: string | null;
  hours_lost: number | null;
  cause: string | null;
}

export interface Visitor {
  id: string;
  name: string;
  organisation: string | null;
  purpose: string | null;
  arrived_at: string | null;
  left_at: string | null;
}

export interface Delivery {
  id: string;
  description: string;
  supplier: string | null;
  quantity: string | null;
  status: DeliveryStatus;
  received_at: string | null;
}

export interface RevisionRequest {
  base_revision_id?: string | null;
  weather: Weather;
  headcount: number;
  work_done: string;
  sections?: Sections;
  evidence_ids?: string[];
  client_saved_at: string;
  late_reason?: string | null;
  device_id?: string | null;
}

export interface AddendumRequest {
  reason: string;
  note?: string | null;
  sections?: Sections;
  evidence_ids?: string[];
  client_added_at: string;
  device_id?: string | null;
}

export interface Revision {
  id: string;
  site_id: string;
  diary_date: string;
  seq: number;
  base_revision_id: string | null;
  weather: Weather;
  headcount: number;
  headcount_suggested: number;
  work_done: string;
  sections: Sections;
  evidence_ids: string[];
  saved_by: string;
  client_saved_at: string;
  server_received_at: string;
  saved_after_close: boolean;
  received_after_close: boolean;
  late_reason: string | null;
  device_id: string | null;
}

export interface Addendum {
  id: string;
  site_id: string;
  diary_date: string;
  reason: string;
  note: string | null;
  sections: Sections;
  evidence_ids: string[];
  added_by: string;
  client_added_at: string;
  server_received_at: string;
  device_id: string | null;
}

export interface DiarySummary {
  site_id: string;
  diary_date: string;
  time_zone: string;
  opens_at: string;
  closes_at: string;
  grace_ends_at: string;
  closed: boolean;
  late: boolean;
  created_by: string;
  created_at: string;
  revision_count: number;
  conflict_revision_ids: string[];
  addendum_count: number;
  head_revision_id: string;
  weather: Weather;
  headcount: number;
  headcount_suggested: number;
}

export interface Diary {
  site_id: string;
  diary_date: string;
  time_zone: string;
  opens_at: string;
  closes_at: string;
  grace_ends_at: string;
  closed: boolean;
  late: boolean;
  created_by: string;
  created_at: string;
  revision_count: number;
  conflict_revision_ids: string[];
  addendum_count: number;
  current: Revision;
  headcount_suggested_now: number;
  addenda: Addendum[];
  toolbox_talk_ids: string[];
  incident_ids: string[];
}

export interface DiaryList {
  diaries: DiarySummary[];
}

export interface RevisionList {
  revisions: Revision[];
}

export interface FirmList {
  firms: string[];
}

export interface EvidenceProblem {
  error: string;
  evidence_ids: string[];
}

export interface SectionProblem {
  error: string;
  section: string;
  row: number;
  field: string;
}
```
