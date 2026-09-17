# Loading Skeletons Validation

**Date**: 2026-09-16
**Spec**: `.specs/features/loading-skeletons/spec.md`
**Diff range**: `a618e06^..2350285` (T1 shared primitive through T18 AISettings, all 18 tasks)
**Verifier**: independent pass, same session as the final implementer (standalone fallback per `sub-agents.md` - no separate sub-agent dispatch capability in this run), applying evidence-or-zero and re-deriving coverage from `spec.md` fresh rather than trusting task-file summaries.

---

## Task Completion

| Task | Status | Notes |
| --- | --- | --- |
| T1 | Done | Shared `Skeleton` primitive |
| T2-T13 | Done | Prior batches (AdminsPage, DomainsSection, DomainsTable, SessionsSection, NotificationsSection, SettingsPage, ServiceListPage, ServicesSection, StatusPagesSection, StatusPagesTable, StatusPageDetail, IncidentsPage) |
| T14 | Done | OverviewPage |
| T15 | Done | PollerStatusPage |
| T16 | Done | PublicStatusPage |
| T17 | Done | EmailProvidersPage |
| T18 | Done | AISettings (build gate run) |

All 18 tasks marked complete in `tasks.md`.

---

## Spec-Anchored Acceptance Criteria

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| SKEL-01: `Skeleton` renders a sized `div` with `bg-neutral-200 dark:bg-neutral-800` + `animate-pulse` | class list contains both tokens, inline size set from props | `web/src/components/ui/Skeleton.test.tsx:8-9` - `expect(el.style.width).toBe("120px")`; `Skeleton.test.tsx:16` - `expect(el.className).toContain("animate-pulse")`; impl at `web/src/components/ui/Skeleton.tsx:27` | ✅ PASS |
| SKEL-02: `motion-reduce:animate-none` disables pulse under reduced motion | class list contains `motion-reduce:animate-none` | `web/src/components/ui/Skeleton.test.tsx:17` - `expect(el.className).toContain("motion-reduce:animate-none")` | ✅ PASS |
| SKEL-03: rendered element carries `data-testid="skeleton"` | testid present | `web/src/components/ui/Skeleton.tsx:25` - `data-testid="skeleton"`; asserted via `getByTestId("skeleton")` at `Skeleton.test.tsx:8` | ✅ PASS |
| SKEL-04: `isLoading` renders ≥1 `Skeleton` approximating the loaded layout instead of plain text, across all 17 screens | ≥1 `[data-testid="skeleton"]` present, old text not the visible content | Representative: `web/src/features/admins/AdminsPage.test.tsx:65` - `expect(screen.getAllByTestId("skeleton").length).toBeGreaterThan(0)`; same pattern confirmed present in all 17 screen test files (verified by grep - see Code Quality section) | ✅ PASS (17/17 screens) |
| SKEL-05: loading root carries `aria-busy="true"` and the translated `loading` string renders `sr-only` | container has `aria-busy="true"`, sr-only text with translated string findable | `web/src/features/overview/OverviewPage.test.tsx:63-64` - `expect(srText.className).toContain("sr-only")`; `expect(srText.closest('[aria-busy="true"]')).toBeInTheDocument()`; impl `web/src/features/overview/OverviewPage.tsx:374-375` | ✅ PASS (17/17 screens - grep confirms `aria-busy` + `sr-only` present in every in-scope screen file) |
| SKEL-06: once loaded, no `Skeleton` remains (mutually exclusive) | `queryAllByTestId("skeleton")` returns 0 after load | `web/src/features/admins/AdminsPage.test.tsx:76` - `expect(screen.queryAllByTestId("skeleton")).toHaveLength(0)`; same assertion present in all 17 screen test files | ✅ PASS |
| SKEL-07: existing `isError` branches (OverviewPage, PollerStatusPage, PublicStatusPage, SessionsSection) left untouched | error-branch source lines unchanged by this feature's diff | `git diff a618e06^..2350285` shows the `isError`/error-branch lines (`OverviewPage.tsx:396`, `PollerStatusPage.tsx:86`, `PublicStatusPage.tsx:297`, `SessionsSection.tsx:98`) as unmodified context, never a `+`/`-` line | ✅ PASS |

**Status**: All 7 ACs covered with `file:line` evidence, no spec-precision gaps.

---

## Discrimination Sensor

Ran in an isolated `git worktree` at `/tmp/vane-sensor-scratch` (checked out at `2350285`, `node_modules` symlinked in for the test runner only - never copied into the real tree). Baseline `git status --porcelain` on the real tree before the sensor: only the pre-existing untracked `node_modules/` (unrelated, present before this session started). Real tree confirmed to match that exact baseline after the worktree was removed.

| Mutation | File:line | Description | Killed? |
| --- | --- | --- | --- |
| 1 | `web/src/components/ui/Skeleton.tsx:27` | Removed `motion-reduce:animate-none` from the class string | ✅ Killed - `Skeleton.test.tsx` "aplica animate-pulse e motion-reduce:animate-none" fails |
| 2 | `web/src/features/poller/PollerStatusPage.tsx:64` | Removed `aria-busy="true"` from the loading container | ✅ Killed - `PollerStatusPage.test.tsx` "mostra skeletons..." fails (`toBeInTheDocument()` on `null`) |
| 3 | `web/src/features/overview/OverviewPage.tsx:374-395` | Loading branch reduced to render 0 `Skeleton` elements | ✅ Killed - `OverviewPage.test.tsx` "mostra skeletons..." fails (`getAllByTestId` throws, 0 found) |
| 4 | `web/src/features/settings/AISettings.tsx:48` | Removed the sr-only loading `<span>` | ✅ Killed - `AISettings.test.tsx` "mostra skeletons..." fails (`findByText("Carregando…")` times out) |

