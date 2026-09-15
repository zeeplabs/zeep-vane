# Domínios & Status Pages Page Validation

**Date**: 2026-09-15
**Spec**: `.specs/features/domains-status-pages-page/spec.md`
**Diff range**: `c3a5f24..5604d59` (12 commits, all on `develop`, all local/unpushed)
**Verifier**: independent sub-agent (author ≠ verifier)

---

## Task Completion

| Task | Status  | Notes |
| ---- | ------- | ----- |
| T1   | ✅ Done | `AttachedNamesByDomainIDs` implemented + tested |
| T2   | ✅ Done | `DomainsHandler.List` extended, tested |
| T3   | ✅ Done | `Domain` type + `useDomains` response envelope fixed |
| T4   | ✅ Done | `useRecheckDomain` added |
| T5   | ✅ Done | `domainStatusMeta.ts` added |
| T6   | ✅ Done | `DomainsTable` added |
| T7   | ✅ Done | `DomainDetailDrawer` added |
| T8   | ✅ Done | `AddDomainDrawer` added |
| T9   | ✅ Done | `StatusPagesTable` added |
| T10  | ✅ Done | `StatusPageDetailDrawer` added |
| T11  | ✅ Done | `AddStatusPageDrawer` added |
| T12  | ⚠️ Partial | Live Playwright check explicitly skipped (documented reason: no dev-server credentials); build gate otherwise clean |

---

