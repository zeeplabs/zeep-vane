# Meu Perfil Page Specification

**Scope: Large (frontend-led).** Three cards, an enrollment drawer with a multi-step state machine, two new frontend modules, one route + user-menu entry point, a small backend addition (`two_factor_enabled` on `GET /api/auth/me`) and a shared-component change (session revoke confirmation). Design and Tasks phases run before Execute; the backend addition is a single narrow task.

## Problem Statement

The redesigned Meu Perfil screen (`handoff-new-layout/Meu Perfil.dc.html`, tracked in `.specs/features/new-layout-migration/gap-analysis.md` §10) is the last screen from the layout migration whose backend capabilities already exist but whose page and UIs were never built. `profile-self-service` delivered `PATCH /api/auth/me` and `POST /api/auth/change-password`, `user-sessions` delivered the per-device sessions API plus a self-contained `<SessionsSection/>`, and `auth-2fa-totp` delivered the full TOTP backend — but all three were explicitly backend-only, so today a user has no screen to change their name or password, no way to enroll in 2FA, no page hosting the sessions list, and `GET /api/auth/me` carries no 2FA status for a security card to render. This spec adds the Meu Perfil page and the missing UIs, and exposes the 2FA status the page needs.

## Goals

- [ ] An authenticated user of any role can reach `/profile` from the topbar user menu and sees Informações pessoais, Segurança and Sessões ativas.
- [ ] The user can change their own display name and their own password (proving the current one), with inline validation and success feedback.
- [ ] The user can enroll in TOTP 2FA (QR + manual secret, code verification, one-time recovery codes) and disable it by re-proving their password.
- [ ] The user can list their active sessions and revoke a non-current one behind a confirmation dialog.
- [ ] `GET /api/auth/me` reports `two_factor_enabled` so the security card can render its state.

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
| --- | --- |
| Notifications card (incident opened/resolved, weekly digest toggles) | Its backend (`notification-preferences`) is specified but not implemented; user decision 2026-09-11 was to keep this page independent and add the card once that backend ships. |
| The 2FA verification step on the login screen | User decision 2026-09-11: the login-time `{challenge_token}` step is auth-flow work, specified separately. Enrolling 2FA from this page does not by itself change login until that spec ships. |
| Email address change | `profile-self-service` already excluded it (no re-verification mechanism; account-takeover risk). |
| Avatar upload | No per-user file storage exists (`Tenant.LogoData` is company branding); the mock's avatar slot is rendered as initials. |
| Recovery-code regeneration / re-viewing after enrollment | Not in `auth-2fa-totp`'s scope; codes are shown exactly once here. |
| A sidebar navigation item for Meu Perfil | The mock reaches the screen through the topbar user menu, not the sidebar. |
| Company settings, billing, other users' accounts | Not part of this screen; self-only by design. |

---

## Assumptions & Open Questions