**Sensor depth**: lightweight (4 targeted behavior-level mutations, default tier - no P0/critical-path code in this feature)
**Result**: 4/4 killed - PASS ✅

---

## Code Quality

| Principle | Status |
| --- | --- |
| Minimum code | ✅ - each screen only swaps its `isLoading` branch content; no unrelated refactors |
| Surgical changes | ✅ - `isError` branches, loaded-content branches, and unrelated files left untouched |
| No scope creep | ✅ - no new features beyond skeleton composition; `PublicStatusPage`'s pre-existing bespoke `LoadingSkeleton` was migrated (in scope of T16) rather than left duplicated |
| Matches patterns | ✅ - every screen follows the same `aria-busy` + `sr-only` + `Skeleton` composition shape established by T2-T13 and reused verbatim by T14-T18 |
| Spec-anchored outcome check (asserted values match spec) | ✅ - see Acceptance Criteria table above |
| Per-layer Coverage Expectation met (screen-component unit tests: loading + loaded-transition covered for all 17 screens) | ✅ |
| Every test maps to a spec requirement - no unclaimed tests | ✅ - all added tests map to SKEL-04/05/06; no speculative tests added |
| Documented guidelines followed | Frontend rules in `AGENTS.md` §5 (i18n via `react-i18next`, no hardcoded strings) - followed: `poller.loading`, `publicStatus.loading`, `emailProviders.loading`, `aiSettings.loading` added to `web/src/lib/i18n.ts` (pt/en) rather than hardcoding strings in the 3 screens that previously had zero i18n usage (`PollerStatusPage`, `EmailProvidersPage`) or an untranslated literal (`AISettings`, `PublicStatusPage`) |

**Deviation noted (process, not spec):** the T14 commit (`b08b313`) included the `web/src/lib/i18n.ts` additions for all four T15-T18 screens' `loading` keys in one edit pass, ahead of when each individual task strictly needed them. This is a commit-atomicity deviation from the "include only files listed in the task" rule (`implement.md` §7) - it does not affect functional correctness (the keys are inert until each screen's own commit wires them up) and no push has occurred, so history was left as-is rather than rewritten. Flagged here for the record.

---

## Edge Cases

- [x] Fixed small skeleton row/card counts (never predicting real eventual counts): OverviewPage renders 4 summary-card skeletons (fixed, matches the always-4-card grid) + chart/incidents card skeletons; PollerStatusPage renders 3 stat-card + 5 list-row skeletons; PublicStatusPage renders a banner + 3 fixed row skeletons; EmailProvidersPage renders exactly 2 (matches its fixed 2-provider list, not a variable one); AISettings renders 1 (matches its fixed single-provider layout).
- [x] `isFetching`/background-refetch (non-`isLoading`) never gets a skeleton - none of T14-T18's changes touch any `isFetching` branch; all condition on `isLoading` only, matching the pre-existing branching.
- [x] jsdom/vitest has no reduced-motion media query set - `animate-pulse` and `motion-reduce:animate-none` both remain present in the DOM class list under test (asserted at `Skeleton.test.tsx:16-17`), consistent with the spec's own note that this is a CSS media-query concern, not a jsdom-class-absence assertion.

---

## Gate Check

- **Gate command**: `npx tsc -b --noEmit && npm run test` (from `web/`)
- **Result**: `tsc` clean (no output/errors); vitest 95 files / 627 tests passed, 0 failed, 0 skipped
- **Test count before this batch (T14-T18) started**: 95 files / 617 tests (per T13's recorded build-gate result in `tasks.md`)
- **Test count after the full feature (T1-T18)**: 95 files / 627 tests
- **Delta**: +10 new tests (2 per screen × 5 screens in this batch: shows-skeletons + removes-skeletons)
- **Skipped tests**: none
- **Failures**: none (one transient flake reproduced on a single run of an unrelated pre-existing test - `AISettings`'s "Checkout" click case - not reproduced on immediate re-run; not connected to this feature's changes)

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| --- | --- | --- |
| SKEL-01 | Implementing | ✅ Verified |
| SKEL-02 | Implementing | ✅ Verified |
| SKEL-03 | Implementing | ✅ Verified |
| SKEL-04 | Implementing | ✅ Verified |
| SKEL-05 | Implementing | ✅ Verified |
| SKEL-06 | Implementing | ✅ Verified |
| SKEL-07 | Implementing | ✅ Verified |

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 7/7 ACs matched spec outcome, 0 spec-precision gaps
**Sensor**: 4/4 mutations killed
**Gate**: 627 passed, 0 failed

**What works**: All 17 in-scope screens replace their plain-text loading state with a `Skeleton`-composed layout approximation, wrapped in `aria-busy="true"` with an `sr-only` translated loading string; the shared `Skeleton` primitive is themed, pulses via `animate-pulse`, and respects `prefers-reduced-motion`; every pre-existing `isError` branch (Overview, Poller, PublicStatus, Sessions) is untouched; loading and loaded content remain mutually exclusive everywhere.

**Issues found**: None blocking. One process deviation noted above (i18n keys for T15-T18 landed in the T14 commit) - functionally inert, no fix required.

**Next steps**: None - feature complete, ready to close.
