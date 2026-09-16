# Two-Factor Authentication (TOTP) Specification

**Scope: Large.** This spec covers WHAT and WHY. Because it changes the login flow itself and is flagged as higher-risk under `AGENTS.md` §7 ("changes to auth/session handling... explain the change and, when in doubt, confirm before applying"), a formal Design phase (architecture of the challenge-token mechanism, `internal/auth` package changes) runs before Execute, not skipped inline as the prior two Medium-scope specs in this cycle were.

## Problem Statement

The redesigned Meu Perfil screen (`handoff-new-layout/Meu Perfil.dc.html`) shows a "Segurança" card with a two-factor authentication toggle: enrollment via QR code/manual secret, a 6-digit verification step, and an enabled/disabled badge. Today there is no TOTP infrastructure anywhere in the codebase (`grep` for `totp`/`two_fa` returns nothing) and `AuthHandler.Login` issues a full session (`auth.IssueSessionWithTenant`) the instant a password check passes — there is no second-factor step in the login flow at all.

## Goals

- [ ] A user can enroll in TOTP-based 2FA: generate a secret, display it as a QR-encodable `otpauth://` URI, and confirm enrollment by entering a real generated code.
- [ ] Once enrolled, `Login` no longer issues a full session on password success alone — it requires a valid TOTP code (or a recovery code) as a second step.
- [ ] Losing the authenticator device does not permanently lock a user out: one-time recovery codes are issued at enrollment.
- [ ] Disabling 2FA requires re-proving the current password, so a hijacked already-authenticated session cannot silently turn 2FA off.

## Out of Scope

