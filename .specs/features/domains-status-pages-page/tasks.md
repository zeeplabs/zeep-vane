# Domínios & Status Pages Page Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/domains-status-pages-page/design.md`
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase (`AGENTS.md` §3, `Makefile`'s `test-integration` target) and spec. Guidelines found: `AGENTS.md` (repo root), sampled `internal/db/status_page_repository_test.go`, `internal/api/domains_handler_test.go`, `web/src/features/services/ServiceListPage.test.tsx`, `web/src/features/services/AddServiceDrawer.test.tsx`.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Repository (`internal/db`) | integration | Every new/changed query path, incl. zero-IDs and zero-rows-attached cases | `internal/db/*_test.go` (`//go:build integration`) | `make test-integration` |
| Handler (`internal/api`) | integration | Every route this feature touches: happy path + every listed edge case + error paths (404/409/422) | `internal/api/*_handler_test.go` (`//go:build integration`) | `make test-integration` |
| React Query hooks (`web/src/features/**/hooks.ts`) | unit | Query key, success + error paths, cache invalidation on mutation success | `web/src/features/**/hooks.test.ts` | `npx vitest run <path>` |
| React components (tables/drawers) | unit | 1:1 to spec ACs for that component; every listed edge case with a DOM assertion | `web/src/features/**/*.test.tsx` | `npx vitest run <path>` |
| Types/config (`web/src/types/api.ts`) | none | Build/typecheck gate only | - | `npx tsc -b --noEmit` |

## Gate Check Commands

> Generated from `AGENTS.md` §3 and `Makefile`.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick (frontend) | After a frontend-only task | `npx tsc -b --noEmit && npx vitest run <changed test file(s)>` |
| Full (backend) | After a task touching `internal/db` or `internal/api` | `go build ./... && go vet ./... && gofmt -l <changed files> && make test-integration` |
| Build (phase completion) | After the last task of each phase | Full backend gate **and** `npx tsc -b --noEmit && npm run test` (whole suite) |

---

## Execution Plan

### Phase 1: Backend join

```
T1 → T2
```

### Phase 2: Frontend type/hook drift fix

```
T3 → T4
```

### Phase 3: Domínios tab UI

```
T5 → T6 → T7 → T8
```

### Phase 4: Status Pages tab UI

```
T9 → T10 → T11
```

### Phase 5: Top-level integration

```
T12
```

---

## Task Breakdown

### T1: Add `StatusPageRepository.AttachedNamesByDomainIDs`

**What**: New batched query method returning, per domain ID, the ordered list of attached status page names.
**Where**: `internal/db/status_page_repository.go`
**Depends on**: None
**Reuses**: `ANY($1)` batching idiom (e.g. `serviceIDsByStatusPage`)
**Requirement**: DSP-03, DSP-04

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `AttachedNamesByDomainIDs(ctx, domainIDs []string) (map[string][]string, error)` implemented, ordered `created_at ASC` per domain
- [x] Empty `domainIDs` slice returns an empty map, no query executed, no error
- [x] Integration tests: zero domains attached (key absent from map), one attached, two-or-more attached (order asserted)
- [x] `make test-integration` passes

**Tests**: integration
**Gate**: full

---

### T2: Extend `DomainsHandler.List`/`domainResponse` with attached-page fields

**What**: Wire `AttachedNamesByDomainIDs` into `List`, adding `attached_page_name`/`attached_page_count` to `domainResponse`.
**Where**: `internal/api/domains_handler.go`
**Depends on**: T1
**Reuses**: Existing `toDomainResponse`, `domainCreatorLister` interface pattern
**Requirement**: DSP-02, DSP-03, DSP-04

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] New narrow interface (`statusPageNameLister`) added to `DomainsHandler`'s dependencies, backed by `*db.StatusPageRepository` in production wiring
- [x] `domainResponse` gains `AttachedPageName *string` (`json:"attached_page_name"`) and `AttachedPageCount int` (`json:"attached_page_count"`)
- [x] `toDomainResponse` (or its caller) sets `attached_page_name` to nil and `attached_page_count` to 0 when a domain has no attached pages; to the first name (by `created_at`) and the full count otherwise
- [x] Integration tests: 0/1/2+ attached pages per domain, asserted via `GET /api/domains` response body
- [x] `make test-integration` passes

