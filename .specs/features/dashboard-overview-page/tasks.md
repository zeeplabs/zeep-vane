# Dashboard Overview Page Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/dashboard-overview-page/design.md`
**Status**: Approved (Execute paused — see `.specs/STATE.md` Handoff)

---

## Test Coverage Matrix

> Generated from codebase (`AGENTS.md` §3/§5, sampled `internal/db/incident_repository_test.go`, `internal/api/public_status_handler_test.go`, `web/src/features/poller/hooks.test.ts`, `web/src/layout/Sidebar.test.tsx`, `web/src/App.test.tsx`) and this feature's spec ACs. Guidelines found: `AGENTS.md` §3 (backend gates, integration-test DB rule, `Page[T]` scope), §5 (frontend gates, i18n via `react-i18next`, MSW mirrors backend shape).

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Repository (`IncidentRepository.CountOpen`, `DomainRepository.CountVerified`) | integration | Happy path (some open/verified rows) + zero-rows edge case each | `internal/db/incident_repository_test.go`, `internal/db/domain_repository_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...` |
| API handler (`OverviewHandler.Get`) | integration | All 9 P1 ACs (OVW-01..09): happy path with real data, zero-tenant empty state (AC9), 30d "—" undefined case (AC3), exactly-14-buckets shape (AC7), max-3-incidents ordering (AC8) | `internal/api/overview_handler_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...` |
| Route wiring (`GET /api/overview` in real `buildAdminRouter`) | integration | Mounted on the real production router (not a bare handler call - `admin-dashboard`'s lesson: unmounted routes are dead RBAC), `anyRole` allows viewer, 401 without session | `internal/cli/routes_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=... go test -tags=integration ./internal/cli/...` |
| Frontend types (`OverviewResponse`, `OverviewUptimeBucket`, `OverviewIncident`) | none | Build gate only - no branching logic to unit-test | `web/src/types/api.ts` | `npx tsc -b --noEmit` |
| Frontend hook (`useOverview`) + MSW default handler | unit | Happy path shape (all 6 response fields present); error path (500 → hook surfaces error state) | `web/src/features/overview/hooks.test.ts` | `npm run test` |
| Frontend page (`OverviewPage.tsx`) | unit (React Testing Library) | Render per spec ACs: 4 cards with real values (OVW-03..06), 14-bar chart (OVW-07), ≤3 incidents list + "Ver todos" link (OVW-08), 4 shortcut links navigate correctly (OVW-10..13), no upsell banner / no activity card (Out of Scope), empty-state rendering (OVW-09) | `web/src/features/overview/OverviewPage.test.tsx` | `npm run test` |
| Routing (`App.tsx` - `/overview` route + `RootRoute` target change) | unit | `/` renders Overview for an authenticated user with a resolved tenant instead of redirecting to `/domains`; direct `/overview` visit also renders it; existing RootRoute public-status-page and unauthenticated-login tests stay green | `web/src/App.test.tsx` | `npm run test` |
| Sidebar (`Sidebar.tsx` - standalone "Visão geral" nav item) | unit | Renders above the 3 existing nav groups; active state on `/` and `/overview` | `web/src/layout/Sidebar.test.tsx` | `npm run test` |

## Gate Check Commands

> Generated from `AGENTS.md` §3/§5 - confirm before Execute.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Backend full (DB-touching) | After T1, T2, T3, T4 | Spin disposable Postgres per `AGENTS.md` §3, then `TEST_DATABASE_URL=... go test -tags=integration ./...`, then `go build/vet ./... && gofmt -l <changed files>`, then destroy the container |
| Frontend build | After every frontend task | `npx tsc -b --noEmit` |
| Frontend test | After every frontend task with `Tests` other than `none` | `npm run test` |
| Phase completion | End of every phase | Backend full (if the phase touched Go) + Frontend build + Frontend test, all green |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Backend counts

```
T1
T2
```

### Phase 2: Backend aggregation endpoint

```
T1 → T3 → T4
T2 → T3
```

### Phase 3: Frontend data layer

```
T4 → T5 → T6
```

### Phase 4: Frontend UI

```
T6 → T7
```

### Phase 5: Routing integration

```
T7 → T8 → T9
```

---

## Task Breakdown

### T1: `IncidentRepository.CountOpen`

**What**: New method `CountOpen(ctx) (int, error)` — `SELECT COUNT(*) FROM incidents WHERE status <> 'resolved'`.
**Where**: `internal/db/incident_repository.go`
**Depends on**: None
**Reuses**: Same shape as `countIncidents` (`internal/db/incident_repository.go:197`) and `CountOpenedResolvedBetween` (line 210) — different WHERE clause only.
**Requirement**: OVW-04

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `CountOpen` returns the count of incidents whose `status <> 'resolved'`
- [x] Zero-rows tenant returns `0`, not an error
- [x] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...`
- [x] Test count: +2 new subtests (some open + some resolved mixed; zero incidents)

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add IncidentRepository.CountOpen`

---

### T2: `DomainRepository.CountVerified`

**What**: New method `CountVerified(ctx) (int, error)` — `SELECT COUNT(*) FROM domains WHERE status = 'verified'`.
**Where**: `internal/db/domain_repository.go`
**Depends on**: None
**Reuses**: Same shape as `countDomains` (`internal/db/domain_repository.go:191`) — different WHERE clause only.
**Requirement**: OVW-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `CountVerified` returns the count of domains whose `status = 'verified'`
- [x] Zero-rows tenant returns `0`, not an error
- [x] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...`
- [x] Test count: +2 new subtests (mixed pending/verified/error; zero domains)

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add DomainRepository.CountVerified`

---

### T3: `OverviewHandler.Get` — aggregation endpoint

**What**: New file defining `OverviewResponse`/`OverviewUptimeBucket`/`OverviewIncident` and `OverviewHandler.Get(w, r)`: fetches `ServiceRepository.List`, `StatusIntervalRepository.ListOverlapping` (30d window), groups intervals by service (same pattern as `public_status_handler.go:260-289`), computes the 30d average via `history.UptimePercent` per service (AC3), computes 14 local-midnight-aligned daily buckets the same way per day (AC7), counts unhealthy services in Go (AC5), calls `IncidentRepository.CountOpen` (AC4) and `ListPaginated(ctx, 1, 3)` (AC8), calls `DomainRepository.CountVerified` (AC6). Returns generic 500 on any repository error (no `err.Error()` leak, `AGENTS.md` §4).
**Where**: `internal/api/overview_handler.go` (new)
**Depends on**: T1, T2
**Reuses**: `history.UptimePercent`, the interval-grouping loop from `public_status_handler.go`, `IncidentRepository.ListPaginated`.
**Requirement**: OVW-02, OVW-03, OVW-04, OVW-05, OVW-06, OVW-07, OVW-08, OVW-09

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Response includes all 6 fields per design's `OverviewResponse` shape
- [x] 30d uptime average is `nil` when zero services have `ok=true` (AC3)
- [x] Exactly 14 buckets returned, oldest first, local-midnight-aligned (AC7)
- [x] Recent incidents capped at 3, ordered `created_at DESC`, resolved incidents included (AC8)
- [x] Zero services/incidents/domains renders documented empty values, no 500 (AC9)
- [x] Any repository error returns a fixed generic message, real error only logged server-side
- [x] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [x] Test count: +6 new tests (happy path full data; zero-tenant empty state; 30d "—" case; 14-bucket count/order assertion; recent-incidents cap+order; repository-error → generic 500)

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): add GET /api/overview aggregation endpoint`

---

### T4: Wire `GET /api/overview` into the production router

**What**: Register the route in `buildAdminRouter` under the existing `anyRole` group (same gate as `/api/domains`/`/api/services`).
**Where**: `internal/cli/routes.go`
**Depends on**: T3
**Reuses**: `protected.With(anyRole).Get(...)` pattern already used for `/api/domains`/`/api/services` (`internal/cli/routes.go:241-242`).
**Requirement**: OVW-02

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `GET /api/overview` reachable on the real mounted router with a viewer-role session (per `admin-dashboard`'s lesson that a handler existing without router wiring is dead code)
- [x] Unauthenticated request returns 401
- [x] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/cli/...`
- [x] Test count: +2 new tests (viewer-role 200 against real router; unauthenticated 401)

