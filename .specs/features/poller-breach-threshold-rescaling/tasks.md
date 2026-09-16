# Poller Breach Threshold Rescaling Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/poller-breach-threshold-rescaling/design.md`
**Status**: Complete

---

## Test Coverage Matrix

> Generated from codebase (`AGENTS.md` §3, `internal/poller/*_test.go`) and spec. Guidelines found: `AGENTS.md` §3 ("Before considering any change done") - no separate lint/coverage-threshold config beyond `gofmt`/`go vet`.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| `poller.breachBound` (pure function) | unit | All branches; 1:1 to BTR-01 + Edge Cases (large n, n<=0, target<=0, target outside [0,100]) | `internal/poller/breach_threshold_test.go` (new) | `go test ./internal/poller/...` |
| `poller.Poller.pollService` (classification rewrite) | unit | All branches; 1:1 to BTR-02..BTR-12 | `internal/poller/poller_recent_window_test.go` | `go test ./internal/poller/...` |
| `poller.Poller` real-Postgres wiring (`//go:build integration`) | none - no signature or schema change, so existing scenarios keep compiling and asserting (per `AGENTS.md` §3's integration-gate scoping) | - | `internal/poller/poller_test.go`, `internal/poller/poller_abort_test.go` | `go build -tags=integration ./... && go vet -tags=integration ./...` |
| `.specs/STATE.md` decision record (docs) | none | - | `.specs/STATE.md` | build gate only |

**Coverage Expectation values** - set from `AGENTS.md` §3 first (build/test/vet/gofmt clean, integration gate only when DB-touching code changed); strong default (all branches, 1:1 to ACs) applied to the two touched layers.

## Gate Check Commands

> Generated from `AGENTS.md` §3. No DB-touching code in this feature (no migration, no schema, no query change) - the integration gate stays at compile-check only, per `AGENTS.md`'s own scoping ("only when DB-touching code changed").

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick | After T1, T2 (unit-only changes) | `go build ./... && go vet ./... && go test ./internal/poller/...` |
| Build | After the last task (T3) | `go build ./... && go vet ./... && go test ./... && go build -tags=integration ./... && go vet -tags=integration ./... && gofmt -l <changed files>` |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Pure bound helper

```
T1
```

### Phase 2: Classification rewrite

```
T1 → T2
```

### Phase 3: Decision record

```
T2 → T3
```

---

## Task Breakdown

### T1: Add `breachBound` and `breachThresholdSigmas` ✅ Complete

**What**: New file with the pure `breachBound(target float64, requestCount int64, sigmas float64) float64` helper implementing `100 - 100*(p0 + sigmas*sqrt(p0*(1-p0)/n))` with `p0 = (100-target)/100`, plus the uncalibrated `breachThresholdSigmas = 3.0` constant documented in the same style as the other AD-019 constants. Total over its inputs: `requestCount <= 0` returns `target` (no sample, no widening); a negative variance (target outside `[0,100]`) is clamped to zero; never panics. Add the new unit test file covering the branches and edge cases.
**Where**: `internal/poller/breach_threshold.go`
**Depends on**: None
**Reuses**: The constant-documentation convention from `internal/poller/poller.go` (`minRecentWindowRequests`, `recentWindowWidth`, `recentWindowLag`); `math.Sqrt` only - no new dependency.
**Requirement**: BTR-01

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `breachBound(99.5, 10000, 3)` returns approximately `99.29` (assert with a tolerance - exact float value asserted, not a range so wide it cannot discriminate)
- [x] `breachBound(target, n, sigmas) <= target` for all tested valid inputs, and approaches `target` as `n` grows (assert `bound(99.5, 1_000_000, 3)` is within a small epsilon of `99.5`)
- [x] `requestCount <= 0` returns `target` (no division by zero, no NaN/Inf)
- [x] `target <= 0` and `target > 100` return finite values without panic
- [x] `breachThresholdSigmas` is a documented constant equal to `3.0`, with the uncalibrated caveat in its doc comment
- [x] Gate check passes: `go build ./... && go vet ./... && go test ./internal/poller/...`
- [x] `gofmt -l internal/poller/breach_threshold.go internal/poller/breach_threshold_test.go` produces no output

**Tests**: unit
**Gate**: quick

**Commit**: `feat(poller): add window-appropriate breach bound helper`

---

### T2: Classify `pollService` from the bound instead of `state` ✅ Complete

**What**: Rewrite the classification `switch` in `pollService`: keep the low-volume carry-forward first; add the `Target <= 0` state-based fallback (factored into a small helper so it is provably the old behavior); add the `SLI < breachBound(...)` breach branch feeding the existing hysteresis; add the below-target-within-band branch (`state == "warning" || state == "breached" || SLI < Target`) mapping to `"degraded"`; keep `normalizeStatus(state)` as the final default. Add the new unit tests to the existing recent-window test file.
**Where**: `internal/poller/poller.go`
**Depends on**: T1
**Reuses**: `breachBound`/`breachThresholdSigmas` (T1), `breachHysteresisCycles` + `Poller.breachStreak` (unchanged hysteresis), `minRecentWindowRequests` (unchanged guard), `normalizeStatus` (unchanged default), `fakeProvider` with its `statuses []datadog.SLOStatus` field (`internal/poller/retry_test.go`).
**Requirement**: BTR-02, BTR-03, BTR-04, BTR-05, BTR-06, BTR-07, BTR-08, BTR-09, BTR-10, BTR-11, BTR-12

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `Target > 0` and `RequestCount >= minRecentWindowRequests`: breach is decided by `SLI < breachBound(...)`, never by `state == "breached"` alone
- [x] New test: `Target=99.5`, `RequestCount=10000`, `State="breached"`, `SLI=99.4` -> first cycle yields `"degraded"`, not `"outage"` (BTR-03)
- [x] New test: `Target=99.5`, `RequestCount=10000`, `State="ok"`, `SLI=99.4` -> `"degraded"` (SLI below target, inside band) (BTR-03)
- [x] New test: `Target=99.5`, `RequestCount=10000`, `SLI=50`, two consecutive cycles -> first carries forward, second yields `"outage"` (BTR-02, BTR-06, BTR-07)
- [x] New test: a breach followed by a within-band window resets the streak, so a later breach does not flip on its own (BTR-08)
- [x] New test: `Target=0`, `State="breached"`, `RequestCount=5000` -> fallback path; two cycles yield `"outage"` (BTR-09)
- [x] Existing low-volume tests (`TestPollService_LowVolumeNoisyBreach_CarriesForwardPreviousStatus`, `...LowVolumeCarryForward_StillInvokesWriters`, `...FirstPollLowVolume_StaysNotConfigured`) pass unmodified (BTR-10)
- [x] Existing failure-path tests pass unmodified; no interval/status write on an exhausted fetch (BTR-11)
- [x] No environment variable, migration, or schema file is added or changed (BTR-12) - confirmed by the task's diff scope
- [x] Existing hysteresis tests that use `Target=0` still pass via the fallback branch (no test silently deleted)
- [x] Test count stated explicitly in the commit/task summary
- [x] Gate check passes: `go build ./... && go vet ./... && go test ./internal/poller/...`
- [x] `gofmt -l internal/poller/poller.go internal/poller/poller_recent_window_test.go` produces no output

**Tests**: unit
**Gate**: quick

**Commit**: `feat(poller): decide breach from a window-rescaled SLI bound, not Datadog state`

---

### T3: Record AD-019 addendum 4 ✅ Complete

**What**: Append `AD-019 addendum 4` to `.specs/STATE.md`'s `## Decisions` section documenting the root fix (SLI vs window-rescaled bound replaces the `overall.state`-based breach), and mark addendum 3's `Status` as superseded by it (the stopgap is replaced, the hysteresis it introduced is retained).
**Where**: `.specs/STATE.md`
**Depends on**: T2 - needs the final constant/helper names and the exact classification order to describe accurately.
**Reuses**: The existing `AD-019 addendum N` entry format in this file.
**Requirement**: N/A (project-memory bookkeeping, not a spec AC)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `AD-019 addendum 4` added with Decision/Reason/Trade-off/Scope/Date/Status fields, matching the existing addenda's format
- [x] Decision names `internal/poller/breach_threshold.go` (`breachBound`, `breachThresholdSigmas`) and the classification order in `pollService`
- [x] `breachThresholdSigmas` explicitly recorded as uncalibrated (no real-traffic pass this session), with the calibration as the documented follow-up
- [x] Addendum 3's `Status` updated to reflect it is superseded by addendum 4 (hysteresis retained)
- [x] No other section of `STATE.md` edited by this task (Handoff/feature-completion entry is a separate, later step after the Verifier runs)
- [x] Gate check passes: `go build ./... && go vet ./... && go test ./... && gofmt -l internal/poller/breach_threshold.go internal/poller/poller.go internal/poller/breach_threshold_test.go internal/poller/poller_recent_window_test.go`

**Tests**: none (docs-only change)
**Gate**: build

**Commit**: `docs(state): record AD-019 addendum 4 - breach threshold rescaled to the window`

---

## Phase Execution Map

```
Phase 1 → Phase 2 → Phase 3

T1 → T2 → T3
```

Execution is strictly sequential - there is no intra-phase parallelism. 3 tasks total fits a single batch (≤ ~8) - no sub-agent offer needed; runs inline.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: `breachBound` + constant | 1 new file + 1 new test file, one cohesive pure function | ✅ Granular |
| T2: `pollService` classification rewrite | 1 modified file + additions to 1 test file, one cohesive behavior | ✅ Granular |
| T3: Record AD-019 addendum 4 | 1 file, docs-only | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | (start, no arrow in) | ✅ Match |
| T2 | T1 | T1 → T2 | ✅ Match |
| T3 | T2 | T2 → T3 | ✅ Match |

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1: `breachBound` + constant | `poller.breachBound` (pure) | unit | unit | ✅ OK |
| T2: `pollService` classification | `poller.Poller.pollService` | unit | unit | ✅ OK |
| T3: Record AD-019 addendum 4 | none (docs) | none | none | ✅ OK |

---

## Tips reminder (not part of the doc - carried from Design)

Blast radius stays inside `internal/poller/breach_threshold.go` (new), `internal/poller/breach_threshold_test.go` (new), `internal/poller/poller.go`, `internal/poller/poller_recent_window_test.go`, and a `.specs/STATE.md` append. Do not touch `internal/connectors/datadog`, the window constants, the persistence path, or any `//go:build integration` file.
