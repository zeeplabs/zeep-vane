# user-sessions Validation

**Date**: 2026-09-11
**Spec**: `.specs/features/user-sessions/spec.md`
**Diff range**: `5083956..a48312a` (branch `develop`; commits `324cc6d`, `dbeaedc`, `a48312a`)
**Verifier**: independent fresh-eyes pass (standalone fallback). Author ≠ verifier: the implementation commits were authored in prior sessions; this verification re-derived coverage from `spec.md`, the production code, and the test files. No general-purpose sub-agent type is available in this harness, so the `tlc-spec-driven` standalone fallback (`references/validate.md` §"Standalone fallback") was used. The two self-evaluations under `evaluations/` were deliberately **not** used as evidence (same-model-as-author bias).

---

## Task Completion

| Task | Status | Notes |
| ---- | ------ | ----- |
| T1 Migration `0031_sessions` | ✅ Done | `.up.sql`/`.down.sql` apply clean for reversal, FK CASCADE verified |
| T2 SessionRepository | ✅ Done | 6 methods + integration tests |
| T3 auth/session.go sid claim | ✅ Done | required claim; issuer guard; challenge token still rejected |
| T4 middleware.RequireAuth | ✅ Done | new signature; missing-row/revoked-row 401; throttle |
| T5 issueSessionForUser + captureSessionContext | ✅ Done | row creation centralized for Login/VerifyTwoFactor |
| T6 SwitchTenant reuses sid | ✅ Done | row-count + sid preservation asserted |
| T7 Logout revokes row | ✅ Done | revoked_at set; raw-token replay 401 |
| T8 UpdateRole/Delete per-session | ✅ Done | `RevokeAllForUser`, global timestamp path retired for admin events |
| T9 SessionsHandler + 2 endpoints | ✅ Done | List/Revoke; 200/404/409 matrix |
| T10 routes.go wiring | ✅ Done | both routes registered; RequireAuth signature threaded |
| T11 frontend types/MSW/mockData | ✅ Done | (contract drift finding F1) |
| T12 hook useSessions | ✅ Done | MSW-backed |
| T13 SessionsSection + i18n | ✅ Done | pt-BR + en keys present |
| T14 frontend integration tests | ✅ Done | component + hook tests |
| T15 Verifier | ✅ Done | this report |

---

