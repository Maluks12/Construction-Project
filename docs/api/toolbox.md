# Toolbox talks API

For the mobile app and the dashboard: toolbox talks, their photos, and the
people who signed. The backend keeps this file true: `TestAPIDoc` (in
`construction_db/internal/core/toolbox`) fails if the types below stop
matching the Go code, or if an error text the handlers send isn't quoted
here.

All endpoints need `Authorization: Bearer <access token>`. Errors are
`{"error": "<message>"}`.

Roles: owner and SHE officer ("leads") on any site of their organisation;
supervisor only on sites they've been added to (any other site is 404
`"site not found"`). Other roles get 403 `"forbidden"`.

## How toolbox talks work

- **A talk** has a topic, who gave it (`presenter_name`, free text), when it
  started and, optionally, when it ended, notes and photos. Its
  `talk_date` is the local date it started, in the site regime's
  `time_zone`: the site diary links talks by that date.
- **Topics.** `GET …/toolbox-topics` lists the site regime's topics, each
  with the regulation it relates to. A talk names one with `topic_key` and
  gets its title as `topic` (send `topic` to word it differently), or has
  a free-text `topic` and no key.
- **Signing.** Each person taps their name on the phone of whoever is
  recording the talk (`method: "tap"`). That phone's account is the
  `recorded_by`. A signature records the name as written, and optionally a
  firm and a clock number. It proves that this account recorded the person
  as present at that time; it doesn't prove who they are (SiteProof keeps no
  biometrics). No ID numbers.
- **Workers (M11).** Each signature shows `worker_id`, the register worker
  it counts as (workers.md):
  - the worker it is linked to: linked automatically when signed with a
    register worker's name, firm and clock number, or by hand by an owner or
    SHE officer;
  - else the register worker with those three;
  - else null.

  The signature itself never changes; a wrong link is unlinked.
- **Not signed (M11).** A talk's `not_signed` lists the register workers
  on the site who didn't sign it: absent from the talk or refused. Nothing
  is inferred about them; it is a list to act on. A worker is listed when a
  sign-in of theirs on the site (attendance.md), not retracted, hadn't ended
  when the talk began and either:
  - was standing at the talk's start, whatever its date: a night shift that
    signed in at 23:35 is listed for a 00:35 talk (decided Fri 20 Nov); or
  - was made on the talk's date before the talk ended (or now, while it has
    no end).

  The day register, absent list and headcount stay by sign-in date.
- **Paper registers.** Where people signed a paper sheet, photograph it, add
  the photo with `kind: "paper_register"`, then record each name with
  `method: "paper"`. Paper attendance is refused until the talk has a
  register photo.