**Tests**: integration
**Gate**: full

**Commit**: `feat(domains): expose attached status page name/count in domain list`

---

### T3: Fix `Domain` frontend type + `useDomains` response envelope

**What**: Bring `web/src/types/api.ts`'s `Domain` interface up to date with the real `domainResponse` shape (design.md's flagged drift), and give `useDomains` its own response type instead of the generic `Page<Domain>`.
**Where**: `web/src/types/api.ts`, `web/src/features/domains/hooks.ts`, `web/src/test/msw/handlers.ts`
**Depends on**: T2 (needs the real field names to match)
**Reuses**: `paginatedPage()` MSW helper
**Requirement**: DSP-01, DSP-02, DSP-03, DSP-04

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `Domain` gains `domain_type`, `status` (`DomainStatus`), `ssl_status` (`DomainSSLStatus`), `verified_at`, `last_error`, `attached_page_name`, `attached_page_count`
- [x] `useDomains` types its query result as a bespoke `DomainsPageResponse` (`items`/`total`/`page`/`page_size`/`dns_target`) instead of `Page<Domain>`
- [x] MSW domain fixture/handler updated to return every new field (mirrors backend response exactly, per AGENTS.md §5)
- [x] `hooks.test.ts` updated/extended asserting the new fields round-trip
- [x] `npx tsc -b --noEmit` clean; `npx vitest run web/src/features/domains/hooks.test.ts` passes

**Tests**: unit
**Gate**: quick

---

### T4: Add `useRecheckDomain` hook

**What**: New mutation hook for `POST /api/domains/{id}/verify`, named distinctly from `status-pages/hooks.ts`'s existing `useVerifyDomain` (different endpoint).
**Where**: `web/src/features/domains/hooks.ts`
**Depends on**: T3
**Reuses**: `useDeleteDomain`'s mutation/invalidation pattern in the same file

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `useRecheckDomain()` posts to `/api/domains/{id}/verify`, invalidates `["domains"]` on success
- [x] Unit test: success path invalidates cache; error path surfaces `ApiError`
- [x] `npx vitest run web/src/features/domains/hooks.test.ts` passes

**Tests**: unit
**Gate**: quick

**Commit**: `fix(domains): fix Domain type drift, add useRecheckDomain hook`

---

### T5: Domain status/SSL badge helpers

**What**: Small shared metadata module mapping `DomainStatus`/`DomainSSLStatus` to `Tag` variant + label + dot color, mirroring `statusMeta.ts`'s pattern for services.
**Where**: `web/src/features/domains/domainStatusMeta.ts` (new)
**Depends on**: T3
**Reuses**: `web/src/features/services/statusMeta.ts` (same shape: `Record<Status, ...>` maps), `Tag`'s `TagVariant`

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `domainStatusLabel`/`domainStatusVariant`/`domainStatusDotColor` (verified/pending/error) and `sslStatusLabel`/`sslStatusColor` (active/pending/error) exported
- [ ] Unit test asserting all 3 domain-status keys and all 3 SSL-status keys are present (prevents a future 4th status silently falling through)
- [ ] `npx vitest run web/src/features/domains/domainStatusMeta.test.ts` passes

**Tests**: unit
**Gate**: quick

---

### T6: `DomainsTable` component

