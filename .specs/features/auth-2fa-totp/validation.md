# Two-Factor Authentication (TOTP) Validation

**Date**: 2026-09-10
**Spec**: `.specs/features/auth-2fa-totp/spec.md`
**Diff range**: `de27c47..3973d83` (11 feature commits: `fed6e40`..`3973d83`)
**Verifier**: independent sub-agent (author ≠ verifier)

---

## Task Completion

| Task | Status  | Notes |
| ---- | ------- | ----- |
| T1   | ✅ Done | Migration `0030_two_factor_auth` — 3 tables, applies/reverses cleanly |
| T2   | ✅ Done | `internal/auth/two_factor.go` |
| T3   | ✅ Done | `VerifySessionClaims` Audience hardening |
| T4   | ✅ Done | `TwoFactorRepository` |
| T5   | ✅ Done | `TwoFactorChallengeRepository` |
| T6   | ✅ Done | `POST /api/auth/2fa/enroll` |
| T7   | ✅ Done | `POST /api/auth/2fa/confirm` |
| T8   | ✅ Done | `Login` session-issuance extracted to `issueSessionForUser` |
| T9   | ✅ Done | `Login` 2FA branch + `POST /api/auth/login/verify-2fa` (TOTP path) |
| T10  | ✅ Done | Recovery-code path in `verify-2fa` |
| T11  | ✅ Done | `POST /api/auth/2fa/disable` |

All 58 "Done when" checkboxes in `tasks.md` marked `[x]`, 0 pending.

---

## Spec-Anchored Acceptance Criteria

### P1: User enrolls in TOTP 2FA

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1 (TOTP-01): enroll generates secret, stores encrypted with `enabled_at` NULL, `200` with `otpauth://` URI + raw secret | `200`, non-empty secret, URI starts `otpauth://totp/` | `internal/api/two_factor_handler_test.go:94-107` — `rec.Code != http.StatusOK`, `body.Secret == ""`, `strings.HasPrefix(uri, "otpauth://totp/")` | ✅ PASS |
| AC2 (TOTP-02): `confirm` with valid code → `200`, `enabled_at` set, 10 recovery codes returned once | `200`, exactly 10 distinct non-empty plaintext codes | `internal/api/two_factor_handler_test.go:188-208` — `len(body.RecoveryCodes) != recoveryCodeCount` (10), per-code non-empty + distinctness loop | ✅ PASS |
| AC3 (TOTP-03): wrong code → `422`, no `enabled_at`, no recovery codes | `422`, `EnabledAt == nil`, no recovery-code match | `internal/api/two_factor_handler_test.go:225-244` — `confirmRec.Code != http.StatusUnprocessableEntity`, `got.EnabledAt != nil`, `ConsumeRecoveryCode(...) == true` both asserted false | ✅ PASS |
| AC4 (TOTP-04): already-enabled `enroll` → `409`, no mutation | `409`, secret/`enabled_at` unchanged | `internal/api/two_factor_handler_test.go:152-166` — `rec.Code != http.StatusConflict`, `got.EncryptedSecret != "ciphertext"`, `got.EnabledAt == nil` | ✅ PASS |

### P1: Login requires the second factor when 2FA is enabled

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1 (TOTP-05): password success for 2FA-enabled user → no `vane_session`, `200` challenge token | `200`, no cookie, non-empty `challenge_token` | `internal/api/auth_handler_test.go:1070-1082` — `rec.Code != http.StatusOK`, `vaneSessionCookie(rec) != nil`, `body.ChallengeToken == ""` | ✅ PASS |
| AC2 (TOTP-06): `verify-2fa` valid token + correct code → same tenant-resolution/session-issuance as `Login` | `200`, cookie set, `loginResponse` with matching `tenant_id` | `internal/api/auth_handler_test.go:1128-1143` — `rec.Code != http.StatusOK`, `vaneSessionCookie(rec) == nil`, `body.Token == ""`, `body.TenantID != tenantID` | ✅ PASS |
| AC3 (TOTP-07): expired/malformed/consumed token → `401`, no session | `401`, no cookie | `internal/api/auth_handler_test.go:1153-1158` (malformed), `:1189-1194` (expired row), `:1225-1230` (already consumed) — all assert exact `http.StatusUnauthorized` + nil cookie | ✅ PASS |
| AC4 (TOTP-08): wrong code/recovery_code → `401`, session/token not consumed | `401`, retry with correct code still succeeds | `internal/api/auth_handler_test.go:1253-1267` — wrong code asserted `StatusUnauthorized`, then correct-code retry on same token asserted `StatusOK` | ✅ PASS |
| AC5 (TOTP-09): no 2FA → `Login` unaffected | Full session, cookie set, unchanged from pre-2FA behavior | `internal/api/auth_handler_test.go:1097-1102` (post-2FA-branch handler) + all pre-existing `TestLogin_*` (lines 156-530) pass unmodified per T8 | ✅ PASS |

