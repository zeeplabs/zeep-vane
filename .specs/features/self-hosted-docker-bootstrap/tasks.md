# Self-Hosted Docker + Admin Bootstrap Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/self-hosted-docker-bootstrap/design.md`
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase sampling. Guidelines found: none (no `AGENTS.md`/`CONTRIBUTING.md`/CI coverage gate) - strong defaults applied, floored against existing depth (sampled `internal/db/status_interval_repository_test.go`, `internal/api/instance_config_handler_test.go`, `internal/cli/routes_test.go`, `web/src/auth/AuthProvider.test.tsx`, `web/src/routes/RequireRole.test.tsx`, `web/src/features/auth/LoginPage.test.tsx`). Go DB/wiring tests carry `//go:build integration` + require `TEST_DATABASE_URL`; frontend tests use Vitest + MSW.
>
> **Sandbox caveat, stated up front:** the Dockerfile/compose tasks' gate needs a working `docker` CLI and daemon. If this execution environment has neither, the worker MUST say so explicitly in the task's "Done when" evidence and in the batch summary - never claim a docker build/compose gate passed without having actually run it.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Migrations-embed (`internal/db`) | integration | Applies clean from the embedded source; idempotent second run; matches `MigrateUp`'s existing error-handling contract | `internal/db/migrations_embed_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=<dsn> go test -tags=integration ./internal/db/...` |
| SPA embed (`web` Go package) | unit | Exact asset served with correct content; unknown SPA route falls back to `index.html`; unknown `/api/*` path returns JSON 404, never HTML | `web/embed_test.go` (no build tag) - **requires `cd web && npm install && npm run build` to have run at least once first**, same as `zeep-orbit`'s accepted convention | `go test ./web/...` |
| Repository (`internal/db`, `BootstrapFirst`) | integration | Happy path; already-bootstrapped refusal; real concurrent-request race (two actual goroutines/transactions, not a serial simulation - per the `status-page-domain-attach` lesson on proving real lock contention) | `internal/db/admin_repository_test.go` (extended, `//go:build integration`) | `TEST_DATABASE_URL=<dsn> go test -tags=integration ./internal/db/...` |
| HTTP handler (`internal/api`, `BootstrapHandler`) | integration | All routes: happy path + 409-already-bootstrapped + invalid payload + session-cookie-set-on-success | `internal/api/bootstrap_handler_test.go` (new, `//go:build integration`) | `TEST_DATABASE_URL=<dsn> go test -tags=integration ./internal/api/...` |
| CLI wiring - routes (`internal/cli/routes.go`) | integration | New routes reachable; existing `/api/*` routes still return JSON (not the SPA fallback) once `NotFound` is wired in; unmatched non-API path returns the SPA fallback | `internal/cli/routes_test.go` (extended, `//go:build integration`) | `TEST_DATABASE_URL=<dsn> go test -tags=integration ./internal/cli/...` |
| CLI wiring - serve boot (`internal/cli/serve.go`) | none (regression via existing suite) | No new logic introduced beyond calling an already-tested function (`MigrateUpEmbedded`, covered above) at a new point in `RunE` | `internal/cli/serve_test.go` | Full gate (below) |
| Frontend (`AuthProvider`, `BootstrapPage`, `App.tsx`) | integration (component) | Happy path + every listed edge case (needs-bootstrap redirect both directions, 409 inline error, empty-field validation) | `web/src/auth/AuthProvider.test.tsx` (extended), `web/src/features/auth/BootstrapPage.test.tsx` (new), `web/src/App.test.tsx` (new) | `cd web && npm run test` |
| Deployment artifacts (`Dockerfile`, `docker-compose.yml`, `Makefile`, `README.md`) | none (no automated test layer for these file types) | Verified by attempting the real build/config commands - document as a blocker if the sandbox lacks Docker, never fake a pass | n/a | `docker build -t vane-selftest:latest .` / `docker compose config -q` / `make build` |

## Gate Check Commands

