# Vane (zeep-vane)

Vane is a self-hosted status page and uptime dashboard connected to Datadog SLOs. A company runs one instance of Vane against its own database, connects its Datadog account, registers the services it wants to expose, and publishes a public status page on its own subdomain — automatic TLS included, no external reverse proxy required.

This repository is single-tenant by design: **one Vane installation serves exactly one company.** There is no `company_id`/tenant column anywhere in the schema (see `.specs/STATE.md`, AD-002). If you need to serve multiple companies, you run multiple installations.

## Table of contents

- [Architecture overview](#architecture-overview)
- [Tech stack](#tech-stack)
- [Repository layout](#repository-layout)
- [Domain model](#domain-model)
- [Authentication & authorization](#authentication--authorization)
- [Public status page routing](#public-status-page-routing)
- [Configuration (environment variables)](#configuration-environment-variables)
- [Running in development](#running-in-development)
- [Docker Compose](#docker-compose)
- [Creating the first admin (owner)](#creating-the-first-admin-owner)
- [Running tests](#running-tests)
- [Database migrations](#database-migrations)
- [Building for production](#building-for-production)
- [Known gaps / backlog](#known-gaps--backlog)

## Architecture overview

Vane is a single Go binary (`cmd/vane`) that embeds the compiled React admin frontend and serves three distinct surfaces:

1. **Admin API + Admin SPA** (`internal/api/*_handler.go`, `web/`) — authenticated, cookie-session based. Company staff manage domains, services, the Datadog integration, incidents, status pages, and other admins here.
2. **Public status page** (`internal/api/public_status_handler.go`, `internal/router/host_router.go`) — unauthenticated. Resolved by the `Host` header of the incoming request: each published `status_pages` row owns a hostname (its own domain or a subdomain), and `HostRouter` dispatches to the public handler scoped to that specific page. No cross-tenant leakage is possible because each request is scoped to exactly one `StatusPage.ID` resolved from the hostname.
3. **A background poller** (`internal/poller`) — polls Datadog's SLO API on an interval (`POLL_INTERVAL_SECONDS`) once a Datadog integration is connected, and writes `status_snapshots` rows that both the admin dashboard and the public status page read from. The public status page **never** calls Datadog directly on a visitor request — it only ever reads the last snapshot the poller wrote, with a silent cache-fallback if the poller falls behind (visitors never see a technical error).

TLS for custom domains is automatic and on-demand: `internal/tls` wraps [CertMagic](https://github.com/caddyserver/certmagic) (the library behind Caddy's automatic HTTPS) so that any hostname belonging to a published, domain-attached status page gets a certificate issued via ACME the first time it's requested — no manual certificate management, no external proxy like nginx/Caddy/Traefik in front of the binary.

```
                        ┌─────────────────────────────┐
                        │        cmd/vane serve         │
                        │                               │
   :8080 (HTTP)         │  ┌─────────────────────────┐  │
   Admin API + SPA ─────┼─▶│ internal/cli.buildAdminRouter│
   (session cookie)     │  └─────────────────────────┘  │
                        │                               │
   :443 (HTTPS, on-demand TLS via CertMagic)             │
   Public status pages ─┼─▶ internal/router.HostRouter  │
   (Host-header routed) │        │                       │
                        │        ▼                       │
                        │  internal/api.PublicStatusHandler│
                        │                               │
                        │  internal/poller (background) │──▶ Datadog SLO API
                        │        │                       │
                        │        ▼                       │
                        │      Postgres                  │
                        └─────────────────────────────┘
```

## Tech stack

**Backend** — Go 1.26
- [chi](https://github.com/go-chi/chi) — HTTP router
- [pgx/v5](https://github.com/jackc/pgx) — Postgres driver, no ORM
- [jwt/v5](https://github.com/golang-jwt/jwt) — session tokens
- [zap](https://github.com/uber-go/zap) — structured logging
- [cobra](https://github.com/spf13/cobra) — CLI (`serve`, `migrate up`)
- [CertMagic](https://github.com/caddyserver/certmagic) — automatic ACME TLS
- `golang.org/x/crypto/bcrypt` — admin password hashing

**Frontend** (`web/`) — React 18 + TypeScript, built with Vite
- React Router — client-side routing
- TanStack Query — server-state/data-fetching layer
- Tailwind CSS v4 — styling (design system: **Nocturne**, see `dashboard-handoff/README.md` and `status-page-handoff/README.md` for the original design references)
- i18next / react-i18next — internationalization
- Radix UI (`react-dialog`) — accessible dialog primitive
- Vitest + Testing Library + **MSW** (Mock Service Worker) — component/hook tests, with real `fetch` calls intercepted by MSW rather than a hand-rolled mock router

**Database** — PostgreSQL (plain SQL migrations, no ORM/query builder; see [Database migrations](#database-migrations))

## Repository layout

```
cmd/vane/              main() — delegates straight to internal/cli
internal/
  api/                 HTTP handlers, one file per resource (*_handler.go)
  auth/                password hashing (bcrypt) + session token primitives
  audit/               admin audit log writer
  cli/                 cobra commands (serve, migrate up) + admin router wiring
  config/              env var loading/validation (internal/config.Load)
  connectors/datadog/  Datadog API client (credential validation, SLO search)
  crypto/              encryption for stored Datadog credentials
  db/                  pgx pool, repositories (one per table), migration runner
  db/migrations/       plain numbered .up.sql / .down.sql pairs
  dbtest/              test-only Postgres locking helpers
  logging/             zap logger construction
  poller/              background SLO polling loop + retry logic
  router/              base router (/healthz) + HostRouter (public status pages)
  tls/                 CertMagic wiring (on-demand TLS policy)
  uploads/             local filesystem storage for uploaded logos
web/
  src/
    auth/              AuthProvider (session state), session-expired handling
    features/          one folder per admin-facing domain area:
                        admins, domains, incidents, integrations, poller,
                        public-status, services, settings, status-pages
    layout/            Sidebar, EmptyState, shared shell components
    lib/                apiClient (fetch wrapper), mockData (pre-integration
                        fixtures, superseded by real API calls), i18n
    routes/            RequireRole route guard
    test/               Vitest setup + MSW handlers
dashboard-handoff/      original HTML/CSS design reference for the admin SPA
status-page-handoff/    original HTML/CSS design reference for the public page
.specs/                 spec-driven feature history: STATE.md (decisions +
                        handoff log per feature), LESSONS.md, per-feature specs
```

## Domain model

Core tables (see `internal/db/migrations/` for exact schema and `internal/db/*_repository.go` for the Go side):

| Table | Purpose |
|---|---|
| `admins` | Company staff accounts. `role` is one of `owner`, `operator`, `viewer` (fixed roles, no configurable permission matrix — see AD-003). |
| `admin_invites` | Pending invitations (email + role) before an invited admin accepts and sets a password. |
| `admin_audit_log` | Append-only log of admin-management actions (invite, role change, removal). |
| `password_reset_tokens` | Single-use tokens for the forgot-password flow. |
| `integrations` | Stored (encrypted) Datadog API key + application key pair, one row per installation. |
| `services` | Logical services the company wants to expose, each mapped to a Datadog SLO. |
| `status_snapshots` | Poller-written point-in-time status per service — what both the admin dashboard and public page actually read. |
| `domains` | Custom hostnames the operator has registered with Vane (validated ownership, DNS target). |
| `status_pages` | A publishable page: a set of services + incidents to expose, an optional `domain_id`/`subdomain` (nullable — a page can exist and be previewed before any domain is attached, see AD-008), and a `state` (`draft`/`published`). |
| `incidents` + updates | Incident timeline entries linked to one or more services, surfaced on the public page for 90 days after resolution. |
| `company_settings` | Singleton row (`CHECK (id = 1)`) — company display name + uploaded logo, shown on the public status page. |

## Authentication & authorization

- Admin login (`POST /api/auth/login`) issues a **JWT session token stored in an `httpOnly`, `Secure`, `SameSite=Strict` cookie** — never in `localStorage`/`sessionStorage`, never read by frontend JavaScript (AD-004). The JWT carries only `sub`/`iat`; the current admin's role is always fetched fresh from `GET /api/auth/me`, not decoded client-side.
- Three fixed roles, enforced per-route in `internal/cli/routes.go` via `api.RequireRole(...)`:
  - **owner** — everything, plus admin management (invite/role-change/remove) and company settings.
  - **operator** — all `mvp-core` write routes (domains, services, integrations, incidents, status pages) and reads.
  - **viewer** — read-only across `mvp-core` resources and poller status.
- `POST /api/auth/logout` clears the cookie; an admin's `sessions_revoked_at` timestamp can invalidate all of that admin's existing sessions at once (used on role change/removal).

## Public status page routing

A visitor's request is dispatched purely by `Host` header (`internal/router/host_router.go`): if the hostname matches a **published** `status_pages` row, the request is routed to `PublicStatusHandler`, scoped to that exact page's services and incidents — nothing else in the installation is reachable through that hostname. Any other hostname falls through to the base router.

There is also an **authenticated preview endpoint**, `GET /api/status-pages/{id}/public-preview`, used by the admin SPA so a company can see what a status page will look like before a domain is attached and DNS/TLS have propagated. Unlike the public path, the preview does **not** require `state == "published"` and works even with no domain attached at all (AD-008) — it is admin-only and was never meant to mirror production 1:1.

## Configuration (environment variables)

Loaded by `internal/config.Load()` (`internal/config/config.go`). A `.env` file at the repo root is loaded automatically if present (via `godotenv`); its absence is not an error.

| Variable | Required | Default | Notes |
|---|---|---|---|
| `DATABASE_URL` | yes | — | Postgres connection string, e.g. `postgres://user:pass@host:5432/dbname?sslmode=disable` |
| `VANE_MASTER_KEY` | yes | — | Symmetric key used to encrypt stored Datadog credentials at rest |
| `VANE_SESSION_SECRET` | yes | — | Signing secret for session JWTs |
| `PORT` | yes | — | Port the admin HTTP API (and SPA) listens on |
| `POLL_INTERVAL_SECONDS` | yes | — | How often the poller queries Datadog for SLO status |
| `LOG_LEVEL` | no | `info` | zap log level |
| `CORS_ALLOWED_ORIGIN` | no | `http://localhost:5173` | Single allowed CORS origin — defaults to the Vite dev server |
| `UPLOADS_DIR` | no | `./data/uploads` | Local filesystem path for uploaded logos. **In production, must point to a volume that survives restarts** — there is no shared/object storage support yet (see [Known gaps](#known-gaps--backlog)) |
| `PUBLIC_DNS_TARGET` | no | *(empty)* | The DNS target (e.g. an IP or CNAME) this instance's admins should point their custom domain at. Left empty, the "attach domain" screen shows "not configured" instead of blocking — Vane cannot reliably discover its own public hostname |
| `HTTPS_PORT` | no | `443` | Port the public, TLS-terminated status page listener binds to |
| `CERTMAGIC_STORAGE_PATH` | no | `./certmagic-data` | Where CertMagic persists issued certificates. **Must be a persistent volume in production** |

## Running in development

You need: Go 1.26+, Node 18+, and a Postgres instance reachable from your machine (a local Docker container is the easiest path — see below).

### 1. Start Postgres

Any reachable Postgres 14+ works. Example with Docker:

```bash
docker run -d --name vane-dev-pg \
  -e POSTGRES_USER=vane -e POSTGRES_PASSWORD=vane -e POSTGRES_DB=vane \
  -p 5432:5432 postgres:16-alpine
```

### 2. Configure environment

Create a `.env` file at the repo root (loaded automatically by the Go binary):

```bash
DATABASE_URL=postgres://vane:vane@localhost:5432/vane?sslmode=disable
VANE_MASTER_KEY=dev-master-key-change-me-0123456789
VANE_SESSION_SECRET=dev-session-secret-change-me-0123456789
PORT=8080
POLL_INTERVAL_SECONDS=60
```

`CORS_ALLOWED_ORIGIN` doesn't need to be set — its default (`http://localhost:5173`) already matches Vite's dev server.

### 3. Run migrations

```bash
go run ./cmd/vane migrate up
```

### 4. Start the backend

```bash
go run ./cmd/vane serve
```

This starts the admin API on `:8080` (plain HTTP, fine for local dev) and the public/TLS listener on `:443` (CertMagic's on-demand TLS only works for real, publicly resolvable hostnames — it is not meaningful against `localhost`; ignore it for local admin-side dev). `GET /healthz` on `:8080` returns `{"status":"ok"}` once the server is up.

### 5. Start the frontend

```bash
cd web
npm install
npm run dev
```

Vite serves the SPA on `:5173` and talks to the backend at `http://localhost:8080` via `VITE_API_BASE_URL` (already set in `web/.env.development`).

### Makefile shortcuts

See the `dev-*` targets in the [Makefile](./Makefile) — `make dev-db`, `make migrate`, `make dev-backend`, `make dev-frontend`, and `make dev` to run backend + frontend together.

## Docker Compose

The fastest way to run a self-hosted instance is the shipped [`docker-compose.yml`](./docker-compose.yml): a single command builds the image (multi-stage `Dockerfile` — frontend build, Go build, then a minimal `scratch` runtime image with no Node.js or Go toolchain) and brings up Postgres alongside it, migrations applied automatically on boot.

`VANE_MASTER_KEY` and `VANE_SESSION_SECRET` have no default in `docker-compose.yml` — `docker compose up` refuses to start without them, so a real deployment can never accidentally run with a secret that is public on GitHub. Generate two random values and put them in a `.env` file next to `docker-compose.yml` (docker compose loads it automatically):

```bash
echo "VANE_MASTER_KEY=$(openssl rand -hex 32)" >> .env
echo "VANE_SESSION_SECRET=$(openssl rand -hex 32)" >> .env
docker compose up -d
```

Once both services report healthy, visit `http://localhost:8080` to complete first-time setup — see [Creating the first admin (owner)](#creating-the-first-admin-owner) below. `http://localhost` is a browser-exempted secure context so the session cookie works there; the admin login cookie is `Secure`-flagged, so deploying to a host reached by IP or internal hostname over plain HTTP means the browser silently drops it after login — put a TLS-terminating reverse proxy in front before exposing this beyond your own machine.

`make build` (frontend build, then the Go binary) is the one-command build path outside Docker too, if you'd rather run the resulting `bin/vane` directly against your own Postgres.

## Creating the first admin (owner)

A fresh, admin-less instance lands on an in-product **bootstrap screen** (`/bootstrap`) instead of the login screen — create the owner's email and password there directly in the browser, no SQL or throwaway script required. The screen is only reachable while the `admins` table is empty; once an owner exists, `/bootstrap` redirects to `/login` instead.

## Running tests

```bash
# Backend
go test ./...
go vet ./...
gofmt -l .        # should print nothing

# Frontend
cd web
npm run test        # vitest run
npx tsc -b --noEmit  # type-check
```

Backend tests that touch Postgres use `internal/dbtest` and expect a reachable test database — check `internal/dbtest/lock.go` and any `*_test.go` file's `TestMain`/setup for the connection string it expects before running the full suite.

## Database migrations

Plain, numbered SQL files in `internal/db/migrations/` (`NNNN_description.up.sql` / `.down.sql`), applied by a minimal runner in `internal/db` — no external migration framework, no ORM.

```bash
go run ./cmd/vane migrate up
```

There is currently only a `migrate up` subcommand (no `down`/rollback command exposed via the CLI, even though `.down.sql` files exist).

## Building for production

```bash
make build   # go build -o bin/vane ./cmd/vane
```

The frontend is built and embedded into the Go binary via `go:embed` (per `.specs/STATE.md` AD-001) — build `web/` (`npm run build`) before building the Go binary if you're producing a release artifact, so the embedded assets are current.

## Known gaps / backlog

Tracked in `.specs/STATE.md`. Not yet solved, not yet requested to be solved:

- Admin invite **resend/cancel** — frontend hooks exist, backend endpoints don't.
- No test coverage for a 404 on updating a non-existent incident.
- No auto-discovery of Datadog services/SLOs/monitors (would have to be added as a connector feature).
- `UPLOADS_DIR` assumes a single filesystem — **multi-replica deployments need a shared/RWX volume or object storage**; not implemented.
- An intermittent `pg_advisory_lock` test flake under parallel test execution (`internal/dbtest`) — recommended fix is to pin `go test -p 1` in CI, not yet applied.
- Manual validation against a **real** Datadog account/API (current test coverage relies on MSW/mocks) hasn't been done.
