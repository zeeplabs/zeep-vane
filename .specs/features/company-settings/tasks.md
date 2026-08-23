# Company Settings Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/company-settings/design.md`
**Status**: Approved

---

## Test Coverage Matrix

> Generated from codebase sampling (`internal/api/domains_handler_test.go`, `internal/db/domains_migration_test.go`, `internal/config/config_test.go`, `web/src/test/msw/handlers.ts`, `web/src/features/public-status/PublicStatusPage.test.tsx`) plus spec ACs. No `AGENTS.md`/lint-config guideline files found - coverage expectations use the strong default (1:1 to spec ACs, every listed edge case), matching the depth already set in `admin-frontend/tasks.md`.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Go migration (`0012_company_settings`) | integration | Singleton constraint enforced (`CHECK (id = 1)` rejects a second row), seed row present after `MigrateUp` | `internal/db/company_settings_migration_test.go` (tag `integration`) | `go test -tags=integration ./internal/db` |
| Go repository (`CompanySettingsRepository`) | integration | `Get`, `Update`, `UpdateLogoURL` - SET-01, SET-03, SET-06 | `internal/db/company_settings_repository_test.go` (tag `integration`) | `go test -tags=integration ./internal/db` |
| Go config (`UPLOADS_DIR`) | unit | Set + unset (default) paths, matching existing `CORS_ALLOWED_ORIGIN` test style | `internal/config/config_test.go` | `go test ./internal/config` |
| Go `internal/uploads.Save` | unit | Fresh write, overwrite-removes-old-file, extension handling - SET-10 | `internal/uploads/store_test.go` | `go test ./internal/uploads` |
| Go handler (`CompanySettingsHandler`) | integration | Every status code in spec: 200/403/422/500 for `Get`/`Update`; 200/422(size)/422(mime)/500 for `UploadLogo` - SET-01/02/04/05/07/08/09/13/14 | `internal/api/company_settings_handler_test.go` (tag `integration`) | `go test -tags=integration ./internal/api` |
| Go handler (`logoFileHandler`) | integration | Serves stored file (200), rejects path-traversal filenames (404), missing file (404) - SET-06, edge case | `internal/api/logo_file_handler_test.go` (tag `integration`) | `go test -tags=integration ./internal/api` |
| Go routing wiring (`routes.go`, `serve.go`) | integration | Admin router exposes all 3 new routes behind `ownerOnly`; public `HostRouter` listener serves `/uploads/{filename}` alongside the existing `/` public status route - SET-11, SET-12, design.md Risks | `internal/cli/routes_test.go` (if present) or `internal/router/host_router_test.go` (extended) | `go test -tags=integration ./internal/cli ./internal/router` |
| Go public status enrichment (`PublicStatusHandler`) | integration | `composeResponse` includes `company.name`/`company.logo_url`; both production and I12 preview paths - SET-15, SET-16 | `internal/api/public_status_handler_test.go`, `internal/api/public_status_preview_handler_test.go` | `go test -tags=integration ./internal/api` |
| React hooks (`settings/hooks.ts`) | unit (via MSW) | `useCompanySettings`, `useUpdateCompanySettings` (no `logo_url` in payload), new `useUploadCompanyLogo` - happy path + 403/422/500 | `web/src/features/settings/hooks.test.ts` | `cd web && npm run test` |
| React page (`SettingsPage.tsx`) | unit/component | Load, edit+save name/e-mail, upload logo (separate action from save), inline error on validation/upload failure - first test file for this component | `web/src/features/settings/SettingsPage.test.tsx` | `cd web && npm run test` |
| React hooks (`public-status/hooks.ts`) | unit (via MSW) | `usePublicStatusPage` returns real `company_name`/`logo_url` from the API response, never `mockData.companySettings`; null-logo case (SET-16) | `web/src/features/public-status/hooks.test.ts` | `cd web && npm run test` |
| MSW handlers (`test/msw/handlers.ts`) | none | Fixture/mock code - covered indirectly by the hook/component tests above | `web/src/test/msw/handlers.ts` | `cd web && npm run test` (via consuming tests) |
| Config/migration SQL files themselves | none | Build gate only | `internal/db/migrations/0012_*.sql` | `go build ./...` |

