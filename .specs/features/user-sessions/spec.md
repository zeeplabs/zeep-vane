# Per-Device User Sessions Specification

**Scope: Large.** Changes the session-token format and `RequireAuth`'s hot path — flagged as higher-risk under `AGENTS.md` §7. A formal Design phase runs before Execute, same as `auth-2fa-totp`.

## Problem Statement

The redesigned Meu Perfil screen (`handoff-new-layout/Meu Perfil.dc.html`) shows a "Sessões ativas" list — one row per device, with location, last-active time, and an "Encerrar" button to revoke that one device individually. Today `vane_session` is a stateless signed JWT (`internal/auth/session.go`) with no server-side record at all; the only revocation mechanism is `User.SessionsRevokedAt`, a single timestamp that invalidates **every** session for a user at once. There is no way to list "your devices" (no rows exist to list) or to end one without ending all of them.

## Goals

- [ ] Every issued session token corresponds to a real, queryable row: which user, what device (`User-Agent`), what IP, when created, when last used.
- [ ] A user can list their own active sessions and identify which one is "this session" (the one making the request).
- [ ] A user can revoke one specific other session, which is rejected on its very next authenticated request — without touching any of their other sessions.
- [ ] Logging out revokes the current session's row, not just the browser's cookie (closes the existing gap where a copied, still-unexpired token remains valid after "logout").

## Out of Scope

| Feature | Reason |
| --- | --- |
| Geolocation ("São Paulo, BR" in the mock) | No IP-geolocation service or library exists anywhere in this codebase, and adding one is a new external dependency (or a paid API) unrelated to the actual security feature (per-device revocation). This spec stores and returns the raw IP; city/country display is deferred until a geolocation approach is decided. |
| Human-readable device labels ("Chrome · macOS" in the mock) beyond the raw `User-Agent` string | Full `User-Agent` parsing is its own maintenance surface (browser/OS detection libraries need updates as new UA strings appear). This spec stores and returns the raw `User-Agent` header; a friendlier label is a frontend-only formatting concern that can improve independently without a backend contract change. |
| Concurrent-session limits (e.g. "max 3 devices") | Not requested anywhere in the handoff; would need a product decision on the limit itself. |
| Admin visibility into another user's sessions | Mock only shows a user's own sessions; no screen in the handoff shows an admin session-management view for other accounts. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Session persistence | New `sessions` table: `id` (UUID, PK), `user_id` (FK), `user_agent` (raw header, nullable), `ip` (nullable — some deployments strip it), `created_at`, `last_seen_at`, `revoked_at` (nullable) | Matches the `pglock`/`status_intervals` pattern of storing exactly the columns a real query needs, nothing speculative. | y — per prior decision (round 1 of this cycle: "Sessão real no banco") |
| Token format change | `sessionClaims` gains a `sid` claim (the session row's UUID). `IssueSessionWithTenant` becomes responsible for both creating the `sessions` row and signing a token carrying its ID — every one of the 4 existing call sites (`AuthHandler.Login`, `AuthHandler.SwitchTenant`, `AdminsHandler.AcceptInvite`, `BootstrapHandler.Create`) gets a session row created alongside the token they already issue. | A session token that doesn't identify *which* session row it is can't be individually revoked — the `sid` claim is the entire mechanism this feature adds. | y — agent default, no objection raised |
| `RequireAuth` enforcement | After the existing `users.GetByID` + `SessionsRevokedAt` check, `RequireAuth` additionally loads the `sessions` row by `sid` and rejects (`401`) if it does not exist or `revoked_at` is set. On success it updates `last_seen_at` — throttled to at most once per 5 minutes per session (a write on every single authenticated request would add unnecessary load for no product value, since the UI only needs a coarse "last active" signal) | One additional indexed lookup per request (by primary key) is cheap; the throttle keeps write volume proportional to real usage, not request volume. | y — agent default, no objection raised |
| `SwitchTenant`'s effect on the session row | Switching the active tenant re-signs a token with the **same** `sid` (same session row, only the `tid` claim inside the JWT changes) — it does not create a new session row | Switching tenants mid-session is the same physical device/browser session continuing, not a new login; creating a second row would make that device show up twice in the list. | y — agent default, no objection raised |
| Revoking your own *current* session | `DELETE /api/auth/sessions/{id}` rejects with `409` if `{id}` is the caller's own current session (matches the mock's `canEnd: !sess.current` — "Encerrar" is never shown for the current row) | Ending your own current session through this endpoint would leave the caller's own next request rejected with no clear signal why; `Logout` already exists as the correct action for ending your own current session. | y — agent default, no objection raised |
| `Logout`'s effect on the session row | `AuthHandler.Logout` now also sets `revoked_at` on the current session's row (resolved via the `sid` claim in the token being logged out), in addition to clearing the cookie as it does today | Closes an existing latent gap: today, a copied/leaked token remains valid until its 24h `SessionTTL` expires even after the browser "logs out". This is a direct, minimal-risk improvement enabled by the new session row, not scope creep — it's the same action already being taken (ending this session), just now actually effective server-side. | y — agent default, no objection raised |
| Ownership check on revoke | `DELETE /api/auth/sessions/{id}` SHALL 404 (not 403) if `{id}` doesn't exist or belongs to a different user — never reveal whether a given session ID exists for someone else | Matches the codebase's existing anti-enumeration posture (e.g. `genericLoginErrorBody`). | y — agent default, no objection raised |
| RBAC | `anyRole`, self only, on `GET /api/auth/sessions` and `DELETE /api/auth/sessions/{id}` | Personal security setting, not admin-managed — same posture as `profile-self-service` and `auth-2fa-totp`. | y — agent default, no objection raised |
| Stale row cleanup | Rows past their token's `SessionTTL` (24h) with no `revoked_at` are left in place, not actively deleted, but excluded from `GET /api/auth/sessions`'s listing (`created_at` older than `SessionTTL` and never revoked reads the same as "already invalid") | No existing background-cleanup job for anything similar (`status_intervals` has one — `DeleteClosedBefore` — but adding a scheduled deletion job for this table is unnecessary complexity when a `WHERE` clause on read achieves the same visible behavior). | y — agent default, no objection raised |

