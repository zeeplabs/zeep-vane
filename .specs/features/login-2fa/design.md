# Login 2FA Step Design

**Spec**: `.specs/features/login-2fa/spec.md`
**Status**: Draft

---

## Architecture Overview

A frontend-only change on top of the already-shipped `auth-2fa-totp` backend. `AuthProvider` gains a typed password-login result and a `verifyTwoFactor` method; `LoginPage` becomes a two-step state machine that holds the challenge token in memory and swaps the credentials form for an extracted verification step. No new route, no backend change.

```mermaid
graph TD
    LP[LoginPage] -->|login(email,pw)| AP[AuthProvider]
    AP -->|POST /api/auth/login| API[(Go API)]
    API -->|challenge_token| AP
    AP -->|LoginOutcome.twoFactorRequired| LP
    LP -->|verifyTwoFactor(token,factor)| AP
    AP -->|POST /api/auth/login/verify-2fa| API
    API -->|session cookie| AP
    AP -->|GET /api/auth/me| API
    AP -.AUTHENTICATED.-> LP
    LP --> STEP[LoginTwoFactorStep]
```

**Chosen approach (confirmed with the user 2026-09-11):** in-place step inside `LoginPage`, approach A on the provider (`login` returns a discriminated outcome; `verifyTwoFactor` completes and hydrates). Rejected: a `/login/2fa` route (would persist the token to the URL/storage) and a `TwoFactorRequiredError` thrown by `login` (exception as normal control flow).

---

## Code Reuse Analysis

### Existing Components to Leverage

| Component | Location | How to Use |
| --- | --- | --- |
| `AuthProvider` / `useAuth` | `web/src/auth/AuthProvider.tsx` | Extend `login`'s return type; add `verifyTwoFactor`; reuse its `/me` hydrate + `AUTHENTICATED` dispatch. |
| `LoginPage` | `web/src/features/auth/LoginPage.tsx` | Add the `step` state machine and render the new step; keep its layout, `EyeIcon`, error and submit patterns. |
| `Field`, `Button` | `web/src/components/ui/` | Inputs and actions for the verification step. |
| `apiFetch` / `ApiError` | `web/src/lib/apiClient.ts` | Request both endpoints; branch on `401`. |
| `react-i18next` | `web/src/lib/i18n.ts` | New `login.twoFactor.*` keys (pt + en). |
| MSW handlers | `web/src/test/msw/handlers.ts` | Add the login 2FA branch, `verify-2fa` handler and a recovery-code seed helper. |
| `AuthHandler.Login` / `VerifyTwoFactor` | `internal/api/auth_handler.go:180,237` | The contracts the frontend mirrors (no change). |
| `auth.TwoFactorChallengeTTL` | `internal/auth/two_factor.go:21` | Documents the 5-minute lifetime the UI's error copy references. |

### Integration Points

| System | Integration Method |
| --- | --- |
| `POST /api/auth/login` | Existing; response now branched on `challenge_token`. |
| `POST /api/auth/login/verify-2fa` | Existing backend; first frontend consumer. `{challenge_token, code}` or `{challenge_token, recovery_code}`. |
| `GET /api/auth/me` | Existing; unchanged hydrate after a successful verify. |
| MSW handlers | Extended so `LoginPage`/`AuthProvider` tests can drive both branches. |

---

## Components

### `AuthProvider` (modified)