| Feature | Reason |
| --- | --- |
| SMS/email-based second factor | Mock only shows an authenticator-app flow (QR + manual secret); no phone number field or SMS provider exists in this codebase. |
| Admin-forced 2FA enrollment (org-wide policy) | Not shown in any handoff screen; this spec is purely self-service, one user enrolling themselves. |
| Regenerating recovery codes after the initial batch is exhausted | Not shown in the mock; today's scope only covers issuing them once at enrollment. A future spec can add a "regenerate codes" action once there's a concrete UI for it. |
| Any change to `internal/auth`'s session token *format* beyond adding the new pre-2FA challenge token | `IssueSessionWithTenant`/`SessionTTL`/the `vane_session` cookie shape stay exactly as they are for a fully-authenticated session; only a new, separate short-lived token type is added for the in-between "password verified, 2FA pending" state. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Login flow shape when 2FA is enabled | `POST /api/auth/login` with correct password for a 2FA-enabled user does **not** set `vane_session` and does **not** return a usable session token. Instead it responds `200` with a short-lived (5 minute) signed "2FA challenge" token, distinct from a session token. A new `POST /api/auth/login/verify-2fa` endpoint takes `{challenge_token, code}` (or `{challenge_token, recovery_code}`) and, on success, performs exactly the same tenant-resolution + session-issuance `Login` already does today, then sets `vane_session`. | User decision: no session cookie is ever set before the second factor is proven — closes the window where a stolen half-authenticated cookie already grants access. | y |
| Challenge token mechanism | A new signed token type in `internal/auth` (same HMAC signing primitive `IssueSessionWithTenant` already uses, different type/audience claim so it can never be accepted by `RequireAuth` as a real session), carrying only `user_id` and a short expiry (5 minutes) | Reuses the existing, already-reviewed signing primitive instead of inventing a second one; the distinct claim type is what stops a challenge token from ever being replayed as a session token even if `RequireAuth`'s parsing were ever refactored carelessly. | y — agent default, no objection raised |
| TOTP library | `github.com/pquerna/otp` (RFC 6238 TOTP + RFC 4226 HOTP, the de facto standard Go TOTP library, MIT-licensed) added as a new dependency | Per the Knowledge Verification Chain: no existing in-repo TOTP code (Step 1), nothing in project docs prescribing one (Step 2); this library is the standard, widely-audited choice or a from-scratch algorithm re-implementation is unjustified risk for a security primitive (Step 3/4). Version to be pinned during Execute via `go get`, not guessed here. | y — agent default, no objection raised |
| Secret storage | New `two_factor_secrets` table (or column set on `users`): `user_id` (PK/FK), `encrypted_secret` (via the existing `internal/crypto.Encrypt`/`VANE_MASTER_KEY` pattern already used by `email_provider_repository.go`), `enabled_at` (NULL until the enrollment-verify step succeeds), `created_at` | Reuses the exact encryption-at-rest pattern already established and reviewed for provider API keys — no new crypto primitive introduced. `enabled_at` NULL distinguishes "secret generated, QR shown, not yet confirmed" from "2FA actually enforced on login" so an abandoned enrollment never silently locks anyone out. | y — agent default, no objection raised |
| Recovery codes | 10 codes generated at the moment enrollment is confirmed (not at secret-generation time, so an abandoned enrollment never burns codes), each stored as a bcrypt hash in a new `two_factor_recovery_codes` table (`user_id`, `code_hash`, `used_at` NULL until consumed), shown to the user exactly once in the success step's response | User decision: recovery codes are in scope this cycle. Hashing (not encrypting) matches how `PasswordHash` is already stored — a recovery code is a credential, not a value that ever needs to be read back. | y |
| Recovery code consumption | `POST /api/auth/login/verify-2fa` accepts `recovery_code` as an alternative to `code`; on match it marks that one row `used_at = now()` (single use) and completes login exactly like a valid TOTP code would | Matches the industry-standard recovery-code UX (each code works exactly once); reusing the same endpoint avoids a third auth surface. | y — agent default, no objection raised |
| Enrollment endpoints | `POST /api/auth/2fa/enroll` (self, `anyRole`) generates and stores a new secret + returns the `otpauth://` URI and raw secret (for manual entry) — overwrites any previous *unconfirmed* enrollment for that user, never a confirmed one. `POST /api/auth/2fa/confirm` (self) takes `{code}`, verifies it against the pending secret, sets `enabled_at`, generates and returns the 10 recovery codes. | Splits "show me a QR" from "I proved I scanned it" exactly like the mock's two-step drawer (scan step, then verify step) — matches `isTwoFaStepScan`/`isTwoFaStepVerify` in the mock 1:1. | y — agent default, no objection raised |
| Disabling 2FA | `POST /api/auth/2fa/disable` (self, `anyRole`) requires `{current_password}` in the body, verified the same way `profile-self-service`'s `change-password` verifies it; on success deletes the `two_factor_secrets` row (or clears `enabled_at`) and all recovery codes for that user | User decision: re-proving the password is required, closing the hijacked-session-disables-2FA gap the mock's 1-click toggle would otherwise leave open. | y |
| What happens to other active sessions when 2FA is enabled or disabled | Nothing — enabling/disabling 2FA does not call `RevokeSessions`; it only changes what a *future* login requires | Enabling/disabling 2FA is not evidence of a compromised password the way a password change is; forcibly logging out every device on every 2FA toggle would be surprising, disruptive UX with no matching signal in the mock. | y — agent default, no objection raised |
| RBAC | `anyRole`, self only, on every new endpoint — no role can enroll/confirm/disable 2FA for another user | Matches `profile-self-service`'s existing self-only pattern; 2FA is a personal-account security setting, not an admin-managed one in this mock. | y — agent default, no objection raised |
| Rate limiting | `verify-2fa` (login step) is added to the existing shared `credentialLimiter`, same as `login`/`password-reset`; `enroll`/`confirm`/`disable` (all requiring an existing authenticated session or a fresh challenge token, not a bare credential guess) are not | `verify-2fa` is exactly the kind of code-guessing surface (6-digit space, or a 10-code recovery pool) the credential limiter exists to slow down. | y — agent default, no objection raised |

