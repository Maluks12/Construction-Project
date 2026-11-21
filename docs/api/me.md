# Who am I: `GET /v1/me`

For the dashboard and the mobile app: who is signed in, their role in the
organisation, and what their role lets them do. The backend keeps this file
true: `TestAPIDoc` (in `construction_db/internal/core/auth`) fails if the
types below stop matching the Go code, and `TestCapabilitiesMatchRoutes`
fails if a capability stops matching the route it describes.

Needs `Authorization: Bearer <access token>`. Every role may call it.
It can answer 503 `"server busy, try again"` with a `Retry-After` header
(seconds): it ran out of time, or waited over 5 seconds for a record another
request was changing; retry after that many seconds.

## `GET /v1/me`

200 with `Me`. 401 `"unauthorized"` when the token is bad or expired, the
session was revoked, or the membership is no longer active (suspended or
removed).

- **Read on every call.** The role and capabilities come from the
  membership as it is now, not from the token or the session. A role
  change shows on the next call; a suspended member gets 401 and no list.
  Call it after login and whenever the app comes back to the foreground.
- **`org_name`** is the organisation's trading name.

## Capabilities: display hints, never permissions

`capabilities` tells a screen what to show: hide "Waive" from someone who
would get 403. **It decides nothing on the server.** Every endpoint enforces
its own guard and never reads this list, so an app that ignores it, or
shows a button it shouldn't, still gets the endpoint's own 403 or 404.

**For a supervisor, every capability means "on your own sites"** (the sites
`GET /v1/sites` returns). The same request on another site answers 404
`"site not found"`.

The list encodes today's route guards. The role matrix is awaiting
approval; when it is approved, this list and the guards change together.

| Capability | What it shows | owner | she_officer | supervisor (own sites) | claims_handler, contracts_manager, bookkeeper |
|---|---|---|---|---|---|
| `capture_records` | Photos, inspections, incidents, actions, toolbox talks, the diary, sign-ins, workers, document uploads | ✓ | ✓ | ✓ | – |
| `read_safety_file` | The site's H&S file | ✓ | ✓ | ✓ | – |
| `write_safety_file` | Add, version or retract H&S file documents | ✓ | ✓ | – | – |
| `manage_sites` | Create, edit and archive sites; site members and locations | ✓ | ✓ | – | – |
| `invite_members` | Invite people and see pending invites | ✓ | ✓ | – | – |
| `view_vault` | The organisation's statutory vault | ✓ | ✓ | – | – |
| `open_medical_certificates` | Open a medical certificate of fitness (every view is logged) | ✓ | ✓ | – | – |
| `verify_chain` | Check a site's evidence chain | ✓ | ✓ | – | – |
| `waive_findings` | Waive a low or medium finding | ✓ | ✓ | – | – |
| `verify_actions` | Verify or reject a corrective action's close-out; move a due date later | ✓ | ✓ | – | – |
| `manage_workers` | Edit workers, and retract competencies and medicals | ✓ | ✓ | – | – |
| `view_compliance` | The compliance summary of a site, and the rollup over the sites you can open | ✓ | ✓ | ✓ | – |
| `export_audit_pack` | Export an audit pack of a site, and download it | ✓ | ✓ | – | – |

Some writes depend on the record as well as the role (a supervisor may
reassign an action they raised, or close out one they own). Those aren't
capabilities: show the button and let the endpoint answer.

## Types for `web/src/lib/types.ts`

<!-- types -->
```ts
export type Capability = "capture_records" | "read_safety_file" | "write_safety_file" | "manage_sites" | "invite_members" | "view_vault" | "open_medical_certificates" | "verify_chain" | "waive_findings" | "verify_actions" | "manage_workers" | "view_compliance" | "export_audit_pack";

export type Role = "owner" | "she_officer" | "supervisor" | "claims_handler" | "contracts_manager" | "bookkeeper";

export interface Me {
  user_id: string;
  org_id: string;
  session_id: string;
  role: Role;
  full_name: string;
  org_name: string;
  capabilities: Capability[];
}
```
