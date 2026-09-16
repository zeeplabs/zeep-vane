# AC Baseline — user-sessions (frozen checklist)

**Source PRD:** `.specs/features/user-sessions/spec.md`
**Frozen at:** 2026-09-11 (spec-driven-eval baseline; not per-run)
**Diff base:** `5083956` (commit immediately before the user-sessions work)
**Diff head:** `dbeaedc` (current HEAD on develop)
**Scoring weights (frozen):** AC_score = 0.6 × I + 0.4 × T; Final = Σ(w × Story_score) / Σ(w); P1 = 2, P0 = 3, P2 = 0
**Evaluator assumption:** same model as the author (minimax-m3); borderline checks recorded as UNMET per Core rule 4.
**k=3 limitation:** with same model + temperature 0 the three passes collapse to one deterministic read; this baseline is the single effective pass and the report flags it.

---

## Story 1 — P1: Every login creates a real, identifiable session row (SESS-01..04)

### SESS-01 — Login, AcceptInvite, Bootstrap.Create create a `sessions` row; token carries its id as `sid`

**Implementation checks (binary):**
- I-01a: `AuthHandler.issueSessionForUser` calls `h.sessions.Create(...)` for the login path (`Login` → `issueSessionForUser`; `VerifyTwoFactor` → `issueSessionForUser`)
- I-01b: `AdminsHandler.AcceptInvite` calls `h.sessions.Create(...)` for the invite-acceptance path
- I-01c: `BootstrapHandler.Create` calls `h.sessions.Create(...)` for the bootstrap path
- I-01d: `captureSessionContext(r)` is the single helper sourcing `User-Agent` and IP for all four call sites
- I-01e: `auth.IssueSessionWithTenant(adminID, tenantID, sessionID, secret)` is invoked with the freshly-created row's id as `sessionID`, so the issued JWT's `sid` claim carries it
- I-01f: `VerifySessionClaims` rejects a token whose `sid` claim is empty (so the claim is enforced as required, not silently allowed to be missing)

**Test checks (binary, integration required — HTTP-driven):**
- T-01a: integration test that POST /api/auth/login results in a `sessions` row carrying the request's `User-Agent` and `r.RemoteAddr`-derived IP, and the issued token's `sid` claim matches that row's id
- T-01b: integration test that AcceptInvite path results in a `sessions` row
- T-01c: integration test that Bootstrap.Create path results in a `sessions` row
- T-01d: integration test that a token with no `sid` claim is rejected with 401 by `RequireAuth`

### SESS-02 — SwitchTenant reuses the existing row (no new row, same `sid`)

**Implementation checks (binary):**
- I-02a: `AuthHandler.SwitchTenant` reads `sid` from context (placed by `RequireAuth`) and calls `auth.IssueSessionWithTenant(user.ID, req.TenantID, sid, h.sessionSecret)` — same `sid`, new `tid`, fresh `iat`
- I-02b: `AuthHandler.SwitchTenant` does NOT call `h.sessions.Create` (no second row)

**Test checks (binary, integration required):**
- T-02a: integration test that after `SwitchTenant`, the row count for that user is unchanged and the new token's `sid` claim equals the pre-switch token's `sid` claim

### SESS-03 — RequireAuth rejects a token whose `sid` doesn't match a non-revoked row

**Implementation checks (binary):**
- I-03a: `RequireAuth` calls `sessions.GetByID(claims.SessionID)` after the user/sessions_revoked_at checks
- I-03b: `RequireAuth` responds 401 when `sessions.GetByID` returns `ErrNotFound` (missing row)
- I-03c: `RequireAuth` responds 401 when the row's `RevokedAt` is set (per-device revocation: Logout, Encerrar, RevokeAllForUser)

**Test checks (binary, integration required):**
- T-03a: integration test that a token with a `sid` that doesn't match any row → 401
- T-03b: integration test that a token whose `sid` row has `revoked_at` set → 401 on next request
- T-03c: integration test that a valid (sid, non-revoked) request → 200

