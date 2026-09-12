# Login 2FA Step Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/login-2fa/design.md`
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase, project guidelines, and spec - confirm before Execute. Guidelines found: `AGENTS.md` (§3 frontend gates, §5 MSW-shape rule), `web/package.json` (`test` = `vitest run`). No backend layer is listed: this feature changes no Go code.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| ---------- | ------------------ | -------------------- | ---------------- | ----------- |
| Auth context provider | unit | `login` returns both outcomes; `verifyTwoFactor` hydrates; challenge path sets no admin | `web/src/auth/AuthProvider.test.tsx` | `cd web && npm run test` |
| React component (step + page) | unit | credentials->step, valid/invalid code, recovery, back, non-2FA unchanged | `web/src/features/auth/*.test.tsx` | `cd web && npm run test` |
| MSW handlers (test infrastructure) | none | build gate only - shapes verified by the consuming provider/page tests | `web/src/test/msw/handlers.ts` | build gate only |
| i18n resources | none | build gate only; no parity test exists in the repo | `web/src/lib/i18n.ts` | build gate only |

## Gate Check Commands

> Generated from codebase - confirm before Execute. This feature touches no Go code, so the integration gate is not required.

| Gate Level | When to Use | Command |
| ---------- | ----------- | ------- |
| Quick | After frontend-only tasks (unit tests) | `cd web && npx tsc -b --noEmit && npm run test` |
| Full | After tasks touching the Go API (not used here) | `go build ./... && go vet ./... && gofmt -l <changed .go> && TEST_DATABASE_URL="<disposable>" go test -tags=integration -p 1 ./...` |
| Build | After infrastructure/config-only tasks and at phase completion | `go build ./... && go vet ./... && cd web && npx tsc -b --noEmit && npm run test` |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Foundation

```
T1 -> T3
T2 -> T4
```

### Phase 2: Login page

```
T3 -> T5
T4 -> T5
```

---

## Task Breakdown

### T1: MSW handlers for the login 2FA branch

**What**: Make `POST /api/auth/login` return `200 {challenge_token}` and set no session when the mock 2FA state is enabled; add `POST /api/auth/login/verify-2fa` accepting `mswValidTotpCode` or a seeded recovery code, establishing the session (`sessionAdminId` + `currentSessionId`) exactly like login, else `401 {"error":"invalid or expired verification"}`; export a recovery-code seed helper.
**Where**: `web/src/test/msw/handlers.ts`
**Depends on**: None
**Reuses**: existing `sessionAdminId`/`currentSessionId` wiring, `setTwoFactorEnabled`, `mswValidTotpCode`
**Requirement**: LOGIN2FA-01, LOGIN2FA-05

**Tools**:

- MCP: NONE
- Skill: `coding-guidelines`

**Done when**:

- [x] login returns `{challenge_token}` and leaves `sessionAdminId` null when 2FA is enabled
- [x] verify-2fa sets the session on the valid code and on a seeded recovery code, and 401s otherwise
- [x] a seeded recovery code is single-use
- [x] Gate check passes: `cd web && npx tsc -b --noEmit`
- [ ] Consuming tests in T3/T5 pass against these handlers

**Tests**: none
**Gate**: build

**Commit**: `test(msw): add login 2FA challenge and verify handlers`

---

### T2: i18n keys for the login 2FA step

**What**: Add `login.twoFactor.*` keys (pt-BR and en) for the step title, instructions, code field, recovery toggle and field, submit, back, invalid and generic errors.
**Where**: `web/src/lib/i18n.ts`
**Depends on**: None
**Reuses**: existing `login.*` key structure and pt/en trees
**Requirement**: LOGIN2FA-12

**Tools**:

- MCP: NONE
- Skill: `coding-guidelines`

**Done when**:

- [x] Every new string exists in both `pt` and `en`
- [x] No component in T4/T5 hardcodes user-facing copy
- [x] Gate check passes: `cd web && npx tsc -b --noEmit`

**Tests**: none
**Gate**: build

**Commit**: `feat(i18n): add login two-factor strings (pt-BR/en)`

---

### T3: AuthProvider typed login outcome + verifyTwoFactor

**What**: `login` returns `{kind:"authenticated"}` or `{kind:"twoFactorRequired",challengeToken}` (challenge path calls no `/me` and dispatches nothing); add `verifyTwoFactor(challengeToken, factor)` posting `{challenge_token, code}` or `{challenge_token, recovery_code}` and, on 200, hydrating `/me` + dispatching `AUTHENTICATED` like login.
**Where**: `web/src/auth/AuthProvider.tsx`
**Depends on**: T1
**Reuses**: the existing boot/login `/me` hydrate path, `apiFetch`
**Requirement**: LOGIN2FA-04, LOGIN2FA-10, LOGIN2FA-11

