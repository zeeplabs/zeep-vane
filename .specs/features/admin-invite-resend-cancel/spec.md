# Admin Invite Resend/Cancel Specification

## Problem Statement

Admin invites (`POST /api/admins`) currently never send an email - the raw token is only logged (dev-only), so the invited person has no way to receive their invite link short of an admin manually copying it out of server logs. There is also no way to resend a lost/expired invite or cancel a pending one without waiting for the 1-hour TTL. Now that `email-provider-connect` is implemented and verified, this feature closes both gaps: wire real email delivery into invite creation, and add resend/cancel actions so an owner can manage a pending invite end-to-end from the admin UI (whose `useResendInvite`/`useCancelInvite` hooks already exist unwired to any backend route or UI action).

## Goals

- [ ] `POST /api/admins` sends the admin-invite email through the active provider (falls back to best-effort, never blocks invite creation)
- [ ] Owner can resend a pending invite (new token, extended expiry, fresh email) via `POST /api/admins/invites/{id}/resend`
- [ ] Owner can cancel a pending invite via `DELETE /api/admins/invites/{id}`, immediately invalidating its token
- [ ] Expired-but-unused invites stay visible and manageable (list/resend/cancel) instead of silently disappearing

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature                                          | Reason                                                                                     |
| ------------------------------------------------ | ------------------------------------------------------------------------------------------- |
| Email templates / provider connection management  | Already built and verified in `email-provider-connect`; this feature only calls `email.Service.SendAdminInvite` |
| Configurable invite TTL                           | Fixed at 1 hour (`adminInviteTTL`) since MVP; changing it is a separate decision            |
| Notifying the invited email on cancel             | Canceling is an internal admin action; the invitee simply finds the link no longer works    |
| Bulk resend/cancel (multiple invites at once)     | No product ask for it; current UI/API is per-invite                                          |
| Distinguishing "canceled" from "accepted" in raw DB rows | Both collapse to `used_at` set (see Assumptions) - out of scope to add a new column for this |
| Frontend `/accept-invite/:token` page | No such route exists yet (`web/src/App.tsx` has no accept-invite route, unlike `/reset-password`). The email's `AcceptURL` still points at this path (backend already exposes `POST /api/admins/invite/{token}/accept`) so the link is future-proofed, but until that page ships an invitee must accept via direct API call. Tracked as a separate backlog item, not built here. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --------------------- | --------------- | --------- | ---------- |
| Expired-but-unused invites visibility | `List()` and the resend/cancel lookup include invites where `used_at IS NULL`, regardless of `expires_at` (query no longer filters `expires_at > now()`) | Otherwise an expired invite becomes unmanageable - it vanishes from the pending list and the owner must re-enter the invitee's email/role from scratch instead of just resending | y |
| Behavior with no active email provider (or send failure) | Invite/resend still creates/updates the DB row and returns its normal success status; the JSON response adds `"email_sent": bool` reflecting whether `SendAdminInvite` succeeded. A send failure is logged at `Error` level with the invite ID but never fails the HTTP request | Same non-blocking convention as the pre-existing password-reset flow; self-hosted owners may invite admins before connecting an email provider, and the invite link itself (shareable out-of-band) still works regardless of email delivery | y |
| Cancel data model | Cancel reuses `used_at` via the same atomic claim pattern as accept (`UPDATE ... WHERE id = $1 AND used_at IS NULL RETURNING ...`) - no new column | Zero-migration, matches the existing single-use-token invariant (a canceled invite must become just as un-acceptable as a used one); the audit log's `"canceled"` entry (target = invite ID) is the only place canceled vs. accepted is distinguished, which is sufficient since raw `admin_invites` rows are never surfaced to any UI as a historical record | y |
| Resend token/expiry semantics | Resend generates a brand-new random token (invalidates the old link) and resets `expires_at` to `now() + 1h`; it does not reuse the old token | Matches the existing `Invite()` convention of always minting a fresh token; keeping the old token alive after resend would mean two valid links for one invite, which nothing in the schema (`token_hash` has no uniqueness requirement across time) was designed to support |
| Resend/cancel of an invite past its original expiry | Allowed, since expired-but-unused invites remain in the manageable set (see first row) - resend on an expired invite is exactly the "fix" for that owner-facing case |
| Resend/cancel target already accepted (`used_at` already set from acceptance) | Returns 404 (`ErrNotFound` from the same atomic `WHERE used_at IS NULL` clause) - indistinguishable from "invite id doesn't exist" | Accepted invites are gone from `List()` already (only `admins` rows represent them going forward); no legitimate caller has an ID for an accepted invite except by guessing, so a generic 404 is fine and avoids leaking acceptance state to a caller who only has a stale ID |
| Concurrent resend/cancel or cancel/cancel race on the same invite | The atomic `UPDATE ... WHERE used_at IS NULL RETURNING`/`... WHERE used_at IS NULL` pattern (same as `ClaimForUse`) ensures only one of two concurrent requests succeeds; the loser gets 404 | Prevents the same TOCTOU class of bug already fixed for accept (see `ClaimForUse` doc comment in `admin_invites.go`) - this holds whenever one side of the race *consumes* the invite (sets `used_at`) |
| Concurrent resend/resend race on the same invite | **Not** mutually exclusive by design - `Refresh` never sets `used_at`, so two concurrent resends can both succeed against the same row. Accepted: no data corruption results, just two token-refresh + email-send cycles instead of one - whichever resend's `UPDATE` commits last leaves its token as the only valid one (the other resend's token is silently superseded, not left dangling) | Making resend/resend mutually exclusive would require a compare-and-swap on the invite's current `token_hash` (an extra read before the update), reopening the repository contract already committed for `Refresh` (T1) for a race with no real consequence - not worth the added complexity for a self-inflicted double-click, which a UI-level debounce could also prevent without any backend change |

