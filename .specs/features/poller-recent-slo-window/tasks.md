# Poller Recent SLO Window Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/poller-recent-slo-window/design.md`
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase (`AGENTS.md` §3, `internal/poller/*_test.go`, `internal/connectors/datadog/client_test.go`) and spec. Guidelines found: `AGENTS.md` §3 ("Before considering any change done") — no separate lint/coverage-threshold config beyond `gofmt`/`go vet`.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| `datadog.Client` (SLO history fetch/parse) | unit | All branches; 1:1 to RSW-01/02/04 + Edge Cases (malformed `state`, missing `thresholds`, zero `RequestCount`) | `internal/connectors/datadog/client_test.go` | `go test ./internal/connectors/datadog/...` |
| `poller.Poller`/`FetchWithRetry` (window computation, retry passthrough, volume guard) | unit | All branches; 1:1 to RSW-01, 02, 03, 05, 06, 07, 08, 09, 10, 11 | `internal/poller/*_test.go` (no build tag — new file added this feature) | `go test ./internal/poller/...` |
| `poller.Poller` real-Postgres wiring (`serviceLister`/`statusIntervalWriter`/`integrationStatusUpdater` against real repositories) | integration (compile-only for this feature — see Gate Check Commands) | Existing scenarios keep compiling and asserting correctly under the new provider signature; no new scenarios required (feature doesn't touch DB/schema, per `AGENTS.md` §3's integration-gate scoping rule) | `internal/poller/poller_test.go`, `internal/poller/poller_abort_test.go` (`//go:build integration`) | `go build -tags=integration ./... && go vet -tags=integration ./...` |
| `normalizeStatus` (pure function) | none — already fully covered, unchanged by this feature | - | `internal/poller/poller_status_test.go` | build gate only |
| `.specs/STATE.md` decision record (docs) | none | - | `.specs/STATE.md` | build gate only |

**Coverage Expectation values** — set from `AGENTS.md` §3 first (build/test/vet/gofmt clean, integration gate only when DB-touching code changed); strong default (domain logic, all branches, 1:1 to ACs) applied for the two touched layers since neither existing test file nor `AGENTS.md` sets a different bar.

## Gate Check Commands

> Generated from `AGENTS.md` §3. No DB-touching code in this feature (no migration, no schema, no query change) — the integration gate stays at compile-check only, per `AGENTS.md`'s own scoping ("only when DB-touching code changed").

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick | After T1, T2, T3 (unit-only changes) | `go build ./... && go vet ./... && go test ./internal/connectors/datadog/... ./internal/poller/...` |
| Full | After T4, T5 (integration-tagged fixture fixes) | `go build ./... && go vet ./... && go test ./internal/connectors/datadog/... ./internal/poller/... && go build -tags=integration ./... && go vet -tags=integration ./...` |
| Build | After the last task in each phase, and after T6 | `go build ./... && go vet ./... && go test ./... && gofmt -l <changed files>` |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Datadog client — windowed fetch

```
T1
```

### Phase 2: Retry layer passthrough

```
T2
```

### Phase 3: Poller core logic + fixture fixes

```
T3 → T4
T3 → T5
```

### Phase 4: Decision record

```
T6
```

---

## Task Breakdown

### T1: Rewrite `FetchSLOStatus` to fetch a windowed SLO history ✅ Complete

**What**: Change `datadog.Client.FetchSLOStatus`'s signature to `(ctx, sloID string, from, to time.Time)`, calling `GET /api/v1/slo/{id}/history?from_ts=&to_ts=` instead of `/slo/search`; add `RequestCount int64` to `SLOStatus`; add the new `sloHistoryResponse` decode struct; update `SLOProvider` interface to match. `SearchSLOs`/`ValidateCredentials` stay untouched (different endpoint, different methods).
**Where**: `internal/connectors/datadog/client.go`
**Depends on**: None
**Reuses**: `Client.get` (auth headers + status-code classification), `defaultBaseURL`/`defaultTimeout`, the existing `thresholds[0]`-style "don't assume a key" pattern (now `map[string]struct{...}`, one present entry).
**Requirement**: RSW-01, RSW-04

**Tools**:
- MCP: NONE (Datadog SDK research already done in Design — reuse the confirmed response shape, no further discovery calls needed)
- Skill: NONE

