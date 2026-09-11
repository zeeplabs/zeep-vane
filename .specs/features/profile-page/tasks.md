# Meu Perfil Page Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/profile-page/design.md`
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase, project guidelines, and spec - confirm before Execute. Guidelines found: `AGENTS.md` (§3 backend/frontend gates, §5 MSW-shape rule), `.github/workflows/ci.yml`, `web/package.json` (`test` = `vitest run`).

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| ---------- | ------------------ | -------------------- | ---------------- | ----------- |
| Go API handler (DB-touching) | integration | `/me` returns both states of `two_factor_enabled`; existing `/me` tests still pass | `internal/api/*_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=<disposable> go test -tags=integration -p 1 ./internal/api/...` |
| React components (cards, drawer, dialog, page) | unit | each flow: happy path + every listed edge/error state | `web/src/**/*.test.tsx` | `cd web && npm run test` |
| React hooks | unit | each mutation: success + error branches | `web/src/**/*.test.ts` | `cd web && npm run test` |
| Auth context provider | unit | `two_factor_enabled` passthrough + `refreshAdmin` re-hydrates | `web/src/auth/AuthProvider.test.tsx` | `cd web && npm run test` |
| Routing | unit | `/profile` renders inside the authenticated layout | `web/src/App.test.tsx` | `cd web && npm run test` |
| MSW handlers (test infrastructure) | none | build gate only - shapes verified by the consuming hook/component tests | `web/src/test/msw/handlers.ts` | build gate only |
| i18n resources | none | build gate only; no parity test exists in the repo | `web/src/lib/i18n.ts` | build gate only |

## Gate Check Commands

> Generated from codebase - confirm before Execute. The integration DB is always a disposable container with `max_connections=300` (AGENTS.md §3), never `vane-dev-pg`.

| Gate Level | When to Use | Command |
| ---------- | ----------- | ------- |
| Quick | After frontend-only tasks (unit tests) | `cd web && npx tsc -b --noEmit && npm run test` |
| Full | After tasks touching the Go API (integration tests) | `go build ./... && go vet ./... && gofmt -l <changed .go> && TEST_DATABASE_URL="<disposable>" go test -tags=integration -p 1 ./...` |
| Build | After infrastructure/config-only tasks and at phase completion | `go build ./... && go vet ./... && cd web && npx tsc -b --noEmit && npm run test` |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Foundation (backend + shared frontend)

```
T1
T2 -> T3
T2 -> T5
T3 -> T5
T2 -> T6
T3 -> T6
T4
```

### Phase 2: Leaf components

```
T5 -> T7
T6 -> T8
T6 -> T9
T6 -> T10
T8 -> T10
T9 -> T10
T5 -> T11
T10 -> T11
T12
```

### Phase 3: Page and wiring

```
T7 -> T13
T11 -> T13
T12 -> T13
T13 -> T14 -> T15
```

---

## Task Breakdown

### T1: Add `two_factor_enabled` to GET /api/auth/me

**What**: Add a `TwoFactorEnabled bool` field to `meResponse` and set it in `Me` by reading `twoFactorStore.GetSecret` (`true` only when a secret exists with `EnabledAt != nil`; `ErrNotFound` or any error degrades to `false`, never failing `/me`).
**Where**: `internal/api/auth_handler.go`
**Depends on**: None
**Reuses**: `twoFactorStore` interface already injected into `AuthHandler`
**Requirement**: PROFPAGE-23

**Tools**:

- MCP: NONE
- Skill: `security-best-practices` (Go), `coding-guidelines`

**Done when**:

- [x] `meResponse` serializes `two_factor_enabled` (always present)
- [x] `Me` returns `false` for a user with no enrollment and `true` for a confirmed one
- [x] Integration test covers both states
- [x] Gate check passes: `go build ./... && go vet ./... && gofmt -l internal/api/auth_handler.go && TEST_DATABASE_URL="<disposable>" go test -tags=integration -p 1 ./internal/api/...`
- [x] Test count: existing `internal/api` integration tests unchanged + ≥2 new assertions pass (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(auth): expose two_factor_enabled on GET /api/auth/me`

---

### T2: MSW handlers for profile and 2FA endpoints

**What**: Add MSW handlers mirroring the real contracts exactly: `PATCH /api/auth/me`, `POST /api/auth/change-password`, `POST /api/auth/2fa/enroll`, `POST /api/auth/2fa/confirm`, `POST /api/auth/2fa/disable`; add `two_factor_enabled` to the `/api/auth/me` response.
**Where**: `web/src/test/msw/handlers.ts`
**Depends on**: None
**Reuses**: existing `resetSessions`/`resetAuthSession` state and `paginatedPage`-style handler conventions
**Requirement**: PROFPAGE-23

**Tools**:

- MCP: NONE
- Skill: `coding-guidelines`

**Done when**:

- [x] Five endpoints respond with the exact backend shapes (`{secret,otpauth_uri}`, `{recovery_codes[10]}`, `{"status":"ok"}`, 409/401/422 error bodies per design)
- [x] `/api/auth/me` includes `two_factor_enabled` reflecting per-test state
- [x] Gate check passes: `cd web && npx tsc -b --noEmit`
- [x] Consuming tests in T3/T5/T6 pass against these handlers

**Tests**: none
**Gate**: build

**Commit**: `test(msw): add profile and 2FA endpoint handlers`

---

### T3: AuthProvider `two_factor_enabled` + `refreshAdmin()`

**What**: Add `two_factor_enabled: boolean` to `AuthenticatedAdmin` and a `refreshAdmin(): Promise<void>` to `AuthContextValue` that re-hydrates `GET /api/auth/me` and dispatches `AUTHENTICATED` (same path as the boot effect); never throws.
**Where**: `web/src/auth/AuthProvider.tsx`
**Depends on**: T2
**Reuses**: the boot-time `/api/auth/me` fetch effect
**Requirement**: PROFPAGE-05, PROFPAGE-12

**Tools**:

- MCP: NONE
- Skill: `coding-guidelines`

**Done when**:

- [x] `admin.two_factor_enabled` is exposed from `/me`
- [x] `refreshAdmin()` fetches `/me` and updates `admin` without a reload; a fetch failure leaves the previous `admin` intact
- [x] `AuthProvider.test.tsx` covers field passthrough and a refresh that changes `two_factor_enabled`
- [x] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test`
- [x] Test count: ≥2 new tests pass (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(auth): refresh authenticated identity and expose 2FA status`

---

### T4: i18n keys for the profile page

**What**: Add `profile.*` and `twoFactor.*` keys (pt-BR and en) for every string the page renders: card titles, field labels, buttons, success/error/validation messages, and the recovery-codes copy.
**Where**: `web/src/lib/i18n.ts`
**Depends on**: None
**Reuses**: existing `sessions.*` key structure and pt/en trees
**Requirement**: PROFPAGE-01

**Tools**:

- MCP: NONE
- Skill: `coding-guidelines`

**Done when**:

- [x] Every new string exists in both `pt` and `en`
- [x] No component in T5-T15 hardcodes user-facing copy
- [x] Gate check passes: `cd web && npx tsc -b --noEmit`

**Tests**: none
**Gate**: build

**Commit**: `feat(i18n): add Meu Perfil page strings (pt-BR/en)`

---

### T5: Profile hooks (`useUpdateProfileName`, `useChangePassword`)

**What**: Two mutations: `useUpdateProfileName` (`PATCH /api/auth/me` `{name}` → `refreshAdmin()` on success) and `useChangePassword` (`POST /api/auth/change-password` `{current_password,new_password}`), surfacing `ApiError` status to callers.
**Where**: `web/src/features/profile/hooks.ts`
**Depends on**: T2, T3
**Reuses**: `useMutation` pattern from `web/src/features/sessions/hooks.ts`; `apiFetch`
**Requirement**: PROFPAGE-04, PROFPAGE-07, PROFPAGE-08, PROFPAGE-09, PROFPAGE-10

**Tools**:

- MCP: NONE
- Skill: `coding-guidelines`

**Done when**:

- [x] Name mutation sends `{name}` and calls `refreshAdmin()` on success
- [x] Password mutation sends the exact body and propagates 401/422 as `ApiError`
- [x] `hooks.test.ts` covers success + 401 + 422 branches
- [x] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test`
- [x] Test count: ≥4 new tests pass (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(profile): add name and password mutation hooks`

---

### T6: Two-factor hooks (`useEnroll2FA`, `useConfirm2FA`, `useDisable2FA`)

**What**: Three mutations over the existing endpoints: enroll (returns `{secret, otpauth_uri}`), confirm (`{code}` → `{recovery_codes}`), disable (`{current_password}` → `refreshAdmin()` on success).
**Where**: `web/src/features/two-factor/hooks.ts`
**Depends on**: T2, T3
**Reuses**: `useMutation` pattern; `apiFetch`; `useAuth().refreshAdmin`
**Requirement**: PROFPAGE-13, PROFPAGE-14, PROFPAGE-17, PROFPAGE-18

**Tools**:

- MCP: NONE
- Skill: `coding-guidelines`

**Done when**:

- [x] Enroll returns the URI + secret; confirm returns 10 codes; disable refreshes the identity
- [x] Error branches (409 enroll, 422 confirm, 401 disable) propagate as `ApiError`
- [x] `hooks.test.ts` covers success + each error branch
- [x] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test`
- [x] Test count: ≥6 new tests pass (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(two-factor): add enroll, confirm and disable hooks`

---

### T7: PersonalInfoCard

**What**: Card with an initials avatar, an editable name field (submit via `useUpdateProfileName`, inline empty-name validation, success toast) and a read-only email.
**Where**: `web/src/features/profile/PersonalInfoCard.tsx`
**Depends on**: T5
**Reuses**: `Card`, `Field`, `Input`, `Button`, `sonner`, `react-i18next` keys from T4
**Requirement**: PROFPAGE-04, PROFPAGE-05, PROFPAGE-06

**Tools**:

- MCP: NONE
- Skill: `coding-guidelines`

**Done when**:

- [x] Renders name, read-only email and initials avatar
- [x] Empty name blocks submit with an inline error; valid name submits and shows a toast
- [x] `PersonalInfoCard.test.tsx` covers success + empty validation + display of the refreshed name
- [x] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test`
- [x] Test count: ≥3 new tests pass (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(profile): add personal info card`

---

### T8: EnrollDrawer

**What**: `Drawer` implementing the 3-step enrollment flow (`scan` → `verify` → `codes`): render `secret`/`otpauth_uri` (`QRCodeSVG` from `qrcode.react` + manual secret), submit a code (`useConfirm2FA`, inline error on 422, stays on `verify`), then show the 10 recovery codes once with copy/download and a "will not be shown again" warning before closing.
**Where**: `web/src/features/two-factor/EnrollDrawer.tsx`
**Depends on**: T6
**Reuses**: `Drawer`, `Button`, `qrcode.react` `QRCodeSVG`
**Requirement**: PROFPAGE-13, PROFPAGE-14, PROFPAGE-15, PROFPAGE-16

**Tools**:

- MCP: context7 (qrcode.react usage)
- Skill: `coding-guidelines`

**Done when**:

- [x] Scan step shows a QR + the raw secret
- [x] Invalid code shows an inline error and keeps step `verify`
- [x] Confirm shows the 10 codes once and calls `onEnabled` after dismissal
- [x] `EnrollDrawer.test.tsx` covers all three steps + invalid code
- [x] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test`
- [x] Test count: ≥4 new tests pass (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(two-factor): add enrollment drawer`

---

### T9: DisableDialog

**What**: `Dialog` requiring the current password, calling `useDisable2FA`; inline error on 401, on success calls `onDisabled`.
**Where**: `web/src/features/two-factor/DisableDialog.tsx`
**Depends on**: T6
**Reuses**: `Dialog`, `Input`, `Button`, `react-i18next` keys
**Requirement**: PROFPAGE-17, PROFPAGE-18

**Tools**:

- MCP: NONE
- Skill: `coding-guidelines`

**Done when**:

- [x] Wrong password shows an inline error and keeps 2FA enabled
- [x] Correct password calls disable and triggers `onDisabled`
- [x] `DisableDialog.test.tsx` covers success + wrong-password
- [x] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test`
- [x] Test count: ≥2 new tests pass (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(two-factor): add disable confirmation dialog`

---

### T10: TwoFactorCard

**What**: Card showing enabled/disabled state from `admin.two_factor_enabled` with an action: enable opens `EnrollDrawer`; disable opens `DisableDialog`; on success the card reflects the refreshed state.
**Where**: `web/src/features/two-factor/TwoFactorCard.tsx`
**Depends on**: T6, T8, T9
**Reuses**: `Card`, `Tag`, `Button`, `useAuth()`
**Requirement**: PROFPAGE-12, PROFPAGE-13, PROFPAGE-17

**Tools**:

- MCP: NONE
- Skill: `coding-guidelines`

**Done when**:

- [x] Disabled state shows "Ativar" and opens the drawer
- [x] Enabled state shows the badge and "Desativar" opening the dialog
- [x] `TwoFactorCard.test.tsx` covers both states + enable/disable transitions
- [x] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test`
- [x] Test count: ≥3 new tests pass (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(two-factor): add 2FA status card`

---

### T11: SecurityCard (password form + 2FA)

**What**: "Segurança" card hosting the current/new/confirm password form (mismatch blocks submit; `useChangePassword`; inline 401/422 errors; clears + toast on success) and `<TwoFactorCard/>`.
**Where**: `web/src/features/profile/SecurityCard.tsx`
**Depends on**: T5, T10
**Reuses**: `Card`, `Field`, `Input`, `Button`, `sonner`, keys from T4
**Requirement**: PROFPAGE-07, PROFPAGE-08, PROFPAGE-09, PROFPAGE-10, PROFPAGE-11

**Tools**:

- MCP: NONE
- Skill: `coding-guidelines`

**Done when**:

- [x] Mismatched confirmation blocks submit with an inline error
- [x] 401 shows the current-password error; 422 shows the policy error; success clears + toasts
- [x] Renders `<TwoFactorCard/>`
- [x] `SecurityCard.test.tsx` covers mismatch, 401, 422, success
- [x] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test`
- [x] Test count: ≥4 new tests pass (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(profile): add security card with password form`

---

### T12: Session revoke confirmation in SessionsSection

**What**: Gate the existing revoke mutation behind a "Encerrar sessão?" `Dialog`; the confirm action calls `revoke.mutate(id)`, cancel closes without a request.
**Where**: `web/src/features/sessions/SessionsSection.tsx`
**Depends on**: None
**Reuses**: `Dialog`, existing `useRevokeSession`; pattern from `web/src/layout/LogoutConfirmDialog.tsx`
**Requirement**: PROFPAGE-19, PROFPAGE-20, PROFPAGE-21, PROFPAGE-22

**Tools**:

- MCP: NONE
- Skill: `coding-guidelines`

**Done when**:

- [x] Clicking "Encerrar" opens the dialog and sends no request
- [x] Confirming revokes and removes the row; cancelling keeps it
- [x] Existing `SessionsSection.test.tsx` updated to go through the dialog
- [x] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test`
- [x] Test count: existing tests (updated) + ≥2 new (confirm/cancel) pass (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(sessions): confirm before revoking a session`

---

### T13: ProfilePage

**What**: Page component rendering the "Meu Perfil" header and composing `<PersonalInfoCard/>`, `<SecurityCard/>` and `<SessionsSection/>`.
**Where**: `web/src/features/profile/ProfilePage.tsx`
**Depends on**: T7, T11, T12
**Reuses**: `PersonalInfoCard`, `SecurityCard`, `SessionsSection`, page-layout conventions
**Requirement**: PROFPAGE-01

**Tools**:

- MCP: NONE
- Skill: `coding-guidelines`

**Done when**:

- [x] Renders the three cards and the header
- [x] `ProfilePage.test.tsx` asserts the three cards render
- [x] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test`
- [x] Test count: ≥1 new test passes (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(profile): add Meu Perfil page`

---

### T14: Route `/profile`

**What**: Register `<Route path="/profile" element={<ProfilePage />} />` inside the existing authenticated route group (no `RequireRole`).
**Where**: `web/src/App.tsx`
**Depends on**: T13
**Reuses**: existing authenticated `<Route>` group in `App.tsx`
**Requirement**: PROFPAGE-01, PROFPAGE-03

**Tools**:

- MCP: NONE
- Skill: `coding-guidelines`

**Done when**:

- [x] `/profile` renders `ProfilePage` for an authenticated user
- [x] An unauthenticated request redirects via `RequireAuth`
- [x] `App.test.tsx` covers the authenticated render (and existing routing tests still pass)
- [x] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test`
- [x] Test count: ≥1 new test passes (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(profile): route /profile inside the authenticated layout`

---

### T15: "Meu Perfil" entry in the user menu

**What**: Add a "Meu Perfil" link to the topbar `AvatarMenu` pointing at `/profile`, above the existing `/settings` link, for every role.
**Where**: `web/src/layout/AvatarMenu.tsx`
**Depends on**: T14
**Reuses**: existing `AvatarMenu` menu-item pattern
**Requirement**: PROFPAGE-02

**Tools**:

- MCP: NONE
- Skill: `coding-guidelines`

**Done when**:

- [x] The user menu shows "Meu Perfil" linking to `/profile`
- [x] `AvatarMenu.test.tsx` asserts the link target
- [x] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test`
- [x] Test count: ≥1 new test passes (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(layout): add Meu Perfil link to the user menu`

---

## Phase Execution Map

Visual representation of task ordering. Phases run in sequence, and tasks within a phase run in order:

| Phase | Order |
| ----- | ----- |
| 1 | T1 and T4 independent; T2 > T3 > T5; T2 > T3 > T6 |
| 2 | T7; T8 > T10 > T11; T9 > T10; T12 |
| 3 | T13 > T14 > T15 |

Execution is strictly sequential - there is no intra-phase parallelism. A single agent (or batch worker) works one task at a time, in order.

**Batch plan (~7 tasks/worker, whole phases):** Phase 1 (6 tasks) = batch 1; Phase 2 (6 tasks) = batch 2; Phase 3 (3 tasks) = batch 3 → **3 batches**, so sub-agents are offered at Execute per the skill.

---

## Task Granularity Check

| Task | Scope | Status |
| ---- | ----- | ------ |
| T1: `/me` flag | 1 handler file change | ✅ Granular |
| T2: MSW handlers | 1 file (5 handlers, cohesive) | ✅ Granular |
| T3: AuthProvider | 1 file | ✅ Granular |
| T4: i18n keys | 1 resource file | ✅ Granular |
| T5: profile hooks | 1 file (2 hooks, cohesive) | ✅ Granular |
| T6: two-factor hooks | 1 file (3 hooks, cohesive) | ✅ Granular |
| T7: PersonalInfoCard | 1 component | ✅ Granular |
| T8: EnrollDrawer | 1 component | ✅ Granular |
| T9: DisableDialog | 1 component | ✅ Granular |
| T10: TwoFactorCard | 1 component | ✅ Granular |
| T11: SecurityCard | 1 component | ✅ Granular |
| T12: SessionsSection confirm | 1 component change | ✅ Granular |
| T13: ProfilePage | 1 component | ✅ Granular |
| T14: `/profile` route | 1 file change | ✅ Granular |
| T15: AvatarMenu link | 1 file change | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| ---- | ---------------------- | ------------- | ------ |
| T1 | None | (none) | ✅ Match |
| T2 | None | (none) | ✅ Match |
| T3 | T2 | T2 -> T3 | ✅ Match |
| T4 | None | (none) | ✅ Match |
| T5 | T2, T3 | T2 -> T5, T3 -> T5 | ✅ Match |
| T6 | T2, T3 | T2 -> T6, T3 -> T6 | ✅ Match |
| T7 | T5 | (cross-phase, excluded by validator) | ✅ Match |
| T8 | T6 | (cross-phase) | ✅ Match |
| T9 | T6 | (cross-phase) | ✅ Match |
| T10 | T6, T8, T9 | T8 -> T10, T9 -> T10 (intra); T6 cross-phase | ✅ Match |
| T11 | T5, T10 | T10 -> T11 (intra); T5 cross-phase | ✅ Match |
| T12 | None | (none) | ✅ Match |
| T13 | T7, T11, T12 | (all cross-phase) | ✅ Match |
| T14 | T13 | T13 -> T14 | ✅ Match |
| T15 | T14 | T14 -> T15 | ✅ Match |

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| ---- | --------------------------- | --------------- | --------- | ------ |
| T1 | Go API handler | integration | integration | ✅ OK |
| T2 | MSW handlers (test infra) | none | none | ✅ OK |
| T3 | Auth context provider | unit | unit | ✅ OK |
| T4 | i18n resources | none | none | ✅ OK |
| T5 | React hooks | unit | unit | ✅ OK |
| T6 | React hooks | unit | unit | ✅ OK |
| T7 | React component | unit | unit | ✅ OK |
| T8 | React component | unit | unit | ✅ OK |
| T9 | React component | unit | unit | ✅ OK |
| T10 | React component | unit | unit | ✅ OK |
| T11 | React component | unit | unit | ✅ OK |
| T12 | React component | unit | unit | ✅ OK |
| T13 | React component | unit | unit | ✅ OK |
| T14 | Routing | unit | unit | ✅ OK |
| T15 | React component | unit | unit | ✅ OK |