## Spec-Anchored Acceptance Criteria

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| DSP-01: Domínios tab renders 6 columns (Status, Domínio, Tipo, Aponta para, SSL, Verificado) | All 6 fields rendered per row | `web/src/features/domains/DomainsTable.test.tsx:60` (Domínio), `:73/84` (Aponta para, DSP-02/03), `:106` (Verificado, DSP-16) assert values; **no assertion targets the Status pill text/variant or "Domínio próprio" (Tipo) or the SSL label anywhere in this file** | ⚠️ Partial GAP — Status/Tipo/SSL columns are rendered (visually confirmed in code) but have zero test-level assertions in `DomainsTable.test.tsx` |
| DSP-02: zero attached pages → "—" | Render "—" | `internal/db/status_page_repository_test.go:841` `TestAttachedNamesByDomainIDs_ZeroAttached_KeyAbsentFromMap`; `internal/api/domains_handler_test.go:483` `got.AttachedPageName != nil` want nil, count 0; `web/src/features/domains/DomainsTable.test.tsx:56-62` `expect(within(row).getByText("—"))` | ✅ PASS |
| DSP-03: exactly one attached page → that page's name | Name shown, count 1 | `internal/db/status_page_repository_test.go:857` `TestAttachedNamesByDomainIDs_OneAttached_ReturnsSingleName`; `internal/api/domains_handler_test.go:508` `*got.AttachedPageName != "One Page Status"` / `AttachedPageCount != 1`; `web/src/features/domains/DomainsTable.test.tsx` (DSP-03 test, name asserted) | ✅ PASS |
| DSP-04: 2+ attached pages → earliest name + ` +N` | Earliest-created name, N = count−1 | `internal/db/status_page_repository_test.go:885` `TestAttachedNamesByDomainIDs_TwoOrMoreAttached_OrderedByCreatedAt` (order asserted); `internal/api/domains_handler_test.go:543` `*got.AttachedPageName != "First Attached Page"` / `AttachedPageCount != 2`; `web/src/features/domains/DomainsTable.tsx:18-22` `attachedPageColumn` (`extra = count - 1`), asserted in `DomainsTable.test.tsx` ("+N" test) | ✅ PASS |
| DSP-05: row click opens detail drawer (status pill, type+target, error banner, SSL/Verified-at, DNS block for custom, actions) | All fields present when applicable | `web/src/features/domains/DomainDetailDrawer.test.tsx:85-87` asserts error text, "Configuração de DNS", "Erro" all present | ✅ PASS |
| DSP-06: "Verificar novamente" calls verify, refreshes drawer | Drawer reflects returned state | `web/src/features/domains/DomainDetailDrawer.test.tsx:110` `expect(screen.getByText("Ativo")).toBeInTheDocument()` after mutation resolves | ✅ PASS |
| DSP-07: "Remover domínio" success closes drawer + removes row | Drawer closed, row gone | `web/src/features/domains/DomainDetailDrawer.test.tsx:129-130` both assertions present | ✅ PASS |
| DSP-08: 409 on delete → inline error, drawer stays open | Error shown, drawer open | `web/src/features/domains/DomainDetailDrawer.test.tsx:146-147` `toHaveTextContent(...)`, button still present (drawer still open) | ✅ PASS |
| DSP-09: two tiles, "Domínio próprio" default | Correct default selection | `web/src/features/domains/AddDomainDrawer.test.tsx:34-35` `aria-pressed` true/false on the two tiles | ✅ PASS |
| DSP-10: "Subdomínio Vane" disabled, no onClick, click doesn't change selection | Non-interactive | `web/src/features/domains/AddDomainDrawer.test.tsx:43,50-51` `aria-disabled="true"`, selection unchanged after click | ✅ PASS (confirmed discriminating — sensor mutation 3 killed) |
| DSP-11: hostname submit via `POST /api/domains` | New domain appears | `web/src/features/domains/AddDomainDrawer.test.tsx:61-64` new hostname present after refetch | ✅ PASS |
| DSP-12: 409 duplicate hostname → inline error | Exact error text shown | `web/src/features/domains/AddDomainDrawer.test.tsx:78` `toHaveTextContent("hostname already registered")` | ✅ PASS |
| DSP-13: Status Pages tab 5 columns | All 5 fields rendered | `web/src/features/status-pages/StatusPagesTable.test.tsx:92-94` URL, "N serviços", "Público" asserted; "Atualizado" timestamp column not directly asserted | ✅ PASS (minor: Atualizado unasserted, low risk — timestamp formatting reused from an existing, already-tested pattern) |
| DSP-14: detail drawer — services list, domain (or "—"), "Ver página pública" link, "Editar página" → `/status-pages/{id}` | All present | `web/src/features/status-pages/StatusPageDetailDrawer.test.tsx:77-83` domain + href asserted; **both drawer fixtures use `service_ids: []`, so the actual services-list rendering (non-empty case) is never exercised by an assertion** | ⚠️ Partial GAP — "Editar página"/domain link path proven; non-empty services list rendering unproven |
| DSP-15: add-page drawer — "Público" default/functional, "Privado" disabled | Non-interactive Privado | `web/src/features/status-pages/AddStatusPageDrawer.test.tsx:35-41` `aria-pressed`/`aria-disabled`, selection unchanged after click | ✅ PASS |
| DSP-16 (edge case): null `verified_at` → "—" | "—", not formatted null | `web/src/features/domains/DomainsTable.test.tsx:106` `getAllByText("—")` length 2 | ✅ PASS |
| DSP-17 (edge case): status page with no `domain_id` → "—" for URL pública, link absent | Both conditions | `web/src/features/status-pages/StatusPagesTable.test.tsx:105` (dash), `web/src/features/status-pages/StatusPageDetailDrawer.test.tsx:92` (`not.toBeInTheDocument()`) | ✅ PASS |
| DSP-18 (edge case): zero domains → `EmptyState`, not empty table | `EmptyState` rendered | `web/src/features/domains/DomainsTable.test.tsx:114-115` `"Nenhum domínio cadastrado"`, zero rows | ✅ PASS (test comment mislabels an unrelated tab-switch test as "(DSP-18)" in `DomainsStatusPagesPage.test.tsx:94` — a labeling inconsistency, not a coverage gap, since the real empty-state assertion above stands on its own) |
| DSP-19 (edge case): switching tabs closes drawer from previous tab | Drawer closed after switch | `web/src/features/domains/DomainsStatusPagesPage.test.tsx:107-123` `queryByRole("button", {name: "Verificar novamente"})` `.not.toBeInTheDocument()` after switching tabs | ⚠️ **Spec-precision gap / weak assertion** — test passes because the drawer's parent JSX subtree unmounts via the tab's conditional render (`{tab === "domains" ? (...) : (...)}`), independent of whether `selectedDomain`/`selectedPage` state actually resets. Confirmed by sensor (mutation 5, see below): removing the state-reset lines from `switchTab` did NOT fail this test. A test that switches away and back to the same tab (asserting the drawer does not silently reopen) would be needed to truly discriminate. |

