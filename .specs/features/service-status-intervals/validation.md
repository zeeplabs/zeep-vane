# service-status-intervals Validation

**Date**: 2026-08-24
**Spec**: `.specs/features/service-status-intervals/spec.md`
**Diff range**: `6ac4983..HEAD` (11 commits: 10 feature + 1 docs; a 12th commit, `3f3c6fa` "feat(auth): update logo handling...", sits inside this range but touches only `web/src/features/auth/*` and `web/src/layout/Sidebar.tsx` — unrelated pre-existing work, excluded from this review)
**Verifier**: independent sub-agent (author ≠ verifier)

---

## Task Completion

| Task | Status  | Notes |
| ---- | ------- | ----- |
| T1   | ✅ Done | `80c52f4` — migration 0014 up/down + `status_intervals_migration_test.go` |
| T2   | ✅ Done | `0e4241f` — `OpenOrExtend` transactional write path |
| T3   | ✅ Done | `97ff668` — `OpenIntervalsByService`, `ListOverlapping`, `DeleteClosedBefore` |
| T4   | ✅ Done | `72190e9` — `BuildHourly` worst-status-wins rewrite |
| T5   | ✅ Done | `5f84c57` — `UptimePercent` |
| T6   | ✅ Done | `3aa9d01` — poller wired to `OpenOrExtend` |
| T7   | ✅ Done | `e5f51f5` — `internal/retention.Pruner` |
| T8   | ✅ Done | `47564e2` — public handler wired to intervals + uptime % |
| T9   | ✅ Done | `2556436` — `serve.go`/`routes.go` wiring + Pruner goroutine started |
| T10  | ✅ Done | `526f787` — snapshot repository/migration-test files deleted |

All 10 tasks map to real, distinct commits in the stated order. `f96e2b6` (docs) added `spec.md`/`design.md` after the fact — matches the feature's documented history.

---

## Spec-Anchored Acceptance Criteria

### P1: Interval-based storage (SHU-01..05)

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| SHU-01 first-ever status → insert one open row | 1 row, `ends_at` NULL, `starts_at == at` | `internal/db/status_interval_repository_test.go:65-89` — `TestOpenOrExtend_FirstObservation_InsertsOneOpenInterval`: `len(intervals) != 1`, `got.EndsAt != nil`, `got.StartsAt.Equal(at)` | ✅ PASS |
| SHU-02 same status → update in place, no new row | 1 row still, `error_budget_remaining`/`last_seen_at` updated, `starts_at` unchanged | `internal/db/status_interval_repository_test.go:91-123` — `TestOpenOrExtend_SameStatus_UpdatesOpenIntervalInPlace`: asserts `len==1`, `LastSeenAt.Equal(second)`, `ErrorBudgetRemaining==90.0`, `StartsAt` unchanged | ✅ PASS |
| SHU-03 different status → close old + open new at same `at` | 2 rows: old `ends_at==at`, new `starts_at==at`, `ends_at` nil | `internal/db/status_interval_repository_test.go:125-161` — `TestOpenOrExtend_DifferentStatus_ClosesOldRowAndOpensNew`: `closed.EndsAt.Equal(second)`, `open.StartsAt.Equal(second)`, `open.EndsAt==nil` | ✅ PASS |
| SHU-04 DB-enforced at-most-one-open-per-service | unique partial index `(service_id) WHERE ends_at IS NULL` exists | `internal/db/status_intervals_migration_test.go:52-62` — `pg_indexes` count == 1 for that index | ✅ PASS |
| SHU-05 race loser surfaces constraint error, no duplicate | exactly one insert wins; loser gets an error, no silent duplicate | `internal/db/status_interval_repository_test.go:174-232` — `TestOpenOrExtend_ConcurrentWriters_RaceLoserGetsError`: a real concurrent goroutine genuinely blocks on the holder's uncommitted row, then `errors.Is(raceErr, ErrIntervalRaceLost)` and `len(intervals)==1` post-race | ✅ PASS — real two-writer contention, not a generic-error stub |

### P1: Worst-status hourly bars (SHU-06..09)

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| SHU-06 worst status wins in-bucket, order outage>degraded>operational | exact 55min-op/5min-outage example resolves `outage` | `internal/history/hourly_test.go:53-70` — `TestBuildHourly_WorstStatusWinsWithinBucket`: `buckets[22].Status != "outage"`. Full order also in `hourly_test.go:74-102` | ✅ PASS |
| SHU-07 no overlap → NoData | bucket status == `no_data` | `internal/history/hourly_test.go:104-116` — `TestBuildHourly_NoOverlappingInterval_ResolvesToNoData` | ✅ PASS |
| SHU-08 exactly 24 buckets, unchanged contract | `len==24`, oldest-first, `America/Sao_Paulo` | `internal/history/hourly_test.go:29-51` — `TestBuildHourly_ReturnsWindowHoursBucketsOldestFirst` | ✅ PASS |
| SHU-09 interval spanning boundary contributes to every overlapped bucket | 3 consecutive hourly buckets all `outage`, 4th `NoData` after interval ends | `internal/history/hourly_test.go:120-146` — `TestBuildHourly_IntervalSpanningMultipleBuckets_CoversEveryOverlappedBucket` | ✅ PASS |
| — same, surfaced end-to-end through handler | hourly bucket in HTTP response reflects worst status | `internal/api/public_status_handler_test.go:196-256` — `TestPublicStatusGet_HourlyHistory_KnownHourStatusAppearsAsSingleBucket` (exactly 1 outage bucket at the expected local-hour `Start`) | ✅ PASS |

