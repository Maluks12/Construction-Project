# Site locations API: zones and floors

For the dashboard and the mobile app: the places inside a site (Rev 4 §5.3),
which inspections, housekeeping walks and hazards will later point at. The
backend keeps this file true: `TestAPIDoc` (in
`construction_db/internal/core/sites`) fails if the types below stop
matching the Go code, or if an error text the location endpoints send isn't
quoted here.

All endpoints need `Authorization: Bearer <access token>`. Errors are
`{"error": "<message>"}`.

## How locations work

- **A tree on one site.** A location is a `zone` or a `floor`, optionally
  under a parent location on the same site (e.g. zone "Block A", floors
  "Ground" and "Level 1" under it). Any kind may sit under any kind.
- **Names** are unique, ignoring case, among the active children of one
  parent (or among the site's top-level locations). Leading and trailing
  spaces are trimmed.
- **Order (ONBOARD).** `position` is an optional whole number from 0 to
  9999 that orders a location among its siblings: numbered ones first, by
  number, then those without one, by name. Use it for floors in building
  order ("Basement" 0, "Ground" 1, "Level 1" 2). The list is flat and
  sorted that way, so the order holds within each parent; build the tree on
  `parent_id`.
- **Archiving** hides a location from the list. A location with active
  children can't be archived (archive the children first), and a new
  location can't go under an archived one. An archived name can be used
  again.
- **Who:** everyone who can see the site reads its locations (a supervisor
  only on sites they've been added to; any other site is 404). Owners and
  SHE officers create, change and archive them; anyone else gets 403
  `"forbidden"`.
- **Offline:** a phone pulls a site's locations as `location_list`
  (`sync.md`); a change to any location re-sends the whole list. Writes are
  online only.

## Endpoints

### `GET /v1/sites/:siteId/locations` — the site's active locations

200 `LocationList`, ordered as above. 404 `"site not found"`.

### `POST /v1/sites/:siteId/locations` — add a location

Body: `CreateLocationRequest`. 201 with the `Location`.

| Status | When |
|---|---|
| 400 | `"name is required"` (blank after trimming), `"position must be a whole number from 0 to 9999, or null"`, `"parent location not found"` (not an active location of this site), or the binding's own text (unknown `kind`, a name over 100 characters, a malformed `parent_id`) |
| 404 | `"site not found"` |
| 409 | `"a location with that name already exists here"` |

### `PATCH /v1/sites/:siteId/locations/:locationId` — rename or reorder

Body: `UpdateLocationRequest`. A field left out is kept; `"position": null`
clears the position. 200 with the `Location`.

| Status | When |
|---|---|
| 400 | `"send name, position or both"` (neither sent), `"name is required"` (blank after trimming), `"position must be a whole number from 0 to 9999, or null"`, or the binding's own text (a name over 100 characters) |
| 404 | `"location not found"` (also an archived one, another site's, or a malformed id) |
| 409 | `"a location with that name already exists here"` |

### `DELETE /v1/sites/:siteId/locations/:locationId` — archive

204.

| Status | When |
|---|---|
| 404 | `"location not found"` |
| 409 | `"location has active child locations"` |

## Across the API

- 400 `"text must not contain NUL characters"`: a NUL character (`\u0000`) anywhere in the JSON body (any value or key, however nested), the query string or the path, on every endpoint, checked before the token (REG4 review). Postgres text can't store one.
- 400 with the JSON decoder's own text: a body that isn't valid JSON, or a `position` that isn't a whole number.
- 401 `"authorization header required"`, `"invalid authorization header
  format"` or `"unauthorized"`.
- 413 `"request body too large; at most 1 MiB"`: on every endpoint, checked before the token (SYNC review).
- **503 `"server busy, try again"`** with a `Retry-After` header (seconds):
  the request ran out of time, or waited over 5 seconds for a record another
  request was changing. Retry after that many seconds.
- 500 `"internal error"`.

## Types for `web/src/lib/types.ts`

Times are RFC 3339 strings in UTC. Ids are UUIDs. In requests, a field typed
`| null` may also be left out.

<!-- types -->
```ts
export type LocationKind = "zone" | "floor";

export interface Location {
  id: string;
  site_id: string;
  parent_id: string | null;
  kind: LocationKind;
  name: string;
  position: number | null;
  created_at: string;
}

export interface LocationList {
  locations: Location[];
}

export interface CreateLocationRequest {
  kind: LocationKind;
  name: string;
  parent_id?: string | null;
  position?: number | null;
}

export interface UpdateLocationRequest {
  name?: string | null;
  position?: number | null;
}
```
