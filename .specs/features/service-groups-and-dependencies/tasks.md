# Service Groups and Dependencies Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/service-groups-and-dependencies/design.md`
**Status**: Draft

---

## Requirement IDs

| ID | Acceptance Criterion (spec.md) |
| --- | --- |
| SGD-01 | P1-AC1: group created, named, ordered, scoped to status page |
| SGD-02 | P1-AC2: grouped service renders under group's section |
| SGD-03 | P1-AC3: ungrouped service renders outside any section |
| SGD-04 | P1-AC4: deleting a group ungroups its members, doesn't delete them |
| SGD-05 | P1-AC5: rename/reorder group reflected on public page |
| SGD-06 | P1-AC6: reordering services within a group/ungrouped set reflected on public page |
| SGD-07 | P2-AC1: parent-child link persisted and rendered nested |
| SGD-08 | P2-AC2: a child cannot itself become a parent (depth cap) |
| SGD-09 | P2-AC3: parent/child each keep independently-computed status |
| SGD-10 | P2-AC4: group membership and parent-child link combine freely |
| SGD-11 | P2-AC5: cross-page `parent_service_id` rejected (422) |
| SGD-12 | P2-AC6: self-referential `parent_service_id` rejected (422) |
| SGD-13 | P2-AC7: removing/deleting a parent leaves children parentless, not dangling |

---

## Test Coverage Matrix

> No `.specs/codebase/TESTING.md` exists in this repo — derived from `AGENTS.md` §3/§4/§5 (backend gate commands, integration-DB hard rule, frontend MSW-parity/i18n rule) and the existing test layout sampled during Design (`internal/db/status_page_repository_test.go`, `internal/api/status_pages_handler_test.go`, `web/src/features/status-pages/*.test.tsx`).

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Go migration (`0046`) | none | Build/gate only - schema verified by the repository tests that exercise it | `internal/db/migrations/` | `go build ./...` (embedded migration compile check) |
| Go repository (group CRUD) | integration | `CreateGroup` appends at end position; `UpdateGroup` renames/repositions; `DeleteGroup` leaves members ungrouped (not deleted); `ListGroups` ordered - SGD-01, SGD-04 | `internal/db/status_page_repository_test.go` | `go test -tags=integration ./internal/db` |
| Go domain logic (`validateServicePlacements`) | unit | All 3 rejection branches (cross-page group, parent not in set, self-ref) + the grandparent-depth branch + the happy path combining group+parent - SGD-08, SGD-10, SGD-11, SGD-12 | `internal/db/status_page_repository_test.go` (or co-located `_validation_test.go`) | `go test ./internal/db` |
| Go repository (`SetServices` rewrite) | integration | Full-replace persists `group_id`/`parent_service_id`/`position`; a call that omits a previously-set group/parent clears it (replace-all semantics); `ON DELETE SET NULL` behavior proven for both FKs via real deletes under RLS - SGD-02, SGD-03, SGD-06, SGD-07, SGD-13 | `internal/db/status_page_repository_test.go` | `go test -tags=integration ./internal/db` |
| Go repository (`ListForStatusPage` JOIN) | integration | Returns `group_id`/`group_name`/`parent_service_id`/`position`; `ORDER BY` reflects group then service position - SGD-02, SGD-03, SGD-06 | `internal/db/service_repository_test.go` | `go test -tags=integration ./internal/db` |
| Go API (group handlers + routes) | integration | `POST`/`PATCH`/`DELETE`/`GET /groups` end-to-end incl. 404s, role gating (`writeRoles` vs `anyRole`), audit log entries - SGD-01, SGD-04, SGD-05 | `internal/api/status_pages_handler_test.go` | `go test -tags=integration ./internal/api` |
| Go API (`SetServices` handler + `GET /services` detail) | integration | New request/response shape end-to-end; all 4 validation errors surface as distinct 422 messages, never a raw DB error - SGD-02, SGD-03, SGD-06, SGD-07, SGD-09, SGD-10, SGD-11, SGD-12 | `internal/api/status_pages_handler_test.go` | `go test -tags=integration ./internal/api` |
| Go API (`public_status_handler` extension) | integration | `publicServiceResponse` carries group/parent fields; `groups` array present incl. an empty group (zero services); each service keeps its own independently-computed status untouched by grouping - SGD-02, SGD-03, SGD-05, SGD-06, SGD-09 | `internal/api/public_status_handler_test.go` | `go test -tags=integration ./internal/api` |
| Frontend types/hooks | none (typecheck only) | `StatusPageServiceGroup` type, extended `Service`/public types, new hooks compile and call the right endpoints/payload shape | `web/src/types/api.ts`, `web/src/features/status-pages/hooks.ts` | `npx tsc -b --noEmit` (in `web/`) |
| Frontend MSW fixtures | none | New in-memory state + handlers mirror the Go validation 1:1 (fixture/test-infra, exercised by the component tests below, not tested standalone) | `web/src/test/msw/handlers.ts` | `npx tsc -b --noEmit` (compiles as part of the test build) |
| Frontend component (`ServiceGroupPanel`, `@dnd-kit` wiring) | unit (vitest + Testing Library) | Drag service into group sets `group_id`; drag onto another service sets `parent_service_id`; keyboard-sensor reorder works (not pointer-only); depth/self-ref violations surfaced as inline errors from the 422 - SGD-01, SGD-04, SGD-07, SGD-08, SGD-10, SGD-11, SGD-12 | `web/src/features/status-pages/ServiceGroupPanel.test.tsx` | `npm run test` (in `web/`) |
| Frontend component (`PublicStatusPage` grouping) | unit (vitest + Testing Library) | Grouped services render under labeled section; ungrouped render outside; child renders nested under parent card, each with its own independent status badge; empty group still renders its header; React key is `service.id` not `service.name` - SGD-02, SGD-03, SGD-05, SGD-06, SGD-07, SGD-09 | `web/src/features/public-status/PublicStatusPage.test.tsx` | `npm run test` (in `web/`) |
| Frontend i18n | none (parity script only) | New group-management/tree-UI strings present in both pt-BR/en, no orphaned keys | `web/src/locales/*.json` | `npm run i18n:check` (in `web/`) |

