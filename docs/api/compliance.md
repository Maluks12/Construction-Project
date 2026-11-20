# Compliance summary API

For the dashboard: what needs attention on a site now, and across the
organisation. The backend keeps this file true: `TestAPIDoc` (in
`construction_db/internal/core/reports`) fails if the types below stop
matching the Go code, or if an error text the handlers send isn't quoted
here.

All endpoints need `Authorization: Bearer <access token>`. Owners and SHE
officers on any site; supervisors on their own sites (another site is 404
`"site not found"`); other roles 403 `"forbidden"`. Errors are
`{"error": "<message>"}`. Read-only: nothing here changes a record.

## The rule (M12 D1–D4)

The summary counts things; it gives no score. Each category has its items,
each linked to its record by id, and a colour:

- **red:** something is already wrong;
- **amber:** something is about to go wrong, or may not apply (see the
  item's `note`);
- **green:** nothing in the category;
- **no colour** (`status: null`, `counted: false`): shown, not counted.

A category is red if any of its items is red, else amber if any is amber,
else green. **The site's `status` is its worst category.** `red_items` and
`amber_items` count the items of counted categories.

| `key` | Counted | Red | Amber |
|---|---|---|---|
| `hs_file` | yes | a required H&S file type missing, or every document of it expired | a current document in its expiring window |
| `vault` | yes | a required organisation document expired (shown on every site) | a current vault document in its expiring window (60 days) |
| `appointments` | yes | – | a required type with no valid or expiring appointee (`note` says when it may not apply); an appointment expiring or expired |
| `findings` | yes | an unactioned `high` or `critical` finding | an unactioned `low` or `medium` finding |
| `actions` | yes | an open action past its due date, with `escalation_step` 1–3 | – |
| `verifications` | yes | – | a close-out awaiting verification for more than 2 days |
| `sign_ins` | yes | a sign-in today whose worker has a warning now (`no_valid_medical`, `medical_unfit`, `competency_expired`) | – |
| `incident_deadlines` | yes | a deadline overdue, or a forthwith one (`due_now`) not met | a deadline due within 2 days |
| `worker_records` | yes | – | a competency or medical of a worker on the site's roster in its expiring window (30 days) |
| `absent_workers` | no | | roster workers who haven't signed in today |
| `pending_uploads` | no | | the site's photos and document files declared more than 24 hours ago and never completed |

- **Not counted on purpose:** a vault type that is missing (onboarding
  shows that); reminders (the inbox has them); expired competencies of
  workers who aren't on site today (they count when the worker signs in).
- **"Today"** is the date in the site regime's `time_zone`, returned as
  `date`. Lead times are the regime's (`GET /v1/regimes/:code/types`,
  `reminders`): expiring windows are the first lead time, "due soon" the
  first `deadline_days_before`, stalled `verification_stalled_days`.
- **Required appointments (D4):** the construction regime requires a
  construction manager (CR 2014 reg 8(1)) and construction supervisors
  (reg 8(7)). Both bind the principal contractor; SiteProof doesn't yet
  record whether this organisation is the principal contractor on the site,
  so a missing appointee is amber with the type's `required_note`, never
  red. Show the note beside it.
- **Absent:** the roster as it stood when the day began, so a worker taken
  off the roster today is still listed today.
- **Pending uploads** are uploads a phone declared and never completed, so
  their bytes may be missing or stuck under Object Lock. Ask the person who
  uploaded it to retry, or let it be.

## Linking an item to its record

`subject_type` and `subject_id` name the record; the other ids open it:

| `subject_type` | `subject_id` | Open with |
|---|---|---|
| `document` | the document | `GET …/safety-file/:subject_id` (H&S file) or `GET /v1/vault/documents/:subject_id` (vault; owners and SHE officers) |
| `document_type` | null | `type_key`: add one in the H&S file |
| `appointment` | the appointment | `GET /v1/construction/sites/:id/appointments` |
| `appointment_type` | null | `type_key`: appoint someone |
| `finding` | the inspection | `inspection_id` and `item_key`: `GET /v1/sites/:id/findings`, raise an action |
| `action` | the action | `GET /v1/sites/:id/actions/:subject_id` |
| `closeout` | the close-out | `action_id`: `GET /v1/sites/:id/actions/:action_id`, verify |
| `sign_in` | the sign-in | `worker_id`: `GET /v1/workers/:worker_id` |
| `deadline` | the deadline | `GET /v1/sites/:id/deadlines/:subject_id`, `incident_id` |
| `competency`, `medical` | the record | `worker_id`: `GET /v1/workers/:worker_id` |
| `roster_entry` | the roster entry | `worker_id` |
| `evidence` | the evidence | `GET /v1/sites/:id/evidence/:subject_id` |
| `file` | the file | `GET /v1/files/:subject_id` |

## Endpoints

### `GET /v1/sites/:id/compliance`

200 with `SiteSummary`. Works on archived sites. 404 `"site not found"`.

### `GET /v1/compliance`

200 with `Rollup`: every site the caller can open, archived ones left out,
**worst first**: status (red, amber, green), then more red items, then more
amber items, then site name (D3). Each site carries its counts and colours,
not its items; open a site's summary for those.

`org_items` holds what belongs to no site, once: the `vault` category, and
for owners and SHE officers the `pending_uploads` of document files with no
site (worker certificates, vault documents).

## Types for `web/src/lib/types.ts`

<!-- types -->
```ts
export type Colour = "red" | "amber" | "green";

export type CategoryKey = "hs_file" | "vault" | "appointments" | "findings" | "actions" | "verifications" | "sign_ins" | "incident_deadlines" | "worker_records" | "absent_workers" | "pending_uploads";

export type ComplianceSubject = "document" | "document_type" | "appointment" | "appointment_type" | "finding" | "action" | "closeout" | "sign_in" | "deadline" | "competency" | "medical" | "roster_entry" | "evidence" | "file";

export type ItemState = "missing" | "expired" | "expiring" | "unactioned" | "overdue" | "due_now" | "due_soon" | "stalled" | "warnings" | "absent" | "pending" | "no_appointee";

export interface ComplianceItem {
  subject_type: ComplianceSubject;
  subject_id: string | null;
  state: ItemState;
  colour: Colour | null;
  label: string;
  inspection_id: string | null;
  item_key: string | null;
  worker_id: string | null;
  incident_id: string | null;
  action_id: string | null;
  type_key: string | null;
  severity: string | null;
  escalation_step: number | null;
  due_on: string | null;
  since: string | null;
  warnings: string[];
  note: string | null;
}

export interface ComplianceCategory {
  key: CategoryKey;
  counted: boolean;
  status: Colour | null;
  count: number;
  items: ComplianceItem[];
}

export interface SiteSummary {
  site_id: string;
  site_name: string;
  site_status: string;
  regime_code: string;
  time_zone: string;
  date: string;
  computed_at: string;
  status: Colour;
  red_items: number;
  amber_items: number;
  categories: ComplianceCategory[];
}

export interface SiteRank {
  site_id: string;
  site_name: string;
  site_status: string;
  status: Colour;
  red_items: number;
  amber_items: number;
  counts: Record<string, number>;
  statuses: Record<string, Colour>;
}

export interface Rollup {
  computed_at: string;
  org_items: ComplianceCategory[];
  sites: SiteRank[];
}
```
