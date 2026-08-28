# Accept Invite Page Specification

## Problem Statement

`POST /api/admins/invite/{token}/accept` has existed and been usable via direct API call since `admin-dashboard`, and the admin-invite email (`admin-invite-resend-cancel`) already sends a working `AcceptURL` pointing at `/accept-invite/:token` - but no such frontend route exists. An invited admin who clicks the link in their email today lands on the SPA's client-side-routing fallback with no way to actually accept the invite short of crafting a raw `curl` request. This feature closes that last gap: a real page that takes the invitee's password and calls the existing backend endpoint.

## Goals

- [ ] An invited admin can open the emailed link, set a password, and land on an active session with no manual API call
- [ ] Every documented failure mode of `AcceptInvite` (weak password, invalid/expired/used token) surfaces as a clear, actionable message on the page - never a silent failure or a raw JSON blob
- [ ] `AcceptInvite` sets a session cookie on success (mirrors `BootstrapHandler.Create`), so login-after-accept requires no extra step

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
| ------- | ------ |
| Changing `AcceptInvite`'s request/response body shape | `{password}` in, `{email,role}` out (plus the new cookie) stays as-is; only the missing `SetCookie` call is added |
| A "resend invite" action from this page (e.g. "link expired, send me a new one") | The invitee has no session and isn't an owner - only an owner can resend (`admin-invite-resend-cancel`, owner-only). Out of scope; the error message tells the invitee to contact their admin instead |
| Redirecting an already-authenticated admin away from `/accept-invite/:token` | Confirmed with the user: no guard, matches the existing precedent of `/reset-password` (no route in this app redirects an active session away from a public route) |
| Building the still-missing `/reset-password` confirm page (`POST /api/auth/password-reset/confirm`) | Separate backend endpoint, separate flow, no product ask to build it now - `PasswordResetRequestPage` remains a stub as-is |
| Password strength meter / client-side complexity rules beyond length | Server validates 8-72 chars only (NIST-aligned, `auth.ValidatePassword`); no product ask for a strength indicator |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --------------------- | --------------- | --------- | ---------- |
| Post-accept session | `AcceptInvite` gains a `SetCookie` call identical to `BootstrapHandler.Create`'s (`sessionCookie(token, ...)`), issued right after `CreateWithRole` succeeds; the frontend does a full `window.location.assign("/")` after 201 (same pattern as `BootstrapPage`, not React Router navigation), so `AuthProvider`'s boot checks re-run against the now-cookied session | User confirmed auto-login over redirect-to-login; reusing `BootstrapPage`'s exact reload pattern (not `AuthProvider.login()`) avoids inventing a second "create session" code path | y |
| Already-authenticated admin opening the link | No guard - the route renders normally regardless of session state, matching `/reset-password`'s existing precedent (no route in this app redirects an active session away from a public route) | User confirmed; adding a new guard type for one route wasn't worth the precedent-free complexity | y |
| Password confirmation field | Page includes a "confirm password" field that must match before submit is enabled, client-side only (same UX as `BootstrapPage`) - the server only ever sees one `password` field | Matches the one existing precedent for "create an account with a password" in this codebase (`BootstrapPage`); a single unconfirmed password field on an irreversible account-creation action is worse UX with no compensating simplicity gain |
| Client-side password length validation | None - only `required` + confirm-match are client-side; the 8-72 char rule is left to the server's 422 response, displayed verbatim | Matches the established pattern (`BootstrapPage` also does not duplicate the length rule client-side); avoids the two rules silently drifting apart |
| Distinguishing "expired" vs "already used" vs "never existed" token in the UI | Not distinguished - `AcceptInvite` already collapses all three into one generic 401 body (`acceptInviteErrorBody`) to avoid enumeration; the page shows one generic message for all 401s: "This invite link is invalid or has expired. Ask your admin to send a new one." | Matches the backend's deliberate anti-enumeration design (`admins.go` doc comment on `ClaimForUse`) - inventing frontend-only distinction would require a backend response-shape change explicitly out of scope here |
| Loading/error state shape | Mirrors `BootstrapPage.tsx`: a `useState` boolean for submitting, `err instanceof ApiError ? err.message : <generic fallback>` pattern, no dedicated `hooks.ts` (this page's only network call is one `apiFetch`, same as `BootstrapPage`) | Matches the simplest existing precedent for a one-shot public POST; a full `hooks.ts` module for a single mutation would be premature abstraction |

**Open questions:** none - all resolved or logged above.

---

## User Stories

### P1: Invited admin accepts their invite and lands logged in ⭐ MVP

**User Story**: As an invited admin, I want to click the link in my invite email, set a password, and immediately be in the dashboard, so that I don't need a separate login step or any manual API knowledge.

**Why P1**: This is the entire reason the feature exists - without it, the invite email's link is a dead end.

**Acceptance Criteria**:

1. WHEN the invitee submits a password (and matching confirmation) on `/accept-invite/:token` THEN the system SHALL call `POST /api/admins/invite/{token}/accept` with `{"password": "<value>"}`
2. WHEN that call responds `201` THEN the system SHALL perform a full-page navigation to `/` (`window.location.assign("/")`), relying on the response's `Set-Cookie` session header to authenticate the subsequent boot check
3. WHILE the request is in flight THEN the system SHALL disable the submit control and show a loading state, preventing a duplicate submission
4. The system SHALL never display, log, or store the raw invite `token` beyond using it as the literal URL path segment already present in the emailed link

**Independent Test**: Seed a pending invite via the mock backend, visit `/accept-invite/<its-token>`, submit a valid password, confirm the app lands on `/` and `GET /api/auth/me` (or the mock equivalent) reflects the newly created admin.

---

### P1: Password confirmation prevents typo lockout ⭐ MVP

**User Story**: As an invited admin, I want the form to catch a typo between my password and its confirmation before I submit, so that I don't accidentally set a password I can't reproduce.

**Why P1**: A one-shot account-creation action with no "forgot password for an account I never finished setting up" recovery path makes this a basic usability floor, not a nice-to-have - matches the existing `BootstrapPage` precedent.

**Acceptance Criteria**:

1. IF the password and confirmation fields don't match at submit time THEN the system SHALL block the request (never call the API) and show an inline mismatch message
2. WHEN the invitee edits either field after seeing the mismatch message THEN the system SHALL clear the message on the next submit attempt (not persist a stale error)

**Independent Test**: Type two different values into password/confirm, click submit, confirm no network call fired and the mismatch message is visible.

---

### P2: Clear, distinct error messages for every accept failure

**User Story**: As an invited admin whose invite has a problem (expired, already used, or I mistyped a weak password), I want a message that tells me what's actually wrong, so I know whether to try again or contact my admin.

**Why P2**: Meaningfully improves the experience for the failure paths, but P1's happy path is independently demoable and P2 doesn't block it.

**Acceptance Criteria**:

1. IF the backend responds `401` THEN the system SHALL show: "This invite link is invalid or has expired. Ask your admin to send a new one." (no enumeration of which of the three causes applied - see Assumptions)
2. IF the backend responds `422` THEN the system SHALL show the server's exact `error` message verbatim (covers both "password is required" and the 8-72 char rule)
3. IF the request fails for a reason other than a parsed `ApiError` (network failure, 5xx) THEN the system SHALL show a generic fallback message, never a raw error object or blank screen

**Independent Test**: Point the mock backend at each of 401/422/500 in turn, submit the form each time, confirm the exact expected message renders and the form remains usable (not stuck in a permanent loading state).

---

## Edge Cases

- IF the `:token` URL segment is empty or missing (malformed link) THEN the system SHALL still submit it as the path segment as-is, relying on the existing backend behavior for an empty token (401, same generic message) - no separate client-side "malformed link" branch, per the anti-enumeration Assumption
- IF the invitee double-clicks submit before the first request resolves THEN the system SHALL NOT send a second request (covered by P1 AC3's disabled-while-submitting state)
- WHEN the invitee navigates to `/accept-invite/:token` directly (not via the email link, e.g. a bookmark or shared URL) THEN the system SHALL render identically - the page has no server-side referrer or freshness check beyond the token's own validity

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --------------- | ------ | ------ | ------- |
| AIP-01 | P1: Invited admin accepts their invite and lands logged in | Tasks | Implementing |
| AIP-02 | P1: Invited admin accepts their invite and lands logged in | Tasks | Implementing |
| AIP-03 | P1: Invited admin accepts their invite and lands logged in | Design | Pending |
| AIP-04 | P1: Invited admin accepts their invite and lands logged in | Design | Pending |
| AIP-05 | P1: Password confirmation prevents typo lockout | Design | Pending |
| AIP-06 | P1: Password confirmation prevents typo lockout | Design | Pending |
| AIP-07 | P2: Clear, distinct error messages for every accept failure | Design | Pending |
| AIP-08 | P2: Clear, distinct error messages for every accept failure | Design | Pending |
| AIP-09 | P2: Clear, distinct error messages for every accept failure | Design | Pending |

**ID format:** `AIP-[NUMBER]`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 9 total, 0 mapped to tasks, 9 unmapped ⚠️ (expected pre-Design)

---

## Success Criteria

How we know the feature is successful:

- [ ] An invited admin can go from "clicks email link" to "sees the authenticated dashboard" with zero manual API calls and zero separate login step
- [ ] Every one of `AcceptInvite`'s documented response codes (201, 401, 422, and the underlying 500) renders a distinct, non-blank, non-raw-JSON message on the page
- [ ] Zero regressions in `AcceptInvite`'s existing backend test suite (the only backend change - `SetCookie` on success - is additive)
