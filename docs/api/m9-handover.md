# M9 handover for the dashboard (Sun 15 Nov 2026)

Everything the backend added in M9 that the dashboard needs. The details,
status codes and exact error texts are in `actions.md` (and, for the
findings list, `inspections.md`); their TypeScript blocks are checked
against the Go code, so copy types from there.

## Types to add to `web/src/lib/types.ts`

- **Actions:** `ActionSummary`, `Action`, `ActionEvent`, `Closeout`,
  `CloseoutEvidence`, `ActionList`, and the unions `ActionState`,
  `ActionStatus`, `ActionSource`, `ActionEventKind`, `ActionRequestKind`,
  `ActionReview`.
- **Waivers:** `Waiver`.
- **Requests:** `RaiseRequest` (with `FindingRef` or `IncidentRef`),
  `EventRequest`, `CloseoutRequest`, `WaiverRequest`, `RetractWaiverRequest`.
- **Error bodies with extra fields:** `EvidenceProblem` (`evidence_ids`, as
  in the other references) and `NotLatestVersion` (`latest_version`).
- **Changed:** `Finding` (in `inspections.md`) gains `corrected_by`,
  `coverage`, `covered_by` and `waiver_id`; new union `Coverage`.

## New endpoints

| Endpoint | For |
|---|---|
| `GET /v1/sites/:id/actions` | A site's register; `?state=overdue` is the overdue list |
| `GET /v1/actions` | The register across every site the user can open; `?state=overdue&owner=me` is "my overdue actions" |
| `GET /v1/sites/:id/actions/:actionId` | One action with its events and close-outs |
| `PUT /v1/sites/:id/actions/:actionId` | Raise an action from a finding or an incident version |
| `PUT /v1/sites/:id/actions/:actionId/events/:eventId` | Reassign, move the due date, verify, reject, reopen, cancel |
| `PUT /v1/sites/:id/actions/:actionId/closeouts/:closeoutId` | Close out with photos |
| `PUT /v1/sites/:id/waivers/:waiverId` | Waive a low or medium finding (owner, SHE officer) |
| `POST /v1/sites/:id/waivers/:waiverId/retract` | Take a waiver back (owner, SHE officer) |

Changed: `GET /v1/sites/:id/findings` takes `?coverage=`
(`unactioned`, `actioned`, `waived`, `superseded`).

## What the dashboard should do

- **Every finding needs an answer.** Show `coverage` on each finding, and
  offer "raise an action" on unactioned ones. `?coverage=unactioned` is the
  to-do list. Offer "no action needed" (a waiver) on low and medium
  findings to owners and SHE officers only, with a reason.
- **Superseded findings** (`corrected_by` set, `coverage: "superseded"`):
  show them as history beside the correction's finding, with no "raise"
  button. Their actions stay, and appear on the correction's finding
  through `covered_by` when the correction's severity is the same or lower.
- **Review flags.** An action with `review: "severity_raised"` was raised on
  a finding a correction made worse. Two separate things follow: the
  correction's finding is unactioned and needs its own action (the old
  action doesn't cover it, even if re-dated), so offer "raise an action"
  there; and the old action is shown to leads with "re-date" (move the due
  date earlier) and "cancel". `"item_not_failed"`: the correction passed the
  item; suggest cancelling.
- **Due dates:** show "by the end of `due_on`" in `time_zone`, not a clock
  time. When raising, preselect the severity's default (the table in
  `actions.md`); any earlier date is fine. A later one is for owners and SHE
  officers only and needs `due_reason`, so don't offer it to supervisors.
- **States and colours:** `overdue` is the alarm; `awaiting_verification`
  is never overdue, but `awaiting_since` shows how long it has waited (a
  stalled verification). `closed_late` is closed, flagged.
- **Close-out:** upload and complete the photos first (`evidence.md`), then
  send them. Refused photos come back in `evidence_ids` with the reason:
  taken before the problem (`"evidence was taken before the action's
  source…"`), or the problem's own photo. `captured_before_raised` on a
  close-out photo means it was fixed on the spot; show it, it isn't an
  error.
- **Verification:** a lead verifies (or rejects, with a reason) the close-out
  in `pending_closeout_id`, sending its id as `closeout_id`. A lead's own
  close-out can be verified, and the event shows `self_verified: true`: show
  that, so nobody reads it as a second pair of eyes.
- **History:** `events` holds every change with who, when (`client_at`, and
  `recorded_at` from the server), why, and the state after. Show it as a
  timeline; nothing is ever edited.
- **Incident actions:** they keep the version they were raised against
  (`incident_version`). When `incident_latest_version` is higher, the
  incident was amended since: say so, and let a lead cancel the action if it
  no longer fits. Raising against an old version is 409 `NotLatestVersion`:
  fetch the incident and retry with `latest_version`.
- **Ids:** every write uses an id the client makes up (UUID v4), so a retry
  is safe and answers 200 with the stored result.

## Open, not in M9

- Escalating overdue actions and reminders (Rev 3 and Rev 4 list
  escalation as P1) come from `cmd/worker` in M11; `awaiting_since` is
  there for it.
- The compliance summary's counts of unactioned, waived and overdue come
  with M12.
- Root-cause investigation of incidents is P2.
