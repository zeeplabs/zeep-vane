# List Pagination Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/list-pagination/design.md`
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase, project guidelines, and spec - confirm before Execute. Guidelines found: none dedicated (no `AGENTS.md`/`CONTRIBUTING.md`); inferred from `README.md` (test-running section, lines 250-261) and 10 sampled existing test files (`internal/db/status_page_repository_test.go`, `internal/api/incidents_handler_test.go`, `internal/api/admins_test.go`, `internal/db/email_provider_repository_test.go`, `internal/api/poller_status_test.go`, `internal/api/public_status_handler_test.go`, `web/src/features/incidents/hooks.test.ts`, `web/src/layout/Sidebar.test.tsx`, `web/src/features/integrations/IntegrationsPage.test.tsx`, `web/src/components/ui/Table.test.tsx`).

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| ---------- | ------------------- | --------------------- | ----------------- | ------------- |
| Go repository (`internal/db/*.go`) | integration | Every `ListPaginated`/`ListUpdatesPaginated` method: page 1 partial, page N exact boundary, page beyond last (empty `items` + correct `total`), zero-row table, `ORDER BY` unchanged. Real Postgres via `internal/dbtest`, build tag `integration` | `internal/db/*_repository_test.go` | `TEST_DATABASE_URL="postgres://vane:vane@localhost:5432/vane?sslmode=disable" go test -tags=integration ./internal/db/...` |
| Go handler (`internal/api/*.go`) | integration | Every changed handler: default page (no `?page=`), explicit `?page=2`, invalid `?page=` (clamped to 1, `200`), envelope shape (`items,total,page,page_size`), existing role-gate tests updated for new response shape | `internal/api/*_handler_test.go`, `internal/api/admins_test.go` | `TEST_DATABASE_URL="postgres://vane:vane@localhost:5432/vane?sslmode=disable" go test -tags=integration ./internal/api/...` |
| `internal/api/pagination.go` (new shared helper) | unit | `parsePage`: missing, `"0"`, negative, non-numeric, valid — all branches | `internal/api/pagination_test.go` | `go test ./internal/api/...` (no build tag - pure unit, no DB) |
| Frontend hook (`web/src/features/*/hooks.ts`) | unit (MSW) | Query key includes `page`; `queryFn` requests the right URL; returns full `Page<T>` envelope | `web/src/features/*/hooks.test.ts` | `cd web && npx vitest run` |
| Frontend component (`Pager.tsx`, list pages, `PublicStatusPage.tsx`) | unit (Testing Library) | `Pager`: disables "Anterior" on page 1, "Próximo" on last page, calls `onChange` with correct page. List pages: renders `Pager`, page navigation re-fetches. Public page: "Carregar mais" appends, disappears when exhausted | `web/src/components/ui/Pager.test.tsx`, `web/src/features/*/*.test.tsx` | `cd web && npx vitest run` |
| Type-only changes (`web/src/types/api.ts`) | none | build gate only | - | `cd web && npx tsc --noEmit` |

**Coverage Expectation source**: no dedicated project testing guideline file exists; matrix follows the strong default (domain/business logic 1:1 to spec ACs, routes cover happy+edge+error) cross-checked against the depth already present in the 10 sampled files (e.g. `admins_test.go`'s existing role-gate + merge tests, `public_status_handler_test.go`'s retention-boundary tests).

## Gate Check Commands

> Generated from codebase - confirm before Execute.

| Gate Level | When to Use | Command |
| ---------- | ----------- | ------- |
| Quick (Go unit) | After a Go-only task with no DB-touching test | `go test ./...` |
| Full (Go integration) | After any task touching a repository or handler | `TEST_DATABASE_URL="postgres://vane:vane@localhost:5432/vane?sslmode=disable" go test -tags=integration ./...` |
| Frontend | After any web task | `cd web && npx tsc --noEmit && npx vitest run` |
| Build | After phase completion | `make build` |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Shared Backend Primitive

```
T1
```

### Phase 2: Incidents (P1 - the endpoint that actually grows)

```
T2 → T3 → T4
```

### Phase 3: Remaining Admin Endpoints (P2 - mechanical replication)

Executed in written order (T6, T7, T8, T9, T10, T11 - one agent, sequential); arrows below show only the real repo-to-handler dependency pairs, not mere writing order:

```
T6 → T7
T8 → T9
T10 → T11
```

### Phase 4: Public Status Page (P3)

```
T12 → T13
```

### Phase 5: Frontend Pager Component + Admin Screens

Executed in written order (T14, T5, T16, T17, T18, T19, T20); arrows below show only `Pager`'s real fan-out, not mere writing order:

```
T14 → T5
T14 → T16
T14 → T17
T14 → T18
T14 → T19
```

### Cross-Phase Dependencies