## Gate Check Commands

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick (backend, unit only) | After a backend task with no DB-backed test | `go test ./...` |
| Full (backend, integration) | After a backend task with a DB-backed/integration test - **disposable Postgres container only, never `vane-dev-pg`** (`AGENTS.md` §3) | `make test-integration` |
| Build (backend) | After phase completion | `go build ./... && gofmt -l . && go vet ./...` |
| Frontend (unit) | After a frontend task | `cd web && npm run test` |
| Frontend (full) | After frontend phase completion | `cd web && npx tsc -b --noEmit && npm run test && npm run i18n:check` |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins. Tasks within a phase marked `[P]` run in parallel; unmarked tasks within a phase run in order.

### Phase 1: Schema

Task: T1

### Phase 2: Backend - groups, validation, placement

Tasks: T2 → T3 → T4 (sequential - each extends the last)

### Phase 3: Backend - handlers, routes, reads

Tasks: T5 → T6 → T7 (sequential — **correction found during Execute**: T5/T6 were originally marked `[P]`, but both touch `internal/api/status_pages_handler.go` and the same `statusPageCreatorLister` interface, which real parallel sub-agents editing from the same starting commit would clobber. Running sequentially instead.)

### Phase 4: Backend - read-path extensions

Tasks: T8 → T9 (T9 depends on T8's JOIN columns)

### Phase 5: Frontend - types, hooks, fixtures

Tasks: T10 → T11 → T12 (sequential - each builds on the last's exports)

### Phase 6: Frontend - UI

Tasks: T13, T14 `[P]` (both depend on Phase 5, touch disjoint files/components)

### Phase 7: Docs

Task: T15

```
Phase 1:  T1
Phase 2:  T1 ──→ T2 ──→ T3 ──→ T4
Phase 3:  T4 ──→ T5 ──→ T6 ──→ T7  (corrected: sequential, shared file — see note above)
Phase 4:  T7 ──→ T8 ──→ T9
Phase 5:  T9 ──→ T10 ──→ T11 ──→ T12
Phase 6:  T12 ─┬→ T13 [P]
                └→ T14 [P]
Phase 7:  T13, T14 ──→ T15
```

---

## Task Breakdown

### T1: Migration `0046` - `status_page_service_groups` table + placement columns

**What**: New migration creates `status_page_service_groups (id, status_page_id, name, position, created_at)` and adds `group_id UUID REFERENCES status_page_service_groups(id) ON DELETE SET NULL`, `parent_service_id UUID REFERENCES services(id) ON DELETE SET NULL`, `position INT NOT NULL DEFAULT 0` to `status_page_services`. `.down.sql` drops the 3 columns then the new table.
**Where**: `internal/db/migrations/0046_status_page_service_groups.up.sql`, `internal/db/migrations/0046_status_page_service_groups.down.sql`
**Depends on**: None
**Reuses**: migration file-pair convention (`0045_drop_domain_ssl_status.up.sql`/`.down.sql`)
**Requirement**: SGD-01 (prerequisite)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `.up.sql` creates the table and adds all 3 columns with correct FKs/defaults; `.down.sql` reverses cleanly
- [x] Migration numbered `0046` (next after `0045`), matches naming convention exactly
- [x] Gate check passes: `go build ./... && gofmt -l . && go vet ./...`

**Status: ✅ Complete** — commit `1e4a365`

**Tests**: none directly (exercised by T2/T4's integration tests)
**Gate**: build

**Commit**: `feat(db): add status page service groups and placement columns`

---

### T2: `StatusPageServiceGroup` CRUD repository methods

**What**: `StatusPageServiceGroup` struct (`ID, StatusPageID, Name, Position, CreatedAt`). `CreateGroup(ctx, statusPageID, name) (*StatusPageServiceGroup, error)` (position = current count of groups for the page), `UpdateGroup(ctx, id, name, position) (*StatusPageServiceGroup, error)`, `DeleteGroup(ctx, id) error`, `ListGroups(ctx, statusPageID) ([]StatusPageServiceGroup, error)` ordered by `position`. `StatusPageRepository.Delete`'s existing manual-cleanup transaction (`internal/db/status_page_repository.go:360-384`) extended to also `DELETE FROM status_page_service_groups WHERE status_page_id = $1`.
**Where**: `internal/db/status_page_repository.go` (or new `internal/db/status_page_service_group_repository.go` if the existing file is already large - check line count first), `internal/db/status_page_repository_test.go`
**Depends on**: T1
**Reuses**: `StatusPageRepository`'s existing transaction/error-handling conventions
**Requirement**: SGD-01, SGD-04

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `CreateGroup` appends at the end position; `UpdateGroup` can rename and/or reposition; `DeleteGroup` removes the group row (members' `group_id` becomes `NULL` via FK, not Go code); `ListGroups` returns position-ordered
- [x] `StatusPageRepository.Delete` cleans up `status_page_service_groups` for the deleted page in the same transaction
- [x] Integration tests cover all 4 methods + the `Delete` cascade-cleanup, run under RLS per `AGENTS.md` §3 (disposable container only)
- [x] Gate check passes: `make test-integration`

**Status: ✅ Complete** — 7 tests added, all pass, full `internal/db` package green

**Tests**: `internal/db/status_page_repository_test.go`
**Gate**: full (integration)

**Commit**: `feat(db): add status page service group CRUD`

---

### T3: `validateServicePlacements` - placement validation rules

**What**: `type ServicePlacement struct { ServiceID string; GroupID *string; ParentServiceID *string; Position int }`. `validateServicePlacements(placements []ServicePlacement, validGroupIDs map[string]bool) error` enforces: every non-nil `GroupID` is in `validGroupIDs`; every non-nil `ParentServiceID` matches some `ServiceID` in the same `placements` slice; no `ParentServiceID == ServiceID` (self-reference); no placement whose `ServiceID` is itself used as another placement's `ParentServiceID` AND which itself has a non-nil `ParentServiceID` (grandparent depth cap). Returns one of 4 distinct sentinel errors (`ErrGroupCrossPage`, `ErrParentNotInSet`, `ErrSelfReference`, `ErrMaxDepthExceeded`).
**Where**: `internal/db/status_page_repository.go`, `internal/db/service_placement_validation_test.go` (SPEC_DEVIATION: `status_page_repository_test.go` is `//go:build integration`-tagged, so pure unit tests placed there never run under this task's "quick" gate — followed existing repo precedent of untagged pure-unit-test files in this package, e.g. `notification_preference_defaults_test.go`)
**Depends on**: None (pure function, logically placed after T2 since T4 wires it into the group-aware `SetServices`)
**Reuses**: sentinel-error pattern already used elsewhere in `internal/db` (e.g. `ErrNotFound`, `ErrServiceInUse`)
**Requirement**: SGD-08, SGD-10, SGD-11, SGD-12

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] All 4 rejection branches covered by table-driven unit tests, plus the happy path (group + parent combined on the same placement, and independently)
- [x] Gate check passes: `go test ./internal/db`

**Status: ✅ Complete** — commit `24e4911`, 9 subtests, deterministic rule order documented (group-cross-page → self-reference → parent-not-in-set → depth cap)

**Tests**: `internal/db/status_page_repository_test.go`
**Gate**: quick

**Commit**: `feat(db): add service placement validation rules`

---

### T4: `SetServices` rewrite - group/parent/position in the full-replace write

**What**: `StatusPageRepository.SetServices(ctx, statusPageID string, placements []ServicePlacement) error` replaces the current `(ctx, id string, serviceIDs []string) error` signature. Before the transaction's `DELETE`/`INSERT`, loads this page's valid group ids (`ListGroups`) and calls `validateServicePlacements`; on validation failure, returns the sentinel error without touching the DB. On success, same `DELETE FROM status_page_services WHERE status_page_id = $1` then loop-`INSERT` as today, now including `group_id, parent_service_id, position` columns.
**Where**: `internal/db/status_page_repository.go`, `internal/db/status_page_repository_test.go`
**Depends on**: T2, T3
**Reuses**: existing `SetServices` transaction shape (`internal/db/status_page_repository.go:138-171`)
**Requirement**: SGD-02, SGD-03, SGD-06, SGD-07, SGD-13

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Full-replace persists all 3 new columns correctly
- [x] A call that drops a previously-set `group_id`/`parent_service_id` for a service clears it (replace-all, not merge)
- [x] Each of the 4 validation errors is returned (not silently swallowed) and the DB is untouched on failure
- [x] Integration test proves `ON DELETE SET NULL` for both FKs via a real delete of a referenced group/service
- [x] Gate check passes: `make test-integration`

**Status: ✅ Complete** — commit `b2c9f75`. `internal/db` builds/tests clean in isolation; repo-wide `go build ./...` intentionally broken until T6 updates `statusPageCreatorLister` (expected T4→T6 boundary, not a regression).

**Tests**: `internal/db/status_page_repository_test.go`
**Gate**: full (integration)

**Commit**: `feat(db): extend SetServices with group and parent placement`

---

### T5: Group handlers + routes `[P]`

**What**: `CreateGroup`/`UpdateGroup`/`DeleteGroup`/`ListGroups` HTTP handlers on `StatusPagesHandler` (or new `StatusPageGroupsHandler` if the existing handler file is already large). `statusPageCreatorLister` interface (`internal/api/status_pages_handler.go:36-44`) gains the matching repository methods. Routes added to the existing linear block in `internal/cli/routes.go`: `POST/PATCH/DELETE /api/status-pages/{id}/groups[/{groupId}]` (`writeRoles`), `GET /api/status-pages/{id}/groups` (`anyRole`) - same gating convention as the existing `/services` route.
**Where**: `internal/api/status_pages_handler.go` (or new `internal/api/status_page_groups_handler.go`), `internal/api/status_pages_handler_test.go`, `internal/cli/routes.go`
**Depends on**: T4
**Reuses**: existing handler/audit-log/role-gating patterns (`internal/api/status_pages_handler.go:234-271`'s `SetServices` handler as the closest precedent)
**Requirement**: SGD-01, SGD-04, SGD-05

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] All 4 endpoints wired, role-gated identically to `/services`, audit log entries recorded for create/update/delete
- [x] 404 on unknown status page or group id
- [x] Integration tests cover each endpoint end-to-end through the real router
- [x] Gate check passes: `make test-integration`

**Status: ✅ Complete** — commit `85ae02e`, 16 tests. Gate verified in an isolated throwaway worktree (temporary T6-style shim, deleted after) since `internal/api`'s own test binary needs `SetServices` to compile too — real tree's `SetServices` untouched, confirmed byte-identical pre/post.

**Tests**: `internal/api/status_pages_handler_test.go`
**Gate**: full (integration)

**Commit**: `feat(api): add status page service group endpoints`

---

### T6: `SetServices` handler rewrite `[P]`

**What**: `setServicesRequest` changes from `{ServiceIDs []string}` to `{Services []servicePlacementRequest{ServiceID, GroupID *string, ParentServiceID *string, Position int}}`. Handler decodes, maps to `[]db.ServicePlacement`, calls the T4 repository method, maps each of the 4 sentinel errors to a distinct 422 message (never a raw DB/Go error - same pattern as `mapDomainVerificationResult`), 404 on unknown status page, 200 + refreshed response on success, audit log entry unchanged (`"status_page_services_updated"`).
**Where**: `internal/api/status_pages_handler.go`, `internal/api/status_pages_handler_test.go`
**Depends on**: T4
**Reuses**: `mapDomainVerificationResult`-style distinct-422-messages pattern (`internal/api/domains_handler.go`)
**Requirement**: SGD-02, SGD-03, SGD-06, SGD-07, SGD-09, SGD-10, SGD-11, SGD-12

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] New request/response shape wired end-to-end
- [x] Each of the 4 validation failures surfaces as its own distinct 422 message, asserted by name in tests (not just status code)
- [x] Existing `status_page_services_updated` audit behavior unchanged
- [x] Integration tests cover happy path (group+parent combined) and all 4 failure cases through the real router
- [x] Gate check passes: `make test-integration`

**Status: ✅ Complete** — commit `b5f29dd`. Repo-wide `go build/gofmt/vet` clean again — T4→T6 compile-break window closed.

**Tests**: `internal/api/status_pages_handler_test.go`
**Gate**: full (integration)

**Commit**: `feat(api): rewrite SetServices to carry group and parent placement`

---

### T7: `GET /api/status-pages/{id}/services` - placement detail read

**What**: New read endpoint returning the full per-service placement detail (`service_id, group_id, parent_service_id, position`) for a status page - the admin editor needs this (plus T5's `GET /groups`) to render the tree, without growing the existing `GET /api/status-pages/{id}` response used by other consumers (Design's explicit decision).
**Where**: `internal/api/status_pages_handler.go`, `internal/api/status_pages_handler_test.go`, `internal/cli/routes.go`, plus `internal/db/status_page_repository.go` (SPEC_DEVIATION: tasks.md's original file list omitted the repository-layer read method `ListServicePlacements` this endpoint needed — a prerequisite the task breakdown missed, not a scope violation by the implementer)
**Depends on**: T6 (reuses its request/response types)
**Reuses**: T6's `servicePlacementRequest`-equivalent shape for the response, `anyRole` gating convention
**Requirement**: SGD-02, SGD-03, SGD-06, SGD-07 (admin-read prerequisite for the frontend tree)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Endpoint returns every placement for the page with all 3 new fields, `anyRole` gated, 404 on unknown page
- [x] Integration test covers the endpoint through the real router
- [x] Gate check passes: `make test-integration`

**Status: ✅ Complete** — commit `fb07627`, 4 tests. Phase 3 closed; repo-wide build/vet/fmt clean.

**Tests**: `internal/api/status_pages_handler_test.go`
**Gate**: full (integration)

**Commit**: `feat(api): add status page service placement detail endpoint`

---

### T8: `ListForStatusPage` JOIN extension

**What**: `ServiceRepository.ListForStatusPage` (`internal/db/service_repository.go:269-296`) SELECT/JOIN extended to include `sps.group_id, sps.parent_service_id, sps.position`, plus a `LEFT JOIN status_page_service_groups g ON g.id = sps.group_id` for `g.name`. `ORDER BY s.name` becomes `ORDER BY g.position NULLS LAST, sps.position`. `Service` struct gains the new fields directly (decided during Execute: `ServiceLister` interface in `public_status_handler.go` is typed to `[]db.Service` and T8's scope forbade touching that file, so a wrapper type wasn't actually viable — other callers never serialize `Service` blindly, they build their own response DTOs, so the extra zero-valued fields are inert elsewhere).
**Where**: `internal/db/service_repository.go`, `internal/db/service_repository_test.go`
**Depends on**: T4 (columns must exist and be populated to test ordering meaningfully)
**Reuses**: existing `ListForStatusPage` query shape
**Requirement**: SGD-02, SGD-03, SGD-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Query returns group/parent/position fields correctly, including `NULL` for ungrouped/parentless services
- [x] Ordering verified: grouped services in group-position order, then group-position order repeats within each group by service position, ungrouped services (`NULLS LAST`) after all grouped ones
- [x] Other callers of `Service`/`ListForStatusPage` unaffected (confirm via existing test suite, no silent behavior change for non-status-page reads)
- [x] Integration test added, run under RLS per `AGENTS.md` §3
- [x] Gate check passes: `make test-integration`

**Status: ✅ Complete** — commit `0a869c2`, 4 tests. Pre-existing unrelated test flakiness confirmed via `git stash` comparison against bare `develop`, not a regression.

**Tests**: `internal/db/service_repository_test.go`
**Gate**: full (integration)

**Commit**: `feat(db): include group and parent placement in ListForStatusPage`

---

### T9: Public status handler - grouped response

**What**: `publicServiceResponse` (`internal/api/public_status_handler.go:95-113`) gains `ID, GroupID *string (omitempty), GroupName *string (omitempty), ParentServiceID *string (omitempty), ParentServiceName *string (omitempty)`. `publicStatusResponse` gains `Groups []publicGroupResponse{ID, Name, Position}` - loaded via T2's `ListGroups`, including groups with zero services (operator-visible empty state). `composeResponse` (`internal/api/public_status_handler.go:219-330`) populates the new fields from T8's extended `ListForStatusPage` result; no new query beyond the one `ListGroups` call (no N+1). Each service's own independently-computed `Status`/`History`/`UptimePercent`/`StatusAnalysis` logic (lines 278-327) is untouched by this task - grouping never affects status computation (spec's explicit non-goal).
**Where**: `internal/api/public_status_handler.go`, `internal/api/public_status_handler_test.go`, plus `internal/api/public_status_preview_handler_test.go` and `internal/cli/routes.go`/`internal/cli/serve.go` (SPEC_DEVIATION: `NewPublicStatusHandler` needed a new `statusPages` dependency to reach `ListGroups`, which rippled into its 2 production wiring sites and 1 more test file — a prerequisite the task breakdown's file list missed, not a scope violation)
**Depends on**: T8
**Reuses**: existing `composeResponse` loop structure
**Requirement**: SGD-02, SGD-03, SGD-05, SGD-06, SGD-09

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `publicServiceResponse`/`publicStatusResponse` carry the new fields correctly, `omitempty` for every nullable one
- [x] An empty group (zero services) still appears in the `groups` array
- [x] Each service's status/history/uptime/analysis values are provably unchanged by grouping (same assertions as pre-existing tests, now also checking the new fields are additive, not replacing anything)
- [x] Integration test covers a page with grouped, ungrouped, and parent/child services together
- [x] Gate check passes: `make test-integration`

**Status: ✅ Complete** — commit `8d09e7a`, 2 new tests + 34 pre-existing unmodified. **Backend half of the feature (T1-T9) fully done.** Pre-existing unrelated flake (`TestDomainHealthScheduler_NSChanged_...`) confirmed via `git stash`, not a regression.

**Tests**: `internal/api/public_status_handler_test.go`
**Gate**: full (integration)

**Commit**: `feat(api): expose service groups and dependencies on public status page`

---

### T10: Frontend types

**What**: `StatusPageServiceGroup` type added to `web/src/types/api.ts`. `Service` type extended with the new optional placement fields (matching T8's additive Go fields). `web/src/lib/publicStatus.ts`'s `PublicServiceEntry` gains `id, group_id?, group_name?, parent_service_id?, parent_service_name?`; `PublicStatusPageData` gains `groups: {id, name, position}[]`.
**Where**: `web/src/types/api.ts`, `web/src/lib/publicStatus.ts`
**Depends on**: T9 (mirrors its exact field names/nullability)
**Reuses**: existing type patterns in both files
**Requirement**: SGD-01 through SGD-13 (type-level prerequisite for all frontend work)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] All new types added, nullability matches the Go `omitempty`/pointer fields exactly
- [ ] Gate check passes: `npx tsc -b --noEmit` (in `web/`)

**Tests**: none (typecheck only)
**Gate**: frontend (unit, tsc only for this task)

**Commit**: `feat(web): add status page service group and placement types`

---

### T11: Frontend hooks

**What**: `useStatusPageGroups(statusPageId)`, `useCreateGroup()`, `useUpdateGroup()`, `useDeleteGroup()`, `useStatusPageServicePlacements(statusPageId)` added to `web/src/features/status-pages/hooks.ts`. `useSetStatusPageServices`'s `SetStatusPageServicesInput.service_ids: string[]` changes to `services: ServicePlacement[]` (matching T6's request shape), mutation body/query-invalidation pattern otherwise unchanged.
**Where**: `web/src/features/status-pages/hooks.ts`
**Depends on**: T10
**Reuses**: existing `useSetStatusPageServices`/`useMutation`/`queryClient.invalidateQueries` patterns (`hooks.ts:86-108`)
**Requirement**: SGD-01 through SGD-13 (hook-level prerequisite)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] All 5 new hooks + the rewritten `useSetStatusPageServices` compile and call the correct endpoints/methods (`POST`/`PATCH`/`DELETE`/`GET` matching T5/T6/T7)
- [ ] Gate check passes: `npx tsc -b --noEmit` (in `web/`)

**Tests**: none (typecheck only, behavior exercised by T13/T14's component tests)
**Gate**: frontend (unit, tsc only for this task)

**Commit**: `feat(web): add status page service group hooks`

---

### T12: MSW fixtures

**What**: New in-memory `statusPageGroupsState`/`statusPagePlacementsState` arrays. `http.get/post/patch/delete` handlers for `/api/status-pages/:id/groups[/:groupId]` and `/api/status-pages/:id/services` (both the T6 write and T7 read), mirroring the Go validation 1:1 (same 404/422 cases as T5/T6/T7), inserted near the existing `status-pages` block (`web/src/test/msw/handlers.ts:1145-1284`).
**Where**: `web/src/test/msw/handlers.ts`
**Depends on**: T11
**Reuses**: existing in-memory-state + handler-mirroring convention used by every other block in the file
**Requirement**: SGD-01 through SGD-13 (test-infra prerequisite for T13/T14)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] All new endpoints mocked with the same validation/error shape as the real handlers
- [ ] Gate check passes: `npx tsc -b --noEmit` (in `web/`, compiles as part of the test build)

**Tests**: none (fixture/test-infra, exercised by T13/T14's component tests)
**Gate**: frontend (unit, tsc only for this task)

**Commit**: `test(web): add MSW fixtures for status page service groups`

---

### T13: `ServiceGroupPanel` - draggable tree UI `[P]`

**What**: `@dnd-kit/core` + `@dnd-kit/sortable` added to `web/package.json` (first D&D dependency in the codebase). New `ServiceGroupPanel.tsx` renders the "Linked" services (from `StatusPageEditorContent.tsx`'s existing split) as a 2-level draggable tree: drag a service into a group section sets `group_id`; drag a service onto another sets `parent_service_id` (with a visual affordance distinguishing "drop into group" from "drop onto service as child"); drag reorders set `position` (array index at save time, per Design). Keyboard sensor wired (not pointer-only - accessibility requirement, not optional). "Create group" affordance (inline name input) calls `useCreateGroup`. Save calls the rewritten `useSetStatusPageServices` with the full placement array. Old `ServiceGroup`/`ServiceRow` pair (`StatusPageEditorContent.tsx:408-473`, unrelated local-only component, name collision noted in Design) is replaced by this component for the "Linked" list only - the "Available"/search side is untouched.
**Where**: `web/src/features/status-pages/ServiceGroupPanel.tsx` (new), `web/src/features/status-pages/ServiceGroupPanel.test.tsx` (new), `web/src/features/status-pages/StatusPageEditorContent.tsx` (modified: wire in new component, remove old `ServiceGroup`/`ServiceRow`), `web/package.json`
**Depends on**: T12
**Reuses**: `Field`/`Button`/`Tag` primitives (`web/src/components/ui/`), `StatusPageEditorContent.tsx`'s existing "Linked"/"Available" split and save-button pattern
**Requirement**: SGD-01, SGD-04, SGD-07, SGD-08, SGD-10, SGD-11, SGD-12

**Tools**:
- MCP: context7 (resolve `@dnd-kit` current API/patterns before use, per Knowledge Verification Chain)
- Skill: NONE

**Done when**:
- [ ] Drag service into group section sets `group_id`; drag onto another service sets `parent_service_id`; both persist via `SetServices` on save
- [ ] Keyboard-only operation works (tab to a service, keyboard-triggered move) - not mouse/pointer-only
- [ ] A 422 from any of the 4 backend validation rules surfaces as an inline, readable error (not a silent failure or raw JSON dump)
- [ ] Old `ServiceGroup`/`ServiceRow` component fully removed, no dead code left
- [ ] Gate check passes: `cd web && npm run test`
- [ ] Test count: all existing `StatusPageEditorContent.test.tsx` tests still pass (none silently deleted) + new `ServiceGroupPanel.test.tsx` tests added

**Tests**: `web/src/features/status-pages/ServiceGroupPanel.test.tsx`
**Gate**: frontend (unit)

**Commit**: `feat(web): add drag-and-drop service group and dependency editor`

---

### T14: Public status page - grouped rendering `[P]`

**What**: `PublicStatusPage.tsx`'s `data.services.map(...)` (lines 382-429) replaced with: group `data.services` by `group_id` client-side (plus `data.groups` for names/order/empty-group display, including a synthetic "ungrouped" bucket rendered last), one `<section>` per group with a header (group name), existing per-service `<Card>` unchanged inside each section, a child service's card rendered indented directly under its parent's card wherever `parent_service_id` is set (within the same section it already belongs to). React key changes from `service.name` to `service.id` (fixes the latent duplicate-name key bug, per Design).
**Where**: `web/src/features/public-status/PublicStatusPage.tsx`, `web/src/features/public-status/PublicStatusPage.test.tsx`
**Depends on**: T12
**Reuses**: existing `<Card>` service-row markup (untouched internals, only its wrapping/grouping changes)
**Requirement**: SGD-02, SGD-03, SGD-05, SGD-06, SGD-07, SGD-09

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Grouped services render under their labeled section; ungrouped render in a trailing section with no visible "ungrouped" label clutter (Design: synthetic bucket, not necessarily a visible header - confirm exact copy at implementation)
- [ ] Child service renders nested/indented under its parent's card, each showing its own independent status badge
- [ ] An empty group (zero services) still renders its header
- [ ] React key is `service.id`
- [ ] Existing range-selector/history/uptime rendering inside each `<Card>` unchanged
- [ ] Gate check passes: `cd web && npm run test`
- [ ] Test count: all existing `PublicStatusPage.test.tsx` tests still pass (none silently deleted) + new grouping tests added

**Tests**: `web/src/features/public-status/PublicStatusPage.test.tsx`
**Gate**: frontend (unit)

**Commit**: `feat(web): render service groups and dependencies on public status page`

---

### T15: Docs - AD entry

**What**: New `AD-NNN` entry in `.specs/STATE.md` documenting the feature (data model, the full-replace write pattern extension, the `@dnd-kit` dependency addition, the public API additive-fields decision). `README.md`'s feature table updated if this is judged "genuinely new/user-facing" per `AGENTS.md` §6 (it is - status page grouping is a visible feature).
**Where**: `.specs/STATE.md`, `README.md`
**Depends on**: T13, T14
**Reuses**: existing `AD-NNN` entry format and README feature-table format
**Requirement**: none directly (process/traceability)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `AD-NNN` entry added with the next free number (confirm by reading `.specs/STATE.md`'s last entry at implementation time, not assumed now)
- [ ] README feature table has a line for service groups/dependencies if judged user-facing-new
- [ ] Full gate green one final time: `go build ./... && go test ./... && go vet ./... && gofmt -l .` + `make test-integration` + `cd web && npx tsc -b --noEmit && npm run test && npm run i18n:check`

**Tests**: none
**Gate**: full (backend) + frontend (full)

**Commit**: `docs(specs): add AD for service groups and dependencies`

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: Migration | 1 schema change (1 table + 3 columns) | ✅ Granular |
| T2: Group CRUD repo methods | 1 cohesive set of methods on 1 concept (group lifecycle) | ✅ Granular (2-3 related methods, same cohesive concept) |
| T3: Placement validation | 1 function | ✅ Granular |
| T4: `SetServices` rewrite | 1 method | ✅ Granular |
| T5: Group handlers + routes | 1 cohesive resource's HTTP surface | ✅ Granular (CRUD for one resource, same cohesive concept) |
| T6: `SetServices` handler rewrite | 1 handler | ✅ Granular |
| T7: Placement detail GET | 1 endpoint | ✅ Granular |
| T8: `ListForStatusPage` JOIN | 1 query | ✅ Granular |
| T9: Public handler grouped response | 1 handler's response shape | ✅ Granular |
| T10: Frontend types | 1 file, additive types | ✅ Granular |
| T11: Frontend hooks | 1 file, 1 cohesive hook set | ✅ Granular (all hooks for the same resource) |
| T12: MSW fixtures | 1 file, 1 cohesive fixture set | ✅ Granular |
| T13: `ServiceGroupPanel` | 1 component (replaces 1 old component) | ✅ Granular (cohesive: one tree-editing concern, drag+keyboard+create-group are facets of the same interaction, not separate concerns) |
| T14: Public page grouping | 1 component's render logic | ✅ Granular |
| T15: Docs | 2 files, both pure documentation | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| ---- | ---------------------- | -------------- | ------ |
| T1 | None | None | ✅ Match |
| T2 | T1 | T1 → T2 | ✅ Match |
| T3 | None (logically after T2) | T2 → T3 | ✅ Match (sequential phase, no parallel conflict) |
| T4 | T2, T3 | T3 → T4 | ✅ Match |
| T5 | T4 | T4 → T5 [P] | ✅ Match |
| T6 | T4 | T4 → T6 [P] | ✅ Match |
| T7 | T6 | T5, T6 → T7 | ✅ Match (T7 waits on both parallel tasks of Phase 3 per the phase boundary) |
| T8 | T4 | T7 → T8 | ✅ Match (Phase 4 starts after Phase 3 completes, i.e. after T7) |
| T9 | T8 | T8 → T9 | ✅ Match |
| T10 | T9 | T9 → T10 | ✅ Match |
| T11 | T10 | T10 → T11 | ✅ Match |
| T12 | T11 | T11 → T12 | ✅ Match |
| T13 | T12 | T12 → T13 [P] | ✅ Match |
| T14 | T12 | T12 → T14 [P] | ✅ Match |
| T15 | T13, T14 | T13, T14 → T15 | ✅ Match |

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1 | Go migration | none | none | ✅ OK |
| T2 | Go repository (group CRUD) | integration | integration (`status_page_repository_test.go`) | ✅ OK |
| T3 | Go domain logic (validation) | unit | unit (`status_page_repository_test.go`) | ✅ OK |
| T4 | Go repository (`SetServices`) | integration | integration (`status_page_repository_test.go`) | ✅ OK |
| T5 | Go API (group handlers) | integration | integration (`status_pages_handler_test.go`) | ✅ OK |
| T6 | Go API (`SetServices` handler) | integration | integration (`status_pages_handler_test.go`) | ✅ OK |
| T7 | Go API (placement detail GET) | integration (bundled into the `SetServices` handler matrix row) | integration (`status_pages_handler_test.go`) | ✅ OK |
| T8 | Go repository (`ListForStatusPage` JOIN) | integration | integration (`service_repository_test.go`) | ✅ OK |
| T9 | Go API (`public_status_handler`) | integration | integration (`public_status_handler_test.go`) | ✅ OK |
| T10 | Frontend types | none (typecheck) | none | ✅ OK |
| T11 | Frontend hooks | none (typecheck) | none | ✅ OK |
| T12 | Frontend MSW fixtures | none (fixture/test-infra) | none | ✅ OK |
| T13 | Frontend component (`ServiceGroupPanel`) | unit | unit (`ServiceGroupPanel.test.tsx`) | ✅ OK |
| T14 | Frontend component (`PublicStatusPage`) | unit | unit (`PublicStatusPage.test.tsx`) | ✅ OK |
| T15 | Docs | none | none | ✅ OK |

---

## MCPs and Skills Confirmation

Per task, the only non-default tool need identified: **T13** should use the `context7` MCP to resolve `@dnd-kit`'s current API before writing the drag-and-drop wiring (Knowledge Verification Chain - this is a brand-new dependency to the codebase, no existing pattern to copy). Every other task uses only standard filesystem/Bash/Go/TS tooling already available, no additional MCP or Skill.

**Question for the user before Execute starts**: confirm T13 should use `context7` for `@dnd-kit` research (recommended), or proceed without it (web search fallback per the Knowledge Verification Chain if `context7` isn't configured/available in this environment).
