# Per-Device User Sessions Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/user-sessions/design.md`
**Spec**: `.specs/features/user-sessions/spec.md`
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase (`AGENTS.md` §3, sampled `internal/api/middleware_test.go`, `internal/auth/session_test.go`, `internal/db/domain_repository_test.go`) and spec ACs - confirm before Execute. Guidelines found: `AGENTS.md` (backend gates, integration-test DB rule, §7's higher-risk-auth confirmation rule - this feature changes `RequireAuth`'s hot path, the single most re-used piece of code in the project). Backend-only, no `web/` changes this cycle.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Schema / migration (config) | none | Build gate only - migration applies cleanly | `internal/db/migrations/0031_*.sql` | `go build ./...` + apply on disposable Postgres |
| `internal/auth` (claim shape, pure) | unit | All branches; round-trip of the new `sid` claim, empty-`sid` treated as absent | `internal/auth/session_test.go` | `go test ./internal/auth/...` |
| Repository (`SessionRepository`) | integration | Key query paths + error paths + the throttle guard on `TouchLastSeen` + the active/expired/revoked predicate shared by `GetActive`/`ListActiveForUser` | `internal/db/session_repository_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...` |
| Middleware (`RequireAuth` sid enforcement) | integration | Every rejection path (missing/revoked/expired row) + the throttled touch, 1:1 to SESS-03/04 | `internal/api/middleware_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...` |
| API handlers (`Login`, `SwitchTenant`, `AcceptInvite`, `BootstrapHandler.Create`, `Sessions`, `RevokeSession`, `Logout`) | integration | All routes in scope: happy + every listed edge case + error paths, 1:1 to spec ACs | `internal/api/auth_handler_test.go`, `internal/api/admins_test.go`, `internal/api/bootstrap_handler_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...` |

## Gate Check Commands

> Generated from `AGENTS.md` §3 - confirm before Execute.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Package-scoped (mid-refactor only, T3) | The one task in this feature that intentionally changes an exported `internal/auth` signature before its callers are updated (see T3's Done when) | `go build ./internal/auth/... && go vet ./internal/auth/... && gofmt -l internal/auth/session.go && go test ./internal/auth/...` |
| Quick (Go, no DB) | Never used stand-alone in this feature past T3 - every other task touches DB-backed behavior | `go build ./... && go vet ./... && gofmt -l <changed files> && go test ./...` |
| Full (Go, DB-touching) | After every task from T4 onward | Spin disposable Postgres per `AGENTS.md` §3, then `TEST_DATABASE_URL=... go test -tags=integration ./...`, then destroy the container |
| Build (phase completion) | End of every phase | Full repo build: `go build ./... && go vet ./... && gofmt -l <changed files>` + Full gate, both green - by the end of Phase 1 the repo is intentionally NOT green (T3 alone breaks `internal/api`/`internal/cli`); Phase 1's Build gate check therefore runs only after T4 (see Execution Plan note) |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Foundation

T3 intentionally leaves the repo non-compiling outside `internal/auth` until T4 (Phase 2) updates callers - documented explicitly so this isn't mistaken for a broken task. T1/T2/T3 have no dependency among themselves.

```
T1 → T2
T3
```

### Phase 2: Enforcement and Primary Wiring

```
T2 → T4
T3 → T4
T4 → T5
T4 → T6
```

### Phase 3: Self-Service Endpoints

```
T4 → T7
T4 → T8
T4 → T9
```

---

## Task Breakdown

### T1: Migration 0031 - sessions table

**What**: New migration creating `sessions` per `design.md`'s Data Models section, up + down.
**Where**: `internal/db/migrations/0031_sessions.up.sql`
**Depends on**: None
**Reuses**: `gen_random_uuid()` PK convention already used elsewhere in this schema.
**Requirement**: SESS-01

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `sessions(id PK, user_id FK ON DELETE CASCADE, user_agent NULL, ip NULL, created_at, last_seen_at, revoked_at NULL)` created, index on `user_id`
- [ ] Migration applies and reverses cleanly on a disposable Postgres
- [ ] Gate check passes: `go build ./...`

**Tests**: none
**Gate**: build

**Commit**: `feat(db): add sessions table for per-device session tracking`

---

### T2: `SessionRepository`

**What**: New repository: `Create`, `GetActive`, `TouchLastSeen`, `ListActiveForUser`, `Revoke`, per `design.md`.
**Where**: `internal/db/session_repository.go`
**Depends on**: T1
**Reuses**: Standard repository shape (`DomainRepository`, `PollerLeadershipRepository`).
**Requirement**: SESS-01, SESS-04, SESS-05, SESS-06, SESS-07, SESS-08, SESS-09, SESS-10

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `Create` inserts a row and returns it with a generated ID
- [ ] `GetActive` returns `db.ErrNotFound` for a missing, revoked, or TTL-expired row (cutoff computed in Go from `auth.SessionTTL`, passed as a bound parameter - not a SQL interval literal)
- [ ] `TouchLastSeen`'s `WHERE last_seen_at < now() - interval '5 minutes'` guard is proven: a second call within 5 minutes does not change `last_seen_at`, a call after does
- [ ] `ListActiveForUser` excludes revoked and TTL-expired rows, orders `created_at DESC`
- [ ] `Revoke` sets `revoked_at` only for the matching `(id, user_id)` pair, reports whether a row was found for that user regardless of prior revocation state
- [ ] Gate check passes on disposable Postgres: `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...`
- [ ] Test count: ≥12 new subtests

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add SessionRepository`

---

### T3: `internal/auth` - add `sid` claim

**What**: `sessionClaims` gains `SessionID string \`json:"sid"\``. `IssueSessionWithTenant(userID, tenantID, sessionID, secret string) (string, error)` - new required parameter. `SessionClaims`/`VerifySessionClaims` expose the parsed `SessionID`. This intentionally breaks compilation of every caller outside `internal/auth` (`internal/api`, `internal/cli`) until T4/T5/T6 update them - see this feature's Gate Check Commands for why this task's own gate is package-scoped, not repo-wide.
**Where**: `internal/auth/session.go`
**Depends on**: None
**Reuses**: Existing `sessionClaims`/`jwt.RegisteredClaims` structure - purely additive field plus one new required parameter.
**Requirement**: SESS-01, SESS-02

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] A token issued via `IssueSessionWithTenant(userID, tenantID, sessionID, secret)` round-trips `sessionID` through `VerifySessionClaims`
- [ ] A token with no `sid` claim (simulating a pre-feature token) parses with `SessionID == ""`, not an error
- [ ] Gate check passes (package-scoped): `go build ./internal/auth/... && go vet ./internal/auth/... && gofmt -l internal/auth/session.go && go test ./internal/auth/...`
- [ ] Test count: +3 new tests in `internal/auth/session_test.go`, all prior tests in that file updated to the new signature and passing

