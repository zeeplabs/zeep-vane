# Accept Invite Page Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

**Tools policy (user-approved default, carried over from `admin-invite-resend-cancel`):** use Context7 MCP when researching an unfamiliar API/library pattern during a task, and any other tool judged useful. No task here touches genuinely unfamiliar tech (pure extension of existing Go/React patterns already in this repo - `BootstrapPage`/`BootstrapHandler.Create` are direct templates), so Context7 is expected to be unnecessary in practice.

---

**Design**: `.specs/features/accept-invite-page/design.md`
**Status**: Approved

---

## Test Coverage Matrix

> Generated from codebase sampling (`internal/api/admins_test.go`'s existing `TestAcceptInvite_*` suite, `internal/api/auth_handler_test.go`'s cookie-assertion pattern, `web/src/features/auth/BootstrapPage.test.tsx`) and `Makefile`/CI. Guidelines found: none dedicated (no `AGENTS.md`/coverage-threshold config) - existing test samples set the floor; strong defaults (1:1 AC mapping, every edge case) set the target.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| ---------- | ------------------- | ---------------------- | ------------------- | -------------- |
| `AdminsHandler.AcceptInvite` (cookie issuance on success) | integration | New: happy path asserts `Set-Cookie` presence + attributes. Regression: every existing failure path (401 variants, 422 variants) still passes unchanged - matches existing `admins_test.go` depth | `internal/api/admins_test.go` | `go test -tags=integration ./internal/api/...` |
| `routes.go` wiring (constructor call site) | none | Compiles; reachable end-to-end via the already-public route - build gate only, no dedicated per-route test (this route carries no role gate to test, unlike owner-only routes) | `internal/cli/routes.go` | build gate only |
| Frontend `AcceptInvitePage` + `/accept-invite/:token` route | unit | Happy path (submit → `window.location.assign("/")`), password-mismatch block, 401/422/generic-error message rendering, disabled-while-submitting - matches `BootstrapPage.test.tsx` depth | `web/src/features/auth/AcceptInvitePage.test.tsx` | `cd web && npm run test` |
| MSW mock handler (test infrastructure, not a spec-covered code layer) | none | Supports the frontend scenarios above; no dedicated test of the mock itself | `web/src/test/msw/handlers.ts` | build gate only (`tsc`) |

## Gate Check Commands

> Generated from `Makefile` and existing `//go:build integration` convention (same commands as `admin-invite-resend-cancel/tasks.md`).

| Gate Level | When to Use | Command |
| ---------- | ----------- | ------- |
| Quick (Go unit) | After a task touching only unit-tested Go layers (none in this feature) | `gofmt -l . && go vet ./... && go test ./...` |
| Full (Go + DB) | After tasks touching the handler or routes wiring | `gofmt -l . && go vet ./... && go test ./... && go test -tags=integration ./...` (requires `TEST_DATABASE_URL`) |
| Web quick | After frontend page/route tasks | `cd web && npx tsc -b --noEmit && npm run test` |
| Build (phase close) | After each phase completes | Full (above) **and** Web quick (above) **and** `cd web && npm run build` **and** `make build` |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Backend

`AcceptInvite` gains session-cookie issuance; `routes.go` wires the real dependencies. Tasks: T1, T2.

### Phase 2: Frontend

Consumes the backend contract (unchanged request/response body; only a new response header). Tasks: T3, T4.

---

## Task Breakdown

### T1: Issue a session cookie on `AcceptInvite` success

