<div align="center">
  <p><strong>Self-hosted status page and uptime dashboard, powered by your own Datadog SLOs.</strong></p>

  <p>
    <a href="https://github.com/zeeplabs/zeep-vane/actions"><img src="https://github.com/zeeplabs/zeep-vane/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
    <a href="https://go.dev/doc/devel/release"><img src="https://img.shields.io/badge/go-1.26+-00ADD8?logo=go" alt="Go" /></a>
    <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue.svg" alt="License" /></a>
    <img src="https://img.shields.io/badge/single--tenant-one%20install%20%3D%20one%20company-informational" alt="Single-tenant" />
  </p>
</div>

---

**Vane** connects to your Datadog account, polls the SLOs behind the services you care about, and publishes a public status page on your own subdomain — automatic TLS included, no external reverse proxy required. One binary, your Postgres, your data.

This repository is single-tenant by design: **one Vane installation serves exactly one company.** There is no `company_id`/tenant column anywhere in the schema (see `.specs/STATE.md`, AD-002). If you need to serve multiple companies, you run multiple installations.

```bash
docker compose up -d
# → http://localhost:8080 — bootstrap the first admin, connect Datadog,
#   register services, publish a status page on your own subdomain
```

---

## 📑 Index

- [Features](#-features)
- [Quick start](#-quick-start)
- [Architecture overview](#%EF%B8%8F-architecture-overview)
- [Tech stack](#-tech-stack)
- [Repository layout](#-repository-layout)
- [Domain model](#-domain-model)
- [Authentication & authorization](#-authentication--authorization)
- [Public status page routing](#-public-status-page-routing)
- [Configuration](#-configuration)
- [Running in development](#%EF%B8%8F-running-in-development)
- [Docker Compose](#-docker-compose)
- [Running tests](#-running-tests)
- [Database migrations](#-database-migrations)
- [Building for production](#-building-for-production)
- [Known gaps / backlog](#-known-gaps--backlog)
- [Contributing](#-contributing)
- [License](#-license)

---

## ✨ Features

### Status pages & incidents

| Feature | Description |
| --- | --- |
| **Custom domains, automatic TLS** | Attach your own hostname to a status page; CertMagic issues an ACME certificate on first request — no nginx/Caddy/Traefik in front |
| **Draft → Published pages** | A page can exist and be previewed (`/api/status-pages/{id}/public-preview`) before any domain is attached (AD-008) |
| **Incident timelines** | Incidents link to one or more services, surfaced on the public page for 90 days after resolution |
| **Hourly uptime history** | Public page renders per-service hourly status bars computed from `status_intervals` |
| **White-label branding** | Company name + logo shown on the public page (`company_settings` singleton) |
| **Paginated admin lists** | Domains, services, status pages, admins, incidents, poller status, and public resolved-incidents all paginate (`Page[T]` envelope, AD-012) |

### Datadog integration

| Feature | Description |
| --- | --- |
| **Background SLO poller** | Polls Datadog's SLO API on `POLL_INTERVAL_SECONDS`, writes open/closed `status_intervals` — never called live on a visitor request |
| **Silent cache-fallback** | If the poller falls behind, the public page still serves the last known interval — visitors never see a technical error |
| **Encrypted credentials** | Datadog API key + application key stored encrypted at rest (`internal/crypto`) |
| **Per-service SLO mapping** | Each exposed service maps to one Datadog SLO |

### Platform

| Feature | Description |
| --- | --- |
| **Single Go binary** | Embeds the compiled React admin SPA via `go:embed` — no Node.js/Go toolchain needed at runtime |
| **Three fixed roles** | `owner` / `operator` / `viewer`, enforced per-route (`internal/api.RequireRole`) — no configurable permission matrix (AD-003) |
| **Cookie-based sessions** | JWT in an `httpOnly`, `Secure`, `SameSite=Strict` cookie — never in `localStorage` (AD-004) |
| **Admin audit log** | Append-only log of admin-management actions (invite, role change, removal) |
| **Rate limiting** | Shared per-client-IP limit across login, password-reset, invite-accept, and bootstrap (10 req/min, burst 10) |
| **i18n** | Admin SPA and public page copy in pt-BR / English |
| **CLI** | `vane serve`, `vane migrate up` |
| **Docker Compose** | One-command self-hosted deploy, migrations applied automatically on boot |

---

## 🚀 Quick start

### Docker Compose

```bash
echo "VANE_MASTER_KEY=$(openssl rand -hex 32)" >> .env
echo "VANE_SESSION_SECRET=$(openssl rand -hex 32)" >> .env
docker compose up -d
```

`docker-compose.yml` builds the image (multi-stage `Dockerfile` — frontend build, Go build, then a minimal `scratch` runtime with no Node.js/Go toolchain) and brings up Postgres alongside it. `VANE_MASTER_KEY`/`VANE_SESSION_SECRET` have no default — Compose refuses to start without them, so a real deployment can never accidentally run with a secret that's public on GitHub.

Once both services report healthy, visit **http://localhost:8080** to complete first-time setup — see [Creating the first admin](#creating-the-first-admin-owner) below.

> **Note:** `http://localhost` is a browser-exempted secure context, so the `Secure`-flagged session cookie still works there. Deploying to a host reached by IP or internal hostname over plain HTTP means the browser silently drops the cookie after login — put a TLS-terminating reverse proxy in front before exposing this beyond your own machine.

### Binary

```bash
make build                 # frontend build + go build -o bin/vane
./bin/vane migrate up
./bin/vane serve
```

### Kubernetes (Helm)

```bash
helm repo add zeep-vane https://zeeplabs.github.io/zeep-vane/helm
helm install zeep-vane zeep-vane/zeep-vane \
  --set secrets.databaseUrl="postgres://user:pass@host:5432/vane?sslmode=require" \
  --set secrets.vaneMasterKey="$(openssl rand -hex 32)" \
  --set secrets.vaneSessionSecret="$(openssl rand -hex 32)"
```

> **Repo name collision:** the local alias in `helm repo add <alias> <url>` is just a name you pick — it has nothing to do with the chart itself. If you already run other ZeepLabs open-source Helm charts (e.g. `zeep-orbit`) and added their repo under a shared alias like `zeeplabs`, adding this one under the *same* alias fails with `Error: repository name (…) already exists` — Helm won't silently repoint an existing alias at a different index URL. Use a distinct alias per chart (as above), or add `--force-update` to repoint an existing alias at this chart's index instead (only do this if you're sure nothing else on the machine still needs that alias pointed at its original URL).

The chart deploys two Services: an internal `ClusterIP` for the admin API/SPA (put it behind your own ingress if you want it reachable from outside the cluster), and a `LoadBalancer` exposing Vane's own CertMagic-terminated `:443` listener directly — this is what customer-attached status-page domains should point their DNS at, since CertMagic issues certificates for hostnames not known at deploy time and a conventional ingress/cert-manager setup can't do that. CertMagic's certificate storage lives in Postgres (no PVC, no local disk) so any replica can serve TLS for any registered domain. Full chart source: [`charts/zeep-vane`](charts/zeep-vane).

#### Upgrading an existing install

```bash
helm repo update zeep-vane                        # refresh the local index (new chart/app versions won't show up otherwise)
helm search repo zeep-vane/zeep-vane --versions    # see what's available
helm get values zeep-vane                          # review the values this release is currently running with first
helm upgrade zeep-vane zeep-vane/zeep-vane \
  --reuse-values \
  --set image.tag="v0.2.0"                         # or --version <chart-version> to move to a newer chart release
```

- `--reuse-values` keeps every value you set at install time (secrets, `config.*`, `ingress.*`, etc.) — without it, `helm upgrade` resets everything to the chart's defaults, which on this chart means silently losing your `secrets.databaseUrl`/`vaneMasterKey`/`vaneSessionSecret`. Pass `--set`/`-f` on top of `--reuse-values` only for the specific value(s) you're changing (e.g. bumping `image.tag`, or a new `config.*` field a release just added).
- `image.tag` (`values.yaml`) defaults to `latest`, so `kubectl rollout restart deployment/zeep-vane` alone won't necessarily pull a newer build if a node already cached that tag — pin an explicit tag (or `image.digest`) once you're past initial evaluation, so an upgrade is a deliberate version bump rather than "whatever `latest` resolves to on whichever node the pod lands on."
- Check `CHANGELOG.md`/the GitHub release notes for the target version before upgrading across a minor version — a new required `secrets.*`/`config.*` value (like a new `AD-NNN` in [`.specs/STATE.md`](.specs/STATE.md) sometimes introduces) will fail `helm upgrade` with `execution error` rather than silently starting misconfigured, but it's still better to know beforehand.
- `helm rollback zeep-vane <REVISION>` (see `helm history zeep-vane` for revision numbers) reverts to a previous release's values/chart version if an upgrade goes wrong — it does not undo any database migration the new version's binary already applied on startup, since Vane's migrations are forward-only and embedded in the binary, not managed by the chart.

---

## 🏗️ Architecture overview

Vane is a single Go binary (`cmd/vane`) that embeds the compiled React admin frontend and serves three distinct surfaces:

1. **Admin API + Admin SPA** (`internal/api/*_handler.go`, `web/`) — authenticated, cookie-session based. Company staff manage domains, services, the Datadog integration, incidents, status pages, and other admins here.
2. **Public status page** (`internal/api/public_status_handler.go`, `internal/router/host_router.go`) — unauthenticated. Resolved by the `Host` header of the incoming request: each published `status_pages` row owns a hostname (its own domain or a subdomain), and `HostRouter` dispatches to the public handler scoped to that specific page. No cross-tenant leakage is possible because each request is scoped to exactly one `StatusPage.ID` resolved from the hostname.
3. **A background poller** (`internal/poller`) — polls Datadog's SLO API on an interval (`POLL_INTERVAL_SECONDS`) once a Datadog integration is connected, and writes `status_intervals` rows (an open/closed interval per service, not a point-in-time snapshot) that both the admin dashboard and the public status page read from. The public status page **never** calls Datadog directly on a visitor request — it only ever reads the last interval the poller wrote, with a silent cache-fallback if the poller falls behind (visitors never see a technical error).

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

---

## 🔧 Tech stack

**Backend** — Go 1.26

| Library | Purpose |
| --- | --- |
| [chi](https://github.com/go-chi/chi) | HTTP router |
| [pgx/v5](https://github.com/jackc/pgx) | Postgres driver, no ORM |
| [jwt/v5](https://github.com/golang-jwt/jwt) | Session tokens |
| [zap](https://github.com/uber-go/zap) | Structured logging |
| [cobra](https://github.com/spf13/cobra) | CLI (`serve`, `migrate up`) |
| [CertMagic](https://github.com/caddyserver/certmagic) | Automatic ACME TLS |
| `golang.org/x/crypto/bcrypt` | Admin password hashing |

**Frontend** (`web/`) — React 18 + TypeScript, built with Vite

| Library | Purpose |
| --- | --- |
| React Router | Client-side routing |
| TanStack Query | Server-state / data-fetching layer |
| Tailwind CSS v4 | Styling (design system: **Nocturne**, dark-mode, mono-accent — tokens in `web/src/styles/tokens.css`, components in `web/src/components/ui/`) |
| i18next / react-i18next | Internationalization |
| Radix UI (`react-dialog`) | Accessible dialog primitive |
| Vitest + Testing Library + **MSW** | Component/hook tests — real `fetch` calls intercepted by MSW rather than a hand-rolled mock router |

**Database** — PostgreSQL (plain SQL migrations, no ORM/query builder; see [Database migrations](#-database-migrations))

---

## 📁 Repository layout

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
    styles/            tokens.css — Nocturne design tokens (Tailwind @theme)
    test/               Vitest setup + MSW handlers
.specs/                 spec-driven feature history: STATE.md (decisions +
                        handoff log per feature), LESSONS.md, per-feature specs
```

---

## 🗄️ Domain model

Core tables (see `internal/db/migrations/` for exact schema and `internal/db/*_repository.go` for the Go side):

| Table | Purpose |
| --- | --- |
| `admins` | Company staff accounts. `role` is one of `owner`, `operator`, `viewer` (fixed roles, no configurable permission matrix — see AD-003). |
| `admin_invites` | Pending invitations (email + role) before an invited admin accepts and sets a password. |
| `admin_audit_log` | Append-only log of admin-management actions (invite, role change, removal). |
| `password_reset_tokens` | Single-use tokens for the forgot-password flow. |
| `integrations` | Stored (encrypted) Datadog API key + application key pair, one row per installation. |
| `services` | Logical services the company wants to expose, each mapped to a Datadog SLO. |
| `status_intervals` | Poller-written status per service as open/closed intervals (an interval opens on a status change and stays open, `ends_at IS NULL`, until the next one; at most one open interval per service, enforced by a partial unique index) — what both the admin dashboard and public page actually read, and what the public page's hourly bars/uptime % are computed from. |
| `domains` | Custom hostnames the operator has registered with Vane (validated ownership, DNS target). |
| `status_pages` | A publishable page: a set of services + incidents to expose, an optional `domain_id`/`subdomain` (nullable — a page can exist and be previewed before any domain is attached, see AD-008), and a `state` (`draft`/`published`). |
| `incidents` + updates | Incident timeline entries linked to one or more services, surfaced on the public page for 90 days after resolution. |
| `company_settings` | Singleton row (`CHECK (id = 1)`) — company display name + uploaded logo, shown on the public status page. |

---

## 🔐 Authentication & authorization

- Admin login (`POST /api/auth/login`) issues a **JWT session token stored in an `httpOnly`, `Secure`, `SameSite=Strict` cookie** — never in `localStorage`/`sessionStorage`, never read by frontend JavaScript (AD-004). The JWT carries only `sub`/`iat`; the current admin's role is always fetched fresh from `GET /api/auth/me`, not decoded client-side.
- Three fixed roles, enforced per-route in `internal/cli/routes.go` via `api.RequireRole(...)`:

  | Role | Access |
  | --- | --- |
  | **owner** | Everything, plus admin management (invite/role-change/remove) and company settings |
  | **operator** | All `mvp-core` write routes (domains, services, integrations, incidents, status pages) and reads |
  | **viewer** | Read-only across `mvp-core` resources and poller status |

- `POST /api/auth/logout` clears the cookie; an admin's `sessions_revoked_at` timestamp can invalidate all of that admin's existing sessions at once (used on role change/removal).
- Every path that sets a password — bootstrap, invite-accept, password-reset-confirm — requires 8–72 characters (`internal/auth.ValidatePassword`). No forced complexity rule (uppercase/digit/symbol): NIST SP 800-63B recommends length over complexity, since complexity rules push users toward predictable substitutions instead of real entropy. 72 is bcrypt's own hard input limit.
- Login, password-reset (request + confirm), invite-accept, and bootstrap all share one per-client-IP rate limit (`internal/ratelimit`) — 10 requests/minute with a burst of 10, shared across all of them so spreading guesses across routes doesn't multiply the effective rate. The client IP is read from the connection (`net/http`'s `RemoteAddr`), never from `X-Forwarded-For`/`X-Real-IP` — if this instance sits behind a reverse proxy, make sure that proxy preserves the real client address in the connection it opens to Vane rather than relying on a spoofable header.

---

## 🌐 Public status page routing

A visitor's request is dispatched purely by `Host` header (`internal/router/host_router.go`): if the hostname matches a **published** `status_pages` row, the request is routed to `PublicStatusHandler`, scoped to that exact page's services and incidents — nothing else in the installation is reachable through that hostname. Any other hostname falls through to the base router.

There is also an **authenticated preview endpoint**, `GET /api/status-pages/{id}/public-preview`, used by the admin SPA so a company can see what a status page will look like before a domain is attached and DNS/TLS have propagated. Unlike the public path, the preview does **not** require `state == "published"` and works even with no domain attached at all (AD-008) — it is admin-only and was never meant to mirror production 1:1.

---

## 📋 Configuration

Loaded by `internal/config.Load()` (`internal/config/config.go`). A `.env` file at the repo root is loaded automatically if present (via `godotenv`); its absence is not an error.

| Variable | Required | Default | Notes |
| --- | --- | --- | --- |
| `DATABASE_URL` | Yes | — | Postgres connection string, e.g. `postgres://user:pass@host:5432/dbname?sslmode=disable` |
| `VANE_MASTER_KEY` | Yes | — | Symmetric key used to encrypt stored Datadog credentials at rest. Stretched into the actual AES-256 key via PBKDF2-HMAC-SHA256 (210,000 iterations, `internal/crypto`) rather than a single unsalted hash, so a weak-but-long key still costs real compute per guess |
| `VANE_SESSION_SECRET` | Yes | — | Signing secret for session JWTs |
| `PORT` | Yes | — | Port the admin HTTP API (and SPA) listens on |
| `POLL_INTERVAL_SECONDS` | Yes | — | How often the poller queries Datadog for SLO status |
| `LOG_LEVEL` | No | `info` | zap log level |
| `CORS_ALLOWED_ORIGIN` | No | `http://localhost:5173` | Single allowed CORS origin — defaults to the Vite dev server |
| `PUBLIC_DNS_TARGET` | No | *(empty)* | The DNS target (e.g. an IP or CNAME) this instance's admins should point their custom domain at. Left empty, the "attach domain" screen shows "not configured" instead of blocking — Vane cannot reliably discover its own public hostname |
| `VANE_ADMIN_BASE_URL` | No | *(empty)* | Scheme+host every admin-facing email link (password-reset, admin-invite) is built from, e.g. `https://admin.example.com`. Never derived from the incoming request's `Host` header — that header is attacker-controlled, and the password-reset request endpoint in particular is unauthenticated, so trusting it would let anyone email a real admin a reset link pointing at a host of their choosing. Left unset, those emails link to a visibly broken placeholder host instead of silently trusting `Host` — **set this before connecting an email provider** |
| `VANE_HTTPS_ENABLED` | No | `true` | Set to `false` to skip starting the public HTTPS listener entirely — e.g. no custom status-page domain to serve yet, or the environment can't bind `HTTPS_PORT` (unprivileged container, port already owned by a reverse proxy). With HTTPS enabled and its bind failing, `vane serve` still exits non-zero (unchanged) — this flag is how an operator avoids that failure mode altogether, rather than a way to survive it |
| `HTTPS_PORT` | No | `443` | Port the public, TLS-terminated status page listener binds to |
| `VANE_DEV_TOKEN_LOGGING` | No | `false` | Set to `true` to additionally log the raw password-reset/admin-invite token — useful for local development with no email provider connected yet. The token is a bearer credential for account takeover — **leave this off in any deployment whose logs reach a shared sink** |
| `VANE_SECURE_COOKIES` | No | `true` | Set to `false` if this instance is reached over plain HTTP by anything other than `http://localhost` — browsers only send a `Secure` cookie back over HTTPS (or the `localhost` exception), so with the default `true` the admin API returns `200` on login and a silent `401` on every request after, on any other HTTP-only host. Setting it `false` means the session token then travels unencrypted on whatever network reaches this instance — **only do this on a network you trust**, and prefer terminating TLS in front of Vane instead |

---

## 🛠️ Running in development

You need: Go 1.26+, Node 18+, and a Postgres instance reachable from your machine (a local Docker container is the easiest path — see below).

### 1. Start Postgres

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

---

## 🐳 Docker Compose

The fastest way to run a self-hosted instance is the shipped [`docker-compose.yml`](./docker-compose.yml): a single command builds the image (multi-stage `Dockerfile` — frontend build, Go build, then a minimal `scratch` runtime image with no Node.js or Go toolchain) and brings up Postgres alongside it, migrations applied automatically on boot.

`VANE_MASTER_KEY` and `VANE_SESSION_SECRET` have no default in `docker-compose.yml` — `docker compose up` refuses to start without them, so a real deployment can never accidentally run with a secret that is public on GitHub. Generate two random values and put them in a `.env` file next to `docker-compose.yml` (docker compose loads it automatically):

```bash
echo "VANE_MASTER_KEY=$(openssl rand -hex 32)" >> .env
echo "VANE_SESSION_SECRET=$(openssl rand -hex 32)" >> .env
docker compose up -d
```

Once both services report healthy, visit `http://localhost:8080` to complete first-time setup — see [Creating the first admin (owner)](#creating-the-first-admin-owner) below. `http://localhost` is a browser-exempted secure context so the session cookie works there; the admin login cookie is `Secure`-flagged, so deploying to a host reached by IP or internal hostname over plain HTTP means the browser silently drops it after login — put a TLS-terminating reverse proxy in front before exposing this beyond your own machine.

`make build` (frontend build, then the Go binary) is the one-command build path outside Docker too, if you'd rather run the resulting `bin/vane` directly against your own Postgres.

### Creating the first admin (owner)

A fresh, admin-less instance lands on an in-product **bootstrap screen** (`/bootstrap`) instead of the login screen — create the owner's email and password there directly in the browser, no SQL or throwaway script required. The screen is only reachable while the `admins` table is empty; once an owner exists, `/bootstrap` redirects to `/login` instead.

---

## ✅ Running tests

```bash
# Backend - unit tests, no database needed
go test ./...
go vet ./...
gofmt -l .        # should print nothing

# Frontend
cd web
npm run test        # vitest run
npx tsc -b --noEmit  # type-check
```

Most backend test files carry `//go:build integration` and are skipped by a plain `go test ./...` above — they touch a real Postgres via `internal/dbtest`. To run them, start a test database (`make dev-db` reuses the same Postgres container as local dev — see [Running in development](#%EF%B8%8F-running-in-development)) and set `TEST_DATABASE_URL`:

```bash
make dev-db
TEST_DATABASE_URL="postgres://vane:vane@localhost:5432/vane?sslmode=disable" go test -tags=integration ./...
```

These apply every migration against that database on their own (`db.MigrateUp`) — no separate migration step needed first.

---

## 🗃️ Database migrations

Plain, numbered SQL files in `internal/db/migrations/` (`NNNN_description.up.sql` / `.down.sql`), applied by a minimal runner in `internal/db` — no external migration framework, no ORM.

```bash
go run ./cmd/vane migrate up
```

There is currently only a `migrate up` subcommand (no `down`/rollback command exposed via the CLI, even though `.down.sql` files exist).

---

## 📦 Building for production

```bash
make build
```

`make build` already depends on `web-build` (`cd web && npm install && npm run build`) and runs it first, then `go build -o bin/vane ./cmd/vane` — one command, no separate manual frontend-build step needed. The frontend is embedded into the Go binary via `go:embed` (per `.specs/STATE.md` AD-001), which is exactly why `web-build` has to run first: a `go build`/`go test` invoked directly (bypassing `make build`) against a clean clone with no `web/dist/` populated yet will fail to compile.

---

## 🧭 Known gaps / backlog

Tracked in `.specs/STATE.md`. Not yet solved, not yet requested to be solved:

- Admin invite **resend/cancel** — frontend hooks exist, backend endpoints don't.
- No auto-discovery of Datadog services/SLOs/monitors (would have to be added as a connector feature).
- An intermittent `pg_advisory_lock`/connection-pressure test flake under sustained back-to-back full-suite runs (`internal/dbtest`'s dedicated lock connections don't always get reaped by Postgres fast enough between rapid consecutive invocations) — doesn't reproduce on a normal single CI run against a rested database; recommended fix is still to pin `go test -p 1` (or reduce dbtest's dedicated-connection footprint), not yet applied.
- Manual validation against a **real** Datadog account/API (current test coverage relies on MSW/mocks) hasn't been done.

---

## 🤝 Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). All contributions welcome — bug fixes, features, docs, tests. Please review the [Code of Conduct](CODE_OF_CONDUCT.md) and [Security Policy](SECURITY.md) before opening a PR or reporting a vulnerability. Maintainers cutting a release should follow [RELEASE.md](RELEASE.md).

---

## 📄 License

zeep-vane is licensed under the [Apache License 2.0](LICENSE).
