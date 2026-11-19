# Site sign-in API

For the mobile app (the supervisor's phone or a site tablet) and the
dashboard: daily sign-in and sign-out, paper sign-in sheets, the day's
register with who was expected and didn't come, and the site roster. The
backend keeps this file true: `TestAPIDoc` (in
`construction_db/internal/core/attendance`) fails if the types below stop
matching the Go code, or if an error text the handlers send isn't quoted
here.

All endpoints need `Authorization: Bearer <access token>`. Errors are
`{"error": "<message>"}`.

Roles: owner and SHE officer on any site of their organisation; supervisor
only on sites they've been added to (any other site is 404 `"site not
found"`). Other roles get 403 `"forbidden"`.

## How sign-in works (M11 D11–D18)

- **Every sign-in is a register worker** (workers.md).
  - A tap on a name sends that worker's `worker_id`.
  - Someone not on the register is added on the spot: send `new_worker`
    (name, optional firm and clock number) with a new `worker_id` the phone
    made up. If the register already has someone with the same name, firm
    and clock number, that worker is signed in instead, and the answer says
    `matched_existing: true` with their name.
  - **"Different person":** show who was matched. If it's someone else,
    retract that sign-in (reason "Different person") and sign in again with
    a new `worker_id` and a clock number. A retracted sign-in counts nowhere.
- **The phone's time counts** (`client_signed_in_at`): it decides the day
  (`sign_in_date`, in the site regime's time zone) and the order. Up to 5
  minutes ahead of the server is allowed.
  - A sign-in queued offline is accepted whenever it arrives.
  - One received more than 48 hours after its day ended shows
    `received_after_close: true`.
  - A diary already saved keeps the headcount it was given;
    `headcount_suggested_now` includes the late arrival.
- **Sign-out is optional.** A sign-in's presence (`ends_at`) ends at the
  earliest of:
  - its sign-out;
  - 16 hours after it (night shifts work: nothing happens at midnight);
  - the worker's next sign-in on the site.

  Once it has ended without a sign-out it shows `lapsed: true`, and no
  sign-out can be recorded any more.
- **One sign-in at a time per worker per site, whatever order they arrive
  in.**
  - Tapping someone who is already signed in (another phone did it, or they
    never signed out) is 409 `AlreadySignedIn` with the standing sign-in's
    id: show it, don't re-queue.
  - A sign-in whose time falls inside one of the worker's other sign-ins on
    the site is refused the same way.
  - An earlier sign-in arriving late from an offline phone (07:00 after 08:00
    was recorded) is accepted and simply ends at the next one. It can be
    signed out before 08:00, not after.
- **Two sites at once** is allowed: phones are often offline and can't know.
  The day's register lists `overlaps` with other sites.
- **Never refused for papers** (Rev 4's "clock-in block"): a sign-in carries
  `warnings` (`no_valid_medical`, `medical_unfit`, `competency_expired`) to
  show, and is recorded anyway.
- **Paper fallback:**
  1. Photograph the signed sheet and upload it as evidence (evidence.md).
  2. Add it to the day: `PUT …/sign-in-sheets/:date/:evidenceId`.
  3. Record each name with `method: "paper"`.

  Paper sign-ins before that day has a sheet photo are refused.
- **Expected = the roster.** A worker's sign-in on a site puts them on its
  roster; owners, SHE officers and the site's supervisors add or remove
  people by hand.
  - **Who is absent:** the day's `absent` list is the roster as it stood
    that day, showing workers by their names then:
    - entries added before the day ended and not removed before it began;
    - whose worker was active;
    - and who didn't sign in.

    A past day's list never changes when the roster or a worker changes
    later.
  - **Lapse:** an entry a sign-in added stops counting after 14 days with no
    sign-in on the site (the regime's `roster_lapse_days`). Entries added by
    hand stay until removed.
  - **On the roster list,** `expected` says whether an entry counts today,
    and `last_sign_in_on` is the worker's last sign-in there.
- **Headcount:** distinct people who signed in that day, plus anyone who only
  signed a toolbox talk (matched on name, firm and clock number, or the
  signature's link to a worker). The diary shows it as the suggestion; the
  supervisor's own number is what's saved (diary.md).
- **Nothing is deleted.** A mistake is retracted, by the person who recorded
  it or an owner or SHE officer, with a reason.

## Endpoints

### `PUT /v1/sites/:siteId/sign-ins/:signInId` — sign in

Body: `SignInRequest`. The response is the `SignIn`. Order of checks: the
body, the site, an identical repeat (200, before anything else), the worker
(or the on-the-spot entry), a sign-in already standing, the paper sheet.

| Status | When |
|---|---|
| 201 | Signed in. `SignIn` |
| 200 | The same sign-in again |
| 400 | `"sign-in id must be a UUID"`, `"worker_id must be a UUID"`, `"method must be tap or paper"`, `"client_signed_in_at is required"`, `"client_signed_in_at is in the future; check the device clock"`, `"device_id must be 1-200 bytes"`; a `FieldProblem` for `new_worker` (workers.md: `"new_worker.name"` etc.) |
| 404 | `"site not found"`, `"worker not found"` |
| 409 | `AlreadySignedIn`: `"this worker is already signed in on this site at that time"` with `sign_in_id`; `"sign-in id already used with different details"`; `"id already used with different details"` (the new worker's id); `"paper sign-in needs a photo of that day's signed sheet; add it first"`; `"the site was archived before this sign-in"` |

### `POST /v1/sites/:siteId/sign-ins/:signInId/sign-out` — sign out

Body: `SignOutRequest`. Anyone who can see the site.

| Status | When |
|---|---|
| 201 | Signed out. `SignIn` |
| 200 | The same sign-out again |
| 400 | `"method must be tap or paper"`, `"client_signed_out_at is required"`, `"client_signed_out_at is in the future; check the device clock"`, `"signed out before signing in"`, `"signed out more than 16 hours after signing in"` |
| 404 | `"sign-in not found"` |
| 409 | `"already signed out at another time"`; `"sign-in was retracted"`; `"signed out after this worker's next sign-in on the site"` |

### `POST /v1/sites/:siteId/sign-ins/:signInId/retract` — retract a sign-in

Body: `RetractRequest`. The person who recorded it, an owner or a SHE
officer. 201 retracted, 200 the same again; 400 `"reason must be 1-500 characters"`; 403 `"only the person who recorded a sign-in, an owner or a SHE officer can retract it"`; 404 `"sign-in not found"`; 409 `"sign-in already retracted with another reason"`.

### `PUT /v1/sites/:siteId/sign-in-sheets/:date/:evidenceId` — add a sheet photo

No body. The evidence must be verified and on the site. 201 `Sheet`, 200
already added; 400 `"date must be YYYY-MM-DD"`, `"evidence id must be a UUID"`, `"evidence not found on this site"`, `"evidence was rejected"`; 409 `"evidence not verified yet"`.

### `GET /v1/sites/:siteId/sign-ins?date=` — the day's register

`Day` for the local date (`YYYY-MM-DD`, today in the site's zone when left
out): its sign-ins with warnings, sheets, `absent`, `overlaps` and
`headcount`. 400 `"date must be YYYY-MM-DD"`.

### `GET /v1/sites/:siteId/roster?all=` — the roster

`{"roster": [RosterEntry]}`: who is expected; `all=true` includes removed
entries.

### `PUT /v1/sites/:siteId/roster/:workerId` — add to the roster

201 `RosterEntry`, 200 already on it; 404 `"worker not found"`.

### `POST /v1/sites/:siteId/roster/:workerId/remove` — remove from the roster

Body: `RemoveRequest`. 200 with the entry, removed; 400 `"reason must be 1-500 characters"`; 404 `"worker is not on this site's roster"`.

## Across the API

- **503 `"server busy, try again"`** with a `Retry-After` header (seconds).
- 401 `"authorization header required"`, `"invalid authorization header
  format"` or `"unauthorized"`.
- 400 with the JSON decoder's own text: a body that isn't valid JSON.
- 500 `"internal error"`.

## Types for `web/src/lib/types.ts`

Times are RFC 3339 strings in UTC; dates `YYYY-MM-DD`. Ids are UUIDs.
`WorkerRequest`, `FieldProblem` and `SignInWarning` are in workers.md.

<!-- types -->
```ts
export type SignInMethod = "tap" | "paper";

export interface SignInRequest {
  worker_id: string;
  new_worker?: WorkerRequest | null;
  method: SignInMethod;
  client_signed_in_at: string;
  device_id?: string | null;
}

export interface SignOutRequest {
  method: SignInMethod;
  client_signed_out_at: string;
}

export interface RetractRequest {
  reason: string;
}

export interface RemoveRequest {
  reason: string;
}

export interface SignIn {
  id: string;
  site_id: string;
  worker_id: string;
  name: string;
  firm: string | null;
  clock_number: string | null;
  method: SignInMethod;
  matched_existing: boolean;
  recorded_by: string;
  client_signed_in_at: string;
  received_at: string;
  sign_in_date: string;
  time_zone: string;
  received_after_close: boolean;
  device_id: string | null;
  sign_out: SignOut | null;
  retraction: Retraction | null;
  ends_at: string;
  lapsed: boolean;
  warnings: SignInWarning[];
}

export interface SignOut {
  method: SignInMethod;
  signed_out_by: string;
  client_signed_out_at: string;
  received_at: string;
}

export interface Retraction {
  reason: string;
  retracted_by: string;
  retracted_at: string;
}

export interface Sheet {
  site_id: string;
  sheet_date: string;
  evidence_id: string;
  added_by: string;
  added_at: string;
}

export interface Overlap {
  sign_in_id: string;
  other_site_id: string;
  other_sign_in_id: string;
}

export interface Day {
  site_id: string;
  date: string;
  sign_ins: SignIn[];
  sheets: Sheet[];
  absent: RosterEntry[];
  overlaps: Overlap[];
  headcount: number;
}

export interface RosterEntry {
  id: string;
  worker_id: string;
  name: string;
  firm: string | null;
  clock_number: string | null;
  active: boolean;
  added_via: "sign_in" | "manual";
  added_by: string;
  added_at: string;
  removed_by: string | null;
  removed_at: string | null;
  remove_reason: string | null;
  last_sign_in_on: string | null;
  expected: boolean;
}

export interface RosterList {
  roster: RosterEntry[];
}

export interface AlreadySignedIn {
  error: string;
  sign_in_id: string;
}
```
