# Loading Skeletons Tasks

## Execution Protocol (MANDATORY — do not skip)

Implement these tasks with the `tlc-spec-driven` skill: activate it by name and follow its Execute flow and Critical Rules. Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user — do not proceed without it.**

---

**Spec**: `.specs/features/loading-skeletons/spec.md`
**Status**: Approved

---

## Test Coverage Matrix

> Generated from codebase (no `AGENTS.md`/CONTRIBUTING testing-standards subsection specific to frontend test depth — strong default applied: cover every spec AC and listed edge case). Sampled: `AdminsPage.test.tsx`, `Table.test.tsx`, `Button.test.tsx`, `OverviewPage.test.tsx`, MSW handlers in `web/src/test/msw/handlers.ts`.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Shared UI primitive (`Skeleton`) | unit | Every prop/behavior in spec ACs (size, pulse class, reduced-motion variant, testid) | `web/src/components/ui/Skeleton.test.tsx` | `npm run test` |
| Screen component (loading-state branch) | unit (RTL) | Loading state renders ≥1 skeleton, `aria-busy`, sr-only text, no skeleton once loaded, error branch untouched | `web/src/features/**/*.test.tsx` (existing file per screen) | `npm run test` |
| Type safety | build | No new TS errors | n/a | `npx tsc -b --noEmit` |

## Gate Check Commands

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick | After the primitive task and each screen task | `npm run test -- <test file>` |
| Build | Last task of each phase | `npx tsc -b --noEmit && npm run test` |

---

## Execution Plan

### Phase 1: Shared primitive

```
T1
```

### Phase 2: Identity & access screens

```
T1 → T2
T1 → T3
T1 → T4
T1 → T5
T1 → T6
T1 → T7
```

### Phase 3: Services & status-page screens

```
T1 → T8
T1 → T9
T1 → T10
T1 → T11
T1 → T12
T1 → T13
```

### Phase 4: Monitoring & remaining screens

```
T1 → T14
T1 → T15
T1 → T16
T1 → T17
T1 → T18
```

Note: T2–T18 each depend only on T1 (the shared primitive) — they are independent of each other and carry no data dependency between screens. They still execute in strict numeric order within their phase per the skill's "no intra-phase parallelism" rule, but that ordering is a sequencing convention, not a real dependency.

---

## Task Breakdown

### T1: Create `Skeleton` primitive

**What**: New shared component rendering a themed, pulsing placeholder block.
**Where**: `web/src/components/ui/Skeleton.tsx`
**Depends on**: None
**Reuses**: Sizing/className patterns from `web/src/components/ui/Card.tsx`
**Requirement**: SKEL-01, SKEL-02, SKEL-03

**Done when**:
- [x] Accepts `width`, `height` (number or string), `radius` (default matches existing card radius token), `className` props.
- [x] Renders `bg-neutral-200 dark:bg-neutral-800 animate-pulse motion-reduce:animate-none` plus `data-testid="skeleton"`.
- [x] Exported from `web/src/components/ui/Skeleton.tsx`, no barrel file change needed (matches how `Card`/`Tag` are imported directly elsewhere).

**Tests**: unit
**Gate**: quick

**Commit**: `feat(ui): add shared Skeleton primitive`

---

### T2: `AdminsPage` skeleton

**What**: Replace the `isLoading` text branch with skeleton table rows matching the admins grid.
**Where**: `web/src/features/admins/AdminsPage.tsx` (test file updated alongside, per co-location rule)
**Depends on**: T1
**Requirement**: SKEL-04, SKEL-05, SKEL-06, SKEL-07

**Done when**:
- [x] Loading branch renders skeleton rows (fixed count, 5) shaped like the real table's columns, wrapped in a container with `aria-busy="true"`.
- [x] The existing `t("...loading")` string (or an equivalent literal if no key exists yet) is rendered `sr-only` inside that container.
- [x] Real content and skeleton are mutually exclusive (same `isLoading ? A : B` shape already in the file).
- [x] Error branch (if any) untouched.
- [x] New test forces `isLoading: true` (MSW delayed response) and asserts `getAllByTestId("skeleton")` non-empty; existing tests still pass.

**Tests**: unit
**Gate**: quick

**Commit**: `feat(admins): add loading skeleton to AdminsPage`

---

### T3: `DomainsSection` skeleton

**Where**: `web/src/features/domains/DomainsSection.tsx` (test file updated alongside, per co-location rule)
**Depends on**: T1
**Requirement**: SKEL-04, SKEL-05, SKEL-06, SKEL-07
**Done when**: same shape as T2, scoped to this file's own loaded layout (domain rows). ✅ Done (2026-09-16).
**Tests**: unit
**Gate**: quick
**Commit**: `feat(domains): add loading skeleton to DomainsSection`

