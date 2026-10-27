# SiteProof web dashboard

Next.js 16 (App Router, TypeScript, Tailwind 4) dashboard for owners and SHE
officers. It talks to the Go API in `../construction_db`.

## Run it

The API must be running first (see `construction_db`), on port 8080.

```powershell
cd web
npm install
Copy-Item .env.example .env.local   # first time only
npm run dev
```

Open http://localhost:3000.

## How it's put together

```
src/
  proxy.ts              Runs before every page: signed-out -> /login, and swaps
                        an expired access token for a new pair (Next 16 renamed
                        middleware.ts to proxy.ts)
  lib/
    api.ts              The only code that calls the Go API (server-side only)
    data.ts             Reads used by pages (one function per endpoint)
    refresh.ts          Refresh-token swap, de-duplicated (see below)
    session.ts          Cookie names and options
    errors.ts           Turns validator errors into readable messages
    types.ts            Response shapes; keep in step with model.go json tags
  app/
    actions/            Server Actions: every write goes through here
    (auth)/             login, signup, invite (public)
    (dashboard)/        sites, sites/[id], team, account (signed in)
  components/           ui.tsx (plain) and client.tsx (forms, buttons)
```

**Tokens never reach the browser's JavaScript.** Login sets two httpOnly
cookies (`sp_access`, 15 min; `sp_refresh`, 30 days). Pages and actions read
them on the Next.js server and call the API with `Authorization: Bearer`.
The browser never calls the Go API, so the API needs no CORS setup.

**Refresh-token reuse.** The API revokes a whole session if a refresh token is
used twice. A browser can easily send the same old token twice (two tabs, or a
page plus its prefetches), so `lib/refresh.ts` shares one refresh call per
token and reuses its result for a minute.

**Roles.** `/v1/me` doesn't return the role yet, so the dashboard learns it
from the API's 403s: if the site members list is forbidden, the person isn't
an owner or SHE officer and the edit controls are hidden. The API still
enforces every rule.

## Checks before pushing

```powershell
npm run lint
npx tsc --noEmit
npm run build
```