Backward dependencies on an earlier, already-complete phase (allowed per the skill's rule: "dependencies point backward or within the same phase only"). Listed separately from the phase blocks above for readability - phases still execute strictly in order, so every arrow below is automatically satisfied by the time its target phase starts.

```
T1 → T3
T1 → T7
T1 → T9
T1 → T11
T2 → T12
T4 → T5
T7 → T16
T9 → T16
T11 → T17
T11 → T18
T11 → T19
T13 → T20
```

---

## Task Breakdown

### T1: Shared `Page[T]` envelope + `parsePage` helper

**What**: New `internal/api/pagination.go` with `type Page[T any] struct{Items,Total,Page,PageSize}` and unexported `parsePage(r *http.Request) int` (missing/invalid/non-positive → `1`)
**Where**: `internal/api/pagination.go`
**Depends on**: None
**Reuses**: none - first shared primitive of this kind
**Requirement**: PAG-02, PAG-03

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `Page[T]` struct with correct JSON tags (`items`,`total`,`page`,`page_size`)
- [x] `parsePage` returns `1` for missing/`"0"`/negative/non-numeric `?page=`, and the parsed value for any positive integer
- [x] Unit tests cover every branch listed above
- [x] Gate check passes: `go test ./internal/api/...`
- [x] Test count: 5+ tests pass

**Tests**: unit
**Gate**: quick

**Commit**: `feat(api): add shared Page[T] envelope and page query-param parser`

---

### T2: `IncidentRepository.ListPaginated` + `ListUpdatesPaginated`

**What**: Replace `List(ctx)` with `ListPaginated(ctx, page, pageSize int) ([]Incident, int, error)` (adds `LIMIT`/`OFFSET` + `COUNT(*) OVER()` with zero-row fallback `SELECT COUNT(*)`); replace `ListUpdates(ctx, id)` with `ListUpdatesPaginated(ctx, id, page, pageSize int) ([]IncidentUpdate, int, error)` (same pattern, scoped by `incident_id`)
**Where**: `internal/db/incident_repository.go`
**Depends on**: None (independent of T1 - pure repository change)
**Reuses**: existing `scanIncidentRows`, `listServiceIDs`, `mustExist` helpers unchanged
**Requirement**: PAG-01, PAG-04, PAG-05, PAG-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `ListPaginated`: page 1 of >25 incidents returns exactly 25, correct `total`; page 2 returns the remainder; page beyond last returns `[]` + correct `total` (via fallback `COUNT(*)`); `ORDER BY created_at DESC` unchanged
- [x] `ListUpdatesPaginated`: same boundary tests scoped to one incident with >25 updates; still 404s via existing `mustExist` for an unknown incident ID
- [x] Old `List` method removed (confirmed no other caller via grep). `ListUpdates` KEPT, not removed — SPEC_DEVIATION: it has an internal caller (`withTimelinesSplit`, used by `ListPublic`/`ListPublicForStatusPage` for the public status page's per-incident timeline) that design.md missed when it said "single caller" for `ListUpdates`. `ListUpdatesPaginated` added alongside it for the two handler call sites, mirroring the already-documented `ServiceRepository`/`poller.go` precedent. See the code comment in `internal/db/incident_repository.go` above `ListUpdates`.
- [x] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...`
- [x] Test count: 10 new tests pass

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): paginate IncidentRepository List and ListUpdates`

---

### T3: `IncidentsHandler.List` + `ListUpdates` wired to pagination

**What**: `List` calls `parsePage(r)` + `ListPaginated(ctx, page, 25)`, responds `Page[incidentResponse]`; `ListUpdates` calls `parsePage(r)` + `ListUpdatesPaginated(ctx, id, page, 25)`, responds `Page[incidentUpdateResponse]`; `AddUpdate`'s post-submit re-fetch switches to `ListUpdatesPaginated(ctx, id, 1, 25)`
**Where**: `internal/api/incidents_handler.go`
**Depends on**: T1, T2
**Reuses**: T1's `parsePage`/`Page[T]`
**Requirement**: PAG-01, PAG-02, PAG-03, PAG-04, PAG-05

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `GET /api/incidents` (no `?page=`) returns page 1, envelope shape `{items,total,page,page_size}`
- [x] `GET /api/incidents?page=2` returns the second page
- [x] `GET /api/incidents?page=abc` (or `0`, or `-1`) clamps to page 1, `200`
- [x] `GET /api/incidents/{id}/updates?page=N` returns the same envelope shape
- [x] Existing role-gate tests (`TestListIncidents_*`, `TestListIncidentUpdates_*`) updated for the new response shape, still passing
- [x] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [x] Test count: existing incidents handler tests (12) still pass + 6 new pagination tests

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): wire incidents and incident-updates endpoints to pagination`

---

### T4: `useIncidents`/`useIncidentUpdates` hooks accept `page`

**What**: `useIncidents(page: number)` and `useIncidentUpdates(incidentId, page: number)` — `queryKey` gains the page segment, `queryFn` requests `?page=${page}`, return type becomes `Page<Incident>`/`Page<IncidentUpdate>`
**Where**: `web/src/features/incidents/hooks.ts`
**Depends on**: T3
**Reuses**: existing mutation `invalidateQueries({queryKey:["incidents"]})` calls, unchanged (prefix match already covers the new page segment, per design.md)
**Requirement**: PAG-01, PAG-05, PAG-11

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `useIncidents(1)` queryKey is `["incidents", 1]`, fetches `/api/incidents?page=1`
- [x] `useIncidentUpdates(id, 1)` queryKey is `["incidents", id, "updates", 1]`, fetches `/api/incidents/{id}/updates?page=1`
- [x] Both return the full `Page<T>` object (not just `.items`)
- [x] MSW handler in `web/src/test/msw/handlers.ts` updated to return the paginated envelope for `/api/incidents` and `/api/incidents/:id/updates`
- [x] Gate check passes: `cd web && npx vitest run` (also verified clean against the stricter Frontend-tier gate, `npx tsc --noEmit && npx vitest run` - see SPEC_DEVIATION note below)
- [x] Test count: existing incidents hook tests (2) updated and passing + 2 new tests asserting queryKey/URL

**SPEC_DEVIATION**: changing `useIncidents`/`useIncidentUpdates`'s signatures broke compilation in two callers tasks.md never lists against this task: `IncidentsPage.tsx` (T5's real target, blocked on T14/`Pager` which doesn't exist yet) and `IncidentDetail.tsx` (not named in any task - spec.md's Out of Scope table only defers a *Pager UI* for its timeline, not the hook signature change). Left broken, this would hand off a non-compiling `web/` to the next batch. Fixed both minimally in this commit: fixed `page=1`, read `.items` instead of a bare array, no `Pager` added (that's still T5's job once T14 lands). `IncidentDetail.tsx` also carries a pre-existing, pagination-exposed gap noted in-code: it finds an incident by id inside the fetched list (no `GET /api/incidents/{id}` endpoint exists), so an incident past page 1 (>25 incidents) won't resolve there - out of scope for this feature, flagged for a future task.

**Tests**: unit
**Gate**: Frontend

**Commit**: `feat(web): incidents hooks accept page param and return paginated envelope`


### T6: `DomainRepository.ListPaginated`

**What**: Replace `List(ctx)` with `ListPaginated(ctx, page, pageSize int) ([]Domain, int, error)`, `page_size` 20, same `COUNT(*) OVER()` + zero-row fallback pattern as T2
**Where**: `internal/db/domain_repository.go`
**Depends on**: None
**Reuses**: same pattern established in T2
**Requirement**: PAG-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Same boundary tests as T2 (page 1 partial, page N exact, beyond-last, empty table)
- [x] Old `List` removed (confirmed single caller via grep)
- [x] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...`
- [x] Test count: 4+ new tests pass (4: Page1, Page2, PageBeyondLast, OrderByHostnameUnchanged)

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): paginate DomainRepository.List`

---

### T7: `DomainsHandler.List` wired to pagination + `useDomains` hook + `DomainsSection.tsx` Pager

**What**: Handler wires `parsePage`+`ListPaginated(ctx,page,20)`→`Page[domainResponse]`; `useDomains(page)` hook updated (queryKey/queryFn per T4's pattern); `DomainsSection.tsx` holds page state, renders `Pager`
**Where**: `internal/api/domains_handler.go`, `web/src/features/domains/hooks.ts`, `web/src/features/domains/DomainsSection.tsx`
**Depends on**: T1, T6
**Reuses**: T1's `Page[T]`/`parsePage`, `Pager` component (T14)
**Requirement**: PAG-08, PAG-09, PAG-11

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Handler: default/explicit/invalid `?page=` behave per T3's pattern
- [ ] Hook: queryKey/queryFn per T4's pattern, MSW handler updated
- [ ] Component: `Pager` renders and navigates; creating a domain refreshes visible pages (PAG-11)
- [ ] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...` and `cd web && npx tsc --noEmit && npx vitest run`
- [ ] Test count: existing domains tests updated + 4 new (2 backend, 2 frontend)