**What**: Table rendering one row per domain (Status/Domínio/Tipo/Aponta para/SSL/Verificado), row click selects a domain, `Pager` wired, `EmptyState` on zero domains.
**Where**: `web/src/features/domains/DomainsTable.tsx` (new)
**Depends on**: T4, T5
**Reuses**: `Pager`, `EmptyState`, `Tag`, `domainStatusMeta.ts`, `formatTimestamp` pattern from `DomainsSection.tsx`
**Requirement**: DSP-01, DSP-02, DSP-03, DSP-04, DSP-16

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Renders all 6 columns per spec.md DSP-01, including "+N" formatting (DSP-04) and "—" for null verified_at (DSP-16) / zero attached pages (DSP-02)
- [ ] Row click calls an `onSelect(domain)` prop
- [ ] Zero domains renders `EmptyState`, not an empty table
- [ ] Unit tests cover: 0/1/2+ attached pages per row, null verified_at, empty list
- [ ] `npx vitest run web/src/features/domains/DomainsTable.test.tsx` passes

**Tests**: unit
**Gate**: quick

---

### T7: `DomainDetailDrawer` component

**What**: Read-only detail drawer for a selected domain: status pill, type + target line, error banner, SSL/Verified-at fields, DNS config (CNAME) for custom domains, "Verificar novamente"/"Remover domínio" actions.
**Where**: `web/src/features/domains/DomainDetailDrawer.tsx` (new)
**Depends on**: T6
**Reuses**: `Drawer`, `useRecheckDomain`, `useDeleteDomain`, `domainStatusMeta.ts`
**Requirement**: DSP-05, DSP-06, DSP-07, DSP-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Renders per DSP-05 (status pill, error banner when `last_error` non-null, SSL/Verified-at, DNS config block for `domain_type === "custom"`)
- [ ] "Verificar novamente" calls `useRecheckDomain`, drawer reflects the returned state (DSP-06)
- [ ] "Remover domínio" calls `useDeleteDomain`; on success closes the drawer and the row disappears from the table (DSP-07); on 409 shows an inline error and stays open (DSP-08)
- [ ] Unit tests cover all four `Done when` behaviors above
- [ ] `npx vitest run web/src/features/domains/DomainDetailDrawer.test.tsx` passes

**Tests**: unit
**Gate**: quick

---

### T8: `AddDomainDrawer` component

**What**: Add-domain drawer with the two-tile type chooser ("Subdomínio Vane" disabled, "Domínio próprio" selected/functional) and hostname submission.
**Where**: `web/src/features/domains/AddDomainDrawer.tsx` (new)
**Depends on**: T7
**Reuses**: `Drawer`, `Field`, `useCreateDomain`, disabled-tile visual pattern from `AddServiceDrawer.tsx`'s `ModeCard`
**Requirement**: DSP-09, DSP-10, DSP-11, DSP-12

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Two tiles rendered, "Domínio próprio" default-selected (DSP-09)
- [ ] "Subdomínio Vane" tile has no `onClick`, `aria-disabled="true"`, clicking it doesn't change selection (DSP-10)
- [ ] Hostname input submits via `useCreateDomain` (DSP-11)
- [ ] 409 duplicate-hostname shows inline error, matching `DomainsSection`'s existing copy (DSP-12)
- [ ] Unit tests cover all four ACs above
- [ ] `npx vitest run web/src/features/domains/AddDomainDrawer.test.tsx` passes

**Tests**: unit
**Gate**: quick

**Commit**: `feat(domains): add DomainsTable/DomainDetailDrawer/AddDomainDrawer (Domínios tab)`

---

### T9: `StatusPagesTable` component

**What**: Table rendering one row per status page (Visib./Página/URL pública/Serviços/Atualizado).
**Where**: `web/src/features/status-pages/StatusPagesTable.tsx` (new)
**Depends on**: T4 (phase-ordering only - no direct code dependency, but Phase 3 must finish first per the execution plan)
**Reuses**: `Pager`, `EmptyState`, `Tag`, existing `useStatusPages`
**Requirement**: DSP-13, DSP-17

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Renders all 5 columns per DSP-13; Visib. always shows "Público" (Out of Scope: visibility toggle is decorative)
- [ ] URL pública renders "—" when the page has no `domain_id` (DSP-17)
- [ ] Row click calls an `onSelect(page)` prop
- [ ] Zero pages renders `EmptyState`
- [ ] Unit tests cover: page with domain, page without domain (DSP-17), empty list
- [ ] `npx vitest run web/src/features/status-pages/StatusPagesTable.test.tsx` passes