## Spec-Anchored Acceptance Criteria

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| ------------------------- | -------------------- | ----------------------- | ------ |
| SESS-01 Login issues a token ⇒ sessions row with UA+IP, token carries `sid` | row exists, `user_agent`/`ip` = request values, `claims.SessionID == row.id` | `internal/api/auth_handler.go:389,396`; `internal/api/auth_handler_test.go:1645` — `storedUA == wantUA`, `storedIP == "203.0.113.7"`, `claims.SessionID != ""` | ✅ PASS |
| SESS-01 AcceptInvite ⇒ row | same | `internal/api/admins.go:385`; `internal/api/admins_test.go:1948` — UA/IP asserted on the persisted row | ✅ PASS |
| SESS-01 Bootstrap ⇒ row | same | `internal/api/bootstrap_handler.go:192,198`; `internal/api/bootstrap_handler_test.go:400` — UA/IP asserted on the persisted row | ✅ PASS |
| SESS-01 VerifyTwoFactor ⇒ row | same (shared issuer) | `internal/api/auth_handler.go:342` → `issueSessionForUser` (same `Create`/`sid` path); covered transitively by the Login row test | ✅ PASS |
| SESS-01 token without sid cannot be issued | issuer returns `ErrMissingSessionID` | `internal/auth/session.go:87`; `internal/auth/session_test.go:165` — `err == ErrMissingSessionID` | ✅ PASS |
| SESS-02 SwitchTenant reuses the row | same `sid`, row count unchanged | `internal/api/auth_handler.go:913`; `internal/api/auth_handler_test.go:1698` — `postClaims.SessionID == preClaims.SessionID` **and** `postCount == preCount` | ✅ PASS |
| SESS-03 RequireAuth rejects missing/revoked sid | `401` | `internal/api/middleware.go:175` (missing row), `:185` (revoked row), `internal/auth/session.go:171` (empty sid); `internal/api/middleware_test.go:329` / `:361` / `:294` — all assert `rec.Code == 401` | ✅ PASS |
| SESS-04 `last_seen_at` throttled to ≤1 write / 5 min | first request writes; second within window is a no-op | `internal/db/session_repository.go:186-198`, `internal/api/middleware.go:195`; `internal/api/middleware_test.go:399` — `afterSecond.Time.Equal(afterFirst.Time)`; repo-level `internal/db/session_repository_test.go:285` | ✅ PASS |
| SESS-05 `GET /api/auth/sessions` ⇒ 200 with all non-revoked, non-expired rows + fields | 200 + `id`,`user_agent`,`ip`,`created_at`,`last_seen_at`,`current` | `internal/api/sessions_handler.go:75-102`, `routes.go:171`; `internal/api/sessions_handler_test.go:110` (200, 3 rows, UA/IP values), `:186` (revoked excluded), `:227` (>24h excluded) | ✅ PASS |
| SESS-06 exactly one row marked current = request's `sid` | count == 1 and id == calling sid | `internal/api/sessions_handler.go:96`; `internal/api/sessions_handler_test.go:140-153` — `currentCount == 1`, `currentID == auth.IssueTestSessionID` | ✅ PASS |
| SESS-07 revoke another owned session ⇒ `revoked_at` set + 200 | 200; row revoked | `internal/api/sessions_handler.go:148,158,165`; `internal/api/sessions_handler_test.go:263` — 200 + `revokedAt != nil` | ✅ PASS |
| SESS-08 revoked session rejected on next request | 401 | `internal/api/sessions_handler_test.go:367` — replay of victim token ⇒ `401` | ✅ PASS |
| SESS-09 revoke own current session ⇒ 409, not revoked | 409; row untouched | `internal/api/sessions_handler.go:137-140`; `internal/api/sessions_handler_test.go:290` — 409 + `revokedAt == nil` | ✅ PASS |
| SESS-10 unknown / other-user / malformed id ⇒ 404 (anti-enumeration) | 404, byte-identical body | `internal/api/sessions_handler.go:148-151`; `internal/api/sessions_handler_test.go:318` (other user), `:351` (malformed); repo `internal/db/session_repository_test.go:199` (`ErrNotFound` both cases) | ✅ PASS |
| SESS-11 Logout sets `revoked_at` and clears cookie; replayed token 401 | revoked_at set; raw token 401 | `internal/api/auth_handler.go:844` + cookie clear `:847`; `internal/api/auth_handler_test.go:1769` (`revokedAt.Valid`), `:1815` (raw-token replay via Authorization ⇒ 401 + revoked_at set) | ✅ PASS |
| Context decision #1: admin events (UpdateRole/Delete) use per-session `RevokeAllForUser` | sessions row `revoked_at` set | `internal/api/admins.go:596`, `:649`; `internal/api/admins_test.go:1006` — `revokedAt != nil && revokedAt >= targetOldClaims.IssuedAt` | ✅ PASS |

**Status**: ✅ All 11 ACs (SESS-01..SESS-11) matched to the spec-defined outcome. 2 non-blocking precision findings (F1, F2) below.

---

## Discrimination Sensor

Scratch `git worktree` at `a48312a`; each mutation applied to the scratch copy, the covering test(s) run with `-tags=integration -p 1`, then reverted via `git checkout --`. Real worktree porcelain was empty before and after; scratch porcelain was clean at removal.

| # | Mutation | File:line (real) | Covering test | Killed? |
| - | -------- | ---------------- | ------------- | ------- |
| M1 | `ListForUser` drops `AND revoked_at IS NULL` | `internal/db/session_repository.go:108` | `TestSessionRepository_ListForUser_ExcludesRevokedAndExpired` | ✅ Killed |
| M2 | Middleware drops the `sess.RevokedAt.Valid` → 401 branch | `internal/api/middleware.go:185` | `TestRequireAuth_RevokedSessionRow_401` | ✅ Killed |
| M3 | `VerifySessionClaims` stops requiring `sid` | `internal/auth/session.go:171` | `TestVerifySessionClaims_MissingSID_ErrInvalidToken` | ✅ Killed |
| M4 | `Revoke` SQL drops `AND revoked_at IS NULL` (non-idempotent) | `internal/db/session_repository.go:167` | `TestSessionRepository_Revoke_Idempotent` | ✅ Killed |
| M5 | `SwitchTenant` creates a new row instead of reusing `sid` | `internal/api/auth_handler.go:913` | `TestSwitchTenant_PreservesSessionRowAndSID` | ✅ Killed |
| M6 | `Logout` stops calling `Revoke` | `internal/api/auth_handler.go:844` | `TestLogout_RevokesSessionRow`, `TestLogout_ReplayedRawToken_401RevokedRow` | ✅ Killed |
| M7 | `UpdateRole` reverts to global `users.RevokeSessions` | `internal/api/admins.go:596` | `TestUpdateAdminRole_ValidChange_200_AppliesRoleRevokesSessionsAndAudits` | ✅ Killed |
| M8 | `List` mis-marks `current` (`s.ID != sid`) | `internal/api/sessions_handler.go:96` | `TestSessionsHandler_List_ReturnsCurrentAndOthers` | ✅ Killed |

