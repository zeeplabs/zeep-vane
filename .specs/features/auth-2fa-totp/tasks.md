# Two-Factor Authentication (TOTP) Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/auth-2fa-totp/design.md`
**Spec**: `.specs/features/auth-2fa-totp/spec.md`
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase (`AGENTS.md` §3, sampled `internal/api/auth_handler_test.go`, `internal/db/domain_repository_test.go`, `internal/auth/session_test.go`) and spec ACs - confirm before Execute. Guidelines found: `AGENTS.md` (backend gates, integration-test DB rule, higher-risk-auth confirmation rule in §7). No frontend layer - this cycle stays backend-only (matches `incident-severity-and-timeline`/`poller-status-real-state`/`profile-self-service`/`domain-verification-state`, none of which touched `web/`).

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Schema / migration (config) | none | Build gate only - migration applies cleanly | `internal/db/migrations/0030_*.sql` | `go build ./...` + apply on disposable Postgres |
| `internal/auth` (TOTP + challenge token crypto, pure) | unit | All branches; 1:1 to TOTP-01..13 where logic lives here (secret generation, code validation, challenge issue/verify, audience-claim rejection) | `internal/auth/two_factor_test.go`, `internal/auth/session_test.go` | `go test ./internal/auth/...` |
| Repository (`TwoFactorRepository`, `TwoFactorChallengeRepository`) | integration | Key query paths + error paths + the atomic-claim race behavior of `MarkUsed` | `internal/db/two_factor_repository_test.go`, `internal/db/two_factor_challenge_repository_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...` |
| API handlers (`Enroll`, `Confirm2FA`, `Disable2FA`, `Login`, `VerifyTwoFactor`) | integration | All routes in scope: happy + every listed edge case + error paths, 1:1 to TOTP-01..13 | `internal/api/auth_handler_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...` |

## Gate Check Commands

> Generated from `AGENTS.md` §3 - confirm before Execute.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick (Go, no DB) | After pure unit-test tasks (`internal/auth` changes only) | `go build ./... && go vet ./... && gofmt -l <changed files> && go test ./...` |
| Full (Go, DB-touching) | After tasks with integration tests (migration, repositories, handlers) | Spin disposable Postgres per `AGENTS.md` §3 (`docker run ... postgres:16-alpine -c max_connections=300`), then `TEST_DATABASE_URL=... go test -tags=integration ./...`, then destroy the container |
| Build (phase completion) | End of every phase | Quick + Full, both green |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Foundation

```
T1
T2 → T3
```

### Phase 2: Repositories

```
T1 → T4
T1 → T5
```

### Phase 3: Enrollment Flow

```
T2 → T6
T4 → T6
T6 → T7
```

### Phase 4: Login Second-Factor Gate

```
T7 → T8
T2 → T9
T5 → T9
T8 → T9
T9 → T10
```

### Phase 5: Disable

```
T7 → T11
```

---

## Task Breakdown

### T1: Migration 0030 - two_factor_secrets, two_factor_recovery_codes, two_factor_challenges

**What**: New migration creating the three tables from `design.md`'s Data Models section, up + down.
**Where**: `internal/db/migrations/0030_two_factor_auth.up.sql`, `.down.sql`
**Depends on**: None
**Reuses**: `crypto.Encrypt`-at-rest pattern already used by `email_providers` (no schema change needed there, just the same encryption convention for `encrypted_secret`).
**Requirement**: TOTP-01

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `two_factor_secrets(user_id PK, encrypted_secret, enabled_at NULL, created_at)` created
- [x] `two_factor_recovery_codes(id PK, user_id FK, code_hash, used_at NULL, created_at)` + index on `user_id` created
- [x] `two_factor_challenges(id PK, user_id FK, expires_at, used_at NULL, created_at)` created
- [x] Migration applies cleanly and reverses cleanly on a disposable Postgres
- [x] Gate check passes: `go build ./...`

**Tests**: none
**Gate**: build

**Commit**: `feat(auth): add 2FA schema (secrets, recovery codes, challenges)`

---

### T2: `internal/auth` TOTP + challenge token functions

**What**: New file `internal/auth/two_factor.go` implementing `GenerateTOTPSecret`, `ValidateTOTPCode`, `IssueTwoFactorChallenge`, `VerifyTwoFactorChallenge`, per `design.md`'s Components section. Adds `github.com/pquerna/otp` to `go.mod` (pinned via `go get`).
**Where**: `internal/auth/two_factor.go`
**Depends on**: None
**Reuses**: `jwt.SigningMethodHS256` + the same secret-signing shape as `IssueSessionWithTenant`.
**Requirement**: TOTP-01, TOTP-05

