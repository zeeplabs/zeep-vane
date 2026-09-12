# Login 2FA Step Specification

**Scope: Medium (frontend-only).** The `auth-2fa-totp` backend already issues a `{challenge_token}` on `POST /api/auth/login` for a 2FA-enabled user and completes the session on `POST /api/auth/login/verify-2fa` (TOTP-05..11, Verified). Only the SPA never handled that branch, so enabling 2FA from the Meu Perfil page currently locks the user out. This spec adds the verification step to `LoginPage`, a typed `login()` outcome plus `verifyTwoFactor()` on `AuthProvider`, the MSW handlers and the copy. No backend change.

## Problem Statement

`auth-2fa-totp` delivered the full TOTP login backend: when a user with confirmed 2FA submits a correct password, `Login` returns `200 {challenge_token}` and sets no session cookie, and `POST /api/auth/login/verify-2fa` (accepting a 6-digit `code` or a `recovery_code`) issues the session. The Meu Perfil feature made 2FA enrollable, but `LoginPage` only knows the `{token}` path: after a challenge response it calls `GET /api/auth/me`, gets a 401, and reports a generic failure. A user who enables 2FA today cannot sign in again from the SPA. This spec closes that loop on the frontend.

## Goals

- [ ] A user with 2FA enabled who enters the correct password sees a verification step instead of an error, and signs in after submitting a valid code.
- [ ] The verification step accepts a one-time recovery code as a fallback.
- [ ] A user without 2FA logs in exactly as before.
- [ ] The challenge token lives only in memory; the current session is never established until the second factor validates.

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
| --- | --- |
| Any backend change | `auth-2fa-totp` P1/P2 are implemented and verified; the endpoints already exist. |
| "Remember this device" / trusted-device cookies | Not in `auth-2fa-totp`'s scope; adds a persistent bypass the security model does not have. |
| A dedicated UI for rate limiting (429) | `verify-2fa` shares the existing login rate limiter (`auth-2fa-totp` spec); the frontend shows the generic error message the API returns. |
| A separate `/login/2fa` route | The token must not be persisted to the URL or storage; the step lives inside `LoginPage`. |
| Changing the password-reset or signup flows | Unrelated; they do not reach the 2FA branch. |

---

## Assumptions & Open Questions

Every ambiguity is resolved or recorded here - nothing is left silently unclear.

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Layer | Frontend only; the four auth endpoints already exist | `auth-2fa-totp` spec.md marks TOTP-05..11 Verified; no new API surface is needed. | y - codebase (`internal/api/auth_handler.go:180-331`) |
| Step placement | In-place step swap inside `LoginPage` (`credentials` -> `twoFactor`) | Keeps the challenge token in component state; a route/storage would expose it to history and refresh. | y - user decision 2026-09-11 |
| Provider API shape | `login()` returns a discriminated `LoginOutcome`; a new `verifyTwoFactor()` completes and hydrates | Explicit result beats exception-as-control-flow; only `LoginPage` consumes `login()`. | y - user decision 2026-09-11 |
| Recovery-code fallback | Included in this feature, via a method toggle inside the step | It is the device-loss safety net; without it a user without their authenticator is permanently locked out. | y - user decision 2026-09-11 |
| Session on challenge | No session/cookie is established until `verify-2fa` succeeds | Backend contract: `Login` returns only the challenge token and sets no cookie. | y - backend contract |
| Challenge expiry | Rely on the backend's 401; no client-side 5-minute timer | The server is the source of truth (`auth.TwoFactorChallengeTTL`, `internal/auth/two_factor.go:21`); a timer would drift and add complexity. | y - agent default, no objection |
| Token persistence | Held in React state only; never URL, `localStorage` or `sessionStorage` | AD-004: session state is never put in client storage; the same reasoning applies to the pre-session challenge token. | y - project decision AD-004 |
| Feedback mechanism | Inline error under the field; success is the navigation itself | Matches `LoginPage`'s existing inline-error pattern; no toast on a full-screen auth page. | y - existing pattern |
| Post-verify navigation | Navigate to `/` like the normal login | `RootRoute`/`RequireAuth` already resolve bootstrap and tenant selection; reusing the path avoids duplicating that logic. | y - existing pattern |
| RBAC | Public, unauthenticated route | Login is pre-session; no role applies. | y - backend contract |

**Open questions:** none - all resolved or logged above.

---

## User Stories

### P1: Login requires the second factor when 2FA is enabled ⭐ MVP

**User Story**: As a user with 2FA enabled, I want the login screen to ask for my authenticator code, so I can sign in securely without being locked out.

**Why P1**: Without this the 2FA feature is unusable and actively locks users out.

**Acceptance Criteria**:

1. WHEN a user with 2FA enabled submits a correct email and password THEN the page SHALL show the verification step and SHALL NOT establish a session.
2. WHEN the user submits a valid 6-digit code in the verification step THEN the page SHALL complete the login and navigate into the app.
3. IF the submitted code is invalid or the challenge has expired THEN the page SHALL show an inline error and SHALL keep the verification step for another attempt.
4. WHEN a user without 2FA submits correct credentials THEN the login SHALL proceed without showing the verification step.