**Sensor depth**: P0/critical-path (auth/session) — 8 targeted behavior-level mutations (≥5, per the P0 tier).
**Result**: **8/8 killed** — PASS ✅. Note on M3: the auth unit test is the discriminating one; the `internal/api` counterpart (`TestRequireAuth_TokenWithoutSessionID_401`) does not independently discriminate this mutation because the middleware's subsequent `GetByID("")` lookup also rejects — the mutant is still killed by the suite as a whole.

---

## Code Quality

| Principle | Status |
| --------- | ------ |
| Minimum code | ✅ |
| Surgical changes | ✅ (test-only edits in unrelated `*_test.go` are the mechanical `RequireAuth` signature update) |
| No scope creep | ✅ |
| Matches patterns | ✅ (repo/handler/middleware shapes match existing conventions) |
| Spec-anchored outcome check (asserted values match spec) | ⚠️ 1 contract-drift finding (F1) |
| Per-layer Coverage Expectation met (domain 1:1 ACs; routes happy+edge+error) | ✅ |
| Every test maps to a spec requirement - no unclaimed tests | ✅ |
| Documented guidelines followed: `AGENTS.md` §4/§5, `.specs/features/user-sessions/context.md` | ✅ (one tag-convention gap, F2) |

---

## Edge Cases

- [x] Absent `User-Agent` ⇒ `NULL` (not empty): `internal/db/session_repository.go:60-67` (`NULLIF`); `internal/db/session_repository_test.go:99-113` asserts `UserAgent.Valid == false`.
- [x] Unresolvable client IP ⇒ `NULL`; sourced from `RemoteAddr` only (`captureSessionContext`, `internal/api/auth_handler.go:410-425`); same repo test.
- [x] Only the current session ⇒ single-item list, no error: handler returns whatever `ListForUser` yields and the caller's row is always included (`sessions_handler.go:87-97`). No dedicated "exactly one row" test — minor coverage gap (F4), behavior is correct by construction.
- [x] `created_at` older than 24h and unrevoked ⇒ excluded: `internal/api/sessions_handler_test.go:227`, `internal/db/session_repository_test.go:127`.

---

## Gate Check

Gate commands from `tasks.md` §"Gate Check Commands" (release gate).

| Gate | Command | Result |
| ---- | ------- | ------ |
| format | `gofmt -l <changed .go files>` | ✅ empty |
| build | `go build ./...` | ✅ exit 0 |
| vet | `go vet ./...` | ✅ exit 0 |
| unit | `go test -count=1 ./...` | ✅ all packages `ok`, exit 0 |
| integration (serialized) | `TEST_DATABASE_URL=<disposable> go test -tags=integration -count=1 -p 1 ./...` | ✅ all packages `ok`, exit 0 |
| integration (default parallel) | `… go test -tags=integration -count=1 ./...` | ⚠️ flaky — pre-existing shared-DB contention (F3); base `5083956` fails the same way |
| frontend typecheck | `cd web && npx tsc -b --noEmit` | ✅ exit 0 |
| frontend tests | `cd web && npm run test` | ✅ 63 files / 339 tests passed |

- **Test count before feature**: not independently captured at `5083956`.
- **Test count after feature**: backend +30 new test funcs across 3 new test files (`sessions_handler_test.go`, `session_repository_test.go`, `sessions_migration_test.go`) + additions to `middleware_test.go`/`auth_handler_test.go`/`admins_test.go`/`bootstrap_handler_test.go`/`session_test.go`; frontend +2 test files, suite 339 passing.
- **Skipped tests**: none.
- **DB rule**: all integration runs used a disposable `vane-test-pg` container on `localhost:5433` (`max_connections=300`); never a real/dev database. Container destroyed after the run.

---

## Fix Plans (non-blocking findings)

### F1: Frontend `SessionView` declares fields the backend never returns

- **Root cause**: `web/src/types/api.ts:141-156` adds `user_id`, `expires_at`, `revoked_at`; the real response shape is `internal/api/sessions_handler.go:45-52` (`id`, `user_agent?`, `ip?`, `created_at`, `last_seen_at?`, `current`). The MSW seed (`web/src/lib/mockData.ts:301-334`) also carries them, and the mock's filter relies on `user_id`/`revoked_at`. TypeScript cannot catch this because the frontend owns its type (exactly the failure mode `AGENTS.md` §5 warns about).
- **Impact**: none at runtime today (the component reads only `id`/`user_agent`/`ip`/`last_seen_at`/`current`); it is contract drift that will mislead the next consumer of `SessionView`.
- **Fix task**: align `SessionView` and the mock seed to the backend's exact fields (keep mock-only fields on a separate mock-internal type), or add the fields to the backend response if they are wanted.
- **Priority**: Minor.

