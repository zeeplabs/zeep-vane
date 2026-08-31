# HA Multi-Replica Validation Report

**Verifier**: independent (did not author this feature or its fixes)
**Date**: 2026-08-30
**Branch**: develop @ 639bfff
**Iteration**: 2 of 3 (re-verification after fix commits addressing all 5 ranked gaps from iteration 1)
**Verdict**: PASS ✅

Iteration 1 (`72cb670`) found: 14/18 ACs matched with precise evidence, 4 spec-precision gaps (HA-03, HA-05, HA-12, HA-17), and a discrimination sensor result of 1 killed / 2 survived. Five fix commits (`256544a`, `bbd3d95`, `639bfff`, `3f0a050`, `7f6516b`) were applied on top. This report re-derives every finding from scratch against the current tree — it does not carry forward iteration 1's conclusions without fresh evidence.

---

## 1. Task completion (tasks.md)

All 13 tasks (T1-T13) remain `[x]` done; no task content changed in this iteration, only follow-up test/doc/fix commits layered on top (outside the original 13-task numbering, as fix commits). Commit trail on top of the original feature and previously-flagged-out-of-scope commits (`b2e8ef9`, `a1cd458`):

```
3f0a050 docs(spec): reconcile HA-03 wording with heartbeat-based leader renewal   (gap 4)
256544a test(ratelimit): cover exact tokens==1.0 decision boundary                (gap 1)
bbd3d95 fix(pglock): check pg_advisory_unlock's returned boolean in Release       (gap 2)
639bfff test(poller): cover HA-05 mid-cycle abort at the data level               (gap 3)
7f6516b test(ratelimit,tls): close HA-12/HA-17 indirect-evidence gaps             (gap 5)
```

All five verified against actual diffs (not self-reports):

- **gap 4 (3f0a050)**: `spec.md` HA-03 wording changed from "renew it... on every successful poll cycle" to describing the actual independent-heartbeat-ticker mechanism. Verified the new text against `internal/cli/poller_manager.go:144-158` (`heartbeatUntilLost`): a `time.Ticker` on `m.leaderHeartbeatInterval` independent of poll-cycle timing, checking `handle.Healthy(ctx)` each tick. The new spec wording is precise and matches the code exactly.
- **gap 1 (256544a)**: new `internal/ratelimit/postgres_bucket_store_integration_test.go` (`TestPostgresBucketStore_Allow_ExactOneTokenBoundary`) seeds `tokens` directly via SQL and calls `allow()` with `refillPerSec=0`, eliminating the wall-clock-nudge that let the mutation survive in iteration 1. Verified by re-injecting the exact mutation (`tokens >= 1` → `tokens > 1` in `internal/ratelimit/postgres_bucket_store.go:72`) in a scratch worktree — now fails (see §4).
- **gap 2 (bbd3d95)**: `internal/pglock/pglock.go`'s `Release` now checks `pg_advisory_unlock`'s returned boolean (`QueryRow(...).Scan(&released)`) and returns an error when `false`, rather than discarding it via `Exec`. Verified by re-injecting iteration 1's mutation (hash-salt desync between `Acquire`/`Release`) — now fails loudly across `internal/pglock` and `internal/tls` (see §4).
- **gap 3 (639bfff)**: new `internal/poller/poller_abort_test.go` (`TestPoller_PollOnce_AbortsMidCycle_NoWritesForServicesAfterLeadershipLoss`) configures three real services, blocks the second one's fetch mid-flight via a fake `SLOProvider`, kills the held advisory lock's backend out-of-band (`pg_terminate_backend`, same mechanism as the existing HA-04 test), and asserts on real `status_intervals` row counts and `current_status` values (not a boolean flag) for all three services. Independently re-read `internal/poller/poller.go`'s `pollOnce`/`pollService` (lines 114-172): confirmed there is genuinely no explicit `ctx.Done()` check in the `for _, svc := range services` loop, and the abort only works because `FetchWithRetry`'s provider call and both `OpenOrExtend`/`UpdateStatus` are ctx-threaded pgx/HTTP calls that reject an already-canceled context before doing I/O. The fix agent's claimed finding is accurate. Independently confirmed the test has teeth by injecting a new mutation not from the fix agent's own description verbatim but the same shape (swapped `ctx` for `context.Background()` in the `OpenOrExtend` call at `poller.go:161`) — test failed as expected (see §4).
- **gap 5 (7f6516b)**: `internal/ratelimit/ip_limiter_test.go` gained `TestIPLimiter_SingleInstance_BurstThenReject_UnchangedFromBeforeHA`, a single-`IPLimiter`-instance test explicitly named and asserting burst-then-429 with byte-for-byte body match (HA-12). `internal/tls/postgres_storage_integration_test.go` gained `TestPostgresStorage_Lock_OutOfBandKill_AutoReleases`, which uses a new `killAdvisoryLockHolderByName` helper doing a real `pg_terminate_backend` against the actual backend holding `PostgresStorage`'s advisory lock (found via `pg_locks` + `hashtextextended`), distinct from the pre-existing `TestPostgresStorage_Lock_CrashedHolderAutoReleases` which still only calls `handle.Release()` (kept, now correctly scoped as testing the graceful path rather than being the sole "crash" evidence).

