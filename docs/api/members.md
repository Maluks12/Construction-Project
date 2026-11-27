# Team management API: members

For the dashboard: the organisation's members, changing a member's role or
status, and the record of every change (ONBOARD). The backend keeps this
file true: `TestAPIDoc` (in `construction_db/internal/core/members`) fails if
the types below stop matching the Go code, or if an error text these
endpoints send isn't quoted here.

All endpoints need `Authorization: Bearer <access token>` from an **owner or
SHE officer** (the `manage_members` capability on `/v1/me`); anyone else gets
403 `"forbidden"`. Errors are `{"error": "<message>"}`.

Inviting people stays at `POST /v1/invites` (`docs/api/` has no separate
reference for invites; see the M4 notes): an invite creates the member with
status `invited`, and accepting it makes them `active`.

## How membership works

- **Statuses.**
  - `invited`: invited, hasn't accepted yet;
  - `active`: can sign in and work;
  - `suspended`: can't sign in; everything they did stays, and their sites
    are kept, so making them `active` again restores them as they were;
  - `removed`: gone from the organisation. A removed member is never
    deleted (their records name them); to bring them back, invite them again
    (`POST /v1/invites` with the same email or mobile). That reuses their
    account, so their history continues.
- **What a PATCH may do.** `active` ↔ `suspended`; `active`, `suspended` or
  `invited` → `removed` (removing an invited member also withdraws their
  invitation). Nothing else: 409 otherwise. The role of an `invited` or
  `removed` member changes only by inviting them again.
- **Who may change whom.**
  - An owner may change any other member, to any role.
  - A SHE officer may change any other member who isn't an owner, to any role
    but owner.
  - Nobody changes their own membership (to step down, ask another owner).
  - The organisation always keeps at least one active owner. Two owners
    demoting each other at the same moment: one succeeds, the other gets
    409 `"an organisation must keep at least one active owner"`.

  These are today's rules, until the role matrix is approved; the database
  enforces them too.
- **Every change needs a reason** (`reason`, 1–500 characters), and is
  recorded for good: who, when, from what, to what, why
  (`GET /v1/members/:userId/history`).
