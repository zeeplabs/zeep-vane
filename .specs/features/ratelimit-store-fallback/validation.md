# Rate Limiter Store Fallback Validation

**Date**: 2026-09-12
**Spec**: `.specs/features/ratelimit-store-fallback/spec.md`
**Diff range**: `d74d660..78e80ad` (7 commits: 516a3f4, 9d7f2a8, c8d6aa6, 60960f8, c57e27c, 969d670, 78e80ad)
**Verifier**: standalone fresh-eyes pass (no worker sub-agent available in this harness; author = verifier limitation recorded below)

> **Author ≠ verifier limitation.** This harness exposes no general-purpose worker sub-agent to dispatch an independent Verifier. Following the skill's standalone fallback, validation ran as a fresh-eyes pass over the committed diff with a spec-anchored check and a scratch-worktree discrimination sensor. The limitation is that the same model that wrote the code ran the check; the discrimination sensor compensates by testing the tests empirically.

---

## Task Completion

| Task | Status  | Notes |
| ---- | ------- | ----- |
| T1   | ✅ Done | `memoryBucketStore` + 6 unit tests |
| T2   | ✅ Done | Fallback + circuit breaker + 7 tests (HA-10 test rewritten) |
| T3   | ✅ Done | In-use-store sweep behavior landed in T2; T3 pinned it with 2 tests |
| T4   | ✅ Done | 2 integration tests against real Postgres |
| T5   | ✅ Done | `AD-029` in `STATE.md` (landed in 9d7f2a8) |

---

## Spec-Anchored Acceptance Criteria

### P1: Requests stay limited when the shared store fails

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| ------------------------- | -------------------- | ----------------------- | ------ |
| RLF-01 primary error → fallback evaluated | request denied per fallback bucket, not allowed | `internal/ratelimit/ip_limiter_test.go:325` - `if rec.Code != http.StatusTooManyRequests` (2nd request is 429, not 200); `internal/ratelimit/ip_limiter_integration_test.go:177` - `t.Error("second allow() with a failing primary = true, want false ...")` | ✅ PASS |
| RLF-02 fallback denial → 429 same shape | `429` + body `rateLimitedBody` | `internal/ratelimit/ip_limiter_test.go:328` - `if got := rec.Body.String(); got != rateLimitedBody`; `internal/ratelimit/ip_limiter_integration_test.go:237` - `if i == 1 && rec.Body.String() != rateLimitedBody` | ✅ PASS |
| RLF-09 fallback parity with primary | identical verdicts, clamp `[0, burst]` | `internal/ratelimit/memory_bucket_store_test.go:145` - `t.Fatalf("call %d: memory=%v fake=%v, want identical verdicts", ...)`; `:73` - `t.Error("third allow() after long idle = true, want false (tokens clamped at burst=2)")` | ✅ PASS |
| RLF-03 both stores error → last-resort allow | `allow() == true` on both fallback-error paths + log | `internal/ratelimit/ip_limiter_test.go:347` - `t.Error("first allow() = false, want true (last-resort fail-open after a primary error)")`; `:352` - `t.Error("second allow() = false, want true (last-resort fail-open when the chosen fallback errors)")` | ✅ PASS |

### P1: A failing store is quarantined behind a circuit breaker

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| ------------------------- | -------------------- | ----------------------- | ------ |
| RLF-04 error opens circuit; skip primary in cooldown | primary called exactly once | `internal/ratelimit/ip_limiter_test.go:377` - `t.Errorf("primary calls = %d, want 1 (circuit open, primary skipped)", got)` | ✅ PASS |
| RLF-05 single half-open probe | exactly one probe reaches primary with a concurrent caller | `internal/ratelimit/ip_limiter_test.go:418` - `t.Errorf("primary calls = %d, want 2 (only the single probe)", got)` | ✅ PASS |
| RLF-06 probe success closes circuit | `openUntil` zero, `probing` false; primary resumes | `internal/ratelimit/ip_limiter_test.go:457` - `t.Errorf("breaker state after success = (openUntil=%v, probing=%v), want closed", ...)`; `internal/ratelimit/ip_limiter_integration_test.go:208` - `t.Errorf("rate_limit_buckets rows after recovery = %d, want 1 (primary used)")` | ✅ PASS |
| RLF-07 probe failure re-arms cooldown | `breakerOpenUntil` in the future; primary skipped again | `internal/ratelimit/ip_limiter_test.go:487` - `t.Errorf("breakerOpenUntil = %v, want in the future (re-armed)", openUntil)` | ✅ PASS |

