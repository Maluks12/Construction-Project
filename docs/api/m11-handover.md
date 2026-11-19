# M11 handover for the dashboard and the mobile app (Thu 19 Nov 2026)

Everything the backend added in M11:
- the worker register with competencies and medicals;
- daily site sign-in;
- the H&S file and the statutory vault;
- appointments;
- reminders.

The details, status codes and exact error texts are in `files.md`,
`workers.md`, `attendance.md`, `documents.md`, `appointments.md` and
`notifications.md`. Their TypeScript blocks are checked against the Go code,
so copy types from there.

## Types to add (`web/src/lib/types.ts`, `mobile/src/lib/types.ts`)

- **Files:** `DeclareRequest`, `File`, `Upload`, `Rejected`, `ViewURL`, and
  the unions `FilePurpose`, `FileContentType`, `FileRejectedReason`.
- **Workers:**
  - `WorkerRequest`, `Worker`, `WorkerList`, `WorkerDetail`;
  - `CompetencyRequest`, `Competency`, `MedicalRequest`, `Medical`,
    `Retraction`, `RetractRequest`;
  - `LinkRequest`, `UnlinkRequest`, `Link`;
  - `RegimeSummary`, `RegimeList`, `RegimeTypes`, `CompetencyType`,
    `AppointmentType`, `MedicalRule`, `DocumentType`, `Reminders`;
  - `FieldProblem`, `IdentityClash`;
  - the unions `RecordState`, `MedicalOutcome`, `SignInWarning`.
- **Sign-in:** `SignInRequest`, `SignOutRequest`, `RemoveRequest`, `SignIn`,
  `SignOut`, `Sheet`, `Overlap`, `Day`, `RosterEntry`, `RosterList`,
  `AlreadySignedIn`, and the union `SignInMethod`. `Retraction` and
  `RetractRequest` have the same shape as workers'.
- **Documents:** `CreateRequest`, `VersionRequest`, `DocumentSummary`,
  `Document`, `Version`, `Readiness`, `Register`, and the unions
  `DocumentScope`, `VersionState`, `ReadinessStatus`.
- **Appointments:** `AppointmentRequest`, `Appointment`, `AppointmentList`.
- **Notifications:** `Notification`, `NotificationList`, and the unions
  `NotificationKind`, `NotificationSubject`, `SubjectState`.
- **Changed:**
  - `Attendance` (toolbox.md) gains `worker_id`;
  - `Talk` gains `not_signed` (new type `NotSigned`);
  - `LabourRow` (diary.md) gains the optional `worker_ids`;
  - `headcount_suggested` and `headcount_suggested_now` now count sign-ins
    too.

## New endpoints

| Endpoint | For |
|---|---|
| `PUT /v1/files/:id`, `POST /v1/files/:id/complete`, `GET /v1/files/:id` | Upload a document file: declare, PUT the bytes, complete |
| `GET /v1/regimes`, `GET /v1/regimes/:code/types` | Regimes, and their competency, appointment and document types, medical rule and reminder lead times |
| `GET /v1/workers?active=&q=&limit=`, `GET/PUT /v1/workers/:id` | The register |
| `PUT /v1/workers/:id/competencies/:recordId`, `…/retract`, `…/view` | Competencies |
| `PUT /v1/workers/:id/medicals/:recordId`, `…/retract`, `…/view` | Medical certificates of fitness |
| `PUT …/toolbox-talks/:talkId/attendance/:attendanceId/worker`, `POST …/worker/unlink` | Link a signature to a worker by hand, or unlink it |
| `GET /v1/sites/:id/sign-ins?date=` | The day's sign-ins, absent roster workers, overlaps, headcount |
| `PUT /v1/sites/:id/sign-ins/:signInId`, `POST …/sign-out`, `POST …/retract` | Sign in (a register worker or someone new), out, retract |
| `PUT /v1/sites/:id/sign-in-sheets/:date/:evidenceId` | A photographed paper sheet |
| `GET /v1/sites/:id/roster`, `PUT /v1/sites/:id/roster/:workerId`, `POST …/remove` | Who is expected on site |
| `GET /v1/vault/documents`, `GET/PUT …/:id`, `PUT …/:id/versions/:n`, `POST …/versions/:n/view` | The statutory vault (owners and SHE officers) |
| `GET /v1/construction/sites/:id/safety-file`, and the same below it | A site's H&S file, with readiness |
| `GET /v1/construction/sites/:id/appointments`, `PUT …/:id`, `POST …/retract`, `POST …/view` | Appointments |
| `GET /v1/notifications?unread=&limit=`, `POST /v1/notifications/:id/read` | The inbox |
| `GET /healthz/jobs` | For the uptime monitor (not the apps) |