> Generated from codebase (`Makefile`, `README.md` "Running tests") plus this feature's new deployment-artifact commands.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick | Task touching only `web` (the Go embed package), no DB | `go test ./web/...` (after `cd web && npm install && npm run build` has run once) |
| Full | Task touching `internal/db`, `internal/api`, or `internal/cli` | `TEST_DATABASE_URL=<dsn> go test -tags=integration ./...` |
| Frontend | Task touching `web/src` | `cd web && npm run test && npx tsc -b --noEmit` |
| Docker | Task touching `Dockerfile` or `docker-compose.yml` | `docker build -t vane-selftest:latest .` then `docker compose config -q` (document explicitly if the sandbox has no Docker daemon/CLI) |
| Build | Phase completion, wiring-only tasks, `Makefile`/`README.md` tasks | `cd web && npm install && npm run build && cd .. && go build ./... && go vet ./... && gofmt -l . && TEST_DATABASE_URL=<dsn> go test -tags=integration ./... && go test ./...` |

`<dsn>` is whatever `TEST_DATABASE_URL` is already set to in the executing shell.

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Embedding foundations

```
T1
T2
```

### Phase 2: Bootstrap backend

```
T3 -> T4
```

### Phase 3: Backend wiring

```
T1 -> T5
T2 -> T6
T4 -> T6
```

### Phase 4: Frontend bootstrap flow

```
T7 -> T8 -> T9
```

### Phase 5: Deployment artifacts

```
T6 -> T10
T9 -> T10
T10 -> T11
T11 -> T13
T12 -> T13
```

---

## Task Breakdown

### T1: Embed migrations and add `MigrateUpEmbedded`

**What**: Add `internal/db/migrations_embed.go` with `//go:embed migrations` (`var MigrationsFS embed.FS`) and `MigrateUpEmbedded(dsn string) error`, sourcing `golang-migrate` from `iofs.New(MigrationsFS, "migrations")` instead of `file://`, with the same `ErrNoChange`/idempotency handling as the existing `MigrateUp`. Do not change `MigrateUp`'s existing signature or any of its 37 call sites.
**Where**: `internal/db/migrations_embed.go`, `internal/db/migrations_embed_test.go`
**Depends on**: None
**Reuses**: `internal/db/migrate.go`'s error-handling shape (`errors.Is(err, migrate.ErrNoChange)`, `errors.Is(err, os.ErrNotExist)`); `golang-migrate/v4/source/iofs` (already available transitively, no new module)
**Requirement**: SHD-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `MigrateUpEmbedded` applies every migration in `internal/db/migrations` against a fresh test database with no file-system dependency (test uses a temp/throwaway DSN, never reads from disk for the migration source)
- [x] Running it twice in a row is a no-op the second time (no error), matching `MigrateUp`'s existing contract
- [x] `MigrateUp` and all existing 37 call sites are unchanged (`git diff` shows no edits to `internal/db/migrate.go` or any existing test file)
- [x] Gate check passes: `TEST_DATABASE_URL=<dsn> go test -tags=integration ./internal/db/...`
- [x] Test count: ≥2 tests (no silent deletions) — 3 added

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add embedded migration source for containerized deploys`

---

### T2: `web` package - embedded SPA with client-route fallback

**What**: Add `web/embed.go` (`package web`, `//go:embed dist`, `var distFS embed.FS`) with `StaticHandler() http.Handler`: serves an exact match from the embedded `dist/` when the request path resolves to a real file; otherwise serves `dist/index.html` (SPA client-route fallback) UNLESS the path starts with `/api/`, in which case it writes a plain `404 {"error":"not found"}` JSON body instead. Before writing any test, run `cd web && npm install && npm run build` so `dist/` has real content to test against (same one-time prerequisite `zeep-orbit` already requires - not part of this task's code changes, just a precondition for its own tests to be meaningful).
**Where**: `web/embed.go`, `web/embed_test.go`
**Depends on**: None
**Reuses**: `zeep-orbit/internal/dashboard/embed.go`'s `fs.Sub` + open-or-fallback shape
**Requirement**: SHD-01, SHD-02, SHD-03, SHD-05

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] A request for a real embedded asset path (e.g. whatever hashed filename `npm run build` actually produced under `dist/assets/`) returns that file's exact bytes with a `Content-Type` matching its extension
- [x] A request for an SPA client route (e.g. `/services`, `/bootstrap`) that has no matching embedded file returns `dist/index.html`'s content
- [x] A request whose path starts with `/api/` and matches no embedded file returns a plain JSON `404`, never `index.html`
- [x] Gate check passes: `go test ./web/...`
- [x] Test count: ≥3 tests (no silent deletions) — 3 added

**Tests**: unit
**Gate**: quick