### P2: The fallback's memory stays bounded

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| ------------------------- | -------------------- | ----------------------- | ------ |
| RLF-08 sweep acts on the in-use store | fallback-path sweep evicts the fallback; primary-path sweep evicts the primary | `internal/ratelimit/ip_limiter_test.go:522` - `t.Error("stale fallback bucket still present, want evicted (sweep targeted the fallback)")`; `:529` `TestIPLimiter_Sweep_PrimaryPathSweepsPrimary` - `primary.cleanupCount() == 1` and `fallback.cleanupCount() == 0` | ✅ PASS |

**Status**: ✅ All 9 requirement IDs covered with `file:line` evidence; asserted values match spec-defined outcomes. No spec-precision gaps (the spec pins concrete status codes, bodies, and boolean outcomes for every criterion).

---

## Discrimination Sensor

Scratch worktree at `HEAD`; mutations applied there, tests run, worktree removed. Real-tree `git status --porcelain` matched the pre-sensor baseline after every run (`porcelain unchanged: True`).

| Mutation | File | Description | Killed? |
| -------- | ---- | ----------- | ------- |
| M1 | `internal/ratelimit/ip_limiter.go` | Revert primary-error path to `return true` (fail-open) | ✅ Killed |
| M2 | `internal/ratelimit/ip_limiter.go` | `pickStore` ignores the open circuit (`if false`) | ✅ Killed |
| M3 | `internal/ratelimit/ip_limiter.go` | Remove the single-probe gate (`breakerProbing` check) | ✅ Killed |
| M4 | `internal/ratelimit/ip_limiter.go` | `onPrimarySuccess` no longer clears `breakerOpenUntil` | ✅ Killed |
| M5 | `internal/ratelimit/memory_bucket_store.go` | Remove the `tokens > burst` ceiling clamp | ✅ Killed |
| M6 | `internal/ratelimit/ip_limiter.go` | Sweep always targets `l.store` instead of the chosen store | ✅ Killed |
| M7 | `internal/ratelimit/ip_limiter.go` | `!usePrimary` fallback-error path returns `false` instead of `true` | ✅ Killed (after fix 78e80ad) |

**Sensor depth**: lightweight (7 mutations, security-sensitive path)
**Result**: 7/7 killed - **PASS ✅**

**Surviving mutant found and fixed:** M7 initially survived because `TestIPLimiter_FallbackError_LastResortAllows` only exercised the *post-primary-error* fallback path; the `!usePrimary` branch (fallback is the chosen store because the circuit is open, and the fallback itself errors) was uncovered. Fixed in `78e80ad` by asserting a second call on the open-circuit path. Re-run: 7/7 killed.

---

## Code Quality

| Principle | Status |
| --------- | ------ |
| Minimum code | ✅ |
| Surgical changes (only ratelimit + STATE/tasks) | ✅ |
| No scope creep | ✅ |
| Matches existing patterns/style | ✅ |
| Spec-anchored outcome check (asserted values match spec) | ✅ |
| Per-layer Coverage Expectation met (package unit tests 1:1 to RLF; integration covers real-store happy + error/recovery) | ✅ |
| Every test maps to a spec requirement - no unclaimed tests | ✅ |
| Documented guidelines followed: `AGENTS.md` §3 (gate + disposable DB), §4 (no raw error leakage - unchanged) | ✅ |