**Tools**:

- MCP: NONE
- Skill: `coding-guidelines`

**Done when**:

- [x] `login` returns `twoFactorRequired` with the token and leaves `admin` null on a challenge response
- [x] `login` still returns `authenticated` and hydrates for a non-2FA user
- [x] `verifyTwoFactor` hydrates the admin on success and propagates `ApiError` on 401
- [x] `AuthProvider.test.tsx` covers all three branches
- [x] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test`
- [x] Test count: ≥4 new tests pass (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(auth): add typed login outcome and verifyTwoFactor`

---

### T4: LoginTwoFactorStep component

**What**: Presentational verification step: a 6-digit code field, a method toggle (app code / recovery code), submit, "Voltar" and an inline error; switching methods clears the other method's input and the error.
**Where**: `web/src/features/auth/LoginTwoFactorStep.tsx`
**Depends on**: T2
**Reuses**: `Field`, `Button`, `useTranslation`, keys from T2
**Requirement**: LOGIN2FA-03, LOGIN2FA-07, LOGIN2FA-08

**Tools**:

- MCP: NONE
- Skill: `coding-guidelines`

**Done when**:

- [x] Renders the code field, toggle, submit and back button
- [x] Switching to recovery clears the code input and the error (and vice versa)
- [x] Submitting calls `onSubmit` with the active method's factor; "Voltar" calls `onBack`
- [x] `LoginTwoFactorStep.test.tsx` covers toggle-clear, submit payload and back
- [x] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test`
- [x] Test count: ≥3 new tests pass (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(auth): add login two-factor step component`

---

### T5: LoginPage two-step wiring

**What**: Drive the `credentials -> twoFactor` state machine: on `login` success navigate `/`; on `twoFactorRequired` store the token and show the step; on verify success navigate `/`; on failure show the inline error and stay; "Voltar" resets to credentials and drops the token; the token is held only in state.
**Where**: `web/src/features/auth/LoginPage.tsx`
**Depends on**: T3, T4
**Reuses**: `LoginTwoFactorStep`, `useAuth`, existing layout/`EyeIcon`/error patterns
**Requirement**: LOGIN2FA-01, LOGIN2FA-02, LOGIN2FA-03, LOGIN2FA-05, LOGIN2FA-06, LOGIN2FA-08, LOGIN2FA-09

**Tools**:

- MCP: NONE
- Skill: `coding-guidelines`

**Done when**:

- [ ] Correct 2FA-enabled credentials show the step and establish no session
- [ ] A valid code (or valid recovery code) signs in and navigates
- [ ] An invalid code/recovery shows the inline error and keeps the step
- [ ] "Voltar" returns to credentials; the token is never in URL/storage
- [ ] A non-2FA login is unchanged
- [ ] `LoginPage.test.tsx` covers all the above
- [ ] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test`
- [ ] Test count: ≥5 new tests pass (no silent deletions)

**Tests**: unit
**Gate**: build

**Commit**: `feat(auth): handle the 2FA step in the login page`

---

## Phase Execution Map

Visual representation of task ordering. Phases run in sequence, and tasks within a phase run in order:

| Phase | Order |
| ----- | ----- |
| 1 | T1 > T3; T2 > T4 |
| 2 | T5 (after T3 and T4) |

Execution is strictly sequential - there is no intra-phase parallelism. A single agent (or batch worker) works one task at a time, in order.

**Batch plan (~7 tasks/worker, whole phases):** 5 tasks total -> fits **one batch** (≤ ~8), so Execute runs **inline** with no sub-agents.

---

## Task Granularity Check

| Task | Scope | Status |
| ---- | ----- | ------ |
| T1: MSW login/verify handlers | 1 file (cohesive handlers) | ✅ Granular |
| T2: i18n keys | 1 resource file | ✅ Granular |
| T3: AuthProvider outcome + verify | 1 file (2 related changes) | ✅ Granular |
| T4: LoginTwoFactorStep | 1 component | ✅ Granular |
| T5: LoginPage wiring | 1 component change | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| ---- | ---------------------- | ------------- | ------ |
| T1 | None | (none) | ✅ Match |
| T2 | None | (none) | ✅ Match |
| T3 | T1 | T1 -> T3 | ✅ Match |
| T4 | T2 | T2 -> T4 | ✅ Match |
| T5 | T3, T4 | T3 -> T5, T4 -> T5 | ✅ Match |

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| ---- | --------------------------- | --------------- | --------- | ------ |
| T1 | MSW handlers (test infra) | none | none | ✅ OK |
| T2 | i18n resources | none | none | ✅ OK |
| T3 | Auth context provider | unit | unit | ✅ OK |
| T4 | React component | unit | unit | ✅ OK |
| T5 | React component | unit | unit | ✅ OK |