**Done when**:
- [ ] `SLOProvider.FetchSLOStatus` and `Client.FetchSLOStatus` both take `(ctx, sloID string, from, to time.Time)`
- [ ] Request hits `GET /api/v1/slo/{sloID}/history?from_ts=<from.Unix()>&to_ts=<to.Unix()>` with the existing `DD-API-KEY`/`DD-APPLICATION-KEY` headers (unchanged from `Client.get`)
- [ ] `SLOStatus.RequestCount` populated from `data.series.denominator.sum`
- [ ] `SLOStatus.State`/`SLI` populated from `data.overall.state`/`sli_value`
- [ ] `SLOStatus.Target`/`Timeframe` populated from the single entry in `data.thresholds` (map, not list — no hardcoded `"30d"` key)
- [ ] `SLOStatus.ErrorBudgetRemaining` computed as `Target - SLI`
- [ ] `ErrUnauthorized`/`ErrTimeout`/`ErrServer`/`ErrNotFound` classification unchanged (reuses `Client.get`; 404 on history maps to `ErrNotFound` same as an empty `/slo/search` result did)
- [ ] Existing `client_test.go` tests for `FetchSLOStatus` updated to the new endpoint/params/response shape
- [ ] New `client_test.go` tests: `RequestCount` decodes correctly; missing/empty `thresholds` map handled without a panic (zero-value `Target`/`Timeframe`, not a crash); malformed/empty `state` string passed through as-is (mapping to `degraded` is `normalizeStatus`'s job, not the client's — client just decodes)
- [ ] `SearchSLOs`/`ValidateCredentials` and their existing tests untouched and still passing
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./internal/connectors/datadog/...`
- [ ] `gofmt -l internal/connectors/datadog/client.go internal/connectors/datadog/client_test.go` produces no output

**Tests**: unit
**Gate**: quick

**Commit**: `feat(datadog): fetch windowed SLO history instead of fixed-timeframe status`

---

### T2: Thread the window through `FetchWithRetry` ✅ Complete

**What**: Add `from, to time.Time` params to `FetchWithRetry`, passed straight through to `provider.FetchSLOStatus`. Retry/backoff/`isTransient` logic body unchanged.
**Where**: `internal/poller/retry.go`
**Depends on**: T1
**Reuses**: `isTransient`, `backoffBase` — unchanged.
**Requirement**: RSW-01, RSW-09, RSW-10

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `FetchWithRetry(ctx, provider, sloID string, from, to time.Time, maxAttempts int) (datadog.SLOStatus, error)` — every retry attempt calls `provider.FetchSLOStatus(ctx, sloID, from, to)` with the same `from`/`to` (no per-attempt window recomputation)
- [ ] `retry_test.go`'s `fakeProvider.FetchSLOStatus` signature updated to `(ctx, sloID string, from, to time.Time)`; every existing call to `FetchWithRetry` in this file updated to pass a `from`/`to` pair
- [ ] New test: `fakeProvider` records the `from`/`to` it received; assert `FetchWithRetry` passes through the exact values given to it (not recomputed internally — window computation belongs to `pollService`, not the retry wrapper)
- [ ] Every existing retry/backoff/auth-shortcut test in this file (success-first-attempt, retries-transient, unauthorized-no-retry, exhausted-retries) still passes unmodified in behavior
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./internal/poller/...`
- [ ] `gofmt -l internal/poller/retry.go internal/poller/retry_test.go` produces no output

**Tests**: unit
**Gate**: quick

**Commit**: `feat(poller): thread the fetch window through FetchWithRetry`

---

### T3: Window computation + low-volume carry-forward guard in `pollService`