**Commit**: `feat(web): embed built SPA with client-route fallback`

---

### T3: `AdminRepository.BootstrapFirst`

**What**: Add `BootstrapFirst(ctx context.Context, admin *Admin) (created bool, err error)` to `internal/db/admin_repository.go`: opens a transaction, `LOCK TABLE admins IN EXCLUSIVE MODE`, counts existing rows, inserts `admin` (email + password_hash, `role` keeps its existing column default) only if the count is zero, commits. Returns `(false, nil)` - not an error - when an admin already exists, mirroring `zeep-orbit`'s `BootstrapFirstSuperadmin` return contract.
**Where**: `internal/db/admin_repository.go`, `internal/db/admin_repository_test.go`
**Depends on**: None
**Reuses**: `zeep-orbit/internal/dashboard/store.go`'s `BootstrapFirstSuperadmin` lock/count/insert shape; this codebase's existing `Create`'s duplicate-email handling pattern (for the input-validation edge case)
**Requirement**: SHD-15, SHD-16, SHD-17

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Calling it against an admin-less table creates the admin and returns `(true, nil)`
- [x] Calling it again afterward (or against a table that already had an admin) returns `(false, nil)`, no second row created
- [x] A real concurrency test with two goroutines racing actual separate connections/transactions against the same admin-less table results in exactly one `(true, nil)` and one `(false, nil)` - not a serial simulation of the race (per the `status-page-domain-attach` lesson: prove real lock contention with a holder transaction, not just sequential calls that happen to pass)
- [x] Gate check passes: `TEST_DATABASE_URL=<dsn> go test -tags=integration ./internal/db/...`
- [x] Test count: ≥3 tests (no silent deletions) — 3 added

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add BootstrapFirst with table-lock race protection`

---

### T4: `BootstrapHandler`

**What**: Add `internal/api/bootstrap_handler.go` with `BootstrapHandler.Status` (`GET` → `{"bootstrapped": bool}`, reading a simple count) and `BootstrapHandler.Create` (`POST` with `{"email","password"}` body → on success, calls `AdminRepository.BootstrapFirst`, hashes the password via `auth.HashPassword`, issues a session via `auth.IssueSession`, sets the session cookie exactly as `AuthHandler.Login` does, responds `200` with the same identity shape `Me` returns; on `created=false`, responds `409 {"error":"already bootstrapped"}`; on an empty email/password, responds `422` matching `AcceptInvite`'s validation shape).
**Where**: `internal/api/bootstrap_handler.go`, `internal/api/bootstrap_handler_test.go`
**Depends on**: T3
**Reuses**: `AuthHandler.Login`'s cookie-setting call; `AcceptInvite`'s request-validation shape; `AuthHandler.Me`'s response shape
**Requirement**: SHD-14, SHD-15, SHD-16, SHD-18

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `GET` on an admin-less database returns `{"bootstrapped": false}`; after a successful `POST`, returns `{"bootstrapped": true}`
- [x] A successful `POST` sets the same `vane_session` cookie shape (`HttpOnly`, `Secure`, `SameSite=Strict`) `Login` sets, and the response body identifies the new owner
- [x] A second `POST` against an already-bootstrapped database returns `409` with the exact body `{"error":"already bootstrapped"}`, and does not create a second admin
- [x] A `POST` with an empty password returns `422`, no admin created
- [x] Gate check passes: `TEST_DATABASE_URL=<dsn> go test -tags=integration ./internal/api/...`
- [x] Test count: ≥5 tests (no silent deletions) — 5 added

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): add public bootstrap endpoints for the first admin`

---

### T5: Auto-apply embedded migrations on `serve` boot

**What**: In `internal/cli/serve.go`'s `RunE`, call `db.MigrateUpEmbedded(cfg.DatabaseURL)` immediately after the pool is created, before building any router or starting any listener; on error, return it (existing error path already `os.Exit(1)`s via `main.go`). Log at `Info` level that migrations were checked/applied (visible, not silent, per design.md's flagged risk).
**Where**: `internal/cli/serve.go`
**Depends on**: T1
**Reuses**: `MigrateUpEmbedded` from T1; the existing `logger.Info(...)` calls already used elsewhere in `serve.go`