- **Purpose**: Surface the 2FA branch and complete it.
- **Location**: `web/src/auth/AuthProvider.tsx`
- **Change**:
  - `type LoginOutcome = { kind: "authenticated" } | { kind: "twoFactorRequired"; challengeToken: string }`
  - `login(email, password): Promise<LoginOutcome>` - `POST /api/auth/login`; if the body has `challenge_token`, return `twoFactorRequired` **without** calling `/me` or dispatching; otherwise hydrate `/me` and return `authenticated` (today's behavior).
  - `verifyTwoFactor(challengeToken, factor): Promise<void>` where `factor` is `{ code: string } | { recoveryCode: string }` - `POST /api/auth/login/verify-2fa` with `{challenge_token, code}` or `{challenge_token, recovery_code}`; on `200` hydrate `/me` and dispatch `AUTHENTICATED` (same path as `login`/`switchTenant`).
- **Interfaces**: extend `AuthContextValue`.

### `LoginTwoFactorStep` (new)

- **Purpose**: Presentational verification step - the code/recovery input, method toggle, inline error, submit and back.
- **Location**: `web/src/features/auth/LoginTwoFactorStep.tsx`
- **Interfaces**: `function LoginTwoFactorStep({ submitting, error, onSubmit, onBack }): JSX.Element`, where `onSubmit(factor: { code: string } | { recoveryCode: string })`.
- **Dependencies**: `Field`, `Button`, `useTranslation`.
- **Reuses**: `LoginPage`'s field/button/error styling; a segmented toggle for the two methods (no new dependency).

### `LoginPage` (modified)

- **Purpose**: Drive the two-step flow and navigation.
- **Location**: `web/src/features/auth/LoginPage.tsx`
- **Change**: `useState<"credentials" | "twoFactor">("credentials")` plus `challengeToken`; `handleSubmit` branches on `login()`'s outcome (`twoFactorRequired` -> store token + switch step); a `handleVerify(factor)` calls `verifyTwoFactor` then `navigate("/")`; "Voltar" resets step and drops the token. Credentials errors keep today's behavior.
- **Reuses**: existing layout, `EyeIcon`, `ApiError` branching.

### `test/msw/handlers.ts` (modified)

- **Purpose**: Mirror the real login 2FA contract for tests.
- **Change**: the login handler branches on the mock 2FA state (`setTwoFactorEnabled`): enabled -> `200 {challenge_token}` and no session; else today's `{token}`. Add `POST /api/auth/login/verify-2fa`: accept the fixed TOTP code (`mswValidTotpCode`) or a seeded recovery code, establish the session (`sessionAdminId` + `currentSessionId`) exactly like login, else `401 {"error":"invalid or expired verification"}`. Export a seed helper for recovery codes.

---

## Data Models

### Frontend (`web/src/auth/AuthProvider.tsx`)

```typescript
export type LoginOutcome =
  | { kind: "authenticated" }
  | { kind: "twoFactorRequired"; challengeToken: string };

export type TwoFactorFactor = { code: string } | { recoveryCode: string };

// AuthContextValue additions
login: (email: string, password: string) => Promise<LoginOutcome>;
verifyTwoFactor: (challengeToken: string, factor: TwoFactorFactor) => Promise<void>;
```

### Backend contracts (existing, exact shapes)

| Endpoint | Request | Success | Failure |
| --- | --- | --- | --- |
| `POST /api/auth/login` | `{email, password}` | `200 {token}` (cookie) or `200 {challenge_token}` (no cookie, 2FA) | `401` generic; `403` no tenant access |
| `POST /api/auth/login/verify-2fa` | `{challenge_token, code}` or `{challenge_token, recovery_code}` | `200 {token}` + session cookie | `401 {"error":"invalid or expired verification"}` |

---

## Error Handling Strategy

| Error Scenario | Handling | User Impact |
| --- | --- | --- |
| Wrong email/password | Existing inline `ApiError.message` | Stays on credentials |
| Wrong/expired code or challenge | Inline `login.twoFactor.invalid`, keep the step | Retry or "Voltar" |
| Wrong/used recovery code | Same inline error, keep the step | Retry or toggle method |
| `500`/network on verify | Generic error (`login.twoFactor.generic`) | Retry |
| `429` rate limited | The API's message shown via the generic branch | Retry later |
| Non-2FA login | Unchanged | Direct sign-in |
| Session never established before verify | No cookie exists until `verify-2fa` 200 | Lockout risk removed |

---

## Risks & Concerns

| Concern | Location (file:line) | Impact | Mitigation |
| --- | --- | --- | --- |
| Challenge token leaks into URL/storage | `LoginPage.tsx` state | Token replay from history/refresh | Token stored only in component state (LOGIN2FA-09); no route, no storage |
| `login()` signature change breaks other callers | `AuthProvider.tsx:73` | Compile errors | Only `LoginPage` consumes `login()` in production (grep confirmed); update its call site and the provider test probe |
| MSW login must not set a session on the challenge branch | `handlers.ts` login handler | Tests would pass while the real flow is broken | Handler sets `sessionAdminId` only on the success/verify path; a test asserts no admin after a challenge |
| Expired challenge looks like a wrong code | `verify-2fa` generic 401 | Confusing copy | Copy says "invalid or expired"; "Voltar" is always available |
| Recovery codes are single-use | `handlers.ts` seed | Reused code must fail | Handler consumes the seeded code (removes it), mirroring `ConsumeRecoveryCode` |

---

## Technology Decisions

| Decision | Choice | Rationale |
| --- | --- | --- |
| No new dependency | Segmented toggle + `Field` | The step is two methods over one input; a component library is overkill. |
| Typed outcome over exception | `LoginOutcome` union | Normal control flow stays in `try/catch`-free code; simpler to assert in tests. |
| Verify hydrates via `/me` | Reuse the existing hydrate path | One identity source; identical to `login`/`switchTenant`, so no tenant-selection divergence. |
| No client expiry timer | Backend 401 is authoritative | Avoids clock drift and a second source of truth. |

No new AD is required: every decision is local to the feature and consistent with AD-004 (no session/challenge state in client storage) and the existing auth patterns.