**Open questions:** none — all resolved or logged above.

---

## User Stories

### P1: User enrolls in TOTP 2FA ⭐ MVP

**User Story**: As any authenticated user, I want to enable two-factor authentication with my authenticator app, so my account is protected even if my password leaks.

**Why P1**: The entire enrollment drawer in the mock (scan/verify/success steps) has nothing real to call without this.

**Acceptance Criteria**:

1. WHEN an authenticated user without 2FA enabled calls `POST /api/auth/2fa/enroll` THEN the system SHALL generate a new TOTP secret, store it encrypted with `enabled_at` NULL, and respond `200` with an `otpauth://` URI and the raw secret for manual entry.
2. WHEN that user calls `POST /api/auth/2fa/confirm` with a `code` that validates against the pending secret THEN the system SHALL set `enabled_at`, generate 10 recovery codes, store their hashes, and respond `200` with the 10 plaintext recovery codes (returned only this once).
3. IF `code` does not validate against the pending secret THEN the system SHALL respond `422` and SHALL NOT set `enabled_at` or generate recovery codes.
4. IF a user who already has 2FA enabled (`enabled_at` set) calls `POST /api/auth/2fa/enroll` THEN the system SHALL respond `409` — an enrollment cannot silently replace an active one; the user must disable first.

**Independent Test**: Enroll, compute the correct TOTP code from the returned secret using the same algorithm, call `confirm`, confirm `200` with 10 recovery codes; repeat `confirm` with a wrong code and confirm `422` with `enabled_at` still NULL; call `enroll` again while already enabled and confirm `409`.

---

### P1: Login requires the second factor when 2FA is enabled ⭐ MVP

**User Story**: As a user with 2FA enabled, I want `Login` to ask for my authenticator code before granting access, so a leaked password alone can't get into my account.

**Why P1**: This is the actual security property the whole feature exists for — without it, enrollment is theater.

**Acceptance Criteria**:

