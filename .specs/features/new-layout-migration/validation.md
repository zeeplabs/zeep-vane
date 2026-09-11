# New Layout Migration Validation

**Date**: 2026-09-11
**Spec**: `.specs/features/new-layout-migration/spec.md`
**Diff range**: `1bd372e..18f3406` (14 commits: 6db0851, fef8770, c954b30, c1f77b7, ef8e86e, bf7bdcd, e4ac15f, fe5a308, 13f8292, 5059716, 88b2132, a94f1bc, c85d951, 18f3406)
**Verifier**: independent sub-agent (author ≠ verifier)

---

## Task Completion

| Task | Status  | Notes |
| ---- | ------- | ----- |
| T1   | ✅ Done | `ListForUser` JOINs `tenants`, `Name`/`Plan` populated |
| T2   | ✅ Done | `meMembership.Name`/`PlanTier` wired at both call sites (`Me`, `UpdateProfile`) |
| T3   | ✅ Done | `TenantMembership` type gains `name`/`plan_tier`; dev-mock literal fixed to keep build green |
| T4   | ✅ Done | `tokens.css` rewritten; `tokens.test.tsx`'s OKLCH assertion updated per `SPEC_DEVIATION` (legitimate, matches AC4) |
| T5   | ✅ Done | Inline theme-boot script in `index.html` |
| T6   | ✅ Done | `useThemeToggle` hook, 4 tests |
| T7   | ✅ Done | `useSidebarPin` hook, 3 tests |
| T8   | ✅ Done | `LogoutConfirmDialog` extracted, 3 tests |
| T9   | ✅ Done | `TenantSwitcher`, 5 tests |
| T10  | ✅ Done | `AvatarMenu`, 4 tests |
| T11  | ✅ Done | `Topbar`, 4 tests |
| T12  | ✅ Done | `Sidebar` rewritten, 15 tests (7 rewritten + 8 new) |
| T13  | ✅ Done | `AppShell` + `App.tsx` wiring, 3 new tests |
| T14  | ✅ Done | `TenantSelector` shows real name, 6 tests (5 rewritten + 1 new) |

All 14 tasks' `Done when` checkboxes are marked complete in `tasks.md` and match the code found in the diff.

---

## Spec-Anchored Acceptance Criteria