Every ambiguity is resolved or recorded here - nothing is left silently unclear.

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Page scope | Shell + Informações pessoais + Segurança (password + 2FA) + Sessões ativas | Backend for these three areas is done; Notificações is intentionally deferred (see Out of Scope). | y - user decision 2026-09-11 |
| Route and entry point | Route `/profile`, reachable from a new "Meu Perfil" link in the topbar user menu (`AvatarMenu`); no sidebar item | Matches the mock's user menu; the existing `/settings` link and its `owner` guard are left untouched. | y - matches mock, no objection raised |
| Frontend module structure | Two modules: `web/src/features/profile/` (page, Informações pessoais, Segurança/password, hooks) and `web/src/features/two-factor/` (2FA card, enrollment drawer, disable dialog, hooks) | Keeps the multi-step security flow isolated and testable, mirroring how `features/sessions/` is its own self-contained module; the page composes both and imports `<SessionsSection/>`. | y - user decision 2026-09-11 |
| 2FA status source | Add a boolean `two_factor_enabled` to `GET /api/auth/me` (read from the existing `two_factor_secrets.enabled_at`) | No status endpoint exists today and the page needs the state to decide enable vs. disable; reusing `/me` avoids a new round-trip and keeps one identity payload. | y - user decision 2026-09-11 |
| 2FA enrollment UX | A `<Drawer>` with three steps: (1) scan - QR rendered from the `otpauth://` URI plus the raw secret for manual entry; (2) verify - 6-digit code; (3) recovery codes - the 10 codes, shown once, with copy and download | The backend returns the codes only in the `confirm` response and the mock has no such screen, so a dedicated one-time step is required or the codes are lost. | y - user decision 2026-09-11 |
| QR rendering | Add the `qrcode.react` dependency (MIT) and render the `otpauth://` URI client-side | The backend returns a URI, not an image, and no QR library exists in `web/` today; a well-maintained component avoids hand-rolled QR encoding. | y - agent default, no objection raised |
| 2FA disable confirmation | A dialog requiring the current password before `POST /api/auth/2fa/disable` | Matches the backend contract (`auth-2fa-totp` requires `current_password`) and closes the hijacked-session-disables-2FA gap. | y - backend contract |
| Identity refresh after mutations | Expose `refreshAdmin()` on `AuthProvider` (re-hydrates `GET /api/auth/me`) and call it after a successful name change or 2FA enable/disable | The name and 2FA status are displayed in the page and the shell; updating the single context source avoids a stale name until reload. | y - agent default, no objection raised |
| Session revoke confirmation | Add a "Encerrar sessão?" confirmation dialog to `<SessionsSection/>` before sending `DELETE /api/auth/sessions/{id}` | User decision 2026-09-11 to match the mock, superseding the component's earlier no-confirmation choice. | y - user decision 2026-09-11 |
| Informações pessoais content | Name (editable, `PATCH /api/auth/me`), email (read-only, from `/me`), avatar as generated initials | Email change and avatar upload are out of scope (above); initials need no storage. | y - agent default, no objection raised |
| Password validation | Client-side confirmation match; policy and current-password checks are the backend's existing rules | Avoids a second password policy in the frontend; the confirm field is pure UX. | y - agent default, no objection raised |
| RBAC | `anyRole`, self only - the page and every endpoint it calls act on the caller's own account | Same posture as `profile-self-service`/`user-sessions`/`auth-2fa-totp`. | y - backend contract |
| Feedback mechanism | `sonner` toasts for success and inline field/dialog errors for failures | Matches the existing sessions/admins patterns. | y - agent default, no objection raised |
| Post-password-change session | The current session stays authenticated after a successful change; no forced logout | Matches `profile-self-service`'s documented behavior (only other sessions are revoked). | y - backend contract |

**Open questions:** none - all resolved or logged above.

---

## User Stories

### P1: User reaches the Meu Perfil page ⭐ MVP

**User Story**: As an authenticated user of any role, I want to open Meu Perfil from the user menu, so I can manage my own account.

**Why P1**: Nothing else in this spec is reachable without the page and its entry point.

**Acceptance Criteria**:

1. WHEN an authenticated user navigates to `/profile` THEN the page SHALL render the "Informações pessoais", "Segurança" and "Sessões ativas" cards.
2. The user menu SHALL render a "Meu Perfil" link that navigates to `/profile` for every authenticated user regardless of role.
3. IF an unauthenticated visitor requests `/profile` THEN the system SHALL redirect to the login screen through the existing `RequireAuth` guard.

**Independent Test**: Log in, open the user menu, click "Meu Perfil", confirm the three cards render; log out and request `/profile` directly, confirm the redirect to `/login`.

---

### P1: User updates their own display name ⭐ MVP

**User Story**: As an authenticated user, I want to change my display name, so teammates see the right name in the sidebar and timeline.

**Why P1**: The Informações pessoais card has no purpose without it.

**Acceptance Criteria**:

1. WHEN the user submits a non-empty name THEN the page SHALL send `PATCH /api/auth/me` with `{name}` and, on `200`, SHALL refresh the authenticated identity and show a success toast.
2. WHEN the update succeeds THEN the page SHALL display the new name without a full page reload.
3. IF the user submits an empty or whitespace-only name THEN the page SHALL block the request and SHALL show an inline validation error.