**Open questions:** none — all resolved or logged above.

---

## User Stories

### P1: Every login creates a real, identifiable session row ⭐ MVP

**User Story**: As the system, I need every issued session token to correspond to a database row, so a user's active sessions can be listed and revoked individually.

**Why P1**: Every other story in this spec depends on this existing first.

**Acceptance Criteria**:

1. WHEN `AuthHandler.Login`, `AuthHandler.SwitchTenant`, `AdminsHandler.AcceptInvite`, or `BootstrapHandler.Create` issues a session token THEN the system SHALL create a corresponding `sessions` row (except `SwitchTenant`, which reuses the existing row per its own AC below) with `user_agent` and `ip` captured from the issuing request, and the issued token SHALL carry that row's ID as its `sid` claim.
2. WHEN `SwitchTenant` re-issues a token for an already-authenticated session THEN the system SHALL reuse the existing session row's ID as the new token's `sid` — it SHALL NOT create a second row.
3. WHEN any protected route is called with a token whose `sid` does not match an existing, non-revoked `sessions` row THEN `RequireAuth` SHALL respond `401`, exactly as it already does for a token issued before `SessionsRevokedAt`.
4. WHILE a session is used for authenticated requests THE system SHALL update that row's `last_seen_at`, throttled to at most once per 5 minutes of wall-clock time per row (not on every single request).

**Independent Test**: Log in, confirm a `sessions` row exists with matching `user_agent`/`ip` and the token's `sid`; call `SwitchTenant` and confirm the row count for that user is unchanged (same `sid`, new `tid` claim only); manually set that row's `revoked_at`, retry any protected request with the old token, confirm `401`.

---

### P1: User lists their own active sessions ⭐ MVP

**User Story**: As any authenticated user, I want to see every device currently logged into my account, so I can spot one I don't recognize.

**Why P1**: This is the entire "Sessões ativas" list in the mock.

**Acceptance Criteria**:

1. WHEN an authenticated user calls `GET /api/auth/sessions` THEN the system SHALL respond `200` with every non-revoked, non-expired `sessions` row for that user, each including `id`, `user_agent`, `ip`, `created_at`, `last_seen_at`, and a boolean marking whether it is the session making this very request.
2. The system SHALL include the caller's own current session in this list (so the mock's "Sessão atual" row has something to render), marked distinctly from the others.
3. Exactly one row in the response SHALL be marked as current — the one whose `sid` matches the request's own token.

**Independent Test**: Log in from two different `User-Agent` values for the same user (two logins), call `GET /api/auth/sessions` with the first token, confirm two rows are returned and exactly the one matching the calling token is marked current.

---

### P1: User revokes one specific other session ⭐ MVP