**Tests**: integration
**Gate**: full

**Commit**: `feat(cli): mount GET /api/overview on the admin router`

---

### T5: Frontend `OverviewResponse` types

**What**: Add `OverviewResponse`, `OverviewUptimeBucket`, `OverviewIncident` interfaces per design's frontend mirror.
**Where**: `web/src/types/api.ts`
**Depends on**: T4
**Reuses**: Existing interface style in the same file (additive only, no changes to existing types).
**Requirement**: OVW-02

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] All 3 interfaces exported matching the backend JSON contract field-for-field
- [x] Gate check passes: `npx tsc -b --noEmit`

**Tests**: none
**Gate**: build

**Commit**: `feat(web): add OverviewResponse frontend types`

---

### T6: `useOverview` hook + MSW default handler

**What**: `useOverview()` hook (`useQuery({queryKey: ["overview"], queryFn: () => apiFetch<OverviewResponse>("/api/overview")})`); add the matching default `http.get("/api/overview", ...)` handler to `web/src/test/msw/handlers.ts` returning a realistic seeded `OverviewResponse` (not a `Page<T>` envelope, per design).
**Where**: `web/src/features/overview/hooks.ts` (new), `web/src/test/msw/handlers.ts` (modify)
**Depends on**: T5
**Reuses**: Exact hook shape as `web/src/lib/branding.ts:17`; MSW handler conventions already in `handlers.ts` for non-paginated endpoints (e.g. `/api/instance/branding`).
**Requirement**: OVW-02

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `useOverview` returns the seeded MSW response shape with all 6 fields present
- [x] A `server.use` override returning 500 surfaces `isError` on the hook
- [x] Gate check passes: `npm run test`
- [x] Test count: +2 new tests (happy path shape; error path)

