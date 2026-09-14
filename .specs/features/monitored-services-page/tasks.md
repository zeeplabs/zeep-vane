# Monitored Services Page Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/monitored-services-page/design.md`
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase sampling. Guidelines found: `AGENTS.md` §3 (backend gate: `go build`/`go test`/`go vet`/`gofmt`, integration gate via `make test-integration` for DB-touching code; frontend gate: `npx tsc -b --noEmit`/`npm run test`), §5 (frontend MSW must mirror real response shape, `Pager` for every paginated list). Existing test samples: `internal/db/service_status_analysis_repository_test.go` + `..._migration_test.go` (repo/migration, `//go:build integration`), `internal/api/services_handler_test.go` + `overview_handler_test.go` (handler, also `//go:build integration` - this codebase tests handlers against a real tenant-scoped Postgres, not mocks), `web/src/features/services/hooks.test.ts`, `web/src/features/overview/OverviewPage.test.tsx` (frontend, vitest + MSW).

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Migration (`0033_service_slo_name`) | integration | Applies clean, reverses clean, column present/absent verified directly | `internal/db/*_migration_test.go` | `make test-integration` |
| Repository (`ServiceRepository`, `IncidentRepository`) | integration | Every new/changed method's key paths + not-found/zero-row cases | `internal/db/*_repository_test.go` | `make test-integration` |
| Handler (`ServicesHandler`) | integration | Every route in scope: happy path + every listed edge case (404, mixed not_configured/polled page, empty note) + error paths, against real tenant-scoped Postgres | `internal/api/services_handler_test.go` | `make test-integration` |
| Frontend hooks (`services/hooks.ts`) | unit | 1:1 to spec ACs touching data-fetch/shape (SVC-01, SVC-14, SVC-20..25); MSW handlers mirror the extended `Page<T>`/detail contract exactly (AGENTS.md §5) | `web/src/features/services/hooks.test.ts` | `npm run test` |
| Frontend components (`ServiceListPage`, `ServiceDetailDrawer`, `AddServiceDrawer`) | unit | Every spec AC for that story (P1/P2/P3 drawer/P3 add) + every listed edge case | `web/src/features/services/*.test.tsx` | `npm run test` |
| Shared status-meta constants (`statusMeta.ts`) | none | Exercised transitively by the components/sections that import it | `web/src/features/services/statusMeta.ts` | build gate only |

## Gate Check Commands

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick (frontend) | After a frontend-only task (hooks, components, shared constants) | `cd web && npx tsc -b --noEmit && npm run test` |
| Full (backend) | After any task touching migration/repository/handler | `go build ./... && go vet ./... && gofmt -l $(git diff --name-only --diff-filter=ACM -- '*.go') && make test-integration` |
| Build (whole-feature) | After the last task in the feature, before Verifier | `go build ./... && go vet ./... && go test ./... && gofmt -l $(git diff --name-only --diff-filter=ACM -- '*.go') && make test-integration && cd web && npx tsc -b --noEmit && npm run test` |

---

## Execution Plan

### Phase 1: Backend data layer

```
T1 → T2 → T3
```

### Phase 2: Backend handlers

```
T3 → T4 → T5
```

### Phase 3: Frontend data layer

```
T5 → T6 → T7
```

### Phase 4: Frontend UI

```
T7 → T8 → T9 → T10 → T11
```

---

## Task Breakdown

### T1: Add `services.slo_name` migration