**Independent Test**: Type a new name, submit, confirm the card and the topbar show it and a toast appears; submit an empty name and confirm no request is sent and an inline error shows.

---

### P1: User changes their own password ⭐ MVP

**User Story**: As an authenticated user, I want to change my password by entering my current one, so I can rotate my credential without the email-reset flow.

**Why P1**: The Segurança card's password form is part of the screen's core value.

**Acceptance Criteria**:

1. WHEN the user submits current, new and confirmation fields with new equal to confirmation THEN the page SHALL send `POST /api/auth/change-password` with `{current_password,new_password}` and, on `200`, SHALL clear the form and show a success toast.
2. IF the confirmation does not equal the new password THEN the page SHALL block the request and SHALL show an inline mismatch error.
3. IF the backend responds `401` THEN the page SHALL show an inline "current password incorrect" error and SHALL leave the stored password unchanged.
4. IF the backend responds `422` THEN the page SHALL show an inline password-policy error.
5. WHEN the password change succeeds THEN the current session SHALL remain authenticated and the user SHALL NOT be forced to log in again.

**Independent Test**: Change the password with the correct current value and confirm the toast and that the session still works; repeat with a wrong current password and confirm the inline `401` error; repeat with a mismatched confirmation and confirm no request is sent.

---

### P1: User manages two-factor authentication ⭐ MVP

**User Story**: As an authenticated user, I want to enroll in or disable TOTP two-factor authentication, so my account is protected by a second factor.

**Why P1**: 2FA enrollment is the reason the Segurança card exists and has no UI today.

**Acceptance Criteria**:

1. WHEN the page loads THEN the 2FA card SHALL render its enabled/disabled state from `two_factor_enabled` in `GET /api/auth/me`.
2. WHEN the user starts enrollment THEN the page SHALL call `POST /api/auth/2fa/enroll` and SHALL render the returned `otpauth://` URI as a QR code together with the raw secret for manual entry.
3. WHEN the user submits a code THEN the page SHALL call `POST /api/auth/2fa/confirm` with `{code}` and, on `200`, SHALL display the returned recovery codes exactly once in a dedicated step.
4. IF the confirm code is invalid THEN the page SHALL show an inline error and SHALL keep the drawer on the verify step.
5. WHEN the user dismisses the recovery-codes step THEN the page SHALL refresh the authenticated identity so the card shows 2FA as enabled.
6. WHEN the user disables 2FA with the correct current password THEN the page SHALL call `POST /api/auth/2fa/disable` with `{current_password}` and, on `200`, SHALL refresh so the card shows 2FA as disabled.
7. IF the disable password is incorrect THEN the page SHALL show an inline error and SHALL leave 2FA enabled.

**Independent Test**: With MSW, enroll, scan the rendered QR/secret, submit a valid code, confirm the 10 recovery codes appear once and the card flips to enabled; submit an invalid code and confirm the inline error; disable with a wrong password (inline error) then the right one (card flips to disabled).

---

### P1: User reviews and revokes active sessions ⭐ MVP

**User Story**: As an authenticated user, I want to see my active sessions and end one that I don't recognize, so I can cut off a device I no longer trust.

**Why P1**: The Sessões ativas card is part of the screen and its confirmation dialog is the mock's behavior.

**Acceptance Criteria**:

1. WHEN the page loads THEN the "Sessões ativas" card SHALL list the caller's active sessions through the existing `<SessionsSection/>` component.
2. WHEN the user clicks "Encerrar" on a non-current session THEN the page SHALL show a confirmation dialog before sending `DELETE /api/auth/sessions/{id}`.
3. WHEN the user confirms the dialog THEN the page SHALL send the `DELETE` request and, on success, SHALL remove the revoked row from the list.
4. IF the user cancels the dialog THEN the page SHALL NOT send the `DELETE` request and the session SHALL remain listed.

**Independent Test**: Click "Encerrar" on a non-current session, cancel, confirm the row is still there and no request was sent; click again and confirm, then confirm the row disappears.

---

### P1: GET /api/auth/me reports 2FA status ⭐ MVP

