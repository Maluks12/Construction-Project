@AGENTS.md

# Frontend: Next.js dashboard

Read `../CLAUDE.md` first (shared rules and the API contract with the Go API).
Run everything from `web/`. How the app is put together is in `README.md`.

Next.js 16 has breaking changes from older versions (the import above says to
read `node_modules/next/dist/docs/` first). Things that already bit us:
`middleware.ts` is now `src/proxy.ts`; `cookies()`, `params` and
`searchParams` are async; the error boundary prop is `retry`; route types come
from `npx next typegen` (`PageProps<"/sites/[id]">`). Cache Components is
switched off on purpose in `next.config.ts`.

## Commands

```
npm install
npm run dev            # http://localhost:3000, needs the API on :8080
npm run lint
npx tsc --noEmit
npm run build
```
Run lint, tsc and build after each change and report the results.

## Rules

- **The browser never sees a token or calls the Go API.** Tokens live in
  httpOnly cookies (`sp_access`, `sp_refresh`). Only server code calls the
  API, through `src/lib/api.ts` (marked `server-only`). Don't add
  `NEXT_PUBLIC_` API URLs or client-side fetches to the API.
- Reads go in `src/lib/data.ts`, one function per endpoint. Writes are Server
  Actions in `src/app/actions/`, returning `FormState` for `useActionState`.
  Every action relies on `api()` for auth; never trust the client for
  authorization.
- Refresh tokens are only swapped in `src/lib/refresh.ts`, which shares one
  refresh per token. Never call `/v1/auth/refresh` anywhere else, or the API's
  reuse detection will revoke the session.
- `src/lib/types.ts` mirrors the Go `json` tags. If a response shape looks
  different from the type, check `construction_db/internal/core/*/model.go`;
  don't patch around it.
- Show API errors through `friendlyMessage` (`src/lib/errors.ts`), never raw
  validator text.
- Hide controls a role can't use, but remember the API is what enforces it.
- UI pieces: plain ones in `src/components/ui.tsx`, interactive ones in
  `src/components/client.tsx`. Use the colour tokens in `globals.css`
  (`bg-surface`, `text-muted`, `border-line`...), not raw colours, so dark
  mode keeps working.
- Dates show in `Africa/Johannesburg`, via `src/lib/format.ts`.

## Status

- **Done:** sign in (email or mobile), sign up, accept invite, sign out;
  sites list and create; site page with edit, archive, nested locations
  (add, rename, archive) and members (list, remove); team page with invite
  (copyable link) and revoke; account page with devices.
- **Done, restyled to the prototype** (dark sidebar, rust accent, Inter and
  Space Grotesk via `@fontsource-variable`): every page. The sidebar's
  "Viewing" picker chooses the site (cookie `sp_site`, see `lib/site.ts`),
  because the safety endpoints are all per site.
- **Wired to the API** (reads in `lib/data.ts`, writes in `app/actions/`):
  overview (the M12 rollup, `GET /v1/compliance`) and site compliance;
  notifications (inbox, mark read, the sidebar count); inspections list and
  detail (abandon, which inspection corrects it); findings & actions (raise,
  waive, retract a waiver, reassign, move due, verify, reject, cancel,
  reopen); incidents (register, detail, report, amend, witness statement,
  raise an action); deadlines (record as met with an H&S file proof for site
  work, retract); notifications & permits (site work, REG4); toolbox talks
  (read, link or unlink a signature, withdraw one) and the site diary (read,
  with conflicts and addenda); people & documents (worker register,
  competencies, medicals, sign-ins and roster, appointments, the H&S file);
  the statutory vault on Organisation; audit packs (queue, follow, download);
  reminders (the regime's lead times and your own reminders); adding someone
  to a site; evidence (list, view a photo, chain check). Built against
  `docs/api/*.md`, **not yet run against the real API**: check each write
  once with it.
- **Roles** come from `GET /v1/me` (`lib/site.ts`: `capabilities()`, `can()`),
  not from 403s. They only hide controls; the API enforces every rule.
- **Uploads** (certificates, letters, H&S file and vault versions) go through
  a Server Action to `lib/upload.ts`, which hashes, declares, PUTs the bytes
  to the signed link and completes the file on the Next.js server.
  `next.config.ts` raises the Server Action and proxy body limits to 21 MB
  for it.
- **Sample data only** (`lib/sample.ts`, each page shows a banner): injury
  on duty, delay claims, subcontractors, sync conflicts; the company details
  card on Organisation (except the trading name) and the subscription card
  on Team.
- **Not on the dashboard:** answering an inspection, close-outs, taking
  photos, signing toolbox talks and signing people in (phone only); the
  evidence access log (no read endpoint yet).
- `lib/regime.ts` copies the incident types table from
  `docs/api/incidents.md`; there is no endpoint for them. Ask the backend
  for one, or keep the two in step by hand.
- **Blocked on the backend** (see "Requested by the frontend" in
  `../construction_db/CLAUDE.md`): listing every org member (ONBOARD's
  `GET /v1/members`; adding to a site offers people on other sites and
  pending invitees until then); organisation details; reading the evidence
  access log; sending invites by SMS or email.