---

### T4: `DomainsTable` skeleton

**Where**: `web/src/features/domains/DomainsTable.tsx` (test file updated alongside, per co-location rule)
**Depends on**: T1
**Requirement**: SKEL-04, SKEL-05, SKEL-06, SKEL-07
**Done when**: same shape as T2, scoped to this file's table grid. ✅ Done (2026-09-16).
**Tests**: unit
**Gate**: quick
**Commit**: `feat(domains): add loading skeleton to DomainsTable`

---

### T5: `SessionsSection` skeleton

**Where**: `web/src/features/sessions/SessionsSection.tsx` (test file updated alongside, per co-location rule)
**Depends on**: T1
**Requirement**: SKEL-04, SKEL-05, SKEL-06, SKEL-07
**Done when**: same shape as T2; preserves the existing `isError` branch untouched (`isLoading`/`isError` both present here per spec edge case SKEL-07). ✅ Done (2026-09-16).
**Tests**: unit
**Gate**: quick
**Commit**: `feat(sessions): add loading skeleton to SessionsSection`

---

### T6: `NotificationsSection` skeleton

**Where**: `web/src/features/notifications/NotificationsSection.tsx` (test file updated alongside, per co-location rule)
**Depends on**: T1
**Requirement**: SKEL-04, SKEL-05, SKEL-06, SKEL-07
**Done when**: same shape as T2 — this file's `isLoading` currently only disables a switch; add a skeleton for the toggle-row list itself (per spec's Out of Scope note, this file DOES get a real skeleton for its rows, only the disabled-switch behavior stays as-is).
**Tests**: unit
**Gate**: quick
**Commit**: `feat(notifications): add loading skeleton to NotificationsSection`

---

### T7: `SettingsPage` skeleton (phase 2 build gate)

**Where**: `web/src/features/settings/SettingsPage.tsx` (test file updated alongside, per co-location rule)
**Depends on**: T1
**Requirement**: SKEL-04, SKEL-05, SKEL-06, SKEL-07
**Done when**: same shape as T2, scoped to this page's form/card layout. Last task of Phase 2 — run the Build gate.
**Tests**: unit
**Gate**: build
**Commit**: `feat(settings): add loading skeleton to SettingsPage`

---

### T8: `ServiceListPage` skeleton

**Where**: `web/src/features/services/ServiceListPage.tsx` (test file updated alongside, per co-location rule)
**Depends on**: T1
**Requirement**: SKEL-04, SKEL-05, SKEL-06, SKEL-07
**Done when**: skeleton rows match the real `grid-cols-[minmax(96px,max-content)_1fr_96px_140px_20px]` template (status/service/uptime/lastCheck/menu columns).
**Tests**: unit
**Gate**: quick
**Commit**: `feat(services): add loading skeleton to ServiceListPage`

---

### T9: `ServicesSection` skeleton

**Where**: `web/src/features/services/ServicesSection.tsx` (test file updated alongside, per co-location rule)
**Depends on**: T1
**Requirement**: SKEL-04, SKEL-05, SKEL-06, SKEL-07
**Done when**: same shape as T2, scoped to this file's row layout.
**Tests**: unit
**Gate**: quick
**Commit**: `feat(services): add loading skeleton to ServicesSection`

---

### T10: `StatusPagesSection` skeleton

**Where**: `web/src/features/status-pages/StatusPagesSection.tsx` (test file updated alongside, per co-location rule)
**Depends on**: T1
**Requirement**: SKEL-04, SKEL-05, SKEL-06, SKEL-07
**Done when**: same shape as T2.
**Tests**: unit
**Gate**: quick
**Commit**: `feat(status-pages): add loading skeleton to StatusPagesSection`

---

### T11: `StatusPagesTable` skeleton

**Where**: `web/src/features/status-pages/StatusPagesTable.tsx` (test file updated alongside, per co-location rule)
**Depends on**: T1
**Requirement**: SKEL-04, SKEL-05, SKEL-06, SKEL-07
**Done when**: same shape as T2.
**Tests**: unit
**Gate**: quick
**Commit**: `feat(status-pages): add loading skeleton to StatusPagesTable`

---

### T12: `StatusPageDetail` skeleton

**Where**: `web/src/features/status-pages/StatusPageDetail.tsx` (test file updated alongside, per co-location rule)
**Depends on**: T1
**Requirement**: SKEL-04, SKEL-05, SKEL-06, SKEL-07
**Done when**: replaces the single `<p>Carregando…</p>` with a skeleton approximating the detail layout (header + a few content blocks).
**Tests**: unit
**Gate**: quick
**Commit**: `feat(status-pages): add loading skeleton to StatusPageDetail`