**User Story**: As the frontend, I want the authenticated identity payload to tell me whether 2FA is enabled, so the security card can choose the right state.

**Why P1**: The 2FA card cannot render without this field.

**Acceptance Criteria**:

1. WHEN an authenticated user calls `GET /api/auth/me` THEN the response SHALL include a boolean `two_factor_enabled` that is `true` only when the user has a confirmed TOTP enrollment.

**Independent Test**: Call `/api/auth/me` for a user with and without a confirmed enrollment and confirm the boolean is `false` and `true` respectively.

---

## Edge Cases

- IF the name update response is `422` (backend rejects an empty name) THEN the page SHALL show the same inline validation error it shows for a client-side empty value.
- IF the enrollment call returns `409` (already enrolled) THEN the page SHALL close the drawer, refresh the identity and show an informational message instead of an error toast.
- IF the sessions list request fails THEN the card SHALL show a load error without affecting the other two cards (each card fails independently).
- IF the recovery-codes step is the user's only chance and they close it THEN the page SHALL warn that the codes will not be shown again before closing.
- WHEN the user's browser locale is English THEN every new user-facing string SHALL render in English through `react-i18next`, and pt-BR otherwise.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| PROFPAGE-01 | P1: Reach the Meu Perfil page (route + cards) | Design | Done |
| PROFPAGE-02 | P1: Reach the Meu Perfil page (user-menu link) | Design | Done |
| PROFPAGE-03 | P1: Reach the Meu Perfil page (auth guard) | Design | Done |
| PROFPAGE-04 | P1: Update display name (success) | Design | Done |
| PROFPAGE-05 | P1: Update display name (no reload) | Design | Done |
| PROFPAGE-06 | P1: Update display name (empty validation) | Design | Done |
| PROFPAGE-07 | P1: Change password (success) | Design | Done |
| PROFPAGE-08 | P1: Change password (confirmation mismatch) | Design | Done |
| PROFPAGE-09 | P1: Change password (wrong current) | Design | Done |
| PROFPAGE-10 | P1: Change password (policy 422) | Design | Done |
| PROFPAGE-11 | P1: Change password (session survives) | Design | Done |
| PROFPAGE-12 | P1: Manage 2FA (status rendering) | Design | Done |
| PROFPAGE-13 | P1: Manage 2FA (enroll QR + secret) | Design | Done |
| PROFPAGE-14 | P1: Manage 2FA (confirm + recovery codes once) | Design | Done |
| PROFPAGE-15 | P1: Manage 2FA (invalid code) | Design | Done |
| PROFPAGE-16 | P1: Manage 2FA (enable refresh) | Design | Done |
| PROFPAGE-17 | P1: Manage 2FA (disable success) | Design | Done |
| PROFPAGE-18 | P1: Manage 2FA (disable wrong password) | Design | Done |
| PROFPAGE-19 | P1: Sessions (list) | Design | Done |
| PROFPAGE-20 | P1: Sessions (confirmation dialog) | Design | Done |
| PROFPAGE-21 | P1: Sessions (confirm revokes and removes row) | Design | Done |
| PROFPAGE-22 | P1: Sessions (cancel keeps session) | Design | Done |
| PROFPAGE-23 | P1: GET /api/auth/me reports 2FA status | Design | Done |

**ID format:** `[CATEGORY]-[NUMBER]` (e.g., `AUTH-01`, `CART-03`, `NOTIF-02`)

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 23 total, 0 mapped to tasks, 23 unmapped ⚠️ (Large scope - the Design and Tasks phases follow before Execute)

---

## Success Criteria

- [ ] `/profile` is reachable from the user menu for every role and renders the three cards.
- [ ] Name and password changes work end-to-end against the existing endpoints, with inline validation and success feedback.
- [ ] A user can enroll in 2FA (QR + secret + code + one-time recovery codes) and disable it with their password.
- [ ] Active sessions are listed and a revoke requires an explicit confirmation.
- [ ] `GET /api/auth/me` returns `two_factor_enabled` and the security card reflects it.
- [ ] The Notificações card, the login-time 2FA step, email change and avatar upload are not built in this feature.
