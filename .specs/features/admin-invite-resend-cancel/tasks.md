# Admin Invite Resend/Cancel Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

**Tools policy (user-approved):** use Context7 MCP when researching an unfamiliar API/library pattern during a task, and any other tool judged useful. No task here touches genuinely unfamiliar tech (pure extension of existing Go/React patterns already in this repo), so Context7 is expected to be unnecessary in practice - the door stays open if a task hits something the codebase doesn't already show.

---

**Design**: `.specs/features/admin-invite-resend-cancel/design.md`
**Status**: Approved

---

## Test Coverage Matrix

> Generated from codebase sampling (`internal/api/admins_test.go`, `internal/db/admin_invites_test.go`, `web/src/features/admins/hooks.test.ts`, `web/src/features/admins/AdminsPage.test.tsx`) and `Makefile`/CI. Guidelines found: none dedicated (no `AGENTS.md`/coverage-threshold config) - existing test samples set the floor; strong defaults (1:1 AC mapping, every edge case) set the target.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| ---------- | ------------------- | ---------------------- | ------------------- | -------------- |
| `AdminInviteRepository` (Refresh, Cancel, List) | integration | Key query paths + error paths (not-found, already-used, concurrent race) - matches existing `admin_invites_test.go` depth | `internal/db/admin_invites_test.go` | `go test -tags=integration ./internal/db/...` |
| `AdminsHandler` (Invite, ResendInvite, CancelInvite, List) | integration | All routes in scope: happy + every listed edge case + error/failure paths, 1:1 to spec ACs | `internal/api/admins_test.go` | `go test -tags=integration ./internal/api/...` |
| `routes.go` wiring (role gates on 2 new routes) | integration | Owner succeeds, operator/viewer 403 - matches existing `routes_test.go` role-auth table pattern | `internal/cli/routes_test.go` | `go test -tags=integration ./internal/cli/...` |
| Frontend `hooks.ts` (useResendInvite, useCancelInvite, useInviteAdmin types) | unit | Every hook: success + error path, matches existing `hooks.test.ts` depth | `web/src/features/admins/hooks.test.ts` | `cd web && npm run test` |
| Frontend `AdminsPage.tsx` (wired buttons, expired tag) | unit | Happy path (click → hook called) + expired-tag rendering, matches existing `AdminsPage.test.tsx` depth | `web/src/features/admins/AdminsPage.test.tsx` | `cd web && npm run test` |

## Gate Check Commands

> Generated from `Makefile` and existing `//go:build integration` convention (same commands as `email-provider-connect/tasks.md`).

| Gate Level | When to Use | Command |
| ---------- | ----------- | ------- |
| Quick (Go unit) | After a task touching only unit-tested Go layers (none in this feature - all Go layers here are integration-tested) | `gofmt -l . && go vet ./... && go test ./...` |
| Full (Go + DB) | After tasks touching the repository, handler, or routes wiring | `gofmt -l . && go vet ./... && go test ./... && go test -tags=integration ./...` (requires `TEST_DATABASE_URL`) |
| Web quick | After frontend hook/page tasks | `cd web && npx tsc -b --noEmit && npm run test` |
| Build (phase close) | After each phase completes | Full (above) **and** Web quick (above) **and** `cd web && npm run build` **and** `make build` |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Repository

T1 and T2 both extend `internal/db/admin_invites.go`; T2 runs after T1 to avoid overlapping edits to the same file, though neither functionally depends on the other's output.

Tasks: T1, T2.

### Phase 2: Handler

Builds on Phase 1's repository methods. Tasks: T3, T4, T5, T6.

### Phase 3: Routes Wiring

Wires Phase 2's handlers into the router. Tasks: T7.

### Phase 4: Frontend

Consumes the finished backend contract. Tasks: T8.

---

## Task Breakdown

### T1: Add `Refresh` and `Cancel` to `AdminInviteRepository`