### SESS-04 — `last_seen_at` updated throttled to ≥ 5 minutes per row

**Implementation checks (binary):**
- I-04a: `RequireAuth` calls `sessions.TouchLastSeen(sess.ID)` on every successful request
- I-04b: `SessionRepository.TouchLastSeen` throttles via the SQL predicate `(last_seen_at IS NULL OR last_seen_at < now() - INTERVAL '5 minutes')` (so writes are at most once per 5 min per row, not per request)
- I-04c: `RequireAuth` treats `TouchLastSeen` failure as advisory (logged, request not blocked) — fail-open posture

**Test checks (binary, integration required for T-04a/T-04b; unit acceptable for the SQL predicate itself):**
- T-04a: integration test that two consecutive authenticated requests within < 5 min result in exactly one `last_seen_at` write for the second request (or both within the throttle) — assertable via the row's `last_seen_at` value advancing once, not twice

---

## Story 2 — P1: User lists their own active sessions (SESS-05, SESS-06)

### SESS-05 — GET /api/auth/sessions returns 200 with the user's non-revoked, non-expired sessions

**Implementation checks (binary):**
- I-05a: `SessionsHandler.List` exists and is registered on the protected route group
- I-05b: handler calls `h.sessions.ListForUser(ctx, user.ID)`
- I-05c: `ListForUser` filters `revoked_at IS NULL` and `created_at >= now() - INTERVAL '24 hours'` (excludes revoked AND expired)
- I-05d: each row in the response carries `id`, `user_agent`, `ip`, `created_at`, `last_seen_at`, `current`
- I-05e: response is a JSON array (not wrapped in `Page<T>`)

**Test checks (binary, integration required):**
- T-05a: integration test that GET /api/auth/sessions returns 200 with the seeded sessions (the handler test list)
- T-05b: integration test that revoked sessions are excluded from the list
- T-05c: integration test that expired sessions (created_at older than SessionTTL) are excluded from the list

### SESS-06 — Caller's current session is in the list, marked distinctly; exactly one row is `current: true`

**Implementation checks (binary):**
- I-06a: handler computes `current = (s.ID == sid)` where `sid` is the request's own JWT `sid` claim (read from context)
- I-06b: the JSON view's `current` field is a `bool` (not an enum, not an index)

**Test checks (binary, integration required):**
- T-06a: integration test with two logins (two distinct sessions for the same user) verifies that calling the list with one of the two tokens returns both rows, with exactly one — the one whose `sid` matches the calling token — marked `current: true`

---

## Story 3 — P1: User revokes one specific other session (SESS-07..10)

### SESS-07 — DELETE /api/auth/sessions/{id} for a session belonging to the caller (other than current) sets `revoked_at` and returns 200