**Tests**: unit
**Gate**: build+test

**Commit**: `feat(web): add useOverview hook and MSW handler`

---

### T7: `OverviewPage.tsx`

**What**: Page component rendering the 4 summary cards (uptime médio 30d, incidentes abertos, serviços com problema, domínios verificados), the 14-bar uptime chart (accessible tooltip on hover/focus, same interaction contract as `PublicStatusPage.tsx`'s bars), the recent-incidents list (≤3 rows + "Ver todos" link to `/incidents`), and the 4-link "Atalhos rápidos" grid (`/services`, `/domains`, `/admins`, `/domains`) — no upsell banner, no activity-feed card (Out of Scope). Add the required `pt-BR`/`en` i18n keys for every string on this page (`react-i18next`, `AGENTS.md` §5 — no hardcoded strings).
**Where**: `web/src/features/overview/OverviewPage.tsx` (new), `web/src/lib/i18n.ts` (modify)
**Depends on**: T6
**Reuses**: `Card.tsx` for every card/section container; `useOverview`.
**Requirement**: OVW-03, OVW-04, OVW-05, OVW-06, OVW-07, OVW-08, OVW-09, OVW-10, OVW-11, OVW-12, OVW-13, OVW-14

**Tools**:
- MCP: NONE
- Skill: `frontend-design` (adopted convention for every shell/screen component in this feature line per prior `new-layout-migration` tasks)

**Done when**:
- [ ] 4 cards render real hook values, including the AC3 "—" case and AC9's `0` cases
- [ ] Chart renders exactly 14 bars with hover/focus tooltips; no-data buckets render distinctly (no fabricated value)
- [ ] Incidents list renders ≤3 rows + "Ver todos" link; empty state when zero incidents
- [ ] All 4 shortcut links resolve to their documented routes (OVW-10..13)
- [ ] No upsell banner, no activity-feed card anywhere in the rendered output
- [ ] Gate check passes: `npx tsc -b --noEmit && npm run test`
- [ ] Test count: +7 new tests (4 cards' values incl. empty states; chart bucket count+tooltip; incidents list incl. empty state; each of the 4 shortcut links' target)

**Tests**: unit
**Gate**: build+test

**Commit**: `feat(web): add OverviewPage`

---

### T8: `App.tsx` — `/overview` route + `RootRoute` redirect change

**What**: Add `<Route path="/overview" element={<OverviewPage />} />` under the existing `AuthenticatedLayout` group; change `RootRoute`'s authenticated branch from `<Navigate to="/domains" replace />` to `<OverviewPage />` directly (no extra redirect hop, per design's Integration Points).
**Where**: `web/src/App.tsx`
**Depends on**: T7
**Reuses**: Existing `AuthenticatedLayout`/`RequireAuth`/`RedirectToBootstrapIfNeeded` wrapping, unchanged.
**Requirement**: OVW-01

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Authenticated user with a resolved tenant loading `/` sees the Overview page, not `/domains`
- [ ] Direct visit to `/overview` also renders the Overview page
- [ ] Every existing `RootRoute` test (public-status-page 200/404 branches) still passes unmodified
- [ ] Gate check passes: `npx tsc -b --noEmit && npm run test`
- [ ] Test count: +2 new tests (authenticated `/` renders Overview; direct `/overview` renders Overview) — 0 existing tests modified beyond what's needed to keep them passing

