# SiteProof mobile app

Expo (SDK 58) / React Native app for supervisors and SHE officers on site:
inspections, incident reports, evidence photos, corrective actions and the
legal call-now prompt. It talks to the Go API in `../construction_db`.

## Run it

The API must be running first (see `../construction_db`), on port 8080.

```powershell
cd mobile
npm install
Copy-Item .env.example .env.local   # first time only, then set the address
npx expo start
```

Then press `a` for an Android emulator, or scan the QR code with a phone.

**The phone must be able to reach the API.** Set `EXPO_PUBLIC_API_URL` in
`.env.local`: `http://10.0.2.2:8080` for the Android emulator, or
`http://<your PC's Wi-Fi address>:8080` for a real phone on the same Wi-Fi
(`ipconfig` shows the address; Windows Firewall has to allow port 8080).
Photos upload straight to storage, so MinIO's address in the API's signed
links has to be reachable from the phone too.

The camera, secure storage and file modules are native code. They are in
Expo Go; a release build is made with EAS (`npx eas-cli@latest build`).

## How it's put together

```
src/
  theme.ts              Colours, fonts and icons from the prototype
  components/ui.tsx     Screen, Card, Row, Pill, Button, Field, tab bar...
  lib/
    api.ts              The only code that calls the Go API; tokens, refresh
    session.tsx         Who is signed in, which site the phone is on
    evidence.ts         Photo: shrink, hash, declare, upload, complete
    types.ts            Copied from ../web/src/lib/types.ts (keep identical)
    regime.ts           Incident types per regime (copied from ../web)
    sample.ts           Data for the screens that aren't connected yet
  app/                  Expo Router: one file per screen
    sign-in             Email or mobile + password
    index               Today: call-now banner, checks due, my actions
    inspect             Start or carry on an inspection
    inspection/[id]     Answer an item; .../complete to review and complete
    report              Report an incident (version 1)
    call/[deadlineId]   Log that a legal deadline was met
    witness/[incidentId] Take a witness statement
    action/[id]         Close out a corrective action with a photo
    alerts              Open deadlines and my overdue actions
    toolbox, diary, outbox   Sample data only (see below)
```

**Tokens** live in the device's secure storage (Keychain / Keystore) via
`expo-secure-store`. The access token lasts 15 minutes; `api.ts` swaps the
refresh token for a new pair, and shares one refresh call between every
request that needs it, because the API revokes a session whose refresh token
is used twice.

**Ids come from the phone.** Inspections, incidents, photos, close-outs and
call logs are all `PUT` under a UUID made up on the phone, so sending one
twice is harmless.

## What isn't built yet

- **Offline.** The spec is offline-first (SQLite queue, sync). This version
  needs signal to save and says so when it can't reach the server. The
  Outbox screen shows the planned design with sample data. Waits on SYNC.
- **Toolbox talks and the site diary** show sample data. Waits on M10.
- **Push notifications.** The Notifications screen lists what is open now.
- **Starting a correction** of a completed inspection, and amending an
  incident, are done on the dashboard for now.
- **Accepting an invite** in the app: the invite link opens the dashboard.
- Checked with `npx tsc --noEmit` and a Metro bundle, **not yet run on a
  device**. The photo flow in particular needs a run against the real API
  and MinIO.

## Checks

```powershell
npx tsc --noEmit
npx expo lint
npx expo-doctor
```
