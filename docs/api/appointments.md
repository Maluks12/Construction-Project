# Appointments API (Construction edition)

For the dashboard and the mobile app: a site's written appointments under the
Construction Regulations 2014 (reg 8: construction manager, supervisors,
health and safety officer; and the competent persons of regs 9 to 28, listed
in the regime). The backend keeps this file true: `TestAPIDoc` (in
`construction_db/internal/editions/construction/appointments`) fails if the
types below stop matching the Go code, or if an error text the handlers send
isn't quoted here.

All endpoints need `Authorization: Bearer <access token>`. Errors are
`{"error": "<message>"}`. The endpoints are under `/v1/construction`; a site
under another edition's regime is 404 `"site not found"`.

Roles: owners and SHE officers record and retract; supervisors read their own
sites. Other roles get 403 `"forbidden"`.

## How appointments work (M11 D21)

- **An appointment** names:
  - a register worker (workers.md);
  - a type from the site regime's `appointment_types` (`GET
    /v1/regimes/:code/types`);
  - the date appointed, and an expiry if the type allows one;
  - the signed letter: a verified `appointment` file of the same site
    (files.md).
- **A new appointee or a renewal** is a new appointment with `supersedes_id`:
  the same site and type, and the person may change. The old one shows
  `superseded`. One live successor per appointment.
- **A mistake** is retracted by an owner or SHE officer, with a reason; it
  stays, marked `retracted`.
- **State** as workers' records: `retracted`, `superseded`, `expired`,
  `expiring` (within 30 days) or `valid`.
- Appointment letters and gap detection (an activity under way with nobody
  appointed) aren't built yet.

## Endpoints

### `GET /v1/construction/sites/:siteId/appointments`

`{"appointments": [Appointment]}`, by type, oldest first.

### `PUT /v1/construction/sites/:siteId/appointments/:appointmentId`

Body: `AppointmentRequest`.

| Status | When |
|---|---|
| 201 | Recorded. `Appointment` |
| 200 | The same again |
| 400 | `"appointment id must be a UUID"`, `"type_key is not an appointment type of this site's regime"`, `"appointed_on and expires_on must be YYYY-MM-DD, expiry not before the appointment"`, `"expires_on is required for this type"`, `"this type has no expiry; leave expires_on out"`, `"client_recorded_at is required, and not in the future"`, `"device_id must be 1-200 bytes"`, `"supersedes_id must be a live appointment of the same type on this site"`; the file's (files.md) |
| 403 | `"only an owner or SHE officer can record or retract appointments"` |
| 404 | `"site not found"`, `"worker not found"` |
| 409 | `"appointment id already used with different details"`; `"that appointment already has a successor"`; `"file already attached to another appointment"`; `"file not verified yet"` |

### `POST /v1/construction/sites/:siteId/appointments/:appointmentId/retract`

Body: `RetractRequest`. 201 retracted, 200 the same again; 400 `"reason must be 1-500 characters"`; 403 as above; 404 `"appointment not found"`; 409 `"appointment already retracted with another reason"`.

### `POST /v1/construction/sites/:siteId/appointments/:appointmentId/view`

The letter: `ViewURL` (files.md), logged, valid 60 seconds. 404 `"appointment not found"`.

## Types for `web/src/lib/types.ts`

`RecordState` and `Retraction` are in workers.md.

<!-- types -->
```ts
export interface AppointmentRequest {
  worker_id: string;
  type_key: string;
  appointed_on: string;
  expires_on?: string | null;
  file_id: string;
  supersedes_id?: string | null;
  client_recorded_at: string;
  device_id?: string | null;
}

export interface Appointment {
  id: string;
  worker_id: string;
  regime_code: string;
  file_id: string;
  supersedes_id: string | null;
  superseded_by: string | null;
  recorded_by: string;
  client_recorded_at: string;
  recorded_at: string;
  device_id: string | null;
  retraction: Retraction | null;
  state: RecordState;
  site_id: string;
  type_key: string;
  worker_name: string;
  appointed_on: string;
  expires_on: string | null;
}

export interface AppointmentList {
  appointments: Appointment[];
}
```