### P2: User completes login with a recovery code

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1 (TOTP-10): valid unused `recovery_code` → mark used, issue session, `200` | `200`, cookie set, code no longer matches after use | `internal/api/auth_handler_test.go:1367-1390` — `rec.Code != http.StatusOK`, cookie asserted non-nil, `ConsumeRecoveryCode(...) == true` after use asserted false | ✅ PASS |
| AC2 (TOTP-11): already-used code → `401`, no session | `401`, no cookie | `internal/api/auth_handler_test.go:1424-1429` | ✅ PASS |
| AC3 (TOTP-11): used code permanently rejected, even in a later unrelated attempt; other codes in the batch still work | Second attempt on a **fresh** challenge still `401`; a different, unused code still `200` | `internal/api/auth_handler_test.go:1395-1430` (fresh-challenge reuse rejected), `:1435-1468` (different code from same batch still `200`) | ✅ PASS |

### P1: User disables 2FA with password confirmation

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AC1 (TOTP-12): correct password → clear `enabled_at`, delete recovery codes, `200` | `200`, `GetSecret` → `db.ErrNotFound`, no recovery code matches | `internal/api/two_factor_handler_test.go:291-305` — `disableRec.Code != http.StatusOK`, `errors.Is(err, db.ErrNotFound)`, `ConsumeRecoveryCode(...) == true` asserted false | ✅ PASS |
| AC2 (TOTP-13): wrong password → `401`, 2FA remains enabled | `401`, `EnabledAt` still set | `internal/api/two_factor_handler_test.go:322-330` | ✅ PASS |
| AC3 (TOTP-13): no 2FA enabled → `200` idempotent no-op | `200` | `internal/api/two_factor_handler_test.go:341-343` | ✅ PASS |

**Status**: ✅ All 13 requirements (TOTP-01..13) covered with `file:line` evidence, asserted value matches the spec-defined exact outcome in every case. No spec-precision gaps.

### Edge Cases (spec.md)

| Edge case | `file:line` + assertion | Result |
| --- | --- | --- |
| Challenge token's user disabled 2FA since issuance → `401`, not silently issue a session | `internal/api/auth_handler_test.go:1299-1304` — `rec.Code != http.StatusUnauthorized`, cookie asserted nil | ✅ PASS |
| Re-enrolling before confirm discards the prior pending secret (no second valid pending secret) | `internal/api/two_factor_handler_test.go:131-133` — `body1.Secret == body2.Secret` asserted false | ✅ PASS |
| TOTP ±1 time-step skew tolerance (accept 1 step, reject 2 steps) | `internal/auth/two_factor_test.go:67-69` (accept) and `:84-86` (reject) | ✅ PASS |

---

## Discrimination Sensor

Sensor run in an isolated `git worktree` (`/tmp/verify-sensor`, `git worktree add ... HEAD`), never `git stash`. Pre-sensor `git status --porcelain` baseline captured and confirmed identical after every mutation was reverted and the scratch worktree removed.

| # | File:line | Description | Killed? |
| - | --------- | ------------ | ------- |
| 1 | `internal/auth/session.go:118` | `VerifySessionClaims`: `if len(claims.Audience) > 0` → `> 999` (never true — Audience-claim rejection defused) | ✅ Killed — `TestVerifySessionClaims_AudienceClaimSet_Rejected` fails |
| 2 | `internal/db/two_factor_challenge_repository.go:81` | `MarkUsed`: dropped `AND used_at IS NULL AND expires_at > now()` from the `UPDATE ... WHERE` clause (atomic one-time claim defused) | ✅ Killed — `TestTwoFactorChallengeRepository_MarkUsed_SecondCall_ReturnsFalseNoError` and `..._ExpiredRow_ReturnsFalseNoError` fail (repository layer). Handler-level `TestVerifyTwoFactor_AlreadyConsumedToken_401` still passed under this mutation because `VerifyTwoFactor`'s own `Lookup`-based `UsedAt` check is independent defense-in-depth — the mutation is still proven fatal at the layer that actually owns the atomic claim. |
| 3 | `internal/api/auth_handler.go:166` | `Login`: `if secret != nil && secret.EnabledAt != nil` → `if false && ...` (2FA gate bypassed) | ✅ Killed — `TestLogin_TwoFactorEnabled_200WithChallengeTokenNoCookie` fails |
| 4 | `internal/api/auth_handler.go:682` | `Confirm2FA`: `if !auth.ValidateTOTPCode(...)` → `if false && ...` (wrong-code rejection bypassed) | ✅ Killed — `TestConfirm2FA_WrongCode_422NoEnableNoRecoveryCodes` fails |
| 5 | `internal/db/two_factor_repository.go:172` | `ConsumeRecoveryCode`: `SET used_at = now()` → `SET used_at = used_at` (claim never actually persists) | ✅ Killed — `TestVerifyTwoFactor_UsedRecoveryCode_401Immediately` fails |