**What**: New migration `0033_service_slo_name` adding `slo_name TEXT NOT NULL DEFAULT ''` to `services`, plus its down migration and a migration test asserting clean apply/reverse (same shape as `service_status_analysis_migration_test.go`).
**Where**: `internal/db/migrations/0033_service_slo_name.up.sql`, `internal/db/migrations/0033_service_slo_name.down.sql`, `internal/db/service_slo_name_migration_test.go`
**Depends on**: None
**Reuses**: `internal/db/service_status_analysis_migration_test.go` (0023's exact test pattern: migrate up, assert column present with default, migrate down, assert column gone, re-apply cleanly)
**Requirement**: SVC-01 (backing field for `slo_name` in the list response)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `0033_service_slo_name.up.sql` adds the column, `.down.sql` drops it
- [x] Migration test asserts: applies with every existing row defaulting to `''`, reverses cleanly, re-applies cleanly
- [x] Gate check passes: `make test-integration`

**Tests**: integration
**Gate**: Full

---

### T2: Extend `ServiceRepository` with `SLOName` and `Get`

**What**: Add `SLOName string` to the `Service` struct; thread it through `Create`'s `INSERT`/`RETURNING` and `ListPaginated`'s `SELECT`; add a new `Get(ctx, id string) (*Service, bool, error)` single-row lookup (not-found → `(nil, false, nil)`, matching this file's existing not-found convention).
**Where**: `internal/db/service_repository.go`, `internal/db/service_repository_test.go` (or add to `service_status_analysis_repository_test.go` if a dedicated file doesn't already exist for `ServiceRepository`'s own CRUD - check first)
**Depends on**: T1
**Reuses**: existing `Create`/`ListPaginated` scan/error-wrap style in the same file
**Requirement**: SVC-01, SVC-14 (drawer needs a single-service read)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `Create` persists and returns `SLOName`
- [x] `ListPaginated` returns `SLOName` for every row, including rows created before T1 (empty string, not an error)
- [x] `Get` returns the full row including `SLOName`, `CurrentStatus`, `StatusAnalysis`; returns `found=false` (no error) for an unknown ID
- [x] Integration tests cover: create-then-read round-trip of `SLOName`, `Get` found + not-found, `ListPaginated` with a mix of pre- and post-migration rows
- [x] Gate check passes: `make test-integration`

**Tests**: integration
**Gate**: Full

---

### T3: Add `IncidentRepository.CountByServiceSince`

**What**: New method `CountByServiceSince(ctx, serviceID string, since time.Time) (int, error)` - counts `incidents` joined through `incident_services` where `service_id = $1 AND created_at >= $2`, any status.
**Where**: `internal/db/incident_repository.go`, `internal/db/incident_repository_test.go`
**Depends on**: T2 (no code dependency - different table - but stays in strict execution order per "whole phase" packing; Foundation-layer work with no UI dependency)
**Reuses**: `CountOpenedResolvedBetween`'s `COUNT(*) FILTER` idiom (`internal/db/incident_repository.go:244`), adapted with the `incident_services` join
**Requirement**: SVC-14 ("Incidentes (30d)" drawer stat)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Returns the correct count for a service with 0, 1, and 3+ incidents in the window
- [x] Excludes incidents outside the window (`created_at < since`)
- [x] Excludes incidents linked to a *different* service
- [x] Gate check passes: `make test-integration`

**Tests**: integration
**Gate**: Full

---

### T4: Extend `ServicesHandler.List`/`Create` with uptime/last-seen/slo_name

**What**: `List` batch-fetches `StatusIntervalRepository.ListOverlapping(serviceIDs, now-30d, now)` for the page's services, buckets by service ID, and adds `uptime_30d *float64` (via `history.UptimePercent`, nil when no data) and `last_seen_at *time.Time` (from the service's open interval, nil when never polled) to each row's response, alongside the now-present `slo_name`. `Create`'s request body gains `slo_name string`, persisted via T2's extended `Create`.
**Where**: `internal/api/services_handler.go`, `internal/api/services_handler_test.go`
**Depends on**: T3
**Reuses**: `OverviewHandler`'s exact batch-interval pattern (`internal/api/overview_handler.go:129-142`), `history.UptimePercent`
**Requirement**: SVC-01, SVC-06, SVC-20..25 (create request contract)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `List` response includes `slo_name`, `uptime_30d`, `last_seen_at` per item
- [ ] A `not_configured` service (zero `StatusInterval` rows) gets `uptime_30d: null`, `last_seen_at: null` in the same page as a polled service that gets real numbers (mixed-fixture test, per design.md Risks & Concerns row 3 - this is the explicit regression this task must catch)
- [ ] `Create` accepts and persists `slo_name`; omitting it still 422s the same way an omitted `slo_id`/`name` already does (no relaxation of existing validation)
- [ ] Gate check passes: `make test-integration`

**Tests**: integration
**Gate**: Full

---

### T5: Add `ServicesHandler.Get` (detail endpoint) + route wiring

**What**: New `GET /api/services/{id}` handler: loads the service (404 fixed-body if not found), computes `uptime_30d`/`last_seen_at` (same as T4, scoped to one service), `incidents_30d` via T3's `CountByServiceSince(id, now-30d)`, and `hourly_buckets` via a second `ListOverlapping(id, now-24h, now)` + `history.BuildBuckets(..., 24, time.Hour)`; passes `status_analysis` through unchanged. Wires the route in `routes.go` under `anyRole` (same role as `List`). Wiring is included in this task, not deferred, per the tasks-process rule against untestable code crossing a task boundary.
**Where**: `internal/api/services_handler.go`, `internal/cli/routes.go`, `internal/api/services_handler_test.go`
**Depends on**: T4
**Reuses**: `NewOverviewHandler`'s load-once-panic-on-failure `time.Location` pattern; `history.BuildBuckets`
**Requirement**: SVC-14..19

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `GET /api/services/{id}` returns the full detail DTO (design.md `ServiceDetail`) for an existing service
- [ ] Returns 404 (fixed generic body, no `err.Error()` leak) for an unknown ID
- [ ] `hourly_buckets` always has exactly 24 entries regardless of how much history the service has (including zero)
- [ ] `status_analysis` is present when `current_status == "degraded"` with a stored analysis, and `null` otherwise (both branches tested, not just the happy one - L-048)
- [ ] `incidents_30d` reflects T3's count for that service
- [ ] Route requires authentication (any role); unauthenticated request gets the existing `RequireAuth` 401
- [ ] Gate check passes: `make test-integration`

**Tests**: integration
**Gate**: Full

---

### T6: Hoist shared service status metadata

**What**: Extract the `statusLabel`/`statusVariant`/`statusDotColor` maps (today inline in `ServicesSection.tsx:16-32`) into `web/src/features/services/statusMeta.ts`, covering all 4 `ServiceStatus` values including `not_configured`; update `ServicesSection.tsx` to import from there instead of its local copy (no behavior change).
**Where**: `web/src/features/services/statusMeta.ts` (new), `web/src/features/services/ServicesSection.tsx` (modify import only)
**Depends on**: T5
**Reuses**: the exact existing map values in `ServicesSection.tsx:16-32`
**Requirement**: SVC-02..05 (single source of truth for the 4-state badge, used by both the old section and the new page)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `statusMeta.ts` exports the 3 maps, one entry per `ServiceStatus` value (4 total each)
- [ ] `ServicesSection.tsx` imports from `statusMeta.ts`; its existing test suite still passes unmodified (pure refactor, zero visible change)
- [ ] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test`

**Tests**: none (pure extraction; exercised transitively by `ServicesSection`'s existing tests)
**Gate**: Quick

---

### T7: Extend `services/hooks.ts` + MSW handlers

**What**: `ServiceResponse` gains `slo_name: string`, `uptime_30d: number | null`, `last_seen_at: string | null`; `toService` becomes synchronous (drops the `fetchSLOName` live call entirely, falls back to `slo_id` display when `slo_name === ""`); `CreateServiceInput` gains `slo_name`; add `useServiceDetail(id)` hook (`GET /api/services/{id}`). Update `web/src/test/msw/handlers.ts` to mirror the extended `List`/`Create` contract and add a detail-endpoint handler, per AGENTS.md §5.
**Where**: `web/src/features/services/hooks.ts`, `web/src/features/services/hooks.test.ts`, `web/src/test/msw/handlers.ts`
**Depends on**: T6
**Reuses**: existing query-key/mutation-invalidation conventions in the same file
**Requirement**: SVC-01, SVC-06, SVC-14, SVC-20..25

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `useServices` returns `slo_name`/`uptime_30d`/`last_seen_at` per item, no network call beyond the single list request (the old per-row `fetchSLOName` call is gone - assert this via a mock call-count check, not just output correctness)
- [ ] `useServiceDetail` fetches and returns the detail shape, including all 24 `hourly_buckets`
- [ ] `useCreateService` sends `slo_name` in its request body
- [ ] MSW handlers return the exact `Page<T>`/detail envelope shape the real backend now returns (AGENTS.md §5)
- [ ] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test`

**Tests**: unit
**Gate**: Quick

---

### T8: Extract `AddServiceDrawer` from `ServicesSection`

**What**: Move the existing inline add-service dialog logic (name field, debounced SLO search, selection, submit validation, error display - `ServicesSection.tsx:59-172`) into a standalone `AddServiceDrawer` component taking `{ open, onOpenChange }`; `ServicesSection` uses it instead of its inline `Dialog` (no visible/behavioral change to `ServicesSection`).
**Where**: `web/src/features/services/AddServiceDrawer.tsx` (new), `web/src/features/services/AddServiceDrawer.test.tsx` (new), `web/src/features/services/ServicesSection.tsx` (modify)
**Depends on**: T7
**Reuses**: the exact extracted logic, `useSLOSearch`, `useCreateService`
**Requirement**: SVC-20..25

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `AddServiceDrawer` renders the name field + SLO search + submit, disabled until a name and a selected SLO are both present (SVC-23)
- [ ] Submitting calls `useCreateService` with `{name, slo_id, slo_name}` and closes on success
- [ ] A failed submit shows an inline error and does not close the drawer (SVC-24)
- [ ] An empty SLO search result shows "Nenhum SLO encontrado" (edge case)
- [ ] No "Polling manual"/"New Relic" option renders anywhere (SVC-25)
- [ ] `ServicesSection`'s existing tests still pass unmodified after the extraction
- [ ] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test`

**Tests**: unit
**Gate**: Quick

---

### T9: Build `ServiceListPage`

**What**: New page component rendering the redesigned table (Status/Serviço+SLO name/Uptime 30d/Última verificação columns), the 5 filter chips (Todos/Operacional/Degradado/Inativo/Não configurado) with page-scoped counts, the search box (name/SLO name substring, case-insensitive), the empty state, and the shared `Pager`. No Latência column anywhere.
**Where**: `web/src/features/services/ServiceListPage.tsx` (new), `web/src/features/services/ServiceListPage.test.tsx` (new)
**Depends on**: T8
**Reuses**: `statusMeta.ts` (T6), `useServices` (T7), `Pager`/`Card`/`Tag`/`Button` from `components/ui`
**Requirement**: SVC-01..13

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Renders every column from SVC-01 for every service on the page, no Latência column/field anywhere (SVC-07)
- [ ] Each `CurrentStatus` value renders its correct badge label (SVC-02..05, all 4 values covered in one test suite - not just `operational`)
- [ ] A service with no open interval shows "—" for Uptime 30d and Última verificação (SVC-06)
- [ ] Status chip click filters to that status only, "Todos" shows all page rows; chip counts reflect the current page (SVC-09, SVC-13)
- [ ] Search narrows by name/SLO-name substring, case-insensitive (SVC-10)
- [ ] Filter + search combine with AND, not OR (SVC-11)
- [ ] No match renders "Nenhum serviço encontrado com esses filtros." (SVC-12)
- [ ] Pager renders with `totalPages = Math.max(1, Math.ceil(total / page_size))` when more than one page exists (SVC-08)
- [ ] All new user-facing strings go through `react-i18next` (AGENTS.md §5), pt-BR key added
- [ ] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test`

**Tests**: unit
**Gate**: Quick

---

### T10: Build `ServiceDetailDrawer`

**What**: Read-only drawer showing Uptime 30d, Última verificação, Incidentes (30d), the conditional degraded-note block, and the 24-bar hourly history strip; no "Pausar monitoramento"/"Editar configuração" controls anywhere.
**Where**: `web/src/features/services/ServiceDetailDrawer.tsx` (new), `web/src/features/services/ServiceDetailDrawer.test.tsx` (new)
**Depends on**: T9
**Reuses**: `useServiceDetail` (T7), `statusMeta.ts` (T6), existing slide-over/`Dialog` primitive in `components/ui`
**Requirement**: SVC-14..19

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Shows Uptime 30d, Última verificação, Incidentes (30d) from the detail response (SVC-14)
- [ ] Renders the note when `current_status === "degraded"` and `status_analysis` is non-empty; does NOT render it otherwise - both branches tested (SVC-15/16, L-048)
- [ ] Renders exactly 24 bars regardless of fixture size, asserted against the literal `24`, not against whatever constant the implementation uses (L-049)
- [ ] Close control and backdrop click both close the drawer (SVC-18)
- [ ] No "Pausar monitoramento"/"Editar configuração" text or control renders (SVC-19)
- [ ] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test`

**Tests**: unit
**Gate**: Quick

---

### T11: Wire `ServiceListPage` into `/services`, connect drawer + add-drawer

**What**: `ServicesPage.tsx` (the `/services` route target) renders `ServiceListPage` instead of `ServicesSection`; row click opens `ServiceDetailDrawer` for that row's ID; "Adicionar serviço" opens `AddServiceDrawer`; on successful add, the list refreshes (existing `useCreateService` invalidation) and the new service is visible.
**Where**: `web/src/features/services/ServicesPage.tsx` (modify), `web/src/features/services/ServicesPage.test.tsx` (modify/extend)
**Depends on**: T10
**Reuses**: `ServiceListPage`, `ServiceDetailDrawer`, `AddServiceDrawer` (T8-T10) - this task is pure composition, no new business logic
**Requirement**: SVC-14, SVC-20 (end-to-end wiring)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `/services` (through the authenticated shell route, not a bare component render - L-050) shows `ServiceListPage`
- [ ] Clicking a row opens `ServiceDetailDrawer` scoped to that service's ID
- [ ] "Adicionar serviço" opens `AddServiceDrawer`; a successful add makes the new service appear in the list without a full page reload
- [ ] Unauthenticated access to `/services` still redirects to `/login` (existing behavior, regression-checked)
- [ ] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test`
- [ ] Whole-feature Build gate passes: `go build ./... && go vet ./... && go test ./... && gofmt -l $(git diff --name-only --diff-filter=ACM -- '*.go') && make test-integration && cd web && npx tsc -b --noEmit && npm run test`

**Tests**: unit
**Gate**: Build

**Commit**: `feat(services): redesign monitored services page with filters, search, and detail drawer`

---

## Phase Execution Map

```
Phase 1 → Phase 2 → Phase 3 → Phase 4

Phase 1:  T1 ------→ T2 ------→ T3
Phase 2:                  T3 ------→ T4 ------→ T5
Phase 3:                                   T5 ------→ T6 ------→ T7
Phase 4:                                                    T7 ------→ T8 ------→ T9 ------→ T10 ------→ T11
```

Execution is strictly sequential - there is no intra-phase parallelism.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: migration + migration test | 1 migration pair + 1 test file | ✅ Granular |
| T2: `ServiceRepository` extend + `Get` | 1 file (+ its test file) | ✅ Granular |
| T3: `IncidentRepository.CountByServiceSince` | 1 method, 1 file | ✅ Granular |
| T4: `ServicesHandler.List`/`Create` extend | 1 file (+ its test file) | ✅ Granular |
| T5: `ServicesHandler.Get` + route wiring | 1 handler + 1 route line - cohesive, wiring can't be split out (would leave Get untestable) | ✅ Granular |
| T6: hoist `statusMeta.ts` | 1 new file + 1 import change | ✅ Granular |
| T7: extend `hooks.ts` + MSW | 1 hooks file + 1 shared MSW file - cohesive (MSW must match hooks in the same task to stay testable) | ✅ Granular |
| T8: extract `AddServiceDrawer` | 1 new component | ✅ Granular |
| T9: `ServiceListPage` | 1 new component | ✅ Granular |
| T10: `ServiceDetailDrawer` | 1 new component | ✅ Granular |
| T11: wire route + compose | 1 file (route composition only, no new logic) | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | (start of Phase 1) | ✅ Match |
| T2 | T1 | T1 → T2 | ✅ Match |
| T3 | T2 | T2 → T3 | ✅ Match |
| T4 | T3 | T3 → T4 | ✅ Match |
| T5 | T4 | T4 → T5 | ✅ Match |
| T6 | T5 | T5 → T6 | ✅ Match |
| T7 | T6 | T6 → T7 | ✅ Match |
| T8 | T7 | T7 → T8 | ✅ Match |
| T9 | T8 | T8 → T9 | ✅ Match |
| T10 | T9 | T9 → T10 | ✅ Match |
| T11 | T10 | T10 → T11 | ✅ Match |

Every task's real logical prerequisites (e.g. T4 also reuses T2/T3's outputs, T7 also mirrors T4/T5's contracts) are satisfied automatically by strict sequential execution - one linear chain T1→T11, no task ever runs before a task whose output it needs. `Depends on` is kept to the immediate predecessor throughout to stay diagram-exact; broader reuse relationships are documented in each task's `Reuses` field instead. No task depends on a task in a later phase.

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1: migration | Migration | integration | integration | ✅ OK |
| T2: `ServiceRepository` | Repository | integration | integration | ✅ OK |
| T3: `IncidentRepository` | Repository | integration | integration | ✅ OK |
| T4: `ServicesHandler.List`/`Create` | Handler | integration | integration | ✅ OK |
| T5: `ServicesHandler.Get` | Handler | integration | integration | ✅ OK |
| T6: `statusMeta.ts` | Entity/config | none | none | ✅ OK |
| T7: `hooks.ts` | Frontend hooks | unit | unit | ✅ OK |
| T8: `AddServiceDrawer` | Frontend component | unit | unit | ✅ OK |
| T9: `ServiceListPage` | Frontend component | unit | unit | ✅ OK |
| T10: `ServiceDetailDrawer` | Frontend component | unit | unit | ✅ OK |
| T11: route wiring | Frontend component (composition) | unit | unit (+ Build gate as whole-feature closeout) | ✅ OK |

No violations.

---

## Tips

(n/a - execution-facing template section, no additional content needed)