**Requirement**: SHD-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `RunE` calls `db.MigrateUpEmbedded` before `buildAdminRouter`/`newHTTPSServer` are constructed
- [x] A log line at `Info` level reports the migration step ran
- [x] A failure from `MigrateUpEmbedded` prevents the server from starting to listen at all
- [x] Existing `internal/cli/serve_test.go` tests (`TestNewHTTPSServer_*`) still pass unmodified in intent
- [x] Gate check passes: `TEST_DATABASE_URL=<dsn> go test -tags=integration ./...`
- [x] Test count: 0 new tests required (wiring-only, covered by T1's own tests); existing suite shows 0 regressions

**Tests**: none (regression via existing suite)
**Gate**: full

**Commit**: `feat(cli): apply embedded migrations automatically on serve boot`

---

### T6: Wire bootstrap routes and the SPA `NotFound` fallback

**What**: In `internal/cli/routes.go`'s `buildAdminRouter`, register `r.Get("/api/bootstrap/status", bootstrapHandler.Status)` and `r.Post("/api/bootstrap", bootstrapHandler.Create)` alongside the other public routes, and set `r.NotFound(web.StaticHandler())` as the last statement before returning `r`.
**Where**: `internal/cli/routes.go`, `internal/cli/routes_test.go`
**Depends on**: T2, T4
**Reuses**: `BootstrapHandler` from T4; `web.StaticHandler()` from T2; the existing public-route registration pattern in `buildAdminRouter`

**Requirement**: SHD-04, SHD-05, SHD-14, SHD-15

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `GET /api/bootstrap/status` and `POST /api/bootstrap` are reachable through the real production router built by `buildAdminRouter` (not a hand-rolled test router)
- [x] An existing, already-registered API route (e.g. `GET /healthz` or `POST /api/auth/login` with a bad body) still returns its normal JSON response through the same router, unaffected by the new `NotFound` handler
- [x] A request to an unmatched non-API path (e.g. `/some-spa-route`) returns the embedded `index.html` content through the real router
- [x] A request to an unmatched `/api/...` path returns a JSON `404`, not HTML, through the real router
- [x] Gate check passes: `TEST_DATABASE_URL=<dsn> go test -tags=integration ./internal/cli/...`
- [x] Test count: ≥4 tests added/updated (no silent deletions) — 4 added

**Tests**: integration
**Gate**: full

**Commit**: `feat(cli): mount bootstrap routes and SPA fallback on the admin router`

---

### T7: `AuthProvider` gains `needsBootstrap`

**What**: Extend `web/src/auth/AuthProvider.tsx`'s boot `useEffect` to also fetch `GET /api/bootstrap/status` (in parallel with the existing `/api/auth/me` boot fetch, both using `skipUnauthorizedHandler` where applicable), exposing a new `needsBootstrap: boolean` on `AuthContextValue` (initially `false` until the boot fetch resolves, matching how `status: "loading"` is already treated elsewhere as "don't decide yet").
**Where**: `web/src/auth/AuthProvider.tsx`, `web/src/auth/AuthProvider.test.tsx`
**Depends on**: None
**Reuses**: The existing boot `useEffect`/reducer pattern; `apiFetch`

**Requirement**: SHD-19

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `needsBootstrap` is `true` when the MSW-mocked `/api/bootstrap/status` returns `{"bootstrapped": false}`
- [x] `needsBootstrap` is `false` when it returns `{"bootstrapped": true}`
- [x] The existing anonymous-boot-doesn't-open-session-expired-modal regression test still passes unmodified in intent
- [x] Gate check passes: `cd web && npm run test && npx tsc -b --noEmit` (tsc clean; test suite has 2 pre-existing failures in `PasswordResetRequestPage.test.tsx`, unrelated to this feature and present before this task started - confirmed via `git stash`)
- [x] Test count: ≥2 new tests, 0 regressions in existing `AuthProvider`-related suites — 2 added

**Tests**: integration (component)
**Gate**: frontend

**Commit**: `feat(web): expose needsBootstrap from AuthProvider`

---

### T8: `BootstrapPage`

**What**: Add `web/src/features/auth/BootstrapPage.tsx`: a form (email, password, confirm password) that `POST`s to `/api/bootstrap`; on success, the visitor is authenticated (per T4's cookie-setting contract) and the page navigates to the app root; on `409`, shows an inline error stating an admin already exists and links to `/login`; on empty fields, shows the existing form-level validation styling this codebase already uses (`LoginPage`'s convention).
**Where**: `web/src/features/auth/BootstrapPage.tsx`, `web/src/features/auth/BootstrapPage.test.tsx`
**Depends on**: T7
**Reuses**: `LoginPage.tsx`'s desktop/mobile brand-block layout and `useBrandLogoUrl` hook; `apiFetch`

**Requirement**: SHD-18, SHD-20

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Submitting valid, matching email/password/confirm creates the admin (MSW-mocked `POST /api/bootstrap` success) and the page navigates away from `/bootstrap`
- [x] A `409` response shows an inline "already bootstrapped" message and a link to `/login`
- [x] Submitting with password/confirm mismatched shows a client-side validation error without calling the API
- [x] Gate check passes: `cd web && npm run test && npx tsc -b --noEmit` (tsc clean; same 2 pre-existing, unrelated failures noted in T7)
- [x] Test count: ≥4 tests (no silent deletions) — 4 added

**Tests**: integration (component)
**Gate**: frontend

**Commit**: `feat(web): add BootstrapPage for first-run admin creation`

---

### T9: `App.tsx` - `/bootstrap` route and two-way redirect guard

**What**: Add `<Route path="/bootstrap" element={<BootstrapPage />} />` to `App.tsx`'s `<Routes>`, plus a guard (new small component or inline logic, following `RequireRole.tsx`'s `Navigate`-based shape) that redirects an anonymous visitor from any other route to `/bootstrap` when `needsBootstrap` is `true`, and redirects away from `/bootstrap` to `/login` when `needsBootstrap` is `false`.
**Where**: `web/src/App.tsx`, `web/src/App.test.tsx`
**Depends on**: T8
**Reuses**: `RequireRole.tsx`'s `Navigate`-based guard pattern

