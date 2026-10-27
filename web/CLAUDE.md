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
- **Blocked on the backend** (see "Requested by the frontend" in
  `../construction_db/CLAUDE.md`): role and names in `/v1/me`; listing org
  members to add people to sites; sending invites by SMS or email.
