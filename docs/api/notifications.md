# Notifications API

For the dashboard and the mobile app: the in-app inbox of reminders that the
worker (`cmd/worker`) raises. The same reminders go by email to anyone with
an email address. The backend keeps this file true: `TestAPIDoc` (in
`construction_db/internal/core/notifications`) fails if the types below stop
matching the Go code, or if an error text the handlers send isn't quoted
here.

All endpoints need `Authorization: Bearer <access token>`. Every role reads
its own inbox. Errors are `{"error": "<message>"}`.
Any endpoint can answer 503 `"server busy, try again"` with a `Retry-After`
header (seconds): it ran out of time, or waited over 5 seconds for a record
another request was changing; retry after that many seconds.

## What is reminded, to whom, when (M11 D23, D25)

Lead times come from the regime (`reminders` in `GET
/v1/regimes/:code/types`); these are construction's.

| `kind` | About (`subject_type`) | To | When (`threshold`) |
|---|---|---|---|
| `incident_deadline` | a reporting or investigation duty (`deadline_duty`) | the incident version's submitter, owners, SHE officers, the site's supervisors | `forthwith` at once; day-based: `before_2d`, `on_day`, then `overdue`; hour-based: `open`, then `overdue` |
| `action_overdue` | a corrective action (`action`) | `escalation_1` the action's owner and raiser; `escalation_2` (1 day later) SHE officers; `escalation_3` (3 days) owners | days overdue |
| `verification_stalled` | a close-out awaiting verification (`closeout`) | owners and SHE officers | `stalled`, after 2 days |
| `competency_expiry`, `medical_expiry` | `competency`, `medical` | owners, SHE officers, supervisors of sites where the worker is on the roster (today's roster: a sign-in-added entry counts for `roster_lapse_days` after the worker's last sign-in) | `before_30d`, `before_14d`, `before_7d`, `on_day` |
| `appointment_expiry` | `appointment` | owners, SHE officers, the site's supervisors | as above |
| `document_expiry` | `document` (H&S file or vault) | H&S file: owners, SHE officers, the site's supervisors; vault: owners and SHE officers | the vault `v2_before_60d`, `v2_before_30d`, `v2_before_7d`, `v2_on_day` (the version's number first); the H&S file as competencies |
| `integrity_alarm` | an audit export (`export`): a stored photo or file in it no longer matched its seal | owners and SHE officers | `seal_mismatch`, once, the moment the build found it (M12 review) |

- **Titles say who and what:**
  - `<worker>: <type>` for competencies;
  - `<worker>: medical certificate of fitness`;
  - `<worker>: <type> (<site>)` for appointments;
  - a document's title, with `(<site>)` for H&S file documents;
  - the rule or action with its site.

  Never a medical outcome. Emails are the title and one plain sentence
  ("expires on 12 Dec 2026, in 7 days", "overdue by 2 days"), with dates
  and days counted in the site's regime's time zone (a record without a
  site: its own regime's).
- **Each reminder comes once per subject and threshold.** A restart, or two
  workers at once, never repeats one.
- When the first look is already past several lead times, only the latest
  is sent: a licence recorded 5 days before it expires gets `before_7d`, not
  three at once.
- **An integrity alarm** is not a reminder: it says an audit pack found a
  stored original whose bytes no longer match the SHA-256 sealed when it was
  received. The export is stopped for good (exports.md). Show it
  prominently; it stays `current`.
- **A renewal, retraction, satisfaction or close** stops further reminders
  about the old subject, and cancels its queued emails. Earlier reminders
  stay in the inbox with `subject_state: "superseded"`.
- **Email:**
  - each email is sent to SMTP and recorded `sent`; with `EMAIL_MODE=log`
    (development) it is only logged and recorded `logged`, never `sent`;
  - an email is sent at most once. One interrupted mid-send is never repeated
    (it may not have arrived); one that failed is not retried.

## Endpoints

### `GET /v1/notifications?unread=&limit=`

`{"notifications": [Notification]}`, newest first. `unread=true` keeps the
unread ones; `limit` 1–200, default 50. 400 `"limit must be 1-200"`, `"unread must be true or false"`.

### `POST /v1/notifications/:notificationId/read`

Marks it read (200 with the `Notification`; reading it again changes
nothing). 404 `"notification not found"` (also for someone else's).

### `GET /healthz/jobs` (no token; for the uptime monitor)

200 `{"status": "ok", "stale": []}`, or 503 `{"status": "stale", "stale":
["emails"]}` when a worker job has gone twice its interval without
completing (Rev 4 §2.3). The jobs are `reminders` (15 minutes), `emails`
(1 minute), `holidays` (6 hours) and, since M12, `exports` (the audit pack
builder; 1 minute: each build is a run, an idle pass is one, and a long
build records a heartbeat each time it renews its lease).

## Types for `web/src/lib/types.ts`

<!-- types -->
```ts
export type NotificationKind = "incident_deadline" | "action_overdue" | "verification_stalled" | "competency_expiry" | "medical_expiry" | "appointment_expiry" | "document_expiry" | "integrity_alarm";

export type NotificationSubject = "deadline_duty" | "action" | "closeout" | "competency" | "medical" | "appointment" | "document" | "export";

export type SubjectState = "current" | "superseded";

export interface Notification {
  id: string;
  kind: NotificationKind;
  subject_type: NotificationSubject;
  subject_id: string;
  threshold: string;
  site_id: string | null;
  title: string;
  due_on: string | null;
  due_at: string | null;
  created_at: string;
  read_at: string | null;
  subject_state: SubjectState;
}

export interface NotificationList {
  notifications: Notification[];
}
```
