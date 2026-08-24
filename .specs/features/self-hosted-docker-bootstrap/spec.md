# Self-Hosted Docker + Admin Bootstrap Specification

## Problem Statement

Vane's "single binary, low footprint" positioning (AD-001) is not real today: `go:embed` for the frontend was decided but never implemented (zero `go:embed` directives exist in the codebase), so the admin API and the SPA only work as two separately-run processes, and there is no `Dockerfile` or `docker-compose.yml` at all. Separately, creating the first admin requires hand-writing a throwaway Go script to bcrypt-hash a password and running a raw `INSERT` against Postgres (documented in `README.md`) - undiscoverable, unscriptable, and the single biggest friction point for anyone trying this self-hosted product for the first time. Both gaps block the same goal: `docker compose up`, open a browser, have a working instance.

## Goals

- [ ] The Go binary serves the built SPA (static assets + client-route fallback) from the same `:8080` listener as the admin API, with no file dependency on disk at runtime
- [ ] Database migrations apply from an embedded source inside the binary - the container never depends on a `migrations/` directory being present on disk
- [ ] `docker compose up` brings up Postgres + the Vane app in one command, migrations applied automatically, with persistent volumes for uploads and CertMagic storage
- [ ] A fresh instance with zero admins redirects the SPA to a bootstrap screen that creates the first owner - no CLI, no SQL, no throwaway script
- [ ] `README.md`'s manual SQL/bcrypt-script bootstrap instructions are removed, replaced by the in-product flow
- [ ] The Dockerfile, docker-compose.yml, Makefile `build` target, and README quick-start mirror `zeep-orbit`'s already-working equivalents in structure and command shape

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
| --- | --- |
| Real public-domain HTTPS testing inside docker-compose | CertMagic's on-demand TLS only works against a real, publicly resolvable hostname (existing behavior, `README.md`); compose exposes the admin/SPA listener over plain HTTP on `:8080` for local/LAN use, unchanged from today's dev setup |
| Multi-replica / orchestration (Kubernetes, Helm) | Separate future scope; this feature targets a single `docker compose up` on one host, matching the product's low-footprint positioning |
| A CLI bootstrap command/flags | User decided the bootstrap flow lives in the product UI (browser-driven), not a CLI command - a CLI path was considered and explicitly not chosen |
| Configurable migration source path at runtime | Migrations are embedded; there is no operator-facing knob for where they come from |
| Automated frontend build in CI/release pipeline | Out of scope for this feature - covered here only to the extent the Dockerfile's own build stage needs it |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Scope of the SPA-embedding gap | Fixed as part of this feature, not deferred | User confirmed after the gap was surfaced: a Dockerfile wrapping today's two-process setup would not deliver a working single-container deploy, so closing the actual `go:embed` gap is required, not optional | y |
| First-admin creation UX | In-product SPA screen (boot check → redirect to `/bootstrap` when zero admins exist), not a CLI command | User explicitly proposed this over CLI flags/env vars/interactive prompt, reasoning it fits the "open the browser" self-hosted flow better | y |
| Bootstrap endpoint behavior when an admin already exists | Refuse with a clear error and non-2xx status; never a silent no-op | User chose "refuse" over "idempotent no-op" - protects against a second, unwanted owner being created by mistake; an operator who wants more admins uses the existing invite flow, not bootstrap | y |
| Bootstrap race protection | A transaction that takes `LOCK TABLE admins IN EXCLUSIVE MODE` before counting and inserting - not `SELECT ... FOR UPDATE` (which has no rows to lock on an empty table) | User confirmed the general approach; the exact technique is corrected to match the sibling project `zeep-orbit`'s already-working `BootstrapFirstSuperadmin` (`internal/dashboard/store.go`), which uses a table-level lock for precisely this empty-table race - a plain email-uniqueness constraint does not stop two different emails from both becoming owners in a race | y |
| Reference implementation to mirror | `zeep-orbit`'s already-working Docker/Compose/bootstrap setup (`baas/zeep-orbit`), not an independently-designed pattern | User explicitly asked: "preciso que o comando de subida seja igual o que existe hoje no orbit que ja funciona" - Design pins the exact structure from that project's `Dockerfile`, `docker-compose.yml`, `Makefile`, `README.md` Quick Start section, and `internal/dashboard/handler.go`/`store.go`'s bootstrap endpoints (`GET .../bootstrap/status` → `{bootstrapped: bool}`, `POST .../bootstrap` → 409 `{"error":"already bootstrapped"}`) | y |
| README's manual SQL bootstrap section | Removed entirely, not kept as a documented fallback | User chose full replacement | y |
| Migrations at runtime | Embedded in the binary via a new embed-based path, existing `MigrateUp(dsn, dir string)` (disk-path based) kept unchanged for the 37 existing call sites (tests, `vane migrate up`) | Changing `MigrateUp`'s signature would touch 37 test files for no requirement-driven reason; the container needs a self-contained path, existing dev/test tooling does not | y (inferred from code, no disagreement expected) |
| Migration application in the container | The Docker image runs migrations automatically before serving (no separate manual step for the compose path) | `vane serve` today does not auto-migrate (confirmed by reading `internal/cli/serve.go`); a docker-compose user has no separate terminal to run `vane migrate up` first, so the image's entrypoint must do both | n - inferred, flagged for the user to override if a separate manual migrate step is preferred |
| `go build`/`go test` and a truly-fresh, never-built checkout | Accept the same friction `zeep-orbit` already ships with: the embedded directory (`web/dist`) is gitignored entirely, so a checkout that has never run the frontend build once will fail to compile until `make build` (or equivalent) runs the frontend build first. Not solved with a committed placeholder. | Confirmed by reading `zeep-orbit`'s `.gitignore` (`internal/dashboard/static/` fully ignored) and its CI workflows (`npm run build` always runs before the Go build/test step) - this is the project's actual, already-working convention, not a gap to fix here. Inventing a stronger guarantee than the reference implementation provides would be scope creep beyond "mirror what already works." | y (revised after inspecting the reference implementation directly) |
| Session behavior on successful bootstrap | Sets the same `httpOnly` session cookie login already sets (AD-004) - the new owner lands authenticated immediately, no separate login step after bootstrapping | Matches the existing login flow's UX; bootstrapping and then immediately being logged out to log back in again would be a worse first-run experience for no security benefit (the request already proved knowledge of the just-chosen password) | n - inferred, no disagreement expected |
| Compose volumes | Named volumes for `UPLOADS_DIR` and `CERTMAGIC_STORAGE_PATH`, both already required to be persistent per existing config (`SET-11`, `internal/tls`) | Without this, an uploaded logo or an issued TLS certificate is lost on container recreation - already a documented requirement elsewhere in the codebase, just never wired into a compose file until now | y (inferred from existing documented requirements, no disagreement expected) |