**Open questions:** none - all resolved or logged above.

---

## User Stories

### P1: Invite emails are actually delivered ⭐ MVP

**User Story**: As an instance owner, I want the admin invite I create to actually email the invitee their signup link, so that I don't have to manually extract a token from server logs to onboard a new admin.

**Why P1**: This is the entire reason `email-provider-connect` was built first; without it, invites remain unusable in production (dev-log-only).

**Acceptance Criteria**:

1. WHEN `POST /api/admins` succeeds in creating the invite record THEN the system SHALL call `email.Service.SendAdminInvite` with the invitee's email, role, and the raw (unhashed) invite token before responding
2. WHEN the email send succeeds THEN the system SHALL respond `201` with `{"status":"invited","email_sent":true}`
3. IF the email send fails (no active provider, or a provider error) THEN the system SHALL still respond `201` with `{"status":"invited","email_sent":false}` and log the failure at Error level with the invite ID - the invite record is not rolled back
4. The system SHALL never include the raw invite token in the HTTP response body (token only ever reaches the invitee via the sent email, or via `VANE_DEV_TOKEN_LOGGING` server logs as today)

**Independent Test**: Connect a SendGrid/Resend provider, invite an admin, confirm the provider's send API is called with the correct template data and `email_sent:true` in the response; disconnect/misconfigure the provider and confirm the invite still succeeds with `email_sent:false`.

---

### P1: Owner resends a pending invite ⭐ MVP

**User Story**: As an instance owner, I want to resend a pending invite (lost, ignored, or expired), so that the invitee gets a fresh working link without me re-entering their email and role.

**Why P1**: Core ask of this feature; the frontend's `useResendInvite` hook already exists with no backend route to call.

**Acceptance Criteria**:

1. WHEN owner calls `POST /api/admins/invites/{id}/resend` for an invite with `used_at IS NULL` THEN the system SHALL generate a new random token, set a new `token_hash`, reset `expires_at` to `now() + 1h`, send the admin-invite email to the invite's stored email/role, and respond `200` with `{"status":"resent","email_sent":bool}`
2. IF `{id}` does not match any invite with `used_at IS NULL` (never existed, or already accepted/canceled) THEN the system SHALL respond `404` without altering any row
3. The system SHALL record a `"resent"` audit entry (actor = requesting owner, target = invite ID) on success
4. WHILE the requesting admin's role is not `owner` THEN the system SHALL respond `403` (existing `RequireRole(owner)` gate, same as `Invite`/`Delete`)
5. WHEN a resend request races a cancel (or another cancel) on the same invite ID THEN the system SHALL ensure exactly one succeeds (200) and the other receives 404, via the atomic `WHERE used_at IS NULL RETURNING`/`WHERE used_at IS NULL` update (a resend racing another resend is not required to be mutually exclusive - see Assumptions)

**Independent Test**: Create an invite, call resend, confirm the old token no longer works via `AcceptInvite` (401) while the new token does; call resend twice concurrently on the same ID and confirm only one 200.