### F2: `ip` design deviation (`INET` → `TEXT`) not tagged `SPEC_DEVIATION`

- **Root cause**: `design.md:68,85` specifies `ip INET`; migration `0031_sessions.up.sql:4` and `session_repository.go:26-31` use `TEXT`. The rationale is documented in prose but not via the project's `// SPEC_DEVIATION` tag (used elsewhere, e.g. `internal/connectors/datadog/client.go:29`).
- **Impact**: none on any AC (the spec requires a nullable `ip`, not a SQL type); process/convention gap only.
- **Fix task**: add the `// SPEC_DEVIATION` tag at the migration/repo, or update `design.md`.
- **Priority**: Minor.

### F3: Default-parallel full integration gate is flaky (pre-existing), amplified by the new `sessions` fixture dependency

- **Evidence**: on a fresh container, `go test -tags=integration ./...` failed with cross-package shared-DB races (`relation "sessions" does not exist`; protected-route 401s because the shared fixture session row was not yet seeded; FK violation seeding the fixture row; `already bootstrapped` 409). The **base commit `5083956` fails the same parallel gate** (different victims: `TestDomainRepository_ListPaginated_...`, `TestPruner_...`, plus worktree-only SPA 404s). Serialized `-p 1` is 100% green at feature HEAD.
- **Impact**: CI (`go test -tags=integration ./...`, `.github/workflows/ci.yml:75`) can go red for reasons unrelated to this feature. The feature adds the `sessions` table + `IssueTestSessionID` fixture row as a hard prerequisite for every protected-route test in `internal/api`, which widens the pre-existing race window.
- **Fix task**: make CI/the release gate run `-p 1` (or move shared-DB packages to per-package scratch databases, as `internal/db` already does).
- **Priority**: Minor (pre-existing infra; not a feature defect).

### F4: No explicit "only the current session" list test

- **Root cause**: edge case "user has no other sessions ⇒ single-item list". Behavior is correct (`ListForUser` returns the current row), but no test exercises exactly one row.
- **Priority**: Minor.

### F5: Frontend locale/copy nits

- `SessionsSection.tsx:26` hardcodes `toLocaleString("pt-BR")` instead of the active i18n language; the load-error state reuses `sessions.genericError` ("Não foi possível encerrar a sessão") for a *fetch* failure. Cosmetic.
- **Priority**: Cosmetic.

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| ----------- | --------------- | ---------- |
| SESS-01 | Pending | ✅ Verified |
| SESS-02 | Pending | ✅ Verified |
| SESS-03 | Pending | ✅ Verified |
| SESS-04 | Pending | ✅ Verified |
| SESS-05 | Pending | ✅ Verified |
| SESS-06 | Pending | ✅ Verified |
| SESS-07 | Pending | ✅ Verified |
| SESS-08 | Pending | ✅ Verified |
| SESS-09 | Pending | ✅ Verified |
| SESS-10 | Pending | ✅ Verified |
| SESS-11 | Pending | ✅ Verified |

---

## Summary

**Overall**: ✅ Ready (non-blocking findings only)

**Spec-anchored check**: 11/11 ACs matched spec outcome; 2 precision findings (F1 contract drift, F2 missing deviation tag)
**Sensor**: 8/8 mutations killed (P0 depth)
**Gate**: build/vet/gofmt/unit green; integration green serialized (`-p 1`); frontend tsc + 339 tests green

**What works**: Every session-issuing path (Login, VerifyTwoFactor, AcceptInvite, Bootstrap) creates a real `sessions` row carrying request UA/IP and a `sid` claim; `SwitchTenant` reuses the row; `RequireAuth` enforces per-row existence + `revoked_at` plus the throttled `last_seen_at`; List/Revoke implement the full 200/404/409 anti-enumeration matrix with exactly one `current`; Logout revokes the row so a copied raw token is dead; change-password/reset keep the global revoke while admin events moved to per-session revocation.

**Issues found**: F1 frontend `SessionView`/mock contract drift (unused today); F2 missing `SPEC_DEVIATION` tag for `INET`→`TEXT`; F3 pre-existing flaky parallel integration gate (base commit also fails) that the new shared fixture can amplify; F4 missing single-current-session list test; F5 locale/copy nits.

**Next steps**: feature is Done. Follow-up items F1–F5 are candidates for a small cleanup task or the backlog; none blocks the release.
