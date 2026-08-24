# Service Status Intervals Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/service-status-intervals/design.md`
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase sampling. Guidelines found: none (no `AGENTS.md`, `CONTRIBUTING.md`, or CI coverage gate in this repo) - strong defaults applied, floored against existing test depth per layer (sampled `internal/db/status_snapshot_repository_test.go`, `internal/db/status_page_repository_test.go`, `internal/poller/poller_test.go`, `internal/history/hourly_test.go`, `internal/api/public_status_handler_test.go`, `internal/cli/poller_manager_test.go`, `internal/cli/serve_test.go`). All DB/wiring-touching tests in this codebase carry `//go:build integration` and require `TEST_DATABASE_URL`; only `internal/history` is pure/untagged.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Domain / pure logic (`internal/history`) | unit | All branches; 1:1 to spec ACs (SHU-06..15); every listed edge case | `internal/history/*_test.go` (no build tag) | `go test ./internal/history/...` |
| Repository (`internal/db`) | integration | Key query paths + error/race handling; 1:1 to write-path ACs (SHU-01..05) and read/prune-path ACs (SHU-16..20) | `internal/db/*_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=<dsn> go test -tags=integration ./internal/db/...` |
| Migration (`internal/db/migrations`) | integration | Applies clean; every new index present; old table gone | `internal/db/status_intervals_migration_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=<dsn> go test -tags=integration ./internal/db/...` |
| Poller wiring (`internal/poller`) | integration | Happy path + failure path, matching existing `poller_test.go` depth | `internal/poller/*_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=<dsn> go test -tags=integration ./internal/poller/...` |
| Retention (`internal/retention`, new) | integration | Tick-triggers-delete happy path + ctx-cancel shutdown + delete-error-logged-and-continues | `internal/retention/*_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=<dsn> go test -tags=integration ./internal/retention/...` |
| HTTP handler (`internal/api`) | integration | All routes in scope: happy path + every listed edge case (no_data, zero services, undefined uptime %) | `internal/api/public_status_handler_test.go`, `internal/api/public_status_preview_handler_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=<dsn> go test -tags=integration ./internal/api/...` |
| Process wiring (`internal/cli/serve.go`, `internal/cli/routes.go`) | none (regression via existing suite) | No new logic introduced - existing `internal/cli/*_test.go` integration suite must keep passing unmodified in intent (only its repository construction call sites change) | `internal/cli/*_test.go` | Full gate (below) |
| Deleted code (`status_snapshots` repository/migration/table) | none | Removal verified by absence of references + full suite green | n/a | Build gate |

## Gate Check Commands

> Generated from codebase (`Makefile`, `README.md` "Running tests"). Guidelines found: `Makefile` (`test`, `lint`, `vet` targets), `README.md` "Running tests" section.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick | After a task touching only `internal/history` (pure, no DB) | `go test ./internal/history/...` |
| Full | After a task touching `internal/db`, `internal/poller`, `internal/retention`, or `internal/api` | `TEST_DATABASE_URL=<dsn> go test -tags=integration ./...` |
| Build | After phase completion, wiring-only tasks, or the final cleanup task | `go build ./... && go vet ./... && gofmt -l . && TEST_DATABASE_URL=<dsn> go test -tags=integration ./... && go test ./...` |

`<dsn>` is whatever `TEST_DATABASE_URL` is already set to in the executing shell (per `README.md` "Running tests" - not a new requirement this feature introduces).

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Data foundation - migration and repository

```
T1 → T2 → T3
```

### Phase 2: Pure bucketing and uptime logic

```
T2 → T4 → T5
```

### Phase 3: Writer and pruner wiring

```
T3 → T6 → T7
```

### Phase 4: Public HTTP response

```
T3 → T8
T5 → T8
```

### Phase 5: Process wiring and cleanup

```
T7 → T9 → T10
T8 → T9
```

---

## Task Breakdown

### T1: Migration `0014_status_intervals` (up + down) with data-integrity indexes [x]

