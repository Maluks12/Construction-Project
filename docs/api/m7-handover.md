# M7 handover for the dashboard (Sun 15 Nov 2026)

Everything the backend added in M7 that the dashboard needs. The details,
status codes and exact error texts are in `evidence.md` and
`inspections.md`; their TypeScript blocks are checked against the Go code,
so copy types from there.

## Types to add to `web/src/lib/types.ts`

- **From `evidence.md`:** `ViewURL`.
- **From `inspections.md`:**
  - **Catalogue:** `ChecklistCatalog`, `RegimeRef`, `ChecklistTemplate`
    (with `sha256`), `ChecklistItem`, `SeverityLevel`, `Severity`,
    `Cadence`, `ResponseType`.
  - **Inspections:** `Inspection`, `InspectionItem`, `AttachedEvidence`,
    `InspectionFinding`, `InspectionSummary`, `InspectionList`,
    `InspectionStatus`, `Result`.
  - **Requests:** `StartInspectionRequest`, `AnswerRequest`,
    `CompleteRequest`, `AbandonRequest`.
  - **Findings:** `Finding`, `FindingList`.
  - **Error bodies with extra fields:** `EvidenceProblem`
    (`evidence_ids`), `StaleAnswer` (`stored`), `IncompleteInspection`
    (`item_keys`).

## New endpoints

| Endpoint | For |
|---|---|
| `POST /v1/sites/:id/evidence/:evidenceId/view` | A 60-second link to the photo itself |
| `GET /v1/sites/:id/checklists` | The site's checklist templates and the severity scale |
| `PUT /v1/sites/:id/inspections/:inspectionId` | Start an inspection (the client makes up the id) |
| `GET /v1/sites/:id/inspections` | List a site's inspections, with progress counts |
| `GET /v1/sites/:id/inspections/:inspectionId` | One inspection, with items, photos and findings |
| `PUT /v1/sites/:id/inspections/:inspectionId/items/:itemKey` | Answer an item (the inspector who started it) |
| `POST /v1/sites/:id/inspections/:inspectionId/complete` | Complete it (the inspector who started it) |
| `POST /v1/sites/:id/inspections/:inspectionId/abandon` | Abandon it, with a reason |
| `GET /v1/sites/:id/findings` | A site's findings, newest first |

## What the dashboard should do

- **Showing a photo:** POST `/view` each time and put the returned `url`
  in `<img src>`. Never store or reuse the link; it expires in 60 seconds,
  and each call is what records a view in the access log.
- **Severity:** show each level's `definition` next to the choice, from the
  catalogue's `severities`. Findings are compared across sites, so everyone
  must grade against the same words.
- **Reused photos:** on an inspection item, flag a photo whose
  `captured_before_start` is true. It was taken before the inspection
  started, by the device's clock.
- **Abandoned inspections** stay in lists and on get (`status:
  "abandoned"`, with who, when and why). Show them marked; don't hide them.
  Abandoning is offered to:
  - the inspector who started it, owners and SHE officers;
  - **only owners and SHE officers once any item has failed.**
- **Corrections:** a completed inspection never changes. "Correct" means
  starting a new inspection with `corrects_id`, on the same checklist; each
  inspection can have one correction.
- **Archived sites:** their inspections and findings stay readable, but new
  inspections can't be started there.
- **Not the dashboard's job:** start, answer and complete are mainly for the
  mobile app. The dashboard can stay read-only for inspections if that's
  simpler. Owners and SHE officers can abandon.

## Behaviour changes to existing endpoints

- **Evidence reads are logged:** `GET …/evidence/:evidenceId` and the
  evidence list each write an access-log row. The responses don't change.
- **Upload `Content-Type` is signed:** send it exactly as `upload.headers`
  gives it (you already should).
