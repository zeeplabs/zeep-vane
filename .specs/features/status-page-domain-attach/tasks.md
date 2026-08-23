# Status Page Domain Attach Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/status-page-domain-attach/design.md`
**Status**: Approved (sub-agent batches confirmed: Batch1 = Phases 1-2 / T1-T9, Batch2 = Phases 3-4 / T10-T15)

---

## Test Coverage Matrix

> Generated from codebase sampling (`internal/api/domains_handler_test.go`, `internal/db/domains_migration_test.go`, `internal/config/config_test.go`, `web/src/test/msw/handlers.ts`, existing `web/src/features/status-pages/*`) plus spec ACs. No `AGENTS.md`/lint-config guideline files found - strong default applied (1:1 to spec ACs, every listed edge case), same depth as `admin-frontend`/`company-settings` tasks.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Go migration (`0013_status_pages_nullable_domain`) | integration | Nullable columns accept `NULL`; partial unique index rejects a duplicate `(domain_id, subdomain)` pair while allowing unlimited rows with `domain_id IS NULL` - SPD-05, SPD-09 | `internal/db/status_pages_migration_test.go` (tag `integration`) | `go test -tags=integration ./internal/db` |
| Go repository (`StatusPageRepository.Create`/`List`, nullable fields) | integration | `Create` with and without domain/subdomain; `List` returns nullable fields correctly - SPD-01, SPD-05 | `internal/db/status_page_repository_test.go` (tag `integration`) | `go test -tags=integration ./internal/db` |
| Go repository (`StatusPageRepository.AttachDomain`) | integration | All 4 distinguishable outcomes: success, not-found, already-attached, invalid-domain, duplicate-pair; race case (2 concurrent attaches, exactly one wins) - SPD-06, SPD-07, SPD-08, SPD-09, edge case | `internal/db/status_page_repository_test.go` (tag `integration`) | `go test -tags=integration ./internal/db` |
| Go config (`PublicDNSTarget`) | unit | Set + unset (default `""`) paths | `internal/config/config_test.go` | `go test ./internal/config` |
| Go handler (`StatusPagesHandler.Create`, relaxed) | integration | Create without domain (201, nulls); create with domain unchanged (201); partial combo (one field only) → 422 - SPD-01, SPD-05 | `internal/api/status_pages_handler_test.go` (tag `integration`) | `go test -tags=integration ./internal/api` |
| Go handler (`StatusPagesHandler.AttachDomain`) | integration | Every status code: 200/404/409(already-attached)/422(empty subdomain)/422(invalid domain)/409(duplicate pair)/403(viewer) - SPD-06, SPD-07, SPD-08, SPD-09, SPD-11 | `internal/api/status_pages_handler_test.go` (tag `integration`) | `go test -tags=integration ./internal/api` |
| Go handler (`InstanceConfigHandler.DNSTarget`) | integration | Configured value returned; unset → `null`; 403 for `viewer` - SPD-10, SPD-11 | `internal/api/instance_config_handler_test.go` (tag `integration`) | `go test -tags=integration ./internal/api` |
| Go handler (`PublicStatusPreviewHandler`, gate removed) | integration | Preview renders for `draft` with `domain_id: null`, `draft` with domain set, and unaffected `published`/`tls_failed`; 404 unchanged for unknown ID - SPD-02, SPD-03, SPD-04 | `internal/api/public_status_preview_handler_test.go` (tag `integration`) | `go test -tags=integration ./internal/api` |
| Go routing wiring (`routes.go`) | integration | Real admin router exposes `PATCH /api/status-pages/{id}/domain` and `GET /api/instance/dns-target` behind `writeRoles` (403 for viewer, 401 no session) - SPD-11 | `internal/cli/routes_test.go` (if present) or a focused test in `internal/api` hitting the real router builder | `go test -tags=integration ./internal/cli` |
| React types (`types/api.ts`) | none | Build gate only | `web/src/types/api.ts` | `cd web && npm run build` |
| MSW handlers (`test/msw/handlers.ts`) | none | Fixture/mock code - covered indirectly by hook/component tests below | `web/src/test/msw/handlers.ts` | `cd web && npm run test` (via consuming tests) |
| React hooks (`status-pages/hooks.ts`) | unit (via MSW) | `useCreateStatusPage` (no domain fields sent), new `useAttachDomain` (happy path + 404/409/422 error cases), new `useDNSTarget` (value + null case) - SPD-01, SPD-06 through SPD-10 | `web/src/features/status-pages/hooks.test.ts` | `cd web && npm run test` |
| React component (`StatusPagesSection.tsx`) | unit/component | Create form has no domain fields; list renders null-safe for domain-less pages | `web/src/features/status-pages/StatusPagesSection.test.tsx` | `cd web && npm run test` |
| React component (`AttachDomainDrawer.tsx`, new) | unit/component | Renders domain picker + subdomain input + DNS target (value and null case); submit success closes drawer; submit error shows inline message - SPD-06 through SPD-10 | `web/src/features/status-pages/AttachDomainDrawer.test.tsx` | `cd web && npm run test` |
| React component (`StatusPageDetail.tsx`, rewritten) | unit/component | 4 distinguishable states render correctly (no-domain / pending / published / tls_failed); preview link always visible regardless of state - SPD-12, SPD-13, SPD-14 | `web/src/features/status-pages/StatusPageDetail.test.tsx` | `cd web && npm run test` |