**What**: Add `internal/db/migrations/0014_status_intervals.up.sql` creating `status_intervals` (columns: `id`, `service_id`, `status`, `error_budget_remaining`, `starts_at`, `last_seen_at`, `ends_at` nullable) with the unique partial index (`service_id` WHERE `ends_at IS NULL`), the `(service_id, starts_at)` index, the partial `ends_at` index, and `DROP TABLE status_snapshots`; add the matching `.down.sql` reversing both (drop `status_intervals`, recreate `status_snapshots` per `0005_status_snapshots.up.sql`). Add `internal/db/status_intervals_migration_test.go` verifying the migration applies clean, all three new indexes exist, and `status_snapshots` no longer exists.
**Where**: `internal/db/migrations/0014_status_intervals.up.sql`, `internal/db/migrations/0014_status_intervals.down.sql`, `internal/db/status_intervals_migration_test.go`
**Depends on**: None
**Reuses**: `internal/db/migrations/0005_status_snapshots.up.sql` (down-migration recreation), the migration-test pattern in `internal/db/status_snapshots_migration_test.go` (`pg_indexes` assertions, `MigrateUp`, `testDatabaseURL`)
**Requirement**: SHU-04, SHU-16 (schema half)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `0014_status_intervals.up.sql` creates the table, all three indexes, and drops `status_snapshots`
- [x] `0014_status_intervals.down.sql` reverses both directions cleanly (`MigrateUp` then `MigrateDown` round-trips without error)
- [x] Migration test asserts: applies clean; unique partial index on `(service_id) WHERE ends_at IS NULL` exists; `(service_id, starts_at)` index exists; `status_snapshots` is absent from `pg_tables`
- [x] Gate check passes: `TEST_DATABASE_URL=<dsn> go test -tags=integration ./internal/db/...` (new migration tests pass; pre-existing `status_snapshot_repository_test.go`/`status_snapshots_migration_test.go` now fail because the table they exercise is dropped by this migration - expected transitional state per design.md's full-replacement decision, resolved when T10 deletes those superseded files)
- [x] Test count: 1 new test file, 2 tests / 8 assertions (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add status_intervals table, drop status_snapshots (0014)`

---

### T2: `StatusIntervalRepository.OpenOrExtend` - transactional write path [x]

**What**: Add `internal/db/status_interval_repository.go` with the `StatusInterval` struct and `StatusIntervalRepository.OpenOrExtend(ctx, serviceID, status string, errorBudgetRemaining float64, at time.Time) error`, implementing the `SELECT ... FOR UPDATE` read-then-branch (Approach A): no open row → INSERT; same status → UPDATE `error_budget_remaining`/`last_seen_at` in place; different status → UPDATE old row's `ends_at`, INSERT new open row. Add `internal/db/status_interval_repository_test.go`.
**Where**: `internal/db/status_interval_repository.go`, `internal/db/status_interval_repository_test.go`
**Depends on**: T1
**Reuses**: the `SELECT ... FOR UPDATE` transaction shape from `internal/db/status_page_repository.go`'s `AttachDomain`
**Requirement**: SHU-01, SHU-02, SHU-03, SHU-04, SHU-05

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] First-ever observation for a service inserts exactly one open interval (`ends_at` NULL, `starts_at == at`)
- [x] A repeated identical status updates `error_budget_remaining` and `last_seen_at` on the existing open row without inserting a new one
- [x] A different status closes the previous open row (`ends_at == at`) and opens exactly one new row starting at the same `at`
- [x] A second writer attempting to open a row for a service that already has an open interval (simulating the `PollerManager.Restart` race, design Risks & Concerns) receives the unique-constraint error (`ErrIntervalRaceLost`), and the first writer's row is left untouched
- [x] Gate check passes: `TEST_DATABASE_URL=<dsn> go test -tags=integration ./internal/db/...` (new tests pass; pre-existing snapshot tests still fail per T1's documented transitional state)
- [x] Test count: 4 tests added (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add StatusIntervalRepository.OpenOrExtend transactional write path`

---

### T3: `StatusIntervalRepository` read and prune methods [x]

**What**: Add `OpenIntervalsByService(ctx) (map[string]StatusInterval, error)`, `ListOverlapping(ctx, serviceIDs []string, windowStart, now time.Time) ([]StatusInterval, error)`, and `DeleteClosedBefore(ctx, cutoff time.Time) (int64, error)` to `StatusIntervalRepository`, with tests covering overlap boundary conditions (interval fully inside window, interval spanning the window edge, still-open interval counted up to `now`, non-overlapping interval excluded, empty `serviceIDs`) and prune boundary conditions (closed-and-older-than-cutoff deleted; closed-but-newer-than-cutoff kept; open interval never deleted regardless of age).
**Where**: `internal/db/status_interval_repository.go` (extend), `internal/db/status_interval_repository_test.go` (extend)
**Depends on**: T2
**Reuses**: the `(service_id, starts_at)` index from T1; the empty-input short-circuit contract from the old `ListRecentByServices`
**Requirement**: SHU-06 through SHU-20 (read/prune data access)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `OpenIntervalsByService` returns only services with a currently-open interval, keyed correctly
- [x] `ListOverlapping` returns every interval overlapping `[windowStart, now]` including an open interval and one spanning the window's left edge, and excludes one entirely before `windowStart`
- [x] `ListOverlapping` with empty `serviceIDs` returns an empty slice, no error, no query executed
- [x] `DeleteClosedBefore` deletes only rows with `ends_at IS NOT NULL AND ends_at < cutoff`, leaving open rows and recently-closed rows untouched, and returns the correct deleted count
- [x] Gate check passes: `TEST_DATABASE_URL=<dsn> go test -tags=integration ./internal/db/...` (new tests pass; pre-existing snapshot tests still fail per T1's documented transitional state)
- [x] Test count: 4 tests added in this task (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add StatusIntervalRepository read and pruning queries`

---

### T4: `internal/history.BuildHourly` - worst-status-wins over intervals [x]

**What**: Change `BuildHourly`'s input type from `[]db.StatusSnapshot` to `[]db.StatusInterval` and its bucket-resolution rule from last-observation-wins to highest-priority-status-wins (`outage` > `degraded` > `operational`) among every interval overlapping each hour's `[start, start+1h)`; a bucket with no overlapping interval stays `NoData`. Rewrite `internal/history/hourly_test.go` accordingly (existing snapshot-based tests are replaced, not merely edited around).
**Where**: `internal/history/hourly.go`, `internal/history/hourly_test.go`
**Depends on**: T2 (needs the `db.StatusInterval` type)
**Reuses**: `HourlyBucket` struct, `NoData` constant, the package's existing pure/no-I/O doc convention
**Requirement**: SHU-06, SHU-07, SHU-08, SHU-09

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] An hour containing both `operational` (55 min) and `outage` (5 min) intervals resolves to `outage` (SHU-06 core case, replaces the old last-wins test)
- [x] An hour with no overlapping interval resolves to `NoData`
- [x] An interval open-ended (`ends_at` nil) still overlapping is counted as covering up to `now`/the current bucket
- [x] An interval spanning multiple bucket boundaries contributes its status to every bucket it overlaps, not only the one containing its `starts_at`
- [x] Function still returns exactly `windowHours` buckets, oldest first, unchanged contract
- [x] Gate check passes: `go test ./internal/history/...`
- [x] Test count: 7 tests (replaces prior 7 snapshot-based tests; no coverage regression)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(history): resolve hourly buckets by worst overlapping status, not last snapshot`

---

### T5: `internal/history.UptimePercent` [x]

**What**: Add `internal/history/uptime.go` with `UptimePercent(intervals []db.StatusInterval, windowStart, now time.Time) (percent float64, ok bool)`: denominator start clips to the latest of `windowStart` or the earliest interval's `starts_at` among `intervals`; downtime is the summed overlap duration of every `outage`-status interval with `[denominatorStart, now]`; result is clamped to `[0, 100]` then floored to one decimal. `ok=false` when `intervals` is empty or the clipped denominator is zero/negative.
**Where**: `internal/history/uptime.go`, `internal/history/uptime_test.go`
**Depends on**: T4
**Reuses**: nothing beyond stdlib `time`/`math`; sibling to `hourly.go` in the same pure package
**Requirement**: SHU-10, SHU-11, SHU-12, SHU-13, SHU-14, SHU-15

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] A 24h window with exactly 6h of `outage` and the rest `operational` returns `(75.0, true)`
- [x] A service whose earliest interval starts 2h into the window uses a 2h denominator, returning `(100.0, true)` when always `operational` in that span
- [x] Zero intervals returns `(_, false)`
- [x] `degraded` intervals do not count as downtime (only `outage` does, per confirmed decision)
- [x] A pathological case that would compute outside `[0, 100]` is clamped before rounding
- [x] Rounding always floors (a case that would round up under normal rounding is asserted to floor instead, e.g. `99.97` → `99.9`, not `100.0`)
- [x] Gate check passes: `go test ./internal/history/...`
- [x] Test count: 7 tests (no silent deletions)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(history): add UptimePercent with outage-only downtime and floor rounding`

---

### T6: Wire `internal/poller` to `OpenOrExtend` [x]

**What**: Replace the `snapshotCreator` interface (`Create`) with a `statusIntervalWriter` interface (`OpenOrExtend`) in `internal/poller/poller.go`; `pollService` calls `p.statusIntervals.OpenOrExtend(ctx, svc.ID, current, status.ErrorBudgetRemaining, time.Now())` instead of `p.snapshots.Create`. Update `internal/poller/poller_test.go`'s fakes and assertions to match (assert an interval was opened/extended, not that a snapshot row was created).
**Where**: `internal/poller/poller.go`, `internal/poller/poller_test.go`
**Depends on**: T3
**Reuses**: existing `Poller` struct shape, `NewPoller` constructor pattern, existing test fakes-per-dependency convention
**Requirement**: SHU-01, SHU-02, SHU-03 (poller as the sole real-world caller)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `pollService` on success calls `OpenOrExtend` with the normalized status and the real fetch time, never `time.Now()` called a second time inconsistently
- [x] `pollService` on a Datadog fetch failure still does not call `OpenOrExtend` at all (existing failure-path contract preserved)
- [x] Existing `TestPoller_Run_StopsOnContextCancel` still passes unmodified in intent
- [x] Gate check passes: `TEST_DATABASE_URL=<dsn> go test -tags=integration ./internal/poller/...`
- [x] Test count: 3 existing tests updated, 0 net test count regression

**Tests**: integration
**Gate**: full

**Commit**: `refactor(poller): write status intervals instead of per-poll snapshots`

---

### T7: `internal/retention.Pruner` [x]

**What**: New package `internal/retention` with `Pruner` (fields: an `intervalDeleter` interface with `DeleteClosedBefore(ctx, cutoff) (int64, error)`, `tick`/`retention time.Duration`, `*zap.Logger`), `NewPruner(...)`, and `Run(ctx context.Context)` - same ticker/`select`/`ctx.Done()` shape as `Poller.Run`, calling `DeleteClosedBefore(ctx, time.Now().Add(-p.retention))` each tick and logging (not crashing) on error.
**Where**: `internal/retention/pruner.go`, `internal/retention/pruner_test.go`
**Depends on**: T6
**Reuses**: the goroutine/ticker/shutdown pattern from `internal/poller/poller.go`'s `Run`

**Requirement**: SHU-16, SHU-17, SHU-18, SHU-19, SHU-20

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] A tick calls `DeleteClosedBefore` with a cutoff of `now - 35 days` and a closed interval older than that is gone afterward
- [x] An open interval, regardless of how old its `starts_at` is, is never deleted by a tick
- [x] `Run` returns promptly when its context is canceled, mid-tick-wait
- [x] A `DeleteClosedBefore` error is logged via `zap.Error` and does not stop `Run`'s loop (asserted by forcing one failing tick, e.g. a canceled sub-context or closed pool for that call, then confirming a subsequent tick still runs)
- [x] Gate check passes: `TEST_DATABASE_URL=<dsn> go test -tags=integration ./internal/retention/...`
- [x] Test count: 3 tests (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(retention): add Pruner deleting closed status intervals older than 35 days`

---

### T8: Wire `internal/api/public_status_handler.go` to intervals + uptime % [x]

**What**: Replace the `latestSnapshotFetcher` interface with a `statusIntervalReader` interface (`OpenIntervalsByService`, `ListOverlapping`); `composeResponse` calls `ListOverlapping` once per request (shared by `history.BuildHourly` and `history.UptimePercent`), reads `LastUpdatedAt` from `OpenIntervalsByService`'s `LastSeenAt`, and adds an `UptimePercent *float64` field to `publicServiceResponse` (nil when `UptimePercent`'s `ok` is `false`, per SHU-15). Update `internal/api/public_status_handler_test.go` and `internal/api/public_status_preview_handler_test.go` (both consume `composeResponse`) - existing 24-bucket/no_data assertions stay, worst-status-wins assertions and the new `uptime_percent` field get new/updated tests.
**Where**: `internal/api/public_status_handler.go`, `internal/api/public_status_handler_test.go`, `internal/api/public_status_preview_handler_test.go`
**Depends on**: T3, T5
**Reuses**: `composeResponse`'s existing shared-by-production-and-preview shape (no separate wiring needed for the preview endpoint, same pattern as UPT-08 in `public-status-hourly-history`)

**Requirement**: SHU-06 through SHU-15 (surfaced to the public response)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `publicServiceResponse.uptime_percent` is `null` in JSON when a service has no recorded intervals in the window (SHU-15), never `0` or `100`
- [x] An hour with a real outage inside it renders as `outage` in the response's `hourly_history`, exercised end-to-end through the handler (not just at the `internal/history` unit level)
- [x] `LastUpdatedAt` reflects the open interval's `last_seen_at`, confirmed to advance on a repeated-same-status poll (regression guard for the design's stated risk: without `last_seen_at`, this would freeze)
- [x] Both the production `Get` and the authenticated preview endpoint return the same shape (existing shared-`composeResponse` contract preserved)
- [x] Gate check passes: `TEST_DATABASE_URL=<dsn> go test -tags=integration ./internal/api/...`
- [x] Test count: ≥4 new/updated tests across the two files (no silent deletions)

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): serve worst-status hourly bars and uptime % from status intervals`

---

### T9: Wire `internal/cli/serve.go` and `internal/cli/routes.go` to the new repository and start the Pruner [x]

**What**: Replace both `db.NewStatusSnapshotRepository(pool)` call sites (`serve.go`'s poller construction and `newHTTPSServer`) and the one in `routes.go` with `db.NewStatusIntervalRepository(pool)`; add one more `go func()` in `RunE` starting `retention.NewPruner(intervals, 1*time.Hour, 35*24*time.Hour, logger).Run(ctx)`, canceled by the same `ctx`/`stop()` already wired for the poller and HTTP/HTTPS listeners.
**Where**: `internal/cli/serve.go`, `internal/cli/routes.go`
**Depends on**: T7, T8
**Reuses**: the existing `go func()` + shared `ctx` shutdown pattern already used for the two listener goroutines in `RunE`

**Requirement**: SHU-16 (process wiring half)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] All three `db.NewStatusSnapshotRepository(pool)` call sites now read `db.NewStatusIntervalRepository(pool)`
- [x] `RunE` starts the `Pruner`'s `Run` in its own goroutine, canceled on the same shutdown path as the poller (`stop()`/`ctx.Done()`)
- [x] Existing `internal/cli/serve_test.go` tests (`TestNewHTTPSServer_*`) still pass unmodified in intent - they exercise `newHTTPSServer`'s routing, which is unaffected by the repository type swap
- [x] Gate check passes: `TEST_DATABASE_URL=<dsn> go test -tags=integration ./...`
- [x] Test count: 0 new tests required (wiring-only); existing suite must show 0 regressions

**Tests**: none (regression via existing suite)
**Gate**: full

**Commit**: `feat(cli): wire status-interval repository and start the retention pruner in serve`

---

### T10: Remove the old snapshot repository and migration test

**What**: Delete `internal/db/status_snapshot_repository.go`, `internal/db/status_snapshot_repository_test.go`, and `internal/db/status_snapshots_migration_test.go` (superseded by T1-T3's interval equivalents); grep-confirm zero remaining references to `StatusSnapshot`, `StatusSnapshotRepository`, or `status_snapshots` anywhere in `internal/` or `cmd/`.
**Where**: `internal/db/status_snapshot_repository.go` (delete), `internal/db/status_snapshot_repository_test.go` (delete), `internal/db/status_snapshots_migration_test.go` (delete)
**Depends on**: T9
**Reuses**: n/a (deletion task)

**Requirement**: SHU-04 (full replacement, closing the loop)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] The three files are deleted
- [ ] `grep -rn "StatusSnapshot\|status_snapshots" internal/ cmd/` returns no matches
- [ ] `go build ./...` succeeds with the files gone (proves nothing else still referenced them)
- [ ] Gate check passes: `go build ./... && go vet ./... && gofmt -l . && TEST_DATABASE_URL=<dsn> go test -tags=integration ./... && go test ./...`
- [ ] Test count: 0 (deletion task; the full suite's existing count, minus the deleted files' tests, must still be 100% green)

**Tests**: none
**Gate**: build

**Commit**: `chore(db): remove superseded status_snapshots repository and migration test`

---

## Phase Execution Map

```
T1 -> T2
T2 -> T3
T2 -> T4
T4 -> T5
T3 -> T6
T6 -> T7
T3 -> T8
T5 -> T8
T7 -> T9
T8 -> T9
T9 -> T10
```

Execution is strictly sequential - there is no intra-phase parallelism. Every dependency in the Task Breakdown above has a matching arrow here (and vice versa): T2 depends on T1; T3 and T4 depend on T2; T5 depends on T4 (transitively on T2); T6 depends on T3; T7 depends on T6 (transitively on T3); T8 depends on T3 and T5 (transitively on T2 and T4); T9 depends on T7 and T8 (transitively on T6); T10 depends on T9.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: Migration + migration test | 1 up/1 down SQL file + 1 test file | ✅ Granular |
| T2: `OpenOrExtend` write path | 1 method (+ struct it introduces) in 1 file | ✅ Granular |
| T3: Read/prune methods | 3 related methods, same file, same repository - cohesive | ✅ Granular (2-3 related things in same file, cohesive) |
| T4: `BuildHourly` rewrite | 1 function in 1 file | ✅ Granular |
| T5: `UptimePercent` | 1 function in 1 new file | ✅ Granular |
| T6: Poller wiring | 1 interface swap + 1 call site in 1 file | ✅ Granular |
| T7: `Pruner` | 1 new type + 2 methods in 1 new file | ✅ Granular |
| T8: Handler wiring | 1 interface swap + `composeResponse` changes in 1 file | ✅ Granular |
| T9: Process wiring | 3 call-site swaps + 1 goroutine addition, 2 files | ✅ Granular (cohesive wiring change) |
| T10: Cleanup deletion | 3 file deletions, 1 grep verification | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | (no incoming arrow) | ✅ Match |
| T2 | T1 | T1 -> T2 | ✅ Match |
| T3 | T2 | T2 -> T3 | ✅ Match |
| T4 | T2 | T2 -> T4 | ✅ Match |
| T5 | T4 | T4 -> T5 | ✅ Match |
| T6 | T3 | T3 -> T6 | ✅ Match |
| T7 | T6 | T6 -> T7 | ✅ Match |
| T8 | T3, T5 | T3 -> T8, T5 -> T8 | ✅ Match |
| T9 | T7, T8 | T7 -> T9, T8 -> T9 | ✅ Match |
| T10 | T9 | T9 -> T10 | ✅ Match |

No dependency points to a later phase. All backward or within the same phase.

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1: Migration | Migration | integration | integration | ✅ OK |
| T2: `OpenOrExtend` | Repository | integration | integration | ✅ OK |
| T3: Read/prune methods | Repository | integration | integration | ✅ OK |
| T4: `BuildHourly` | Domain/pure logic | unit | unit | ✅ OK |
| T5: `UptimePercent` | Domain/pure logic | unit | unit | ✅ OK |
| T6: Poller wiring | Poller wiring | integration | integration | ✅ OK |
| T7: `Pruner` | Retention | integration | integration | ✅ OK |
| T8: Handler wiring | HTTP handler | integration | integration | ✅ OK |
| T9: Process wiring | Process wiring | none (regression via existing suite) | none | ✅ OK |
| T10: Cleanup | Deleted code | none | none | ✅ OK |

No violations. No task defers its required tests to a later task.

---

## Requirement Coverage

All 20 `SHU-*` ACs from `spec.md` map to at least one task above: SHU-01/02/03/05 → T2; SHU-04 → T1 (schema) + T10 (closing the loop); SHU-06..09 → T4 (+ surfaced in T8); SHU-10..15 → T5 (+ surfaced in T8); SHU-16..20 → T1 (schema) + T3 (queries) + T7 (scheduling). 20 total, 20 mapped, 0 unmapped.