**User Story**: As any authenticated user, I want to end one device's access without logging myself out everywhere, so I can kick out an old laptop I no longer use while staying logged in here.

**Why P1**: This is the mock's "Encerrar" action — the actual point of a per-device (not global) session model.

**Acceptance Criteria**:

1. WHEN an authenticated user calls `DELETE /api/auth/sessions/{id}` for a session row belonging to them, other than their own current one THEN the system SHALL set that row's `revoked_at` and respond `200`.
2. WHEN a request is subsequently made using the revoked session's token THEN `RequireAuth` SHALL reject it with `401`.
3. IF `{id}` is the caller's own current session THEN the system SHALL respond `409` and SHALL NOT revoke it.
4. IF `{id}` does not exist, or belongs to a different user THEN the system SHALL respond `404`.
5. The system SHALL NOT affect any other session belonging to the same user when revoking one specific `{id}`.

**Independent Test**: Log in twice as the same user (sessions A and B), call `DELETE /api/auth/sessions/{B.id}` using token A, confirm `200`; retry any protected route with token B and confirm `401`; confirm token A still works. Repeat targeting A's own ID using token A and confirm `409`.

---

### P2: Logout revokes the current session server-side

**User Story**: As any authenticated user, I want "Sair" to actually invalidate my session on the server, so a copy of my token (e.g. from a shared or compromised device) stops working the moment I log out.

**Why P2**: A real security improvement, but not required for the primary list/revoke flows the mock's screen needs.

**Acceptance Criteria**:

1. WHEN an authenticated user calls `POST /api/auth/logout` THEN the system SHALL set `revoked_at` on the session row matching the request token's `sid`, in addition to clearing the `vane_session` cookie as it does today.
2. WHEN the same (now-logged-out) token is used for any subsequent protected request THEN the system SHALL respond `401`.

**Independent Test**: Log in, copy the raw token before logging out, call `Logout`, then replay the copied token directly (bypassing the cleared cookie) against any protected route, confirm `401` — today this would still succeed until the 24h TTL expired.

---

## Edge Cases

- IF the request's `User-Agent` header is absent (e.g. a non-browser API client) THEN the system SHALL store `user_agent` as `NULL` rather than rejecting the login.
- IF the client IP cannot be determined (matches this codebase's existing IP-sourcing rule: connection `RemoteAddr` only, never `X-Forwarded-For`) THEN the system SHALL store `ip` as `NULL` rather than rejecting the login.
- WHEN `GET /api/auth/sessions` is called and the user has no other sessions besides the current one THEN the system SHALL return a single-item list (just the current session) — not an error.
- WHEN a session row's `created_at` is older than `SessionTTL` (24h) and `revoked_at` is still unset THEN `GET /api/auth/sessions` SHALL exclude it from the listing (the token backing it is already expired and unusable, whether or not `RequireAuth` ever actually rejected it).

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| SESS-01 | P1: Every login creates a session row | - | ✅ Verified |
| SESS-02 | P1: SwitchTenant reuses the row | - | ✅ Verified |
| SESS-03 | P1: RequireAuth rejects a revoked/missing sid | - | ✅ Verified |
| SESS-04 | P1: last_seen_at throttled update | - | ✅ Verified |
| SESS-05 | P1: List own sessions | - | ✅ Verified |
| SESS-06 | P1: List marks exactly one as current | - | ✅ Verified |
| SESS-07 | P1: Revoke one other session | - | ✅ Verified |
| SESS-08 | P1: Revoked session rejected on next use | - | ✅ Verified |
| SESS-09 | P1: Cannot revoke own current session (409) | - | ✅ Verified |
| SESS-10 | P1: Revoke unknown/other-user session (404) | - | ✅ Verified |
| SESS-11 | P2: Logout revokes current session server-side | - | ✅ Verified |

**Coverage:** 11 total, 11 verified ✅ (Verifier PASS: `.specs/features/user-sessions/validation.md`, 8/8 mutants killed)

---

## Success Criteria

- [ ] Every session-issuing endpoint creates or reuses a real `sessions` row carried via a new `sid` claim.
- [ ] `RequireAuth` rejects any token whose session row is missing or revoked.
- [ ] A user can list their sessions with exactly one marked current, and revoke any one other session without affecting the rest.
- [ ] `Logout` actually invalidates the current session server-side, not just the cookie.
- [ ] No geolocation or User-Agent-parsing dependency is introduced.