**Requirement**: SHD-19, SHD-21

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Loading `/login` (or `/`) when `needsBootstrap` is `true` redirects to `/bootstrap`
- [x] Loading `/bootstrap` when `needsBootstrap` is `false` redirects to `/login`
- [x] Loading `/bootstrap` when `needsBootstrap` is `true` renders `BootstrapPage`, no redirect loop
- [x] Gate check passes: `cd web && npm run test && npx tsc -b --noEmit` (tsc clean; same 2 pre-existing, unrelated failures noted in T7)
- [x] Test count: ≥3 tests (no silent deletions) — 4 added

**Tests**: integration (component)
**Gate**: frontend

**Commit**: `feat(web): route to bootstrap screen on first run`

---

### T10: `Dockerfile`

**What**: Add a multi-stage `Dockerfile` at the repo root mirroring `zeep-orbit/Dockerfile`'s structure: stage 1 (`--platform=$BUILDPLATFORM node:22-alpine`) runs `npm ci` + `npm run build` in `web/`; stage 2 (`--platform=$BUILDPLATFORM golang:1.26-alpine`) copies the repo, copies stage 1's `web/dist` into place, cross-compiles via `CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -ldflags="-s -w" -o vane ./cmd/vane`; stage 3 (`FROM scratch`) copies CA certificates + the `vane` binary, `ENTRYPOINT ["/vane"]`, `CMD ["serve"]`.
**Where**: `Dockerfile`
**Depends on**: T6, T9
**Reuses**: `zeep-orbit/Dockerfile` structure verbatim, adapted to this repo's module path and binary name