**Tools**:
- MCP: `context7` (verify `pquerna/otp`'s current API before writing against it, per the Knowledge Verification Chain)
- Skill: NONE

**Done when**:
- [x] `GenerateTOTPSecret` returns a valid `otpauth://` URI and raw secret
- [x] `ValidateTOTPCode` accepts a code computed from the same secret and rejects a wrong one, with the library's default ±1 skew
- [x] `IssueTwoFactorChallenge`/`VerifyTwoFactorChallenge` round-trip correctly; `VerifyTwoFactorChallenge` rejects an expired token
- [x] Gate check passes: `go build ./... && go vet ./... && gofmt -l internal/auth/two_factor.go && go test ./internal/auth/...`
- [x] Test count: ≥8 new tests pass (generate, validate correct/wrong/skew, issue/verify round-trip, expired, tampered)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(auth): add TOTP secret/code and 2FA challenge token functions`

---

### T3: Harden `VerifySessionClaims` to reject a non-empty `Audience` claim

**What**: `VerifySessionClaims` returns `ErrInvalidToken` if the parsed token's `Audience` claim is non-empty, so a 2FA challenge token (which always sets `Audience`) can never pass as a session token.
**Where**: `internal/auth/session.go` (edit)
**Depends on**: T2
**Reuses**: Existing `sessionClaims`/`jwt.RegisteredClaims.Audience` field (no new struct field).
**Requirement**: TOTP-05

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] A token signed with `Audience: []string{"2fa_challenge"}` fails `VerifySessionClaims` with `ErrInvalidToken`
- [x] Every existing `VerifySessionClaims`/`RequireAuth` test still passes unmodified (no `Audience` is ever set by `IssueSessionWithTenant` today)
- [x] Gate check passes: `go build ./... && go vet ./... && gofmt -l internal/auth/session.go && go test ./internal/auth/... ./internal/api/...`
- [x] Test count: +1 new test (`TestVerifySessionClaims_AudienceClaimSet_Rejected`), all prior tests unchanged and passing

**Tests**: unit
**Gate**: quick

**Commit**: `fix(auth): reject a session token carrying an audience claim`

---

### T4: `TwoFactorRepository`

**What**: New repository per `design.md`: `CreatePendingSecret`, `GetSecret`, `ConfirmSecret`, `DeleteSecret`, `CreateRecoveryCodes`, `ConsumeRecoveryCode`, `DeleteRecoveryCodes`.
**Where**: `internal/db/two_factor_repository.go`
**Depends on**: T1
**Reuses**: Standard repository shape (`PollerLeadershipRepository`, `DomainRepository`).
**Requirement**: TOTP-01, TOTP-02, TOTP-03, TOTP-04, TOTP-10, TOTP-11, TOTP-12, TOTP-13

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `CreatePendingSecret` upserts with `enabled_at NULL`
- [x] `GetSecret` returns `db.ErrNotFound` for no row
- [x] `ConfirmSecret` only sets `enabled_at` when it was NULL
- [x] `CreateRecoveryCodes` replaces any prior batch (delete-then-insert, atomic within one transaction)
- [x] `ConsumeRecoveryCode` matches a hash via bcrypt, marks it used atomically (`UPDATE ... WHERE used_at IS NULL RETURNING`), and rejects a second consumption of the same code
- [x] Gate check passes on disposable Postgres: `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...`
- [x] Test count: ≥10 new subtests (create/get/confirm happy+error paths, recovery code create/consume/reuse-rejected)

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add TwoFactorRepository for TOTP secrets and recovery codes`

---

### T5: `TwoFactorChallengeRepository`

**What**: New repository: `Create`, `Lookup`, `MarkUsed` (atomic claim), per `design.md`.
**Where**: `internal/db/two_factor_challenge_repository.go`
**Depends on**: T1
**Reuses**: Same repository shape as T4; atomic-`RETURNING` claim pattern from `status-page-domain-attach`'s `AttachDomain`.
**Requirement**: TOTP-06, TOTP-07, TOTP-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `Create` inserts a row with the given TTL and returns its ID
- [x] `Lookup` returns the row without mutating it (a wrong-code retry must not consume it)
- [x] `MarkUsed` succeeds exactly once per row; a second call returns `ok=false`, no error
- [x] A concurrency test proves two simultaneous `MarkUsed` calls on the same row never both succeed (real contention, not a same-goroutine sequential call - same discipline as `status-page-domain-attach`'s corrected concurrency test)
- [x] Gate check passes on disposable Postgres: `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...`
- [x] Test count: ≥6 new subtests

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add TwoFactorChallengeRepository with atomic one-time claim`

---

### T6: `Enroll` endpoint

**What**: `POST /api/auth/2fa/enroll` - generates a secret via T2, stores it via T4, returns the `otpauth://` URI and raw secret; `409` if already enabled.
**Where**: `internal/api/auth_handler.go` (edit: new `Enroll` method, `twoFactor` field/interface)
**Depends on**: T2, T4
**Reuses**: `writeAdminError`/`writeUnauthorized` helpers already in `auth_handler.go`.
**Requirement**: TOTP-01, TOTP-04

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `200` with `otpauth://` URI + raw secret on first enroll
- [x] `409` when `enabled_at` is already set, no row mutated
- [x] Route registered in `internal/cli/routes.go`, wired behind `RequireAuth` only (`anyRole`, self)
- [x] Gate check passes on disposable Postgres: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [x] Test count: ≥3 new tests

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): add POST /api/auth/2fa/enroll`

---

### T7: `Confirm2FA` endpoint

**What**: `POST /api/auth/2fa/confirm` - validates `code` against the pending secret, sets `enabled_at`, generates and returns 10 recovery codes; `422` on a wrong code.
**Where**: `internal/api/auth_handler.go` (edit; route registered in `internal/cli/routes.go`)
**Depends on**: T6
**Reuses**: `auth.ValidateTOTPCode` (T2), `TwoFactorRepository.CreateRecoveryCodes` (T4).
**Requirement**: TOTP-02, TOTP-03

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Correct code: `200`, `enabled_at` set, 10 plaintext recovery codes returned exactly once
- [ ] Wrong code: `422`, `enabled_at` still NULL, no recovery codes generated
- [ ] Gate check passes on disposable Postgres: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [ ] Test count: ≥3 new tests

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): add POST /api/auth/2fa/confirm`

---

### T8: Extract `Login`'s session-issuance into a shared helper

**What**: Pure refactor - move the tenant-resolution + `IssueSessionWithTenant` + cookie block (`internal/api/auth_handler.go:120-157`) into an unexported method, called by `Login`. No behavior change; every existing `Login` test must still pass unmodified.
**Where**: `internal/api/auth_handler.go` (edit)
**Depends on**: T7
**Reuses**: The exact existing logic - this is extraction, not rewriting.
**Requirement**: TOTP-05, TOTP-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `Login`'s behavior is byte-for-byte unchanged for a user without 2FA (all existing `TestLogin_*` tests pass with zero modifications)
- [ ] The extracted helper is callable with just a `*db.User` and the response writer
- [ ] Gate check passes on disposable Postgres: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [ ] Test count: same as before this task (0 net new tests expected - this is a refactor task, verified by an unchanged pass count)

**Tests**: integration
**Gate**: full

**Commit**: `refactor(api): extract Login's session-issuance into a shared helper`

---

### T9: `Login` 2FA branch + `VerifyTwoFactor` (TOTP code path)

**What**: `Login` checks `TwoFactorRepository.GetSecret`; if `enabled_at` is set, issues a challenge token (T5) instead of a session. New `POST /api/auth/login/verify-2fa` validates the challenge token, then the `code` against the user's confirmed secret, then calls T8's shared helper on success.
**Where**: `internal/api/auth_handler.go` (edit `Login`, new `VerifyTwoFactor`), `internal/cli/routes.go` (new route, added to `credentialLimiter`)
**Depends on**: T2, T5, T8
**Reuses**: T8's shared session-issuance helper; `credentialLimiter` middleware already wired for `login`/`password-reset`.
**Requirement**: TOTP-05, TOTP-06, TOTP-07, TOTP-08, TOTP-09

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `Login` for a 2FA-enabled user returns `200` with a challenge token, no `vane_session` cookie set
- [ ] `Login` for a 2FA-disabled user is unaffected (T8 already guarantees this; this task re-confirms via a joint test)
- [ ] `verify-2fa` with a valid token + correct code issues a full session (cookie set, `loginResponse` body)
- [ ] `verify-2fa` with an expired/malformed/already-consumed token returns `401`, no session
- [ ] `verify-2fa` with a wrong code returns `401`, challenge token remains usable for retry (not marked used)
- [ ] `verify-2fa` rejects a challenge whose user disabled 2FA since the token was issued (edge case)
- [ ] Gate check passes on disposable Postgres: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [ ] Test count: ≥8 new tests

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): gate Login behind a TOTP second factor via verify-2fa`

---

### T10: Recovery-code login path

**What**: `verify-2fa` accepts `recovery_code` as an alternative to `code`; on match, consumes it (T4's `ConsumeRecoveryCode`) and issues a session the same way; a used code is permanently rejected.
**Where**: `internal/api/auth_handler.go` (edit `VerifyTwoFactor`)
**Depends on**: T9
**Reuses**: `TwoFactorRepository.ConsumeRecoveryCode` (T4).
**Requirement**: TOTP-10, TOTP-11

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] A valid, unused recovery code logs the user in and marks it used
- [ ] The same code retried immediately returns `401`
- [ ] A different, unused code from the same batch still works
- [ ] Gate check passes on disposable Postgres: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [ ] Test count: ≥3 new tests

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): support recovery-code login in verify-2fa`

