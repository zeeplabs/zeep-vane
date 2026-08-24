# Self-Hosted Docker + Admin Bootstrap Validation

**Date**: 2026-08-24
**Spec**: `.specs/features/self-hosted-docker-bootstrap/spec.md`
**Diff range**: `630f574..76f517a` (13 commits: 97179cc, 3ce4d7a, 3770637, ce7bde5, 688c56d, 4d2782e, 89df360, 87f4333, 176747b, aa47baf, 05f2bd7, 3d3d6ab, 76f517a) + 3 fix rounds: `df99d5d`, `b4e2930`, `526139e`, `7e401dc`
**Verifier**: independent sub-agent (author ≠ verifier)
**Verdict**: ✅ **PASS** (iteration 3/3 - see "Gate Check — Iteration 3" below; iterations 1-2 were not green, see their sections for history)
**Result**: PASS

---

## Task Completion

| Task | Status  | Notes |
| ---- | ------- | ----- |
| T1   | ✅ Done | `internal/db/migrations_embed.go` |
| T2   | ✅ Done | `web/embed.go` |
| T3   | ✅ Done | `internal/db/admin_repository.go:202` |
| T4   | ✅ Done | `internal/api/bootstrap_handler.go` |
| T5   | ✅ Done | `internal/cli/serve.go:86` |
| T6   | ✅ Done | `internal/cli/routes.go:67,68,134` |
| T7   | ✅ Done | `web/src/auth/AuthProvider.tsx` |
| T8   | ✅ Done | `web/src/features/auth/BootstrapPage.tsx` |
| T9   | ✅ Done | `web/src/App.tsx` |
| T10  | ✅ Done | `Dockerfile` - rebuilt and independently re-verified |
| T11  | ✅ Done | `docker-compose.yml` - `docker compose config -q` re-verified |
| T12  | ✅ Done | `Makefile` - `make build` re-run, `bin/vane` produced |
| T13  | ✅ Done | `README.md` - manual SQL section confirmed removed |

---

## Spec-Anchored Acceptance Criteria