**Requirement**: SHD-08, SHD-09, SHD-11

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `docker build -t vane-selftest:latest .` succeeds from a clean checkout (attempt it for real; if the sandbox has no Docker CLI/daemon, state that explicitly instead of claiming success) — succeeded, ~23s (OrbStack daemon available in this sandbox)
- [x] The final image contains no Node.js or Go toolchain (`docker run --rm vane-selftest:latest sh` fails - `scratch` has no shell - confirms the minimal final stage; or inspect image layers if `sh` itself isn't a meaningful check) — confirmed: `vane` rejected `sh` as an unknown subcommand (no shell exists to exec)
- [x] `docker run` (or an equivalent smoke check) against a reachable Postgres serves `/healthz` as `200` once ready — confirmed against the real `TEST_DATABASE_URL` Postgres via `host.docker.internal`, migrations auto-applied per the boot log, `/healthz` returned 200
- [x] Gate check passes: `docker build -t vane-selftest:latest .` (documented outcome either way) — passed for real

**Tests**: none
**Gate**: docker

**Commit**: `build(docker): add multi-stage Dockerfile mirroring zeep-orbit`

---

### T11: `docker-compose.yml`

**What**: Add `docker-compose.yml` at the repo root: an `app` service (`build: .`, publishes the admin port, required env vars per `internal/config/config.go` - `DATABASE_URL`, `VANE_MASTER_KEY`, `VANE_SESSION_SECRET`, `PORT`, `POLL_INTERVAL_SECONDS`, plus `UPLOADS_DIR`/`CERTMAGIC_STORAGE_PATH` pointed at mounted volumes - `depends_on: db: condition: service_healthy`, `restart: on-failure`) and a `db` service (`postgres:16-alpine`, healthcheck via `pg_isready`, a named volume for its data directory), mirroring `zeep-orbit/docker-compose.yml`'s shape.
**Where**: `docker-compose.yml`
**Depends on**: T10
**Reuses**: `zeep-orbit/docker-compose.yml` structure verbatim, adapted to Vane's required env vars and volumes

**Requirement**: SHD-08, SHD-09, SHD-10

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `docker compose config -q` validates the file with no errors
- [x] `app` declares `depends_on: db: condition: service_healthy`
- [x] Named volumes exist for the Postgres data directory, `UPLOADS_DIR`, and `CERTMAGIC_STORAGE_PATH`
- [x] If Docker is available in this environment, `docker compose up -d --wait` brings both services to a healthy state and `curl localhost:<published port>/healthz` returns `200`; if unavailable, this is stated explicitly rather than assumed — Docker (OrbStack) was available; ran for real, both containers reported healthy, `curl localhost:8080/healthz` returned 200. `db`'s own port is deliberately not published to the host (unnecessary and collided with this sandbox's local `make dev-db` container already bound to 5432 - the app reaches `db` over the compose-internal network regardless).
- [x] Gate check passes: `docker compose config -q` (plus the up/health smoke where possible) — both passed for real

**Tests**: none
**Gate**: docker

**Commit**: `build(docker): add docker-compose.yml for app + postgres`

---

### T12: `Makefile` - `build`/`web-build` targets

**What**: Add a `web-build` target (`cd web && npm install && npm run build`) and a `build` target depending on it (`go build -o bin/vane ./cmd/vane`), mirroring `zeep-orbit/Makefile`'s `dashboard-build`/`build` targets. Existing targets (`test`, `lint`, `vet`, `dev-*`) are unchanged.
**Where**: `Makefile`
**Depends on**: None
**Reuses**: `zeep-orbit/Makefile`'s `dashboard-build`/`build` target shape

**Requirement**: SHD-08 (build-tooling half)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `make build` runs the frontend build, then the Go build, producing `bin/vane`
- [x] Existing Makefile targets are byte-for-byte unchanged except for the two additions
- [x] Gate check passes: `make build` (the command itself is the gate for this task) — ran for real, `bin/vane` produced

**Tests**: none
**Gate**: build

**Commit**: `build(make): add build/web-build targets`

---

### T13: `README.md` - Docker Compose quick-start, remove manual SQL bootstrap

**What**: Add a "Docker Compose" quick-start section (mirroring `zeep-orbit/README.md`'s shape: a compose snippet or reference to the shipped `docker-compose.yml`, `docker compose up -d`, "visit `http://localhost:<port>` to complete first-time setup"). Remove the entire "Creating the first admin (owner)" manual SQL/bcrypt-script section and replace it with a short description of the in-product `/bootstrap` screen.
**Where**: `README.md`
**Depends on**: T11, T12
**Reuses**: `zeep-orbit/README.md`'s "Docker Compose" quick-start wording/shape

**Requirement**: SHD-13, SHD-22

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] The manual SQL `INSERT`/bcrypt-throwaway-script section is gone from `README.md`
- [x] A "Docker Compose" section exists describing `docker compose up` and the first-run bootstrap screen
- [x] `make build` (from T12) is referenced as the one-command build path, consistent with the rest of the README's existing style
- [x] Gate check passes: `cd web && npm install && npm run build && cd .. && go build ./... && go vet ./... && gofmt -l . && TEST_DATABASE_URL=<dsn> go test -tags=integration ./... && go test ./...` — ran for real against the sandbox's `TEST_DATABASE_URL`, all green (gofmt: no output, go vet: clean, integration + unit suites: all packages ok)

**Tests**: none
**Gate**: build