---

## Edge Cases

- [x] Store never errored → identical behavior: `ip_limiter_test.go:100` (HA-12 single-instance unchanged), `:144` (exceeds burst 429 + body).
- [x] Recovery is probe-gated (delay accepted): `TestIPLimiter_HalfOpenProbeSuccess_ClosesCircuit`; `TestIPLimiter_HalfOpenProbeFailure_RearmsCooldown`.
- [x] Fallback and primary buckets independent (~2× transient): accepted design trade-off, exercised implicitly by the recovery test (fallback exhausted, primary fresh).
- [x] Fallback constructed with identical parameters: single call site `NewIPLimiter` (`internal/ratelimit/ip_limiter.go`) passes the same `perMinute`/`burst`/`idleTTL` to both stores.
- [x] Last-resort log on both fallback-error paths (RLF-03): `ip_limiter_test.go:337` asserts allow; logs are emitted on both branches.

---

## Gate Check

- **Gate command**: `go build ./... && go vet ./... && gofmt -l <changed files> && go test ./internal/ratelimit/...` plus `TEST_DATABASE_URL=... go test -tags=integration -count=1 -p 1 ./...`
- **Unit (`go test -count=1 ./...`)**: all packages pass; 0 failures.
- **Integration (`-tags=integration -count=1 -p 1 ./...`)**: **exit 0**, 22 packages `ok`, 0 failures, against a disposable Postgres container (`localhost:5433`, `max_connections=300`), container destroyed afterward.
- **Test count before feature**: 12 test functions in `internal/ratelimit`
- **Test count after feature**: 27 test functions
- **Delta**: +15 test functions (1 rewritten: `TestIPLimiter_StoreError_FailsOpen` → `TestIPLimiter_PrimaryError_UsesFallback`)
- **Skipped tests**: none (integration tests skip only when `TEST_DATABASE_URL` is unset; it was set)
- **Failures**: none

---

## Fix Plans (from the sensor)

### Fix 1: last-resort path uncovered (M7) - DONE

- **Root cause**: `TestIPLimiter_FallbackError_LastResortAllows` made a single `allow` call, which returns through the post-primary-error branch. The `!usePrimary` branch (open circuit, fallback chosen and erroring) was never executed, so flipping its return value survived.
- **Fix**: second `allow` call on the same failing limiter now exercises the open-circuit path and asserts `true` (`internal/ratelimit/ip_limiter_test.go:352`).
- **Priority**: Minor (test-coverage gap, not a production defect) - fixed in `78e80ad`.

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| ----------- | --------------- | ---------- |
| RLF-01 | Pending | ✅ Verified |
| RLF-02 | Pending | ✅ Verified |
| RLF-03 | Pending | ✅ Verified |
| RLF-04 | Pending | ✅ Verified |
| RLF-05 | Pending | ✅ Verified |
| RLF-06 | Pending | ✅ Verified |
| RLF-07 | Pending | ✅ Verified |
| RLF-08 | Pending | ✅ Verified |
| RLF-09 | Pending | ✅ Verified |

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 9/9 requirement IDs matched spec outcomes; 0 spec-precision gaps.
**Sensor**: 7/7 mutations killed (one survived initially, fixed and re-killed).
**Gate**: unit + full integration green; +15 tests.

**What works**: primary store failures now route to an in-process fallback that enforces the same token-bucket limit; a circuit breaker quarantines a failing store (5s cooldown, single half-open probe); the idle sweep bounds whichever store is in use; and a request is allowed without limiting only in the documented double-failure last resort.

**Issues found**: one surviving mutant (test-coverage gap), fixed.

**Next steps**: none. Item 4 (rate-limit fail-open) is closed; `AD-029` supersedes `HA-10`. Next: item 3 (TLS keys at rest).
