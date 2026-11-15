# M8 handover for the dashboard (Sun 15 Nov 2026)

Everything the backend added in M8 that the dashboard needs. The details,
status codes and exact error texts are in `incidents.md`; its TypeScript
block is checked against the Go code, so copy types from there.

## Types to add to `web/src/lib/types.ts`

- **Incidents:** `Incident`, `Version`, `IncidentType`, `Person`,
  `AttachedEvidence` (incidents' own: id and capture time only),
  `DeadlineChanges`, `WitnessStatement`, `IncidentSummary`, `IncidentList`,
  `Treatment`.
- **Deadlines:** `Deadline`, `DeadlineDetail`, `DeadlineList`,
  `DeadlineState`, `Satisfaction`, `Retraction`, `SatisfactionMethod`.
- **Requests:** `SubmitVersionRequest`, `PersonInput`, `StatementRequest`,
  `SatisfactionRequest`, `RetractRequest`.
- **Error bodies with extra fields:** `EvidenceProblem` (`evidence_ids`),
  `UnknownTypes` (`taxonomy_codes`), `NotNextVersion` (`latest_version`).

`AttachedEvidence` in `incidents.md` has two fields; the inspections one has
four. Name them apart in `types.ts` (e.g. `IncidentEvidence`).

## New endpoints

| Endpoint | For |
|---|---|
| `GET /v1/sites/:id/incidents` | The incident register, with open and overdue deadline counts |
| `GET /v1/sites/:id/incidents/:incidentId` | One incident as of a version (`?version=`), with every deadline row and statement |
| `PUT /v1/sites/:id/incidents/:incidentId/versions/:version` | Report (version 1) or amend (the next version) |
| `PUT /v1/sites/:id/incidents/:incidentId/witness-statements/:statementId` | Record a witness statement |
| `GET /v1/sites/:id/deadlines` | The site's live deadlines, soonest first |
| `GET /v1/sites/:id/deadlines/:deadlineId` | One deadline with its satisfaction history |
| `PUT /v1/sites/:id/deadlines/:deadlineId/satisfactions/:satisfactionId` | Record that a deadline was met (the call, the form sent) |
| `POST /v1/sites/:id/deadlines/:deadlineId/satisfactions/:satisfactionId/retract` | Take back a satisfaction recorded in error (owner, SHE officer) |

## What the dashboard should do

- **Deadlines are guidance.** Show every deadline with its `label`,
  `source_ref` and, when set, `caveat`. Never word one as authoritative.
- **Due dates:** for a day-based rule show "by the end of `due_on`" in the
  site's time zone (`time_zone`), not a clock time. When
  `legal_latest_on` is set, show it beside the due date as "the law
  allows at least until …" with `legal_latest_source_ref`, never "until"
  alone: whether the law allows longer is unsettled. Alerts and colours
  use the due date only.
- **Forthwith** (`state: "due_now"`): a call-now prompt with a "record the
  call" action, not a countdown.
- **`overdue_when_computed`:** the deadline was already past when the
  system learned it applied (a type added days later). Say so, so nobody
  reads it as having been missed while the system was watching.
- **History:** superseded and withdrawn rows stay in `deadlines`. Show the
  live ones first and the rest as history, with `closed_by`, `closed_at`
  and `closed_reason`.
- **Amending:** the dashboard sends the whole version again with the next
  number and an `amend_reason`; on 409 `NotNextVersion`, fetch the incident
  and start from `latest_version`.
- **Downgrades:** withdrawing or postponing a report deadline is refused
  (403) for supervisors. Offer it to owners and SHE officers only.
- **Retracting** a satisfaction is owner and SHE officer only, with a
  reason; the deadline's alerts come back. If the retracted call had
  started a death notice (GAR 8(2)), the incident's next amendment
  withdraws it, so only an owner or SHE officer can amend it (403 for a
  supervisor).
- **Recorded late:** when a satisfaction has `recorded_after_due: true`,
  the call was in time but reached the server after the due time (an
  offline phone). Show both `satisfied_at` and `recorded_at`, because an
  overdue alert may have shown in the meantime.
- **Added by a call:** a deadline with `added_by_satisfaction_id` was
  started by recording that satisfaction, not by a version. No version
  lists it in `changes.added`.
- **Old incidents:** an incident whose deadlines would fall before
  1 Jan 2024 gets 400 (the holiday calendar starts then). Show the text.
- **Incident types:** labels come from the incident (`types[].label`);
  `incidents.md` lists the construction regime's types for the form. Allow
  several types per incident.
- **Not the dashboard's job:** reporting is mainly the mobile app's.

## Open, not in M8

- Who may see injury and treatment details (POPIA). Today every owner, SHE
  officer and supervisor on the site can; a decision is due before launch
  (BUILD_PLAN, DEPLOY).
- Reminders and alerts for deadlines come from `cmd/worker` in M11.
- Corrective actions from incidents come in M9.