- **When a signature counts:** from the talk's `started_at` to its
  `ended_at`, or, while it has no end, up to 12 hours after the start. That
  crosses midnight: a night-shift talk at 23:40 can be signed at 00:05. Not
  in the future either (5 minutes' leeway for device clocks). The talk still
  belongs to the day it started (`talk_date`), and so do its signatures in
  the diary's headcount.
- **End time:** not before `started_at`, at most 12 hours after it, and not
  before a signature that stands (a retracted one doesn't count).
- **The same person** can sign a talk once: same name, same firm and same
  clock number (case and spacing don't count). Two different people with
  the same name are told apart by their firm or clock number: the 409 for a
  second signature says so in `hint` (`AlreadySigned`).
- **Nothing is deleted.** A signature given by mistake is **retracted**, by
  the person who recorded it or a lead, with a reason. It stays on the talk,
  marked `retraction`, and leaves `signed_count`. Photos can be added, never
  removed.
- **Editing a talk.** Until the first signature, anything about it can
  change: send the whole talk again with `PUT`. After someone has signed,
  they signed for that topic at that time, so only `ended_at` can be set,
  once.
- **Ids come from the device.** Talks and signatures are `PUT` under an id
  the phone made up (a UUID v4), so retrying is always safe: an identical
  repeat answers 200.
- **Archived sites** keep their talks, and still take signatures and
  photos. A new talk is accepted only if it started before the site was
  archived.

## Endpoints

### `GET /v1/sites/:siteId/toolbox-topics` — the topic list

Owner, SHE officer, supervisor. `{"topics": [ToolboxTopic]}` from the site's
regime. 404 `"site not found"`.

### `PUT /v1/sites/:siteId/toolbox-talks/:talkId` — create or edit a talk

Body: `TalkRequest`. Owner, SHE officer, supervisor. The response is the
`Talk`. The talk's row is locked first, so an edit and a signature arriving
together take turns: an edit that comes second is refused if the signature
went first.

| Status | When |
|---|---|
| 201 | Created. `Talk` |
| 200 | Edited, or the same talk again: the stored `Talk` |
| 400 | `"talk id must be a UUID"`, `"topic_key is not a topic of this site's regime"`, `"topic is required without a topic_key"`, `"topic must be 1-200 characters"`, `"presenter_name must be 1-200 characters"`, `"notes must be 1-4000 characters"`, `"started_at is required"`, `"started_at is in the future; check the device clock"`, `"ended_at is in the future; check the device clock"`, `"ended_at must not be before started_at"`, `"ended_at must be at most 12 hours after started_at"`, `"device_id must be 1-200 bytes"` |
| 404 | `"site not found"` |
| 409 | `"talk id already used on another site"`; `"people have signed this talk; only its end time can be set, once"`; `"ended_at is before a signature already given"`; `"the site was archived before this talk started"` |

### `GET /v1/sites/:siteId/toolbox-talks/:talkId` — get a talk

Owner, SHE officer, supervisor. The `Talk` with its photos (oldest first) and
every signature, retracted ones included (in signing order). Works on an
archived site. 404 `"site not found"` or `"toolbox talk not found"`. Photos
show only their id, so this isn't a view of the evidence; open a photo with
the evidence `view` endpoint (logged).

### `GET /v1/sites/:siteId/toolbox-talks?date=&limit=` — a site's talks

Owner, SHE officer, supervisor. `{"talks": [TalkSummary]}`, newest first.
`date` (`YYYY-MM-DD`) keeps the talks of one local date. `limit` 1–200,
default 50. 400 `"date must be YYYY-MM-DD"` or `"limit must be 1-200"`
(checked before the site), 404 `"site not found"`.

### `PUT /v1/sites/:siteId/toolbox-talks/:talkId/photos/:evidenceId` — add a photo

Body: `PhotoRequest`. Owner, SHE officer, supervisor. The evidence must be
verified and on the talk's site. The response is the `Talk`.

| Status | When |
|---|---|
| 201 | Added. `Talk` |
| 200 | Already added with this kind: the `Talk` |
| 400 | `"evidence id must be a UUID"`, `"kind must be photo or paper_register"`, `"evidence not found on this site"`, `"evidence was rejected"` |
| 404 | `"site not found"`, `"toolbox talk not found"` |
| 409 | `"evidence not verified yet"` (retry once the upload is complete); `"photo already added to this talk as another kind"` |

### `PUT /v1/sites/:siteId/toolbox-talks/:talkId/attendance/:attendanceId` — sign

Body: `SignRequest`. Owner, SHE officer, supervisor; the caller is the
recorder. The response is the `Attendance`. Order of checks: the site, then
an identical repeat (200, before anything else), then the talk, the time,
the same person, the register photo.

| Status | When |
|---|---|
| 201 | Signed. `Attendance` |
| 200 | The same signature again (same recorder, same fields) |
| 400 | `"attendance id must be a UUID"`, `"name must be 1-200 characters"`, `"firm must be 1-200 characters"`, `"clock_number must be 1-50 characters"`, `"method must be tap or paper"`, `"client_signed_at is required"`, `"client_signed_at is in the future; check the device clock"`, `"signed before the talk started"`, `"signed after the talk ended"`, `"signed more than 12 hours after the talk started"`, `"device_id must be 1-200 bytes"` |
| 404 | `"site not found"`, `"toolbox talk not found"` |
| 409 | `"attendance id already used with different details"`; `"this person has already signed this talk"` (as `AlreadySigned`, with `hint`: `"a different person with the same name should add their firm or clock number"`); `"paper attendance needs a photo of the signed register; add it first"` |

### `POST /v1/sites/:siteId/toolbox-talks/:talkId/attendance/:attendanceId/retract` — retract a signature

Body: `RetractRequest`. The person who recorded the signature, an owner or a
SHE officer. The response is the `Attendance`, with `retraction`. Safe to
repeat with the same reason.

| Status | When |
|---|---|
| 201 | Retracted. `Attendance` |
| 200 | Already retracted with this reason |
| 400 | `"reason must be 1-500 characters"`, `"client_retracted_at is required"`, `"client_retracted_at is in the future; check the device clock"` |
| 403 | `"only the person who recorded a signature, an owner or a SHE officer can retract it"` |
| 404 | `"site not found"`, `"attendance not found"` |
| 409 | `"signature already retracted with another reason"` |

## Across the API

- **503 `"server busy, try again"`** with a `Retry-After` header (seconds):
  the request ran out of time. Retry after that many seconds.
- 401 `"authorization header required"`, `"invalid authorization header
  format"` or `"unauthorized"`: missing, malformed, expired or revoked
  token.
- 400 with the JSON decoder's own text: a body that isn't valid JSON or has
  a field of the wrong type.
- 500 `"internal error"`: a server fault.

## Types for `web/src/lib/types.ts`

Times are RFC 3339 strings in UTC with up to microsecond precision;
`talk_date` is a date (`YYYY-MM-DD`). Ids are UUIDs.

<!-- types -->
```ts
export type AttendanceMethod = "tap" | "paper";

export type TalkPhotoKind = "photo" | "paper_register";

export interface ToolboxTopic {
  key: string;
  title: string;
  source_ref: string;
}

export interface ToolboxTopicList {
  topics: ToolboxTopic[];
}

export interface TalkRequest {
  topic_key?: string | null;
  topic?: string | null;
  presenter_name: string;
  notes?: string | null;
  started_at: string;
  ended_at?: string | null;
  device_id?: string | null;
}

export interface PhotoRequest {
  kind: TalkPhotoKind;
}

export interface SignRequest {
  name: string;
  firm?: string | null;
  clock_number?: string | null;
  method: AttendanceMethod;
  client_signed_at: string;
  device_id?: string | null;
}

export interface RetractRequest {
  reason: string;
  client_retracted_at: string;
}

export interface TalkSummary {
  id: string;
  site_id: string;
  topic_key: string | null;
  topic: string;
  presenter_name: string;
  notes: string | null;
  started_at: string;
  ended_at: string | null;
  talk_date: string;
  time_zone: string;
  recorded_by: string;
  device_id: string | null;
  created_at: string;
  updated_at: string | null;
  signed_count: number;
  photo_count: number;
}

export interface Talk {
  id: string;
  site_id: string;
  topic_key: string | null;
  topic: string;
  presenter_name: string;
  notes: string | null;
  started_at: string;
  ended_at: string | null;
  talk_date: string;
  time_zone: string;
  recorded_by: string;
  device_id: string | null;
  created_at: string;
  updated_at: string | null;
  signed_count: number;
  photos: TalkPhoto[];
  attendance: Attendance[];
  not_signed: NotSigned[];
}

export interface TalkList {
  talks: TalkSummary[];
}

export interface TalkPhoto {
  evidence_id: string;
  kind: TalkPhotoKind;
  added_by: string;
  added_at: string;
}

export interface Attendance {
  id: string;
  talk_id: string;
  name: string;
  firm: string | null;
  clock_number: string | null;
  method: AttendanceMethod;
  recorded_by: string;
  client_signed_at: string;
  signed_at: string;
  device_id: string | null;
  retraction: Retraction | null;
  worker_id: string | null;
}

export interface Retraction {
  reason: string;
  retracted_by: string;
  client_retracted_at: string;
  retracted_at: string;
}

export interface NotSigned {
  worker_id: string;
  name: string;
  firm: string | null;
  clock_number: string | null;
  sign_in_id: string;
}

export interface AlreadySigned {
  error: string;
  hint: string;
}
```