**Open questions:** none - all resolved or logged above.

---

## User Stories

### P1: Go binary serves the SPA with no on-disk dependency ⭐ MVP

**User Story**: As the operator of a Vane instance, I want the API and the admin UI to come from one running process, so that a single container is a complete deployment.

**Why P1**: Without this, a Dockerfile only packages half the product - the other half (the SPA) still needs a separate process/container, defeating the point of a single-binary self-hosted tool.

**Acceptance Criteria**:

1. The system SHALL embed the built SPA's static assets (`web/dist`) into the Go binary at build time, with no runtime dependency on a directory being present on disk.
2. WHEN a request's path does not match `/api/*`, `/healthz`, or an existing static asset in the embedded SPA THEN the system SHALL serve the embedded `index.html` (client-side route fallback), so a direct browser navigation or refresh on any SPA route (e.g. `/services`, `/bootstrap`) works.
3. WHEN a request's path matches an existing embedded static asset (e.g. `/assets/index-abc123.js`) THEN the system SHALL serve that exact file with its correct `Content-Type`, never the `index.html` fallback.
4. The system SHALL serve the embedded SPA from the same `:8080` listener the admin API already uses - not a separate port or process.
5. IF a request's path starts with `/api/` and matches no registered route THEN the system SHALL return the existing API 404 behavior, never the SPA's `index.html` fallback.
6. The system SHALL apply database migrations from an embedded source at container startup, with no dependency on a `migrations/` directory existing on disk in the runtime image.
7. WHEN the frontend has been built at least once (via `make build` or the Dockerfile's build stage) THEN `go build ./...` and `go test ./...` SHALL both succeed - matching `zeep-orbit`'s existing convention (embedded directory gitignored, Makefile's `build` target runs the frontend build first; a truly-never-built checkout requires that one-time step, same as the reference project).

**Independent Test**: Build the Docker image from a clean checkout, run the container against a fresh Postgres with no prior migrations applied, `curl` `/services` (a client-side route) and confirm `index.html` is returned; `curl` a real asset path and confirm the correct file/content-type; confirm the database now has all migrations applied without a separate `vane migrate up` invocation.

---

### P1: `docker compose up` runs the full stack in one command

**User Story**: As someone evaluating Vane for the first time, I want to run one command and get a working instance, so that trying the product doesn't require reading Go/Postgres setup docs first.

**Why P1**: This is the concrete deliverable the whole feature exists for - everything else (SPA embedding, bootstrap) is a precondition for this command to produce something usable.

**Acceptance Criteria**:

1. The system SHALL provide a `docker-compose.yml` defining a `postgres` service and an `app` service (built from a new `Dockerfile`), with `app` depending on `postgres` being healthy before starting.
2. The system SHALL provide named, persistent volumes for the Postgres data directory, `UPLOADS_DIR`, and `CERTMAGIC_STORAGE_PATH`, so an uploaded logo or an issued TLS certificate survives `docker compose down && docker compose up`.
3. WHEN `app`'s container starts THEN the system SHALL apply any pending migrations before beginning to serve requests.
4. The system SHALL expose `GET /healthz` on the `app` service's published port, returning `200` once the process is ready to serve, usable as the compose healthcheck.
5. The `Dockerfile` SHALL build the frontend and the Go binary in separate build stages, producing a final runtime image that contains only the compiled binary and its runtime dependencies - no Node.js toolchain, no Go toolchain (mirroring `zeep-orbit`'s `Dockerfile`: `--platform=$BUILDPLATFORM` build stages, `FROM scratch` final stage, `ENTRYPOINT`/`CMD` split).
6. `Makefile` SHALL gain a `build` target that builds the frontend and then the Go binary in that order (mirroring `zeep-orbit`'s `build`/`dashboard-build` targets), so `make build` is the one command that always produces a working, SPA-embedding binary.
7. `README.md`'s run instructions SHALL gain a "Docker Compose" quick-start section in the same shape as `zeep-orbit`'s (a runnable `docker-compose.yml` snippet or reference to the shipped file, followed by `docker compose up -d` and "visit `http://localhost:<port>` to complete first-time setup").

**Independent Test**: From a clean checkout with no other setup beyond `make build` once, run `docker compose up`, wait for both services to report healthy, and load the app's published port in a browser - the bootstrap screen (next story) appears with zero prior manual steps.

---

### P1: First-run bootstrap creates the first owner from the browser

**User Story**: As someone who just started a fresh Vane instance, I want to create my own admin account from the browser, so that I never touch SQL or a throwaway script.

**Why P1**: This is the other half of "one command, working instance" - without it, `docker compose up` still ends at a login screen nobody can pass.

**Acceptance Criteria**:

1. WHEN the SPA boots and no admin exists in the database THEN the system SHALL redirect an anonymous visitor to a bootstrap screen instead of the login screen.
2. The system SHALL expose a public, unauthenticated `GET` endpoint reporting whether any admin exists, for the SPA's boot-time redirect decision.
3. The system SHALL expose a public, unauthenticated `POST` endpoint that creates the first admin (email + password), assigned the `owner` role by the database's existing default.
4. WHILE at least one admin already exists in the database the system SHALL reject the bootstrap `POST` endpoint with a clear error and a non-2xx status, never creating an additional admin through this path.
5. IF two bootstrap `POST` requests race when the database has zero admins THEN the system SHALL let exactly one succeed (transaction-locked check-then-insert) and the other SHALL receive the same "already bootstrapped" rejection as AC4, never two owners created.
6. WHEN the bootstrap `POST` endpoint succeeds THEN the system SHALL set the same `httpOnly` session cookie the login endpoint sets (AD-004), so the new owner is authenticated immediately without a separate login step.
7. WHEN a visitor navigates directly to the bootstrap screen's route while an admin already exists THEN the system SHALL redirect to the login screen instead of showing the bootstrap form.
8. `README.md`'s manual SQL/bcrypt-script bootstrap section SHALL be removed and replaced with instructions describing the in-product bootstrap screen.

**Independent Test**: Against a freshly migrated, admin-less database, load the app in a browser - confirm it lands on the bootstrap screen, not login. Submit the form, confirm the resulting session is authenticated as `owner`. Reload the app - confirm it now lands on the login screen, and a direct `POST` to the bootstrap endpoint is rejected.

---

## Edge Cases

- IF the bootstrap `POST` payload's email is already registered as an admin (impossible in practice since AC4 already blocks any admin existing, but guards the endpoint's own input validation) THEN the system SHALL return the existing duplicate-email error shape, not a generic 500.
- IF the Postgres container is not yet ready when `app` starts THEN the system SHALL rely on the compose `depends_on: condition: service_healthy` gate to delay `app`'s start, rather than the application retrying its own connection in a loop.
- WHEN a request path is `/api/some/real/route` that legitimately 404s (e.g. an unknown incident ID) THEN the system SHALL return that route's normal JSON 404, never the SPA fallback (distinguishes a real API 404 from "path doesn't match any route at all").
- IF `web/dist` has never been built on a given checkout THEN `go build`/`go test` fail to compile until the frontend is built once (per the revised assumption above, matching `zeep-orbit`) - not a bug in this feature, an accepted, already-precedented convention. The Docker image's build stage always runs the frontend build first, so this never surfaces in the container.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| SHD-01 | P1: Binary serves SPA | Execute | Verified |
| SHD-02 | P1: Binary serves SPA | Execute | Verified |
| SHD-03 | P1: Binary serves SPA | Execute | Verified |
| SHD-04 | P1: Binary serves SPA | Execute | Verified |
| SHD-05 | P1: Binary serves SPA | Execute | Verified |
| SHD-06 | P1: Binary serves SPA | Execute | Verified |
| SHD-07 | P1: Binary serves SPA | Execute | Verified |
| SHD-08 | P1: docker compose up | Execute | Verified |
| SHD-09 | P1: docker compose up | Execute | Verified |
| SHD-10 | P1: docker compose up | Execute | Verified |
| SHD-11 | P1: docker compose up | Execute | Verified |
| SHD-12 | P1: docker compose up | Execute | Verified |
| SHD-13 | P1: docker compose up | Execute | Verified |
| SHD-14 | P1: docker compose up | Execute | Verified |
| SHD-15 | P1: First-run bootstrap | Execute | Verified |
| SHD-16 | P1: First-run bootstrap | Execute | Verified |
| SHD-17 | P1: First-run bootstrap | Execute | Verified |
| SHD-18 | P1: First-run bootstrap | Execute | Verified |
| SHD-19 | P1: First-run bootstrap | Execute | Verified |
| SHD-20 | P1: First-run bootstrap | Execute | Verified |
| SHD-21 | P1: First-run bootstrap | Execute | Verified |
| SHD-22 | P1: First-run bootstrap | Execute | Verified |

**ID format:** `SHD-[NUMBER]` (Self-Hosted Docker + bootstrap)

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 22 total, 22 mapped to tasks, 0 unmapped ✅ (Execute complete, Verifier PASS)

---

## Success Criteria

- [ ] `docker compose up` on a clean checkout, with no other manual step, ends with a browser-usable bootstrap screen
- [ ] Creating the first admin through that screen requires zero SQL, zero throwaway scripts, zero CLI commands
- [ ] `go build ./...` and `go test ./...` both pass on a fresh checkout before `npm run build` has ever run
- [ ] An uploaded logo and an issued TLS certificate both survive `docker compose down && docker compose up`
- [ ] `README.md` no longer instructs a manual SQL insert for the first admin