**Tests**: integration (backend) + unit (frontend)
**Gate**: full + Frontend

**Commit**: `feat: paginate domains endpoint, hook, and UI`

---

### T8: `ServiceRepository.ListPaginated` (keeps `List` for the poller)

**What**: Add `ListPaginated(ctx, page, pageSize int) ([]Service, int, error)`, `page_size` 20, same pattern as T2/T6 — **does not touch or remove** the existing `List(ctx)` (still used by `internal/poller/poller.go:115`)
**Where**: `internal/db/service_repository.go`
**Depends on**: None
**Reuses**: same pattern established in T2
**Requirement**: PAG-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Same boundary tests as T2/T6, run against `ListPaginated` only
- [ ] Existing `List(ctx)` untouched — a test confirms `internal/poller` still compiles and its existing tests still pass unmodified
- [ ] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/... ./internal/poller/...`
- [ ] Test count: 4+ new tests pass, 0 poller tests broken

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add ServiceRepository.ListPaginated alongside existing List`

---

### T9: `ServicesHandler.List` wired to pagination + `useServices` hook + `ServicesSection.tsx` Pager

**What**: Handler wires `parsePage`+`ListPaginated(ctx,page,20)`→`Page[serviceResponse]`; `useServices(page)` hook updated (keeps its existing per-row SLO-name client-side enrichment, now bounded to the page's rows only); `ServicesSection.tsx` holds page state, renders `Pager`
**Where**: `internal/api/services_handler.go`, `web/src/features/services/hooks.ts`, `web/src/features/services/ServicesSection.tsx`
**Depends on**: T1, T8
**Reuses**: T1's `Page[T]`/`parsePage`, `Pager` component (T14)
**Requirement**: PAG-08, PAG-09, PAG-11

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Handler/hook/component follow the same pattern validated in T7
- [ ] SLO-name enrichment still works per-row on the paginated page (not the whole table)
- [ ] Gate check passes: same commands as T7
- [ ] Test count: existing services tests updated + 4 new (2 backend, 2 frontend)

**Tests**: integration (backend) + unit (frontend)
**Gate**: full + Frontend

**Commit**: `feat: paginate services endpoint, hook, and UI`

---

### T10: `StatusPageRepository.ListPaginated`

**What**: Replace `List(ctx)` with `ListPaginated(ctx, page, pageSize int) ([]StatusPage, int, error)`, `page_size` 20 — `serviceIDsByStatusPage` batch lookup now runs against only the paged IDs
**Where**: `internal/db/status_page_repository.go`
**Depends on**: None
**Reuses**: same pattern established in T2; existing `serviceIDsByStatusPage` batch helper unchanged in shape, called with fewer IDs
**Requirement**: PAG-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Same boundary tests as T2/T6/T8
- [ ] `serviceIDsByStatusPage` still returns correct `service_ids` for the paged subset
- [ ] Old `List` removed (confirmed single caller)
- [ ] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...`
- [ ] Test count: 4+ new tests pass

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): paginate StatusPageRepository.List`

---

### T11: `StatusPagesHandler.List`, `PollerStatusHandler.List`, `EmailProvidersHandler.List`, `AdminsHandler.List` wired to pagination (backend only)

**What**: Four handlers wired to their respective `ListPaginated`/merge-and-slice, `page_size` 20 each:
- `StatusPagesHandler.List` → `StatusPageRepository.ListPaginated` (from T10)
- `PollerStatusHandler.List` → new `IntegrationRepository.ListPaginated` (added in this task, single caller confirmed, no poller.go conflict)
- `EmailProvidersHandler.List` → new `EmailProviderRepository.ListPaginated` (added in this task, via `email.Service.List` passthrough gaining a `page` param)
- `AdminsHandler.List` → fetches full admins + full pending invites (unchanged queries), merges (unchanged logic), applies `page`/`page_size=20` slicing to the merged Go slice in the handler (per spec Assumption - no repository signature change)

**Where**: `internal/api/status_pages_handler.go`, `internal/db/integration_repository.go`, `internal/api/poller_status.go`, `internal/db/email_provider_repository.go`, `internal/email/service.go`, `internal/api/email_providers_handler.go`, `internal/api/admins.go`
**Depends on**: T1, T10
**Reuses**: T1's `Page[T]`/`parsePage`
**Requirement**: PAG-08, PAG-09

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] All four endpoints: default/explicit/invalid `?page=` behave per T3's pattern, envelope shape correct
- [ ] `AdminsHandler.List`: merged list correctly sliced (e.g. 15 admins + 10 invites = 25 total, page 1 of 20 shows first 20 by the existing sort order, page 2 shows the remaining 5)
- [ ] `IntegrationRepository.ListPaginated`/`EmailProviderRepository.ListPaginated` added without touching any non-HTTP caller (confirmed via grep: `poller.go` never calls either)
- [ ] Existing role-gate tests for all four endpoints updated for the new response shape, still passing
- [ ] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/... ./internal/db/... ./internal/email/...`
- [ ] Test count: existing tests for these 4 endpoints (15+) still pass + 8 new pagination tests (2 per endpoint)

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): wire status-pages, poller-status, email-providers, and admins endpoints to pagination`