---

### P1: Owner cancels a pending invite ⭐ MVP

**User Story**: As an instance owner, I want to cancel a pending invite, so that a link I no longer want to honor (wrong email, invited by mistake, role changed my mind) stops working immediately instead of waiting up to an hour for expiry.

**Why P1**: Other half of this feature's stated scope; `useCancelInvite` hook already exists unwired.

**Acceptance Criteria**:

1. WHEN owner calls `DELETE /api/admins/invites/{id}` for an invite with `used_at IS NULL` THEN the system SHALL set `used_at = now()` on that row and respond `200` with `{"status":"canceled"}`
2. WHEN a canceled invite's token is later submitted to `AcceptInvite` THEN the system SHALL respond `401` (identical to an expired/already-used token - `ClaimForUse`'s `WHERE used_at IS NULL` already covers this with no code change needed)
3. IF `{id}` does not match any invite with `used_at IS NULL` THEN the system SHALL respond `404` without altering any row
4. The system SHALL record a `"canceled"` audit entry (actor = requesting owner, target = invite ID) on success
5. WHILE the requesting admin's role is not `owner` THEN the system SHALL respond `403`

**Independent Test**: Create an invite, cancel it, confirm `AcceptInvite` with its token now returns 401; confirm the invite disappears from `GET /api/admins`'s pending list.

---

### P2: Expired-but-unused invites remain manageable

**User Story**: As an instance owner, I want an invite that expired before the invitee used it to still show up as pending, so that I can resend it instead of starting over.

**Why P2**: Meaningfully improves the resend flow but the P1 stories are independently demoable without it (an owner can still resend/cancel a not-yet-expired invite).

**Acceptance Criteria**:

1. WHEN `GET /api/admins` is called THEN the system SHALL include every invite with `used_at IS NULL` in the pending section regardless of `expires_at`, tagging entries where `expires_at <= now()` with `"expired": true` in the JSON response
2. WHEN owner resends an invite whose `expires_at` has already passed THEN the system SHALL treat it identically to resending a not-yet-expired one (new token, new 1h expiry, per the P1 resend story)

**Independent Test**: Create an invite, manually age its `expires_at` into the past (test fixture), confirm it still appears in `GET /api/admins` with `expired:true`, then resend it and confirm it becomes usable again.

---

## Edge Cases

- IF the email provider call times out or errors THEN the system SHALL treat it as `email_sent:false` (never a 500) - covered by P1 story 1, AC3
- IF `{id}` in resend/cancel is not a valid UUID / doesn't parse THEN the system SHALL respond `404` (same not-found path, no separate validation branch needed since the query simply won't match)
- IF resend/cancel is called on an invite that was already accepted (an `admins` row now exists for that email) THEN the system SHALL still respond based on the invite row state alone (`used_at` already set from acceptance) - `404`, no special-casing of "already accepted" vs "already canceled"

---

## Requirement Traceability

| Requirement ID | Story                                        | Phase  | Status  |
| --------------- | --------------------------------------------- | ------ | ------- |
| INVITE-01       | P1: Invite emails are actually delivered      | Tasks | Implementing |
| INVITE-02       | P1: Invite emails are actually delivered      | Tasks | Implementing |
| INVITE-03       | P1: Owner resends a pending invite            | Tasks | Implementing |
| INVITE-04       | P1: Owner resends a pending invite            | Tasks | Implementing |
| INVITE-05       | P1: Owner cancels a pending invite            | Tasks | Implementing |
| INVITE-06       | P1: Owner cancels a pending invite            | Tasks | Implementing |
| INVITE-07       | P2: Expired-but-unused invites remain manageable | Tasks | Implementing |
| INVITE-08       | Concurrency safety (resend/cancel race)       | Tasks | Implementing |
| INVITE-09       | Auth boundary (owner-only)                    | Tasks | Implementing |

**ID format:** `INVITE-[NUMBER]`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 9 total, 9 mapped to tasks (T1-T8), 0 unmapped

---

## Success Criteria

How we know the feature is successful:

- [ ] An owner can invite an admin and the invitee receives a real email with a working link (no server-log copy-paste needed) whenever a provider is connected
- [ ] An owner can resend or cancel any pending invite - including expired ones - directly from the admin UI, with the two hooks already shipped in `hooks.ts` fully wired end to end
- [ ] Zero regressions in existing `AcceptInvite`/`Invite`/`List` behavior (all current admin-invite tests keep passing)
