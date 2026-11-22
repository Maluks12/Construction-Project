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
                        and the type blocks in docs/api/*.md
    site.ts             Which site the sidebar's "Viewing" picker points at
    regime.ts           Incident types per regime (copied from docs/api)
    compliance.ts       Compliance summary labels, colours and record links
    upload.ts           Document file upload (hash, declare, PUT, complete)
    sample.ts           Data for the pages that aren't connected yet
  app/
    actions/            Server Actions: every write goes through here
    (auth)/             login, signup, invite (public)
    (dashboard)/        signed in. Sample data only: injury-on-duty,
                        delay-claims, subcontractors, sync-conflicts.
                        Everything else is wired
  components/           ui.tsx (plain), client.tsx (forms, buttons),
                        photo.tsx (open a photo), open-file.tsx (open a
                        document or pack), documents.tsx and
                        document-forms.tsx (H&S file and vault),
                        deadline.tsx, states.tsx
```

The look comes from the SiteProof prototype; the colour tokens are in
`app/globals.css`. The phone app (`../mobile`) and the team's admin portal
(`../admin`) use the same tokens.

**Tokens never reach the browser's JavaScript.** Login sets two httpOnly
cookies (`sp_access`, 15 min; `sp_refresh`, 30 days). Pages and actions read
them on the Next.js server and call the API with `Authorization: Bearer`.
The browser never calls the Go API, so the API needs no CORS setup.

**Refresh-token reuse.** The API revokes a whole session if a refresh token is
used twice. A browser can easily send the same old token twice (two tabs, or a
page plus its prefetches), so `lib/refresh.ts` shares one refresh call per
token and reuses its result for a minute.

**Roles.** `GET /v1/me` returns the role and its capabilities, read on
every request (`lib/site.ts`: `can("manage_sites")` and so on). They only
decide which controls to show; the API still enforces every rule.

## Checks before pushing

```powershell
npm run lint
npx tsc --noEmit
npm run build
```