## Gate Check Commands

> Generated from `Makefile` (`go test ./...`, `gofmt -l .`, `go vet ./...`) and `web/package.json` (`npm run test`, `npm run build`), matching the commands already established in `admin-frontend/tasks.md`.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick (backend, unit only) | After a backend task with no DB-backed test | `go test ./...` |
| Quick (frontend) | After a frontend task | `cd web && npm run test` |
| Full (backend, integration) | After a backend task with a DB-backed/integration test | `go test -tags=integration ./... && gofmt -l . && go vet ./...` |
| Build (backend) | After phase completion | `go build ./... && gofmt -l . && go vet ./...` |
| Build (frontend) | After phase completion | `cd web && npm run build` |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Backend Foundation

Tasks: T1, T2, T3, T4 (executed in this order; only T2 has a real dependency, on T1 - see the dependency graph in `## Phase Execution Map` below for exact edges)

### Phase 2: Backend HTTP Surface + Wiring

Tasks: T5, T6, T7, T8, T9 (executed in this order; see the dependency graph below for exact edges)

### Phase 3: Public Status Enrichment

Tasks: T10

### Phase 4: Frontend

Tasks: T11, T12, T13, T14 (executed in this order; see the dependency graph below for exact edges)

---

## Task Breakdown

### T1: Company settings migration (singleton table)