**Tests**: unit
**Gate**: quick

**Commit**: `feat(auth): add sid claim to session tokens`

---

### T4: `RequireAuth` sid enforcement + `Login`/`SwitchTenant` wiring

**What**: `RequireAuth` gains a `sessions sessionValidator` dependency; after the existing `SessionsRevokedAt` check, it resolves `claims.SessionID` via `SessionRepository.GetActive`, rejects `401` on failure, and calls `TouchLastSeen` (best-effort) on success, storing the session ID in context (`SessionIDFromContext`). `AuthHandler.Login` creates a session row before issuing its token; `SwitchTenant` reuses the current request's session ID instead of creating a new row. This is the task that restores repo-wide compilation after T3.
**Where**: `internal/api/middleware.go`
**Depends on**: T2, T3
**Reuses**: T2's `SessionRepository`; T3's `sid` claim; `internal/api/auth_handler.go`'s existing `Login`/`SwitchTenant` bodies (extended, not rewritten).
**Requirement**: SESS-01, SESS-02, SESS-03, SESS-04

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `Login` creates a `sessions` row with `user_agent`/`ip` from the request and issues a token carrying its ID
- [ ] `SwitchTenant` reuses the existing session row (row count for that user unchanged, only `tid` claim changes)
- [ ] A protected request with a `sid` matching no active row is rejected `401`
- [ ] `last_seen_at` updates on use, throttled per T2's guard (proven via a test asserting no second write within 5 minutes)
- [ ] Every pre-existing `RequireAuth`/`Login`/`SwitchTenant` test passes with the new dependency wired in
- [ ] Gate check passes on disposable Postgres: `TEST_DATABASE_URL=... go test -tags=integration ./...`
- [ ] Test count: ≥10 new tests, 0 regressions in existing suites

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): enforce per-session revocation in RequireAuth, wire Login/SwitchTenant`

---

### T5: `AdminsHandler.AcceptInvite` session wiring

**What**: `AcceptInvite` (`internal/api/admins.go:376`) creates a session row before issuing its token, same shape as T4's `Login` change.
**Where**: `internal/api/admins.go`
**Depends on**: T4
**Reuses**: `SessionRepository.Create` (T2), same `userAgentPtr`/`remoteIPPtr` extraction helpers introduced in T4.
**Requirement**: SESS-01

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Accepting an invite creates a `sessions` row and the issued token's `sid` matches it
- [ ] Existing `AcceptInvite` tests pass unmodified except for the new session-row assertion
- [ ] Gate check passes on disposable Postgres: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [ ] Test count: +2 new tests

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): create a session row on invite acceptance`

---

### T6: `BootstrapHandler.Create` session wiring