**What**: Add two new atomic, ID-keyed methods: `Refresh(ctx, id, newTokenHash string, newExpiresAt time.Time) (*AdminInvite, error)` (updates `token_hash`+`expires_at` where `used_at IS NULL`, `ErrNotFound` otherwise) and `Cancel(ctx, id string) error` (sets `used_at = now()` where `used_at IS NULL`, `ErrNotFound` if `RowsAffected() == 0`).
**Where**: `internal/db/admin_invites.go`
**Depends on**: None
**Reuses**: The `ClaimForUse`/`MarkUsed` atomic-update shape (design.md Code Reuse Analysis); `ErrNotFound` convention already used throughout this file
**Requirement**: INVITE-03, INVITE-05, INVITE-08

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `Refresh` and `Cancel` implemented exactly as above, both single-statement atomic `UPDATE ... WHERE used_at IS NULL RETURNING`/`... WHERE used_at IS NULL`
- [x] Both return `ErrNotFound` for: unknown ID, already-accepted invite, already-canceled invite (all collapse to the same `used_at IS NOT NULL` state)
- [x] Concurrent `Refresh`/`Refresh` and `Refresh`/`Cancel` on the same ID: exactly one call succeeds, the other gets `ErrNotFound` (test with two goroutines, same pattern as `TestAcceptInvite_ConcurrentAccept_OnlyOneSucceeds`)
- [x] Gate check passes: `go test -tags=integration ./internal/db/...`
- [x] Test count: existing `admin_invites_test.go` tests (7) + at least 6 new tests (Refresh success, Refresh not-found×3 variants, Cancel success, Cancel not-found, 1 concurrency test) = 13+ pass, no silent deletions

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add atomic Refresh/Cancel to AdminInviteRepository`

---

### T2: Drop expiry filter from `AdminInviteRepository.List`

**What**: Change `List`'s `WHERE` clause from `used_at IS NULL AND expires_at > now()` to `used_at IS NULL` only, so expired-but-unused invites stay in the result set (spec P2, expired stays manageable).
**Where**: `internal/db/admin_invites.go` (modify `List`)
**Depends on**: None
**Reuses**: `List`'s existing scan/order-by structure, unchanged otherwise
**Requirement**: INVITE-07

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `List` returns invites with `used_at IS NULL` regardless of `expires_at`, still ordered `created_at DESC`
- [x] Existing test `TestAdminInviteRepository_List_ReturnsOnlyPendingNotExpiredMostRecentFirst` updated to reflect the new contract (rename + adjust assertion so an expired-unused invite IS included; add a case proving a used/accepted invite is still excluded)
- [x] Gate check passes: `go test -tags=integration ./internal/db/...`
- [x] Test count: no net loss versus T1's baseline; the modified test still exercises "excludes used", now also asserts "includes expired-unused". Also updated the downstream handler test `TestListAdmins_Owner_200_ExcludesUsedAndExpiredInvites` (renamed `TestListAdmins_Owner_200_ExcludesUsedIncludesExpiredInvites`), whose exclusion assertion was direct fallout of this contract change and is superseded by INVITE-07 in T6.

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): include expired-unused invites in AdminInviteRepository.List`

---

### T3: Wire `email.Service.SendAdminInvite` into `AdminsHandler.Invite`