- **Suspending or removing takes effect at once.** The member's sessions in
  this organisation end: their very next request (even with an access token
  that hasn't expired) is 401, their refresh token is refused, and a phone
  syncing at that moment is refused on the next record of its batch. Emails
  queued to them are cancelled and no new reminders go to them. A role
  change signs no one out: the new role applies from the next request.
- **Sites (supervisors).** A supervisor sees only the sites they're on; the
  other roles see every site.
  - **Into supervisor:** send `site_ids` with the change (`[]` for none
    yet); they replace the person's sites. Only live sites (not archived).
  - **Out of supervisor, or removed:** their site list is cleared, and the
    answer's `cleared_site_ids` says which. Add them again through
    `PUT /v1/sites/:id/members/:userId` if needed.
  - **Suspended:** the list is kept.
  - `site_ids` goes only with a change into supervisor. To move an existing
    supervisor between sites, use the site member endpoints
    (`PUT`/`DELETE /v1/sites/:id/members/:userId`); every addition and
    removal, by any path, is in `site_history`.
- **Open work.** Nothing is reassigned when someone is suspended or removed.
  `open_work` lists the corrective actions they own that are still open or
  awaiting verification, each with `owner_eligible` (whether they can still
  act on it), and their sites. Reassign them on the action, which shows
  `owner_eligible: false` too (`actions.md`): after a suspension or removal,
  a change into a role that can't own actions, or being taken off the site.

## Endpoints

### `GET /v1/members` — the organisation's members

200 `MemberList`, by name. `?status=` one of `invited`, `active`,
`suspended`, `removed` lists only those; without it, everyone except the
removed. Use `?status=active` to pick people to add to a site. 400
`"status must be invited, active, suspended or removed"`.

### `GET /v1/members/:userId` — one member, with open work

200 `MemberDetail`. 404 `"member not found"` (also a malformed id or
someone in another organisation).

### `PATCH /v1/members/:userId` — change role, status or both

Body: `UpdateRequest`. 200 `MemberChange`: the member as they are now. A
PATCH that changes nothing is 200 and records nothing.

| Status | When |
|---|---|
| 400 | `"send role, status or both"`, `"reason must be 1-500 characters"`, `"role must be owner, she_officer, supervisor, claims_handler, contracts_manager or bookkeeper"`, `"status must be active, suspended or removed"`, `"site_ids is required when the role becomes supervisor"`, `"site_ids goes only with a change into supervisor"`, `"site_ids must name the organisation's sites that aren't archived"`, or the JSON decoder's own text |
| 403 | `"you can't change your own membership; ask another owner"`, `"only an owner can change an owner or grant the owner role"`, `"forbidden"` (not an owner or SHE officer) |
| 404 | `"member not found"` |
| 409 | `"an organisation must keep at least one active owner"`, `"that status change isn't allowed: active and suspended swap; active, suspended and invited members can be removed; a removed member comes back only by a new invite"`, `"an invited or removed member's role changes by inviting them again"` |

### `GET /v1/members/:userId/history` — the record of changes

200 `MemberHistory`, oldest first: `history` is each change of role,
status or name; `site_history` each time they were added to or removed from
a site. 404 `"member not found"`.

Rows from before ONBOARD (Thu 26 Nov 2026), invitations and their
acceptance carry no `reason`; a name change has no `prev_role` or
`prev_status`. `site_ids` and `prev_site_ids` are set only on a change that
changed the person's sites.

## Across the API

- 400 `"text must not contain NUL characters"`: a NUL character (`\u0000`) anywhere in the JSON body (any value or key, however nested), the query string or the path, on every endpoint, checked before the token (REG4 review). Postgres text can't store one.
- 401 `"authorization header required"`, `"invalid authorization header
  format"` or `"unauthorized"`.
- 413 `"request body too large; at most 1 MiB"`: on every endpoint, checked before the token (SYNC review).
- **503 `"server busy, try again"`** with a `Retry-After` header (seconds):
  the request ran out of time, or waited over 5 seconds for a record another
  request was changing (a member change waits for any other change to the
  same organisation's members). Retry after that many seconds.
- 500 `"internal error"`.

## Types for `web/src/lib/types.ts`

Times are RFC 3339 strings in UTC; `_on` fields are dates (`YYYY-MM-DD`).
Ids are UUIDs. `Role` is in `me.md`. In requests, a field typed `| null` may
also be left out.

<!-- types -->
```ts
export type MemberStatus = "invited" | "active" | "suspended" | "removed";

export type PatchStatus = "active" | "suspended" | "removed";

export type SiteChange = "added" | "removed";

export interface Member {
  user_id: string;
  full_name: string;
  email: string | null;
  mobile: string | null;
  role: Role;
  status: MemberStatus;
  site_ids: string[];
  created_at: string;
  updated_at: string;
}

export interface MemberList {
  members: Member[];
}

export interface OpenAction {
  id: string;
  site_id: string;
  title: string;
  status: "open" | "awaiting_verification";
  due_on: string;
  overdue: boolean;
  owner_eligible: boolean;
}

export interface OpenWork {
  actions: OpenAction[];
  site_ids: string[];
}

export interface MemberDetail {
  user_id: string;
  full_name: string;
  email: string | null;
  mobile: string | null;
  role: Role;
  status: MemberStatus;
  site_ids: string[];
  created_at: string;
  updated_at: string;
  open_work: OpenWork;
}

export interface MemberChange {
  user_id: string;
  full_name: string;
  email: string | null;
  mobile: string | null;
  role: Role;
  status: MemberStatus;
  site_ids: string[];
  created_at: string;
  updated_at: string;
  open_work: OpenWork;
  cleared_site_ids: string[];
}

export interface UpdateRequest {
  role?: Role | null;
  status?: PatchStatus | null;
  reason: string;
  site_ids?: string[] | null;
}

export interface HistoryEntry {
  role: Role;
  status: MemberStatus;
  full_name: string;
  prev_role: Role | null;
  prev_status: MemberStatus | null;
  reason: string | null;
  site_ids: string[] | null;
  prev_site_ids: string[] | null;
  changed_at: string;
  changed_by: string | null;
  changed_by_name: string | null;
}

export interface SiteHistoryEntry {
  site_id: string;
  change: SiteChange;
  changed_at: string;
  changed_by: string | null;
}

export interface MemberHistory {
  history: HistoryEntry[];
  site_history: SiteHistoryEntry[];
}
```