---

### T13: `IncidentsPage` skeleton (phase 3 build gate)

**Where**: `web/src/features/incidents/IncidentsPage.tsx` (test file updated alongside, per co-location rule)
**Depends on**: T1
**Requirement**: SKEL-04, SKEL-05, SKEL-06, SKEL-07
**Done when**: same shape as T2. Last task of Phase 3 — run the Build gate.
**Tests**: unit
**Gate**: build
**Commit**: `feat(incidents): add loading skeleton to IncidentsPage`

---

### T14: `OverviewPage` skeleton

**Where**: `web/src/features/overview/OverviewPage.tsx` (test file updated alongside, per co-location rule)
**Depends on**: T1
**Requirement**: SKEL-04, SKEL-05, SKEL-06, SKEL-07
**Done when**: skeleton approximates the summary-grid + chart + recent-incidents card layout (a few skeleton cards, not a single block). `isError` branch untouched (SKEL-07).
**Tests**: unit
**Gate**: quick
**Commit**: `feat(overview): add loading skeleton to OverviewPage`

---

### T15: `PollerStatusPage` skeleton

**Where**: `web/src/features/poller/PollerStatusPage.tsx` (test file updated alongside, per co-location rule)
**Depends on**: T1
**Requirement**: SKEL-04, SKEL-05, SKEL-06, SKEL-07
**Done when**: same shape as T2. `isError` branch untouched (SKEL-07).
**Tests**: unit
**Gate**: quick
**Commit**: `feat(poller): add loading skeleton to PollerStatusPage`

---

### T16: `PublicStatusPage` skeleton

**Where**: `web/src/features/public-status/PublicStatusPage.tsx` (test file updated alongside, per co-location rule)
**Depends on**: T1
**Requirement**: SKEL-04, SKEL-05, SKEL-06, SKEL-07
**Done when**: skeleton approximates the banner + service-list layout. `isError` branch untouched (SKEL-07). Public unauthenticated page — verify the new sr-only string doesn't require an i18n key that doesn't exist yet (reuse or add one following this file's existing i18n usage).
**Tests**: unit
**Gate**: quick
**Commit**: `feat(public-status): add loading skeleton to PublicStatusPage`

---

### T17: `EmailProvidersPage` skeleton

**Where**: `web/src/features/email-providers/EmailProvidersPage.tsx` (test file updated alongside, per co-location rule)
**Depends on**: T1
**Requirement**: SKEL-04, SKEL-05, SKEL-06, SKEL-07
**Done when**: same shape as T2.
**Tests**: unit
**Gate**: quick
**Commit**: `feat(email-providers): add loading skeleton to EmailProvidersPage`

---

### T18: `AISettings` skeleton (phase 4 build gate)

**Where**: `web/src/features/settings/AISettings.tsx` (test file updated alongside, per co-location rule)
**Depends on**: T1
**Requirement**: SKEL-04, SKEL-05, SKEL-06, SKEL-07
**Done when**: same shape as T2. Last task of the feature — run the Build gate, then this triggers feature-level Verifier dispatch (Execute step 9).
**Tests**: unit
**Gate**: build
**Commit**: `feat(settings): add loading skeleton to AISettings`

---

## Phase Execution Map

```
Phase 1 → Phase 2 → Phase 3 → Phase 4

Phase 1:  T1
Phase 2:  T1 → {T2, T3, T4, T5, T6, T7}   (executed in that numeric order)
Phase 3:  T1 → {T8, T9, T10, T11, T12, T13}  (executed in that numeric order)
Phase 4:  T1 → {T14, T15, T16, T17, T18}     (executed in that numeric order)
```

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1 | 1 component | ✅ Granular |
| T2–T18 | 1 screen file + its test file each | ✅ Granular (cohesive pair) |

## Diagram-Definition Cross-Check

| Task | Depends On (body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | — | ✅ Match |
| T2–T18 | T1 | T1 → T[N] (star, per phase) | ✅ Match (all depend only on the Phase 1 primitive; numeric order within a phase is a sequencing convention, not a dependency) |

## Test Co-location Validation

| Task | Code Layer | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1 | Shared UI primitive | unit | unit | ✅ OK |
| T2–T18 | Screen component | unit | unit | ✅ OK |

---

## Tools

MCP: NONE required (no external API/library lookup needed — Tailwind v4 `animate-pulse`/`motion-reduce` are already-known first-party utilities).
Skill: `tlc-spec-driven` (this skill) for the Execute flow itself. No other skill needed.