**What**: Add `emailSvc *email.Service` and `companySettings *db.CompanySettingsRepository` fields to `AdminsHandler` (grow `NewAdminsHandler`'s signature accordingly); after `h.invites.Create` succeeds, look up `CompanyName` via `companySettings.Get`, build `email.AdminInviteEmailData{CompanyName, Role: invite.Role, AcceptURL: fmt.Sprintf("%s://%s/accept-invite/%s", scheme, r.Host, rawToken)}` (scheme from a new `httpsEnabled bool` field/param, same value as `cfg.HTTPSEnabled`), call `h.emailSvc.SendAdminInvite`, and set `email_sent` in the JSON response based on whether that call errored (logging the error, never failing the request). Remove/rewrite the now-stale `// Email delivery is out of scope for the MVP...` doc comment block.
**Where**: `internal/api/admins.go` (modify `AdminsHandler` struct, `NewAdminsHandler`, `Invite`)
**Depends on**: None
**Reuses**: `email.AdminInviteEmailData`, `email.Service.SendAdminInvite` (both already implemented, `email-provider-connect`); `generateAdminInviteToken` (unchanged)
**Requirement**: INVITE-01, INVITE-02

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `NewAdminsHandler` takes the two new dependencies plus an `httpsEnabled bool`; all existing call sites (`internal/cli/routes.go`, `internal/api/admins_test.go`) updated to compile (routes.go's `adminsHandler` construction now passes real `emailService`/`companySettingsRepo`/`cfg.HTTPSEnabled`, reordered earlier in `buildAdminRouter` so they exist by then - registering the two new routes themselves is still T7)
- [x] `Invite`'s success response body is `{"status":"invited","email_sent":true|false}` in both the send-succeeds and send-fails branches, `201` either way
- [x] A `SendAdminInvite` failure (fake provider returns error, or `ErrNoActiveProvider`) is logged at Error with the invite ID and does NOT roll back the created invite row or change the HTTP status
- [x] Stale "Email delivery is out of scope" comment removed/rewritten to describe the actual send-then-log-failure behavior
- [x] Gate check passes: `go test -tags=integration ./internal/api/...`
- [x] Test count: existing `admins_test.go` invite tests (6) still pass + 3 new (email sent → `email_sent:true` + no-token-in-response assertion added to the existing happy-path test; email fails → `email_sent:false`, invite still created; no active provider → same as fails) = 9 pass, no silent deletions

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): send real admin-invite email on Invite`

---

### T4: Add `AdminsHandler.ResendInvite`

**What**: Add `ResendInvite(w, r)` handling `POST /api/admins/invites/{id}/resend` (route not registered yet - that's T7): generate a new token via `generateAdminInviteToken`, call `h.invites.Refresh(ctx, id, hashAdminInviteToken(rawToken), time.Now().Add(adminInviteTTL))`; on `ErrNotFound` respond 404; on success, build `AdminInviteEmailData` (same shape as T3) from the refreshed invite's `Email`/`Role`, call `h.emailSvc.SendAdminInvite`, respond `200` with `{"status":"resent","email_sent":bool}`, record a `"resent"` audit entry (actor = requesting owner, target = invite ID).
**Where**: `internal/api/admins.go` (new method)
**Depends on**: T1, T3
**Reuses**: `generateAdminInviteToken`, `hashAdminInviteToken`, `adminInviteTTL`, `writeAdminError`, `h.audit.Record`, `AdminFromContext`, T3's email-send-then-log-failure pattern
**Requirement**: INVITE-03, INVITE-04, INVITE-08, INVITE-09

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `ResendInvite` implemented exactly as above; old token no longer accepted after resend (verify via `ClaimForUse` returning `ErrNotFound` for the old hash), new token accepted
- [x] Unknown/already-accepted/already-canceled ID → `404`, no audit entry recorded, no email sent
- [x] Two concurrent resend calls on the same ID: `Refresh` does not set `used_at`, so this is NOT required to be mutually exclusive (spec.md Assumptions, updated during T4) - test instead proves no corruption: both requests get a definitive response (200 or a legitimate error, never a hang/panic), and exactly one token is valid afterward (whichever `UPDATE` committed last)
- [x] `"resent"` audit entry recorded on success only
- [x] Gate check passes: `go test -tags=integration ./internal/api/...`
- [x] Test count: 5 new tests (happy path email-sent, happy path email-fails, not-found, already-accepted, concurrent-resend-no-corruption) pass, no silent deletions

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): add AdminsHandler.ResendInvite`

---

### T5: Add `AdminsHandler.CancelInvite`

**What**: Add `CancelInvite(w, r)` handling `DELETE /api/admins/invites/{id}` (route not registered yet - that's T7): call `h.invites.Cancel(ctx, id)`; on `ErrNotFound` respond 404; on success respond `200` with `{"status":"canceled"}` and record a `"canceled"` audit entry (actor = requesting owner, target = invite ID).
**Where**: `internal/api/admins.go` (new method)
**Depends on**: T1
**Reuses**: `writeAdminError`, `h.audit.Record`, `AdminFromContext`
**Requirement**: INVITE-05, INVITE-06, INVITE-09

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `CancelInvite` implemented exactly as above; a canceled invite's original token subsequently rejected by `AcceptInvite` with `401` (falls out of `ClaimForUse`'s existing `WHERE used_at IS NULL` - no code change needed there, just a test proving it)
- [x] Unknown/already-accepted/already-canceled ID → `404`, no audit entry recorded
- [x] `"canceled"` audit entry recorded on success only
- [x] Gate check passes: `go test -tags=integration ./internal/api/...`
- [x] Test count: 3 new tests (happy path + canceled-then-accept-401 + audit entry present in one test, not-found, already-canceled-no-duplicate-audit) pass, no silent deletions

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): add AdminsHandler.CancelInvite`

---

### T6: Add `expired` flag to `AdminsHandler.List`'s pending entries

**What**: Extend `adminResponse` with `Expired bool \`json:"expired,omitempty"\`` (present only for `Status: "pending"` entries); in `List`, after scanning each invite, set `item.Expired = invite.ExpiresAt.Before(time.Now())`.
**Where**: `internal/api/admins.go` (modify `adminResponse`, `List`)
**Depends on**: T2
**Reuses**: `List`'s existing invite-to-`adminResponse` mapping loop
**Requirement**: INVITE-07

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] A pending invite whose `expires_at` is in the past appears in `GET /api/admins`'s response with `"expired":true`; a not-yet-expired one omits the key entirely (`omitempty` on `false`) - asserted consistently via `Expired bool` decode in tests
- [x] Active admin entries never carry an `expired` field
- [x] Gate check passes: `go test -tags=integration ./internal/api/...`
- [x] Test count: 2 new tests (expired invite flagged via updated `TestListAdmins_Owner_200_ExcludesUsedIncludesExpiredInvites`, non-expired invite not flagged + active admins never flagged) pass, no silent deletions

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): flag expired pending invites in AdminsHandler.List`

---

### T7: Wire routes and constructor call site

**What**: In `buildAdminRouter`, update the `api.NewAdminsHandler(...)` call site to pass `emailService`, `companySettingsRepo` (both already constructed earlier in this function for other handlers), and `cfg.HTTPSEnabled`; register `r.With(ownerOnly).Post("/api/admins/invites/{id}/resend", adminsHandler.ResendInvite)` and `r.With(ownerOnly).Delete("/api/admins/invites/{id}", adminsHandler.CancelInvite)`.
**Where**: `internal/cli/routes.go` (modify `buildAdminRouter`)
**Depends on**: T3, T4, T5
**Reuses**: `ownerOnly` (`api.RequireRole(db.RoleOwner)`), the already-constructed `emailService`/`companySettingsRepo` variables
**Requirement**: INVITE-09

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Both new routes reachable end-to-end (owner can resend/cancel a real invite through the full router, not just the handler in isolation)
- [ ] Operator and viewer roles get `403` on both new routes (extend `routes_test.go`'s existing role-auth table, same pattern as the other admin-management routes)
- [ ] Gate check passes: `go test -tags=integration ./internal/cli/...` and full build gate (`gofmt -l . && go vet ./... && go test ./... && go test -tags=integration ./...`)
- [ ] Test count: existing `routes_test.go` role-auth cases + at least 4 new rows (owner/operator/viewer × 2 routes, collapsed to however many rows the existing table format uses) pass, no silent deletions

**Tests**: integration
**Gate**: full

**Commit**: `feat(cli): register admin invite resend/cancel routes`

---

### T8: Wire frontend resend/cancel actions and expired tag

**What**: In `hooks.ts`: change `AdminRow` to add `expired?: boolean`; change `useResendInvite`'s and `useInviteAdmin`'s mutation return types to `{status: string; email_sent: boolean}` (from `void`/`{status:string}`). In `AdminsPage.tsx`: remove the `disabled`/`Tooltip("Ainda não disponível")` wrapping on the "Reenviar"/"Cancelar" buttons in `pendingColumns`, wire their `onClick` to `useResendInvite().mutate(a.id)` / `useCancelInvite().mutate(a.id)`, and render an "Expirado" `Tag` next to the pending status tag when `a.expired` is true.
**Where**: `web/src/features/admins/hooks.ts`, `web/src/features/admins/AdminsPage.tsx`
**Depends on**: T4, T5, T6, T7
**Reuses**: `useResendInvite`, `useCancelInvite` (already exported, unused until now), existing `Table`/`Button`/`Tag`/`Tooltip` components, existing MSW handler setup pattern in this feature's test files
**Requirement**: INVITE-03, INVITE-05, INVITE-07

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Clicking "Reenviar" on a pending row calls `POST /api/admins/invites/{id}/resend` and invalidates the `["admins"]` query on success (already implemented in the hook - just needs to be reachable from the UI)
- [ ] Clicking "Cancelar" on a pending row calls `DELETE /api/admins/invites/{id}` and invalidates `["admins"]` on success
- [ ] A pending row with `expired: true` shows an additional "Expirado" tag next to "Pendente"
- [ ] No remaining `disabled`/"Ainda não disponível" tooltip on either button
- [ ] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test`
- [ ] Test count: existing `AdminsPage.test.tsx`/`hooks.test.ts` suites + at least 3 new tests (resend click → mutation called, cancel click → mutation called, expired tag renders) pass, no silent deletions

