# New Layout Migration — Fundação (Design System + App Shell) Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/new-layout-migration/design.md`
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase (`AGENTS.md` §3, sampled `web/src/components/ui/Dialog.test.tsx`, `web/src/layout/Sidebar.test.tsx` - MSW-backed real-login pattern, `internal/db/tenant_membership_repository.go`'s existing test file, `internal/api/auth_handler_test.go`) and this feature's spec ACs. Guidelines found: `AGENTS.md` §3 (backend gates, integration-test DB rule) and §5 (frontend gates, i18n via `react-i18next`, no hardcoded strings).

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Repository (`TenantMembershipRepository.ListForUser`) | integration | JOIN returns correct `name`/`plan` per membership; a tenant row with empty `plan` returns `""` unchanged (no backend default invented, SHELL-21) | `internal/db/tenant_membership_repository_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...` |
| API handlers (`Me`, `UpdateProfile` - both build `[]meMembership`) | integration | Both call sites return `name`/`plan_tier` per membership, 1:1 to SHELL-20/21 | `internal/api/auth_handler_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...` |
| Frontend types / config (`TenantMembership` type, `tokens.css`, `index.html` theme-boot script) | none | Build gate only - no branching logic to unit-test | `web/src/types/api.ts`, `web/src/styles/tokens.css`, `web/index.html` | `npx tsc -b --noEmit` |
| Hooks (`useThemeToggle`, `useSidebarPin`) | unit | All branches: default (no stored value), stored value read, toggle writes + updates DOM attribute/state, `localStorage` unavailable falls back silently (Edge Case) | `web/src/lib/*.test.ts` | `npm run test` |
| Shell components (`LogoutConfirmDialog`, `TenantSwitcher`, `AvatarMenu`, `Topbar`, `Sidebar`, `AppShell`) | unit (React Testing Library) | Render + interaction per component's spec ACs (SHELL-08..19); role gates (owner-only) proven both ways; `TenantSwitcher` renders `null` for single-membership users (SHELL-10) | `web/src/layout/*.test.tsx` | `npm run test` |
| `TenantSelector.tsx` (existing, modified) | unit | Shows real `name` (fallback `tenant_id` per Edge Case) instead of raw `tenant_id` | `web/src/features/auth/TenantSelector.test.tsx` (existing, extended) | `npm run test` |

## Gate Check Commands

> Generated from `AGENTS.md` §3/§5 - confirm before Execute.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Backend quick (no DB) | Never used stand-alone in this feature - the only backend task (T1+T2) touches DB-backed behavior | n/a |
| Backend full (DB-touching) | After T1 and after T2 | Spin disposable Postgres per `AGENTS.md` §3, then `TEST_DATABASE_URL=... go test -tags=integration ./...`, then `go build ./... && go vet ./... && gofmt -l <changed files>`, then destroy the container |
| Frontend build | After every frontend task | `npx tsc -b --noEmit` |
| Frontend test | After every frontend task with a `Tests` value other than `none` | `npm run test` |
| Phase completion | End of every phase | Backend full (if the phase touched Go) + Frontend build + Frontend test, all green |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Backend membership enrichment

```
T1 → T2
```

### Phase 2: Frontend theming foundation

```
T3
T4 → T5
T4 → T6
T7
```

T3-T7 have no dependency among themselves beyond T3 (frontend type) being the natural first step consumed by later phases - none of T4-T7 import `TenantMembership`, so they execute in file order without blocking each other.

### Phase 3: Shell components

```
T3 → T9 → T12 → T13
T6 → T11 → T13
T8 → T10 → T11
T7 → T12
T8 → T12
```

### Phase 4: Consistency fix

```
T3 → T14
```

---

## Task Breakdown

### T1: `TenantMembershipRepository.ListForUser` - JOIN `tenants`

**What**: Query gains `JOIN tenants t ON t.id = tm.tenant_id`, selecting `t.name`/`t.plan`; `TenantMembership` struct (Go) gains `Name`/`Plan` fields.
**Where**: `internal/db/tenant_membership_repository.go`
**Depends on**: None
**Reuses**: Existing `ListForUser` query shape (`internal/db/tenant_membership_repository.go:75-98`); same RLS/transaction convention already in effect (`AD-022`) - no new policy needed, the tenant row is already readable by its own membership.
**Requirement**: SHELL-20, SHELL-21

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `ListForUser` returns `Name`/`Plan` populated from `tenants` for every membership row
- [x] A tenant with `plan = ''` (empty, `AD-022`'s free-text default) returns `Plan: ""` unchanged - no default invented in the repository
- [x] `ListForTenant` (same file, different method) is untouched
- [x] Gate check passes on disposable Postgres: `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...`
- [x] Test count: +3 new subtests (name/plan populated; empty plan passthrough; multi-membership ordering unchanged)

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): join tenants into TenantMembershipRepository.ListForUser`

---

### T2: `meMembership`/`meResponse` - expose `name`/`plan_tier`

**What**: `meMembership` struct gains `Name`/`PlanTier` JSON fields; both call sites that build `[]meMembership` from `memberships.ListForUser` (`Me`, `UpdateProfile`) populate them from T1's new struct fields.
**Where**: `internal/api/auth_handler.go`
**Depends on**: T1
**Reuses**: `meMembership`/`meResponse` (`internal/api/auth_handler.go:405-427`); the two existing mapping loops (`Me` at line ~450-453, `UpdateProfile` at line ~503-506) - same shape, extended with 2 fields each.
**Requirement**: SHELL-20, SHELL-21

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `GET /api/auth/me` returns `name`/`plan_tier` per membership
- [x] `PATCH /api/auth/me` (UpdateProfile) returns the same enriched membership shape
- [x] Every pre-existing `Me`/`UpdateProfile` test passes unmodified except assertions extended for the new fields
- [x] Gate check passes on disposable Postgres: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [x] Test count: +4 new tests (2 per handler: happy path with real name/plan, empty-plan passthrough)

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): expose tenant name/plan_tier in /api/auth/me memberships`

---

### T3: `TenantMembership` (frontend type) - add `name`/`plan_tier`

**What**: `TenantMembership` interface gains `name: string`/`plan_tier: string`.
**Where**: `web/src/types/api.ts`
**Depends on**: None
**Reuses**: Existing `TenantMembership` interface (`web/src/types/api.ts:10-13`) - additive fields only.
**Requirement**: SHELL-20, SHELL-21

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `TenantMembership` exports `name`/`plan_tier` as required `string` fields
- [x] Gate check passes: `npx tsc -b --noEmit`

**Tests**: none
**Gate**: build

**Commit**: `feat(web): add name/plan_tier to TenantMembership type`

---

### T4: `tokens.css` rewrite - light/dark tokens, Manrope, new palette

**What**: Replace Nocturne token values with the handoff's light+dark sets under `:root`/`[data-theme="dark"]` (same token names the `ui/` components already consume - `bg`, `surface`, `text`, `divider`, `accent`, `critical`, `success`, `warning`, `neutral-*`), add new shell-only tokens (`sidebar-bg`, `sidebar-hover-bg`, `card-header-bg`, `text-muted`, `topbar-icon`), swap font import to Manrope (400/500/600/700), remap `--radius-sm/md/lg` and `--shadow-*` per the handoff's control/card/drawer scale.
**Where**: `web/src/styles/tokens.css`
**Depends on**: None
**Reuses**: Existing `@theme` block structure and token *names* (`web/src/styles/tokens.css:1-109`) - values change, names mostly don't, per design.md's Risks & Concerns (existing `ui/` components inherit the new palette without being edited this round - accepted).
**Requirement**: SHELL-01, SHELL-02, SHELL-03, SHELL-04

**Tools**:
- MCP: NONE
- Skill: frontend-design

**Done when**:
- [x] `:root` defines the light token set, `[data-theme="dark"]` overrides with the dark set, using the exact hex values from `handoff-new-layout/README.md`'s Global shell/Design tokens tables
- [x] `--color-accent`/`--color-accent-hover` are `#5A46C7`/`#4C3AAE` in both themes
- [x] Status colors (`--color-success`/`--color-warning`/`--color-critical`) are identical in both themes (only their light-mode background tint token differs, if one is added)
- [x] Manrope loaded (Google Fonts `@import`, weights 400/500/600/700), `--font-heading`/`--font-body` point to it
- [x] New shell tokens (`--color-sidebar-bg`, `--color-sidebar-hover-bg`, `--color-card-header-bg`, `--color-text-muted`, `--color-topbar-icon`) defined in both themes
- [x] Gate check passes: `npx tsc -b --noEmit` (no TS impact expected, run as safety net) and `npm run test` (no existing test breaks from the token rename/value change)

**Deviation**: `tokens.test.tsx` had one pre-existing assertion (`define os 3 tokens semânticos em OKLCH`) that hard-required the Nocturne OKLCH format for the 3 semantic tokens. That format is exactly what this task replaces (spec.md AC4/SHELL-04 mandates the handoff's fixed hex values). Updated the assertion to check the new hex values instead of removing/weakening it - see `SPEC_DEVIATION` comment at the call site. No other test was touched; all 296 tests pass.

**Tests**: none
**Gate**: build

**Commit**: `feat(web): replace Nocturne tokens with the new light/dark design system`

---

### T5: Theme-boot inline script (`index.html`)

**What**: Inline `<script>` in `<head>` reads `localStorage["vane:theme"]` and sets `document.documentElement.dataset.theme` synchronously before the bundle mounts, defaulting to light and failing silently if `localStorage` is unavailable.
**Where**: `web/index.html`
**Depends on**: T4
**Reuses**: The `vane:theme` key convention defined in design.md (shared with T6's hook).
**Requirement**: SHELL-05, SHELL-07

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] With no stored value, `document.documentElement.dataset.theme` is unset/`"light"` before first paint (no dark flash)
- [x] With a stored `"dark"` value, the attribute is `"dark"` before first paint
- [x] `localStorage` access wrapped in `try/catch` - a throw (e.g. private-mode restriction) does not block app boot
- [x] Gate check passes: `npx tsc -b --noEmit` (script is plain JS in the HTML, not type-checked, but the app must still build) and `npm run test`

**Tests**: none
**Gate**: build

**Commit**: `feat(web): apply persisted theme before first paint to avoid a flash`

---

### T6: `useThemeToggle` hook

**What**: `useThemeToggle(): { theme: "light" | "dark"; toggleTheme(): void }` - reads/writes `document.documentElement.dataset.theme` and `localStorage["vane:theme"]`.
**Where**: `web/src/lib/useThemeToggle.ts`
**Depends on**: T4
**Reuses**: The `vane:theme` key T5's script already reads on boot.
**Requirement**: SHELL-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `toggleTheme()` flips `theme`, updates `document.documentElement.dataset.theme`, and persists to `localStorage`
- [x] A component consuming the hook re-renders with the new theme after toggling
- [x] `localStorage` write failure (mocked throw) does not throw out of `toggleTheme()`
- [x] Gate check passes: `npm run test`
- [x] Test count: 4 new tests

**Tests**: unit
**Gate**: build

**Commit**: `feat(web): add useThemeToggle hook`

---

### T7: `useSidebarPin` hook

**What**: `useSidebarPin(): { pinned: boolean; togglePinned(): void }` - same shape/persistence convention as T6, key `vane:sidebar-pinned`.
**Where**: `web/src/lib/useSidebarPin.ts`
**Depends on**: None
**Reuses**: Same `localStorage` try/catch pattern as T6 (sibling hook, not a shared abstraction - two tiny hooks, no premature generalization).
**Requirement**: SHELL-14

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `togglePinned()` flips `pinned` and persists to `localStorage`
- [x] Default (no stored value) is `pinned: false`
- [x] `localStorage` failure falls back silently to in-memory state
- [x] Gate check passes: `npm run test`
- [x] Test count: 3 new tests

**Tests**: unit
**Gate**: build

**Commit**: `feat(web): add useSidebarPin hook`

---

### T8: `LogoutConfirmDialog`

**What**: Extract the logout confirmation modal (today inlined in `Sidebar.tsx`) into its own reusable component.
**Where**: `web/src/layout/LogoutConfirmDialog.tsx`
**Depends on**: None
**Reuses**: `Dialog`/`Button` (`web/src/components/ui/{Dialog,Button}.tsx`), the exact copy/i18n keys (`logoutDialog.*`) already used by `Sidebar.tsx:208-223`.
**Requirement**: SHELL-09 (shared by Sidebar and AvatarMenu, per design.md's Risks & Concerns)

**Tools**:
- MCP: NONE
- Skill: frontend-design

**Done when**:
- [x] Component accepts `open`/`onOpenChange`/`onConfirm` props and renders the same copy/behavior `Sidebar.tsx`'s inline modal has today
- [x] Confirming calls `onConfirm`; cancelling closes without calling it
- [x] Gate check passes: `npm run test`
- [x] Test count: 3 new tests

**Tests**: unit
**Gate**: build

**Commit**: `refactor(web): extract LogoutConfirmDialog from Sidebar`

---

### T9: `TenantSwitcher`

**What**: Sidebar tenant-switcher popover - `null` for single-membership users, otherwise lists tenants (initials avatar, name, plan badge, checkmark on active) and calls `switchTenant` on selection.
**Where**: `web/src/layout/TenantSwitcher.tsx`
**Depends on**: T3
**Reuses**: `useAuth().admin.memberships`/`switchTenant` (`web/src/auth/AuthProvider.tsx`) - no new auth plumbing.
**Requirement**: SHELL-10, SHELL-11

**Tools**:
- MCP: NONE
- Skill: frontend-design

**Done when**:
- [x] Renders `null` when `memberships.length <= 1`
- [x] Lists every membership (name, plan badge - `""`/falsy plan shows "Free") with the active tenant checked
- [x] Selecting a different tenant calls `switchTenant(tenantId)`
- [x] A membership missing `name` falls back to displaying `tenant_id` (Edge Case)
- [x] Gate check passes: `npm run test`
- [x] Test count: 5 new tests

**Tests**: unit
**Gate**: build

**Commit**: `feat(web): add TenantSwitcher sidebar popover`

---

### T10: `AvatarMenu`

**What**: Topbar avatar popover - name/email, "Configurações" (owner-only, same gate as today's Settings nav item), "Sair" (opens T8's dialog).
**Where**: `web/src/layout/AvatarMenu.tsx`
**Depends on**: T8
**Reuses**: `useAuth()` (`admin`, `logout`, `hasRole`), `LogoutConfirmDialog` (T8).
**Requirement**: SHELL-08, SHELL-09

**Tools**:
- MCP: NONE
- Skill: frontend-design

**Done when**:
- [x] Shows authenticated user's name/email
- [x] "Configurações" link only rendered for `hasRole(["owner"])`, matching `Sidebar.tsx`'s current gate on the Settings nav item
- [x] "Sair" opens `LogoutConfirmDialog`; confirming calls `logout()`
- [x] Gate check passes: `npm run test`
- [x] Test count: 4 new tests

**Tests**: unit
**Gate**: build

**Commit**: `feat(web): add AvatarMenu topbar popover`

---

### T11: `Topbar`

**What**: 60px topbar - page title (prop), theme toggle, static notification bell (no link, no badge), `AvatarMenu`.
**Where**: `web/src/layout/Topbar.tsx`
**Depends on**: T6, T10
**Reuses**: `useThemeToggle` (T6), `AvatarMenu` (T10).
**Requirement**: SHELL-08

**Tools**:
- MCP: NONE
- Skill: frontend-design

**Done when**:
- [x] Renders the `title` prop
- [x] Theme toggle button calls `toggleTheme()`
- [x] Bell icon renders with no `href`/`onClick` (static, per Assumptions)
- [x] `AvatarMenu` rendered
- [x] Gate check passes: `npm run test`
- [x] Test count: 4 new tests (3 planned + 1 extra covering the static-bell criterion explicitly)

**Tests**: unit
**Gate**: build

**Commit**: `feat(web): add Topbar shell component`

---

### T12: `Sidebar` rewrite

**What**: Replace the current fixed 236px sidebar with the collapsible 72/240px version - hover expand/collapse (suppressed while pinned via T7), 3 nav groups (Monitoramento/Plataforma/Organização, omitting "Planos & Faturamento"), same `hasRole(["owner"])` gates as today, `TenantSwitcher` (T9) at the top, `LogoutConfirmDialog` (T8) reused instead of inlined.
**Where**: `web/src/layout/Sidebar.tsx` (rewritten in place)
**Depends on**: T7, T8, T9
**Reuses**: `useBrandLogoUrl`, existing nav icons/i18n keys (`web/src/layout/Sidebar.tsx` current file), `useAuth().hasRole` gates already applied to Admins/Settings.
**Requirement**: SHELL-02, SHELL-03, SHELL-04, SHELL-06

**Tools**:
- MCP: NONE
- Skill: frontend-design

**Done when**:
- [x] Sidebar is `72px` wide by default, `240px` on hover
- [x] Hover-expand/collapse suppressed while `useSidebarPin().pinned` is true
- [x] "Fixar menu" toggle calls `togglePinned()`
- [x] Three nav groups render with the items listed in spec SHELL-05; "Planos & Faturamento" is absent
- [x] Role gates (Usuários, Configurações) match today's `hasRole(["owner"])` behavior exactly - proven for both an owner and a non-owner session
- [x] Current-route nav item gets the accent-tinted highlight
- [x] `TenantSwitcher` rendered at the top; logout button opens `LogoutConfirmDialog`
- [x] Every existing `Sidebar.test.tsx` assertion that still applies to the new layout passes (rewritten where the old assertions target removed markup - labels updated to match spec SHELL-05's exact copy: "Usuários", "Serviços monitorados", "Domínios & Status", "Poller Status")
- [x] Gate check passes: `npm run test`
- [x] Test count: existing suite (7 tests, rewritten) + 8 new tests (nav highlight, sidebar logout dialog, TenantSwitcher position, hover collapse/expand, pinned suppresses collapse, group omission, pin persistence, Configurações both ways) = 15 total in Sidebar.test.tsx

**Tests**: unit
**Gate**: build

**Commit**: `feat(web): rewrite Sidebar as collapsible 72/240px shell nav`

---

### T13: `AppShell` + wire into `App.tsx`

**What**: New `AppShell` composing `Sidebar` (T12) + `Topbar` (T11) + content area (scroll, `32px 40px` padding, `1200px` max-width); `AuthenticatedLayout` in `App.tsx` uses it in place of the current `<Sidebar/>` + `<main>` markup, keeping `PollerBanner`/`Outlet` exactly where they are today.
**Where**: `web/src/layout/AppShell.tsx`, `web/src/App.tsx` (modified)
**Depends on**: T11, T12
**Reuses**: `AuthenticatedLayout`'s existing `PollerBanner`/`Outlet` placement (`web/src/App.tsx:109-124`) - moved into `AppShell`, not reimplemented.
**Requirement**: SHELL-01, SHELL-08, SHELL-12

**Tools**:
- MCP: NONE
- Skill: frontend-design

**Done when**:
- [x] Every authenticated route renders inside `AppShell` (sidebar + topbar + content area)
- [x] `PollerBanner` still renders in the same position relative to routed content as before
- [x] Content area has its own scroll, `32px 40px` padding, `1200px` max-width centered (arbitrary-value classes `overflow-auto`/`px-[40px] py-[32px]`/`max-w-[1200px] mx-auto` - build-gate/source-verified, same precedent as T4's token-literal work; jsdom doesn't compute layout pixels for a runtime assertion)
- [x] Topbar title derives from the current route (a simple pathname→label lookup in `AppShell`, per design.md's Risks & Concerns mitigation)
- [x] Every existing `App.test.tsx` assertion covering authenticated routing still passes
- [x] Gate check passes: `npx tsc -b --noEmit && npm run test`
- [x] Test count: existing suite (331 total, unmodified) + 3 new tests in `AppShell.test.tsx` (title derivation ×2, PollerBanner position preserved)

**Tests**: unit
**Gate**: build

**Commit**: `feat(web): add AppShell, replace AuthenticatedLayout's sidebar+main with it`

---

### T14: `TenantSelector.tsx` - show real tenant name

**What**: Full-page tenant selector (existing, shown when a multi-tenant user hasn't picked an active tenant yet) displays `membership.name` instead of the raw `tenant_id`, falling back to `tenant_id` if `name` is empty.
**Where**: `web/src/features/auth/TenantSelector.tsx`
**Depends on**: T3
**Reuses**: Existing component structure (`web/src/features/auth/TenantSelector.tsx:58-66`) - one-line display change, not a rewrite.
**Requirement**: SHELL-20 (consolidated fix, per design.md's Components section)

**Tools**:
- MCP: NONE
- Skill: frontend-design

**Done when**:
- [ ] Renders `membership.name` for each listed tenant
- [ ] Falls back to `membership.tenant_id` when `name` is empty (Edge Case)
- [ ] Existing `TenantSelector.test.tsx` assertions pass, updated where they asserted the old raw-ID display
- [ ] Gate check passes: `npm run test`
- [ ] Test count: existing suite updated, +1 new test (fallback case)

**Tests**: unit
**Gate**: build

**Commit**: `fix(web): show real tenant name in TenantSelector instead of raw tenant_id`

---

## Phase Execution Map

```
Phase 1 → Phase 2 → Phase 3 → Phase 4

Phase 1:  T1 ------→ T2
Phase 2:  T3
          T4 ------→ T5
          T4 ------→ T6
          T7
Phase 3:  T3 ------→ T9 ------→ T12 ------→ T13
          T6 ------→ T11 ------→ T13
          T8 ------→ T10 ------→ T11
          T7 ------→ T12
          T8 ------→ T12
Phase 4:  T3 ------→ T14
```

Execution is strictly sequential within a phase - no intra-phase parallelism. T4/T5's and T4/T6's `Depends on: T4` chains and T3/T7's independence are the only intra-Phase-2 orderings; all of Phase 2 still executes in the listed order (T3 → T4 → T5 → T6 → T7) since tasks within a phase run in sequence regardless of whether every pair has a real dependency.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: ListForUser JOIN | 1 repository method | ✅ Granular |
| T2: meMembership wiring | 1 file, 1 cohesive concern (2 call sites of the same mapping) | ✅ Granular |
| T3: TenantMembership type | 1 type, 1 file | ✅ Granular |
| T4: tokens.css rewrite | 1 file, 1 concern (token values) | ✅ Granular |
| T5: theme-boot script | 1 file, 1 concern | ✅ Granular |
| T6: useThemeToggle | 1 hook | ✅ Granular |
| T7: useSidebarPin | 1 hook | ✅ Granular |
| T8: LogoutConfirmDialog | 1 component | ✅ Granular |
| T9: TenantSwitcher | 1 component | ✅ Granular |
| T10: AvatarMenu | 1 component | ✅ Granular |
| T11: Topbar | 1 component | ✅ Granular |
| T12: Sidebar rewrite | 1 component (1 file) | ✅ Granular |
| T13: AppShell + wiring | 2 files, 1 cohesive concern (new shell + its single call site) | ✅ Granular |
| T14: TenantSelector fix | 1 file, 1-line display change | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | — | ✅ Match |
| T2 | T1 | T1 → T2 | ✅ Match |
| T3 | None | — | ✅ Match |
| T4 | None | — | ✅ Match |
| T5 | T4 | T4 → T5 | ✅ Match |
| T6 | T4 | T4 → T6 | ✅ Match |
| T7 | None | — | ✅ Match |
| T8 | None | — | ✅ Match |
| T9 | T3 | T3 → T9 | ✅ Match |
| T10 | T8 | T8 → T10 | ✅ Match |
| T11 | T6, T10 | T6 → T11 (via T10 chain), T10 → T11 | ✅ Match |
| T12 | T7, T8, T9 | T7 → T12, T8 → T12, T9 → T12 | ✅ Match |
| T13 | T11, T12 | T11 → T13, T12 → T13, T9 → T13 | ✅ Match |
| T14 | T3 | T3 → T14 | ✅ Match |

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1 | Repository | integration | integration | ✅ OK |
| T2 | API handler | integration | integration | ✅ OK |
| T3 | Frontend type | none | none | ✅ OK |
| T4 | Frontend config (CSS tokens) | none | none | ✅ OK |
| T5 | Frontend config (HTML script) | none | none | ✅ OK |
| T6 | Hook | unit | unit | ✅ OK |
| T7 | Hook | unit | unit | ✅ OK |
| T8 | Component | unit | unit | ✅ OK |
| T9 | Component | unit | unit | ✅ OK |
| T10 | Component | unit | unit | ✅ OK |
| T11 | Component | unit | unit | ✅ OK |
| T12 | Component | unit | unit | ✅ OK |
| T13 | Component + wiring | unit | unit | ✅ OK |
| T14 | Component (modified) | unit | unit | ✅ OK |