### P1: 24h uptime % (SHU-10..15)

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| SHU-10 outage-only downtime | `degraded` doesn't count | `internal/history/uptime_test.go:62-79` — `TestUptimePercent_DegradedDoesNotCountAsDowntime`: `percent != 100.0` fails | ✅ PASS |
| SHU-11 full-window denominator | 6h outage / 24h window → `75.0` exactly | `internal/history/uptime_test.go:10-29` — `TestUptimePercent_SixHoursOutageInTwentyFourHourWindow_Returns75`: `percent != 75.0` | ✅ PASS |
| SHU-12/13 clipped denominator for newer-than-window service | 2h denominator, always-op → `100.0` | `internal/history/uptime_test.go:31-49` — `TestUptimePercent_ServiceNewerThanWindow_UsesClippedDenominator` | ✅ PASS |
| SHU-14 clamp to `[0,100]` before rounding | pathological overflow clamps to `0.0` (entire window outage) | `internal/history/uptime_test.go:81-103` — `TestUptimePercent_ClampsToZeroHundredRange`: `percent != 0.0` | ✅ PASS |
| SHU-14/15 floor, never round up | `99.97106%` → `99.9`, not `100.0` | `internal/history/uptime_test.go:105-128` — `TestUptimePercent_RoundingAlwaysFloors`: `percent != 99.9` | ✅ PASS — exact value asserted, matches spec's own example |
| SHU-15 zero intervals → undefined (`ok=false`) | not `0`/`100` | `internal/history/uptime_test.go:51-60` — `TestUptimePercent_ZeroIntervals_ReturnsUndefined` | ✅ PASS |
| SHU-15 `uptime_percent` null in JSON, not fabricated | HTTP response field is `nil`/`null` | `internal/api/public_status_handler_test.go:262-311` — `TestPublicStatusGet_ServiceWithNoSnapshotsEver_AllHourlyBucketsNoData`: `found.UptimePercent != nil` fails; asserted against the real handler (`composeResponse`), not just the pure function | ✅ PASS — T8-level, not just T5-level |
| — same, positive case end-to-end | `75.0` through the full HTTP round-trip | `internal/api/public_status_handler_test.go:317-356` — `TestPublicStatusGet_UptimePercent_OutageWindowComputesExpectedValue`: `*found.UptimePercent != 75.0` | ✅ PASS |

### P2: Automatic pruning (SHU-16..20)

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| SHU-16 delete `ends_at IS NOT NULL AND ends_at < now-35d` | 40-day-old closed row gone, 10-day-old kept | `internal/db/status_interval_repository_test.go:350-397` — `TestDeleteClosedBefore_DeletesOnlyClosedRowsOlderThanCutoff`: `deleted != 1`, remaining rows checked by `EndsAt` value | ✅ PASS |
| SHU-16/17 pruner runs on its own ticker, in-process | tick triggers a real delete via `Pruner.Run` | `internal/retention/pruner_test.go:62-121` — `TestPruner_Run_TickDeletesClosedIntervalsOlderThan35Days` | ✅ PASS |
| SHU-17/actual cadence | hardcoded `1*time.Hour` in `serve.go` | `internal/cli/serve.go:31-32` — `const pruneTick = 1 * time.Hour`, `const pruneRetention = 35 * 24 * time.Hour`; wired at `serve.go:94` | ✅ PASS |
| SHU-19 open interval never deleted, regardless of `starts_at` age | 100-day-old open row survives a prune tick | `internal/db/status_interval_repository_test.go:373-396` and `internal/retention/pruner_test.go:76-120` (`openCount != 1` check) | ✅ PASS |
| SHU-20 delete error logged, retried next tick, no crash | `Run`'s loop continues past a failing tick | `internal/retention/pruner_test.go:165-192` — `TestPruner_Run_DeleteErrorIsLoggedAndLoopContinues`: forces 1 failure via `fakeFailingDeleter`, asserts `calls >= 2` | ✅ PASS |
| — ctx-cancel shutdown | `Run` returns promptly on cancel | `internal/retention/pruner_test.go:126-145` — `TestPruner_Run_ReturnsPromptlyOnContextCancel` | ✅ PASS |