1. WHEN `POST /api/auth/login` succeeds on password for a user with 2FA `enabled_at` set THEN the system SHALL NOT set the `vane_session` cookie and SHALL respond `200` with a short-lived challenge token instead of a session token.
2. WHEN `POST /api/auth/login/verify-2fa` is called with a valid, unexpired challenge token and a `code` that validates against that user's confirmed secret THEN the system SHALL perform the same tenant-resolution and session-issuance `Login` performs today and respond exactly like a successful `Login` would (session cookie set, `loginResponse` body).
3. IF the challenge token is expired, malformed, or already consumed THEN the system SHALL respond `401` and SHALL NOT issue a session.
4. IF the `code` (or `recovery_code`) does not validate THEN the system SHALL respond `401` and SHALL NOT issue a session or consume the challenge token (the user may retry within the token's remaining validity).
5. WHEN a user does not have 2FA enabled THEN `POST /api/auth/login` SHALL behave exactly as it does today — full session issued on password success, no challenge token involved.

**Independent Test**: Enable 2FA for a user, log in with the correct password, confirm no `vane_session` cookie is set and a challenge token is returned; call `verify-2fa` with the correct current TOTP code and confirm a session is now issued; repeat login and call `verify-2fa` with a wrong code and confirm `401` with no session. Confirm a user without 2FA still gets a full session directly from `Login`.

---

### P2: User completes login with a recovery code

**User Story**: As a user who lost their authenticator device, I want to log in using one of my recovery codes, so I'm not permanently locked out of my account.

**Why P2**: Important safety net, but only exercised in the device-loss edge case — not required for the primary enroll/login flows to work.

**Acceptance Criteria**:

1. WHEN `POST /api/auth/login/verify-2fa` is called with a valid challenge token and a `recovery_code` that matches an unused hash for that user THEN the system SHALL mark that code `used_at = now()`, issue a session exactly like a valid TOTP code would, and respond `200`.
2. IF the `recovery_code` was already used THEN the system SHALL respond `401` and SHALL NOT issue a session.
3. The system SHALL treat a used recovery code as permanently spent — it SHALL NOT accept the same code again even in a later, unrelated login attempt.

**Independent Test**: Consume one of the 10 recovery codes to log in successfully; immediately retry the same code and confirm `401`; confirm a different, unused code from the same batch still works.

---

### P1: User disables 2FA with password confirmation

**User Story**: As a user, I want to turn off 2FA when I no longer want it, but only by re-proving my password, so an already-open but hijacked session can't quietly remove my account's second factor.

**Why P1**: Required for the mock's "Desativar" action to be safe to ship; without the password check this endpoint would be a privilege-escalation shortcut for session hijacking.

**Acceptance Criteria**:

1. WHEN a user with 2FA enabled calls `POST /api/auth/2fa/disable` with a `current_password` matching their stored hash THEN the system SHALL clear `enabled_at` (or delete the row), delete all recovery codes for that user, and respond `200`.
2. IF `current_password` does not match THEN the system SHALL respond `401` and SHALL NOT disable 2FA.
3. IF a user without 2FA enabled calls this endpoint THEN the system SHALL respond `200` idempotently (no-op) rather than erroring — disabling something already off is not a failure state.

**Independent Test**: Disable with the correct password, confirm `200` and that a subsequent login for that user no longer requests a 2FA step; repeat with a wrong password and confirm `401` with 2FA still enabled.

---

## Edge Cases

- IF `POST /api/auth/login/verify-2fa` receives a challenge token belonging to a user who disabled 2FA in the few minutes since the token was issued THEN the system SHALL respond `401` (the token's implied "2FA required" precondition no longer holds) rather than silently issuing a session.
- WHEN a user regenerates their enrollment (only reachable pre-confirmation, per AC4 of the enrollment story) THEN any previously generated but unconfirmed secret SHALL be discarded, not left as a second valid pending secret.
- WHEN computing/validating a TOTP code THEN the system SHALL allow the standard ±1 time-step window (per `pquerna/otp`'s default skew tolerance) to tolerate minor clock drift between server and authenticator app, matching the library's documented default rather than a custom tolerance invented for this spec.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| TOTP-01 | P1: User enrolls in TOTP 2FA | Execute | Verified |
| TOTP-02 | P1: User enrolls in TOTP 2FA (confirm) | Execute | Verified |
| TOTP-03 | P1: User enrolls in TOTP 2FA (bad code) | Execute | Verified |
| TOTP-04 | P1: User enrolls in TOTP 2FA (already enabled) | Execute | Verified |
| TOTP-05 | P1: Login requires second factor | - | Verified |
| TOTP-06 | P1: Login requires second factor (verify success) | - | Verified |
| TOTP-07 | P1: Login requires second factor (bad/expired token) | - | Verified |
| TOTP-08 | P1: Login requires second factor (bad code) | - | Verified |
| TOTP-09 | P1: Login requires second factor (no 2FA, unchanged) | - | Verified |
| TOTP-10 | P2: Recovery code login | - | Verified |
| TOTP-11 | P2: Recovery code reuse rejected | - | Verified |
| TOTP-12 | P1: Disable 2FA with password | - | Verified |
| TOTP-13 | P1: Disable 2FA wrong password | - | Verified |

**Coverage:** 13 total, 13 verified, 0 unmapped. See `.specs/features/auth-2fa-totp/validation.md`.

---

## Success Criteria

- [ ] A user can enroll, confirm with a real TOTP code, and receive 10 recovery codes.
- [ ] `Login` never sets `vane_session` for a 2FA-enabled user without a verified second factor.
- [ ] A spent recovery code never works twice.
- [ ] Disabling 2FA requires the current password.
- [ ] No existing session-token format or `RequireAuth` behavior changes for users without 2FA enabled.