| ID | Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- | --- |
| SHD-01 | Embed built SPA, no on-disk runtime dependency | Static assets compiled into binary via `go:embed` | `web/embed.go:24` (`//go:embed dist`) + `web/embed_test.go:34` `TestStaticHandler_RealAsset_ServesExactFileWithContentType` | ✅ PASS |
| SHD-02 | Unmatched non-API/non-asset path → `index.html` fallback | Client route (e.g. `/services`, `/bootstrap`) returns embedded `index.html` | `web/embed.go:67` `serveIndex` + `web/embed_test.go:62` `TestStaticHandler_SPARoute_FallsBackToIndexHTML`; also through the **real production router**: `internal/cli/routes_test.go:800` `TestAdminRouter_UnmatchedNonAPIPath_ReturnsEmbeddedIndexHTML` | ✅ PASS |
| SHD-03 | Real asset path → exact file + correct `Content-Type` | Exact bytes, `Content-Type` matching extension | `web/embed.go:56-58` (`fs.Stat` + `fileServer.ServeHTTP`) + `web/embed_test.go:34` asserts `Content-Type` | ✅ PASS |
| SHD-04 | SPA + API served from the same `:8080` listener | Bootstrap routes and SPA fallback mounted on the same router as the existing admin API, no second port/process | `internal/cli/routes.go:67-68` (bootstrap routes registered on `buildAdminRouter`) + `internal/cli/routes_test.go:728` `TestAdminRouter_BootstrapRoutes_ReachableThroughRealRouter` (reaches them through the real production router) | ✅ PASS |
| SHD-05 | Unmatched `/api/...` → API 404, never SPA fallback | Plain JSON `404`, not HTML | `web/embed.go:62-65` (`writeNotFoundJSON`) + `internal/cli/routes_test.go:821` `TestAdminRouter_UnmatchedAPIPath_ReturnsJSON404NotHTML` (through the real router, asserts `rec.Code == 404` and valid JSON body) | ✅ PASS |
| SHD-06 | Migrations apply from an embedded source, no disk dependency | All migrations applied against a fresh DB with no `migrations/` on disk | `internal/db/migrations_embed.go:22-58` (`MigrationsFS`, `MigrateUpEmbedded`) + `internal/db/migrations_embed_test.go:57` `TestMigrateUpEmbedded_FreshDatabase_AppliesAllMigrations`; wired at boot: `internal/cli/serve.go:86` | ✅ PASS |
| SHD-07 | After one frontend build, `go build ./...` and `go test ./...` succeed | Both commands exit 0 | Empirically re-run by the Verifier: `go build ./...` exit 0; `go test ./...` exit 0 (all packages `ok`). This is a compile-time/process property, not unit-testable in a `file:line` sense - verified by executing the actual commands. **Not mapped to any task in `tasks.md`'s Requirement Coverage table** despite that table's own "22 total, 22 mapped, 0 unmapped" claim - traceability gap, not a functional gap (see Gaps below). | ✅ PASS (empirical; traceability gap noted) |
| SHD-08 | `docker-compose.yml`: `postgres` + `app`, `app depends_on postgres healthy` | `depends_on: condition: service_healthy` present | `docker-compose.yml:18-20` | ✅ PASS (`docker compose config -q` re-run by Verifier, exit 0) |
| SHD-09 | Named persistent volumes: Postgres data, `UPLOADS_DIR`, `CERTMAGIC_STORAGE_PATH` | Three named volumes, mounted | `docker-compose.yml:15-17` (uploads, certmagic) + `docker-compose.yml:34-40` (pgdata + volume declarations) | ✅ PASS |
| SHD-10 | `app` applies pending migrations before serving | Migration step runs before the listener starts | `internal/cli/serve.go:86` (`MigrateUpEmbedded` before router/listener construction) + `Dockerfile:38-39` (`ENTRYPOINT ["/vane"]`, `CMD ["serve"]` - compose's `app` runs this same boot path) | ✅ PASS |
| SHD-11 | `GET /healthz` returns `200` once ready, usable as compose healthcheck | `200` from the app's published port | `internal/router/router.go:31` (`r.Get("/healthz", healthzHandler(pool))`) + re-verified independently by the Verifier via `docker build` + container run against the sandbox's Postgres, `/healthz` → `200` | ✅ PASS |
| SHD-12 | Multi-stage `Dockerfile`, final image has no Node/Go toolchain | `FROM scratch` final stage, only CA certs + binary | `Dockerfile:8` (web-builder stage), `Dockerfile:16` (builder stage), `Dockerfile:31-39` (`FROM scratch`, `COPY --from=builder` binary+certs only). Independently re-verified by the Verifier: rebuilt the image (`docker build -t vane-verify2:latest .`), `docker run --rm vane-verify2:latest sh` → `Error: unknown command "sh"` (no shell exists to exec), confirming a toolchain-free final image. **Not mapped to any task in `tasks.md`'s Requirement Coverage table** - same traceability gap as SHD-07. | ✅ PASS (traceability gap noted) |
| SHD-13 | `make build` produces `bin/vane` via frontend build then Go build | `bin/vane` exists after `make build` | `Makefile:7-11` (`web-build`, `build` targets); re-run by the Verifier: `make build` → `bin/vane` produced (20.8MB binary) | ✅ PASS |
| SHD-14 | README gains a "Docker Compose" quick-start section | Section describing `docker compose up -d` + first-run setup | `README.md:220-230` ("## Docker Compose" section, `docker compose up -d`, references the bootstrap screen) | ✅ PASS |
| SHD-15 | `POST /api/bootstrap` refused (non-2xx) while an admin exists | `409` status, never a silent no-op | `internal/db/admin_repository.go:217-221` (`count > 0` → `(false, nil)`) + `internal/api/bootstrap_handler.go:97-100` (`409` + `alreadyBootstrappedBody`) + `internal/api/bootstrap_handler_test.go:253` `TestBootstrapHandler_Create_AlreadyBootstrapped_Returns409NoSecondAdmin` | ✅ PASS |
| SHD-16 | `GET /api/bootstrap/status` reports whether any admin exists | `{"bootstrapped": bool}` | `internal/api/bootstrap_handler.go:40-58` (`Status`) + `internal/api/bootstrap_handler_test.go:160,179` (`TestBootstrapHandler_Status_NoAdmins_ReturnsFalse`, `TestBootstrapHandler_Status_AfterSuccessfulCreate_ReturnsTrue`) | ✅ PASS |
| SHD-17 | Two racing `POST /api/bootstrap` requests on an empty table → exactly one succeeds | One `created=true`, one refused, never two owners | `internal/db/admin_repository.go:202-236` (`LOCK TABLE admins IN EXCLUSIVE MODE` inside a transaction) + `internal/db/admin_repository_test.go:489` `TestAdminRepository_BootstrapFirst_ConcurrentCalls_RealLockContention` (real holder transaction proving actual lock contention, not a serial simulation) | ✅ PASS |
| SHD-18 | Successful bootstrap sets the same session cookie `Login` sets | `httpOnly` cookie, immediate authentication | `internal/api/bootstrap_handler.go:108` (`http.SetCookie(w, sessionCookie(...))`) + `internal/api/bootstrap_handler_test.go:202` `TestBootstrapHandler_Create_Success_SetsSessionCookieAndReturnsIdentity` | ✅ PASS |
| SHD-19 | SPA redirects anonymous visitor to `/bootstrap` when no admin exists | Redirect to `/bootstrap`, not `/login` | `web/src/auth/AuthProvider.tsx:75,105,179` (`needsBootstrap` state, `/api/bootstrap/status` fetch, exposed on context) + `web/src/App.tsx:34-37` (guard) + `web/src/auth/AuthProvider.test.tsx:151,164` + `web/src/App.test.tsx:22,31` | ✅ PASS |
| SHD-20 | Public `POST /api/bootstrap` creates first admin (email+password), `owner` role by column default | `owner` role, no explicit role param needed | `internal/api/bootstrap_handler.go:90-112` (inserts via `BootstrapFirst`, responds with `db.RoleOwner`) + `web/src/features/auth/BootstrapPage.tsx` (form) + `BootstrapPage.test.tsx:44` `"submissão válida ... cria o admin ... (SHD-16/SHD-18)"` | ✅ PASS |
| SHD-21 | Direct navigation to `/bootstrap` while an admin exists → redirect to `/login` | Redirect, not the bootstrap form | `web/src/App.tsx:41-49` (`BootstrapRoute` guard: `!needsBootstrap` → `Navigate to="/login"`) + `web/src/App.test.tsx:40,48` | ✅ PASS |
| SHD-22 | README's manual SQL/bcrypt-script bootstrap section removed | No manual `INSERT INTO admins` instructions remain | `README.md` - grep for `INSERT INTO admins` / manual bcrypt-script instructions returns no matches; `README.md:234` describes the in-product `/bootstrap` screen instead | ✅ PASS |

**Status**: ✅ 22/22 ACs covered with `file:line` evidence matching the spec-defined outcome. Two (SHD-07, SHD-12) are correctly implemented and independently re-verified by the Verifier but are absent from `tasks.md`'s own Requirement Coverage table despite that table's "22 total, 22 mapped, 0 unmapped" claim - a traceability documentation gap, not a functional one.

---

## Discrimination Sensor

Ran in an isolated `git worktree` (never the real tree); baseline `git status --porcelain` captured before, confirmed identical after cleanup.

| # | File:line | Mutation | Killed? |
| - | --- | --- | --- |
| 1 | `internal/db/admin_repository.go:217` | Inverted `BootstrapFirst`'s existence check: `if count > 0` → `if count < 0` (never true with real data, so it would always attempt to create a second admin) | ✅ Killed - `TestAdminRepository_BootstrapFirst_AdminAlreadyExists_ReturnsFalseNoSecondAdmin`, `TestAdminRepository_BootstrapFirst_ConcurrentCalls_RealLockContention`, `TestBootstrapHandler_Create_AlreadyBootstrapped_Returns409NoSecondAdmin` all failed |
| 2 | `internal/cli/routes.go:134` | Removed `r.NotFound(web.StaticHandler().ServeHTTP)` wiring entirely | ✅ Killed - `TestAdminRouter_UnmatchedNonAPIPath_ReturnsEmbeddedIndexHTML`, `TestAdminRouter_UnmatchedAPIPath_ReturnsJSON404NotHTML` both failed |
| 3 | `internal/db/migrations_embed.go:46-56` | `MigrateUpEmbedded` builds the migrator but never calls `m.Up()` - migrations silently skipped | ✅ Killed - `TestMigrateUpEmbedded_FreshDatabase_AppliesAllMigrations` failed |

**Sensor depth**: lightweight (default tier, 3 targeted mutations on the highest-risk new code: the bootstrap race guard, the SPA/API fallback dispatch, and the migration-apply step).
**Sensor outcome**: 3/3 mutations killed (discrimination sensor itself has no open issue - see Gate Check below for the one open issue this report identifies).
**Isolation**: scratch worktree removed via `git worktree remove --force`; real tree `git status --porcelain` after == before (`M web/tsconfig.tsbuildinfo`, pre-existing and unrelated).

---

## Edge Cases (spec.md)

- [x] Bootstrap payload with an email that's already an admin: structurally unreachable given AC4/SHD-15's guard (an admin existing always short-circuits to `409` before any insert attempt) - correctly not separately tested; the guard itself (`internal/db/admin_repository.go:217`) is what makes the duplicate-email path unreachable, matching the spec's own framing ("impossible in practice ... guards the endpoint's own input validation").
- [x] Postgres not ready when `app` starts: handled by compose's `depends_on: condition: service_healthy` (`docker-compose.yml:18-20`), no app-level retry loop present - matches spec.
- [x] Real API 404 (e.g. unknown incident ID) vs. "no route at all": `internal/cli/routes_test.go:780` `TestAdminRouter_ExistingAPIRoute_StillReturnsJSON_AfterNotFoundWired` confirms `GET /healthz` still returns JSON through the real router after `NotFound` is wired in - independently re-run by the Verifier (see Gate Check).
- [x] `web/dist` never built → `go build`/`go test` fail to compile until built once: accepted convention per spec/design; not a bug. Confirmed the Dockerfile's build stage always runs `npm run build` first (`Dockerfile:8-14`), so this never surfaces in the container.

---

## Code Quality

| Principle | Status |
| --- | --- |
| Minimum code | ✅ |
| Surgical changes | ✅ |
| No scope creep | ✅ |
| Matches patterns | ✅ - mirrors `zeep-orbit`'s Docker/compose/bootstrap conventions as directed |
| Spec-anchored outcome check (asserted values match spec) | ✅ |
| Per-layer Coverage Expectation met (domain 1:1 ACs; routes happy+edge+error) | ✅ |
| Every test maps to a spec requirement - no unclaimed tests | ✅ |
| Documented guidelines followed | none found (`AGENTS.md`/`CONTRIBUTING.md` absent) - strong defaults applied, consistent with existing test depth |

---

## Gate Check

- **Gate command (per tasks.md)**: `TEST_DATABASE_URL=<dsn> go test -tags=integration ./...`
- **Result, default parallelism**: **FLAKY** - re-run 3 times independently by the Verifier, all 3 runs failed (non-zero exit), with a *different* combination of failing tests each time:
  - Run 1: 6 failures across `internal/api`, `internal/cli`, `internal/db`
  - Run 2: 5 failures across the same three packages, different test names
  - Run 3: 4 failures, again a different combination
  - Root cause confirmed by re-running with `-p 1` (serialized package execution): **0 failures, 3/3 clean runs**. `go test ./...` runs each package's tests as a separate OS process; by default multiple packages run concurrently. Three packages (`internal/db`, `internal/api`, `internal/cli`) each independently added their own "snapshot admins table, `DELETE FROM admins`, run bootstrap test, restore" helper (`internal/db/admin_repository_test.go:330`, `internal/api/bootstrap_handler_test.go:94`, `internal/cli/routes_test.go:670`) - none of the three serializes against the other two, so their clear/restore windows race each other and any other package's test that reads/writes the shared `admins` table against the same `TEST_DATABASE_URL` Postgres instance.
  - This codebase already has a precedent for exactly this failure class: `internal/dbtest/lock.go`'s `LockDatadogIntegration` - a Postgres advisory lock added specifically because "`go test ./...` runs separate packages' test binaries in parallel, and internal/db, internal/api, and internal/poller each have tests that insert, update, or delete that same unique row - without serialization those tests race each other's writes." No equivalent guard was added for the `admins` table despite the new bootstrap tests reintroducing the identical hazard for a different shared singleton table.
- **Individual per-package integration runs** (`go test -tags=integration ./internal/db/...`, `./internal/api/...`, `./internal/cli/...` run one at a time): all green, consistently, across 4+ repeated runs.
- **`go build ./...`**: exit 0. **`go vet ./...`**: exit 0 (no output). **`gofmt -l .`**: exit 0 (no output, nothing to format). **`go test ./...`** (unit only): exit 0, all packages `ok`.
- **Frontend**: `cd web && npm run test -- --run`: 175/177 passed, 2 failed (`PasswordResetRequestPage.test.tsx`) - independently confirmed **pre-existing and unrelated**: re-ran the identical test file against a clean `git worktree` checked out at `630f574` (the commit immediately before this feature's first commit) and the same 2 failures reproduce there (`useBrandLogoUrl`'s `useQuery` call has no `QueryClientProvider` in that test's render tree - unrelated to bootstrap/Docker). `npx tsc -b --noEmit`: exit 0, clean.
- **Docker**: `docker build -t vane-verify:latest .` (and independently re-run as `vane-verify2:latest`): both succeeded (~20s each, OrbStack daemon). `docker compose config -q`: exit 0. `make build`: exit 0, `bin/vane` produced (20.8MB).
- **Test count**: consistent with tasks.md's per-task counts (T1: 3, T2: 3, T3: 3, T4: 5, T6: 4, T7: 2+, T8: 4, T9: 4) - no silent deletions observed.
- **Skipped tests**: none observed.

---

## Gate Check — Iteration 2 re-verification (Verifier, independent, `df99d5d`/`b4e2930`)

A fix worker applied `dbtest.LockAdminsTable` (new, `internal/dbtest/lock.go`, distinct advisory key `727100002` vs. `LockDatadogIntegration`'s `727100001`) at 6 call sites across `internal/db/admin_repository_test.go`, `internal/api/bootstrap_handler_test.go`, `internal/cli/routes_test.go` (×2), and `internal/api/admins_test.go`, plus fixed 2 `LockDatadogIntegration` call sites (`internal/api/integrations_handler_test.go`, `internal/api/poller_status_test.go`) that were passing a bounded 5s setup `ctx` instead of `context.Background()`. The worker reported "8/8 consecutive green runs" of the mandatory gate at default parallelism.

**That report is not trustworthy as evidence and does not hold up under independent re-run.**

- `git log`/`git show` confirm both commits exist and match their stated diffs exactly (6 `LockAdminsTable` call sites, 2 `LockDatadogIntegration` ctx fixes, `tasks.md` coverage-table correction for SHD-07/SHD-12) - no discrepancy there.
- `dbtest/lock.go` itself is correctly implemented: dedicated connection (not borrowed from the test's pool, avoiding the `pool.Close` deadlock the doc comment warns about), distinct advisory-lock key from `LockDatadogIntegration`, released via `t.Cleanup`. No issue with the primitive.
- **First re-run attempt (plain `go test -tags=integration ./...`, no `-count`), 5 runs**: all 5 exit 0, all packages report `(cached)`. This is a false positive: Go's build cache reuses a previous pass/fail result for a package when its inputs are unchanged, so none of these 5 "runs" actually re-executed against Postgres. This is almost certainly what produced the worker's "8/8 green" claim too - it is the natural failure mode of re-running the same gate command back-to-back without `-count=1`, and 8/8 identical passes with zero variance is itself a signal of a cache hit, not 8 independent executions.
- **Second re-run, forcing real execution with `-count=1`, 5 runs**: **5/5 FAILED**, non-zero exit every time:
  - `internal/api`: `TestDeleteAdmin_SelfRemovalAsLastOwner_409` failed in 4/5 runs (`status = 200, want 409` - the delete succeeded because the table was *not* down to a single owner at assertion time); `TestListAdmins_Operator_403` failed once (`admins.UpdateRole() returned unexpected error: db: not found` - the admin row it expected was gone).
  - `internal/cli`: `TestAdminRouter_BootstrapRoutes_ReachableThroughRealRouter` failed in 5/5 runs (`GET /api/bootstrap/status "bootstrapped" = true` on a table it just cleared, or `POST /api/bootstrap status = 409, want 200, body = {"error":"already bootstrapped"}` - another process re-populated `admins` inside this test's supposedly-locked clear/restore window).
- **Root cause of the still-open race, confirmed by code inspection**: the fix's coverage rule ("lock every test that bulk-clears the table or asserts an exact row/owner count") is narrower than the actual invariant several tests depend on ("no admin row - especially no `owner`-role row - is created or deleted by *any* concurrently-running process for the duration of this test"). Several call sites create `RoleOwner` admins as ordinary test setup **without** taking `LockAdminsTable` at all:
  - `internal/cli/routes_test.go:109,120,429,552` - `issueRoutesTestToken(t, admins, db.RoleOwner)`, called by tests other than the one locked call site (`TestAdminRouter_OwnerAndOperator_AllWriteRoutes_PassAuthorization`, line 381).
  - `internal/api/poller_status_test.go:152` - `issueTestSessionTokenWithRole(t, admins, db.RoleOwner)`; this file only takes `LockDatadogIntegration` (for the unrelated `integrations` row), never `LockAdminsTable`.
  - Because `go test -tags=integration ./...` runs each package as a separate concurrent OS process against the *same* `TEST_DATABASE_URL` Postgres instance, any one of these unlocked owner-creating tests can insert or remove an owner-role row while `TestDeleteAdmin_SelfRemovalAsLastOwner_409` (which holds the lock only via `quarantineAmbientOwners`, and only against other lock-holders) is mid-flight, breaking its "I am provably the last owner" assumption and its 409 expectation. The lock only serializes lock-holders against each other - it provides no protection against non-lock-holding callers, and the fix commit did not audit for those.
- **Verdict**: the mandatory gate is still flaky at default parallelism after `df99d5d`. The fix reduced the failure surface (the specific bulk-clear helpers no longer race each other) but did not close it, because the underlying invariant ("no admin table mutation by any other test while this test's window is open") is broader than "tests that bulk-clear or exact-count," and at least 5 call sites creating owner-role rows were never audited against that broader invariant.
- **Also re-confirmed, unaffected by this iteration's change** (all still pass): `go build ./...` exit 0; `go vet ./...` exit 0; `gofmt -l .` exit 0 (no output); `go test ./...` (unit, no tag) exit 0, all packages `ok`.
- **`tasks.md` Requirement Coverage table**: re-read after `b4e2930` - now lists 22 distinct SHD IDs (SHD-07 added to T2/T12, SHD-12 added to T10), matching the "22 total, 22 mapped, 0 unmapped" summary line. This part of the fix is correct and closes Fix 2 below.
- **Frontend gate**: not re-run this iteration - no production or test code outside the Go integration-test files changed in `df99d5d`/`b4e2930`, so the frontend surface is unaffected; the prior iteration's frontend results stand.
- **Discrimination sensor**: not re-run - production logic is unchanged in this iteration (only test infrastructure and a documentation table changed), so the prior iteration's 3/3-killed sensor result remains valid evidence.

---

## Fix Plans

### Fix 1: `admins` table cross-package test isolation (flaky mandatory gate)

- **Root cause**: Three independently-written test helpers (`internal/db/admin_repository_test.go:330` `snapshotAndClearAdmins`, `internal/api/bootstrap_handler_test.go:94`, `internal/cli/routes_test.go:670` `clearAdminsForBootstrapRoutesTest`) each clear and restore the shared `admins` table for their own bootstrap tests, but none of them serializes against the other two or against any other test elsewhere that touches `admins` (e.g. `internal/db/admin_repository_test.go`'s own non-bootstrap tests, `internal/cli/routes_test.go`'s `TestAdminRouter_OwnerAndOperator_AllWriteRoutes_PassAuthorization`). Because `go test -tags=integration ./...` (the mandatory Full/Build gate per `tasks.md`) runs each package as a separate concurrent process against the same shared `TEST_DATABASE_URL` Postgres instance, these windows race, non-deterministically deleting or miscounting rows another package's test depends on.
- **Fix task**: Add a Postgres advisory-lock helper for the `admins` table, mirroring the existing precedent `internal/dbtest/lock.go`'s `LockDatadogIntegration` (e.g. `dbtest.LockAdminsTable(t, ctx, dsn)`), and call it at the top of every test in `internal/db/admin_repository_test.go`, `internal/api/bootstrap_handler_test.go`, and `internal/cli/routes_test.go` that clears/depends on the `admins` table's exact contents - not just the three bootstrap-specific ones, since `TestAdminRepository_CountActiveOwners_CountsOnlyOwners` and `TestAdminRouter_OwnerAndOperator_AllWriteRoutes_PassAuthorization` were both observed failing from this same contention during the Verifier's repeated runs.
- **Priority**: Major - the feature's own functional behavior is correct (all 22 ACs pass, discrimination sensor 3/3 killed, every package passes cleanly in isolation), but the mandatory gate command specified by this feature's own `tasks.md` does not reliably pass as written, which is a regression in CI/local-dev reliability for every future run of `go test ./...` on this repository, not just this feature's own tests.

### Fix 2: `tasks.md` Requirement Coverage table traceability gap

- **Root cause**: SHD-07 and SHD-12 are never referenced in any task's `Requirement:` field, and the "Requirement Coverage" section's closing claim ("22 total, 22 mapped, 0 unmapped") is incorrect - only 20 distinct SHD IDs appear in that summary line.
- **Fix task**: Update `tasks.md`'s Requirement Coverage section to add `SHD-07 → T2/T12` (frontend-build-then-Go-build convention) and `SHD-12 → T10` (Dockerfile's multi-stage/scratch final image), and correct the "22 total, 22 mapped" count. Documentation-only; both underlying capabilities are implemented and independently verified above.
- **Priority**: Minor - no functional impact; both ACs are demonstrably satisfied. Matters for future audits trusting the coverage table's own claim at face value.
- **Status (iteration 2)**: ✅ **Closed**. `b4e2930` applies exactly this fix; re-read `tasks.md` and confirmed 22 distinct SHD IDs now appear in the Requirement Coverage table, matching its "22 total, 22 mapped, 0 unmapped" line.

### Fix 3 (new, iteration 2): `LockAdminsTable` coverage is narrower than the invariant it needs to protect

- **Root cause**: `df99d5d` correctly identified the failure class and added `dbtest.LockAdminsTable`, but scoped its application to "tests that bulk-clear the `admins` table or assert an exact row/owner count" - a narrower set than the actual invariant several tests need: *no other concurrently-running test process may create or delete any admin row, particularly an `owner`-role row, for the duration of this test*. At least 5 call sites create `RoleOwner` admins as ordinary test setup without taking the lock at all: `internal/cli/routes_test.go:109,120,429,552` (`issueRoutesTestToken(t, admins, db.RoleOwner)`, used by tests other than the one locked call site at line 381) and `internal/api/poller_status_test.go:152` (`issueTestSessionTokenWithRole(t, admins, db.RoleOwner)` - this file never imports/calls `LockAdminsTable`, only the unrelated `LockDatadogIntegration`). Because these run as separate concurrent OS processes against the same shared Postgres instance, any of them can insert/remove an owner row mid-flight and break another test's "I am the last/only owner" assumption even while that other test correctly holds the lock - the lock only protects against *other lock-holders*, not against every writer.
- **Reproduced**: 5/5 fresh (`-count=1`, cache bypassed) runs of the exact mandatory gate command failed. `internal/api.TestDeleteAdmin_SelfRemovalAsLastOwner_409` failed in 4/5 runs (`status = 200, want 409`); `internal/api.TestListAdmins_Operator_403` failed once (`db: not found`); `internal/cli.TestAdminRouter_BootstrapRoutes_ReachableThroughRealRouter` failed in 5/5 runs (`"bootstrapped" = true` on a table it just cleared, or a spurious `409` on its own bootstrap POST).
- **Fix task**: Either (a) extend `LockAdminsTable` to every call site that creates or deletes a row in `admins` with `role = owner` (not just the bulk-clear/exact-count ones) - i.e. `internal/cli/routes_test.go`'s `issueRoutesTestToken` calls and `internal/api/poller_status_test.go`'s `issueTestSessionTokenWithRole` call - or (b) give each such test its own uniquely-seeded admin(s) that never touch role=owner counting semantics, if the owner role is incidental to what that test is checking (e.g. `poller_status_test.go`'s test may not actually need `RoleOwner` specifically, only *some* role with sufficient privilege). Whichever fix is chosen, re-run the full mandatory gate 5+ times **with `-count=1`** (not the default, cache-eligible invocation) to get real signal.
- **Priority**: Major (same class as Fix 1 in iteration 1) - this is the same mandatory gate still failing, just with the specific race narrowed rather than closed.

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| --- | --- | --- |
| SHD-01 through SHD-22 | Implementing | ✅ Verified |

---

## Summary (Iteration 1 - superseded, kept for history)

**Iteration 1 Outcome (historical, superseded)**: FAIL ❌ - the mandatory Full/Build gate command from `tasks.md` (`TEST_DATABASE_URL=<dsn> go test -tags=integration ./...`) does not reliably pass (3/3 independent re-runs failed at default parallelism; see Gate Check). All 22 ACs are functionally implemented and independently verified with `file:line` evidence, and the discrimination sensor killed 3/3 injected mutations, but per this skill's own rule ("Non-zero exit code = STOP"), a flaky mandatory gate keeps this feature from being marked done until Fix 1 lands and the gate is re-confirmed green across repeated runs.

**Overall**: ⚠️ Issues (functionally complete, gate reliability gap)

**Spec-anchored check**: 22/22 ACs matched spec outcome (0 spec-precision gaps)
**Sensor**: 3/3 mutations killed
**Gate**: functionally green in every configuration tested (per-package, unit-only, docker, compose, make, frontend) but **flaky under the exact mandatory command specified in `tasks.md`** (`go test -tags=integration ./...` at default parallelism) - 3/3 independent re-runs failed, 0/3 failures when serialized (`-p 1`)

**What works**: SPA embedding with correct fallback/404 semantics (SHD-01-05), embedded migrations applied automatically at boot with a visible `Info` log (SHD-06, SHD-10, design.md risk (c) satisfied), full Docker/Compose/Makefile/README deployment story mirroring `zeep-orbit` (SHD-08-14, independently rebuilt and re-verified by the Verifier), race-safe first-admin bootstrap with a real concurrency test proving lock contention (SHD-15-18), and the frontend's boot-time redirect in both directions (SHD-19-22). The existing production router's other API routes are unaffected by the new `NotFound` wiring (design.md risk (a), independently re-confirmed). The public, unauthenticated bootstrap endpoint is documented as an accepted risk equivalent to the existing invite-accept endpoint, not a new one (design.md risk (b) - confirmed as documented, not silently accepted).

**Issues found**:
1. The mandatory Full/Build gate command is flaky due to unserialized cross-package access to the shared `admins` table in tests - see Fix 1.
2. `tasks.md`'s own Requirement Coverage table under-counts by 2 (SHD-07, SHD-12 unmapped despite its "0 unmapped" claim) - see Fix 2.

---

## Summary (Iteration 2 - current)

**Iteration 2 Outcome (historical, superseded)**: FAIL ❌ - still. The fix worker's claimed "8/8 consecutive green runs" does not survive independent re-verification: those runs (and this Verifier's own first attempt at repeating them) were `go test` cache hits (`(cached)` in output), not real re-executions - forcing real execution with `-count=1` produces **5/5 failed runs**. Fix 1 (from iteration 1) is real and correct as far as it goes - `LockAdminsTable` is a sound primitive, applied to the right places for the bulk-clear/exact-count tests it was scoped to - but its scope was too narrow to close the invariant several tests actually depend on. See Fix 3 above for the specific unlocked call sites and reproduction.

**Overall**: ❌ FAIL (mandatory gate still non-deterministically red)

**Spec-anchored check**: unchanged from iteration 1 - 22/22 ACs still matched spec outcome (no production code changed this iteration, only test infrastructure and a docs table)
**Sensor**: unchanged from iteration 1 - 3/3 mutations killed (not re-run; production logic untouched, so prior result stands)
**Gate**: `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...` (unit) all still exit 0. `TEST_DATABASE_URL=<dsn> go test -tags=integration ./...` at default parallelism: **5/5 failed** when actually re-executed (`-count=1`); the apparent "green" runs without `-count=1` are cache hits and not evidence of anything.

**Issues found (iteration 2)**:
1. `LockAdminsTable`'s coverage is narrower than the invariant it needs to protect - several owner-role-admin-creating call sites in `internal/cli/routes_test.go` and `internal/api/poller_status_test.go` never take the lock, so they can still race a locked test's "I am the only/last owner" assumption. See Fix 3.
2. Process note, not a code issue: gate re-verification must use `-count=1` (or an equivalent cache-bypass) to produce real evidence; a bare repeated `go test` invocation reports cached pass/fail and cannot detect this class of race at all. Recommend adding this to `tasks.md`'s gate-command documentation and/or `LESSONS.md` so future Verifier/worker rounds don't repeat the same false-green mistake.

**Fix 2 (traceability table) is closed** - correctly fixed by `b4e2930`, confirmed by re-reading `tasks.md`.

**Next steps**: Route Fix 3 to an implementer - extend `LockAdminsTable` to the 5 identified unlocked owner-creating call sites (or eliminate their dependence on the owner role/count invariant if it's incidental), then re-verify with `TEST_DATABASE_URL=<dsn> go test -tags=integration -count=1 ./...` run 5+ times before considering this feature's gate green. Do not accept a re-run report that omits `-count=1` or otherwise doesn't rule out cache hits.

**Next steps**: Route Fix 1 to an implementer (add an `admins`-table advisory-lock test helper mirroring `dbtest.LockDatadogIntegration`, apply it across the three affected test files) and re-run the Full gate 3+ times at default parallelism to confirm the fix before closing the feature. Fix 2 can be applied directly to `tasks.md` as a documentation correction.

---

## Gate Check — Iteration 3 re-verification (Verifier, independent, `526139e`/`7e401dc`)

A fix worker (continued manually after a session cut) applied two commits:

- `526139e` - widened `dbtest.LockAdminsTable` coverage from "tests that bulk-clear/exact-count `admins`" to every shared test constructor/helper that creates an admin row (`issueRoutesTestToken`, `issueTestSessionTokenWithRole`, `issueTestSessionToken`, `newAdminsRouter`, `newAdminRepositoryForTest`, plus the equivalents in `admin_invites_test.go`, `password_reset_repository_test.go`, `audit/log_test.go`, `api/middleware_test.go`/`auth_handler_test.go`) - closing exactly the gap iteration 2 identified (`internal/cli/routes_test.go`'s non-locked `issueRoutesTestToken` call sites, `internal/api/poller_status_test.go`'s `issueTestSessionTokenWithRole` call). Also made `lockAdvisoryKey` idempotent per `(*testing.T, key)` via a `heldLocks` map (so a test reaching the same lock through more than one helper doesn't deadlock itself on its own second dedicated connection), removed the now-redundant explicit lock call in `TestAdminRouter_OwnerAndOperator_AllWriteRoutes_PassAuthorization` (would otherwise deadlock against `issueRoutesTestToken`'s own lock), and fixed a hardcoded non-unique test email in `TestAdminsMigration_AppliesClean_AndEnforcesUniqueEmail`.
- `7e401dc` - while investigating, the fix author found a **second, structurally identical, previously-undetected race**: `company_settings` (a singleton row, `id=1`) is reset/asserted by `internal/db`, `internal/api`, and `internal/cli` tests with no cross-package serialization at all - the exact same hazard class as `admins`, just never flagged because iteration 1/2's re-runs happened not to surface it. Added `dbtest.LockCompanySettings` (distinct advisory key `727100003` - confirmed distinct from `727100001`/`727100002` by reading the diff), applied at 6 call sites across those three packages.

**Independent verification performed by this Verifier (skeptical of both the prior session's own summary and the "I ran it 16 times" claim in my task brief) - not accepted at face value:**

1. `git log --oneline -25` confirmed all expected commits present in order: `df99d5d`, `b4e2930`, `526139e`, `7e401dc`, plus the 13 `self-hosted-docker-bootstrap` feature commits (`97179cc`..`76f517a`) and prior features' history. No discrepancy.
2. Read the full diffs of `526139e` and `7e401dc` (`git show`, not summaries). Confirmed: `internal/dbtest/lock.go`'s new `companySettingsLockKey = 727100003` constant is distinct from `datadogIntegrationLockKey = 727100001` and `adminsTableLockKey = 727100002`; the `heldLocks` idempotency map is correctly scoped per `*testing.T` and cleaned up in the same `t.Cleanup` that releases the lock; `internal/api/admins_test.go`'s `issueTestSessionTokenWithRole` (the helper `poller_status_test.go` depends on, per iteration 2's own finding) now calls `dbtest.LockAdminsTable` before `admins.Create`, closing that specific gap.
3. Ran the mandatory gate **16 times**, each with `-count=1` explicitly (never bare `go test`, never trusting `(cached)` output), against the real `TEST_DATABASE_URL=postgres://zeep:zeep@localhost:5434/zeep?sslmode=disable` Postgres instance, each run's full output saved to a separate log file and its exit code recorded independently:
   - **Outcome: 16/16 PASS, 0 failures, 0 flakes.** A failure-marker search against all 16 logs returns zero hits for every one.
   - Confirmed these were real executions, not cache hits, by observing genuinely varying per-package wall-clock times across runs (`internal/api`: 43.4s-62.7s; `internal/db`: 42.5s-61.3s; `internal/poller`: 39.1s-57.9s) - a `(cached)` run reports near-instant, uniform timing; these did not.
   - Directly inspected live Postgres advisory-lock contention mid-run (`pg_stat_activity`, `wait_event = advisory`) during several of the 16 runs to confirm the locks are genuinely serializing concurrent test-package access (not silently no-oping) - observed real lock queueing (multiple sessions blocked on `pg_advisory_lock`), consistent with the fix's intended mechanism, and every run still completed and passed.
   - `TestPublicStatusPreview_PublishedPage_200Unaffected` (the flake reported by the prior session, in `internal/api/public_status_preview_handler_test.go` - a file untouched by any commit in this feature) did **not** fail in any of the 16 runs. This Verifier cannot independently confirm the prior session's claimed 1/16 failure of that specific test (it simply didn't reproduce here), but confirms the file is untouched by this feature's diff and the reported failure, if real, is unrelated to `admins`/`company_settings` - consistent with the prior session's own isolation testing (5/5 clean when run alone).
4. **No failure in any of the 16 runs mentions `admins` or `company_settings`** - the explicit bar this Verifier's task set for an automatic FAIL. Both Fix 1 (iteration 1) and Fix 3 (iteration 2) are now confirmed closed; no new, narrower race surfaced.
5. Re-ran the full non-integration/build surface: `go build ./...` exit 0; `go vet ./...` exit 0, no output; `gofmt -l .` exit 0, no output; `go test ./...` (unit, no tag) exit 0, all packages `ok`.
6. Frontend gate re-run: `cd web && npm run test -- --run` → 175/177 passed, 2 failed, both in `PasswordResetRequestPage.test.tsx` (`useBrandLogoUrl`'s `useQuery` call has no `QueryClientProvider` in that test's render tree) - identical failure signature and count to iterations 1/2, confirmed pre-existing and unrelated (no commit in this feature touches that file or `useBrandLogoUrl`). `npx tsc -b --noEmit` exit 0, clean.
7. **Spec-anchored check and discrimination sensor**: re-read iteration 1's 22/22 AC table and 3/3-mutation-killed sensor result. Confirmed no production code changed across `df99d5d`, `b4e2930`, `526139e`, or `7e401dc` - `git show --stat` on all four confirms every changed file is either a `_test.go` file, `internal/dbtest/lock.go` (test-only infrastructure package, never imported by non-test code), or `tasks.md`. Both results stand unmodified from iteration 1.

**Verdict**: ✅ **PASS**. The mandatory gate (`TEST_DATABASE_URL=<dsn> go test -tags=integration -count=1 ./...`) is now reliably green - 16/16 independent real executions, 0 failures, 0 flakes, with live confirmation the serialization mechanism is actually engaging (not a no-op fix). Fix 1 → Fix 3's iterative narrowing (bulk-clear/exact-count tests → every owner-creating helper) closed the `admins` race completely; the separately-discovered `company_settings` race is closed by the same mechanism. All 22 ACs remain independently verified with `file:line` evidence (unchanged since iteration 1, no production code touched since). The one known frontend flake (`PasswordResetRequestPage.test.tsx`, 2 tests) and the one reported-but-unreproduced Go flake (`TestPublicStatusPreview_PublishedPage_200Unaffected`) are both pre-existing, in files untouched by this feature, and documented below as known non-blocking risks - not this feature's responsibility to fix.

**Overall**: ✅ PASS

**Spec-anchored check**: 22/22 ACs matched spec outcome (unchanged since iteration 1 - no production logic changed in iterations 2-3)
**Sensor**: 3/3 mutations killed (unchanged since iteration 1 - not re-run, production logic untouched)
**Gate**: `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...` (unit) all exit 0. `TEST_DATABASE_URL=<dsn> go test -tags=integration -count=1 ./...`: **16/16 real, independent executions, 0 failures**. Frontend: 175/177 (2 pre-existing unrelated failures), `tsc` clean.

**Known non-blocking risks (documented, not fixed here, both out of this feature's scope)**:
1. `TestPublicStatusPreview_PublishedPage_200Unaffected` (`internal/api/public_status_preview_handler_test.go`) - reported by the prior session as 1/16 failures under full-suite parallelism, with a 500 error, but passing 5/5 when run in isolation. Not reproduced in this Verifier's own 16 runs (0/16). File untouched by any commit in this feature. Recommend opening as a separate backlog item to determine if this is a genuine rare flake (e.g. connection-pool contention under heavy parallel load) or was itself an artifact of the same class of cross-package races this feature just fixed two instances of - worth one more targeted investigation given the precedent, but not blocking this feature's PASS.
2. `PasswordResetRequestPage.test.tsx` (2 tests, `web/src/features/auth/`) - `useBrandLogoUrl`'s `useQuery` call has no `QueryClientProvider` in that test's render tree. Confirmed pre-existing (reproduces on a clean `630f574` worktree, per iteration 1) and unrelated to this feature. Straightforward fix would be wrapping that test's render in a `QueryClientProvider` - recommend a small separate task.

**Requirement Traceability**: SHD-01 through SHD-22 remain ✅ Verified (unchanged since iteration 1).

**Next steps**: None for this feature - close it. The two documented risks above are candidates for separate backlog items, not gates on this feature.
