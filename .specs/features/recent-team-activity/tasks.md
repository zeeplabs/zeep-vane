# Recent Team Activity Tasks

## Execution Protocol (MANDATORY — do not skip)

Implement these tasks with the `tlc-spec-driven` skill: activate it by name and follow its Execute flow and Critical Rules. Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user — do not proceed without it.**

---

**Spec**: `.specs/features/recent-team-activity/spec.md`
**Context**: `.specs/features/recent-team-activity/context.md`
**Design**: `.specs/features/recent-team-activity/design.md`
**Status**: Approved

---

## Test Coverage Matrix

> Generated from codebase (no repo-specific frontend/backend testing-standards doc beyond AGENTS.md §3's gate list — strong default applied). Sampled: `internal/audit/log_test.go`, `internal/api/admins_test.go`, `internal/db/tenant_invites_test.go`, `web/src/features/overview/OverviewPage.test.tsx`.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Migration | none (schema only) | Applies/rolls back cleanly | `internal/db/migrations/0037_*.sql` | build gate only (migration applied as part of integration gate setup) |
| `audit.Log` (domain) | unit | Every branch: `targetLabel` persisted, empty string converted to SQL `NULL` | `internal/audit/log_test.go` | `go test ./internal/audit/...` |
| `db.TenantInviteRepository.Cancel` (repository, DB-touching) | integration | Returns the canceled invite; existing cancel-path behavior unchanged | `internal/db/tenant_invites_test.go` (`-tags=integration`) | `TEST_DATABASE_URL=... go test -tags=integration -p 1 ./internal/db/...` |
| `db.AuditLogRepository` (repository, DB-touching, new) | integration | `ListRecent` ordering, limit capping, tenant isolation (RLS), actor-deleted fallback | `internal/db/audit_log_repository_test.go` (new, `-tags=integration`) | `TEST_DATABASE_URL=... go test -tags=integration -p 1 ./internal/db/...` |
| `api.AdminsHandler` / `DomainsHandler` / `StatusPagesHandler` (route/controller, modified) | unit (existing per-handler test files, httptest-based, no real DB) | Each modified call site: label captured and passed to a fake/mock `audit.Log`, correct value asserted | `internal/api/admins_test.go`, `internal/api/domains_handler_test.go`, `internal/api/status_pages_handler_test.go` | `go test ./internal/api/...` |
| `api.AuditLogHandler` (route/controller, new) | unit + integration for the route wiring | Happy path (200, correct shape, limit capping, default), 401, tenant isolation (covered at the repository integration layer, re-asserted here via a fake repository for the handler-level 401/limit-parsing paths) | `internal/api/audit_log_handler_test.go` (new) | `go test ./internal/api/...` |
| Frontend hook (`useRecentActivity`) | unit (RTL/MSW) | Query key, happy path, error path | `web/src/features/overview/hooks.test.ts` (new or extended) | `npm run test` |
| Frontend phrase mapping + `RecentActivity` component | unit (RTL) | Every one of the 8 actions renders its exact phrase; `null` target_label renders gracefully; unknown action renders fallback; loading/error/empty states | `web/src/features/overview/OverviewPage.test.tsx` | `npm run test` |
| Type safety | build | No new TS errors | n/a | `npx tsc -b --noEmit` |
| Go static checks | build | `go vet` clean, `gofmt -l` clean on changed files | n/a | `go vet ./... && gofmt -l <changed files>` |

## Gate Check Commands

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick (Go, non-DB) | Tasks touching only in-memory logic (`audit.Log`, handler call sites against fakes) | `go build ./... && go test ./...` |
| Quick (frontend) | Frontend-only tasks | `npx tsc -b --noEmit && npm run test -- <test file>` |
| Full (Go, DB-touching) | Migration, `TenantInviteRepository.Cancel`, `AuditLogRepository` | Disposable Postgres container per AGENTS.md §3 (`make test-integration`, or the manual `docker run ... && TEST_DATABASE_URL=... go test -tags=integration -p 1 ./... && docker stop vane-test-pg`) — never `vane-dev-pg` |
| Build | Last task of each phase | `go build ./... && go vet ./... && gofmt -l <changed> && go test ./...` (add the Full DB gate too when the phase touched DB code); frontend equivalent: `npx tsc -b --noEmit && npm run test` |

---

## Execution Plan

### Phase 1: Schema + audit package

```
T1 → T2
```
(T2 genuinely depends on T1 — the column must exist before the insert can reference it.)

### Phase 2: TenantInviteRepository.Cancel

```
T3
```

### Phase 3: admins.go call sites (5)

```
T2 → T4
T2 → T5
T2 → T6
T3 → T6
T2 → T7
T2 → T8
```
Note: T4, T5, T7, T8 each depend only on T2 (the extended `Record` signature) — they are independent of each other and execute in numeric order as a sequencing convention, not a real dependency. T6 additionally depends on T3 (needs `Cancel`'s new return value).

### Phase 4: domains_handler.go + status_pages_handler.go call sites (4)

```
T2 → T9
T2 → T10
T2 → T11
T2 → T12
```
Note: T9–T12 each depend only on T2 — independent of each other, numeric order is a sequencing convention.

### Phase 5: Read path (repository + handler + route)

```
T1 → T13
T13 → T14
```

### Phase 6: Frontend

```
T15 → T16 → T17
```

---

## Task Breakdown

### T1: Migration `0037_audit_log_target_label`

**What**: Add nullable `target_label TEXT` column to `admin_audit_log`.
**Where**: `internal/db/migrations/0037_audit_log_target_label.up.sql` (paired `.down.sql` in the same migration, per this repo's existing up/down convention)
**Depends on**: None
**Requirement**: ACTIVITY-01

**Done when**:
- [x] `up.sql`: `ALTER TABLE admin_audit_log ADD COLUMN target_label TEXT;`
- [x] `down.sql`: `ALTER TABLE admin_audit_log DROP COLUMN target_label;`
- [x] Migration applies and rolls back cleanly against a disposable Postgres container.

**Tests**: none (schema only, per matrix)
**Gate**: full (DB-touching — apply/rollback verified against the disposable container)
**Commit**: `feat(db): add target_label column to admin_audit_log`

---

### T2: `audit.Log.Record` signature change

**What**: Add `targetLabel string` parameter, persist it (empty string → SQL `NULL`).
**Where**: `internal/audit/log.go` (test file updated alongside, per co-location rule)
**Depends on**: T1
**Requirement**: ACTIVITY-02

**Done when**:
- [x] `Record(ctx, actorID, targetID, targetLabel, action string) error` — updated signature.
- [x] `targetLabel == ""` inserts SQL `NULL` for `target_label`, matching this codebase's existing `nilIfEmpty`-style convention for optional text columns.
- [x] Non-empty `targetLabel` persists verbatim.
- [x] Unit test (in-memory/fake pool or the package's existing test double — check `log_test.go`'s current approach) covers both branches.

**Tests**: unit
**Gate**: quick (Go, non-DB) — plus a one-off integration check that the real column accepts both cases (fold into T1's DB-touching gate if convenient, or verify manually; the package's own unit test does not require a live DB per its existing pattern — check `log_test.go` first)
**Commit**: `feat(audit): add targetLabel parameter to Record`

---

### T3: `TenantInviteRepository.Cancel` returns the canceled invite

**What**: Change `Cancel(ctx, tenantID, id) error` to `Cancel(ctx, tenantID, id) (*TenantInvite, error)`, using a `RETURNING` clause (mirror `Refresh`'s existing shape).
**Where**: `internal/db/tenant_invites.go` (test file updated alongside, per co-location rule)
**Depends on**: T1
**Requirement**: ACTIVITY-05

**Done when**:
- [x] `Cancel` returns the full canceled `*TenantInvite` (including `Email`) plus `error`.
- [x] `db.ErrNotFound` behavior unchanged (still returned when the invite doesn't exist).
- [x] Existing test(s) for `Cancel` updated to the new signature; new assertion confirms the returned invite's `Email` matches what was canceled.
- [x] The method's only call site (`internal/api/admins.go`, `canceled` action) is NOT yet updated in this task (that's T6) — this task only changes the repository method and its own tests; `go build ./...` will fail here until T6 lands, which is expected and acceptable since T3→T6 both land within the same Execute session before any intermediate push (AGENTS.md: local commits may accumulate).

**Tests**: integration (DB-touching repository)
**Gate**: full
**Commit**: `feat(db): return the canceled invite from TenantInviteRepository.Cancel`

---

### T4: `invited` call site passes `req.Email` as label

**Where**: `internal/api/admins.go:222` (test file updated alongside, per co-location rule)
**Depends on**: T2
**Requirement**: ACTIVITY-03

**Done when**:
- [ ] `h.audit.Record(r.Context(), actor.ID, invite.ID, req.Email, "invited")`.
- [ ] Existing invite-creation test(s) updated/extended to assert the audit call (via the handler's existing fake/mock `audit.Log` double) received `req.Email` as the label.

**Tests**: unit
**Gate**: quick
**Commit**: `feat(admins): record invite email as invited audit label`

---

### T5: `resent` call site passes `invite.Email` as label

**Where**: `internal/api/admins.go:484` (+ test file)
**Depends on**: T2
**Requirement**: ACTIVITY-03

**Done when**: `h.audit.Record(r.Context(), actor.ID, invite.ID, invite.Email, "resent")`; test asserts the label.
**Tests**: unit
**Gate**: quick
**Commit**: `feat(admins): record invite email as resent audit label`

---

### T6: `canceled` call site passes the canceled invite's email as label

**Where**: `internal/api/admins.go:518` (+ test file)
**Depends on**: T3, T2
**Requirement**: ACTIVITY-03, ACTIVITY-05

**Done when**:
- [ ] Call site updated to use `Cancel`'s new return value: `canceledInvite, err := h.invites.Cancel(...)`, then `h.audit.Record(r.Context(), actor.ID, id, canceledInvite.Email, "canceled")`.
- [ ] `go build ./...` passes now that both the repository (T3) and this call site are updated together.
- [ ] Test asserts the label equals the canceled invite's email.

**Tests**: unit
**Gate**: quick
**Commit**: `feat(admins): record invite email as canceled audit label`

---

### T7: `role_changed` call site fetches and passes admin name/email as label

**Where**: `internal/api/admins.go:608` (+ test file)
**Depends on**: T2
**Requirement**: ACTIVITY-03

**Done when**:
- [ ] Before `Record`, fetch `h.users.GetByID(ctx, targetID)` (only if not already available at this point in the function — check current variable scope first) to get the target's display name/email.
- [ ] `h.audit.Record(ctx, actor.ID, targetID, targetUser.Name, "role_changed")` (or `.Email` if `Name` can be empty — pick whichever this codebase's other displays already prefer for a user identifier; check `AdminsPage`'s existing row rendering for precedent).
- [ ] If `GetByID` fails (edge case — target concurrently deleted), fall back to an empty label rather than failing the whole role-change request (the role change itself already succeeded; the audit label is best-effort, matching the fire-and-forget error-logging-only posture every other `Record` call site already has).
- [ ] Test asserts the label equals the target's name/email.

**Tests**: unit
**Gate**: quick
**Commit**: `feat(admins): record target admin name as role_changed audit label`

---

### T8: `removed` call site fetches admin name BEFORE the conditional user delete

**Where**: `internal/api/admins.go:675` (+ test file)
**Depends on**: T2
**Requirement**: ACTIVITY-03, ACTIVITY-04

**Done when**:
- [ ] Fetch `h.users.GetByID(ctx, targetID)` for the label BEFORE the `if remaining == 0 { h.users.Delete(...) }` block (spec AC4 — label must be captured before the row can disappear).
- [ ] `h.audit.Record(ctx, actor.ID, targetID, removedUser.Name, "removed")`.
- [ ] Same best-effort fallback as T7 if the lookup fails.
- [ ] Test explicitly covers the case where `remaining == 0` (user IS hard-deleted) and confirms the label was still captured correctly — this is the discriminating case the Verifier's sensor should target (a mutation moving the fetch after the delete should make this specific test fail).

**Tests**: unit
**Gate**: build (last task of Phase 3 — run `go build ./... && go vet ./... && gofmt -l <changed> && go test ./...`)
**Commit**: `feat(admins): record target admin name as removed audit label`

---

### T9: `domain_verified` call site passes the domain string as label

**Where**: `internal/api/domains_handler.go:312` (+ test file)
**Depends on**: T2
**Requirement**: ACTIVITY-03

**Done when**: `h.audit.Record(r.Context(), actor.ID, id, domain.Domain, "domain_verified")` (verify the exact in-scope variable name at that line first); test asserts the label.
**Tests**: unit
**Gate**: quick
**Commit**: `feat(domains): record domain string as domain_verified audit label`

---

### T10: `domain_deleted` call site captures the domain string BEFORE delete

**Where**: `internal/api/domains_handler.go:349` (+ test file)
**Depends on**: T2
**Requirement**: ACTIVITY-03, ACTIVITY-04

**Done when**: Domain string captured before the delete call runs (same ordering concern as T8); `Record(..., domainString, "domain_deleted")`; test covers the label surviving the delete.
**Tests**: unit
**Gate**: quick
**Commit**: `feat(domains): record domain string as domain_deleted audit label`

---

### T11: `status_page_deleted` call site captures the page name BEFORE delete

**Where**: `internal/api/status_pages_handler.go:272` (+ test file)
**Depends on**: T2
**Requirement**: ACTIVITY-03, ACTIVITY-04

**Done when**: Status page name captured before the delete call runs; `Record(..., pageName, "status_page_deleted")`; test covers the label surviving the delete.
**Tests**: unit
**Gate**: quick
**Commit**: `feat(status-pages): record page name as status_page_deleted audit label`

---

### T12: `status_page_domain_verified` call site passes the page name as label

**Where**: `internal/api/status_pages_handler.go:355` (+ test file)
**Depends on**: T2
**Requirement**: ACTIVITY-03

**Done when**: `Record(..., pageName, "status_page_domain_verified")`; test asserts the label. Last task of Phase 4 — run the Build gate.
**Tests**: unit
**Gate**: build
**Commit**: `feat(status-pages): record page name as status_page_domain_verified audit label`

---

### T13: `AuditLogRepository.ListRecent`

**What**: New repository, `ListRecent(ctx, limit) ([]AuditLogEntry, error)` per design.md.
**Where**: `internal/db/audit_log_repository.go` (test file updated alongside, per co-location rule)
**Depends on**: T1
**Requirement**: ACTIVITY-06, ACTIVITY-08

**Done when**:
- [ ] `ListRecent` query per design.md: `LEFT JOIN users`, `ORDER BY created_at DESC LIMIT $1`, `actor_deleted` computed from `u.id IS NULL`.
- [ ] Integration test: seeds rows across two tenants, confirms only the caller's tenant's rows return (RLS) — spec AC8.
- [ ] Integration test: seeds >20 rows, confirms `ListRecent(ctx, 20)` returns exactly 20 in `created_at DESC` order.
- [ ] Integration test: seeds a row whose actor's user was then hard-deleted, confirms `actor_deleted: true` and empty `actor_name`.
- [ ] Integration test: seeds a row with `target_label = NULL` (pre-feature style), confirms it comes back as a Go `nil`/empty, not an error.

**Tests**: integration
**Gate**: full
**Commit**: `feat(db): add AuditLogRepository.ListRecent`

---

### T14: `AuditLogHandler` + route wiring (phase 5 build gate)

**What**: New handler `GET /api/audit-log?limit=N`, wired into the router, no role gate.
**Where**: `internal/api/audit_log_handler.go` (test file, and `internal/cli/routes.go` wiring, updated alongside — cohesive pair per co-location rule; per this session's L-059 lesson, confirm reachability via a real-router test, not just a handler-level one, since this route has no role gate to assert but should still be proven wired)
**Depends on**: T13
**Requirement**: ACTIVITY-06, ACTIVITY-07

**Done when**:
- [ ] `limit` query param: default 5, invalid/≤0 → 5, >20 → 20 (spec AC6, edge cases).
- [ ] `401` with no session (spec AC7).
- [ ] `200` with the mapped JSON shape (`action`, `target_label`, `actor_name`, `actor_deleted`, `created_at`) for a valid session, any role.
- [ ] Route registered in `internal/cli/routes.go` under `protected` (no `ownerOnly`/role wrapper).
- [ ] Handler test covers: 401, default limit, explicit limit, limit>20 capping, invalid limit fallback, response shape for a known fixture.
- [ ] Build gate: `go build ./... && go vet ./... && gofmt -l <changed> && go test ./...` plus the full DB-touching integration gate (disposable Postgres container) since this phase's T13 touches the DB layer.

**Tests**: unit (handler, against a fake repository) + the integration coverage T13 already provides at the repository layer
**Gate**: build
**Commit**: `feat(api): add GET /api/audit-log endpoint`

---

### T15: Frontend `useRecentActivity` hook

**Where**: `web/src/features/overview/hooks.ts` (new file if none exists for overview-scoped hooks — check first) (+ test file)
**Depends on**: T14
**Requirement**: ACTIVITY-09

**Done when**:
- [ ] `useRecentActivity()` — `useQuery({ queryKey: ["audit-log"], queryFn: () => apiFetch("/api/audit-log?limit=5") })` (or the codebase's established hook-wrapping convention — check an existing simple hook like `useOverview` first).
- [ ] MSW handler added for `GET /api/audit-log` in `web/src/test/msw/handlers.ts`, mirroring the real backend's response shape exactly (per AGENTS.md §5 — this is the exact class of bug that rule exists to prevent).
- [ ] Test covers the happy path and an error path.

**Tests**: unit
**Gate**: quick (frontend)
**Commit**: `feat(overview): add useRecentActivity hook`

---

### T16: Frontend action-phrase mapping + fallback logic

**Where**: `web/src/features/overview/activityPhrases.ts` (new; test file co-located alongside per the co-located tests rule)
**Depends on**: T15
**Requirement**: ACTIVITY-10, ACTIVITY-11

**Done when**:
- [ ] All 8 actions map to their pt-BR/en phrases per `context.md`'s table, added as new `i18n.ts` keys (`overview.activity.<action>` or similar, following the file's existing nesting convention).
- [ ] An action string not in the map renders the generic fallback phrase (spec AC10, edge case).
- [ ] A `null`/missing `target_label` renders the phrase gracefully, omitting the target clause — never the literal string `"null"`/`"undefined"` (spec AC11).
- [ ] Unit tests cover: all 8 known actions render their exact phrase, one unknown action renders the fallback, one `null`-label entry renders gracefully per action type.

**Tests**: unit
**Gate**: quick (frontend)
**Commit**: `feat(overview): add audit action phrase mapping with fallback`

---

### T17: `RecentActivity` component rewrite (phase 6 build gate — last task of the feature)

**Where**: `web/src/features/overview/OverviewPage.tsx` (test file updated alongside, per co-location rule)
**Depends on**: T16
**Requirement**: ACTIVITY-09, ACTIVITY-12, ACTIVITY-13

**Done when**:
- [ ] `ACTIVITY_FEED` hardcoded array deleted entirely (spec AC9 — not just unused).
- [ ] `RecentActivity` uses `useRecentActivity()` + the T16 phrase mapping.
- [ ] Loading state uses the shared `Skeleton` primitive (spec AC12; reuse the same pattern the loading-skeletons feature already established for this exact page's other cards, e.g. `OverviewPage.tsx`'s summary-card skeletons).
- [ ] Error state matches the page's existing error-row pattern (spec AC12).
- [ ] Zero-entries state renders an explicit empty-state message, not a blank card (spec AC13).
- [ ] Test suite covers: real entries render with correct phrasing (including a `null`-label entry), loading shows skeleton, error shows the error row, empty tenant shows the empty-state message.
- [ ] Build gate (last task of the entire feature): `go build ./... && go vet ./... && gofmt -l <changed> && go test ./...` (Go side, in case any Go file was touched incidentally — expected to be none in this task) AND `npx tsc -b --noEmit && npm run test` (frontend, full suite).
- [ ] Feature-level Verifier dispatch (Execute step 9) — mandatory, not prompted.

**Tests**: unit
**Gate**: build
**Commit**: `feat(overview): wire RecentActivity card to real audit-log data`

---

## Phase Execution Map

```
Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5 → Phase 6

Phase 1:  T1 → T2
Phase 2:  T1 → T3
Phase 3:  T2 → {T4, T5, T7, T8} (numeric order)
Phase 3:  T2 → T6
Phase 3:  T3 → T6
Phase 4:  T2 → {T9, T10, T11, T12} (numeric order)
Phase 5:  T1 → T13 → T14
Phase 6:  T14 → T15 → T16 → T17
```

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1 | 1 migration pair | ✅ Granular |
| T2 | 1 function signature | ✅ Granular |
| T3 | 1 repository method | ✅ Granular |
| T4–T12 | 1 call site each | ✅ Granular |
| T13 | 1 new repository | ✅ Granular |
| T14 | 1 new handler + route wiring (cohesive pair) | ✅ Granular |
| T15 | 1 hook | ✅ Granular |
| T16 | 1 phrase-mapping module | ✅ Granular |
| T17 | 1 component rewrite | ✅ Granular |

## Diagram-Definition Cross-Check

| Task | Depends On (body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | — | ✅ Match |
| T2 | T1 | T1 → T2 | ✅ Match |
| T3 | T1 | T1 → T3 | ✅ Match |
| T4 | T2 | T2 → T4 | ✅ Match |
| T5 | T2 | T2 → T5 | ✅ Match |
| T6 | T2, T3 | T2 → T6, T3 → T6 | ✅ Match |
| T7 | T2 | T2 → T7 | ✅ Match |
| T8 | T2 | T2 → T8 | ✅ Match |
| T9 | T2 | T2 → T9 | ✅ Match |
| T10 | T2 | T2 → T10 | ✅ Match |
| T11 | T2 | T2 → T11 | ✅ Match |
| T12 | T2 | T2 → T12 | ✅ Match |
| T13 | T1 | T1 → T13 | ✅ Match |
| T14 | T13 | T13 → T14 | ✅ Match |
| T15 | T14 | T14 → T15 | ✅ Match |
| T16 | T15 | T15 → T16 | ✅ Match |
| T17 | T16 | T16 → T17 | ✅ Match |

## Test Co-location Validation

| Task | Code Layer | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1 | Migration | none | none | ✅ OK |
| T2 | `audit.Log` | unit | unit | ✅ OK |
| T3 | Repository (DB-touching) | integration | integration | ✅ OK |
| T4–T12 | Handler call site | unit | unit | ✅ OK |
| T13 | Repository (DB-touching, new) | integration | integration | ✅ OK |
| T14 | Handler (new) | unit + integration | unit (+ T13's integration coverage) | ✅ OK |
| T15–T17 | Frontend | unit | unit | ✅ OK |

---

## Tools

MCP: NONE required.
Skill: `tlc-spec-driven` for the Execute flow itself.
