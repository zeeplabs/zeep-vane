# Profile Self-Service Specification

## Problem Statement

The redesigned Meu Perfil screen (`handoff-new-layout/Meu Perfil.dc.html`, tracked in `.specs/features/new-layout-migration/gap-analysis.md`) shows a "Informações pessoais" card (name, email, avatar) and a "Segurança" card with a current/new/confirm password form. Today `GET /api/auth/me` returns the authenticated user's name/email, but nothing lets a user change their own name, and nothing lets an authenticated user change their own password knowing the current one — `UserRepository.UpdatePasswordHash` exists but is only reachable via the unauthenticated email-token reset flow (`password_reset_handler.go`). This spec adds the two authenticated self-service mutations the screen needs, without touching login, session issuance, or any other auth mechanism.

## Goals

- [ ] An authenticated user can change their own display name.
- [ ] An authenticated user can change their own password by proving they know the current one — independent of the email-token reset flow.
- [ ] Changing password revokes every other active login for that user (reuses the existing global `SessionsRevokedAt` mechanism), same as the reset-by-email flow already does.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Email address change | No re-verification mechanism exists for an email change (the only verification flow today, `email_verification_repository.go`, is for initial SaaS signup). Changing the login credential without re-verifying it is a real account-takeover risk if a session is compromised. Deferred to a future spec that designs the re-verification step properly. |
| Avatar upload | The mock shows an avatar slot but no upload mechanism exists (`Tenant.LogoData` is the only image-upload precedent, scoped to company branding, not per-user). Out of scope until a per-user file-storage decision is made. |
| Per-device session listing/revocation | Covered by the separate `user-sessions` spec — this spec's password change reuses the existing global revoke, not per-device revoke. |
| 2FA enrollment/enforcement | Covered by the separate `auth-2fa-totp` spec. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Name update surface | New endpoint `PATCH /api/auth/me`, body `{name}`, updates the authenticated user's own `Name` only | Mirrors the existing self-scoped pattern (`Me`/`Logout`/`SwitchTenant` on `AuthHandler` already read `UserFromContext`); a name is not sensitive enough to warrant a dedicated confirmation step. | y — agent default, no objection raised |
| Password change surface | New endpoint `POST /api/auth/change-password`, body `{current_password, new_password}` | Keeps it a distinct action from profile-field updates (matches the mock's separate "Atualizar senha" button) and from the token-based reset flow, which stays untouched. | y — agent default, no objection raised |
| Current-password verification | Compare `current_password` against the stored `PasswordHash` with the same bcrypt compare already used by `Login` | Reuses the existing, already-audited comparison path instead of introducing a second one. | y — agent default, no objection raised |
| New-password strength rule | Same minimum length/complexity rule already enforced by `signup_handler.go`/`bootstrap_handler.go` for `password`, no new rule | One password policy for the whole app avoids two answers to "why did my password get rejected". | y — agent default, no objection raised |
| Effect on other sessions | `UserRepository.RevokeSessions` is called after a successful change, exactly like `PasswordResetHandler.Confirm` already does | Changing a password because it may be compromised should end every other login immediately; this is the existing behavior for the reset-by-email path, kept consistent here. | y — agent default, no objection raised |
| RBAC | `anyRole`, self only — no role can change another user's password or name through this endpoint | This is a self-service action; changing another admin's credentials is an account-takeover vector and isn't requested by the mock (which shows no "manage other users' passwords" UI). | y — agent default, no objection raised |
| Rate limiting | `change-password` is added to the existing shared `credentialLimiter` (the same limiter already covering `/api/auth/login` and the password-reset routes) | A wrong-current-password guess is exactly the kind of credential-guessing attempt that limiter exists to slow down; `PATCH /api/auth/me` (name only) is not credential-related and stays unlimited like other authenticated writes. | y — agent default, no objection raised |

**Open questions:** none — all resolved or logged above.

---

## User Stories

### P1: User updates their own display name ⭐ MVP

**User Story**: As any authenticated user, I want to change my display name, so my teammates see the right name in the sidebar/timeline attribution.

**Why P1**: Blocks the "Informações pessoais" card of the redesigned screen from being wired to anything real.

**Acceptance Criteria**:

1. WHEN an authenticated user calls `PATCH /api/auth/me` with a non-empty `name` THEN the system SHALL update that user's `Name` and respond `200` with the updated profile.
2. IF `name` is empty or missing THEN the system SHALL respond `422` and SHALL NOT change the stored name.
3. The system SHALL NOT allow this endpoint to change any field other than `name` (no `email`, no role, no tenant membership) — a request body containing other fields SHALL have those fields ignored, not applied.

**Independent Test**: `PATCH /api/auth/me` with `{name: "New Name"}` as an authenticated user, confirm `200` and `GET /api/auth/me` reflects the change; repeat with `{name: ""}` and confirm `422` with no change.

---

### P1: User changes their own password ⭐ MVP

**User Story**: As any authenticated user, I want to change my password by entering my current one, so I don't have to log out and use the email-reset flow just to rotate my credential.

**Why P1**: This is the entire "Segurança" card's password section; without it the redesigned screen has a form that does nothing.

**Acceptance Criteria**:

1. WHEN an authenticated user calls `POST /api/auth/change-password` with a `current_password` that matches their stored hash and a `new_password` meeting the app's password policy THEN the system SHALL update their `PasswordHash`, SHALL call `RevokeSessions` for that user, and SHALL respond `200`.
2. IF `current_password` does not match the stored hash THEN the system SHALL respond `401` and SHALL NOT change the password or revoke sessions.
3. IF `new_password` fails the app's password policy THEN the system SHALL respond `422` and SHALL NOT change the password or revoke sessions.
4. WHEN the password change succeeds THEN the caller's own current session SHALL remain valid for the rest of the request/response cycle (the new session-issuing step, if any, is out of scope — the caller is not forced to immediately re-login), but any other previously issued session for that user SHALL be rejected on its next authenticated request, per the existing `SessionsRevokedAt` check in `RequireAuth`.

**Independent Test**: Log in as a user to get session A, call `change-password` with the correct current password, confirm `200`; issue a second login for the same user beforehand as session B, confirm a request with session B is now rejected while session A (the one that made the change) still works for its current request. Repeat with a wrong `current_password` and confirm `401` with no session revoked.

---

## Edge Cases

- IF `new_password` equals `current_password` THEN the system SHALL still accept it as valid (not a special case) as long as it passes the password policy — matching the reset-by-email flow's existing behavior of not comparing old vs. new.
- WHEN `change-password` is called without an active session (unauthenticated) THEN the system SHALL respond `401` via the existing `RequireAuth` middleware, same as any other protected route.
- IF the request body for either endpoint contains additional unknown fields THEN the system SHALL ignore them rather than erroring, matching this codebase's existing JSON-decoding convention.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| PROFSS-01 | P1: User updates own display name | Execute | Verified |
| PROFSS-02 | P1: User updates own display name (validation) | Execute | Verified |
| PROFSS-03 | P1: User updates own display name (field scoping) | Execute | Verified |
| PROFSS-04 | P1: User changes own password | Execute | Verified |
| PROFSS-05 | P1: User changes own password (wrong current) | Execute | Verified |
| PROFSS-06 | P1: User changes own password (weak new) | Execute | Verified |

**Coverage:** 6 total, 6 mapped to tasks, 0 unmapped (Medium scope — tasks were implicit in Execute, not a formal `tasks.md`)

---

## Success Criteria

- [x] `PATCH /api/auth/me` lets a user change their own name only, `422` on empty.
- [x] `POST /api/auth/change-password` verifies the current password, enforces the existing password policy, and revokes other sessions on success.
- [x] Neither endpoint can affect another user's account.
- [x] `change-password` is covered by the existing credential rate limiter.
