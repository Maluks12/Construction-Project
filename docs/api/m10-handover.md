# M10 handover for the dashboard and the mobile app (Mon 16 Nov 2026)

Everything the backend added in M10: toolbox talks with attendance, and the
site diary. The details, status codes and exact error texts are in
`toolbox.md` and `diary.md`; their TypeScript blocks are checked against the
Go code, so copy types from there.

## Types to add (`web/src/lib/types.ts`, `mobile/src/lib/types.ts`)

- **Toolbox talks:** `ToolboxTopic`, `ToolboxTopicList`, `TalkSummary`,
  `Talk`, `TalkList`, `TalkPhoto`, `Attendance`, `Retraction`, and the
  unions `AttendanceMethod`, `TalkPhotoKind`.
- **Toolbox requests:** `TalkRequest`, `PhotoRequest`, `SignRequest`,
  `RetractRequest`.
- **Site diary:** `Diary`, `DiarySummary`, `DiaryList`, `Revision`,
  `RevisionList`, `Addendum`, `FirmList`, `Weather`, `Sections`,
  `WeatherTimes`, `LabourRow`, `PlantRow`, `Instruction`, `Delay`,
  `Visitor`, `Delivery`, and the unions `WeatherCondition`, `PlantState`,
  `InstructionForm`, `DeliveryStatus`.
- **Diary requests:** `RevisionRequest`, `AddendumRequest`.
- **Error bodies with extra fields:** `EvidenceProblem` (as before),
  `SectionProblem` (`section`, `row`, `field`) and `AlreadySigned` (`hint`).

Nothing existing changed.

## New endpoints

| Endpoint | For |
|---|---|
| `GET /v1/sites/:id/toolbox-topics` | The site regime's topic list, each with its regulation |
| `GET /v1/sites/:id/toolbox-talks?date=&limit=` | A site's talks, newest first; `?date=` for one day |
| `GET /v1/sites/:id/toolbox-talks/:talkId` | One talk with its photos and signatures |
| `PUT /v1/sites/:id/toolbox-talks/:talkId` | Create a talk, or edit it until someone signs (then only the end time, once) |
| `PUT /v1/sites/:id/toolbox-talks/:talkId/photos/:evidenceId` | Add a photo (`photo`, or `paper_register` for a signed sheet) |
| `PUT /v1/sites/:id/toolbox-talks/:talkId/attendance/:attendanceId` | One person signs (tap) or is recorded from the paper register |
| `POST /v1/sites/:id/toolbox-talks/:talkId/attendance/:attendanceId/retract` | Withdraw a signature given by mistake, with a reason |
| `GET /v1/construction/sites/:id/diary?from=&to=&limit=` | The diary register, newest day first |
| `GET /v1/construction/sites/:id/diary/:date` | A day's diary: current revision, conflict, addenda, headcount suggestion, linked talks and incidents |
| `GET /v1/construction/sites/:id/diary/:date/revisions` | Every revision of a day, for a conflict side by side |
| `PUT /v1/construction/sites/:id/diary/:date/revisions/:revisionId` | Save the whole diary |
| `PUT /v1/construction/sites/:id/diary/:date/addenda/:addendumId` | Add to a closed diary, with a reason |
| `GET /v1/construction/sites/:id/diary-firms` | Firm names used on the site before, for autocomplete |

The diary is under `/v1/construction` (Construction edition); a site under
another edition's regime answers 404 there.

## What the mobile app should do

- **Toolbox talk ("tap to sign").** Create the talk (`PUT`, topic from
  `GET …/toolbox-topics` or free text, presenter, start time). Each person
  taps their name: `PUT …/attendance/:id` with `method: "tap"`, the name as
  they'd write it, and optionally their firm and clock number. No ID
  numbers. Set `ended_at` when the talk ends.
- **The same name twice.** A person is their name, firm and clock number
  together (case and spacing don't count). A second signature of all three
  is 409 `"this person has already signed this talk"` with a `hint`: if it's
  really someone else with the same name, ask for their firm or clock
  number and send it again.
- **Night shifts.** People can sign from the talk's start until its end, or
  up to 12 hours after the start while it has none, across midnight. The
  end time can't be set before a signature already given, nor more than 12
  hours after the start. The talk still counts on the day it started.
- **Paper register.** Photograph the signed sheet, upload and complete it
  (`evidence.md`), add it with `kind: "paper_register"`, then record each
  name with `method: "paper"`. Paper names before the photo are refused.
- **The mockup's "Unsigned people are recorded as absent or refused"** needs
  an expected list of people, which comes with M11's worker register. Until
  then, only signatures are recorded.
- **Site diary.** Three things are required: `weather` (one or more
  conditions; the mockup's single chip works), `headcount`, `work_done`.
  Everything under "More detail" is optional `sections`; leave out what
  wasn't filled in. Give every row an id when it's created and keep it in
  later saves.
- **Headcount hint.** `headcount_suggested_now` on `GET …/diary/:date` is the
  number who signed that day's talks; show it as the hint beside the field.
  The supervisor's own number is what's saved.
- **Saving.** Each save is a new revision id with the whole diary, and
  `base_revision_id` set to the `current.id` the phone last fetched (null
  for the first). Keep saves queued offline; one saved before midnight still
  counts if it reaches the server within 48 hours.
- **Conflicts.** If the response has `conflict_revision_ids`, two phones
  wrote without seeing each other. Show both (`GET …/revisions`) and let the
  supervisor save a merged revision based on the current one; that clears
  it.
- **After midnight.** Saving is refused with `"site diary closed at
  midnight; add an addendum"`: offer an addendum (reason required, plus a
  note, sections or photos). For a day with no diary at all, offer a late
  diary instead (`late_reason`).
- **Photos.** Upload and complete them first. A revision naming a photo still
  uploading is 409 with its id: keep the revision queued and resend it once
  the photo is complete, inside the 48 hours.

## What the dashboard should do

- **Toolbox register:** list talks by day with `signed_count` and
  `photo_count`; on a talk, show retracted signatures struck through with
  the reason, not hidden.
- **Diary:** show the current revision, then the addenda in order, each
  with who, when (phone and server time) and why. Flag `late`,
  `received_after_close` (saved on time, arrived late: show both times),
  conflicts, and a headcount that differs from the suggestion.
- **Links:** show the day's talks and incidents from `toolbox_talk_ids` and
  `incident_ids`; they aren't copied into the diary.

## Open, not in M10

- Worker identity: M11 links signatures to the worker register (no change
  to signatures already given).
- Site sign-ins will join the headcount suggestion: BUILD_PLAN, M11, "Daily site sign-in and sign-out from the worker register" (distinct people who signed in that day, plus anyone who only signed a talk, on the same identity key).
- Toolbox talk content (what the presenter reads out) is a proposed task,
  not scheduled (BUILD_PLAN, TALKS).
- JBCC delay events and contract instructions will link to diary rows by
  their ids; the principal-contractor tier will link labour firms.
