# Notification Preferences and Incident Email Delivery Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/notification-preferences/design.md`
**Spec**: `.specs/features/notification-preferences/spec.md`
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase (`AGENTS.md` §3, sampled `internal/email/service_test.go`, `internal/cli/poller_manager_test.go`, `internal/api/incidents_handler_test.go`) and spec ACs - confirm before Execute. Guidelines found: `AGENTS.md` (backend gates, integration-test DB rule, `AD-013`'s Postgres-only cross-replica coordination rule - directly governs the digest scheduler's design). Backend-only, no `web/` changes this cycle.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Schema / migration (config) | none | Build gate only - migration applies cleanly | `internal/db/migrations/0032_*.sql` | `go build ./...` + apply on disposable Postgres |
| Repository (`NotificationPreferenceRepository`) | integration | Key query paths + default-resolution behavior + error paths | `internal/db/notification_preference_repository_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...` |
| `internal/notify` (`NotificationService`, pure orchestration over injected interfaces) | unit | All branches; 1:1 to NOTIFPREF-04..09; non-fatal-on-send-failure behavior | `internal/notify/service_test.go` | `go test ./internal/notify/...` |
| `internal/email` (3 new send methods) | unit | Matches existing `SendSignupVerification` test depth - active-provider lookup, `ErrNoActiveProvider`, render+send happy path | `internal/email/service_test.go` | `go test ./internal/email/...` |
| API handlers (`GET`/`PATCH /api/auth/notification-preferences`, incident hook points) | integration | All routes/hooks in scope: happy + every listed edge case + error paths, 1:1 to spec ACs | `internal/api/auth_handler_test.go`, `internal/api/incidents_handler_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...` |
| `DigestScheduler` (leader election + scheduling) | integration | Two-instance leader-election dedup proven via real Postgres advisory-lock contention (same discipline as `ha-multi-replica`'s corrected concurrency test), exactly-one-send-per-cycle | `internal/cli/digest_scheduler_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=... go test -tags=integration ./internal/cli/...` |

## Gate Check Commands

> Generated from `AGENTS.md` §3 - confirm before Execute.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick (Go, no DB) | After pure unit-test tasks (`internal/notify`, `internal/email` additions) | `go build ./... && go vet ./... && gofmt -l <changed files> && go test ./...` |
| Full (Go, DB-touching) | After tasks with integration tests (migration, repository, handlers, scheduler) | Spin disposable Postgres per `AGENTS.md` §3, then `TEST_DATABASE_URL=... go test -tags=integration ./...`, then destroy the container |
| Build (phase completion) | End of every phase | Quick + Full, both green |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Preferences Foundation

```
T1 → T2
T2 → T3
```

### Phase 2: Event-Driven Notifications

```
T2 → T4
T4 → T5
T5 → T6
```

### Phase 3: Weekly Digest

```
T2 → T7
T7 → T8
T8 → T9
```

---

## Task Breakdown

### T1: Migration 0032 - notification_preferences

**What**: New migration creating `notification_preferences` per `design.md`'s Data Models section, up + down.
**Where**: `internal/db/migrations/0032_notification_preferences.up.sql`
**Depends on**: None
**Reuses**: `CHECK` constraint enum convention already used by `incidents.severity` (`incident-severity-and-timeline`).
**Requirement**: NOTIFPREF-01

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `notification_preferences(user_id, notification_type CHECK IN (...), enabled, PK (user_id, notification_type))` created
- [ ] Migration applies and reverses cleanly on a disposable Postgres
- [ ] Gate check passes: `go build ./...`

**Tests**: none
**Gate**: build

**Commit**: `feat(db): add notification_preferences table`

---

### T2: `NotificationPreferenceRepository`

**What**: New repository: `Get`, `Upsert`, `ResolveEnabledForUsers`, per `design.md`.
**Where**: `internal/db/notification_preference_repository.go`
**Depends on**: T1
**Reuses**: Standard repository shape (`DomainRepository`).
**Requirement**: NOTIFPREF-01, NOTIFPREF-02, NOTIFPREF-03

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `Get` returns stored rows only; no default-application inside the repository (defaults are a caller concern per `design.md`)
- [ ] `Upsert` writes exactly the provided keys via `ON CONFLICT ... DO UPDATE`, leaves omitted types untouched
- [ ] `ResolveEnabledForUsers` correctly applies the documented default (`true`/`true`/`false`) for users with no row, in one batched query
- [ ] Gate check passes on disposable Postgres: `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...`
- [ ] Test count: ≥8 new subtests

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add NotificationPreferenceRepository`

---

### T3: `GET`/`PATCH /api/auth/notification-preferences`

**What**: Self-service read (with defaults applied) and partial-update endpoints.
**Where**: `internal/api/auth_handler.go`
**Depends on**: T2
**Reuses**: `profile-self-service`'s self-scoped, partial-update pattern (`UpdateProfile`).
**Requirement**: NOTIFPREF-01, NOTIFPREF-02, NOTIFPREF-03

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `GET` for a brand-new user with no rows returns the documented defaults
- [ ] `PATCH {weekly_digest: true}` changes only that key; a subsequent `GET` confirms the other two are unchanged
- [ ] Both endpoints are scoped to the caller - no request field can target another user
- [ ] Routes registered in `internal/cli/routes.go` behind `RequireAuth` only (`anyRole`, self)
- [ ] Gate check passes on disposable Postgres: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [ ] Test count: ≥5 new tests

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): add notification preferences read/write endpoints`

---

### T4: `email.Service` - `SendIncidentOpened`/`SendIncidentResolved`

**What**: Two new send methods mirroring `SendSignupVerification`'s shape, plus their email templates.
**Where**: `internal/email/service.go`
**Depends on**: T2
**Reuses**: `s.templates`, `s.repo.GetActiveProvider`/`Get`, `ErrNoActiveProvider`.
**Requirement**: NOTIFPREF-04, NOTIFPREF-05, NOTIFPREF-07, NOTIFPREF-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Both methods render and send through the active provider, matching `SendSignupVerification`'s error-passthrough shape
- [ ] Both return `ErrNoActiveProvider` with no provider connected, without calling any send API
- [ ] Gate check passes: `go build ./... && go vet ./... && gofmt -l internal/email/service.go && go test ./internal/email/...`
- [ ] Test count: ≥6 new tests

**Tests**: unit
**Gate**: quick

**Commit**: `feat(email): add incident-opened and incident-resolved send methods`

---

### T5: `NotificationService`

**What**: New package `internal/notify` implementing `NotifyIncidentOpened`/`NotifyIncidentResolved` per `design.md` - resolves recipients (`ListForTenant` filtered to owner/operator, then filtered by preference) and calls T4's send methods, logging (never returning) individual send failures.
**Where**: `internal/notify/service.go`
**Depends on**: T4
**Reuses**: `TenantMembershipRepository.ListForTenant`, `NotificationPreferenceRepository.ResolveEnabledForUsers` (T2), `email.Service` (T4).
**Requirement**: NOTIFPREF-04, NOTIFPREF-05, NOTIFPREF-06, NOTIFPREF-07, NOTIFPREF-08, NOTIFPREF-09

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Given a tenant with an opted-in owner, an opted-out operator, and an opted-in viewer, `NotifyIncidentOpened` sends exactly once (to the owner) - viewer never emailed regardless of preference
- [ ] A send failure for one recipient is logged and does not prevent or fail the call for the caller
- [ ] `NotifyIncidentResolved` uses the same recipient-resolution logic
- [ ] Gate check passes: `go build ./... && go vet ./... && gofmt -l internal/notify/service.go && go test ./internal/notify/...`
- [ ] Test count: ≥8 new tests (mocked membership/preference/email dependencies, per unit-test convention for this layer)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(notify): add NotificationService for incident opened/resolved`

---

### T6: Wire `IncidentsHandler.Create`/`Transition`/`ConfirmClose`

**What**: Each of the 3 confirmed hook points calls `NotificationService` after its existing success path, before writing the response; failure is logged only.
**Where**: `internal/api/incidents_handler.go`
**Depends on**: T5
**Reuses**: `ActiveTenantIDFromContext` (already available via tenant-context middleware).
**Requirement**: NOTIFPREF-04, NOTIFPREF-05, NOTIFPREF-06, NOTIFPREF-07, NOTIFPREF-08, NOTIFPREF-09

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Creating an incident fires `NotifyIncidentOpened`; disconnecting the active provider still returns `201`
- [ ] `Transition` to `resolved` fires `NotifyIncidentResolved`; transitioning to any other status does not
- [ ] `ConfirmClose` fires `NotifyIncidentResolved` unconditionally
- [ ] An auto-created incident (`AutoCreated = true`) still fires the opened notification (edge case)
- [ ] Gate check passes on disposable Postgres: `TEST_DATABASE_URL=... go test -tags=integration ./internal/api/...`
- [ ] Test count: ≥6 new tests

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): send incident-opened/resolved notifications from the incident lifecycle`

---

### T7: `email.Service.SendWeeklyDigest` + digest content assembly

**What**: New send method plus a small, pure content-assembly function (`internal/notify` or `internal/cli`, decided at Execute time per where it's consumed) computing a tenant's uptime summary (`status_intervals`/`internal/history`) and incident counts for the prior 7 days.
**Where**: `internal/email/service.go`
**Depends on**: T2
**Reuses**: `internal/history`, `StatusIntervalRepository`, `IncidentRepository`'s `created_at`/`resolved_at` filtering, `SendSignupVerification`'s send shape.
**Requirement**: NOTIFPREF-10, NOTIFPREF-12

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `SendWeeklyDigest` sends a rendered summary through the active provider
- [ ] Content assembly correctly aggregates a tenant's uptime and incident counts for the prior 7 days from existing data, no new metric invented
- [ ] Gate check passes: `go build ./... && go vet ./... && gofmt -l internal/email/service.go && go test ./internal/email/...`
- [ ] Test count: ≥5 new tests

**Tests**: unit
**Gate**: quick

**Commit**: `feat(email): add weekly digest send method and content assembly`

---

### T8: `DigestScheduler`

**What**: New type per `design.md`: computes next Monday 00:00 UTC, fires every 7 days, uses `pglock.TryAcquire` (a new, distinct lock key) to ensure exactly one replica sends per cycle, reuses `db.SystemTenantLister` to enumerate tenants under RLS.
**Where**: `internal/cli/digest_scheduler.go`
**Depends on**: T7
**Reuses**: `internal/pglock`, `db.SystemTenantLister` (`AD-024`), `PollerManager`'s leader-loop shape (structurally copied, not inherited, per `design.md`'s Tech Decisions).
**Requirement**: NOTIFPREF-10, NOTIFPREF-11, NOTIFPREF-12

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Two scheduler instances against the same database: exactly one instance's `runOnce` executes per triggered cycle, proven via real advisory-lock contention (not a same-process sequential call)
- [ ] A tenant with zero enabled `weekly_digest` recipients sends no email and builds no content for that tenant
- [ ] `TenantRepository.List`-under-RLS returns the real tenant set when called through `SystemTenantLister` (not zero rows, confirming `AD-024`'s mechanism generalizes correctly to this second caller)
- [ ] Gate check passes on disposable Postgres: `TEST_DATABASE_URL=... go test -tags=integration ./internal/cli/...`
- [ ] Test count: ≥6 new tests

**Tests**: integration
**Gate**: full

**Commit**: `feat(cli): add weekly digest scheduler with leader-election dedup`

---

### T9: Boot wiring

**What**: `DigestScheduler` starts at boot alongside `PollerManager` and stops on shutdown.
**Where**: `internal/cli/serve.go`
**Depends on**: T8
**Reuses**: The same start/stop lifecycle already used for `PollerManager` in this file.
**Requirement**: NOTIFPREF-10

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `vane serve` starts the digest scheduler goroutine and stops it cleanly on context cancellation, matching `PollerManager`'s existing shutdown behavior
- [ ] Gate check passes on disposable Postgres: `TEST_DATABASE_URL=... go test -tags=integration ./...`
- [ ] Test count: +1 new test (or existing `serve`-level test extended) confirming the scheduler is constructed and started

**Tests**: integration
**Gate**: full

**Commit**: `feat(cli): start the weekly digest scheduler at boot`

---

## Phase Execution Map

```
Phase 1 → Phase 2 → Phase 3

Phase 1:  T1 ------→ T2 ------→ T3
Phase 2:  T4 ------→ T5 ------→ T6
Phase 3:  T7 ------→ T8 ------→ T9
```

Phase 2 and Phase 3 each depend only on T2 (Phase 1) as their entry point - not on each other - but execute in the fixed phase order above since phases run sequentially by convention in this skill, not because of a real cross-phase dependency between them.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: Migration 0032 | 1 schema change | ✅ Granular |
| T2: NotificationPreferenceRepository | 1 repository | ✅ Granular |
| T3: Preferences endpoints | 1 cohesive read/write pair on one resource | ✅ Granular |
| T4: SendIncidentOpened/Resolved | 2 methods, 1 cohesive concern (event-driven incident email) | ✅ Granular |
| T5: NotificationService | 1 package, 1 concern | ✅ Granular |
| T6: Wire 3 incident hook points | 3 call sites, 1 cohesive concern (event notification wiring) | ✅ Granular |
| T7: SendWeeklyDigest + content assembly | 1 method + 1 pure function, 1 cohesive concern | ✅ Granular |
| T8: DigestScheduler | 1 component | ✅ Granular |
| T9: Boot wiring | 1 file, 1 concern | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | — | ✅ Match |
| T2 | T1 | T1 → T2 | ✅ Match |
| T3 | T2 | T2 → T3 | ✅ Match |
| T4 | T2 | T2 → T4 | ✅ Match |
| T5 | T4 | T4 → T5 | ✅ Match |
| T6 | T5 | T5 → T6 | ✅ Match |
| T7 | T2 | T2 → T7 | ✅ Match |
| T8 | T7 | T7 → T8 | ✅ Match |
| T9 | T8 | T8 → T9 | ✅ Match |

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1 | Schema / migration | none | none | ✅ OK |
| T2 | Repository | integration | integration | ✅ OK |
| T3 | API handler | integration | integration | ✅ OK |
| T4 | `internal/email` | unit | unit | ✅ OK |
| T5 | `internal/notify` | unit | unit | ✅ OK |
| T6 | API handler | integration | integration | ✅ OK |
| T7 | `internal/email` | unit | unit | ✅ OK |
| T8 | `internal/cli` (scheduler) | integration | integration | ✅ OK |
| T9 | `internal/cli` (boot wiring) | integration | integration | ✅ OK |