**Tests**: unit
**Gate**: build+test

**Commit**: `feat(web): route "/" and "/overview" to OverviewPage`

---

### T9: Sidebar "Visão geral" nav item

**What**: Add a standalone `NavLink to="/overview"` above the 3 existing nav groups (dashboard-grid icon per mock, no group label — mirrors `README.md` line 38's "always first" placement).
**Where**: `web/src/layout/Sidebar.tsx`
**Depends on**: T8
**Reuses**: Existing `navItemClass`/`NavLink` pattern already used by every other sidebar entry (`web/src/layout/Sidebar.tsx:149` etc).
**Requirement**: OVW-15, OVW-16

**Tools**:
- MCP: NONE
- Skill: `frontend-design` (same convention as T7)

**Done when**:
- [ ] "Visão geral" renders as the first item, above all 3 nav groups
- [ ] Active state applies on both `/` and `/overview`
- [ ] Gate check passes: `npx tsc -b --noEmit && npm run test`
- [ ] Test count: +2 new tests (renders first, above groups; active-state assertion on both routes)

**Tests**: unit
**Gate**: build+test

**Commit**: `feat(web): add Visão geral sidebar nav item`

---

## Phase Execution Map

```
Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5

Phase 1:  T1
          T2
Phase 2:  T3 ------→ T4
Phase 3:  T5 ------→ T6
Phase 4:  T7
Phase 5:  T8 ------→ T9
```

Execution is strictly sequential - there is no intra-phase parallelism. A single agent (or batch worker) works one task at a time, in order.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: `IncidentRepository.CountOpen` | 1 method | ✅ Granular |
| T2: `DomainRepository.CountVerified` | 1 method | ✅ Granular |
| T3: `OverviewHandler.Get` | 1 endpoint (1 new file) | ✅ Granular |
| T4: Wire route | 1 route registration | ✅ Granular |
| T5: Frontend types | 1 file, additive types only | ✅ Granular |
| T6: `useOverview` + MSW handler | 1 hook + 1 handler, same feature slice | ✅ Granular (cohesive pair — hook is untestable without its own MSW handler, merged per Tasks §"Resolving compilation dependencies") |
| T7: `OverviewPage.tsx` | 1 component (+ its i18n keys, same reasoning as T6) | ✅ Granular (i18n keys merged forward — component's own tests need them to exist) |
| T8: `App.tsx` routing change | 1 file | ✅ Granular |
| T9: Sidebar nav item | 1 file | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | None | ✅ Match |
| T2 | None | None | ✅ Match |
| T3 | T1, T2 | T1→T3, T2→T3 | ✅ Match |
| T4 | T3 | T3→T4 | ✅ Match |
| T5 | T4 | T4→T5 | ✅ Match |
| T6 | T5 | T5→T6 | ✅ Match |
| T7 | T6 | T6→T7 | ✅ Match |
| T8 | T7 | T7→T8 | ✅ Match |
| T9 | T8 | T8→T9 | ✅ Match |

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1: `CountOpen` | Repository | integration | integration | ✅ OK |
| T2: `CountVerified` | Repository | integration | integration | ✅ OK |
| T3: `OverviewHandler.Get` | API handler | integration | integration | ✅ OK |
| T4: Route wiring | Route wiring | integration | integration | ✅ OK |
| T5: Frontend types | Entity/config | none | none | ✅ OK |
| T6: `useOverview` + MSW | Frontend hook | unit | unit | ✅ OK |
| T7: `OverviewPage.tsx` | Frontend page | unit | unit | ✅ OK |
| T8: `App.tsx` | Routing | unit | unit | ✅ OK |
| T9: `Sidebar.tsx` | Sidebar | unit | unit | ✅ OK |

---

## Tips (n/a - implementation phase)