**What**: Add `internal/db/migrations/0012_company_settings.up.sql` (table with `CHECK (id = 1)` singleton constraint + seed row) and `.down.sql`, plus a migration test asserting the constraint and the seed.
**Where**: `internal/db/migrations/0012_company_settings.up.sql`, `internal/db/migrations/0012_company_settings.down.sql`, `internal/db/company_settings_migration_test.go`
**Depends on**: None
**Reuses**: `internal/db/migrations/0006_domains.up.sql` style, `internal/db/domains_migration_test.go` structure, `internal/db/migrate.go` runner
**Requirement**: SET-03, SET-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `MigrateUp` creates `company_settings` with exactly one seeded row (`name=""`, `contact_email=""`, `logo_url=NULL`)
- [x] A direct `INSERT INTO company_settings (id) VALUES (2)` (or any id != 1) fails with a constraint violation, asserted in the test
- [x] Gate check passes: `go test -tags=integration ./internal/db`
- [x] Test count: 2+ new tests pass (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add singleton company_settings migration`

---

### T2: CompanySettingsRepository

**What**: `db.CompanySettings` struct + `CompanySettingsRepository` with `Get`, `Update(name, contactEmail)`, `UpdateLogoURL(logoURL)`.
**Where**: `internal/db/company_settings_repository.go`, `internal/db/company_settings_repository_test.go`
**Depends on**: T1
**Reuses**: `internal/db/domain_repository.go` struct/constructor pattern, `internal/db/admin_repository.go:89-101` (`Update*` + `RowsAffected` pattern), `internal/db/pool.go`
**Requirement**: SET-01, SET-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `Get` returns the singleton row with no "not found" branch
- [x] `Update` persists `name`/`contact_email` and returns the updated row
- [x] `UpdateLogoURL` persists `logo_url` independently of `Update`
- [x] Gate check passes: `go test -tags=integration ./internal/db`
- [x] Test count: 3+ new tests pass (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add CompanySettingsRepository`

---

### T3: `UPLOADS_DIR` config

**What**: Add optional `UploadsDir` field to `config.Config`, read from `UPLOADS_DIR`, defaulting to `./data/uploads` when unset.
**Where**: `internal/config/config.go`, `internal/config/config_test.go`
**Depends on**: None
**Reuses**: `CORSAllowedOrigin`/`defaultCORSAllowedOrigin` pattern (`config.go:21,26,64-67`)
**Requirement**: SET-11

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `UPLOADS_DIR` set -> `Config.UploadsDir` reflects it
- [x] `UPLOADS_DIR` unset -> `Config.UploadsDir == "./data/uploads"`
- [x] Gate check passes: `go test ./internal/config`
- [x] Test count: 2 new tests pass (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(config): add UPLOADS_DIR setting`

---

### T4: `internal/uploads.Save` atomic file writer

**What**: `Save(dir, ext string, r io.Reader) (servedPath string, err error)` - writes to a temp file, `os.Rename`s over `logo<ext>`, removing any other `logo.*` first; lazily `MkdirAll`s `dir`.
**Where**: `internal/uploads/store.go`, `internal/uploads/store_test.go`
**Depends on**: None
**Reuses**: nothing existing (new, dependency-free package per design.md)
**Requirement**: SET-10, SET-11, edge case (lazy `MkdirAll`)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] First `Save` into a non-existent dir creates it and writes `logo<ext>`
- [ ] A second `Save` with a different extension removes the first file, leaving exactly one `logo.*`
- [ ] Returned `servedPath` is `/uploads/logo<ext>`
- [ ] Gate check passes: `go test ./internal/uploads`
- [ ] Test count: 3+ new tests pass (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(uploads): add atomic single-file store`

---

### T5: `CompanySettingsHandler.Get`/`Update`

**What**: JSON handler for `GET`/`PATCH /api/company-settings` - decode, validate (`name` non-empty, `contact_email` via `net/mail.ParseAddress`), map to 200/422/500.
**Where**: `internal/api/company_settings_handler.go`, `internal/api/company_settings_handler_test.go`
**Depends on**: T2
**Reuses**: `internal/api/domains_handler.go:17-20` (narrow-interface pattern), `writeInternalError` (`auth_handler.go:139`)
**Requirement**: SET-01, SET-03, SET-04, SET-05

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `GET` returns 200 + current settings (fresh-install seeded row included)
- [ ] `PATCH` with valid body persists and returns 200
- [ ] `PATCH` with empty `name` -> 422, no persistence (assert via a following `GET`)
- [ ] `PATCH` with malformed `contact_email` -> 422, no persistence
- [ ] Gate check passes: `go test -tags=integration ./internal/api`
- [ ] Test count: 4+ new tests pass (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): add company settings GET/PATCH handler`

---

### T6: `CompanySettingsHandler.UploadLogo`

**What**: Multipart handler for `POST /api/company-settings/logo` - `http.MaxBytesReader` (10 MB) before `ParseMultipartForm`, `http.DetectContentType` sniff against `image/png`/`image/svg+xml`, calls `uploads.Save` then `UpdateLogoURL` only on write success.
**Where**: `internal/api/company_settings_handler.go` (extend), `internal/api/company_settings_handler_test.go` (extend)
**Depends on**: T4, T5
**Reuses**: `internal/uploads.Save` (T4), same handler/interfaces from T5
**Requirement**: SET-07, SET-08, SET-09, SET-10, SET-13

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Valid PNG upload (< 10 MB) -> 200, `logo_url` updated in a following `GET`
- [ ] Upload > 10 MB -> 422, `logo_url` unchanged
- [ ] Upload with a non-PNG/SVG payload -> 422, `logo_url` unchanged
- [ ] Second valid upload overwrites the first (only one file remains on disk, asserted via a temp test dir)
- [ ] Gate check passes: `go test -tags=integration ./internal/api`
- [ ] Test count: 4+ new tests pass (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): add company logo upload endpoint`

---

### T7: `logoFileHandler`

**What**: `NewLogoFileHandler(uploadsDir string) http.Handler` - validates `filename` has no `/`/`..` segments, serves via `http.ServeFile`, 404 on anything else or a missing file.
**Where**: `internal/api/logo_file_handler.go`, `internal/api/logo_file_handler_test.go`
**Depends on**: None
**Reuses**: none existing (new pattern per design.md)
**Requirement**: SET-06, edge case (missing file -> 404)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Requesting the exact stored filename serves file bytes with 200
- [ ] Requesting a filename containing `..` or `/` -> 404 (never resolves outside `uploadsDir`)
- [ ] Requesting a filename that doesn't exist on disk -> 404
- [ ] No authentication required (handler registered outside any `RequireAuth`/`RequireRole` chain, asserted by the test not injecting a session)
- [ ] Gate check passes: `go test -tags=integration ./internal/api`
- [ ] Test count: 3+ new tests pass (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): add public logo file handler`

---

### T8: Wire admin routes

**What**: In `buildAdminRouter`, register `GET`/`PATCH /api/company-settings` and `POST /api/company-settings/logo` under `ownerOnly`, and mount `logoFileHandler` at `/uploads/{filename}` (unauthenticated, outside the `protected` group).
**Where**: `internal/cli/routes.go` (modify)
**Depends on**: T5, T6, T7
**Reuses**: `ownerOnly` group (`routes.go:50`), existing route-registration style (`routes.go:65-68`)
**Requirement**: SET-02, SET-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `GET`/`PATCH /api/company-settings` reachable only with an owner session on the real admin router (403 for operator/viewer, 401 with no session)
- [ ] `POST /api/company-settings/logo` same RBAC
- [ ] `GET /uploads/{filename}` reachable on the admin router with no session
- [ ] Gate check passes: `go test -tags=integration ./internal/cli`
- [ ] Test count: 3+ new tests pass (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(cli): wire company settings routes into admin router`

---

### T9: Dual-mount `/uploads/` on the public listener

**What**: In the public Host-routed listener wiring, wrap `publicHandler.Get` and `logoFileHandler` in a small mux (`http.ServeMux` or `chi.Mux`) mounted at `/` and `/uploads/` respectively, then pass that mux (not `publicHandler.Get` alone) to `router.HostRouter`.
**Where**: `internal/cli/serve.go` (modify, around the block building the public listener)
**Depends on**: T7
**Reuses**: `router.HostRouter` (`internal/router/host_router.go`) unchanged - only its input handler changes
**Requirement**: SET-06 (public serving), design.md Risks & Concerns (HostRouter forwards every path to one handler)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] A request to `/uploads/{filename}` on a published status page's hostname serves the logo file (200), not the JSON status payload
- [ ] A request to `/` on that same hostname still serves the existing public status JSON unchanged
- [ ] Gate check passes: `go test -tags=integration ./internal/router ./internal/cli`
- [ ] Test count: 2+ new/modified tests pass (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(cli): mount logo file handler on public status listener`

---

### T10: Public status response includes company identity

**What**: Add `companySettingsGetter` interface + `Company` field to `PublicStatusHandler`/`publicStatusResponse`; `composeResponse` populates it from `CompanySettingsRepository.Get`; update both construction call sites (`routes.go:44` for I12 preview, `serve.go:166` for production) to inject the repository.
**Where**: `internal/api/public_status_handler.go` (modify), `internal/api/public_status_handler_test.go` (modify), `internal/api/public_status_preview_handler_test.go` (modify), `internal/cli/routes.go` (modify), `internal/cli/serve.go` (modify)
**Depends on**: T2
**Reuses**: existing shared `composeResponse` (`public_status_handler.go:122`) - covers both production and I12 preview from one change
**Requirement**: SET-15, SET-16

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Production public status response includes `company.name`/`company.logo_url` matching the persisted `company_settings` row
- [ ] I12 dev/preview response includes the same fields, sourced the same way
- [ ] `logo_url: null` when no logo has ever been uploaded (SET-16)
- [ ] `mockData.companySettings` no longer referenced by `public_status_handler.go` (it never was) - confirms this task fully replaces the frontend mock's data source
- [ ] Gate check passes: `go test -tags=integration ./internal/api ./internal/cli`
- [ ] Test count: 3+ modified/new tests pass (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): include company identity in public status response`

---

### T11: MSW handlers for company settings

**What**: Add `GET`/`PATCH /api/company-settings` and `POST /api/company-settings/logo` handlers to `web/src/test/msw/handlers.ts`, backed by new in-memory `companySettingsState`, plus `resetCompanySettings()` wired into `test/setup.ts` the same way `resetAdmins()` is; update the existing `GET /api/status-pages/:id/public-preview` handler to include a `company` field sourced from the same state (so T14's hook test has something real to assert against).
**Where**: `web/src/test/msw/handlers.ts` (modify), `web/src/test/setup.ts` (modify, if it centralizes reset calls)
**Depends on**: None (frontend-only, mirrors the backend contract from T5/T6/T10 without depending on Go code)
**Reuses**: `resetAdmins`/`adminsState` pattern (`handlers.ts:97-102`), `mockData.companySettings` as the initial seed value
**Requirement**: SET-01, SET-07, SET-15 (frontend mock parity)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `GET /api/company-settings` returns the seeded state
- [ ] `PATCH /api/company-settings` updates the in-memory state and returns it
- [ ] `POST /api/company-settings/logo` accepts a `FormData` upload and updates `logo_url` in state
- [ ] `GET /api/status-pages/:id/public-preview` response now includes `company: {name, logo_url}` from the same state
- [ ] Gate check passes: `cd web && npm run test`
- [ ] Test count: existing suite (129 tests) still 100% green - no regressions from the handler change alone

**Tests**: none (fixture code; covered indirectly by T12/T13/T14)
**Gate**: quick

**Commit**: `test(msw): add company settings handlers`

---

### T12: `settings/hooks.ts` - drop `logo_url` from PATCH, add upload hook

**What**: Remove `logo_url` from `UpdateCompanySettingsInput`/`useUpdateCompanySettings`'s body; add `useUploadCompanyLogo()` mutation posting `FormData` to `/api/company-settings/logo`.
**Where**: `web/src/features/settings/hooks.ts` (modify), `web/src/features/settings/hooks.test.ts` (new)
**Depends on**: T11
**Reuses**: existing `useMutation`/`useQuery` + `apiFetch` pattern already in this file
**Requirement**: SET-01, SET-07 (frontend contract)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `useUpdateCompanySettings` sends only `{name, contact_email}`
- [ ] `useUploadCompanyLogo` posts `FormData` and updates the `["company-settings"]` query cache on success
- [ ] Hook test covers happy path for all 3 hooks + a 422/500 error case
- [ ] Gate check passes: `cd web && npm run test`
- [ ] Test count: 5+ new tests pass (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(web): update company settings hooks for real logo upload endpoint`

---

### T13: `SettingsPage.tsx` - real save + separate logo upload

**What**: Rewrite `handleLogoChange` to call `useUploadCompanyLogo` directly on file selection (immediate upload, not deferred to form submit); `handleSubmit` no longer includes `logo_url`; add the page's first test file.
**Where**: `web/src/features/settings/SettingsPage.tsx` (modify), `web/src/features/settings/SettingsPage.test.tsx` (new)
**Depends on**: T12
**Reuses**: existing form structure, `ApiError` handling pattern already in the file (`SettingsPage.tsx:56-57`)
**Requirement**: SET-01, SET-07, SET-08, SET-09 (frontend UX)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Loads and displays persisted name/e-mail/logo from `useCompanySettings`
- [ ] Editing + submitting name/e-mail calls `useUpdateCompanySettings` with only those two fields
- [ ] Selecting a logo file triggers the upload mutation immediately, independent of the name/e-mail form state
- [ ] Upload/validation failure (422/500 from MSW) surfaces the existing inline error UI
- [ ] Gate check passes: `cd web && npm run test`
- [ ] Test count: 4+ new tests pass (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(web): wire SettingsPage to real company settings + logo upload`

---

### T14: `public-status/hooks.ts` - drop `mockData.companySettings`

**What**: Remove the `mockData.companySettings` import and its two usages (`hooks.ts:69-70`); read `company_name`/`logo_url` from the API response's new `company` field instead.
**Where**: `web/src/features/public-status/hooks.ts` (modify), `web/src/features/public-status/hooks.test.ts` (new)
**Depends on**: T11
**Reuses**: existing `usePublicStatusPage` structure; MSW `public-preview` handler updated in T11
**Requirement**: SET-15, SET-16

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `usePublicStatusPage` returns `company_name`/`logo_url` sourced from the MSW-mocked API response, not `mockData`
- [ ] `logo_url: null` case renders/returns `null`, not a fabricated placeholder (SET-16)
- [ ] No import of `mockData` (or `companySettings` specifically) remains in `public-status/hooks.ts`
- [ ] Gate check passes: `cd web && npm run test`
- [ ] Test count: 2+ new tests pass (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(web): read real company identity in public status hook`

---

## Phase Execution Map

This is the authoritative dependency graph - every edge below matches a task's `Depends on` field exactly (see the Diagram-Definition Cross-Check table):

```
T1 -> T2
T2 -> T5
T2 -> T10
T4 -> T6
T5 -> T6 -> T8
T5 -> T8
T7 -> T8
T7 -> T9
T11 -> T12 -> T13
T11 -> T14
```

Execution is strictly sequential within and across phases in task-number order (T1..T14) - there is no intra-phase parallelism. A single agent (or batch worker) works one task at a time, in order. T3 has no dependents and no dependencies (isolated node, runs in its Phase 1 slot); the graph above only shows edges that exist.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: Company settings migration | 1 migration pair + 1 test file | ✅ Granular |
| T2: CompanySettingsRepository | 1 repository (3 methods, 1 file) | ✅ Granular |
| T3: `UPLOADS_DIR` config | 1 config field | ✅ Granular |
| T4: `internal/uploads.Save` | 1 function | ✅ Granular |
| T5: `CompanySettingsHandler.Get`/`Update` | 1 handler, 2 closely-related endpoints on the same resource | ✅ Granular (cohesive - same struct, same file) |
| T6: `CompanySettingsHandler.UploadLogo` | 1 handler method on the struct T5 created | ✅ Granular |
| T7: `logoFileHandler` | 1 handler function | ✅ Granular |
| T8: Wire admin routes | 1 file, route registration only | ✅ Granular |
| T9: Dual-mount `/uploads/` on public listener | 1 file, wiring only | ✅ Granular |
| T10: Public status response includes company identity | 1 shared code path (`composeResponse`) + its 2 call sites | ✅ Granular (single cohesive change, not "implement a feature") |
| T11: MSW handlers for company settings | 1 file (fixture/mock layer) | ✅ Granular |
| T12: `settings/hooks.ts` update | 1 file, 1 concept (hooks for one resource) | ✅ Granular |
| T13: `SettingsPage.tsx` rewrite | 1 component | ✅ Granular |
| T14: `public-status/hooks.ts` update | 1 file, 1 function | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | No incoming edge | ✅ Match |
| T2 | T1 | T1 -> T2 | ✅ Match |
| T3 | None | No incoming edge (isolated node) | ✅ Match |
| T4 | None | No incoming edge | ✅ Match |
| T5 | T2 | T2 -> T5 | ✅ Match |
| T6 | T4, T5 | T4 -> T6, T5 -> T6 | ✅ Match |
| T7 | None | No incoming edge | ✅ Match |
| T8 | T5, T6, T7 | T5 -> T6 -> T8 (T6 -> T8), T7 -> T8 | ✅ Match |
| T9 | T7 | T7 -> T9 | ✅ Match |
| T10 | T2 | T2 -> T10 | ✅ Match |
| T11 | None | No incoming edge | ✅ Match |
| T12 | T11 | T11 -> T12 | ✅ Match |
| T13 | T12 | T11 -> T12 -> T13 (T12 -> T13) | ✅ Match |
| T14 | T11 | T11 -> T14 | ✅ Match |

Every edge in the `## Phase Execution Map` graph corresponds to exactly one `Depends on` entry above, and vice versa. No task depends on a later-phase task - all dependencies point backward or within the same phase.

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1: Migration | Go migration | integration | integration | ✅ OK |
| T2: Repository | Go repository | integration | integration | ✅ OK |
| T3: Config | Go config | unit | unit | ✅ OK |
| T4: Uploads store | Go `internal/uploads` | unit | unit | ✅ OK |
| T5: Handler Get/Update | Go handler | integration | integration | ✅ OK |
| T6: Handler UploadLogo | Go handler | integration | integration | ✅ OK |
| T7: logoFileHandler | Go handler | integration | integration | ✅ OK |
| T8: Wire admin routes | Go routing wiring | integration | integration | ✅ OK |
| T9: Dual-mount public listener | Go routing wiring | integration | integration | ✅ OK |
| T10: Public status enrichment | Go public status handler | integration | integration | ✅ OK |
| T11: MSW handlers | MSW fixture code | none | none | ✅ OK |
| T12: settings hooks | React hooks | unit | unit | ✅ OK |
| T13: SettingsPage | React page/component | unit/component | unit | ✅ OK |
| T14: public-status hooks | React hooks | unit | unit | ✅ OK |

No violations - every task's `Tests` field matches its layer's matrix requirement.

---

## Tips

- **Phases are ordered** - Each phase completes before the next; tasks run in order within a phase
- **Reuses = Token saver** - Always reference existing code
- **One commit per task** - commit messages listed above