### `last_seen_at` design risk (explicitly flagged in design.md)

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| `LastUpdatedAt` advances on repeated same-status poll | timestamp moves forward, doesn't freeze at interval open | `internal/api/public_status_handler_test.go:360-401` — `TestPublicStatusGet_LastUpdatedAt_AdvancesOnRepeatedSameStatusPoll`: `found.LastUpdatedAt.Equal(secondSeenAt)` (not `firstSeenAt`) | ✅ PASS — the exact regression design.md flagged is directly tested |

**Status**: ✅ All 20 SHU ACs covered with spec-exact-value evidence — 0 spec-precision gaps.

**Uncovered by a dedicated test (flagged, not a spec-outcome mismatch)**:
- Edge case "poller's first-ever poll for a brand-new service fails → no interval row created at all" — the code path (`internal/poller/poller.go:109-121`) returns before calling `OpenOrExtend` on any fetch failure, so it is structurally correct and identical for a brand-new vs. established service. But `TestPoller_PollOnce_ConnectionFailure_MarksIntegrationInvalidAndKeepsLastStatus` (`internal/poller/poller_test.go:137-186`) seeds one successful poll before the failing one — there is no test that starts from zero intervals, fails, and then asserts `OpenIntervalsByService`/`ListOverlapping` return empty for that service. Evidence-or-zero: no `file:line` exists for the "brand-new + first-poll-fails" combination specifically, so this edge case counts as **not independently covered**, even though the shared code path is exercised.

---

## Discrimination Sensor

Isolated via `git worktree add <scratch> HEAD` (never `git stash`); baseline `git status --porcelain` was empty before and after.

| Mutation | File:line | Description | Killed? |
| --- | --- | --- | --- |
| 1 | `internal/db/status_interval_repository.go:86` | `case open.Status == status:` → `case open.Status != status:` (inverts same/different status branch in `OpenOrExtend`) | ✅ Killed — `TestOpenOrExtend_SameStatus_UpdatesOpenIntervalInPlace` and `TestOpenOrExtend_DifferentStatus_ClosesOldRowAndOpensNew` both fail |
| 2 | `internal/history/hourly.go:19-23` | `statusPriority` reversed (`outage:1, operational:3`) — makes `operational` beat `outage` | ✅ Killed — `TestBuildHourly_WorstStatusWinsWithinBucket` and `TestBuildHourly_PriorityOrder_OutageBeatsDegradedBeatsOperational` both fail |
| 3 | `internal/history/uptime.go:76` | `math.Floor(pct*10)/10` → `math.Round(pct*10)/10` | ✅ Killed — `TestUptimePercent_RoundingAlwaysFloors` fails (`got 100, want 99.9`) |

**Sensor depth**: lightweight (3 targeted mutations, default tier)
**Result**: 3/3 killed — PASS ✅
**Isolation verified**: `git status --porcelain` on the real tree was empty before sensor work and empty after `git worktree remove --force`.

---

## Code Quality

| Principle | Status |
| --- | --- |
| Minimum code | ✅ — no functionality beyond spec/design/tasks scope |
| Surgical changes | ✅ — feature commits touch exactly the files tasks.md declared per task |
| No scope creep | ✅ — the one unrelated commit in the numeric range (`3f3c6fa`, auth logo handling) is pre-existing, unrelated work interleaved in history, not part of this feature's file set |
| Matches patterns | ✅ — `OpenOrExtend`'s `SELECT ... FOR UPDATE` shape mirrors `AttachDomain`; `Pruner.Run` mirrors `Poller.Run`'s ticker/`ctx.Done()` shape, as design.md specified |
| Spec-anchored outcome check (asserted values match spec) | ✅ — see table above; exact `75.0`, `99.9`, `100.0` literals used, not generic assertions |
| Per-layer Coverage Expectation met (domain 1:1 ACs; routes happy+edge+error) | ✅ — `internal/history` has 1:1 unit tests per SHU-06..15; `internal/api` covers happy path, no-data, null-uptime, and the `last_seen_at` regression guard |
| Every test maps to a spec requirement — no unclaimed tests | ✅ — every test file's comments cite the SHU-* ID(s) it covers |
| Documented guidelines followed | ✅ — none found in-repo (`Makefile`, `README.md` "Running tests" section only); strong defaults applied, consistent with tasks.md's own stated finding |

---

## Edge Cases