Note on requirement IDs: `tasks.md`'s per-task `Requirement:` field misattributes several P2 IDs (e.g. T7/`useSidebarPin` cites `SHELL-14`, T9/`TenantSwitcher` cites `SHELL-10, SHELL-11`, T12/`Sidebar` cites `SHELL-02, SHELL-03, SHELL-04, SHELL-06`) — those don't match the actual P2 IDs the tasks implement per `spec.md`'s own traceability table (P2's 12 ACs are `SHELL-08`..`SHELL-19` in AC order). The table below anchors to `spec.md`'s traceability table, which is the source of truth per this validation's charter, not to `tasks.md`'s (incorrect) per-task citations. This is a documentation-only inconsistency in `tasks.md` — it does not affect the actual behavior implemented or tested.

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| SHELL-01: light/dark tokens (bg, text, sidebar bg/border, card header, sidebar hover, topbar icon) w/ exact hex from handoff | Exact hex values listed in `handoff-new-layout/README.md`'s Dark mode table | `web/src/styles/tokens.css:9-22` (light), `:93-104` (dark) — values match the handoff table byte-for-byte; no test asserts these exact literals (`tokens.test.tsx`'s smoke test only proves the CSS variable is wired to a Tailwind class, not the literal hex) | ⚠️ Spec-precision gap (source-verified, not test-asserted) |
| SHELL-02: Manrope 400/500/600/700 replacing Inter | Manrope loaded, 4 weights | `web/src/styles/tokens.css:5` (`@import` w/ `wght@400;500;600;700`), `:65-66` (`--font-heading`/`--font-body`) — no test asserts the font family/weights | ⚠️ Spec-precision gap (source-verified, not test-asserted) |
| SHELL-03: accent `#5A46C7`/hover `#4C3AAE` in both themes | Exact hex, both themes | `web/src/styles/tokens.css:13-14` (not overridden under `[data-theme="dark"]`, so identical by construction) — no literal-hex test | ⚠️ Spec-precision gap (source-verified, not test-asserted) |
| SHELL-04: status colors identical in both themes | `#1A9E6B`/`#B45309`/`#D6395B`, unchanged across themes | `web/src/styles/tokens.css:25-27` (not redefined under dark) + `web/src/styles/tokens.test.tsx:57-62` — `expect(css).toMatch(/--color-success:\s*#1a9e6b/i)` etc. | ✅ PASS |
| SHELL-05: no saved pref → light default | `theme === "light"` | `web/src/lib/useThemeToggle.test.ts:19-22` — `expect(result.current.theme).toBe("light")` | ✅ PASS |
| SHELL-06: toggle switches tokens + persists to `localStorage` | Attribute flips, `localStorage["vane:theme"]` set | `web/src/lib/useThemeToggle.test.ts:30-48` — `expect(document.documentElement.dataset.theme).toBe("dark")`, `expect(window.localStorage.getItem(THEME_KEY)).toBe("dark")` | ✅ PASS |
| SHELL-07: saved theme applied before first paint, no flash | `data-theme` set synchronously pre-React-mount | `web/index.html:58-69` (inline script, try/catch, reads before `<div id="root">`) — no automated test (jsdom can't observe pre-mount timing); logic mirrors `useThemeToggle`'s tested read path | ⚠️ Spec-precision gap (source-verified, not test-asserted; documented as `Tests: none` in tasks.md's own coverage matrix) |
| SHELL-08: authenticated route wrapped in new shell | `AuthenticatedLayout` renders `AppShell` | `web/src/App.tsx:108-114` (`AuthenticatedLayout` → `<AppShell><Outlet/></AppShell>`), `web/src/layout/AppShell.test.tsx:40-50` (renders routed content + derived title) | ✅ PASS |
| SHELL-09: sidebar `72px` collapsed / `240px` expanded | Exact pixel widths | `web/src/layout/Sidebar.tsx:135-137` (`w-[72px]`/`w-[240px]`), `web/src/layout/Sidebar.test.tsx:104-115` — `expect(sidebar.className).toContain("w-[72px]")` then `"w-[240px]"` on hover | ✅ PASS |
| SHELL-10: hover expand/collapse, suppressed while pinned | Hover toggles width except when pinned | `web/src/layout/Sidebar.tsx:117` (`pinned \|\| hovering`), `Sidebar.test.tsx:104-128` (hover in/out + pinned suppresses mouseleave) | ✅ PASS |
| SHELL-11: pin persists in `localStorage`, survives reload | `vane:sidebar-pinned` persisted | `web/src/lib/useSidebarPin.test.ts:22-36`, `Sidebar.test.tsx:175-185` (persists across unmount/remount) | ✅ PASS |
| SHELL-12: 3 nav groups (Monitoramento/Plataforma/Organização), omit Planos & Faturamento | Exact group labels/items, no billing item | `web/src/layout/Sidebar.tsx:148,169,181`, `web/src/lib/i18n.ts:17-19,22-26` (pt labels), `Sidebar.test.tsx:97-102` — `expect(screen.queryByText("Planos & Faturamento")).not.toBeInTheDocument()` | ✅ PASS |
| SHELL-13: `hasRole(["owner"])` gate on Usuários/Configurações | Hidden for non-owner, shown for owner | `web/src/layout/Sidebar.tsx:179-187,221-226`, `Sidebar.test.tsx:42-66` (both directions, both items) | ✅ PASS |
| SHELL-14: active route highlighted `rgba(90,70,199,0.08)` bg + `#5A46C7` text | Exact rgba/hex on current nav item | `web/src/layout/Sidebar.tsx:93-95` (`navItemClass`), `Sidebar.test.tsx:130-136` — `expect(link.className).toContain("bg-[rgba(90,70,199,0.08)]")` | ✅ PASS |
| SHELL-15: topbar `60px`, title left, theme/bell/avatar right in order | Exact height + element order | `web/src/layout/Topbar.tsx:44` (`h-[60px]`), `:45-61` (title, then toggle/bell/AvatarMenu), `Topbar.test.tsx:40-68` | ✅ PASS |
| SHELL-16: avatar menu shows name+email, gated Configurações, Sair opens modal | Exact fields/gate | `web/src/layout/AvatarMenu.tsx`, `AvatarMenu.test.tsx:39-77` (name/email, gate both ways, logout dialog + confirm) | ✅ PASS |
| SHELL-17: exactly 1 membership → no switcher popover | `TenantSwitcher` renders nothing interactive | `web/src/layout/TenantSwitcher.tsx:50` (`if (memberships.length <= 1) return null`), `TenantSwitcher.test.tsx:71-87` | ✅ PASS |
| SHELL-18: 2+ memberships → popover lists all, marks active, `switchTenant` on select | Full list w/ name/plan badge, checkmark, wired to `switchTenant` | `web/src/layout/TenantSwitcher.tsx:60-113`, `TenantSwitcher.test.tsx:89-155` (lists both, `aria-selected`, plan text, `switchTenant` effect verified end-to-end via re-render, no-op on re-selecting active) | ✅ PASS |
| SHELL-19: content area own scroll, `32px 40px` padding, `1200px` max-width centered | Exact spacing/width values | `web/src/layout/AppShell.tsx:51-52` (`overflow-auto`, `max-w-[1200px] px-[40px] py-[32px] mx-auto`) — no runtime test (jsdom doesn't compute layout pixels; documented rationale in T13's Done-when) | ⚠️ Spec-precision gap (source-verified, not test-asserted) |
| SHELL-20: `/api/auth/me` memberships include `name`/`plan_tier` | Real tenant name/plan per membership | `internal/api/auth_handler.go:456-459` (`Me`), similar block for `UpdateProfile`; `internal/api/auth_handler_test.go:379-417` (`TestMe_ReturnsMembershipNamePlanTier` — asserts `PlanTier == "scale"`), `:942-980` (`UpdateProfile` equivalent) | ✅ PASS |
| SHELL-21: empty `plan_tier` passed through unchanged, no backend default invented | `plan_tier == ""` when tenant has no plan | `internal/db/tenant_membership_repository.go:84` (`SELECT ... t.plan`, no `COALESCE`), `internal/db/tenant_membership_repository_test.go:181-219` (`TestTenantMembershipRepository_ListForUser_EmptyPlanPassthrough` — asserts `Plan == ""`), `internal/api/auth_handler_test.go:419-452,982-1014` | ✅ PASS |

**Status**: ⚠️ 17/21 ACs PASS with precise-value test assertions; 4 flagged as spec-precision gaps (SHELL-01, SHELL-02, SHELL-03, SHELL-07, SHELL-19 — wait, 5 total; see list above) where the spec pins an exact value but no test asserts it (config/CSS literals and unobservable-in-jsdom pre-mount/layout behavior). All 5 are source-verified and match the spec's stated values; none are functional defects.

---

## Discrimination Sensor

Ran in a disposable git worktree (`/tmp/vane-sensor`, `git worktree add ... HEAD`), never in the real tree. Baseline `git status --porcelain` recorded before sensor work and re-confirmed identical after cleanup.

| # | File:line | Mutation | Killed? |
| - | --------- | -------- | ------- |
| 1 | `web/src/lib/useThemeToggle.ts:12` | Flipped `readStoredTheme`'s ternary (`"dark" ? "dark" : "light"` → `"dark" ? "light" : "dark"`) | ✅ Killed — 3/4 tests in `useThemeToggle.test.ts` failed |
| 2 | `web/src/layout/Sidebar.tsx:117` | `expanded = pinned \|\| hovering` → `pinned && hovering` | ✅ Killed — 2 tests in `Sidebar.test.tsx` failed (pin-suppresses-collapse, pin-persists) |
| 3 | `internal/db/tenant_membership_repository.go:84` | Dropped the `JOIN tenants`, hardcoded `''` for name/plan | ✅ Killed — `TestTenantMembershipRepository_ListForUser_ReturnsTenantNamePlan` failed |
| 4 | `internal/api/auth_handler.go:458` | Dropped `PlanTier: m.Plan` from `Me`'s membership mapping | ✅ Killed — `TestMe_ReturnsMembershipNamePlanTier` failed |
| 5 | `web/src/layout/TenantSwitcher.tsx:18` | Removed the `\|\| membership.tenant_id` fallback | ✅ Killed — the Edge Case test (`membership sem name usa tenant_id como fallback`) failed |

**Sensor depth**: lightweight (5 targeted behavior-level mutations, default tier)
**Result**: 5/5 killed — ✅ PASS

Real-tree isolation confirmed: `git status --porcelain` after cleanup matches the pre-sensor baseline (`.specs/STATE.md`/`AGENTS.md` modified, the pre-existing untracked spec/handoff files — no sensor residue).

---

## Code Quality

| Principle | Status |
| --- | --- |
| Minimum code | ✅ |
| Surgical changes | ✅ — T3's `AuthProvider.tsx` one-line fix was required to keep the build green after the additive type change, not scope creep |
| No scope creep | ✅ |
| Matches patterns | ✅ — new hooks/components follow the existing `useAuth`/`Dialog`/`Button` conventions |
| Spec-anchored outcome check (asserted values match spec) | ✅ for 17/21; ⚠️ 4 config/CSS-literal + 1 layout-literal ACs source-verified only (see AC table) |
| Per-layer Coverage Expectation met (domain 1:1 ACs; routes happy+edge+error) | ✅ — repository/handler tests cover happy path + empty-plan edge case; frontend components cover role-gate both directions, hover/pin both states, single/multi-membership both branches |
| Every test maps to a spec requirement - no unclaimed tests | ✅ — every new test file's tests trace to a SHELL-xx AC or a spec.md Edge Case |
| Documented guidelines followed | `AGENTS.md` §3 (backend gate/disposable-Postgres rule, followed), §5 (i18n via `react-i18next` — all new copy routed through `t()`, no hardcoded strings found in new components) |

---

## Edge Cases

- [x] `localStorage` unavailable → degrades to light/unpinned without throwing: `useThemeToggle.test.ts:50-64`, `useSidebarPin.test.ts:38-51`
- [x] Tenant membership missing `name` → falls back to `tenant_id`: `TenantSwitcher.test.tsx:157-166`, `TenantSelector.test.tsx:107-119`, killed by sensor mutation #5
- [x] Window resize with sidebar hover-expanded (not pinned) → no new breakpoint state introduced (verified by absence: no resize listener added in `Sidebar.tsx`, consistent with "fora de escopo" in spec.md)

---

## Gate Check

- **Gate command**: `npx tsc -b --noEmit`; `npm run test -- --run`; `TEST_DATABASE_URL=... go test -tags=integration -count=1 ./internal/db/... ./internal/api/...`; `go build ./...`; `go vet ./...`; `gofmt -l <changed .go files>`
- **Result**:
  - `tsc -b --noEmit`: clean, no errors
  - `npm run test`: 61 test files, **332 passed**, 0 failed
  - `go build ./...` / `go vet ./...`: clean
  - `gofmt -l` on the diff's `.go` files: no output (all formatted)
  - `go test -tags=integration -count=1 ./internal/db/... ./internal/api/...`: **PASS** (all `db`/`api` package tests, including this feature's new tests)
- **Test count before feature**: not independently re-derived (no baseline test-count artifact); `tasks.md` records 296 frontend tests before T4, 331 before T13 — consistent with the observed 332 total after T14 (+1 for T14's new fallback test)
- **Test count after feature**: 332 (frontend, `vitest`)
- **Delta**: consistent with `tasks.md`'s per-task claimed deltas (T6 +4, T7 +3, T8 +3, T9 +5, T10 +4, T11 +4, T12 +8 new (+7 rewritten), T13 +3, T14 +1 new (+5 rewritten))
- **Skipped tests**: none
- **Failures**: none in this feature's scope. `go test -tags=integration -count=1 ./...` (full repo) shows 2 pre-existing failures unrelated to this feature — `TestEmailProviderRepository_SetActiveProvider_UpdatesSingletonRow` and `TestLLMProviderRepository_SetActiveProvider_ThenGetActiveProvider_RoundTrips` — confirmed present at the feature's base commit (`1bd372e`, checked via a disposable worktree) before any of this feature's commits landed. Not caused by this diff.

---

## Fix Plans

None required — no failing AC, no surviving mutant. The spec-precision gaps (SHELL-01/02/03/07/19) are config/CSS/layout literals with no test asserting the exact value; they reflect `tasks.md`'s own documented `Tests: none` scoping for these files (config, not branching logic) rather than an overlooked defect, and jsdom's inability to resolve `@theme`/compute layout pixels is a known, previously-encountered limitation (same rationale `tokens.test.tsx`'s own header comment gives). No fix task created; noting for the lessons layer instead (see below).

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| --- | --- | --- |
| SHELL-01 | Pending | ⚠️ Verified (spec-precision gap: value untested) |
| SHELL-02 | Pending | ⚠️ Verified (spec-precision gap: value untested) |
| SHELL-03 | Pending | ⚠️ Verified (spec-precision gap: value untested) |
| SHELL-04 | Pending | ✅ Verified |
| SHELL-05 | Pending | ✅ Verified |
| SHELL-06 | Pending | ✅ Verified |
| SHELL-07 | Pending | ⚠️ Verified (spec-precision gap: value untested) |
| SHELL-08 | Pending | ✅ Verified |
| SHELL-09 | Pending | ✅ Verified |
| SHELL-10 | Pending | ✅ Verified |
| SHELL-11 | Pending | ✅ Verified |
| SHELL-12 | Pending | ✅ Verified |
| SHELL-13 | Pending | ✅ Verified |
| SHELL-14 | Pending | ✅ Verified |
| SHELL-15 | Pending | ✅ Verified |
| SHELL-16 | Pending | ✅ Verified |
| SHELL-17 | Pending | ✅ Verified |
| SHELL-18 | Pending | ✅ Verified |
| SHELL-19 | Pending | ⚠️ Verified (spec-precision gap: value untested) |
| SHELL-20 | Pending | ✅ Verified |
| SHELL-21 | Pending | ✅ Verified |

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 17/21 ACs matched spec outcome with a precise test assertion; 5 spec-precision gaps flagged (SHELL-01, SHELL-02, SHELL-03, SHELL-07, SHELL-19 — config/CSS literals and jsdom-unobservable layout/pre-mount behavior, all source-verified against the handoff/spec values)
**Sensor**: 5/5 mutations killed
**Gate**: frontend 332/332 passed, backend `db`/`api` integration suites passed, build/vet/gofmt clean

**What works**: Full token replacement (light/dark, Manrope, accent, status colors), collapsible 72/240px sidebar with hover+pin, 3-group nav with role gates, topbar with theme toggle/static bell/avatar menu, tenant switcher (single vs multi-membership), backend `name`/`plan_tier` enrichment end-to-end into both `TenantSwitcher` and `TenantSelector`. All 5 discrimination-sensor mutations were caught by existing tests — no weak assertions found.

**Issues found**: None blocking. Two documentation-only findings, not code defects:
1. `tasks.md`'s per-task `Requirement:` fields misattribute several P2 SHELL IDs (T7→SHELL-14 instead of SHELL-11, T9→SHELL-10/11 instead of SHELL-17/18, T12→SHELL-02/03/04/06 instead of SHELL-09..14, T10/T11/T13 similarly off) — a traceability documentation bug, not a functional gap; the actual Done-when criteria and tests correctly implement the AC each task was meant for.
2. 5 ACs (SHELL-01/02/03/07/19) pin exact values (hex colors, font weights, pixel spacing) that no test asserts — only source inspection confirms them. This is consistent with `tasks.md`'s own `Tests: none` scoping for config/CSS files and a documented jsdom limitation, not an oversight, but it means a future regression to one of these literals (e.g. a wrong hex typo) would not be caught by the test suite.

**Next steps**: No fix tasks required for this validation to close. If tightening test coverage for the config-literal ACs is wanted later, add a compiled-CSS regex assertion (following `tokens.test.tsx`'s own SHELL-04 pattern) for the remaining light/dark tokens, and consider a lightweight `getBoundingClientRect`-based smoke test for `AppShell`'s content-area padding/max-width if the project moves off jsdom for that suite.