**What**: In `pollService`, compute `to := time.Now()`, `from := to.Add(-p.interval)`; call `FetchWithRetry(ctx, p.provider, svc.SLOID, from, to, maxFetchAttempts)`; add package constant `minRecentWindowRequests = 10`; branch on `status.RequestCount < minRecentWindowRequests` to carry forward `svc.CurrentStatus` instead of `normalizeStatus(status.State)`. Add a new non-integration-tagged unit test file with in-memory fakes for the `Poller`'s three small interfaces (`serviceLister`, `serviceStatusUpdater`, `statusIntervalWriter` — `integrationStatusUpdater` only if a test needs it) so this logic gets real unit coverage under the default `go test ./...` gate, without a real Postgres instance.
**Where**: `internal/poller/poller.go`
**Depends on**: T2
**Reuses**: `fakeProvider` (from `retry_test.go`, same package — already windowed after T2), `normalizeStatus` (unchanged), `db.Service` (existing struct, just constructed with a `CurrentStatus` value in the fake).
**Requirement**: RSW-01, RSW-02, RSW-03, RSW-05, RSW-06, RSW-07, RSW-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `pollService` computes `from`/`to` from `p.interval` and `time.Now()` — no new config/field
- [ ] `RequestCount >= 10` → `current := normalizeStatus(status.State)` (unchanged mapping)
- [ ] `RequestCount < 10` (including `0`) → `current := svc.CurrentStatus` (carry-forward, no recompute)
- [ ] Carry-forward still calls `statusIntervals.OpenOrExtend(ctx, svc.ID, current, ...)` and `statuses.UpdateStatus(ctx, svc.ID, current)` — no gap in the interval/status write, even though `current` is unchanged
- [ ] Fetch-failure path (exhausted retries) is untouched: no interval write, no status update, `current_status` stays whatever it was (same lines as today, just reached via the new `FetchWithRetry` call signature)
- [ ] New test: window passed to the fake provider equals `[time.Now()-p.interval, time.Now())` within a tolerance (fakes can capture wall-clock bounds around the call)
- [ ] New test: `RequestCount: 10+`, `State: "ok"` on a service whose prior `CurrentStatus` was `"outage"` → recomputes to `operational` same cycle (RSW-03)
- [ ] New test: `RequestCount: 3`, `State: "breached"` on a service whose prior `CurrentStatus` was `"operational"` → stays `operational` (carry-forward beats a noisy breach) (RSW-06)
- [ ] New test: low-volume carry-forward still invokes the fake `statusIntervalWriter`/`serviceStatusUpdater` with the carried value (RSW-07)
- [ ] New test: a service with `CurrentStatus: "not_configured"` (never polled) whose first window also has `RequestCount < 10` stays `"not_configured"` — no special-cased default in the code (RSW-08)
- [ ] Test count stated explicitly in the commit/task summary (no silent deletions from existing files)
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./internal/poller/...`
- [ ] `gofmt -l internal/poller/poller.go internal/poller/poller_recent_window_test.go` produces no output

**Tests**: unit
**Gate**: quick

**Commit**: `feat(poller): compute status from a recent window with a low-volume carry-forward guard`

---

### T4: Fix `poller_test.go`'s integration fakes for the new signature

**What**: Update `sloKeyedFakeProvider.FetchSLOStatus` and every `fakeProvider`/`sloKeyedFakeProvider` literal in this file to the new `(ctx, sloID string, from, to time.Time)` signature, and add `RequestCount: 10` (or higher) to every `datadog.SLOStatus{...}` fixture whose test asserts a specific resulting `current_status` — so this feature's low-volume guard doesn't silently change what these pre-existing integration scenarios assert. Compile-check only for this feature (no DB-touching change, per `AGENTS.md` §3's integration-gate scoping) — do not spin up a Postgres container for this task.
**Where**: `internal/poller/poller_test.go`
**Depends on**: T3
**Reuses**: Nothing new — mechanical signature/fixture update only.
**Requirement**: RSW-09, RSW-10, RSW-11 (failure-path contract unchanged — this task proves the file that tests it still compiles and its intent is preserved)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `sloKeyedFakeProvider.FetchSLOStatus` matches the new interface signature
- [ ] Every `datadog.SLOStatus{...}` literal in this file that a test uses to assert a specific resulting status gets `RequestCount: 10` (or a higher literal) added — audited line by line (this file's current fixtures at lines ~149, 154, 234, 268, 301, 351, 385 per Design's risk note; re-grep at task time since line numbers shift)
- [ ] Fixtures that only exercise the failure path (`errs` without a reachable `status`) are left as-is — they never reach the volume-guard branch
- [ ] Gate check passes (compile-only, no DB): `go build -tags=integration ./... && go vet -tags=integration ./...`
- [ ] `gofmt -l internal/poller/poller_test.go` produces no output

**Tests**: integration (compile-only per this feature's scope — see Test Coverage Matrix)
**Gate**: full

**Commit**: `fix(poller): update integration test fakes for the windowed SLOProvider signature`

---

### T5: Fix `poller_abort_test.go`'s integration fake for the new signature

**What**: Update `abortTestProvider.FetchSLOStatus` to the new `(ctx, sloID string, from, to time.Time)` signature. Compile-check only (same reasoning as T4).
**Where**: `internal/poller/poller_abort_test.go`
**Depends on**: T3
**Reuses**: Nothing new — mechanical signature update only.
**Requirement**: RSW-09, RSW-10, RSW-11

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `abortTestProvider.FetchSLOStatus` matches the new interface signature
- [ ] Any `datadog.SLOStatus{...}` literal in this file whose test asserts a resulting status gets `RequestCount: 10` (or higher) if needed — audit at task time, same reasoning as T4
- [ ] Gate check passes (compile-only, no DB): `go build -tags=integration ./... && go vet -tags=integration ./...`
- [ ] `gofmt -l internal/poller/poller_abort_test.go` produces no output

**Tests**: integration (compile-only per this feature's scope)
**Gate**: full

**Commit**: `fix(poller): update abort-test integration fake for the windowed SLOProvider signature`

---

### T6: Record AD-019 in `.specs/STATE.md`

**What**: Append a new `AD-019` entry to `.specs/STATE.md`'s `## Decisions` section documenting this feature's decision (recent-window status supersedes the `mvp-core`-era implicit assumption of mirroring the SLO's fixed-timeframe `state` 1:1), per Design's closing note and the `memory.md` convention every prior feature in this file follows.
**Where**: `.specs/STATE.md`
**Depends on**: T3 — needs the final shape of the change (constant name, window source) to describe accurately. The decision's substance doesn't depend on the mechanical fixture fixes, so this task is sequenced after Phase 3 for a clean "everything's in" record, not because it needs their output.
**Reuses**: The existing `AD-NNN` entry format already used by AD-001 through AD-018 in this file.
**Requirement**: N/A (project-memory bookkeeping, not a spec AC)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `AD-019` entry added with Decision/Reason/Trade-off/Scope/Date/Status fields, matching the existing entries' format
- [ ] Decision text names the specific files changed (`internal/connectors/datadog/client.go`, `internal/poller/{retry.go,poller.go}`) and the new contract (windowed `FetchSLOStatus`, `minRecentWindowRequests = 10` carry-forward guard)
- [ ] No other section of `STATE.md` edited by this task (Handoff/feature-completion entry is a separate, later step once the Verifier runs — not part of this task)
- [ ] Gate check passes: `go build ./... && go vet ./... && go test ./... && gofmt -l internal/connectors/datadog/client.go internal/poller/retry.go internal/poller/poller.go internal/poller/poller_recent_window_test.go internal/connectors/datadog/client_test.go internal/poller/retry_test.go internal/poller/poller_test.go internal/poller/poller_abort_test.go`