## What the mobile app should do

- **Uploading a certificate or letter:** as photos, but through
  `/v1/files`:
  1. hash the file;
  2. declare it with its purpose (and `site_id` for appointment letters and
     H&S file documents);
  3. PUT the bytes with the given headers;
  4. complete it;
  5. then send the record with its `file_id`.

  A record naming a file still uploading is 409 `"file not verified yet"`:
  keep it queued.
- **Sign-in screen:** cache the register (`GET /v1/workers?active=true`) and
  the roster.
  - **A tap** on a name: `PUT …/sign-ins/:new-id` with `worker_id`.
  - **Someone new:** make up a `worker_id`, send `new_worker` with name and
    optional firm and clock number.
  - **If the answer has `matched_existing: true`,** show who it matched
    (name, firm, clock number). If the supervisor says "different person":
    retract that sign-in (reason "Different person") and sign in again with a
    new `worker_id` and a clock number.
- **409 `AlreadySignedIn`:** the person is already signed in on this site
  (another phone did it, or they never signed out). Show it and drop the
  local one; don't retry.
- **Warnings** (`no_valid_medical`, `medical_unfit`, `competency_expired`)
  are shown, never a block: the sign-in is recorded either way.
- **Sign-out** is optional; offer it at the gate. Without one, a sign-in
  lapses after 16 hours. Nothing happens at midnight, so night shifts sign
  out the next morning.
- **Offline:** queue sign-ins with the phone's time. Late ones are accepted;
  after the diary's 48-hour grace they show `received_after_close`.
- **Paper sheets:**
  1. photograph and complete the sheet as evidence;
  2. `PUT …/sign-in-sheets/:date/:evidenceId`;
  3. record each name with `method: "paper"`.
- **Medicals:** a supervisor can capture one (photo or PDF of the
  certificate, then the outcome and dates) but can't open it again; that's
  intended (403 on `view`).
- **Toolbox talk:** show `not_signed` as "signed in but didn't sign: absent
  or refused".

## What the dashboard should do

- **Register:**
  - workers with their competencies and medicals, each with `state`;
  - renew with `supersedes_id`;
  - retract with a reason, showing retracted records struck through rather
    than hidden.
- **Medicals:**
  - show outcome and dates to everyone;
  - only owners and SHE officers get an "open certificate" button.

  **Note the change from Rev 4 (decided 18 Nov):** Rev 4 §3.4 says "status
  only, never contents" and §2.12 lets a SHE officer see health documents
  only with a claims-handler capability. We store the certificate itself,
  because inspectors and principal contractors ask for it. It is readable by
  the owner and SHE officers only, and every view is logged.
- **Day register** (`GET …/sign-ins`):
  - who signed in and out;
  - `absent` (expected from the roster, not signed in);
  - `overlaps` (the same worker on two sites at once);
  - `headcount` beside the diary's entered number.
- **H&S file:** the readiness list (present, missing, expired) per required
  type, and each document's versions with the current one's state.
- **Vault:** the same for the organisation, for owners and SHE officers.
  Letters of Good Standing need an issuer: `FEM`, `Compensation Fund` or
  `RMA`.
- **Appointments:** per site, by type, with the current appointee; a new
  appointee supersedes the old.
- **Inbox:** the bell. Unread count from `?unread=true`. Show
  `subject_state: "superseded"` reminders greyed out (the thing was renewed,
  satisfied or closed).

## Open, not in M11

- **Channels:** push (Expo), SMS and WhatsApp reminders (BILL metering), and
  per-organisation lead times. BUILD_PLAN, proposed Wed 18 Nov.
- **Appointments:** letters and gap detection; induction records and site
  clearance.
- **Vault:**
  - BILL's plan gate (one middleware, noted on the route group);
  - PC's cross-org read (one row-level security policy);
  - "verification status" of vault documents (Rev 4 A.8, P2).
- **Sign-in:** QR codes on workers' own phones, and the geofence (Rev 4
  §2.7). Not built.
- **ONBOARD's CSV import** reuses `ValidateWorker`, `ValidateCompetency` and
  `CheckRows` (workers.md).
