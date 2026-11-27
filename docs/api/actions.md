# Corrective actions API

For the dashboard and the mobile app: corrective actions raised from
inspection findings and incidents, their owner and due date, close-outs with
photos, verification, the overdue list, and waivers of low and medium
findings. The backend keeps this file true: `TestAPIDoc` (in
`construction_db/internal/core/actions`) fails if the types below stop
matching the Go code, or if an error text the handlers send isn't quoted
here.

All endpoints need `Authorization: Bearer <access token>`. Errors are
`{"error": "<message>"}`.

Roles: owner and SHE officer ("leads") on any site of their organisation;
supervisor only on sites they've been added to (any other site is 404
`"site not found"`). Other roles get 403 `"forbidden"`.

## How corrective actions work

- **From a finding or an incident.** An action is raised from exactly one
  source: a finding (an inspection's failed item, `GET
  /v1/sites/:siteId/findings`) or an incident version (`docs/api/incidents.md`).
  Raising is always a person's choice; nothing is created automatically.
- **Every finding is addressed.** A finding with no action and no waiver is
  **unactioned**: the findings list shows `coverage` and filters on it.
  A finding may have several actions.
- **Waivers.** A lead may mark a **low or medium** finding "no action
  needed", with a reason. Critical and high findings can't be waived: a fix
  made on the spot is raised as an action and closed out with a photo.
- **Owner and responsible party.** The owner is accountable: an owner, a SHE
  officer, or a supervisor added to the site. `responsible_party` is free
  text for whoever does the work off SiteProof (e.g. "ABC Scaffolding,
  J. Dlamini").
  **`owner_eligible`** (ONBOARD) is whether the owner can still act on the
  action: an active member who could be made its owner now (an owner or SHE
  officer, or a supervisor on its site), the rule raise and reassign use. It
  turns false when the owner is suspended or removed (`members.md`), moved to
  another role, or taken off the site. Nothing is reassigned automatically:
  show such an action as needing a new owner and offer `reassign`. The owner
  gets no more reminders about it; the escalation to SHE officers and owners
  carries on.
- **Due dates.** A date in the site regime's time zone (`time_zone`), due by
  the end of that day: `due_at` is the next local midnight, and from then on
  an open action is **overdue**. The default comes from the severity (below).
  At raise, any date up to the default is fine; a later one takes a lead and
  a `due_reason`. Afterwards, the raiser, the owner or a lead may bring it
  earlier; only a lead may move it later, with a reason.
- **Close-out, then verification.** The owner (or a lead) submits a
  close-out with at least one photo: the action **awaits verification**. A
  lead verifies it (**closed**) or rejects it with a reason (open again). A
  lead may reopen a closed action or cancel one that isn't closed, each with
  a reason. A lead verifying their own close-out is allowed, and the event
  says `self_verified: true`.
- **Close-out photos must be able to show the fix**: verified evidence on the
  site, not taken before the source (the time the failed item was answered,
  or the incident occurred; 5 minutes' leeway for device clocks), and not one
  of the source's own photos. A photo taken after the source but before the
  action was raised (fixed on the spot, recorded afterwards) is accepted and
  marked `captured_before_raised`.
- **Nothing is edited.** Every change is an event, recorded with who, when
  (device and server time), why, and the action's state after it. The
  action's current state is always its latest event's.
- **Ids come from the device.** Every write is a `PUT` under an id the phone
  made up (a UUID v4), so retrying is always safe: an identical repeat
  answers 200 with the stored result.

### Corrected inspections

A finding is **superseded** once its inspection has a completed correction
(`corrected_by` on the finding, `source_corrected_by` on its actions). Then:

- no new action or waiver on it; act on the correction's finding;
- its actions stay as they are, never closed silently;
- they also cover the correction's finding for the same item **if the
  correction's severity is the same or lower**. If it's higher, the
  correction's finding is unactioned and needs its own action (the old one
  doesn't cover it, even if re-dated), and, separately, the old action shows
  `review: "severity_raised"` for a lead to re-date or cancel.
  If the correction doesn't fail that item, it shows `review:
  "item_not_failed"`. A waiver carries over the same way, never to a higher
  severity.

### Default due dates

From the local date the action was raised (`client_raised_at`), by severity
(see the definitions in `docs/api/inspections.md`):

| severity | due (days after the day raised) |
|---|---|
| `low` | 14 |
| `medium` | 7 |
| `high` | 1 |
| `critical` | 0 |

These are product defaults, not statutory periods.

### States

`state` is worked out from `status` and the due time when the action is
read:

| state | meaning |
|---|---|
| `open` | not closed out, due time not reached |
| `overdue` | not closed out, past `due_at` |
| `awaiting_verification` | a close-out is waiting for a lead (`awaiting_since` says since when); never overdue |
| `closed` | verified; the close-out was submitted before `due_at` |
| `closed_late` | verified; the close-out was submitted (device time) at or after `due_at` |
| `cancelled` | cancelled by a lead |

### Review flags

| review | meaning |
|---|---|
| `severity_raised` | the newest completed correction fails the item at a higher severity. The correction's finding is unactioned and needs its own action: this one doesn't cover it, even if re-dated. Separately, a lead re-dates or cancels this one |
| `item_not_failed` | the newest completed correction doesn't fail the item; consider cancelling |

## Endpoints

### `PUT /v1/sites/:siteId/actions/:actionId` — raise an action

Body: `RaiseRequest`, with exactly one of `finding` or `incident`. Owner, SHE
officer, supervisor. The response is the `Action`.

- A finding's action takes the finding's severity; leave `severity` out (or
  send the same). An incident's action needs `severity`.
- An incident's action is raised against its **latest** version. Later
  versions never change the action; it keeps `incident_version`, and
  `incident_latest_version` shows where the incident is now.
- `due_on` defaults to the severity's default date. `due_reason` is stored on
  the raise event.
- Order of checks: the site, then an identical repeat (200, before anything
  else, so a retry after the site was archived or the finding corrected still
  succeeds), then the site archived, the source, the owner, the due date.

| Status | When |
|---|---|
| 201 | Raised. `Action` |
| 200 | The same raise again (same person, same fields): the stored `Action` as it is now |
| 400 | `"action id must be a UUID"`, `"give exactly one of finding or incident"`, `"finding needs inspection_id (a UUID) and item_key"`, `"incident needs incident_id (a UUID) and version (1 or more)"`, `"severity must be low, medium, high or critical"`, `"severity is required for an incident's action"`, `"a finding's action has the finding's severity; leave severity out"`, `"title must be 1-200 characters"`, `"description must be 1-2000 characters"`, `"owner_id must be a UUID"`, `"responsible_party must be 1-200 characters"`, `"due_on must be a date, YYYY-MM-DD"`, `"due_reason is required for a due date later than the default (1-500 characters)"`, `"client_raised_at is required"`, `"client_raised_at is in the future; check the device clock"`, `"device_id must be 1-200 bytes"`, `"finding not found on this site"`, `"incident version not found on this site"`, `"owner must be an owner, SHE officer or supervisor on this site"`, `"due_on must not be before the day the action is raised"` |
| 403 | `"only an owner or a SHE officer can set a due date later than the default or move it later"` |
| 404 | `"site not found"` |
| 409 | `"action id already used with different details"` (also when the id is on another site, or is an event's id); `"site is archived"`; `"finding is superseded by a correction; act on the correction's finding"`; `"raise it against the incident's latest version"` (as `NotLatestVersion`, with `latest_version`) |

### `GET /v1/sites/:siteId/actions/:actionId` — get an action

Owner, SHE officer, supervisor. The `Action` with every event (oldest first)
and every close-out. Works on an archived site. 404 `"site not found"` or
`"action not found"`. Close-out photos show only their id and capture time,
so this isn't a view of the evidence; open a photo with the evidence
`view` endpoint (logged).

### `GET /v1/sites/:siteId/actions?state=&owner=&source=&owner_eligible=&limit=` — a site's register

Owner, SHE officer, supervisor. `ActionList` (`{"actions": []}` when empty,
never `null`), due soonest first, then oldest raised. Works on an archived
site.

- `state` is one of the states above. **`state=overdue` is the overdue
  list.**
- `owner` is `me` or a user id; `source` is `finding` or `incident`.
- `owner_eligible=false` lists the actions whose owner can no longer act on
  them (suspended, removed, another role, off the site; ONBOARD): they need a
  new owner (`reassign`). `owner_eligible=true` the rest.
- `limit` is 1–200, default 50. No paging yet.

| Status | When |
|---|---|
| 200 | `ActionList` |
| 400 | `"state must be open, overdue, awaiting_verification, closed, closed_late or cancelled"`, `"source must be finding or incident"`, `"owner must be me or a user id"`, `"owner_eligible must be true or false"`, `"limit must be 1-200"` |
| 404 | `"site not found"` |

### `GET /v1/actions?state=&owner=&source=&owner_eligible=&limit=` — the register across sites

The same as the site register, across every site the caller can open: all
of the organisation's for an owner or SHE officer (archived ones included),
the sites they were added to for a supervisor. `/v1/actions?state=overdue`
is the organisation's overdue list; add `owner=me` for "my overdue actions".
`/v1/actions?owner_eligible=false` is every action whose owner can no
longer act on it.
The same 400 texts as above.

### `PUT /v1/sites/:siteId/actions/:actionId/events/:eventId` — change an action

Body: `EventRequest`. Owner, SHE officer, supervisor (who may do what is
below). The response is the `Action`. Each `kind` takes only its own fields;
anything else is 400 `"a field was given that this kind doesn't take"`.

| kind | fields | who | |
|---|---|---|---|
| `reassign` | `owner_id`, `responsible_party` (null for none) | the raiser or a lead | open or awaiting verification |
| `move_due` | `due_on`; `reason` to move it later | earlier: the raiser, the owner or a lead; later: a lead | not before today (local) |
| `verify` | `closeout_id` (the one awaiting) | a lead | closes it |
| `reject` | `closeout_id`, `reason` | a lead | open again |
| `reopen` | `reason` | a lead | a closed action only |
| `cancel` | `reason` | a lead | anything not closed |

Order of checks: the site, an identical repeat of the event id (200), the
action, closed or cancelled, who may, then the request against the action's
state.

| Status | When |
|---|---|
| 201 | Recorded. `Action` |
| 200 | The same event again: the stored `Action` as it is now |
| 400 | `"event id must be a UUID"`, `"kind must be reassign, move_due, verify, reject, reopen or cancel"`, `"client_at is required"`, `"client_at is in the future; check the device clock"`, `"reason must be 1-500 characters"`, `"reason is required to reject, reopen or cancel (1-500 characters)"`, `"reason is required to move a due date later (1-500 characters)"`, `"a field was given that this kind doesn't take"`, `"owner_id must be a UUID"`, `"responsible_party must be 1-200 characters"`, `"due_on must be a date, YYYY-MM-DD"`, `"closeout_id must be a UUID"`, `"device_id must be 1-200 bytes"`, `"nothing would change"`, `"owner must be an owner, SHE officer or supervisor on this site"`, `"due_on must not be in the past"` |
| 403 | `"only the action's raiser, an owner or a SHE officer can reassign it"`, `"only the action's raiser, its owner, an owner or a SHE officer can move its due date earlier"`, `"only an owner or a SHE officer can set a due date later than the default or move it later"`, `"only an owner or a SHE officer can verify, reject, reopen or cancel an action"` |
| 404 | `"site not found"`, `"action not found"` |
| 409 | `"event id already used with different details"`; `"action is closed; reopen it first"`; `"action is cancelled"`; `"action has no close-out awaiting verification"`; `"closeout_id is not the close-out awaiting verification"`; `"only a closed action can be reopened"` |

### `PUT /v1/sites/:siteId/actions/:actionId/closeouts/:closeoutId` — close out

Body: `CloseoutRequest`. The action's owner, or a lead. The response is the
`Action`, now awaiting verification. Upload and complete the photos first
(`docs/api/evidence.md`).

| Status | When |
|---|---|
| 201 | Submitted. `Action` |
| 200 | The same close-out again: the stored `Action` as it is now |
| 400 | `"close-out id must be a UUID"`, `"note must be 1-2000 characters"`, `"a close-out needs at least one photo"`, `"at most 20 photos"`, `"evidence_ids must be UUIDs"`, `"evidence_ids must not repeat"`, `"client_submitted_at is required"`, `"client_submitted_at is in the future; check the device clock"`, `"device_id must be 1-200 bytes"`; photos (as `EvidenceProblem`, naming them): `"evidence not found on this site"`, `"evidence was rejected"`, `"evidence was taken before the action's source, so it can't show the fix"`, `"evidence is one of the source's own photos; take a photo of the fix"` |
| 403 | `"only the action's owner, an owner or a SHE officer can close it out"` |
| 404 | `"site not found"`, `"action not found"` |
| 409 | `"close-out id already used with different details"`; `"action is closed; reopen it first"`; `"action is cancelled"`; `"a close-out is already awaiting verification"`; `"evidence not verified yet"` (as `EvidenceProblem`: still uploading, retry later) |

### `PUT /v1/sites/:siteId/waivers/:waiverId` — waive a finding

Body: `WaiverRequest`. Owner or SHE officer. A **low or medium** finding
that isn't superseded; one live waiver per finding. The response is the
`Waiver`.

| Status | When |
|---|---|
| 201 | Waived. `Waiver` |
| 200 | The same waiver again: the stored `Waiver` |
| 400 | `"waiver id must be a UUID"`, `"finding needs inspection_id (a UUID) and item_key"`, `"reason must be 1-500 characters"`, `"client_waived_at is required"`, `"client_waived_at is in the future; check the device clock"`, `"device_id must be 1-200 bytes"`, `"finding not found on this site"`, `"only a low or medium finding can be waived; raise an action instead"` |
| 403 | `"only an owner or a SHE officer can waive a finding or retract a waiver"` |
| 404 | `"site not found"` |
| 409 | `"waiver id already used with different details"`; `"site is archived"`; `"finding is superseded by a correction; act on the correction's finding"`; `"finding already has a waiver; retract it first"` |

### `POST /v1/sites/:siteId/waivers/:waiverId/retract` — take a waiver back

Body: `RetractWaiverRequest`. Owner or SHE officer. The finding is
unactioned again; the waiver stays on record with who, when and why it was
retracted. Repeating with the same reason answers 200.

| Status | When |
|---|---|
| 201 | Retracted. `Waiver` |
| 200 | Already retracted with this reason |
| 400 | `"reason must be 1-500 characters"` |
| 403 | `"only an owner or a SHE officer can waive a finding or retract a waiver"` |
| 404 | `"site not found"`, `"waiver not found"` |
| 409 | `"waiver was already retracted with a different reason"` |

## Across the API

- 400 `"text must not contain NUL characters"`: a NUL character (`\u0000`) anywhere in the JSON body (any value or key, however nested), the query string or the path, on every endpoint, checked before the token (REG4 review). Postgres text can't store one.
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
and other `_on` fields are dates (`YYYY-MM-DD`). Ids are UUIDs. `Severity`
is in `docs/api/inspections.md`.

<!-- types -->
```ts
export type ActionState = "open" | "overdue" | "awaiting_verification" | "closed" | "closed_late" | "cancelled";

export type ActionStatus = "open" | "awaiting_verification" | "closed" | "cancelled";

export type ActionSource = "finding" | "incident";

export type ActionEventKind = "raised" | "reassigned" | "due_changed" | "cancelled" | "closeout_submitted" | "verified" | "rejected" | "reopened";

export type ActionRequestKind = "reassign" | "move_due" | "verify" | "reject" | "reopen" | "cancel";

export type ActionReview = "severity_raised" | "item_not_failed";

export interface FindingRef {
  inspection_id: string;
  item_key: string;
}

export interface IncidentRef {
  incident_id: string;
  version: number;
}

export interface RaiseRequest {
  finding?: FindingRef | null;
  incident?: IncidentRef | null;
  severity?: Severity | null;
  title: string;
  description?: string | null;
  owner_id: string;
  responsible_party?: string | null;
  due_on?: string | null;
  due_reason?: string | null;
  client_raised_at: string;
  device_id?: string | null;
}

export interface EventRequest {
  kind: ActionRequestKind;
  owner_id?: string | null;
  responsible_party?: string | null;
  due_on?: string | null;
  closeout_id?: string | null;
  reason?: string | null;
  client_at: string;
  device_id?: string | null;
}

export interface CloseoutRequest {
  note?: string | null;
  evidence_ids: string[];
  client_submitted_at: string;
  device_id?: string | null;
}

export interface ActionSummary {
  id: string;
  site_id: string;
  source: ActionSource;
  inspection_id: string | null;
  item_key: string | null;
  source_corrected_by: string | null;
  incident_id: string | null;
  incident_version: number | null;
  incident_latest_version: number | null;
  review: ActionReview | null;
  severity: Severity;
  title: string;
  description: string | null;
  raised_by: string;
  client_raised_at: string;
  raised_at: string;
  device_id: string | null;
  time_zone: string;
  source_at: string;
  default_due_on: string;
  status: ActionStatus;
  state: ActionState;
  owner_id: string;
  owner_eligible: boolean;
  responsible_party: string | null;
  due_on: string;
  due_at: string;
  pending_closeout_id: string | null;
  awaiting_since: string | null;
  closed_at: string | null;
}

export interface Action {
  id: string;
  site_id: string;
  source: ActionSource;
  inspection_id: string | null;
  item_key: string | null;
  source_corrected_by: string | null;
  incident_id: string | null;
  incident_version: number | null;
  incident_latest_version: number | null;
  review: ActionReview | null;
  severity: Severity;
  title: string;
  description: string | null;
  raised_by: string;
  client_raised_at: string;
  raised_at: string;
  device_id: string | null;
  time_zone: string;
  source_at: string;
  default_due_on: string;
  status: ActionStatus;
  state: ActionState;
  owner_id: string;
  owner_eligible: boolean;
  responsible_party: string | null;
  due_on: string;
  due_at: string;
  pending_closeout_id: string | null;
  awaiting_since: string | null;
  closed_at: string | null;
  events: ActionEvent[];
  closeouts: Closeout[];
}

export interface ActionEvent {
  id: string;
  seq: number;
  kind: ActionEventKind;
  actor_id: string;
  client_at: string;
  recorded_at: string;
  device_id: string | null;
  reason: string | null;
  status: ActionStatus;
  owner_id: string;
  responsible_party: string | null;
  due_on: string;
  closeout_id: string | null;
  self_verified: boolean | null;
}

export interface Closeout {
  id: string;
  note: string | null;
  submitted_by: string;
  client_submitted_at: string;
  submitted_at: string;
  device_id: string | null;
  evidence: CloseoutEvidence[];
  decision: "verified" | "rejected" | null;
  late: boolean;
  recorded_after_due: boolean;
}

export interface CloseoutEvidence {
  evidence_id: string;
  client_captured_at: string;
  captured_before_raised: boolean;
}

export interface ActionList {
  actions: ActionSummary[];
}

export interface WaiverRequest {
  inspection_id: string;
  item_key: string;
  reason: string;
  client_waived_at: string;
  device_id?: string | null;
}

export interface RetractWaiverRequest {
  reason: string;
}

export interface Waiver {
  id: string;
  site_id: string;
  inspection_id: string;
  item_key: string;
  severity: Severity;
  reason: string;
  waived_by: string;
  client_waived_at: string;
  waived_at: string;
  device_id: string | null;
  retracted_by: string | null;
  retracted_at: string | null;
  retract_reason: string | null;
}

export interface EvidenceProblem {
  error: string;
  evidence_ids: string[];
}

export interface NotLatestVersion {
  error: string;
  latest_version: number;
}
```