**Tests**: none (docs-only change)
**Gate**: build

**Commit**: `docs(state): record AD-019 — poller status now reflects a recent window, not the SLO's fixed timeframe`

---

## Phase Execution Map

```
Phase 1 → Phase 2 → Phase 3 → Phase 4

T1 → T2 → T3
T3 → T4
T3 → T5
T3 → T6
```

Execution is strictly sequential - there is no intra-phase parallelism. 6 tasks total fits a single batch (≤ ~8) — no sub-agent offer needed; runs inline.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: Rewrite `FetchSLOStatus` | 1 file pair (client.go + its test file) | ✅ Granular |
| T2: Thread window through `FetchWithRetry` | 1 file pair (retry.go + its test file) | ✅ Granular |
| T3: Window + carry-forward guard in `pollService` | 1 modified file + 1 new test file, one cohesive behavior | ✅ Granular |
| T4: Fix `poller_test.go` fakes | 1 file, mechanical signature/fixture fix | ✅ Granular |
| T5: Fix `poller_abort_test.go` fake | 1 file, mechanical signature fix | ✅ Granular |
| T6: Record AD-019 | 1 file, docs-only | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | (start, no arrow in) | ✅ Match |
| T2 | T1 | T1 → T2 | ✅ Match |
| T3 | T2 | T2 → T3 | ✅ Match |
| T4 | T3 | T3 → T4 | ✅ Match |
| T5 | T3 | T3 → T5 | ✅ Match |
| T6 | T3 | T3 → T6 | ✅ Match |

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1: Rewrite `FetchSLOStatus` | `datadog.Client` | unit | unit | ✅ OK |
| T2: Thread window through `FetchWithRetry` | `poller.Poller`/`FetchWithRetry` | unit | unit | ✅ OK |
| T3: Window + carry-forward guard | `poller.Poller`/`FetchWithRetry` | unit | unit | ✅ OK |
| T4: Fix `poller_test.go` fakes | `poller.Poller` real-Postgres wiring (integration file) | integration (compile-only this feature) | integration | ✅ OK |
| T5: Fix `poller_abort_test.go` fake | `poller.Poller` real-Postgres wiring (integration file) | integration (compile-only this feature) | integration | ✅ OK |
| T6: Record AD-019 | none (docs) | none | none | ✅ OK |

---

## Tips reminder (not part of the doc — carried from Design)

Blast radius stays inside `internal/connectors/datadog/client.go`, `internal/poller/{retry.go,poller.go}`, their test files, one new test file, and one `.specs/STATE.md` append. Do not touch `service-status-intervals`/`public-status-hourly-history` files — `OpenOrExtend`/`UpdateStatus`/interval bucketing are reused unchanged.