**Independent Test**: With MSW, submit correct credentials for a 2FA-enabled admin and confirm the code step appears (no navigation); submit a valid code and confirm navigation; submit a wrong code and confirm the inline error with the step still shown; submit a normal admin's credentials and confirm it signs in directly.

---

### P2: Login with a recovery code

**User Story**: As a user who lost their authenticator, I want to sign in with a one-time recovery code, so I am not locked out of my account.

**Why P2**: Important safety net, but only exercised in the device-loss edge case.

**Acceptance Criteria**:

1. WHEN the user switches to the recovery-code method and submits a valid unused recovery code THEN the page SHALL complete the login.
2. IF the recovery code is invalid or already used THEN the page SHALL show an inline error and SHALL keep the verification step.
3. WHEN the user switches between the app-code and recovery-code methods THEN the page SHALL clear the other method's input and error.

**Independent Test**: Toggle to recovery, submit a valid code and confirm sign-in; submit an invalid code and confirm the inline error; toggle back to app code and confirm the recovery input and error are cleared.

---

### P2: Cancel the verification step

**User Story**: As a user on the verification step, I want to go back and re-enter my credentials, so I can recover from a mistyped password or an expired challenge.

**Why P2**: Comfort feature around the P1 flow, not required for the happy path.

**Acceptance Criteria**:

1. WHEN the user chooses "Voltar" in the verification step THEN the page SHALL return to the credentials step and SHALL discard the current challenge token.
2. The page SHALL hold the challenge token only in memory and SHALL NOT persist it to the URL or to browser storage.

**Independent Test**: Reach the verification step, choose "Voltar", confirm the credentials form returns; assert the token was never written to `localStorage`/`sessionStorage` or the URL.

---

### P2: Provider surfaces the 2FA branch

**User Story**: As the app, I want the auth provider to tell the page that a second factor is required, so the page can render the right step.

**Why P2**: Internal contract that enables the P1 UI; not user-visible on its own.

**Acceptance Criteria**:

1. WHEN `login` receives a `challenge_token` in the response THEN the provider SHALL report a `twoFactorRequired` outcome and SHALL NOT hydrate an authenticated admin.
2. WHEN `verifyTwoFactor` succeeds THEN the provider SHALL hydrate the authenticated admin from `GET /api/auth/me`, exactly like the normal login.

**Independent Test**: Unit-test `login` against a 2FA challenge response and confirm no admin is set and the token is returned; unit-test `verifyTwoFactor` and confirm the admin is hydrated from `/me`.

---

## Edge Cases

- IF the challenge token has expired (5 minutes) THEN `verify-2fa` returns `401` and the page SHALL show the inline error; the user recovers via "Voltar".
- IF 2FA was disabled after the challenge was issued THEN the backend returns `401`; the page SHALL show the same inline error.
- IF the API returns a non-401 failure (500/network) THEN the page SHALL show the existing generic credentials error, not a silent failure.
- WHEN the browser locale is English THEN every new string SHALL render in English through `react-i18next`, and pt-BR otherwise.
- The verification step SHALL NOT render the password field or the password-recovery link.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| LOGIN2FA-01 | P1: verification step shown, no session | Design | Pending |
| LOGIN2FA-02 | P1: valid code signs in | Design | Pending |
| LOGIN2FA-03 | P1: invalid code/expired inline, stay | Design | Pending |
| LOGIN2FA-04 | P1: non-2FA login unchanged | Design | Pending |
| LOGIN2FA-05 | P2: valid recovery code signs in | Design | Pending |
| LOGIN2FA-06 | P2: invalid/used recovery inline, stay | Design | Pending |
| LOGIN2FA-07 | P2: method switch clears input/error | Design | Pending |
| LOGIN2FA-08 | P2: Voltar returns to credentials | Design | Pending |
| LOGIN2FA-09 | P2: token in memory only | Design | Pending |
| LOGIN2FA-10 | P2: login reports twoFactorRequired | Design | Pending |
| LOGIN2FA-11 | P2: verifyTwoFactor hydrates admin | Design | Pending |
| LOGIN2FA-12 | Edge: all new strings pt + en | Design | Pending |

**ID format:** `[CATEGORY]-[NUMBER]` (e.g., `AUTH-01`, `CART-03`, `NOTIF-02`)

**Status values:** Pending -> In Design -> In Tasks -> Implementing -> Verified

**Coverage:** 12 total, 0 mapped to tasks, 12 unmapped (Design and Tasks phases follow before Execute)

---

## Success Criteria

- [ ] A 2FA-enabled user can complete login from the SPA with a TOTP code.
- [ ] The same user can complete login with an unused recovery code.
- [ ] A non-2FA user's login is unchanged.
- [ ] The challenge token is never persisted outside component memory.
- [ ] Every new string ships in pt-BR and English.