**What**: `Create` (`internal/api/bootstrap_handler.go:184`) creates a session row before issuing its token, same shape as T5.
**Where**: `internal/api/bootstrap_handler.go`
**Depends on**: T4
**Reuses**: Same helpers as T5.
**Requirement**: SESS-01

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Bootstrapping the first admin creates a `sessions` row and the issued token's `sid` matches it
- [ ] Existing bootstrap tests pass unmodified except for the new session-row assertion
- [ ] Gate check passes on disposable Postgres: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [ ] Test count: +2 new tests

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): create a session row on bootstrap`

---

### T7: `GET /api/auth/sessions`

**What**: New `AuthHandler.Sessions` endpoint listing the caller's active sessions, marking exactly one as current.
**Where**: `internal/api/auth_handler.go`
**Depends on**: T4
**Reuses**: `SessionRepository.ListActiveForUser` (T2), `SessionIDFromContext` (T4).
**Requirement**: SESS-05, SESS-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Two logins from the same user return two rows, exactly the calling token's session marked current
- [ ] A user with only their current session returns a single-item list, not an error
- [ ] Route registered in `internal/cli/routes.go` behind `RequireAuth` only (`anyRole`, self)
- [ ] Gate check passes on disposable Postgres: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [ ] Test count: ≥4 new tests

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): add GET /api/auth/sessions`

---

### T8: `DELETE /api/auth/sessions/{id}`

**What**: New `AuthHandler.RevokeSession` endpoint. `409` if `{id}` is the caller's own current session (checked from context, no query needed); `404` if `{id}` doesn't exist or belongs to another user; otherwise revokes it.
**Where**: `internal/api/auth_handler.go`
**Depends on**: T4
**Reuses**: `SessionRepository.Revoke` (T2), `SessionIDFromContext` (T4), the anti-enumeration 404 convention already used by `domains_handler.go`'s `Delete`.
**Requirement**: SESS-07, SESS-08, SESS-09, SESS-10

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Revoking another of the caller's own sessions returns `200`; that session's token is rejected `401` on next use; the caller's own current token still works
- [ ] Targeting the caller's own current session returns `409`, no mutation
- [ ] Targeting a nonexistent or another user's session returns `404`
- [ ] Route registered in `internal/cli/routes.go`
- [ ] Gate check passes on disposable Postgres: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [ ] Test count: ≥5 new tests

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): add DELETE /api/auth/sessions/{id}`

---

### T9: `Logout` revokes the current session server-side

**What**: `AuthHandler.Logout` sets `revoked_at` on the session matching the request token's `sid`, in addition to clearing the cookie.
**Where**: `internal/api/auth_handler.go`
**Depends on**: T4
**Reuses**: `SessionRepository.Revoke` (T2), `SessionIDFromContext` (T4).
**Requirement**: SESS-11

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] After `Logout`, replaying the copied (pre-logout) raw token against any protected route returns `401`
- [ ] The cookie-clearing behavior is unchanged
- [ ] A `Revoke` failure during logout is logged, not fatal - the cookie still clears
- [ ] Gate check passes on disposable Postgres: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [ ] Test count: +2 new tests

**Tests**: integration
**Gate**: full

**Commit**: `fix(api): revoke the session row on logout, not just the cookie`

---

## Phase Execution Map

```
Phase 1 → Phase 2 → Phase 3

Phase 1:  T1 ------→ T2
          T3 (independent, breaks build until Phase 2)
Phase 2:  T4 ------→ T5
Phase 2:  T4 ------→ T6
Phase 3:  T4 ------→ T7
Phase 3:  T4 ------→ T8
Phase 3:  T4 ------→ T9
```

Execution is strictly sequential within a phase - no intra-phase parallelism.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: Migration 0031 | 1 schema change | ✅ Granular |
| T2: SessionRepository | 1 repository | ✅ Granular |
| T3: auth sid claim | 1 file, 1 concern (claim shape) | ✅ Granular |
| T4: RequireAuth + Login/SwitchTenant wiring | 1 tight dependency chain (enforcement point + its two most direct callers) - deliberately not split further per this feature's own flagged Risk (call sites must land together to keep the repo compiling) | ⚠️ OK - documented exception, not further splittable without leaving the repo non-compiling mid-phase |
| T5: AcceptInvite wiring | 1 call site | ✅ Granular |
| T6: BootstrapHandler.Create wiring | 1 call site | ✅ Granular |
| T7: GET /api/auth/sessions | 1 endpoint | ✅ Granular |
| T8: DELETE /api/auth/sessions/{id} | 1 endpoint | ✅ Granular |
| T9: Logout revokes session | 1 method, 1 concern | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | — | ✅ Match |
| T2 | T1 | T1 → T2 | ✅ Match |
| T3 | None | — | ✅ Match |
| T4 | T2, T3 | T2 → T4, T3 → T4 | ✅ Match |
| T5 | T4 | T4 → T5 | ✅ Match |
| T6 | T4 | T4 → T6 | ✅ Match |
| T7 | T4 | T4 → T7 | ✅ Match |
| T8 | T4 | T4 → T8 | ✅ Match |
| T9 | T4 | T4 → T9 | ✅ Match |

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1 | Schema / migration | none | none | ✅ OK |
| T2 | Repository | integration | integration | ✅ OK |
| T3 | `internal/auth` | unit | unit | ✅ OK |
| T4 | Middleware + API handler | integration | integration | ✅ OK |
| T5 | API handler | integration | integration | ✅ OK |
| T6 | API handler | integration | integration | ✅ OK |
| T7 | API handler | integration | integration | ✅ OK |
| T8 | API handler | integration | integration | ✅ OK |
| T9 | API handler | integration | integration | ✅ OK |