**Tests**: unit
**Gate**: build

**Commit**: `feat(web): wire admin invite resend/cancel actions and expired tag`

---

## Phase Execution Map

Phases run in order: Phase 1 → Phase 2 → Phase 3 → Phase 4. Within a phase, tasks execute in listed order regardless of whether a dependency edge exists (same-file convention, not a functional dependency) - only true dependency edges are drawn below:

```
T1 ------→ T4
T1 ------→ T5
T2 ------→ T6
T3 ------→ T4
T3 ------→ T7
T4 ------→ T7
T5 ------→ T7
T4 ------→ T8
T5 ------→ T8
T6 ------→ T8
T7 ------→ T8
```

- **Phase 1** (repository): T1, then T2 - no dependency between them, ordered only to avoid overlapping edits to the same file.
- **Phase 2** (handler): T3, then T4, then T5, then T6 - T4 depends on T1+T3; T5 depends on T1; T6 depends on T2; T3 has no dependency. Ordered sequentially since all four edit `internal/api/admins.go`.
- **Phase 3** (routes wiring): T7 - depends on T3, T4, T5.
- **Phase 4** (frontend): T8 - depends on T4, T5, T6, T7.

Execution is strictly sequential - there is no intra-phase parallelism. A single agent works one task at a time, in order. All 8 tasks fit a single ~7-8 task batch, so Execute proceeds inline with no sub-agent dispatch (see Sub-Agent Delegation trigger: > ~8 tasks).