- [x] "First poll for a brand-new service fails → no interval row" — code path is correct (early return before `OpenOrExtend`) but **not independently tested** for the brand-new-service case specifically (see gap above)
- [x] `PollerManager.Restart` race — covered by `TestOpenOrExtend_ConcurrentWriters_RaceLoserGetsError`, a genuine two-writer contention test, not a generic-error stub
- [x] Interval crossing a bucket boundary contributes to every overlapped bucket — `TestBuildHourly_IntervalSpanningMultipleBuckets_CoversEveryOverlappedBucket`
- [x] Zero/negative clipped denominator → undefined — `TestUptimePercent_ZeroDenominator_ReturnsUndefined`

---

## Gate Check

- **Gate command**: `go build ./... && go vet ./... && gofmt -l . && TEST_DATABASE_URL=<dsn> go test -tags=integration ./... && go test ./...`
- **Result**: build ✅ clean; vet ✅ clean; gofmt ✅ no files listed; integration suite: 299 passed, 3 failed on the first `-count=1` run, all 3 confirmed pre-existing/order-dependent flakes unrelated to this feature's diff (`TestUpdateAdminRole_SelfDemotionAsLastOwner_409`, `TestConnectDatadog_InvalidCredentials_422NothingSaved`, `TestAdminRepository_CountActiveOwners_CountsOnlyOwners` — none touched by `6ac4983..HEAD`, all pass individually in isolation); plain `go test ./...` (no DB) — all packages pass.
- **Test count before feature**: not independently re-derivable at `6ac4983` without checking out that commit and standing up its schema against a since-migrated DB (would require a separate DB instance); per tasks.md's own worker-reported deltas: ~34 new/updated tests across Batch 1 (T1-T7) + ~2 new/updated in Batch 2 (T8-T10, mostly in the two handler test files).
- **Test count after feature**: 302 integration-tagged test functions executed (299 pass + 3 pre-existing flakes, confirmed unrelated); `internal/history` alone: 15 unit tests (7 `hourly_test.go` + 7 `uptime_test.go` + shared helpers), 0 requiring a DB.
- **Delta**: net positive; no test deletions beyond the explicitly-superseded `status_snapshot_repository_test.go`/`status_snapshots_migration_test.go` (T10, by design)
- **Skipped tests**: none observed
- **Failures**: 3, all pre-existing and outside this feature's diff surface (see above) — re-ran each in isolation and all passed, confirming test-order/shared-state flakiness, not a regression introduced by this feature

---

## Requirement Traceability Update

`spec.md`'s own Requirement Traceability table (lines 132-161) still lists all 20 `SHU-*` rows as `Design`/`Pending` — stale, not updated by any feature commit. Recommended update (not applied here — Verifier is read-only on production/spec files per this task's boundary):

| Requirement | Previous Status | New Status |
| --- | --- | --- |
| SHU-01 .. SHU-20 | Design / Pending | ✅ Verified (see Spec-Anchored Acceptance Criteria table above for the file:line evidence backing each) |

---

## Fix Plans

None required — no surviving mutants, no spec-anchored gaps, gate passes. One documentation-only gap noted below (not a fix task, since the code path is already structurally correct).

### Fix 1 (optional, not blocking): dedicated test for "brand-new service, first poll fails"

- **Root cause**: not a bug — `pollService`'s early return already prevents `OpenOrExtend` from being called on any fetch failure, brand-new or not. The gap is purely in test evidence: no test starts from a service with zero prior intervals, fails its poll, and asserts zero rows exist afterward.
- **Fix task**: add a test in `internal/poller/poller_test.go` that creates a fresh service, runs `pollOnce` with a provider that fails immediately (no seeding poll), then asserts `OpenIntervalsByService`/direct row count for that service is 0.
- **Priority**: Minor (documentation/evidence gap, not a functional defect)

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 20/20 SHU ACs matched their spec-defined outcome with `file:line` evidence and exact-value assertions (e.g. `75.0`, `99.9`, `ErrIntervalRaceLost`); 0 spec-precision gaps flagged.

**Sensor**: 3/3 mutations killed (status-branch inversion, priority-order reversal, floor→round substitution) — tests are discriminating for the highest-risk new logic.

**Gate**: build/vet/gofmt clean; integration suite 299/302 passed on first run, 3 confirmed pre-existing flakes unrelated to this diff (all pass in isolation); pure `go test ./...` fully green.

**What works**: Interval write path with DB-enforced single-open-interval invariant and a genuinely-tested concurrent-writer race; worst-status-wins hourly bucketing surfaced end-to-end through the public HTTP handler; uptime % with exact-value floor/clamp assertions and `null`-not-fabricated JSON contract; hourly retention pruner with own ticker, error-survives-and-continues behavior; full removal of the superseded snapshot repository/migration with a clean grep trail.

**Issues found**: One minor test-evidence gap (see Fix 1) — not blocking, code path already correct by inspection.

**Next steps**: Optionally add the poller edge-case test in Fix 1. Update `spec.md`'s Requirement Traceability table to `✅ Verified` for SHU-01..20 (mechanical doc update, not code).
