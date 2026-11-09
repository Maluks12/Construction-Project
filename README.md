# SiteProof — Construction edition

South African site safety and compliance platform. This repo holds two apps:

| Folder | What it is | Runs on |
|---|---|---|
| `construction_db/` | Go API (Gin, pgx), PostgreSQL migrations, regime data | `http://localhost:8080` |
| `web/` | Next.js 16 dashboard for owners and SHE officers | `http://localhost:3000` |
| `docs/` | Product spec (Rev 4 is the source of truth) and `BUILD_PLAN.md` | — |

The API, the database and the file storage all run in Docker. The dashboard
runs with Node on your machine. Every command below is for **Windows
PowerShell**.

- [1. Prerequisites](#1-prerequisites)
- [2. First-time setup](#2-first-time-setup)
- [3. Start the backend](#3-start-the-backend)
- [4. Start the dashboard](#4-start-the-dashboard)
- [5. Day-to-day commands](#5-day-to-day-commands)
- [6. Configuration reference](#6-configuration-reference)
- [7. Running the API without Docker (optional)](#7-running-the-api-without-docker-optional)
- [8. Tests and checks](#8-tests-and-checks)
- [9. Looking inside the database and storage](#9-looking-inside-the-database-and-storage)
- [10. Testing uploads from a real phone](#10-testing-uploads-from-a-real-phone)
- [11. Troubleshooting](#11-troubleshooting)
- [12. Where to read next](#12-where-to-read-next)

---

## 1. Prerequisites

### Required

| Tool | Version | Why you need it |
|---|---|---|
| **Windows 10 (22H2) or 11** with virtualisation enabled in the BIOS | — | Docker Desktop runs Linux containers through WSL 2 |
| **WSL 2** | latest | The engine Docker Desktop uses on Windows |
| **Docker Desktop** | 4.x with Compose v2 (`docker compose`, not `docker-compose`) | Runs PostgreSQL 16, MinIO (local S3), the migration tool and the API |
| **Git** | any recent | Clone and pull the repo |
| **Node.js** | 22 LTS (Next.js 16 needs at least 20.9) | Runs the dashboard; includes `npm` |

### Optional

| Tool | Why |
|---|---|
| **Go 1.25.5 or newer** | Only to run the backend tests (`go test`) or run the API outside Docker. Docker builds the API without it. |
| **VS Code** with the Go and ESLint extensions | Editing |
| **DBeaver** or **pgAdmin** | Browsing the database (see section 9) |

### Install them

Open PowerShell **as Administrator**:

```powershell
wsl --install                                   # restart Windows if it asks
winget install -e --id Docker.DockerDesktop
winget install -e --id Git.Git
winget install -e --id OpenJS.NodeJS.LTS
winget install -e --id GoLang.Go                # optional
```

Then:

1. Restart Windows if WSL or Docker asked you to.
2. Start **Docker Desktop**. In *Settings → General*, make sure *Use the WSL 2
   based engine* is ticked. Wait until the status says Docker is running.
3. Close PowerShell and open a **normal (non-admin)** window, so the new
   programs are on your PATH.
4. Check everything is there:

```powershell
wsl --status
docker --version
docker compose version
git --version
node --version      # v22.x
npm --version
go version          # only if you installed Go
```

If `npm` fails with *"running scripts is disabled on this system"*, allow
locally created scripts once:

```powershell
Set-ExecutionPolicy -Scope CurrentUser RemoteSigned
```

### Docker images that get downloaded

The first run pulls these. You need internet access and roughly 1 GB of disk.

| Image | Used for |
|---|---|
| `postgres:16-alpine` | Database |
| `cgr.dev/chainguard/minio` (pinned by digest) | Local S3 for evidence photos |
| `cgr.dev/chainguard/minio-client` (pinned by digest) | Creating the evidence bucket |
| `migrate/migrate:v4.20.1` | Applying database migrations |
| `golang:1.25.5-alpine`, `gcr.io/distroless/static-debian12` | Building and running the API image |

MinIO no longer publishes its own images, so Chainguard's build is used. If
your network blocks `cgr.dev`, the MinIO containers won't start.

---

## 2. First-time setup

### Clone

```powershell
git clone <repo url> SiteProof
cd SiteProof
```

`.gitattributes` keeps Go files with Unix line endings, so `gofmt` passes in
CI even when Git converts line endings on Windows. You don't need to change
any Git settings.

### Create the backend `.env`

The backend reads its settings from `construction_db\.env`. That file is
git-ignored: never commit it.

```powershell
cd construction_db
Copy-Item .env.example .env
```

Generate three random secrets. Use hex, not passwords with symbols: the
Postgres password is placed inside a connection URL, where `@`, `/`, `:` or
`#` would break it.

```powershell
function New-Secret($n) {
  $b = New-Object byte[] $n
  [Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($b)
  -join ($b | ForEach-Object { $_.ToString('x2') })
}
"POSTGRES_PASSWORD    = $(New-Secret 16)"
"JWT_SECRET           = $(New-Secret 32)"
"S3_SECRET_ACCESS_KEY = $(New-Secret 16)"
```

Open the file and replace the placeholders:

```powershell
notepad .env
```

| Line | Set it to |
|---|---|
| `POSTGRES_PASSWORD` | the first secret |
| `DATABASE_URL` | the same Postgres password in place of `change-me` |
| `JWT_SECRET` | the second secret (must be 32+ characters) |
| `S3_SECRET_ACCESS_KEY` | the third secret (MinIO needs 8+ characters) |

Leave everything else as it is. Section 6 explains every setting.

> If you change `POSTGRES_PASSWORD` **after** the database has been created,
> Postgres keeps the old one. Either change it back or wipe the database with
> `docker compose down -v` (this deletes all local data).

---

## 3. Start the backend

Run these from `construction_db`, in this order:

```powershell
docker compose up -d db minio            # 1. database and file storage
docker compose run --rm storage-init     # 2. once: create the evidence bucket
docker compose run --rm migrate up       # 3. create the tables
docker compose up -d --build api         # 4. build and start the API
```

What each step does:

1. **db** starts PostgreSQL 16 on `localhost:5433` (5433, not 5432, so it
   doesn't clash with a Postgres you may already have). **minio** starts a
   local S3 on `localhost:9000`. Both have health checks; the other steps wait
   for them.
2. **storage-init** creates the `evidence` bucket with Object Lock switched on
   and a default retention of 1 day in GOVERNANCE mode, the same way
   production is set up. The API refuses to start without this.
3. **migrate** applies every file in `construction_db\migrations`, including
   the `siteproof_app` database role the API runs as and the row-level
   security rules.
4. **api** builds the Go image and starts it on `localhost:8080`. At startup
   it checks the config, the regime data, the database and the evidence
   bucket, and exits with a clear message if any of them is wrong.

Check it's running:

```powershell
docker compose ps                                # db, minio and api: running / healthy
Invoke-RestMethod http://localhost:8080/healthz  # status: ok
docker compose logs -f api                       # live API logs; Ctrl+C to stop watching
```

Use `Invoke-RestMethod` rather than `curl`: in Windows PowerShell 5.1, `curl`
is an alias for a different command.

---

## 4. Start the dashboard

Open a **second** PowerShell window at the repo root:

```powershell
cd web
npm install
Copy-Item .env.example .env.local        # first time only
npm run dev
```

Open <http://localhost:3000>. Click **Create an account** to make your first
company, your owner account and your first site. Passwords need at least 10
characters.

The dashboard never calls the API from the browser. Its server calls the API
at `API_URL` and keeps the login tokens in httpOnly cookies, so there's no
CORS setup to do.

To try other roles, go to **Team**, create an invite, copy the link, and open
it in a private browser window.

---

## 5. Day-to-day commands

### Backend (`construction_db`)

```powershell
docker compose up -d db minio api        # start everything after a reboot
docker compose up -d --build api         # rebuild after pulling backend code changes
docker compose run --rm migrate up       # after pulling new migrations
docker compose run --rm migrate version  # which migration the database is on
docker compose logs -f api               # API logs
docker compose stop                      # stop everything, keep the data
docker compose down                      # remove the containers, keep the data
docker compose down -v                   # remove containers AND all data (database + uploaded files)
```

After changing `.env`, recreate the API so it picks the change up:

```powershell
docker compose up -d --force-recreate api
```

After `docker compose down -v`, repeat section 3 from step 1 (the bucket and
the tables are gone).

### Dashboard (`web`)

```powershell
npm run dev                              # dev server on :3000
npm install                              # after package.json changes
```

### After `git pull`

```powershell
cd construction_db
docker compose run --rm migrate up
docker compose up -d --build api
cd ..\web
npm install
```

---

## 6. Configuration reference

### `construction_db\.env`

| Variable | Required | Default | Meaning |
|---|---|---|---|
| `POSTGRES_USER` | yes | `siteproof` | Database superuser created by the `db` container |
| `POSTGRES_PASSWORD` | yes | — | Its password. Hex only (see section 2) |
| `POSTGRES_DB` | yes | `siteproof` | Database name |
| `DATABASE_URL` | yes | — | Only used by `go run` (section 7). Docker Compose builds its own URL from the three values above, pointing at `db:5432` |
| `PORT` | no | `8080` | API port |
| `JWT_SECRET` | yes | — | Signs access tokens. 32+ characters |
| `REQUEST_TIMEOUT` | no | `10s` | One deadline per request. Past it, or with no free database connection, the API answers 503 `"server busy, try again"` with `Retry-After` |
| `DB_MAX_CONNS` | no | `20` | Database connections per API instance |
| `S3_BUCKET` | yes | `evidence` | Evidence bucket name |
| `S3_REGION` | no | `af-south-1` | S3 region |
| `S3_ACCESS_KEY_ID` | locally | `siteproof` | MinIO login; also the API's S3 key. Leave empty on AWS to use the default credential chain |
| `S3_SECRET_ACCESS_KEY` | locally | — | MinIO password, 8+ characters. Set both S3 keys or neither |
| `S3_ENDPOINT` | locally | — | Where the API reaches S3. Compose sets `http://minio:9000` itself; this value is for `go run` |
| `S3_PUBLIC_ENDPOINT` | no | `http://localhost:9000` | The host that upload links point at. It is part of the link's signature, so it must be an address the uploading device can reach (see section 10) |
| `EVIDENCE_LOCK_MODE` | yes | `GOVERNANCE` | Must match the bucket's default retention mode, or the API won't start |

### `web\.env.local`

| Variable | Default | Meaning |
|---|---|---|
| `API_URL` | `http://localhost:8080` | Where the dashboard's server reaches the Go API. Never sent to the browser |
| `APP_URL` | `http://localhost:3000` | The dashboard's own address, used to build invite links |

### Ports used

| Port | Service |
|---|---|
| 3000 | Dashboard (`npm run dev`) |
| 8080 | Go API |
| 5433 | PostgreSQL (mapped from 5432 inside Docker) |
| 9000 | MinIO S3 API |

---

## 7. Running the API without Docker (optional)

Useful with a debugger. Needs Go installed. The database and MinIO still run in
Docker.

```powershell
cd construction_db
docker compose up -d db minio            # if not already running
docker compose stop api                  # free port 8080
go run ./cmd/api
```

`go run` reads `construction_db\.env`, so `DATABASE_URL` must point at
`localhost:5433` and `S3_ENDPOINT` at `http://localhost:9000` (the defaults
in `.env.example`). Variables already set in your PowerShell session win over
`.env`.

If `go.mod` asks for a newer Go than you have, Go downloads the right
toolchain automatically the first time.

---

## 8. Tests and checks

### Backend

```powershell
cd construction_db
gofmt -l .           # must print nothing
go vet ./...
go test ./...        # Docker Desktop must be running
```

The tests start their own throwaway PostgreSQL and MinIO containers
(testcontainers), apply every migration, and remove the containers afterwards.
They don't touch your local database. The first run is slow while the images
download.

CI runs `go test -race`. The race detector needs a C compiler on Windows, so
plain `go test ./...` is the normal local command.

### Dashboard

```powershell
cd web
npm run lint
npx next typegen     # generates route types; tsc fails without them on a fresh clone
npx tsc --noEmit
npm run build
```

Run all four after every change. `npm run dev` and `npm run build` also
generate the route types, so `typegen` only matters if you haven't run either
yet.

---

## 9. Looking inside the database and storage

### SQL shell

```powershell
cd construction_db
docker compose exec db psql -U siteproof -d siteproof
```

Use the `POSTGRES_USER` and `POSTGRES_DB` from your `.env`. Useful commands:
`\dt` lists tables, `\d sites` describes one, `\q` quits.

You are connected as the superuser, which **bypasses row-level security**, so
you see every company's rows. The API sees only its own tenant's rows.

### GUI tool (DBeaver, pgAdmin)

Host `localhost`, port `5433`, database `siteproof`, user `siteproof`,
password = your `POSTGRES_PASSWORD`.

### Evidence files in MinIO

MinIO's web console isn't exposed. List the stored files with the MinIO
client:

```powershell
docker compose run --rm --entrypoint mc storage-init ls --recursive local/evidence
```

Files are under `orgs/<org id>/sites/<site id>/evidence/<evidence id>`. They
are locked for 1 day locally and can't be deleted before that, except by
wiping everything with `docker compose down -v`.

---

## 10. Testing uploads from a real phone

The upload link the API hands out points at `S3_PUBLIC_ENDPOINT`. A phone
can't reach `localhost` on your PC, so:

1. Find your PC's Wi-Fi address:

   ```powershell
   ipconfig        # "IPv4 Address" under your Wi-Fi adapter, e.g. 192.168.1.20
   ```

2. In `construction_db\.env`, set
   `S3_PUBLIC_ENDPOINT=http://192.168.1.20:9000` (your address).
3. Open the ports in Windows Firewall (PowerShell **as Administrator**):

   ```powershell
   New-NetFirewallRule -DisplayName "SiteProof MinIO" -Direction Inbound -Protocol TCP -LocalPort 9000 -Action Allow
   New-NetFirewallRule -DisplayName "SiteProof API"   -Direction Inbound -Protocol TCP -LocalPort 8080 -Action Allow
   ```

4. Recreate the API: `docker compose up -d --force-recreate api`.
5. Keep the phone on the same Wi-Fi network, and point the app at
   `http://192.168.1.20:8080`.

Your PC's address can change when you reconnect to Wi-Fi; repeat steps 1, 2
and 4 if uploads start failing with signature errors.

---

## 11. Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `docker: error during connect` / `cannot find the file specified` | Docker Desktop isn't running. Start it and wait for "running". |
| Docker Desktop says WSL 2 is missing or out of date | Run `wsl --update` as Administrator, then restart Docker Desktop. |
| `Bind for 0.0.0.0:5433 failed: port is already allocated` (or 9000, 8080) | Something else uses the port. Find it with `Get-NetTCPConnection -LocalPort 5433 \| Select-Object OwningProcess` then `Get-Process -Id <id>`. Stop it, or change the left-hand port in `docker-compose.yml` locally. |
| API exits: `missing required environment variable` | A required value in `.env` is empty. See section 6. |
| API exits: `JWT_SECRET must be at least 32 characters` | Generate a longer one (section 2). |
| API exits: `set role siteproof_app (run migrations first?)` | Migrations haven't run: `docker compose run --rm migrate up`. |
| API exits: `check evidence bucket ... object lock is not enabled` / `no default retention` / bucket not found | Run `docker compose run --rm storage-init`. |
| API exits: `default retention is GOVERNANCE, want COMPLIANCE` | `EVIDENCE_LOCK_MODE` must be `GOVERNANCE` locally. |
| API exits: `check regimes` | The database's regime rows and the code's regime data disagree, usually after pulling code without running migrations. Run `migrate up`, then rebuild the API. |
| `password authentication failed` | `POSTGRES_PASSWORD` changed after the database was created. Change it back, or `docker compose down -v` and start again. |
| `migrate` says `Dirty database version N` | A migration failed halfway. Read the error above it, fix the cause, then `docker compose run --rm migrate force <N-1>` and `migrate up` again. On a local database it's often simpler to `down -v` and start over. |
| Dashboard: *"Can't reach the SiteProof API at http://localhost:8080"* | The API isn't running, or `API_URL` in `web\.env.local` is wrong. Check `docker compose ps` and `Invoke-RestMethod http://localhost:8080/healthz`. |
| Dashboard keeps sending you back to the login page | Your session was revoked or expired, or the API was reset with `down -v`. Sign in again or create a new account. |
| *"This account belongs to more than one company"* at login | The dashboard can't pick a company yet. Use an account that belongs to one company. |
| `npm` / `npx`: *"running scripts is disabled"* | `Set-ExecutionPolicy -Scope CurrentUser RemoteSigned` |
| `npx tsc --noEmit`: *"Cannot find name 'PageProps'"* | Run `npx next typegen` first. |
| `go test` fails to start containers | Docker Desktop must be running. On a slow first run, the images are still downloading; try again. |
| Phone upload fails with `SignatureDoesNotMatch` or can't connect | `S3_PUBLIC_ENDPOINT` must be the exact address the phone uses, and port 9000 must be open (section 10). |

---

## 12. Where to read next

| File | What's in it |
|---|---|
| `CLAUDE.md` | How the team works, the API contract between backend and dashboard, architecture rules |
| `construction_db\CLAUDE.md` | Backend layout, conventions, the evidence engine, known risks and spec issues, status |
| `construction_db\internal\core\evidence\CHAIN.md` | The evidence hash chain, byte by byte, with test vectors |
| `construction_db\internal\regimes\SOURCES_za_construction_2014.md` | Legal sources for every checklist, deadline and incident type |
| `web\CLAUDE.md` and `web\README.md` | Dashboard rules, structure and status |
| `docs\BUILD_PLAN.md` | Milestones and due dates (everything due Sat 5 Dec 2026) |
| `docs\spec\SiteProof_Rev4_Construction_and_Mining.pdf` | The product spec |

Personal preferences for Claude (your name as reviewer, shell, how much
explanation you want) go in a `CLAUDE.local.md` next to the relevant
`CLAUDE.md`. Those files are git-ignored.