**Status**: ❌ Gaps present (2 coverage gaps: DSP-01 partial, DSP-14 partial) + 1 spec-precision/discrimination gap (DSP-19, confirmed by sensor)

---

## Payload/Conjunction Rule (DSP-01–DSP-04, domain row fields)

Applied specifically to the domain row's payload-bearing fields as instructed:

- `status`/`ssl_status` values are asserted at the **repository/handler** layer with exact string values (`"verified"`, `"active"`, etc. — see `domains_handler_test.go`'s verify tests), so the backend payload is not just "some response," the exact field values are checked.
- At the **frontend** layer, `DomainsTable.test.tsx` asserts `attached_page_name`/`attached_page_count`/`verified_at` values conjunctively (all present together in the same row), but does **not** conjunctively assert `status`/`domain_type`/`ssl_status` alongside them in the same row-level check — this is the same gap noted under DSP-01 above, not a new one.

---

## Discrimination Sensor

Isolated scratch: `git worktree add /tmp/vane-sensor HEAD` (never `git stash`). Pre-sensor real-tree baseline: `git status --porcelain` showed only 2 pre-existing untracked spec files (`design.md`, `spec.md` from this validation task's own reads). Post-cleanup baseline matched exactly — confirmed via worktree removal + `git status --porcelain` re-check.

| # | Target | File:line | Mutation | Killed? |
| - | ------ | --------- | -------- | ------- |
| 1 | `AttachedNamesByDomainIDs` ordering (DSP-04) | `internal/db/status_page_repository.go:219` | `ORDER BY domain_id, created_at ASC` → `DESC` | ✅ Killed — `TestAttachedNamesByDomainIDs_TwoOrMoreAttached_OrderedByCreatedAt` failed with order mismatch |
| 2 | "+N" attached-page-count display (DSP-04) | `web/src/features/domains/DomainsTable.tsx:20` | `count - 1` → `count` (off-by-one) | ✅ Killed — 2 `DomainsTable.test.tsx` tests failed (DSP-03 "one attached" and DSP-04 "+N" tests) |
| 3 | Disabled-tile non-interactivity (DSP-10) | `web/src/features/domains/AddDomainDrawer.tsx:140-142` | Removed `disabled ? undefined : onClick` guard and `aria-disabled={disabled}` (hardcoded `false`, always-attached `onClick`) | ✅ Killed — `AddDomainDrawer.test.tsx`'s DSP-10 test failed |
| 4 | 409-on-delete inline-error-not-close (DSP-08) | `web/src/features/domains/DomainDetailDrawer.tsx:48-51` | Added `onClose()` inside the `catch` block (closes drawer even on 409) | ✅ Killed — `DomainDetailDrawer.test.tsx`'s DSP-08 test failed |
| 5 | Tab-switch drawer-closing (DSP-19) | `web/src/features/domains/DomainsStatusPagesPage.tsx:32-37` | Removed `setSelectedDomain(null)`/`setSelectedPage(null)`/`setAddOpen(false)` from `switchTab`, keeping only `setTab(next)` | ❌ **Survived** — `DomainsStatusPagesPage.test.tsx`'s DSP-19 test still passed, because the drawer unmounts via the tab's conditional JSX render regardless of whether the underlying selection state resets → fix task created |

**Sensor depth**: lightweight (5 targeted mutations, as instructed)
**Result**: 4/5 killed — ❌ FAIL (one surviving mutant on the highest-risk cross-tab state-leak behavior explicitly called out as a spec edge case, DSP-19)

---

## Code Quality

| Principle | Status |
| --- | --- |
| Minimum code | ✅ |
| Surgical changes | ✅ — narrow interface (`statusPageNameLister`) added per existing pattern, no over-abstraction |
| No scope creep | ✅ — `DomainsSection`/`StatusPagesSection` deliberately left untouched per design.md's stated precedent |
| Matches patterns | ✅ — `ANY($1)` batching idiom, `ModeCard` disabled-tile pattern, `Page[T]`-adjacent bespoke envelope all consistent with existing codebase conventions |
| Spec-anchored outcome check (asserted values match spec) | ⚠️ Mostly — 2 partial gaps (DSP-01, DSP-14) and 1 discrimination gap (DSP-19) documented above |
| Per-layer Coverage Expectation met (domain 1:1 ACs; routes happy+edge+error) | ✅ backend; ⚠️ frontend has the two coverage gaps above |
| Every test maps to a spec requirement — no unclaimed tests | ✅ — test names are DSP-tagged throughout |
| Documented guidelines followed | `AGENTS.md` §3 (backend gate), §5 (frontend MSW/Pager/i18n conventions) — followed |

---

## Edge Cases

- [x] DSP-16 (null `verified_at` → "—"): handled correctly
- [x] DSP-17 (no `domain_id` → "—", link absent): handled correctly
- [x] DSP-18 (zero domains → `EmptyState`): handled correctly
- [⚠️] DSP-19 (tab switch closes drawer): behavior is correct in the shipped code (state does reset, confirmed by reading `switchTab`), but the **test does not discriminate** this from the alternative "state doesn't reset but the tab unmount hides it anyway" — flagged via the sensor, not a behavior bug

---

## Gate Check

- **Gate commands**: `go build ./...`, `go vet ./...`, `gofmt -l internal/db/status_page_repository.go internal/api/domains_handler.go` (+ test files), `make test-integration`-equivalent (disposable `postgres:16-alpine` container, port 5433, `-p 1`), `npx tsc -b --noEmit`, `npm run test`
- **Backend build/vet/gofmt**: clean, exit 0
- **Backend integration tests**: `internal/api` and `internal/db` packages pass in full (including all new `AttachedNamesByDomainIDs`/`domains_handler` tests). **3 pre-existing failures** in unrelated singleton-state tests (`TestEmailProviderRepository_SetActiveProvider_UpdatesSingletonRow`, `TestLLMProviderRepository_SetActiveProvider_ThenGetActiveProvider_RoundTrips`, `TestServiceRepository_ListPaginated_ReturnsSLONameForMixOfPreAndPostMigrationRows`) were independently confirmed to **also fail on commit `c3a5f24^`** (the commit immediately before this feature's range), verified in a separate throwaway worktree against the same fresh disposable database — these are not a regression introduced by this feature. Full suite (excluding those 3 known-pre-existing tests) passes clean.
- **Frontend**: `npx tsc -b --noEmit` clean; `npm run test` — **89 test files, 504 tests, all passed**
- **Test count**: cannot state an exact "before feature" baseline (out of scope of this Verifier run to check pre-feature test count across the whole repo), but the diff added new `*.test.tsx`/`*.test.ts` files for every new component/hook per `git diff --stat`, consistent with the Test Coverage Matrix

---

## Fix Plans

### Fix 1: DSP-19 test does not discriminate the tab-switch state-reset behavior

- **Root cause**: `web/src/features/domains/DomainsStatusPagesPage.test.tsx`'s DSP-19 test only asserts the previously-open drawer's contents disappear after switching tabs — which happens automatically because `DomainDetailDrawer`/`StatusPageDetailDrawer` live inside each tab's conditionally-rendered JSX subtree. It does not assert that `selectedDomain`/`selectedPage`/`addOpen` state was actually reset.
- **Fix task**: Extend the existing DSP-19 test (or add a sibling test) to: open a domain detail drawer → switch to Status Pages tab → switch back to Domínios tab → assert the drawer does NOT reopen (i.e., `queryByRole("button", {name: "Verificar novamente"})` is still absent). This would fail against the mutated `switchTab` (state not reset) and pass against the real shipped code (state is reset), making it a true discriminator.
- **Priority**: Minor — the shipped behavior itself is correct (verified by reading `DomainsStatusPagesPage.tsx:32-37`); this is a test-strength gap, not a user-facing bug.

### Fix 2: DSP-01's Status/Tipo/SSL columns lack assertions in `DomainsTable.test.tsx`

- **Root cause**: Existing tests focus on the "Aponta para" (DSP-02/03/04) and "Verificado" (DSP-16) columns, which were this feature's actual backend-driven risk. The Status pill text/variant, "Domínio próprio" (Tipo), and SSL label were carried over from `domainStatusMeta.ts` (already unit-tested in isolation at `domainStatusMeta.test.ts`) but never asserted at the table-rendering level.
- **Fix task**: Add one assertion per column in an existing or new `DomainsTable.test.tsx` case, e.g. `expect(within(row).getByText("Domínio próprio")).toBeInTheDocument()` and asserting the status/SSL `Tag` text for a known fixture.
- **Priority**: Minor — `domainStatusMeta.ts` is unit-tested and the table's usage is straightforward prop-passing, so risk of an actual bug is low, but per evidence-or-zero this remains uncovered at this layer.

### Fix 3: DSP-14's services list is only tested with an empty list

- **Root cause**: Both `StatusPageDetailDrawer.test.tsx` fixtures use `service_ids: []`; the drawer's actual rendering of attached service names when the list is non-empty has no assertion.
- **Fix task**: Add a test case with a non-empty `service_ids` fixture and assert the corresponding service name(s) render in the drawer's services list.
- **Priority**: Minor — code path is simple (map over an array), but unexercised by any test.

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| --- | --- | --- |
| DSP-01 | Pending | ⚠️ Needs Fix (partial — Status/Tipo/SSL unasserted) |
| DSP-02 | Pending | ✅ Verified |
| DSP-03 | Pending | ✅ Verified |
| DSP-04 | Pending | ✅ Verified |
| DSP-05 | Pending | ✅ Verified |
| DSP-06 | Pending | ✅ Verified |
| DSP-07 | Pending | ✅ Verified |
| DSP-08 | Pending | ✅ Verified |
| DSP-09 | Pending | ✅ Verified |
| DSP-10 | Pending | ✅ Verified |
| DSP-11 | Pending | ✅ Verified |
| DSP-12 | Pending | ✅ Verified |
| DSP-13 | Pending | ✅ Verified |
| DSP-14 | Pending | ⚠️ Needs Fix (partial — services-list non-empty case unasserted) |
| DSP-15 | Pending | ✅ Verified |
| DSP-16 | Pending | ✅ Verified |
| DSP-17 | Pending | ✅ Verified |
| DSP-18 | Pending | ✅ Verified |
| DSP-19 | Pending | ⚠️ Needs Fix (test doesn't discriminate state-reset; behavior itself correct) |

---

## Summary

**Overall**: ⚠️ Issues (backend and gate are fully clean; frontend behavior is correct by code inspection; 3 test-coverage/discrimination gaps found, none indicating an actual shipped bug)

**Spec-anchored check**: 16/19 ACs cleanly matched spec outcome, 3 flagged (DSP-01 partial, DSP-14 partial, DSP-19 discrimination gap)
**Sensor**: 4/5 mutations killed, 1 survived (DSP-19)
**Gate**: All clean (backend build/vet/gofmt/integration; frontend tsc/504 tests) — 3 pre-existing unrelated integration-test failures confirmed NOT caused by this feature

**What works**: The backend join (`AttachedNamesByDomainIDs`), the extended `domainResponse`, the full Domínios tab (table/detail/add), the full Status Pages tab (table/detail/add), and the tabbed page rewrite all function correctly per the spec, backed by real assertions for the great majority of ACs. Zero schema changes, as promised.

**Issues found**:
1. `DomainsStatusPagesPage.test.tsx`'s DSP-19 test doesn't discriminate whether tab-switch actually resets selection state vs. merely unmounting the drawer's subtree — add a switch-away-and-back assertion.
2. `DomainsTable.test.tsx` never asserts the Status/Tipo/SSL column values (only Aponta para/Verificado).
3. `StatusPageDetailDrawer.test.tsx` never exercises a non-empty services list.

**Next steps**: Route Fix 1–3 above as fix tasks to an implementer; all are test-only additions (no production code change expected), then re-verify.