---

## Task Granularity Check

| Task | Scope | Status |
| ---- | ----- | ------ |
| T1: Add Refresh/Cancel to AdminInviteRepository | 2 tightly-coupled functions, 1 file | ✅ Granular (2-3 related things in same file, cohesive) |
| T2: Drop expiry filter from List | 1 function (query change) | ✅ Granular |
| T3: Wire SendAdminInvite into Invite | 1 method + constructor signature, 1 file | ✅ Granular |
| T4: Add ResendInvite | 1 endpoint | ✅ Granular |
| T5: Add CancelInvite | 1 endpoint | ✅ Granular |
| T6: Add expired flag to List | 1 field + 1 function change | ✅ Granular |
| T7: Wire routes and constructor call site | 1 file, 1 concern (route registration) | ✅ Granular |
| T8: Wire frontend resend/cancel + expired tag | 2 files, 1 cohesive concern (activate already-built hooks in already-built UI) | ✅ Granular (2-3 related things, cohesive - no new component created) |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| ---- | ----------------------- | -------------- | ------ |
| T1 | None | None | ✅ Match |
| T2 | None | None | ✅ Match |
| T3 | None | None | ✅ Match |
| T4 | T1, T3 | T1→T4, T3→T4 | ✅ Match |
| T5 | T1 | T1→T5 | ✅ Match |
| T6 | T2 | T2→T6 | ✅ Match |
| T7 | T3, T4, T5 | T3→T7, T4→T7, T5→T7 | ✅ Match |
| T8 | T4, T5, T6, T7 | T4→T8, T5→T8, T6→T8, T7→T8 | ✅ Match |

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| ---- | ----------------------------- | ----------------- | ----------- | ------ |
| T1: Add Refresh/Cancel | `AdminInviteRepository` | integration | integration | ✅ OK |
| T2: Drop expiry filter | `AdminInviteRepository` | integration | integration | ✅ OK |
| T3: Wire SendAdminInvite into Invite | `AdminsHandler` | integration | integration | ✅ OK |
| T4: Add ResendInvite | `AdminsHandler` | integration | integration | ✅ OK |
| T5: Add CancelInvite | `AdminsHandler` | integration | integration | ✅ OK |
| T6: Add expired flag | `AdminsHandler` | integration | integration | ✅ OK |
| T7: Wire routes | `routes.go` wiring | integration | integration | ✅ OK |
| T8: Frontend wiring | `hooks.ts` + `AdminsPage.tsx` | unit | unit | ✅ OK |

---

## Requirement Traceability (updated)

| Requirement ID | Story | Tasks | Status |
| --------------- | ------ | ----- | ------ |
| INVITE-01 | Invite emails are actually delivered | T3 | In Tasks |
| INVITE-02 | Invite emails are actually delivered | T3 | In Tasks |
| INVITE-03 | Owner resends a pending invite | T1, T4, T8 | In Tasks |
| INVITE-04 | Owner resends a pending invite | T4 | In Tasks |
| INVITE-05 | Owner cancels a pending invite | T1, T5, T8 | In Tasks |
| INVITE-06 | Owner cancels a pending invite | T5 | In Tasks |
| INVITE-07 | Expired-but-unused invites remain manageable | T2, T6, T8 | In Tasks |
| INVITE-08 | Concurrency safety | T1, T4 | In Tasks |
| INVITE-09 | Auth boundary (owner-only) | T4, T5, T7 | In Tasks |

**Coverage:** 9 total, 9 mapped to tasks, 0 unmapped