---

## 2. Spec-anchored acceptance criteria (HA-01..HA-18)

| AC | file:line | assertion/behavior | Match |
|---|---|---|---|
| HA-01 | `internal/pglock/pglock.go:47-64` (`pg_try_advisory_lock`); `internal/cli/poller_manager_test.go:246-266` | non-blocking try before poll cycle; two-replica exclusivity test | ✅ |
| HA-02 | `internal/cli/poller_manager.go:113-119` (`leaderRetryInterval`, 10s default) | same test as HA-01 | ✅ |
| HA-03 | `internal/cli/poller_manager.go:144-158` `heartbeatUntilLost`; `spec.md` line 53 (revised wording) | independent heartbeat ticker via `Handle.Healthy()`, not tied to poll-cycle completion — spec wording now matches code exactly | ✅ (gap closed) |
| HA-04 | `internal/cli/poller_manager_test.go:283-322` (`pg_terminate_backend` kill + failover assertion) | ✅ | Real out-of-band kill |
| HA-05 | `internal/poller/poller_abort_test.go` (`TestPoller_PollOnce_AbortsMidCycle_NoWritesForServicesAfterLeadershipLoss`) | real 3-service poll cycle, real out-of-band kill, asserts `status_intervals` row counts + `current_status` per service | ✅ (gap closed) |
| HA-06 | grep clean | ✅ | No new env var |
| HA-07 | `internal/cli/poller_manager_test.go:225-240` | ✅ | |
| HA-08 | `internal/ratelimit/ip_limiter_integration_test.go:52-97` | ✅ | Cross-replica shared bucket |
| HA-09 | Same test + `ip_limiter_test.go:128`; boundary now also covered at `postgres_bucket_store_integration_test.go` | ✅ | Byte-for-byte body checked; exact-1.0 threshold now deterministic |
| HA-10 | `internal/ratelimit/ip_limiter_test.go:216-233` | ✅ | Fail-open confirmed; log call present in code |
| HA-11 | `internal/ratelimit/ip_limiter_integration_test.go:100-138` | ✅ | |
| HA-12 | `internal/ratelimit/ip_limiter_test.go:100` `TestIPLimiter_SingleInstance_BurstThenReject_UnchangedFromBeforeHA` | ✅ (gap closed) | Dedicated, explicitly-named single-replica test |
| HA-13 | `internal/tls/postgres_storage.go:39` interface assertion; `manager.go:53` | ✅ | |
| HA-14 | `postgres_storage_integration_test.go:68-86` | ✅ | No caching layer in code |
| HA-15 | `postgres_storage_integration_test.go:313-353` | ✅ | |
| HA-16 | `postgres_storage.go:198-207`; `pglock.go:80` (`hashtextextended(name,0)`) | ✅ | |
| HA-17 | `postgres_storage_integration_test.go` `TestPostgresStorage_Lock_OutOfBandKill_AutoReleases` (new `killAdvisoryLockHolderByName` + real `pg_terminate_backend`) | ✅ (gap closed) | Direct proof at the `PostgresStorage` layer, not only by composition |
| HA-18 | `charts/zeep-vane/templates/pvc.yaml` confirmed deleted; grep clean on `values.yaml`/`deployment.yaml`/`NOTES.txt` | ✅ | |

**Coverage**: 18/18 fully matched with precise, freshly-verified evidence. 0 spec-precision gaps remain.

---

## 3. Build-level gate

Disposable Postgres container (`vane-verify2-pg`, port 5440, `max_connections=300`), created and destroyed by this Verifier. `vane-dev-pg` was never touched (confirmed running throughout, untouched).

| Command | Result |
|---|---|
| `go build ./...` | ✅ pass |
| `go vet ./...` | ✅ pass |
| `gofmt -l .` | ✅ pass (no output) |
| `go test ./...` | ✅ pass (all packages) |
| `TEST_DATABASE_URL=... go test -tags=integration ./...` | ✅ pass overall; one `internal/db` failure on the full parallel run (`TestCompanySettingsMigration_AppliesClean_SeedsSingletonRow`) reproduced then re-confirmed as cross-package test-data pollution against the shared container (same known issue flagged in iteration 1) — passes cleanly re-run in isolation (`go test -tags=integration ./internal/db/...`) |
| `helm lint charts/zeep-vane` | ✅ pass (1 chart linted, 0 failed; only an "icon recommended" info) |
| `helm template ... --set secrets.*=x` | ✅ pass, renders cleanly, zero PVC/persistence references |