**What**: Add `sessionSecret string` and `secureCookies bool` fields to `AdminsHandler` (grow `NewAdminsHandler`'s signature accordingly); in `AcceptInvite`, after `h.admins.CreateWithRole` succeeds, call `auth.IssueSession(admin.ID, h.sessionSecret)` and `http.SetCookie(w, sessionCookie(token, int(auth.SessionTTL.Seconds()), h.secureCookies))` before writing the `201` response - exact sequence `bootstrap_handler.go:113-119` already uses.
**Where**: `internal/api/admins.go` (modify `AdminsHandler` struct, `NewAdminsHandler`, `AcceptInvite`)
**Depends on**: None
**Reuses**: `auth.IssueSession`, `sessionCookie` (`internal/api/auth_handler.go:99-109`), `auth.SessionTTL` - all already implemented, zero new logic
**Requirement**: AIP-01, AIP-02

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `NewAdminsHandler` takes the two new params; all existing call sites (`internal/cli/routes.go`, `internal/api/admins_test.go`) updated to compile (routes.go's real value-passing edit is committed under T2, not this commit, even though the file already contains it in the working tree)
- [x] A successful `AcceptInvite` response includes a `Set-Cookie` header matching `sessionCookie`'s shape (name, `HttpOnly`, `Secure`, `SameSite=Strict`, `MaxAge` == `auth.SessionTTL`) and the cookie's token verifies (`auth.VerifySession`) back to the newly created admin's own ID - stronger than round-tripping through `/api/auth/me`, which isn't registered on this test router
- [x] Every existing `TestAcceptInvite_*` test (valid token, expired, already-used, concurrent, missing password, weak password) still passes unmodified in behavior (only the router/handler construction changes to supply the two new params)
- [x] Gate check passes: `go test -tags=integration ./internal/api/...`
- [x] Test count: existing `TestAcceptInvite_*` tests (6) + 1 new (`Set-Cookie` shape + `VerifySession` round-trip) = 7 pass, no silent deletions

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): issue a session cookie on successful AcceptInvite`

---

### T2: Wire `routes.go` call site with real session config

**What**: Update the `api.NewAdminsHandler(...)` call site in `buildAdminRouter` to pass `cfg.SessionSecret` and `cfg.SecureCookies` (both already loaded into `cfg` and already passed to `api.NewAuthHandler`/`api.NewBootstrapHandler` at the same call site).
**Where**: `internal/cli/routes.go` (modify `buildAdminRouter`)
**Depends on**: T1
**Reuses**: `cfg.SessionSecret`, `cfg.SecureCookies` (already-loaded config fields, already used by sibling handlers in this exact function)
**Requirement**: AIP-01

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `adminsHandler` is constructed with `cfg.SessionSecret` and `cfg.SecureCookies`
- [x] Gate check passes: full build gate (`gofmt -l . && go vet ./... && go test ./... && go test -tags=integration ./...` plus `make build`) - no dedicated new test for this task (public route, no role gate to exercise; T1's tests already prove the handler behavior end-to-end)

**Tests**: none
**Gate**: build

**Commit**: `feat(cli): wire session config into AdminsHandler for invite acceptance`

---

### T3: Add MSW mock handler for `POST /api/admins/invite/:token/accept`

**What**: Add a handler to `web/src/test/msw/handlers.ts` mimicking `AcceptInvite`'s contract against the existing `adminInvitesState`: missing/empty `password` → `422` `{"error":"password is required"}`; password shorter than 8 or longer than 72 chars → `422` `{"error":"password must be between 8 and 72 characters"}`; unknown/already-consumed token → `401` `{"error":"invalid or expired invite token"}`; otherwise mark the matching invite consumed (remove it from `adminInvitesState`, mirroring the real `used_at` semantics) and respond `201` `{"email":..., "role":...}`. Add a small test-only helper to seed a known token→invite mapping (mirrors `seedExpiredAdminInvite` from `admin-invite-resend-cancel`).
**Where**: `web/src/test/msw/handlers.ts`
**Depends on**: None
**Reuses**: `adminInvitesState` (already populated by the existing `POST /api/admins` mock, `admin-invite-resend-cancel`); `seedExpiredAdminInvite`'s pattern for a new `seedAdminInviteToken(token, email, role)` test helper
**Requirement**: AIP-01, AIP-05, AIP-07, AIP-08, AIP-09

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] New handler registered for `POST /api/admins/invite/:token/accept`, matching the four response shapes above
- [x] New `seedAdminInviteToken(token, email, role)` export lets a test seed a specific, known token (the real backend never lets a test choose the raw token - the mock must, since the frontend test drives the URL directly)
- [x] Gate check passes: `cd web && npx tsc -b --noEmit` (no dedicated test of the mock itself - it's exercised indirectly by T4's page tests)

**Tests**: none
**Gate**: build

**Commit**: `test(web): add MSW mock for admin invite acceptance`

---

### T4: Add `AcceptInvitePage` and wire its route

**What**: Create `AcceptInvitePage.tsx` mirroring `BootstrapPage.tsx`'s structure (branded two-column layout via `useBrandLogoUrl`, `Field`/`Button` components) but with only password + confirm-password fields (no email - the invite already carries it server-side): reads `token` via `useParams<{token: string}>()`, blocks submit on password/confirm mismatch (client-side only, i18n key `acceptInvite.passwordMismatch`), on submit calls `apiFetch(`/api/admins/invite/${token}/accept`, {method: "POST", body: JSON.stringify({password}), skipUnauthorizedHandler: true})`, on success does `window.location.assign("/")`, on `ApiError` with `status===401` shows `acceptInvite.invalidOrExpired`, with `status===422` shows `err.message` verbatim, otherwise shows `acceptInvite.genericError`; disables submit while in flight. Add the four new i18n keys (pt+en) to `web/src/lib/i18n.ts`. Register `<Route path="/accept-invite/:token" element={<AcceptInvitePage />} />` in `App.tsx`, outside every guard block (same un-guarded placement as `/status/:id`).
**Where**: `web/src/features/auth/AcceptInvitePage.tsx`, `web/src/App.tsx`, `web/src/lib/i18n.ts`
**Depends on**: T3
**Reuses**: `BootstrapPage.tsx`'s exact state/handler/layout shape; `apiFetch`/`ApiError`; `Field`/`Button`; `useBrandLogoUrl`; `BootstrapPage.test.tsx`'s `window.location.assign` spy technique
**Requirement**: AIP-01, AIP-02, AIP-03, AIP-04, AIP-05, AIP-06, AIP-07, AIP-08, AIP-09

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Visiting `/accept-invite/:token` renders the password/confirm form (no email field)
- [ ] Valid submit → `apiFetch` called with the exact body/URL shape above → `window.location.assign("/")` called with `"/"` (spy-asserted, per `BootstrapPage.test.tsx`'s pattern)
- [ ] Mismatched password/confirm → inline message shown, `apiFetch`/`fetch` never called, submit stays enabled
- [ ] Mocked `401` → `acceptInvite.invalidOrExpired` message shown, form still usable (not stuck disabled)
- [ ] Mocked `422` → the mock's exact `error` string shown verbatim
- [ ] Mocked network failure / 500 → `acceptInvite.genericError` shown
- [ ] Submit button disabled while the request is in flight (no double-submit)
- [ ] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test`
- [ ] Test count: at least 6 new tests (happy path, mismatch, 401, 422, generic error, disabled-while-submitting) pass, no silent deletions

**Tests**: unit
**Gate**: build

**Commit**: `feat(web): add /accept-invite/:token page`

---

## Phase Execution Map

Phases run in order: Phase 1 → Phase 2. Within a phase, tasks execute in listed order:

```
T1 ------→ T2
T3 ------→ T4
```

- **Phase 1** (backend): T1, then T2 - T2 depends on T1's signature change.
- **Phase 2** (frontend): T3, then T4 - T4 depends on T3's mock handler to test against. Phase 2 has no dependency on Phase 1's completion (the request/response body contract is unchanged - only a new response header is added, which the frontend never inspects), but runs second per the Execution Plan's stated order.

Execution is strictly sequential - there is no intra-phase parallelism. A single agent works one task at a time, in order. All 4 tasks fit a single batch, so Execute proceeds inline with no sub-agent dispatch (see Sub-Agent Delegation trigger: > ~8 tasks).

---

## Task Granularity Check

| Task | Scope | Status |
| ---- | ----- | ------ |
| T1: Issue session cookie on AcceptInvite | 1 method + constructor signature, 1 file | ✅ Granular |
| T2: Wire routes.go call site | 1 file, 1 concern (constructor call site) | ✅ Granular |
| T3: MSW mock handler | 1 file, 1 handler + 1 test helper | ✅ Granular |
| T4: AcceptInvitePage + route + i18n | 3 files, 1 cohesive concern (one new page and everything needed to render/route/copy it) | ⚠️ 2-3 related things across files, cohesive - no new component beyond the page itself; matches the granularity rule's explicit exception |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| ---- | ------------------------ | -------------- | ------ |
| T1 | None | None | ✅ Match |
| T2 | T1 | T1→T2 | ✅ Match |
| T3 | None | None | ✅ Match |
| T4 | T3 | T3→T4 | ✅ Match |

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| ---- | ----------------------------- | ----------------- | ----------- | ------ |
| T1: Issue session cookie | `AdminsHandler.AcceptInvite` | integration | integration | ✅ OK |
| T2: Wire routes.go | `routes.go` wiring | none | none | ✅ OK |
| T3: MSW mock handler | test infrastructure | none | none | ✅ OK |
| T4: AcceptInvitePage | Frontend page + route | unit | unit | ✅ OK |

---

## Requirement Traceability (updated)

| Requirement ID | Story | Tasks | Status |
| --------------- | ------ | ----- | ------ |
| AIP-01 | Invited admin accepts their invite and lands logged in | T1, T2, T3, T4 | In Tasks |
| AIP-02 | Invited admin accepts their invite and lands logged in | T1, T4 | In Tasks |
| AIP-03 | Invited admin accepts their invite and lands logged in | T4 | In Tasks |
| AIP-04 | Invited admin accepts their invite and lands logged in | T4 | In Tasks |
| AIP-05 | Password confirmation prevents typo lockout | T3, T4 | In Tasks |
| AIP-06 | Password confirmation prevents typo lockout | T4 | In Tasks |
| AIP-07 | Clear, distinct error messages for every accept failure | T3, T4 | In Tasks |
| AIP-08 | Clear, distinct error messages for every accept failure | T3, T4 | In Tasks |
| AIP-09 | Clear, distinct error messages for every accept failure | T3, T4 | In Tasks |

**Coverage:** 9 total, 9 mapped to tasks, 0 unmapped