## Gate Check Commands

> Generated from `Makefile` (`go test ./...`, `gofmt -l .`, `go vet ./...`) and `web/package.json` (`npm run test`, `npm run build`), matching the commands already established in prior features' `tasks.md`.

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

Tasks: T1, T2, T3, T4 - see dependency graph in `## Phase Execution Map` below for exact edges.

### Phase 2: Backend HTTP Surface + Wiring

Tasks: T5, T6, T7, T8, T9 - see dependency graph below.

### Phase 3: Frontend Types + Data Layer

Tasks: T10, T11, T12 - see dependency graph below.

### Phase 4: Frontend UI

Tasks: T13, T14, T15 - see dependency graph below.

---

## Task Breakdown

### T1: Nullable-domain migration with partial unique index

**What**: `internal/db/migrations/0013_status_pages_nullable_domain.up.sql` (drop `NOT NULL` on `domain_id`/`subdomain`, add `CREATE UNIQUE INDEX ... WHERE domain_id IS NOT NULL`) + `.down.sql`, plus a migration test.
**Where**: `internal/db/migrations/0013_status_pages_nullable_domain.up.sql`, `internal/db/migrations/0013_status_pages_nullable_domain.down.sql`, `internal/db/status_pages_migration_test.go`
**Depends on**: None
**Reuses**: `internal/db/migrations/0012_company_settings.up.sql` numbering/style, `internal/db/domains_migration_test.go` structure
**Requirement**: SPD-05, SPD-09

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `MigrateUp` allows inserting a `status_pages` row with `domain_id: NULL, subdomain: NULL`
- [x] Inserting a second row with the same non-null `(domain_id, subdomain)` pair as an existing row fails with a constraint violation
- [x] Inserting two rows with `domain_id: NULL` (any/no subdomain) succeeds - the partial index never blocks domain-less rows
- [x] Gate check passes: `go test -tags=integration ./internal/db`
- [x] Test count: 3+ new tests pass (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): make status_pages domain nullable with partial unique index`

---

### T2: Nullable `StatusPage` model + `Create`/`List` adapted

**What**: `db.StatusPage.DomainID`/`Subdomain` become `*string`; `Create`/`List` read/write the nullable columns correctly.
**Where**: `internal/db/status_page_repository.go`, `internal/db/status_page_repository_test.go`
**Depends on**: T1
**Reuses**: existing `Create`/`List` structure, just widened to nullable
**Requirement**: SPD-01, SPD-05

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `Create` with `DomainID: nil, Subdomain: nil` succeeds, returned row has both `nil`
- [x] `Create` with both set (existing with-domain path) still succeeds unchanged
- [x] `List` returns a mix of domain-less and domained rows with correct nullability
- [x] Existing tests for `Create`/`List` updated to the new pointer types, none weakened or deleted
- [x] Gate check passes: `go test -tags=integration ./internal/db`
- [x] Test count: at least the same count as before this task, plus 2+ new (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): make StatusPage domain_id and subdomain nullable`

---

### T3: `AttachDomain` repository method