---

### T12: `PublicStatusHandler`'s resolved-incidents pagination (backend)

**What**: `ListPublicForStatusPage`'s `resolved` return value becomes `pagination.Page[db.IncidentPublic]` (new `page`, `pageSize=10` params added to the method signature); `active` stays a plain unpaginated slice (design.md: at most one active incident per service). Shared composition function updates both the real public handler and the preview handler that reuses it
**Where**: `internal/db/incident_repository.go` (`ListPublicForStatusPage`), `internal/api/public_status_handler.go`, `internal/api/public_status_preview_handler.go`
**Depends on**: T2 (reuses the same `COUNT(*) OVER()` + fallback pattern established there)
**Reuses**: the existing shared composition function between the real public handler and the preview handler
**Requirement**: PAG-12, PAG-13, PAG-14, PAG-15

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `ListPublicForStatusPage(ctx, id, retentionDays, page, 10)`: page 1 of >10 resolved incidents within retention returns exactly 10 + correct `total`; page 2 returns the remainder; active incidents unaffected (still a plain slice, unpaginated)
- [ ] Both `public_status_handler.go` and `public_status_preview_handler.go` responses carry the new envelope for `resolved`
- [ ] Existing retention-boundary tests (`TestPublicStatusGet_..._BeyondRetention_Hidden`, etc.) still pass against the new signature
- [ ] Gate check passes: `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/... ./internal/api/...`
- [ ] Test count: existing public-status tests (10+) updated and passing + 4 new pagination tests

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): paginate resolved incidents on the public status page`

---

### T13: `usePublicStatusPage` hook gains `loadMoreResolvedIncidents`

**What**: Hook fetches page 1 of resolved incidents on load; exposes a `loadMoreResolvedIncidents()` action that fetches the next page and appends to local state; `total`/`page_size` exposed so the component knows when exhausted
**Where**: `web/src/features/public-status/hooks.ts`
**Depends on**: T12
**Reuses**: existing `usePublicStatusPage` structure, `retry: false`/`refetchInterval` behavior unchanged
**Requirement**: PAG-13, PAG-14, PAG-15

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Initial load fetches page 1 (10 resolved incidents) — active incidents unaffected
- [ ] `loadMoreResolvedIncidents()` appends page 2's items without replacing/reordering page 1's items
- [ ] Exposes enough state (`hasMore`/`total`/current loaded count) for T20's "Carregar mais" button to know when to hide
- [ ] MSW handler updated for the paginated `resolved` shape
- [ ] Gate check passes: `cd web && npx vitest run`
- [ ] Test count: existing public-status hook tests updated + 3 new (initial page, load more appends, exhausted state)

**Tests**: unit
**Gate**: Frontend

**Commit**: `feat(web): usePublicStatusPage loads resolved incidents progressively`

---

### T14: `Pager` component

**What**: New reusable `Pager({page, totalPages, onChange})` — Anterior/Próximo buttons (reusing `Button` `ghost`/`icon` variant classes) + "Página X de Y" text; disables Anterior on `page<=1`, Próximo on `page>=totalPages`
**Where**: `web/src/components/ui/Pager.tsx`
**Depends on**: None
**Reuses**: `web/src/components/ui/Button.tsx` variant classes
**Requirement**: PAG-07

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Renders "Página X de Y", calls `onChange(page-1)`/`onChange(page+1)` on click
- [ ] Anterior disabled at page 1, Próximo disabled at last page
- [ ] `total_pages` computed as `max(1, ceil(total/page_size))` by the caller (per spec Edge Cases) — component itself just takes `totalPages` as a prop, no `total==0` special-casing inside `Pager`
- [ ] Gate check passes: `cd web && npx tsc --noEmit && npx vitest run`
- [ ] Test count: 4+ new tests (renders, disables at both boundaries, calls onChange)

**Tests**: unit
**Gate**: Frontend

**Commit**: `feat(web): add reusable Pager component`

---

### T5: `IncidentsPage.tsx` renders `Pager`

**What**: `IncidentsPage.tsx` holds `page` in `useState`, passes it to `useIncidents` (T4), renders the list from `data.items`, adds `Pager` (T14) below the table wired to `data.total`/`data.page_size`
**Where**: `web/src/features/incidents/IncidentsPage.tsx`
**Depends on**: T4, T14
**Reuses**: T4's hook, T14's `Pager`
**Requirement**: PAG-07, PAG-11

**Tools**: MCP: NONE / Skill: NONE

**Done when**:
- [ ] Page navigates via `Pager`, refetches the right page
- [ ] Creating a new incident while on page 2, then navigating back to page 1, shows the new incident (PAG-11, cache invalidation across pages)
- [ ] Gate check passes: `cd web && npx tsc --noEmit && npx vitest run`
- [ ] Test count: existing `IncidentsPage.test.tsx` tests updated and passing + 2 new tests (pager navigation, post-create page-1 refresh)

**Tests**: unit
**Gate**: Frontend

**Commit**: `feat(web): wire Pager into IncidentsPage`

---

### T16: `DomainsSection.tsx`, `ServicesSection.tsx` render `Pager` (moved from T7/T9, see note)

**What**: Same UI deliverables described in T7 and T9 above, now unblocked by `Pager`
**Where**: `web/src/features/domains/DomainsSection.tsx`, `web/src/features/services/ServicesSection.tsx`
**Depends on**: T7, T9, T14
**Reuses**: T14's `Pager`
**Requirement**: PAG-09, PAG-11

**Tools**: MCP: NONE / Skill: NONE

**Done when**: identical to the UI-specific Done-when items in T7 and T9

**Tests**: unit
**Gate**: Frontend

**Commit**: `feat(web): wire Pager into DomainsSection and ServicesSection`

---

### T17: `StatusPagesSection.tsx` gains `page` state + `Pager`

**What**: `useStatusPages(page)` hook updated (queryKey/queryFn per T4's pattern, `refetchInterval` behavior preserved per design.md's note on `status-pages/hooks.ts:15-19`); `StatusPagesSection.tsx` holds page state, renders `Pager`
**Where**: `web/src/features/status-pages/hooks.ts`, `web/src/features/status-pages/StatusPagesSection.tsx`
**Depends on**: T11, T14
**Reuses**: T14's `Pager`
**Requirement**: PAG-08, PAG-09, PAG-11

**Tools**: MCP: NONE / Skill: NONE

**Done when**:
- [ ] Hook: queryKey/queryFn per T4's pattern, `refetchInterval` still fires correctly against the current page
- [ ] Component: `Pager` renders and navigates; creating/attaching a domain refreshes visible pages
- [ ] Gate check passes: `cd web && npx tsc --noEmit && npx vitest run`
- [ ] Test count: existing status-pages tests updated + 3 new

**Tests**: unit
**Gate**: Frontend

**Commit**: `feat(web): wire Pager into StatusPagesSection`

---

### T18: `PollerStatusPage.tsx`/`PollerBanner.tsx`, `EmailProvidersPage.tsx` gain `page` state + `Pager`

**What**: `usePollerStatus(page)` and `useEmailProviders(page)` hooks updated (queryKey/queryFn per T4's pattern, existing `refetchInterval: 30_000` on poller preserved); both list components hold page state, render `Pager`
**Where**: `web/src/features/poller/hooks.ts`, `web/src/features/poller/PollerStatusPage.tsx`, `web/src/features/email-providers/hooks.ts`, `web/src/features/email-providers/EmailProvidersPage.tsx`
**Depends on**: T11, T14
**Reuses**: T14's `Pager`
**Requirement**: PAG-08, PAG-09, PAG-11

**Tools**: MCP: NONE / Skill: NONE

**Done when**:
- [ ] Both hooks: queryKey/queryFn per T4's pattern; poller's 30s polling still fires against the current page
- [ ] Both components: `Pager` renders and navigates
- [ ] `PollerBanner.tsx` (which also consumes poller status) confirmed unaffected or updated as needed - it does not need its own pager (it shows a summary banner, not a paginated list)
- [ ] Gate check passes: `cd web && npx tsc --noEmit && npx vitest run`
- [ ] Test count: existing poller/email-providers tests updated + 4 new (2 per screen)

**Tests**: unit
**Gate**: Frontend

**Commit**: `feat(web): wire Pager into PollerStatusPage and EmailProvidersPage`

---

### T19: `AdminsPage.tsx` gains `page` state + `Pager`

**What**: `useAdmins(page)` hook updated (queryKey/queryFn per T4's pattern, `Page<AdminRow>` return type); `AdminsPage.tsx` holds page state, renders `Pager`
**Where**: `web/src/features/admins/hooks.ts`, `web/src/features/admins/AdminsPage.tsx`
**Depends on**: T11, T14
**Reuses**: T14's `Pager`
**Requirement**: PAG-08, PAG-09, PAG-11

**Tools**: MCP: NONE / Skill: NONE

**Done when**:
- [ ] Hook: queryKey/queryFn per T4's pattern
- [ ] Component: `Pager` renders and navigates; inviting/removing an admin refreshes visible pages
- [ ] Gate check passes: `cd web && npx tsc --noEmit && npx vitest run`
- [ ] Test count: existing admins tests updated + 3 new

**Tests**: unit
**Gate**: Frontend

**Commit**: `feat(web): wire Pager into AdminsPage`

---

### T20: `PublicStatusPage.tsx` renders "Carregar mais" for resolved incidents

**What**: Renders resolved incidents from `usePublicStatusPage`'s progressive state (T13); shows "Carregar mais" `WHILE` loaded resolved count `< total`; button calls `loadMoreResolvedIncidents()`; hides once exhausted. Active incidents rendering unchanged (still unpaginated)
**Where**: `web/src/features/public-status/PublicStatusPage.tsx`
**Depends on**: T13
**Reuses**: T13's hook state
**Requirement**: PAG-13, PAG-14, PAG-15

**Tools**: MCP: NONE / Skill: NONE

**Done when**:
- [ ] Renders 10 resolved incidents + visible "Carregar mais" when more exist
- [ ] Clicking appends the next page without reordering/duplicating
- [ ] Button disappears once all resolved incidents are loaded
- [ ] Active incidents section unaffected (regression check on existing tests)
- [ ] Gate check passes: `cd web && npx tsc --noEmit && npx vitest run`
- [ ] Test count: existing `PublicStatusPage.test.tsx` tests updated + 3 new (initial 10, load more, button disappears)

**Tests**: unit
**Gate**: Frontend

**Commit**: `feat(web): render Carregar mais for resolved incidents on public status page`

---

## Phase Execution Map

Visual representation of task ordering. Phases run in sequence, and tasks within a phase run in order. Every screen's `Pager` wiring task (`T5`, `T16`-`T19`) lives in Phase 5 alongside `Pager` itself (`T14`) - none of them depends on a later phase.

```
Phase 1:  T1
Phase 2:  T2 ------→ T3 ------→ T4
Phase 3 (written order T6,T7,T8,T9,T10,T11 - real dependency pairs only):
          T6 ------→ T7
          T8 ------→ T9
          T10 ------→ T11
