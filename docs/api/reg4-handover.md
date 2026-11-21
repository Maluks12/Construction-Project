# REG4 handover for the dashboard and the app (Sat 21 Nov 2026)

What the backend added in REG4 (Regulation 4 notification) that the frontend
needs. The details, status codes and exact error texts are in
`site-work.md` and `deadlines.md`; their TypeScript blocks are checked
against the Go code, so copy types from there.

## What it is

A site's planned construction work, with the notices it needs before it
starts: the CR 2014 reg 4 notification to the provincial director (7 clear
days before) and, for larger work, the reg 3 construction work permit
application (30 clear days before, thresholds from GN 850 of 2018). The user
records the work and ticks what it involves; the deadlines follow.

## Contract changes

- **Deadline endpoints moved package, not path.** `GET/PUT/POST
  /v1/sites/:id/deadlines…` are unchanged on the wire, but their reference
  is now `deadlines.md` (was `incidents.md`).
- **`Deadline`** gains `subject_type` (`"incident"` | `"site_work"`),
  `site_work_id` and `proof_document_types`. Exactly one of `incident_id`
  and `site_work_id` is set.
- **`Satisfaction`** gains `document_id` and `document_version`;
  **`SatisfactionRequest`** takes them (both or neither).
- **`GET /v1/sites/:id/deadlines`** takes `subject_type=` as a filter.
- **Notifications:** a new `NotificationKind`, `"site_work_deadline"`.
- **Compliance summary:** a new `CategoryKey`, `"site_work_deadlines"`, and
  `ComplianceItem.site_work_id`. `incident_deadlines` now lists incident
  deadlines only.
- **Audit packs:** `pack_format` is 2; packs have a `site_work` section after
  `hs_file`.
- **Regime types** (`GET …/types`, `workers.md`): `Reminders` gains
  `notification_days_before` (`[7, 2, 0]`).

### From the review (Sat 21 Nov)

- **Deadline reminders are per computed deadline (R1).** A notification of
  kind `incident_deadline` or `site_work_deadline` now has `subject_type`
  `"deadline"` and `subject_id` the deadline row's id; it was
  `"deadline_duty"` and the duty's id. `kind` and `threshold` are as before.
  Open it with `GET /v1/sites/:siteId/deadlines/:subject_id`.
  - When a deadline is recomputed (a moved start, a corrected
    `occurred_at`, a holiday, work cancelled then reinstated), the new row's
    reminders come again for its own date. The old row's inbox items show
    `subject_state: "superseded"`.
  - Notifications stored before keep `"deadline_duty"`: show them as before
    (`NotificationSubject` has both).
- **NUL characters are refused everywhere (R4).** Any request with a NUL
  (`\u0000`) in its JSON body, query string or path gets 400 `"text must not
  contain NUL characters"`, before the token is checked. The app should
  strip NUL from what users type; it used to be a 500.
- **Audit pack spans (R5).** Site work is in a pack when any of its
  versions' spans overlaps the period. A span starts at the record's
  earliest due date if that is before its start, and a cancelled version
  ends on its cancellation day (`exports.md`).

## Types to add to `web/src/lib/types.ts`

- From `site-work.md`: `SiteWorkStatus`, `VersionRequest` (name it
  `SiteWorkVersionRequest`), `SiteWorkFact`, `SiteWorkVersion`,
  `SiteWorkChanges`, `SiteWork`, `SiteWorkSummary`, `SiteWorkList`,
  `SiteWorkRule`, `Catalogue` (name it `SiteWorkCatalogue`), `UnknownFacts`.
  `NotNextVersion` is the same shape as the incidents one.
- From `deadlines.md`: `DeadlineSubjectType`, and the new fields above on
  `Deadline`, `DeadlineDetail`, `Satisfaction`, `SatisfactionRequest`.

## New endpoints

| Endpoint | For |
|---|---|
| `GET /v1/construction/sites/:id/site-work-facts` | The form: the facts to tick and the rules they start, with sources and caveats |
| `PUT /v1/construction/sites/:id/site-work/:workId/versions/:version` | Record work (version 1) or change it (the next version, with `amend_reason`) |
| `GET /v1/construction/sites/:id/site-work/:workId?version=` | One record, with every deadline row |
| `GET /v1/construction/sites/:id/site-work?status=&limit=` | The site's work, the earliest start first, with open and overdue counts |

## What the dashboard should do

- **The form.** Show the facts from `site-work-facts` as checkboxes with
  their labels; don't hard-code them (they come from the site's regime).
  Show the rules beside them so the user sees why a notice is or isn't due,
  including the two exclusions: a permit site needs no reg 4 notice, and a
  single-storey dwelling never needs a permit.
- **Always show the caveat.** Both rules carry one: clear-day counting is our
  reading (no court ruling found); reg 3 binds the client, and GN 850's
  current status wasn't confirmed against the Gazette.
- **Due dates.** "By the end of Sun 22 Nov" for work starting Mon 30 Nov.
  There is never a `legal_latest_on` for these.
- **Late.** `version.late_when_recorded` and `overdue_when_computed`: say the
  work was recorded too late for the notice period, not that it was missed
  while the system watched.
- **Met.** Record the notice through the deadline endpoint with the
  Annexure 2 from the H&S file (`document_id`, `document_version`): upload it
  to the safety file first. For reg 3 link the permit itself.
- **A notice given for another start (R2).** A met notice stays met when the
  start moves (the satisfaction belongs to the duty). Annexure 2 names the
  expected commencement date, so when a satisfaction's `deadline_id` is a
  superseded row with a different `basis_at` from the live row, show "notice
  given for start <date>" (that row's `basis_at`, as a date in
  `time_zone`), so the user can decide whether to tell the provincial
  director of the new date.
- **Changes.** A later start, removing a fact, a permit fact, or cancelling
  takes an owner or SHE officer when an open notice would be weakened (403
  `"only an owner or a SHE officer can withdraw a notification deadline or
  move it later"`): offer them to leads only. A later start of a notice
  already met also needs a lead. An earlier start or a new fact is open to
  supervisors. Show `changes` after each submission.
- **Colours.** `site_work_deadlines`: reg 4 overdue is red; reg 3 overdue is
  amber with `note` (the client's duty).

## For the mobile app (SYNC)

Site work is pushed like incidents: device-chosen ids, device-numbered
versions by PUT, `client_submitted_at` and `device_id` on each version, an
identical repeat is 200, a different one 409, a skipped version 409 with
`latest_version`. Deadlines are derived on the server: pull them, never push
them. A version arriving late is accepted; if the start has passed, the
notice is overdue from the start.

## Open, not in REG4

- Who may record site work follows the incident roles until the role matrix
  is approved.
- PC will record the organisation's role on the site; then reg 3 becomes red
  for a client (or its agent) and uncounted otherwise, and reg 4 uncounted
  for a subcontractor whose principal contractor's notice is linked.
- Reg 3(7) (no work before the permit is issued) and the director's 30 days
  to issue it are not modelled.
