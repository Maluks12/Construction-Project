# SiteProof admin portal

Internal portal for the SiteProof team (Self Hawk Tech): organisations, users,
billing, regime data, evidence health, background jobs, support and a staff
audit log. A separate Next.js 16 app from the customer dashboard in `../web`,
so it can be deployed and locked down on its own.

**Front end only for now.** The Go API has no admin endpoints yet, so every
page reads sample records from `src/lib/sample.ts`, nothing is fetched, and
the buttons are disabled. A banner on every page says so.

## Run it

```powershell
cd admin
npm install
npm run dev
```

Open http://localhost:3001 (the dashboard uses 3000).

## How it's put together

```
src/
  lib/sample.ts         Every page's data. Replace per page when endpoints exist
  components/ui.tsx     Copied from ../web so both apps look the same
  components/table.tsx  Table helper for the registers
  app/
    login/              Staff sign-in form (not connected)
    (portal)/           Overview, organisations, users, billing, regimes,
                        evidence, jobs, support, audit-log, settings
    globals.css         Same colour tokens as ../web
```

## Before this goes anywhere public

- It has **no sign-in**. The login page is only the form's shape. Staff auth
  (with a second factor) has to exist in the API first.
- Wiring it needs admin endpoints the backend doesn't have. Follow the web
  app's rule when that happens: tokens in httpOnly cookies, only server code
  calls the API, reads in `lib/data.ts`, writes in Server Actions.
- Decide what staff may see. The pages are designed to show counts, status
  and hashes, never a customer's inspections, incidents or photos.

## Checks

```powershell
npm run lint
npx tsc --noEmit
npm run build
```