**Sensor depth**: expanded (5 mutations, P0/critical-path tier per `AGENTS.md` §7 — auth/session change) — covers all five highest-risk behaviors introduced by this feature (Audience-claim isolation, challenge one-time-use, the login 2FA gate itself, wrong-code rejection, recovery-code one-time-use).

**Result**: 5/5 killed — ✅ PASS

---

## Code Quality

| Principle | Status |
| --- | --- |
| No features beyond what was asked | ✅ |
| No abstractions for single-use code | ✅ |
| No unnecessary "flexibility" added | ✅ |
| Only touched files required for the tasks (19 files, all in scope per `tasks.md` `Where` fields — confirmed via `git diff --stat de27c47..3973d83`) | ✅ |
| Didn't "improve" unrelated code (`AGENTS.md`/`STATE.md` changes present in the working tree predate this feature and are untouched by these 11 commits) | ✅ |
| Matches existing patterns/style (repository-per-table, `writeAdminError`/generic anti-enumeration bodies, atomic-`RETURNING` claim pattern reused from `status-page-domain-attach`) | ✅ |
| Would senior engineer approve? | ✅ |
| Tests map to acceptance criteria and are non-shallow | ✅ — every status-code and payload-field assertion targets the spec-defined exact value, not just "no error" |
| Spec-anchored outcome check | ✅ — see AC tables above |
| Per-layer Coverage Expectation met (unit for `internal/auth`, integration for repositories and handlers, happy+edge+error per route) | ✅ |
| Every test in scope maps to a spec AC, edge case, or Done-when criterion | ✅ — no speculative tests found during review |
| Documented guidelines followed | `AGENTS.md` §3 (disposable-Postgres integration gate) and §7 (auth higher-risk confirmation, honored via expanded sensor tier) |

One minor deviation, not a defect: T9's implementation exports `auth.TwoFactorChallengeTTL` (was to be package-internal per a literal reading of design.md's constant naming) so the handler can size the `two_factor_challenges` row's TTL identically to the signed token's expiry — a single source of truth for the 5-minute window, which is the safer choice, not scope creep.

---

## Gate Check

- **Gate command** (Build level, per `tasks.md`): `go build ./... && go vet ./... && gofmt -l <files>`, then full-suite `TEST_DATABASE_URL=... go test -tags=integration ./...` on a disposable Postgres (`docker run ... postgres:16-alpine -c max_connections=300`, port 5434, destroyed after use — `vane-dev-pg` never touched).
- **Result**: `go build`/`go vet`/`gofmt` clean. Feature-scope integration tests (`./internal/auth/... ./internal/db/... ./internal/api/...`) — **612 passed, 0 failed**. Full-repo suite — 2 failures, both confirmed pre-existing and unrelated (see below).
- **Test count before feature**: baseline at `de27c47` (immediately pre-feature).
- **Test count after feature**: +19 (`internal/auth`), +19 (`internal/db`), +16 (`internal/api`, T6/T7 + T9/T10/T11) — no test deleted or weakened.
- **Skipped tests**: none.
- **Failures**: `TestEmailProviderRepository_SetActiveProvider_UpdatesSingletonRow` and `TestLLMProviderRepository_SetActiveProvider_ThenGetActiveProvider_RoundTrips` — reproduced independently by checking out `de27c47` (pre-feature) into an isolated `git worktree` and re-running both under `-count=3`: fail identically at baseline, confirming no relation to this feature's changes. Not investigated further as they are out of this feature's scope.

---

## Fix Plans

None. No gaps found.

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| --- | --- | --- |
| TOTP-01 | Pending | ✅ Verified |
| TOTP-02 | Pending | ✅ Verified |
| TOTP-03 | Pending | ✅ Verified |
| TOTP-04 | Pending | ✅ Verified |
| TOTP-05 | Pending | ✅ Verified |
| TOTP-06 | Pending | ✅ Verified |
| TOTP-07 | Pending | ✅ Verified |
| TOTP-08 | Pending | ✅ Verified |
| TOTP-09 | Pending | ✅ Verified |
| TOTP-10 | Pending | ✅ Verified |
| TOTP-11 | Pending | ✅ Verified |
| TOTP-12 | Pending | ✅ Verified |
| TOTP-13 | Pending | ✅ Verified |

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 13/13 ACs matched spec-defined outcome, 0 spec-precision gaps
**Sensor**: 5/5 mutations killed
**Gate**: 612 passed (feature scope), 0 failed; 2 unrelated pre-existing failures confirmed at baseline

**What works**: Full enroll → confirm → login-challenge → verify-2fa (TOTP and recovery-code paths) → disable lifecycle. Challenge-token isolation (`Audience` claim) and one-time-use (`MarkUsed` atomic claim) both proven by the sensor to be load-bearing, not decorative. `Login` for non-2FA users is byte-for-byte unchanged (T8 refactor verified via 0 net-new tests and all pre-existing `TestLogin_*` passing unmodified).

**Issues found**: None.

**Next steps**: Feature is done. No fix tasks to route.
