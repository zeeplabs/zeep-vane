# Manual Polling Monitoring Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/manual-polling-monitoring/design.md`
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase sampling. Guidelines found: `AGENTS.md` §3 (backend gate: `go build`/`go test`/`go vet`/`gofmt`, `make test-integration` for DB-touching code), §5 (frontend: `react-i18next` for every user-facing string), §7 (poller/auth-adjacent changes are higher-risk - confirm before applying, already covered by this feature's Design phase). Existing test samples: `internal/poller/poller_test.go` (`//go:build integration`, real Postgres via `dbtest`), `internal/db/service_status_analysis_repository_test.go` (repo, integration), `internal/api/services_handler_test.go` (handler, integration, real tenant-scoped Postgres), `web/src/features/services/AddServiceDrawer.test.tsx` (frontend, vitest + MSW).

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Migration (`0034_service_polling_mode`) | integration | Applies clean (existing rows default to `monitor_mode='slo'`), reverses clean, `CHECK` constraints reject both invalid combinations | `internal/db/*_migration_test.go` | `make test-integration` |
| Repository (`ServiceRepository`) | integration | Create (both modes), `ListPollingManual` filtering, constraint-violation error surfaced | `internal/db/service_repository_test.go` | `make test-integration` |
| `internal/checks` (format/safety/actual check) | unit | All branches per check type (http/tcp/ping) x (valid/invalid format, safe/blocked range, success/failure/timeout) - no DB, self-contained (literal IPs + local `httptest`/`net.Listen` servers, no real DNS/external network) | `internal/checks/*_test.go` | `go test ./internal/checks/...` |
| Handler (`ServicesHandler.Create`, polling branch) | integration | Every new AC: happy path (both modes), SSRF-blocked 422, malformed-target 422, mode-field-crossing rejected (slo fields present in polling mode and vice versa) | `internal/api/services_handler_test.go` | `make test-integration` |
| `poller.ManualScheduler` | integration | Discovery of a new service, check-success -> operational, 2-consecutive-failure -> outage, single-failure carries status forward, `status_intervals` row shape identical to the Datadog poller's, goroutine count returns to baseline after `Run`'s ctx cancels (Risks & Concerns leak guard) | `internal/poller/manual_scheduler_test.go` | `make test-integration` |
| `PollerManager` (manual scheduler wiring) | integration | Starts on leadership acquired regardless of Datadog integration state, stops on leadership loss and on `Stop()`, independent of `Restart`'s own Datadog-only lifecycle | `internal/cli/poller_manager_test.go` | `make test-integration` |
| Frontend (`AddServiceDrawer`, `ServiceListPage`/`ServiceDetailDrawer` display) | unit | Every spec P3 AC + the list/detail display fallback for polling-manual rows (no `slo_name`/`slo_id` to show) | `web/src/features/services/*.test.tsx` | `npm run test` |

## Gate Check Commands

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick (frontend) | After a frontend-only task | `cd web && npx tsc -b --noEmit && npm run test` |
| Quick (Go unit, no DB) | After `internal/checks`-only task | `go build ./... && go vet ./... && go test ./internal/checks/...` |
| Full (backend, DB-touching) | After any task touching migration/repository/handler/poller/cli | `go build ./... && go vet ./... && gofmt -l $(git diff --name-only --diff-filter=ACM -- '*.go') && make test-integration` |
| Build (whole-feature) | After the last task, before Verifier | `go build ./... && go vet ./... && go test ./... && gofmt -l $(git diff --name-only --diff-filter=ACM -- '*.go') && make test-integration && cd web && npx tsc -b --noEmit && npm run test` |

---

## Execution Plan

### Phase 1: Data layer

```
T1 → T2
```

### Phase 2: Safe network checks

```
T2 → T3 → T4
```

### Phase 3: Handler

```
T4 → T5
```

### Phase 4: Scheduler

```
T5 → T6
```

### Phase 5: Poller lifecycle wiring

```
T6 → T7
```

### Phase 6: Frontend

```
T7 → T8 → T9
```

---

## Task Breakdown

### T1: Add `0034_service_polling_mode` migration

**What**: `slo_id` becomes nullable; add `monitor_mode` (default `'slo'`), `poll_type`, `poll_target`, `poll_interval_seconds`; add the three `CHECK` constraints from design.md (mode enum, poll_type enum, mode/field-combination exclusivity). Migration test covers: clean apply with existing rows defaulting to `monitor_mode='slo'`, both valid combinations accepted, both invalid combinations rejected by the constraint, clean reverse + re-apply.
**Where**: `internal/db/migrations/0034_service_polling_mode.up.sql`, `internal/db/migrations/0034_service_polling_mode.down.sql`, `internal/db/service_polling_mode_migration_test.go`
**Depends on**: None
**Reuses**: `internal/db/service_status_analysis_migration_test.go`'s apply/reverse/re-apply test shape
**Requirement**: MP-01, MP-02, MP-03, MP-05

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Migration applies cleanly against a database with existing `services` rows (all default to `monitor_mode='slo'`, `slo_id` unchanged)
- [x] `INSERT` with `monitor_mode='slo'` + `slo_id` set + all `poll_*` NULL succeeds; the reverse combination (`monitor_mode='polling'` + all three `poll_*` set + `slo_id` NULL) succeeds
- [x] `INSERT` with `monitor_mode='slo'` + a `poll_*` field set is rejected by the constraint; `monitor_mode='polling'` with `slo_id` set is rejected; `monitor_mode='polling'` missing any one of the three `poll_*` fields is rejected
- [x] Reverses cleanly, re-applies cleanly
- [x] Gate check passes: `make test-integration`

**Tests**: integration
**Gate**: Full

---

### T2: Extend `ServiceRepository` for polling mode

**What**: `Service` struct gains `MonitorMode string`, `PollType *string`, `PollTarget *string`, `PollIntervalSeconds *int`; `SLOID` becomes `*string`. `Create`'s `INSERT` branches its column list by `MonitorMode`. New `ListPollingManual(ctx) ([]Service, error)` (`WHERE monitor_mode = 'polling'`).
**Where**: `internal/db/service_repository.go`, `internal/db/service_repository_test.go`
**Depends on**: T1
**Reuses**: existing `Create`/`List` scan/error-wrap conventions in the same file
**Requirement**: MP-01, MP-02, MP-05, MP-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `Create` persists a polling-mode service correctly (all four new fields, `SLOID` nil) and an slo-mode service unchanged from today
- [x] `ListPollingManual` returns only `monitor_mode='polling'` rows, empty slice (not nil-panic) when none exist
- [x] A `Create` call violating the DB constraint (e.g. both `SLOID` and `PollType` set) surfaces the constraint violation as an error, not a silent partial insert
- [x] Gate check passes: `make test-integration`

**Tests**: integration
**Gate**: Full

---

### T3: `internal/checks.ValidateTargetFormat`

**What**: Pure-parsing format validation per check type - HTTP(S): `url.Parse`, requires scheme + host; TCP: `host:port` via `net.SplitHostPort`; Ping: bare host, optional `:port` (default `80` applied by `RunCheck`/`ValidateTargetSafety` downstream, not this function - this function only validates the *input* shape).
**Where**: `internal/checks/format.go`, `internal/checks/format_test.go`
**Depends on**: T2
**Reuses**: stdlib only
**Requirement**: MP-04

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] HTTP(S): rejects a bare host (no scheme), accepts a full URL with `http://` or `https://`
- [x] TCP: rejects a bare host (no port), accepts `host:port`
- [x] Ping: accepts a bare host, accepts `host:port`
- [x] An unrecognized `pollType` value returns an error (defensive - `ServicesHandler`'s own validation is expected to catch this first, but the function must not panic)
- [x] Gate check passes: `go test ./internal/checks/...`

**Tests**: unit
**Gate**: Quick (Go unit, no DB)

---

### T4: `internal/checks.ValidateTargetSafety` + `RunCheck`

**What**: Shared SSRF-safe `net.Dialer` (`Control` hook rejecting `127.0.0.0/8`/`10.0.0.0/8`/`172.16.0.0/12`/`192.168.0.0/16`/`169.254.0.0/16`). `ValidateTargetSafety` resolves the host (`net.DefaultResolver.LookupIPAddr`) and rejects if any resolved IP is blocked, without requiring reachability (a DNS-not-found target is allowed through, per design.md's Tech Decision). `RunCheck` performs the real check (HTTP GET via a client whose `Transport.DialContext` is the safe dialer; TCP/Ping via the safe dialer's own `DialContext` directly, Ping defaulting to port `80`), with the given timeout; 2xx/3xx = HTTP success, a completed dial = TCP/Ping success, anything else (including a `Control`-hook rejection - MP-15) is a failed check.
**Where**: `internal/checks/dial.go`, `internal/checks/safety.go`, `internal/checks/check.go`, `internal/checks/dial_test.go`, `internal/checks/safety_test.go`, `internal/checks/check_test.go`
**Depends on**: T3
**Reuses**: nothing pre-existing (design.md: first admin-supplied dynamic dial target in this codebase)
**Requirement**: MP-03, MP-07, MP-08, MP-09, MP-15

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `ValidateTargetSafety` rejects a literal `127.0.0.1`/`10.x`/`172.16-31.x`/`192.168.x`/`169.254.x` target for all three check types
- [x] `ValidateTargetSafety` allows a target whose DNS does not resolve at all (no error)
- [x] `RunCheck` against a local `httptest.Server` returning 200/301 succeeds; 500 and connection-refused both fail
- [x] `RunCheck` (TCP/Ping) against a local `net.Listen`-backed port succeeds; a closed/unreachable port fails
- [x] `RunCheck` against a literal blocked-range target fails via the dialer's own `Control` hook (proving the same safety net applies at check time, not only at `ValidateTargetSafety` time - MP-15)
- [x] `RunCheck` respects its timeout parameter (a deliberately slow/non-responding server fails within the given timeout, not hanging past it)
- [x] Gate check passes: `go test ./internal/checks/...`

**Tests**: unit
**Gate**: Quick (Go unit, no DB)

---

### T5: Extend `ServicesHandler.Create` for polling mode

**What**: `monitor_mode` defaults to `"slo"` when omitted (existing requests unaffected). When `"polling"`: requires `poll_type`/`poll_target`/`poll_interval_seconds` (one of 30/60/300), rejects any `slo_id`/`slo_name` present, runs `checks.ValidateTargetFormat` then `checks.ValidateTargetSafety`, 422 (fixed generic message, no raw resolver error) on any failure.
**Where**: `internal/api/services_handler.go`, `internal/api/services_handler_test.go`
**Depends on**: T4
**Reuses**: `internal/checks` (T3/T4); existing 422 response shape/tests in the same file
**Requirement**: MP-01, MP-02, MP-03, MP-04, MP-05

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Omitting `monitor_mode` behaves exactly as before (existing SLO-mode tests in this file still pass unmodified)
- [x] A valid polling-mode request (each of the 3 check types) creates the service with the right fields persisted
- [x] A polling-mode request with an SSRF-blocked target returns 422 and creates nothing
- [x] A polling-mode request with a malformed target for its check type returns 422 and creates nothing
- [x] A polling-mode request that also includes `slo_id` is rejected (422), and an slo-mode request that includes `poll_type`/`poll_target`/`poll_interval_seconds` is rejected (422)
- [x] Gate check passes: `make test-integration`

**Tests**: integration
**Gate**: Full

---

### T6: `poller.ManualScheduler`

**What**: Reconciliation loop (own ticker, `manualDiscoveryInterval` = 15s, immediate first pass) iterating tenants (`tenantLister`/`TenantTxFunc`, mirroring `Poller`'s own tenant iteration) and listing each tenant's `ListPollingManual` services; spawns one goroutine per newly-seen service ID, each running its own `time.Ticker` at `PollIntervalSeconds`, immediate-first-check-then-tick. Each check: `checks.RunCheck` (no DB access during the network call itself), then a short-lived tenant-scoped transaction (`tenantTx`) to `OpenOrExtend` (errorBudgetRemaining `0`) + `UpdateStatus`, using a local (per-goroutine, no shared map/lock) consecutive-failure counter: success -> `operational` + reset counter to 0; failure -> counter+1, status unchanged until counter reaches 2, then `outage`. Every per-service goroutine's context is an explicit child of `Run`'s own ctx, canceled when `Run` returns (Risks & Concerns: goroutine-leak guard).
**Where**: `internal/poller/manual_scheduler.go`, `internal/poller/manual_scheduler_test.go`
**Depends on**: T5
**Reuses**: `poller.TenantTxFunc`, the same fake-tenant-iteration test pattern `poller_test.go` already uses; `checks.RunCheck` (T4)
**Requirement**: MP-06, MP-07, MP-08, MP-09, MP-10, MP-11, MP-12, MP-13, MP-14, MP-15

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] A newly-created polling-manual service gets its first check within one reconciliation cycle + its own interval (test with a short interval)
- [x] A successful check writes `operational` and a `status_intervals` row identical in shape to what the Datadog poller writes for the same table
- [x] 1 failed check leaves the prior status unchanged (MP-10); the 2nd consecutive failure flips to `outage` (MP-11/MP-06); a success after failures resets the streak to 0
- [x] With zero polling-manual services configured, `Run` starts and returns cleanly on ctx cancellation without error (MP-09 - no-op, not an error)
- [x] Goroutine count (`runtime.NumGoroutine()` before/after, allowing for scheduler noise via a bounded-retry assertion) returns to baseline after `Run`'s ctx is canceled
- [x] A target that resolves to a blocked range on a later cycle (simulating DNS rebinding) is treated as a failed check, not a panic or a successful connect (MP-15)
- [x] Gate check passes: `make test-integration`

**Tests**: integration
**Gate**: Full

---

### T7: Wire `ManualScheduler` into `PollerManager`

**What**: `PollerManager` gains a second `manualCancel`/`manualDone` pair. `RunLeaderLoop`'s acquire branch starts the manual scheduler unconditionally (parallel to, not inside, the existing `m.Restart(ctx)` call) right after `m.leading.Store(true)`; the leadership-loss branch and `PollerManager.Stop()` stop it symmetrically under the same `m.mu` sections already guarding the Datadog pair. New `newManualSchedulerFromPool(pool, logger) *poller.ManualScheduler` builder in `internal/cli/serve.go` (mirrors `newPollerFromStoredIntegration`'s tenant-wiring, reusing `db.NewSystemTenantLister`/`poolTenantTx`).
**Where**: `internal/cli/poller_manager.go`, `internal/cli/serve.go`, `internal/cli/poller_manager_test.go`
**Depends on**: T6
**Reuses**: `db.NewSystemTenantLister`, `poolTenantTx` (both already used by the Datadog poller's own construction)
**Requirement**: MP-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Manual scheduler starts on leadership acquisition even when no Datadog integration is connected (the existing "poller not started" warning path for Datadog is untouched and independent)
- [ ] Manual scheduler stops on leadership loss and on `Stop()`, same as the Datadog poller
- [ ] Calling `Restart` (e.g. via a Datadog key rotation) does not restart or otherwise disturb an already-running manual scheduler
- [ ] Gate check passes: `make test-integration`

**Tests**: integration
**Gate**: Full

---

### T8: Extend `AddServiceDrawer` with the mode toggle

**What**: `monitorMode` state (`"slo" | "polling"`, default `"slo"`). "Fonte" chips gain a disabled, non-interactive "New Relic" option (no click handler wired, `aria-disabled="true"`, never sent in any request body). Polling mode renders check-type chips (HTTP(S)/TCP/Ping), a target `Field` whose label/placeholder switches per the spec's Assumptions table, and interval chips (30s/1min/5min). Submit sends only the fields for the active mode. `useCreateService`'s input type and `hooks.ts`'s `ServiceResponse`/create-request mapping extended for the new fields.
**Where**: `web/src/features/services/AddServiceDrawer.tsx`, `web/src/features/services/hooks.ts`, `web/src/features/services/AddServiceDrawer.test.tsx`, `web/src/features/services/hooks.test.ts`, `web/src/test/msw/handlers.ts`
**Depends on**: T7
**Reuses**: `ServiceListPage.tsx`'s existing chip-toggle visual pattern (border/bg-tint group)
**Requirement**: MP-16, MP-17, MP-18, MP-19, MP-20

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Drawer defaults to "Baseado em SLO" with today's existing fields/behavior unchanged
- [ ] Selecting "Polling manual" swaps in check-type/target/interval fields and hides "Fonte"/SLO-search
- [ ] Clicking the disabled "New Relic" chip does nothing (selection stays on Datadog, no state change)
- [ ] Submit is disabled until the active mode's required fields are filled; submitting a polling-mode request sends only `name`/`monitor_mode`/`poll_type`/`poll_target`/`poll_interval_seconds`, never `slo_id`
- [ ] Every new string added goes through `react-i18next` (pt-BR keys added)
- [ ] MSW handlers mirror the extended request/response contract
- [ ] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test`

**Tests**: unit
**Gate**: Quick (frontend)

---

### T9: Polling-manual row display in `ServiceListPage`/`ServiceDetailDrawer`

**What**: Both currently render `service.slo_name || service.slo_id` as the row/drawer subtitle - undefined for a polling-manual service (no SLO fields at all). Both now render `service.poll_target` when `monitor_mode === "polling"`, falling back to the existing `slo_name || slo_id` otherwise.
**Where**: `web/src/features/services/ServiceListPage.tsx`, `web/src/features/services/ServiceDetailDrawer.tsx`, `web/src/features/services/ServiceListPage.test.tsx`, `web/src/features/services/ServiceDetailDrawer.test.tsx`
**Depends on**: T8
**Reuses**: existing subtitle rendering already in both files (extended, not duplicated - a small shared helper is fine if it avoids repeating the same ternary twice)
**Requirement**: MP-01 (list/detail must render every service correctly regardless of mode - implied by the spec's own "no special-casing in read paths" success criterion, made concrete for the two frontend read surfaces `internal/history`/`OverviewHandler` don't cover)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] A polling-manual service's row/drawer subtitle shows its `poll_target`, not blank/undefined
- [ ] An slo-mode service's subtitle is unchanged (regression-checked against existing tests)
- [ ] Gate check passes: `cd web && npx tsc -b --noEmit && npm run test`
- [ ] Whole-feature Build gate passes: `go build ./... && go vet ./... && go test ./... && gofmt -l $(git diff --name-only --diff-filter=ACM -- '*.go') && make test-integration && cd web && npx tsc -b --noEmit && npm run test`

**Tests**: unit
**Gate**: Build

**Commit**: `feat(services): add manual HTTP/TCP/Ping polling as a second monitoring mode`

---

## Phase Execution Map

```
Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5 → Phase 6

Phase 1:  T1 ------→ T2
Phase 2:            T2 ------→ T3 ------→ T4
Phase 3:                                 T4 ------→ T5
Phase 4:                                            T5 ------→ T6
Phase 5:                                                       T6 ------→ T7
Phase 6:                                                                  T7 ------→ T8 ------→ T9
```

Execution is strictly sequential - there is no intra-phase parallelism.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: migration + test | 1 migration pair + 1 test file | ✅ Granular |
| T2: `ServiceRepository` extend | 1 file (+ its test file) | ✅ Granular |
| T3: `ValidateTargetFormat` | 1 function, 1 file | ✅ Granular |
| T4: `ValidateTargetSafety` + `RunCheck` + shared dialer | 3 small files, one cohesive unit (dialer is shared state both functions need - splitting it out would just relocate an import, not reduce coupling) | ✅ Granular |
| T5: `ServicesHandler.Create` extend | 1 file (+ its test file) | ✅ Granular |
| T6: `ManualScheduler` | 1 new type, 1 file | ✅ Granular |
| T7: `PollerManager` wiring | 2 files (manager + production builder) - cohesive, the builder is meaningless without the manager change it feeds | ✅ Granular |
| T8: `AddServiceDrawer` + hooks | 2 files (component + its data hook) - cohesive, same reasoning as `monitored-services-page`'s own T7 | ✅ Granular |
| T9: list/detail display fallback | 2 files, same one-line change repeated in two render paths | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | (start of Phase 1) | ✅ Match |
| T2 | T1 | T1 → T2 | ✅ Match |
| T3 | T2 | T2 → T3 | ✅ Match |
| T4 | T3 | T3 → T4 | ✅ Match |
| T5 | T4 | T4 → T5 | ✅ Match |
| T6 | T5 | T5 → T6 | ✅ Match |
| T7 | T6 | T6 → T7 | ✅ Match |
| T8 | T7 | T7 → T8 | ✅ Match |
| T9 | T8 | T8 → T9 | ✅ Match |

Strict linear chain, one task at a time; broader `Reuses` relationships (e.g. T6 reusing T4's `RunCheck`, already satisfied by sequential execution) are documented in each task's `Reuses` field, not re-added as extra `Depends on` entries. No task depends on a later-phase task.

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1: migration | Migration | integration | integration | ✅ OK |
| T2: `ServiceRepository` | Repository | integration | integration | ✅ OK |
| T3: `ValidateTargetFormat` | `internal/checks` | unit | unit | ✅ OK |
| T4: `ValidateTargetSafety`/`RunCheck` | `internal/checks` | unit | unit | ✅ OK |
| T5: `ServicesHandler.Create` | Handler | integration | integration | ✅ OK |
| T6: `ManualScheduler` | Poller | integration | integration | ✅ OK |
| T7: `PollerManager` wiring | Poller lifecycle (cli) | integration | integration | ✅ OK |
| T8: `AddServiceDrawer` | Frontend component | unit | unit | ✅ OK |
| T9: list/detail display | Frontend component | unit | unit (+ Build gate closeout) | ✅ OK |

No violations.

---

## Tips

(n/a - execution-facing template section, no additional content needed)
