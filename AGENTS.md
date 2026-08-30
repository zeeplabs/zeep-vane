# AGENTS.md

Rules for any AI agent (Claude Code, Codex, Cursor, etc.) working in this repository. This file is the source of truth — `CLAUDE.md` only points here.

---

## 1. Project shape

- Go backend (`cmd/vane`, `internal/*`) + React/Vite/TS admin SPA (`web/`), embedded into the Go binary at build time via `go:embed` (`web/embed.go`, AD-009/AD-001).
- **Single-tenant by design** (AD-002): one Vane installation serves exactly one company. There is no `company_id`/tenant column anywhere in the schema, and there never should be — if a change looks like it needs one, the actual answer is "run another installation," not "add a tenant column."
- Three fixed admin roles (`owner`/`operator`/`viewer`, AD-003) — no configurable permission matrix. Don't add a granular-permissions system without an explicit decision recorded in `.specs/STATE.md`.
- Larger features are spec'd under `.specs/features/<name>/` (`spec.md`, `design.md`, `tasks.md`) before implementation, and every non-trivial architectural choice gets an `AD-NNN` entry in `.specs/STATE.md`. Read `.specs/STATE.md` before touching auth, the domain model, public routing, or pagination — it's the running log of *why* things are the way they are, and contradicting a recorded decision without addressing it there is a bug in the change, not just in the docs.
- Small fixes/UI tweaks don't need a spec.

## 2. Branching and commits

- All work happens on `main` — there is no `develop`/release-branch flow in this repo (unlike `zeep-orbit`). Don't invent one.
- Commit style follows `CONTRIBUTING.md`: `type: short description` (types: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`). Keep messages about *why*, not just *what*.
- **Never commit unless explicitly asked to.** Staging/committing on your own initiative is not acceptable, even at the end of a task.
- **Never push unless explicitly asked to.** Local commits are fine to accumulate; pushing is a separate, explicit decision.
- Never `--force` push, `--amend` a pushed commit, or rewrite `main` history without explicit instruction.

## 3. Before considering any change done

Run and confirm clean:

- Backend: `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l <changed files>`.
- Backend integration tests (`//go:build integration`, only when DB-touching code changed): `TEST_DATABASE_URL=... go test -tags=integration ./...` — see rule below, never against a real dev database.
- Frontend (`web/`): `npx tsc -b --noEmit`, `npm run test` (vitest).

Don't report a task as complete without having actually run these.

### Integration test database — hard rule

**Never run the integration test gate against `vane-dev-pg`** (or any database holding real/dev data the user cares about). Always spin up a disposable Postgres container for the gate and destroy it afterward:

```bash
docker run -d --rm --name vane-test-pg -p 5433:5432 \
  -e POSTGRES_USER=vane -e POSTGRES_PASSWORD=vane -e POSTGRES_DB=vane \
  postgres:16-alpine -c max_connections=300
TEST_DATABASE_URL="postgres://vane:vane@localhost:5433/vane?sslmode=disable" go test -tags=integration ./...
docker stop vane-test-pg
```

`max_connections=300` matters — the default 100 gets exhausted under `go test ./...`'s package parallelism. This has caused real dev-database pollution once already; treat it as non-negotiable.

## 4. Backend rules

- The public status page (`internal/api/public_status_handler.go`, `internal/router/host_router.go`) **never** calls Datadog live on a visitor request. It only reads what the poller (`internal/poller`) already wrote to `status_intervals`, with a silent cache-fallback if the poller is behind — a visitor must never see a technical error caused by Datadog being slow/down.
- `Page[T]` generic envelope (`internal/api`, AD-012) is the pattern for every paginated list endpoint: `{items, total, page, page_size}`, `parsePage(r)` helper, offset-based. Use the generic wrapper unless the endpoint has non-list fields alongside the list (e.g. `email-providers`' `active_provider`) — in that case the pagination fields (`total`/`page`/`page_size`) sit loose next to the endpoint's own fields instead of forcing the generic wrapper.
- Don't leak raw internal errors (`err.Error()`) into HTTP responses for 500s. Log the real error server-side, return a fixed generic message to the client.
- Session cookies are `httpOnly`, `Secure`, `SameSite=Strict` (AD-004) — never put session state in `localStorage`/`sessionStorage`, and never decode role/permissions from the JWT client-side; always re-fetch from `GET /api/auth/me`.
- The client IP for rate limiting comes from the connection (`RemoteAddr`), never from `X-Forwarded-For`/`X-Real-IP` — those are spoofable unless a trusted proxy strips/sets them, which this codebase doesn't assume.
- The authenticated preview endpoint (`GET /api/status-pages/{id}/public-preview`, AD-008) deliberately does **not** mirror production 1:1 — it works pre-publish and with no domain attached. Don't "fix" it to require `state == "published"`; that was tried and reverted for a documented reason.

## 5. Frontend rules

- Every list screen backed by a paginated endpoint uses the shared `Pager` component (`web/src/components/ui/Pager.tsx`) — `totalPages` is always computed by the caller (`Math.max(1, Math.ceil(total / page_size))`), never internally by `Pager`. Don't build a second pagination UI.
- React Query `queryKey` for paginated data must include the page number (`["resource", page]`) so each page gets its own cache entry — omitting it causes stale/cross-page data bugs.
- User-facing strings go through `react-i18next` — this app ships pt-BR and English. No hardcoded strings in components.
- MSW (`web/src/test/msw/handlers.ts`) mocks must mirror the real backend response shape exactly, including the `Page<T>` envelope for paginated endpoints — a mock returning a bare array while the backend returns `{items,...}` will pass TypeScript (the frontend defines its own types) but crash at runtime. Use the existing `paginatedPage()` helper for new paginated mock endpoints instead of hand-rolling pagination logic.

## 6. Documentation that must stay in sync

- `README.md` — update the relevant feature table when a feature is genuinely new/user-facing, and the [Configuration](README.md#-configuration) table whenever an environment variable is added, renamed, or its default changes. Don't invent env vars or config surfaces that aren't documented there.
- `.specs/STATE.md` — add an `AD-NNN` entry for any new architectural decision (not just "we did X" — the *why*, the *trade-off*, and the *status*). This is what lets a future agent (or the user) understand *why* something is the way it is without re-deriving it from a diff.

## 7. Risk and confirmation

- Treat database migrations, changes to auth/session handling, and anything touching TLS/CertMagic (`internal/tls`) as higher-risk — explain the change and, when in doubt, confirm before applying.
- Never disable/bypass a security check (role enforcement, rate limiting, cookie flags) to "make something work" without flagging it explicitly and getting confirmation first.
- Never run a destructive or data-mutating command (migrations, integration test gate, `docker compose down -v`, etc.) against a database the user didn't explicitly designate as disposable — see the integration test rule in section 3.