**Gate: 6 passed, 0 failed.**

---

## 4. Discrimination sensor

Scratch git worktree (`git worktree add`, never `git stash`) at `develop@639bfff`, disposable Postgres on port 5439 (distinct from the gate's 5440 and the pre-existing `vane-dev-pg`), one mutation injected and tested at a time, reverted between each via `git checkout --`.

| # | Mutation | Location | Test run | Result |
|---|---|---|---|---|
| 1 (re-run) | Desync advisory-lock hash salt between `Acquire` and `Release` (`hashtextextended($1, 0)` → `hashtextextended($1, 1)` in `Acquire` only) | `internal/pglock/pglock.go:80` | `go test -tags=integration ./internal/pglock/... ./internal/tls/...` | **KILLED** — `TestAcquire_SecondCallerBlocksUntilReleased` and 4 `internal/tls` tests (`TestPostgresStorage_Lock_BlocksSecondInstanceUntilUnlocked`, `TestPostgresStorage_Unlock_RemovesTrackedHandle`, `TestPostgresStorage_Lock_CrashedHolderAutoReleases`, `TestPostgresStorage_Lock_OutOfBandKill_AutoReleases`) all fail with `pg_advisory_unlock reported this session did not hold the lock it was asked to release`. Gap 2's fix (`bbd3d95`) directly closes this. |
| 2 (re-run) | Off-by-one clamp in rate-limiter decision (`tokens >= 1` → `tokens > 1`) | `internal/ratelimit/postgres_bucket_store.go:72` | `go test -tags=integration ./internal/ratelimit/...` | **KILLED** — `TestPostgresBucketStore_Allow_ExactOneTokenBoundary/exactly_1.0_tokens_is_allowed` fails (`allow() with tokens == 1.0 exactly = false, want true`). Gap 1's fix (`256544a`) directly closes this. |
| 3 (new) | Revert HA-05 write-path ctx-threading: swap `ctx` for `context.Background()` in `pollService`'s `OpenOrExtend` call | `internal/poller/poller.go:161` | `go test -tags=integration ./internal/poller/... -run TestPoller_PollOnce_AbortsMidCycle` | **KILLED** — `TestPoller_PollOnce_AbortsMidCycle_NoWritesForServicesAfterLeadershipLoss` fails (`status_intervals rows for service ... = 1, want 0`), i.e. svc2's write landed even though the lock had already been lost. Confirms the new HA-05 test independently has teeth, not just on the fix agent's word. |

**Sensor: 3 mutations tested (2 re-run from iteration 1 + 1 new), 3 killed, 0 survived.**

Cleanup: worktree removed (`git worktree remove --force`), both sensor/gate containers stopped, `git status --porcelain` on the real tree unchanged before/after (only the pre-existing untracked `validation.md` itself, present both before and after).

---

## 5. Code quality spot-check

- `pglock.Release`'s new boolean check (gap 2) is a real correctness fix, not just a sensor-satisfying test addition — it closes a class of bug (session/connection-reuse across a hash-key mismatch) that was previously silently masked, and the doc comment explaining *why* the check matters is accurate and non-obvious.
- `poller_abort_test.go`'s doc comment honestly discloses the underlying gap it found (`pollOnce`'s loop has no explicit `ctx.Done()` check, relies entirely on ctx-threaded I/O failing fast) rather than overclaiming the abort is enforced by an explicit guard. This matches what independent re-reading of `poller.go` confirms.
- `postgres_bucket_store_integration_test.go`'s boundary test correctly isolates the timing confound (uses `refillPerSec=0` and direct SQL seeding) rather than trying to win a race against wall-clock refill — a robust, non-flaky way to hit the exact boundary.
- No scope creep: all five fix commits are narrowly scoped to the gaps they name (tests + one 22-line production fix in `pglock.go`), consistent with `AGENTS.md`'s incremental-change expectations.
- `HA-17`'s original `TestPostgresStorage_Lock_CrashedHolderAutoReleases` was left in place rather than deleted/renamed — reasonable, since it still provides value as a graceful-release regression test; the new out-of-band test supplements rather than replaces it.

---

## Ranked gaps

None. All 18 ACs matched with fresh evidence, the build-level gate is fully green (6/6), and all 3 sensor mutations (2 re-run + 1 new) are killed.