---

### T11: `Disable2FA` endpoint

**What**: `POST /api/auth/2fa/disable` - requires `current_password`; on success clears the secret and deletes all recovery codes; idempotent `200` no-op if 2FA isn't enabled.
**Where**: `internal/api/auth_handler.go` (edit), `internal/cli/routes.go`
**Depends on**: T7
**Reuses**: `auth.VerifyPassword` (same check `ChangePassword` already uses).
**Requirement**: TOTP-12, TOTP-13

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Correct password: `200`, secret cleared, recovery codes deleted, a subsequent login no longer requires 2FA
- [ ] Wrong password: `401`, 2FA remains enabled
- [ ] No 2FA enabled: `200` no-op
- [ ] Gate check passes on disposable Postgres: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [ ] Test count: ≥3 new tests

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): add POST /api/auth/2fa/disable`

---

## Phase Execution Map

```
Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5

Phase 1:  T1              T2 ------→ T3
Phase 2:  T4          T5
Phase 3:  T6 ------→ T7
Phase 4:  T8 ------→ T9 ------→ T10
Phase 5:  T11
```

Execution is strictly sequential within a phase - no intra-phase parallelism. T4/T5 both depend only on T1 and can be ordered either way within Phase 2.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: Migration 0030 | 1 schema change (3 tables, 1 concern) | ✅ Granular |
| T2: TOTP + challenge token functions | 1 file, pure functions | ✅ Granular |
| T3: Harden VerifySessionClaims | 1 function edit | ✅ Granular |
| T4: TwoFactorRepository | 1 repository | ✅ Granular |
| T5: TwoFactorChallengeRepository | 1 repository | ✅ Granular |
| T6: Enroll endpoint | 1 endpoint | ✅ Granular |
| T7: Confirm2FA endpoint | 1 endpoint | ✅ Granular |
| T8: Extract Login helper | 1 refactor, no new endpoint | ✅ Granular |
| T9: Login branch + verify-2fa (TOTP path) | 1 endpoint + 1 modified endpoint, one cohesive login-gate concern | ✅ Granular |
| T10: Recovery-code path | 1 alternative-input branch on an existing endpoint | ✅ Granular |
| T11: Disable2FA endpoint | 1 endpoint | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | — | ✅ Match |
| T2 | None | — | ✅ Match |
| T3 | T2 | T2 → T3 | ✅ Match |
| T4 | T1 | T1 → T4 | ✅ Match |
| T5 | T1 | T1 → T5 | ✅ Match |
| T6 | T2, T4 | T2 → T6, T4 → T6 | ✅ Match |
| T7 | T6 | T6 → T7 | ✅ Match |
| T8 | T7 | T7 → T8 | ✅ Match |
| T9 | T2, T5, T8 | T2 → T9, T5 → T9, T8 → T9 | ✅ Match |
| T10 | T9 | T9 → T10 | ✅ Match |
| T11 | T7 | T7 → T11 | ✅ Match |

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1 | Schema / migration | none | none | ✅ OK |
| T2 | `internal/auth` | unit | unit | ✅ OK |
| T3 | `internal/auth` | unit | unit | ✅ OK |
| T4 | Repository | integration | integration | ✅ OK |
| T5 | Repository | integration | integration | ✅ OK |
| T6 | API handler | integration | integration | ✅ OK |
| T7 | API handler | integration | integration | ✅ OK |
| T8 | API handler (refactor) | integration | integration | ✅ OK |
| T9 | API handler | integration | integration | ✅ OK |
| T10 | API handler | integration | integration | ✅ OK |
| T11 | API handler | integration | integration | ✅ OK |