**Tests**: unit
**Gate**: quick

---

### T10: `StatusPageDetailDrawer` component

**What**: Read-only detail drawer: attached services list, linked domain, "Ver página pública" (disabled/absent if no domain), "Editar página" linking to `/status-pages/{id}`.
**Where**: `web/src/features/status-pages/StatusPageDetailDrawer.tsx` (new)
**Depends on**: T9
**Reuses**: `Drawer`, `Link` (react-router)
**Requirement**: DSP-14, DSP-17

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Renders service list + domain (or "—") per DSP-14
- [ ] "Editar página" is a `Link` to `/status-pages/{id}` (DSP-14 per spec's Assumptions decision)
- [ ] "Ver página pública" absent/disabled when no domain attached (DSP-17)
- [ ] Unit tests cover: with domain (link present + correct href), without domain (link absent, "—" shown)
- [ ] `npx vitest run web/src/features/status-pages/StatusPageDetailDrawer.test.tsx` passes

**Tests**: unit
**Gate**: quick

---

### T11: `AddStatusPageDrawer` component

**What**: Add-page drawer: name field, visibility chooser ("Público" selected/functional, "Privado" disabled), service checklist, submit via existing `useCreateStatusPage`.
**Where**: `web/src/features/status-pages/AddStatusPageDrawer.tsx` (new)
**Depends on**: T10
**Reuses**: `Drawer`, `Field`, `useCreateStatusPage`, `useServices` (checklist source), disabled-tile pattern from T8
**Requirement**: DSP-15

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] "Público" tile default-selected/functional; "Privado" tile disabled, no `onClick`, `aria-disabled="true"` (DSP-15, Out of Scope)
- [ ] Name field + service checklist submit via `useCreateStatusPage`
- [ ] Unit tests cover: default selection, disabled tile non-interactive, successful submit calls the mutation with checked services
- [ ] `npx vitest run web/src/features/status-pages/AddStatusPageDrawer.test.tsx` passes

**Tests**: unit
**Gate**: quick

**Commit**: `feat(status-pages): add StatusPagesTable/StatusPageDetailDrawer/AddStatusPageDrawer (Status Pages tab)`

---

### T12: Rewrite `DomainsStatusPagesPage` with tabs