**Implementation checks (binary):**
- I-07a: `SessionsHandler.Revoke` exists and is registered on the protected route group
- I-07b: handler rejects with 409 (NOT 200, NOT revoke) when `id == sid` (caller's current session) — emits the constant `cannotRevokeCurrentSessionBody`
- I-07c: for any other id, handler calls `h.sessions.GetByIDAndUser(ctx, id, user.ID)` (ownership check, anti-enumeration via ErrNotFound for both "missing" and "other-user")
- I-07d: handler calls `h.sessions.Revoke(ctx, id)` and returns 200 with `{"status":"ok"}`
- I-07e: a malformed UUID for `{id}` is treated as 404 (anti-enumeration: same body as missing/other-user) — `isInvalidUUIDSyntax` branch

**Test checks (binary, integration required):**
- T-07a: integration test that DELETE for a valid (caller-owned, non-current) session returns 200 and sets `revoked_at` on the row
- T-07b: integration test that DELETE for a malformed UUID returns 404 with `sessionNotFoundBody`
- T-07c: integration test that DELETE for a session belonging to a different user returns 404 with `sessionNotFoundBody` (byte-identical to the missing case — anti-enumeration)
- T-07d: integration test that DELETE for the caller's own current session returns 409 with `cannotRevokeCurrentSessionBody` AND does NOT set `revoked_at` on the row

### SESS-08 — Subsequent request with the revoked session's token returns 401

**Implementation checks (binary):**
- I-08a: shared with SESS-03 I-03c (RequireAuth rejects revoked rows) — counted once toward SESS-08

**Test checks (binary, integration required):**
- T-08a: integration test that after a successful DELETE /api/auth/sessions/{id}, replaying the revoked token against any protected route returns 401

### SESS-09 — 409 if {id} is the caller's own current session (and not revoked)

**Implementation checks (binary):**
- I-09a: shared with SESS-07 I-07b — counted once toward SESS-09

**Test checks (binary, integration required):**
- T-09a: shared with SESS-07 T-07d — counted once toward SESS-09

### SESS-10 — 404 if {id} doesn't exist or belongs to a different user (anti-enumeration)

**Implementation checks (binary):**
- I-10a: shared with SESS-07 I-07c (ownership via `GetByIDAndUser`) and I-07e (malformed UUID) — counted once toward SESS-10

**Test checks (binary, integration required):**
- T-10a: shared with SESS-07 T-07b and T-07c — counted once toward SESS-10

---

## Story 4 — P2: Logout revokes the current session server-side (SESS-11)

### SESS-11 — POST /api/auth/logout sets `revoked_at` on the row matching the request's `sid`; subsequent request with the same token returns 401

**Implementation checks (binary):**
- I-11a: `AuthHandler.Logout` reads `sid` from `SessionIDFromContext(r.Context())` (the value `RequireAuth` placed there after the JWT/sid-row check)
- I-11b: `Logout` calls `h.sessions.Revoke(r.Context(), sid)` (fail-open: log warning, never fail the request)
- I-11c: `Logout` clears the `vane_session` cookie (`http.SetCookie(w, sessionCookie("", -1, ...))`)
- I-11d: if `sid` is missing from context, `Logout` responds 401 (defensive — should never happen behind `RequireAuth`)

**Test checks (binary, integration required):**
- T-11a: integration test that POST /api/auth/logout sets `revoked_at` on the row matching the token's `sid`
- T-11b: integration test that after logout, replaying the same token against any protected route returns 401
- T-11c: integration test that logout clears the `vane_session` cookie (`Set-Cookie` with `Max-Age=-1` or `Max-Age=0`)

---

## Edge cases from spec (recorded but not scored as separate ACs — covered by the ACs above)

- EC-1: missing `User-Agent` → stored as NULL — covered by SessionRepository.Create (`NULLIF($2, '')`)
- EC-2: client IP cannot be determined → stored as NULL — covered by captureSessionContext (returns `""` when SplitHostPort fails → NULL)
- EC-3: user has no other sessions besides current → single-item list — covered by SESS-05 I-05c (no filtering by "other" — current row is also returned)
- EC-4: created_at older than SessionTTL → excluded from list — covered by SESS-05 I-05c

---

## Out of scope (per spec.md §Out of Scope — w = 0, reported as roadmap readiness)

- Geolocation of IP
- Human-readable device labels beyond raw `User-Agent`
- Concurrent-session limits
- Admin visibility into another user's sessions

---

## Test-required level (per AC, fixed by policy — not judged per-AC)

- All HTTP-driven side-effects (any AC asserting a request returns a status, a row is created, or a row's `revoked_at` is set) → **integration** (`//go:build integration` in Go, real DB; MSW + Vitest in web)
- The `last_seen_at` SQL throttle predicate itself (SESS-04 I-04b) → unit on the SQL via integration test of the throttle behavior is also acceptable (a stricter-level test satisfies the floor)
- Pure logic (e.g. JWT claim checks, SidFromContext helpers) → **unit** (Go `*_test.go` without `//go:build integration`)