Phase 4:  T12 ------→ T13
Phase 5 (written order T14,T5,T16,T17,T18,T19,T20 - Pager's real fan-out only):
          T14 ------→ T5
          T14 ------→ T16
          T14 ------→ T17
          T14 ------→ T18
          T14 ------→ T19

Cross-phase (backward, auto-satisfied by strict phase ordering):
          T1 ------→ T3
          T1 ------→ T7
          T1 ------→ T9
          T1 ------→ T11
          T2 ------→ T12
          T4 ------→ T5
          T7 ------→ T16
          T9 ------→ T16
          T11 ------→ T17
          T11 ------→ T18
          T11 ------→ T19
          T13 ------→ T20
```

**Note**: Phases execute strictly in order (Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5). Within Phase 3 and Phase 5, tasks still run one at a time in the written order shown in the Execution Plan above (e.g. `T7` runs immediately after `T6` even though nothing outside `T6→T7`/`T8→T9`/`T10→T11` is a real dependency) - the arrows here show only genuine blocking prerequisites, matching each task's `Depends on` field exactly, per the Diagram-Definition Cross-Check below. `T20` depends only on `T13` (Phase 4), not on `T19` - it does not need `Pager`.

---

## Task Granularity Check

| Task | Scope | Status |
| ---- | ----- | ------ |
| T1: Shared Page[T] + parsePage | 1 file | ✅ Granular |
| T2: IncidentRepository pagination | 1 file, 2 methods (cohesive - same table, same pattern) | ✅ Granular |
| T3: IncidentsHandler wiring | 1 file, 2 handlers (cohesive - same endpoint family) | ✅ Granular |
| T4: incidents hooks | 1 file, 2 hooks (cohesive - same feature) | ✅ Granular |
| T5: IncidentsPage Pager | 1 file | ✅ Granular |
| T6: DomainRepository pagination | 1 file | ✅ Granular |
| T7: domains handler+hook+component | 3 files, 1 endpoint family (cohesive - established pattern, mechanical) | ✅ Granular |
| T8: ServiceRepository pagination | 1 file | ✅ Granular |
| T9: services handler+hook+component | 3 files, 1 endpoint family (cohesive) | ✅ Granular |
| T10: StatusPageRepository pagination | 1 file | ✅ Granular |
| T11: 4 handlers backend wiring | 7 files, 4 endpoints (bundled: all four are the same mechanical repo+handler wiring pattern already proven 3x by T3/T7/T9, and 3 of the 4 repos are 1-line-signature-change-plus-tests; splitting further multiplies ceremony without changing risk) | ✅ Granular (bundled by design, see rationale) |
| T12: PublicStatusHandler pagination | 3 files, 1 cohesive change (shared composition function) | ✅ Granular |
| T13: usePublicStatusPage | 1 file | ✅ Granular |
| T14: Pager component | 1 file | ✅ Granular |
| T16: Domains/Services Pager UI | 2 files, mechanical (established in T14, proven pattern) | ✅ Granular |
| T17: StatusPages hook+component | 2 files, 1 endpoint | ✅ Granular |
| T18: Poller+EmailProviders hook+component | 4 files, 2 endpoints (cohesive - identical mechanical pattern) | ✅ Granular |
| T19: Admins hook+component | 2 files, 1 endpoint | ✅ Granular |
| T20: PublicStatusPage Carregar mais | 1 file | ✅ Granular |

**T11 flagged deliberately**: it groups 4 endpoints into one task instead of 4 separate tasks. Rationale: by the time T11 runs, the exact mechanical pattern (repo `ListPaginated` + handler wiring) has already been independently proven 3 times (T2/T3 for incidents, T6/T7 for domains, T8/T9 for services) — the remaining 4 endpoints (status-pages, poller-status, email-providers, admins) are the same pattern with no new decision left to make, and 3 of them are single-file repository changes. Splitting into 4 tasks would multiply commit/gate ceremony without isolating any new risk. If any one of the four reveals a real surprise during Execute, it gets split out into its own fix task at that point (per the skill's Execution Contract).

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| ---- | ----------------------- | -------------- | ------ |
| T1 | None | None | ✅ Match |
| T2 | None | None | ✅ Match |
| T3 | T1, T2 | T2→T3 (T1 is Phase 1, cross-phase dependency implicit - see note) | ✅ Match |
| T4 | T3 | T3→T4 | ✅ Match |
| T6 | None | None | ✅ Match |
| T7 | T1, T6 | T6→T7 (T1 cross-phase, implicit) | ✅ Match |
| T8 | None | None | ✅ Match |
| T9 | T1, T8 | T8→T9 (T1 cross-phase, implicit) | ✅ Match |
| T10 | None | None | ✅ Match |
| T11 | T1, T10 | T10→T11 (T1 cross-phase, implicit) | ✅ Match |
| T12 | T2 | T2 is Phase 2, T12 is Phase 4 (cross-phase, implicit - phases are strictly sequential so T2 is guaranteed complete) | ✅ Match |
| T13 | T12 | T12→T13 | ✅ Match |
| T14 | None | None | ✅ Match |
| T5 | T4, T14 | Phase 5 sequential chain implies T14 done before T5; T4 is Phase 2 (cross-phase, guaranteed complete) | ✅ Match |
| T16 | T7, T9, T14 | Phase 5 chain implies T14 before T16; T7/T9 are Phase 3 (cross-phase, guaranteed complete) | ✅ Match |
| T17 | T11, T14 | Phase 5 chain implies T14 before T17; T11 is Phase 3 (cross-phase, guaranteed complete) | ✅ Match |
| T18 | T11, T14 | Same as T17 | ✅ Match |
| T19 | T11, T14 | Same as T17 | ✅ Match |
| T20 | T13 | Phase 4→Phase 5 sequential guarantees T13 complete before T20 | ✅ Match |

**Rule check**: no task depends on a task in a later phase - every cross-phase dependency listed above points backward (earlier phase → later phase consumer), which is allowed; phases execute strictly in order so an earlier phase's task is always complete before a later phase starts.

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| ---- | ---------------------------- | ----------------- | ----------- | -------- |
| T1 | `internal/api/pagination.go` (shared helper) | unit | unit | ✅ OK |
| T2 | `internal/db/incident_repository.go` | integration | integration | ✅ OK |
| T3 | `internal/api/incidents_handler.go` | integration | integration | ✅ OK |
| T4 | `web/src/features/incidents/hooks.ts` | unit | unit | ✅ OK |
| T5 | `web/src/features/incidents/IncidentsPage.tsx` | unit | unit | ✅ OK |
| T6 | `internal/db/domain_repository.go` | integration | integration | ✅ OK |
| T7 | handler (integration) + hook/component (unit) | integration + unit | integration + unit | ✅ OK |
| T8 | `internal/db/service_repository.go` | integration | integration | ✅ OK |
| T9 | handler (integration) + hook/component (unit) | integration + unit | integration + unit | ✅ OK |
| T10 | `internal/db/status_page_repository.go` | integration | integration | ✅ OK |
| T11 | 4 handlers + 2 new repo methods | integration | integration | ✅ OK |
| T12 | `internal/db/incident_repository.go` (public), 2 handlers | integration | integration | ✅ OK |
| T13 | `web/src/features/public-status/hooks.ts` | unit | unit | ✅ OK |
| T14 | `web/src/components/ui/Pager.tsx` | unit | unit | ✅ OK |
| T16 | 2 components | unit | unit | ✅ OK |
| T17 | hook + component | unit | unit | ✅ OK |
| T18 | 2 hooks + 2 components | unit | unit | ✅ OK |
| T19 | hook + component | unit | unit | ✅ OK |
| T20 | `web/src/features/public-status/PublicStatusPage.tsx` | unit | unit | ✅ OK |

No `Tests: none` used anywhere - every task touches a layer with a required test type in the matrix.

---

## Tips

(unchanged from skill template - omitted here per project convention of not duplicating skill boilerplate into generated artifacts)
