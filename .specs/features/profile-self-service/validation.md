# Validation Report — profile-self-service

**Result**: PASS

Independent Verifier run (author != verifier). Diff range: `f351e62..138e892` (commits `1973f6d`, `9f7c5c2`, `138e892`).

## Per-AC Evidence

| AC | Assertion | Test (file:line) | Spec outcome | Covered? |
| --- | --- | --- | --- | --- |
| PROFSS-01 | `PATCH /api/auth/me` `{name}` → 200, name persisted | `internal/api/auth_handler_test.go:740` `TestUpdateProfile_ValidName_200UpdatesName` | 200 + `Name` updated & persisted | Yes |
| PROFSS-02 | empty `name` → 422, no change | `internal/api/auth_handler_test.go:781` `TestUpdateProfile_EmptyName_422NoChange` | 422, name unchanged | Yes |
| PROFSS-03 | extra fields (`email`, `role`) ignored, not applied | `internal/api/auth_handler_test.go:810` `TestUpdateProfile_ExtraFieldsIgnored` | 200, only `name` applied, `email`/`role` untouched | Yes (persisted state) — **gap**: only asserts the persisted `Email` column is unchanged, never asserts the JSON *response* echoes the unmutated email. See Sensor result below; this is a spec-precision gap, not a live defect (no code path currently persists a client-supplied email — `userGetter` has no `UpdateEmail`). |
| PROFSS-04 | correct current password + valid new password → 200, hash updated, other sessions revoked | `internal/api/auth_handler_test.go:889` `TestChangePassword_CorrectCurrentAndValidNew_200RevokesOtherSessions` | 200, `PasswordHash` verifies against new password, session B (issued earlier) now 401 | Yes |
| PROFSS-05 | wrong `current_password` → 401, no change, no revoke | `internal/api/auth_handler_test.go:912` `TestChangePassword_WrongCurrentPassword_401NoChange` | 401, hash unchanged, calling session still valid (nothing revoked) | Yes |
| PROFSS-06 | weak `new_password` → 422, no change | `internal/api/auth_handler_test.go:945` `TestChangePassword_WeakNewPassword_422NoChange` | 422, hash unchanged | Yes |

Unauthenticated-access checks (spec's edge case, not separately numbered):
- `TestUpdateProfile_NoSession_401` (`auth_handler_test.go:772`) and `TestChangePassword_NoSession_401` (`auth_handler_test.go:963`) both exercise a request with no `Authorization` header through the real `RequireAuth` middleware in `newMeRouter` — genuine 401 path, not a stubbed check.

## Targeted scrutiny (auth-sensitive)

- (a) **Cross-user impossible by construction**: `ChangePassword` compares `req.CurrentPassword` against `user.PasswordHash`, where `user` comes from `UserFromContext`. `RequireAuth` (`internal/api/middleware.go:87-134`) loads that user itself via `users.GetByID(r.Context(), claims.AdminID)` — `claims.AdminID` comes from the verified, signed session token, never from the request body/params. There is no field in `changePasswordRequest` that can select a different user ID. Confirmed by reading `RequireAuth` directly, not inferred.
- (b) **`updateProfileRequest` struct is name-only**: `type updateProfileRequest struct { Name string \`json:"name"\` }` (`auth_handler.go`) — no `role`/`email`/`tenant_id` field exists, so `json.Decode` structurally cannot populate them; `UpdateName` (`internal/db/user_repository.go`) issues `UPDATE users SET name = $1 WHERE id = $2` only. No other column is touched.
- (c) `RevokeSessions` is called only after `UpdatePasswordHash` succeeds (`auth_handler.go:349`), and the wrong-password/weak-password paths `return` before reaching it. Confirmed in code and by the discrimination sensor (mutant #1 below).
- (d) `internal/cli/routes.go:140` applies `protected.Use(requireAuth)` to the whole protected group, and line 159 additionally chains `protected.With(credentialLimiter.Middleware).Post("/api/auth/change-password", ...)` — both middlewares are actually present on the route registration, not just described in a comment.
- (e) Confirmed via `TestUpdateProfile_NoSession_401` / `TestChangePassword_NoSession_401`, both routed through the real `RequireAuth` middleware (not a hand-rolled 401 stub).

## Discrimination Sensor (mutation testing)

Performed in an isolated `git worktree` at commit `138e892` (never touched the main working tree), destroyed afterward.

| # | Mutant | Result |
| --- | --- | --- |
| 1 | `ChangePassword`: removed the `RevokeSessions` call on success | **Caught** — `TestChangePassword_CorrectCurrentAndValidNew_200RevokesOtherSessions` failed (session B still got 200 instead of 401). |
| 2 | `updateProfileRequest`: added an `Email` field and set it on the in-memory `user` object used for the JSON response (not persisted) | **Survived** — all `TestUpdateProfile_*` tests still passed, because `TestUpdateProfile_ExtraFieldsIgnored` only checks the persisted `Email` column via `repo.GetByID`, never the JSON response body's `email` field. Real-world impact is limited (no code path exists to persist a client-supplied email — `userGetter` never exposes an `UpdateEmail`), but it is a genuine test-coverage gap against AC3's literal "ignored, not applied" wording if the response is considered part of "applied". |
| 3 | `ChangePassword`: replaced the `VerifyPassword` check with a constant `true` (current-password check always passes) | **Caught** — `TestChangePassword_WrongCurrentPassword_401NoChange` failed (200 instead of 401). |

**Sensor score: 2/3 caught.**

## Build / static checks

- `go build ./...` — clean.
- `go vet ./...` — clean.
- `gofmt -l` on all changed files (`internal/db/user_repository.go`, `internal/db/user_repository_test.go`, `internal/api/auth_handler.go`, `internal/api/auth_handler_test.go`, `internal/cli/routes.go`) — no output (already formatted).

## Integration tests

Disposable Postgres (`postgres:16-alpine`, port 5437, `max_connections=300`), destroyed after the run — never touched `vane-dev-pg`.

```
TEST_DATABASE_URL="postgres://vane:vane@localhost:5437/vane?sslmode=disable" \
  go test -tags=integration -p 1 ./internal/db/... ./internal/api/... ./internal/cli/...
```

Result: `ok internal/db`, `ok internal/api`, `ok internal/cli`.

## Verdict

PASS. No security or correctness defect found — cross-user password/name mutation is structurally impossible (verified by reading `RequireAuth`, not just tests), password-change gating and session revocation behave per spec, and the route is correctly wired behind both `requireAuth` and `credentialLimiter`. One test-precision gap identified (mutant #2, PROFSS-03): the "extra fields ignored" test should additionally assert the JSON response body does not echo an attacker-supplied `email`/`role`, not only that the DB column is unchanged. Recommend adding that assertion in a follow-up, non-blocking commit.