**What**: `AttachDomain(ctx, id, domainID, subdomain string) (*StatusPage, error)` - `SELECT ... FOR UPDATE` then conditional `UPDATE`, mapping to `ErrNotFound`, new `ErrDomainAlreadyAttached`, `ErrInvalidDomain` (FK violation), `ErrDuplicateDomainSubdomain` (unique violation).
**Where**: `internal/db/status_page_repository.go` (extend), `internal/db/status_page_repository_test.go` (extend)
**Depends on**: T2
**Reuses**: `pgerrcode` mapping pattern from `internal/db/domain_repository.go:36-49`
**Requirement**: SPD-06, SPD-07, SPD-08, SPD-09

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Attaching to a domain-less page succeeds, returns updated row with `state` unchanged (`"draft"`)
- [x] Attaching to a nonexistent page ID returns `ErrNotFound`
- [x] Attaching to a page that already has a domain returns `ErrDomainAlreadyAttached`, row unmodified
- [x] Attaching with a `domain_id` that doesn't exist returns `ErrInvalidDomain`, row unmodified
- [x] Attaching a `(domain_id, subdomain)` pair already used by another page returns `ErrDuplicateDomainSubdomain`, row unmodified
- [x] Two concurrent `AttachDomain` calls on the same domain-less page: exactly one succeeds, the other gets `ErrDomainAlreadyAttached` (test runs both in goroutines against the real DB)
- [x] Gate check passes: `go test -tags=integration ./internal/db`
- [x] Test count: 6+ new tests pass (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add StatusPageRepository.AttachDomain`

---

### T4: `PUBLIC_DNS_TARGET` config

**What**: Add optional `PublicDNSTarget` field to `config.Config`, read from `PUBLIC_DNS_TARGET`, default `""`.
**Where**: `internal/config/config.go`, `internal/config/config_test.go`
**Depends on**: None
**Reuses**: `CORSAllowedOrigin`/`UploadsDir` optional-var pattern (`config.go:64-78`)
**Requirement**: SPD-10

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `PUBLIC_DNS_TARGET` set → `Config.PublicDNSTarget` reflects it
- [x] `PUBLIC_DNS_TARGET` unset → `Config.PublicDNSTarget == ""`
- [x] Gate check passes: `go test ./internal/config`
- [x] Test count: 2 new tests pass (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(config): add PUBLIC_DNS_TARGET setting`

---

### T5: Relax `StatusPagesHandler.Create` validation

**What**: `Create` no longer requires `domain_id`/`subdomain`; if either is present without the other, `422`; response struct's `DomainID`/`Subdomain` become nullable JSON fields.
**Where**: `internal/api/status_pages_handler.go` (modify), `internal/api/status_pages_handler_test.go` (modify)
**Depends on**: T2
**Reuses**: existing `Create`/`statusPageResponse` structure
**Requirement**: SPD-01, SPD-05

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `POST /api/status-pages` with only `name` (+ optional `service_ids`) → `201`, response has `domain_id: null, subdomain: null`
- [x] `POST /api/status-pages` with `domain_id`+`subdomain` both set (existing path) → `201` unchanged
- [x] `POST /api/status-pages` with only one of `domain_id`/`subdomain` set → `422`
- [x] Existing Create tests updated for the relaxed validation, none weakened or deleted
- [x] Gate check passes: `go test -tags=integration ./internal/api`
- [x] Test count: at least the same as before, plus 2+ new (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): allow creating a status page without a domain`

---

### T6: `StatusPagesHandler.AttachDomain` handler

**What**: `PATCH /api/status-pages/{id}/domain` - decodes `{domain_id, subdomain}`, `422` on empty fields, maps repository errors to `404`/`409`/`422`/`409`, else `200` + updated `statusPageResponse`.
**Where**: `internal/api/status_pages_handler.go` (extend), `internal/api/status_pages_handler_test.go` (extend)
**Depends on**: T3
**Reuses**: `statusPageCreatorLister`-style narrow interface, `statusPageResponse`, `writeInternalError`
**Requirement**: SPD-06, SPD-07, SPD-08, SPD-09

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Valid attach on a domain-less page → `200`, response reflects new `domain_id`/`subdomain`
- [x] Attach on already-domained page → `409`
- [x] Attach with empty `subdomain` → `422`
- [x] Attach with a nonexistent `domain_id` → `422`
- [x] Attach with a `(domain_id, subdomain)` pair already used elsewhere → `409`
- [x] Attach on a nonexistent status page ID → `404`
- [x] Gate check passes: `go test -tags=integration ./internal/api`
- [x] Test count: 6+ new tests pass (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): add status page domain attach endpoint`

---

### T7: `InstanceConfigHandler.DNSTarget`

**What**: `GET` handler returning `{"target": "<value>"}` or `{"target": null}`.
**Where**: `internal/api/instance_config_handler.go`, `internal/api/instance_config_handler_test.go`
**Depends on**: T4
**Reuses**: nothing existing (smallest possible new handler)
**Requirement**: SPD-10

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Configured value → `200` with `{"target": "<configured value>"}`
- [x] Unconfigured (`""`) → `200` with `{"target": null}`
- [x] Gate check passes: `go test -tags=integration ./internal/api`
- [x] Test count: 2+ new tests pass (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): add instance DNS target read endpoint`

---

### T8: Remove `published`-only gate from preview handler

**What**: `PublicStatusPreviewHandler.Get` no longer 404s when `state != "published"`; comment rewritten to reflect `AD-008` instead of the old "mirrors production" rationale.
**Where**: `internal/api/public_status_preview_handler.go` (modify), `internal/api/public_status_preview_handler_test.go` (modify)
**Depends on**: None
**Reuses**: existing `composeResponse` call, unchanged 404-on-unknown-ID branch
**Requirement**: SPD-02, SPD-03, SPD-04

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Preview for a `draft` page with `domain_id: null` → `200`
- [x] Preview for a `draft` page with a domain attached → `200` (previously `404`)
- [x] Preview for `published`/`tls_failed` pages unaffected (`200`)
- [x] Preview for a nonexistent status page ID → `404` unchanged
- [x] Gate check passes: `go test -tags=integration ./internal/api`
- [x] Test count: 2+ new tests pass, 0 existing tests deleted (the old `404`-on-draft test is REWRITTEN to assert `200`, not deleted - document this in the commit)

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): stop gating status page preview on published state`

---

### T9: Wire new routes into admin router

**What**: Register `PATCH /api/status-pages/{id}/domain` and `GET /api/instance/dns-target` under `writeRoles` in `buildAdminRouter`; instantiate `InstanceConfigHandler` with `cfg.PublicDNSTarget`.
**Where**: `internal/cli/routes.go` (modify)
**Depends on**: T6, T7
**Reuses**: `writeRoles` group (`routes.go:48`), existing route-registration style
**Requirement**: SPD-11

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `PATCH /api/status-pages/{id}/domain` reachable on the real admin router, `403` for `viewer`, `401` with no session
- [x] `GET /api/instance/dns-target` same RBAC
- [x] Gate check passes: `go test -tags=integration ./internal/cli`
- [x] Test count: 2+ new tests pass (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(cli): wire status page domain attach and dns-target routes`

---

### T10: Nullable domain types in frontend

**What**: `StatusPage.domain_id`/`subdomain` become `string | null` in `types/api.ts`.
**Where**: `web/src/types/api.ts`
**Depends on**: None
**Reuses**: existing `StatusPage` interface
**Requirement**: SPD-01, SPD-05

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `StatusPage.domain_id: string | null`, `StatusPage.subdomain: string | null`
- [x] Gate check passes: `cd web && npm run build` (type errors at every read site are EXPECTED here and fixed in T13/T15 - this task only changes the type)

**Tests**: none
**Gate**: build

**Commit**: `feat(web): make StatusPage domain fields nullable in types`

---

### T11: MSW handlers for domain attach + DNS target

**What**: Update `POST /api/status-pages` mock to accept a domain-less body; add `PATCH /api/status-pages/:id/domain` and `GET /api/instance/dns-target` mock handlers, backed by in-memory state + reset functions.
**Where**: `web/src/test/msw/handlers.ts` (modify), `web/src/test/setup.ts` (modify, if it centralizes reset calls)
**Depends on**: T10
**Reuses**: `resetDomainsAndStatusPages`/`statusPagesState` pattern (`handlers.ts:46-60`)
**Requirement**: SPD-01, SPD-06 through SPD-10

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `POST /api/status-pages` without domain fields returns a page with `domain_id: null, subdomain: null`
- [x] `PATCH /api/status-pages/:id/domain` mirrors the 4 backend outcomes (200/404/409/422) by status code
- [x] `GET /api/instance/dns-target` returns a configurable mock value or `null`
- [x] Gate check passes: `cd web && npm run test`
- [x] Test count: existing suite still green - no regressions from the handler change alone

**Tests**: none (fixture code; covered indirectly by T12-T15)
**Gate**: quick

**Commit**: `test(msw): add status page domain attach and dns-target handlers`

---

### T12: `status-pages/hooks.ts` - drop domain fields, add attach/DNS-target hooks

**What**: `useCreateStatusPage`'s input drops `domain_id`/`subdomain`; add `useAttachDomain()` and `useDNSTarget()`.
**Where**: `web/src/features/status-pages/hooks.ts` (modify), `web/src/features/status-pages/hooks.test.ts` (new)
**Depends on**: T10, T11
**Reuses**: existing `useMutation`/`useQuery` + `apiFetch` pattern
**Requirement**: SPD-01, SPD-06 through SPD-10

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `useCreateStatusPage` sends a body with no domain fields
- [x] `useAttachDomain` happy path updates the status page query cache; 404/409/422 cases surface as `ApiError`
- [x] `useDNSTarget` returns the configured value or `null`
- [x] Gate check passes: `cd web && npm run test`
- [x] Test count: 6+ new tests pass (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(web): add domain attach and dns-target hooks`

---

### T13: `StatusPagesSection.tsx` - domain-less create form

**What**: Remove domain/subdomain fields from the create form; list rendering becomes null-safe for `domain_id`/`subdomain`.
**Where**: `web/src/features/status-pages/StatusPagesSection.tsx` (modify), `web/src/features/status-pages/StatusPagesSection.test.tsx` (new or modified, per existing test coverage)
**Depends on**: T12
**Reuses**: existing form/table structure
**Requirement**: SPD-01

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Create form has no domain/subdomain inputs; submitting creates a domain-less page
- [ ] List row for a domain-less page renders without a broken URL (no literal `"https://null.undefined"`)
- [ ] Gate check passes: `cd web && npm run test`
- [ ] Test count: 2+ new/modified tests pass (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(web): remove domain fields from status page create form`

---

### T14: `AttachDomainDrawer.tsx`

**What**: New `Drawer`-based component: domain picker (`useDomains`) + subdomain input + DNS target display (`useDNSTarget`) + submit (`useAttachDomain`).
**Where**: `web/src/features/status-pages/AttachDomainDrawer.tsx`, `web/src/features/status-pages/AttachDomainDrawer.test.tsx`
**Depends on**: T12
**Reuses**: `Drawer` component pattern already used for "Criar status page"/"Criar incidente"
**Requirement**: SPD-06 through SPD-10

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Renders domain picker populated from `useDomains`, subdomain text input, DNS target value or a "not configured" note when `null`
- [ ] Successful submit closes the drawer and the parent page reflects the new domain
- [ ] 404/409/422 responses render an inline error, drawer stays open
- [ ] Gate check passes: `cd web && npm run test`
- [ ] Test count: 4+ new tests pass (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(web): add attach domain drawer`

---

### T15: `StatusPageDetail.tsx` - 4 distinguishable states

**What**: Rewrite state rendering into 4 distinguishable cases (no domain / domain pending / published / tls_failed); preview link always visible regardless of state; "Anexar domínio" action opens `AttachDomainDrawer` when `domain_id` is null.
**Where**: `web/src/features/status-pages/StatusPageDetail.tsx` (modify), `web/src/features/status-pages/StatusPageDetail.test.tsx` (new or modified)
**Depends on**: T12, T14
**Reuses**: existing `Tag` component, `AttachDomainDrawer` (T14)
**Requirement**: SPD-02, SPD-12, SPD-13, SPD-14

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `domain_id: null` → "Sem domínio configurado" label + button opening `AttachDomainDrawer`
- [ ] `domain_id` set + `state == "draft"` → "Aguardando validação de DNS/certificado" label (not the old ambiguous text)
- [ ] `state == "published"` → unchanged existing rendering
- [ ] `state == "tls_failed"` → unchanged existing rendering
- [ ] Preview link ("Pré-visualizar página pública") renders regardless of state/domain (moved out of the `published`-only block)
- [ ] Gate check passes: `cd web && npm run test`
- [ ] Test count: 4+ new/modified tests pass (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(web): show distinguishable domain states and always-visible preview link`

---

## Phase Execution Map

This is the authoritative dependency graph - every edge below matches a task's `Depends on` field exactly (see the Diagram-Definition Cross-Check table):

```
T1 -> T2 -> T3
T2 -> T5
T3 -> T6
T4 -> T7
T6 -> T9
T7 -> T9
T10 -> T11 -> T12
T10 -> T12
T12 -> T13
T12 -> T14
T12 -> T15
T14 -> T15
```

Execution is strictly sequential within and across phases in task-number order (T1..T15) - there is no intra-phase parallelism. T8 has no dependencies and no dependents (isolated node - runs in its Phase 2 slot).

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: Nullable-domain migration | 1 migration pair + 1 test file | ✅ Granular |
| T2: Nullable `StatusPage` model | 1 repository, 2 methods widened | ✅ Granular |
| T3: `AttachDomain` repository method | 1 new method on existing struct | ✅ Granular |
| T4: `PUBLIC_DNS_TARGET` config | 1 config field | ✅ Granular |
| T5: Relax `Create` validation | 1 handler method | ✅ Granular |
| T6: `AttachDomain` handler | 1 new handler method | ✅ Granular |
| T7: `InstanceConfigHandler.DNSTarget` | 1 new handler, 1 method | ✅ Granular |
| T8: Remove preview gate | 1 file, 1 conditional removed | ✅ Granular |
| T9: Wire routes | 1 file, route registration only | ✅ Granular |
| T10: Nullable frontend types | 1 file, 2 fields | ✅ Granular |
| T11: MSW handlers | 1 file (fixture/mock layer) | ✅ Granular |
| T12: `status-pages/hooks.ts` update | 1 file, 3 hooks | ✅ Granular |
| T13: `StatusPagesSection.tsx` | 1 component | ✅ Granular |
| T14: `AttachDomainDrawer.tsx` | 1 new component | ✅ Granular |
| T15: `StatusPageDetail.tsx` | 1 component | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | No incoming edge | ✅ Match |
| T2 | T1 | T1 -> T2 | ✅ Match |
| T3 | T2 | T2 -> T3 | ✅ Match |
| T4 | None | No incoming edge | ✅ Match |
| T5 | T2 | T2 -> T5 | ✅ Match |
| T6 | T3 | T3 -> T6 | ✅ Match |
| T7 | T4 | T4 -> T7 | ✅ Match |
| T8 | None | No incoming edge (isolated node) | ✅ Match |
| T9 | T6, T7 | T6 -> T9, T7 -> T9 | ✅ Match |
| T10 | None | No incoming edge | ✅ Match |
| T11 | T10 | T10 -> T11 | ✅ Match |
| T12 | T10, T11 | T10 -> T12, T11 -> T12 | ✅ Match |
| T13 | T12 | T12 -> T13 | ✅ Match |
| T14 | T12 | T12 -> T14 | ✅ Match |
| T15 | T12, T14 | T12 -> T15, T14 -> T15 | ✅ Match |

Every edge in the `## Phase Execution Map` graph corresponds to exactly one `Depends on` entry above, and vice versa. No task depends on a later-phase task - all dependencies point backward or within the same phase.

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1: Migration | Go migration | integration | integration | ✅ OK |
| T2: StatusPage model | Go repository | integration | integration | ✅ OK |
| T3: AttachDomain method | Go repository | integration | integration | ✅ OK |
| T4: Config | Go config | unit | unit | ✅ OK |
| T5: Create validation | Go handler | integration | integration | ✅ OK |
| T6: AttachDomain handler | Go handler | integration | integration | ✅ OK |
| T7: InstanceConfigHandler | Go handler | integration | integration | ✅ OK |
| T8: Preview gate removal | Go handler | integration | integration | ✅ OK |
| T9: Route wiring | Go routing wiring | integration | integration | ✅ OK |
| T10: Frontend types | React types | none | none | ✅ OK |
| T11: MSW handlers | MSW fixture code | none | none | ✅ OK |
| T12: status-pages hooks | React hooks | unit | unit | ✅ OK |
| T13: StatusPagesSection | React component | unit/component | unit | ✅ OK |
| T14: AttachDomainDrawer | React component | unit/component | unit | ✅ OK |
| T15: StatusPageDetail | React component | unit/component | unit | ✅ OK |

No violations - every task's `Tests` field matches its layer's matrix requirement.

---

## Tips

- **Phases are ordered** - Each phase completes before the next; tasks run in order within a phase
- **Reuses = Token saver** - Always reference existing code
- **One commit per task** - commit messages listed above