**What**: Replace the flat `DomainsSection`+`StatusPagesSection` stack with the tabbed layout wiring every component from T6-T11.
**Where**: `web/src/features/domains/DomainsStatusPagesPage.tsx` (rewrite)
**Depends on**: T8, T11
**Reuses**: Everything above; `DomainsSection`/`StatusPagesSection` themselves stay untouched (still used by `IntegrationsPage`/wherever else, per design.md's "genuinely different layout" precedent)
**Requirement**: DSP-18, DSP-19 (edge cases), full spec end-to-end

**Tools**:
- MCP: `mcp__plugin_playwright_playwright__*` (live visual verification against the dev server, matching how `monitored-services-page`/`manual-polling-monitoring` were verified)
- Skill: NONE

**Done when**:
- [ ] Two-tab switcher (Domínios/Status Pages) renders both tables, "Adicionar domínio"/"Criar status page" button label switches with the active tab
- [ ] Switching tabs closes any open detail/add drawer from the previous tab (DSP-19)
- [ ] `App.test.tsx` (or a new page-level test) smoke-tests the route renders without crashing
- [ ] Live Playwright check against the dev server: both tabs, both add-drawers, both detail-drawers, dark mode (per this session's earlier contrast fix — confirm no white-on-white regression)
- [ ] Full build gate: `go build/vet/test`, `gofmt -l`, `make test-integration`, `npx tsc -b --noEmit`, `npm run test` (whole suite) all clean

**Tests**: unit + manual (Playwright)
**Gate**: build

**Commit**: `feat(domains): redesign Domínios & Status Pages page to tabbed layout`

---

## Phase Execution Map

```
Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5

Phase 1:  T1 → T2
Phase 2:  T3 → T4
Phase 3:  T5 → T6 → T7 → T8
Phase 4:  T9 → T10 → T11
Phase 5:  T12

Cross-phase edges (same rule as within a phase - every `Depends on` has a matching arrow here):
T2 → T3
T3 → T5
T4 → T6
T4 → T9
T8 → T12
T11 → T12
```

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: `AttachedNamesByDomainIDs` | 1 method, 1 file | ✅ Granular |
| T2: `DomainsHandler.List` extension | 1 file (handler + response type) | ✅ Granular |
| T3: `Domain` type + `useDomains` fix | 2-3 related files, one cohesive drift-fix | ✅ OK (cohesive) |
| T4: `useRecheckDomain` | 1 hook, 1 file | ✅ Granular |
| T5: `domainStatusMeta.ts` | 1 file | ✅ Granular |
| T6: `DomainsTable` | 1 component | ✅ Granular |
| T7: `DomainDetailDrawer` | 1 component | ✅ Granular |
| T8: `AddDomainDrawer` | 1 component | ✅ Granular |
| T9: `StatusPagesTable` | 1 component | ✅ Granular |
| T10: `StatusPageDetailDrawer` | 1 component | ✅ Granular |
| T11: `AddStatusPageDrawer` | 1 component | ✅ Granular |
| T12: `DomainsStatusPagesPage` rewrite | 1 file (composition only - all logic lives in T6-T11's components) | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | (start of Phase 1) | ✅ Match |
| T2 | T1 | T1 → T2 | ✅ Match |
| T3 | T2 | (start of Phase 2, after Phase 1) | ✅ Match |
| T4 | T3 | T3 → T4 | ✅ Match |
| T5 | T3 | (start of Phase 3, after Phase 2) | ✅ Match |
| T6 | T4, T5 | T5 → T6 (T4 satisfied by phase order) | ✅ Match |
| T7 | T6 | T6 → T7 | ✅ Match |
| T8 | T7 | T7 → T8 | ✅ Match |
| T9 | T4 (phase-order only) | (start of Phase 4, after Phase 3) | ✅ Match |
| T10 | T9 | T9 → T10 | ✅ Match |
| T11 | T10 | T10 → T11 | ✅ Match |
| T12 | T8, T11 | (start of Phase 5, after Phase 3 and Phase 4) | ✅ Match |

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1 | Repository | integration | integration | ✅ OK |
| T2 | Handler | integration | integration | ✅ OK |
| T3 | Hooks + types | unit (hooks); none (types) — highest wins | unit | ✅ OK |
| T4 | Hooks | unit | unit | ✅ OK |
| T5 | Component-adjacent metadata module | unit | unit | ✅ OK |
| T6 | Component | unit | unit | ✅ OK |
| T7 | Component | unit | unit | ✅ OK |
| T8 | Component | unit | unit | ✅ OK |
| T9 | Component | unit | unit | ✅ OK |
| T10 | Component | unit | unit | ✅ OK |
| T11 | Component | unit | unit | ✅ OK |
| T12 | Component (composition) + full-suite gate | unit + build | unit + manual, gate: build | ✅ OK |

---

## Task Verification Standards

Every task's `Done when` entries are specific and binary pass/fail, each citing the gate command from **Gate Check Commands** and (where applicable) an expected test-count delta implicit in "unit tests cover: [enumerated cases]" — checked against the actual new test count at Execute time, not assumed.