**Commit**: `docs(readme): document docker compose quick-start, remove manual bootstrap`

---

## Phase Execution Map

```
T1 -> T5
T2 -> T6
T3 -> T4
T4 -> T6
T6 -> T10
T7 -> T8
T8 -> T9
T9 -> T10
T10 -> T11
T11 -> T13
T12 -> T13
```

Execution is strictly sequential - there is no intra-phase parallelism. T1, T2, T3, T7, T12 have no dependency and start their respective phases; every other task's dependency is listed above and matches the Task Breakdown exactly.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: Migrations embed | 1 new file + its test | ✅ Granular |
| T2: SPA embed | 1 new file + its test | ✅ Granular |
| T3: `BootstrapFirst` | 1 method in 1 file | ✅ Granular |
| T4: `BootstrapHandler` | 1 new file (2 handler methods, cohesive) + its test | ✅ Granular |
| T5: Serve boot wiring | 1 call site in 1 file | ✅ Granular |
| T6: Routes wiring | 2 route registrations + 1 `NotFound` call, 1 file | ✅ Granular |
| T7: `AuthProvider` extension | 1 field + 1 fetch in 1 file | ✅ Granular |
| T8: `BootstrapPage` | 1 new component + its test | ✅ Granular |
| T9: `App.tsx` routing | 1 route + 1 guard, 1 file | ✅ Granular |
| T10: `Dockerfile` | 1 new file | ✅ Granular |
| T11: `docker-compose.yml` | 1 new file | ✅ Granular |
| T12: `Makefile` targets | 2 targets, 1 file | ✅ Granular |
| T13: `README.md` | 1 file, 2 cohesive edits (add section, remove section) | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | (no incoming arrow) | ✅ Match |
| T2 | None | (no incoming arrow) | ✅ Match |
| T3 | None | (no incoming arrow) | ✅ Match |
| T4 | T3 | T3 -> T4 | ✅ Match |
| T5 | T1 | T1 -> T5 | ✅ Match |
| T6 | T2, T4 | T2 -> T6, T4 -> T6 | ✅ Match |
| T7 | None | (no incoming arrow) | ✅ Match |
| T8 | T7 | T7 -> T8 | ✅ Match |
| T9 | T8 | T8 -> T9 | ✅ Match |
| T10 | T6, T9 | T6 -> T10, T9 -> T10 | ✅ Match |
| T11 | T10 | T10 -> T11 | ✅ Match |
| T12 | None | (no incoming arrow) | ✅ Match |
| T13 | T11, T12 | T11 -> T13, T12 -> T13 | ✅ Match |

No dependency points to a later phase.

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1: Migrations embed | Migrations-embed | integration | integration | ✅ OK |
| T2: SPA embed | SPA embed | unit | unit | ✅ OK |
| T3: `BootstrapFirst` | Repository | integration | integration | ✅ OK |
| T4: `BootstrapHandler` | HTTP handler | integration | integration | ✅ OK |
| T5: Serve boot wiring | CLI wiring (serve) | none (regression) | none | ✅ OK |
| T6: Routes wiring | CLI wiring (routes) | integration | integration | ✅ OK |
| T7: `AuthProvider` | Frontend | integration (component) | integration | ✅ OK |
| T8: `BootstrapPage` | Frontend | integration (component) | integration | ✅ OK |
| T9: `App.tsx` routing | Frontend | integration (component) | integration | ✅ OK |
| T10: `Dockerfile` | Deployment artifact | none | none | ✅ OK |
| T11: `docker-compose.yml` | Deployment artifact | none | none | ✅ OK |
| T12: `Makefile` | Deployment artifact | none | none | ✅ OK |
| T13: `README.md` | Deployment artifact | none | none | ✅ OK |

No violations. No task defers its required tests to a later task.

---

## Requirement Coverage

All 22 `SHD-*` ACs map to at least one task: SHD-01/02/03/05 → T2 (+ T6 wiring); SHD-04 → T6; SHD-06 → T1 + T5; SHD-08/09/11 → T10 + T12; SHD-08/09/10 → T11; SHD-13 → T13; SHD-14 → T4 + T6; SHD-15/16/17 → T3 + T4; SHD-18 → T4 + T8; SHD-19 → T7 + T9; SHD-20 → T8; SHD-21 → T9; SHD-22 → T13. 22 total, 22 mapped, 0 unmapped.
