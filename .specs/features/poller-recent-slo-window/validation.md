# Poller Recent SLO Window Validation

**Date**: 2026-09-08
**Spec**: `.specs/features/poller-recent-slo-window/spec.md`
**Diff range**: `ea47931..9906f25` (6 commits: `9ed9b8b`, `1b11078`, `c05db01`, `8a8abbd`, `2bb369b`, `9906f25`)
**Verifier**: independent sub-agent (author ≠ verifier)

---

## Task Completion

| Task | Status  | Notes |
| ---- | ------- | ----- |
| T1   | ✅ Done | `FetchSLOStatus` rewritten to fetch `/slo/{id}/history`, `SLOStatus.RequestCount` added |
| T2   | ✅ Done | `FetchWithRetry` threads `from, to` through unchanged retry/backoff logic |
| T3   | ✅ Done | `pollService` computes window, applies `minRecentWindowRequests = 10` carry-forward guard; new `poller_recent_window_test.go` |
| T4   | ✅ Done | `poller_test.go` fakes updated to windowed signature, `RequestCount: 10`+ added to status-asserting fixtures |
| T5   | ✅ Done | `poller_abort_test.go` fake updated to windowed signature |
| T6   | ✅ Done | AD-019 recorded in `.specs/STATE.md:152-158` |

**Out-of-band note (not a task gap, flagged for the orchestrator):** `.specs/features/poller-recent-slo-window/spec.md` and `design.md` are untracked in git (`git status --porcelain` shows both as `??`) — only `tasks.md` was ever committed. Not a code/test defect and outside this Verifier's mandate to fix, but the feature's planning artifacts are not currently versioned alongside the implementation.

---

## Spec-Anchored Acceptance Criteria

### P1: Recent-window status replaces 30-day SLO state

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| RSW-01: poll cycle requests SLO status for `[now-interval, now)` instead of fixed 30d timeframe | Window = `[to-p.interval, to)`, hits `GET /slo/{id}/history?from_ts=&to_ts=` | `internal/poller/poller.go:160-161` (`to := time.Now(); from := to.Add(-p.interval)`); `internal/poller/poller_recent_window_test.go:76-79` - `wantFrom := gotTo.Add(-time.Hour); if !gotFrom.Equal(wantFrom)`; `internal/connectors/datadog/client.go:169-170` builds `sloHistoryPath` endpoint with `from.Unix()`/`to.Unix()`; `internal/connectors/datadog/client_test.go:68-72` - `if gotFromTs != "1000"` / `if gotToTs != "2000"` | ✅ PASS |
| RSW-02: `RequestCount >= 10` → `current_status` from `normalizeStatus(status.State)` | `ok`→`operational`, `warning`→`degraded`, `breached`→`outage`, other→`degraded` | `internal/poller/poller.go:176-177`; `internal/poller/poller_recent_window_test.go:82-101` (`RequestCount: 10`, `State: "ok"` → `statuses.calls[0].status != "operational"`); mapping table unchanged, `internal/poller/poller_status_test.go:5-20` (`TestNormalizeStatus`, all 4 branches) | ✅ PASS |
| RSW-03: `state` maps to `operational` after prior `degraded`/`outage` → updates same cycle, no cooldown | `current_status` flips to `operational` within one poll | `internal/poller/poller_recent_window_test.go:82-101` - `db.Service{CurrentStatus: "outage"}` in, `statuses.calls[0].status == "operational"` out, single call | ✅ PASS |
| RSW-04: stop reading `status.state`/`error_budget_remaining` from `/slo/search` for `current_status` | `pollService`'s only Datadog call is windowed `FetchSLOStatus`; `SearchSLOs` (still `/slo/search`) has zero poller callers | `internal/poller/poller.go:163` (`FetchWithRetry(ctx, p.provider, svc.SLOID, from, to, ...)` → `provider.FetchSLOStatus`, history endpoint per RSW-01); grep confirms `SearchSLOs` callers are only `internal/cli/routes.go:203` and `internal/api/integrations_handler.go:171` (admin name-lookup, not poller) | ✅ PASS |
| RSW-05: status differs from previous → open/extend interval + update cached status exactly as today | `OpenOrExtend` then `UpdateStatus`, unconditionally, unchanged call sites | `internal/poller/poller.go:180-190` (unchanged from pre-feature shape - same two calls, same order); `internal/poller/poller_recent_window_test.go:98-100` (`intervals.calls[0].status == "operational"`); real-DB confirmation `internal/poller/poller_test.go:90-98` | ✅ PASS |

### P1: Low-volume guard prevents noise-driven status flapping

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| RSW-06: `RequestCount < 10` → carry forward previous `current_status`, no recompute | Exact prior value preserved even against a contradicting `state` | `internal/poller/poller.go:171,175`; `internal/poller/poller_recent_window_test.go:103-122` - `RequestCount: 3`, `State: "breached"`, prior `CurrentStatus: "operational"` → `statuses.calls[0].status != "operational"` fails if not carried forward (i.e. asserts it stays `"operational"`, not `"outage"`) | ✅ PASS |
| RSW-07: carry-forward still opens/extends interval (no hourly-history gap) | `OpenOrExtend`/`UpdateStatus` both still called once, with the carried value | `internal/poller/poller_recent_window_test.go:124-143` - `RequestCount: 0`; `len(intervals.calls) != 1` / `len(statuses.calls) != 1` both asserted == 1 | ✅ PASS |
| RSW-08: first-ever poll (`not_configured`) + low volume → stays `not_configured`, no special-cased default | `current_status` remains exactly `"not_configured"` | `internal/poller/poller_recent_window_test.go:145-161` - `RequestCount: 1`, `CurrentStatus: "not_configured"` in → `statuses.calls[0].status != "not_configured"` asserted false | ✅ PASS |

### P2: Failure handling stays unchanged

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| RSW-09: timeout/5xx retried up to `maxFetchAttempts`, unchanged | 2 transient failures then success = 3 calls total | `internal/poller/retry_test.go:80-98` (`TestFetchWithRetry_SuccessThirdAttempt_RetriesTransientErrors` - `provider.calls != 3`); window passthrough also re-verified post-signature-change at `retry_test.go:56-78` | ✅ PASS |
| RSW-10: 401/403 never retried | 1 call only, `ErrUnauthorized` returned | `internal/poller/retry_test.go:114-129` - `errors.Is(err, datadog.ErrUnauthorized)`, `provider.calls != 1` | ✅ PASS |
| RSW-11: every retry attempt fails → `current_status` untouched, no interval write | Last-known status preserved, integration marked invalid | `internal/poller/poller_test.go` `TestPoller_PollOnce_ConnectionFailure_MarksIntegrationInvalidAndKeepsLastStatus` (integration-tagged, seeds `operational` via a real poll, then fails 3x, asserts `integration.Status == "invalid"` and the service's last status is unchanged) | ⚠️ Spec-precision-adjacent: compile-verified only (`go build -tags=integration ./...` per this feature's own Gate Check scoping, since no DB-touching code changed) - not executed against a live Postgres in this validation run. Behavior is unchanged from pre-feature code (only the fake's signature changed), so risk is low, but the assertion was not observed to pass live in this session. |

**Status**: ✅ 10/11 ACs fully covered and executed; 1/11 (RSW-11) covered by an existing, unmodified-in-behavior integration test that was compile-checked but not executed in this session, consistent with the feature's own `AGENTS.md`-derived gate scoping (no DB-touching change). No spec-precision gaps - every criterion with a defined outcome was matched to an exact assertion.

---

## Discrimination Sensor

| Mutation | File:line | Description | Killed? |
| --- | --- | --- | --- |
| 1 | `internal/poller/poller.go:171` | Flipped volume-guard boundary `RequestCount < minRecentWindowRequests` → `<= minRecentWindowRequests` | ✅ Killed - `TestPollService_SufficientVolume_RecomputesFromRecoveredState` (RequestCount:10 now wrongly treated as low-volume, stayed `"outage"` instead of recomputing `"operational"`) |
| 2 | `internal/poller/poller.go:161` | Halved the window: `to.Add(-p.interval)` → `to.Add(-p.interval / 2)` | ✅ Killed - `TestPollService_WindowPassedToProvider_MatchesIntervalBounds` (`from` off by 30 min from expected `to - 1h`) |
| 3 | `internal/connectors/datadog/client.go:193` | Sign-flipped `ErrorBudgetRemaining = Target - SLI` → `SLI - Target` | ✅ Killed - `TestFetchSLOStatus_ValidResponse_ReturnsNormalizedStatus` (`ErrorBudgetRemaining = 0.404`, want `-0.404`) |

**Sensor depth**: lightweight (3 mutations, standard-tier feature)
**Result**: 3/3 killed - PASS ✅

**Isolation**: mutations applied only inside a temporary `git worktree` (`git worktree add <scratch> HEAD`), removed via `git worktree remove --force` after the run. `git status --porcelain` on the real tree was identical before and after the sensor run (2 pre-existing untracked files, `design.md`/`spec.md`, unchanged) - no `git stash` used.

---

## Interactive UAT Results

Not performed - this is a backend-only poller/connector change with no UI surface (per `validate.md` §3: "For backend-only or infrastructure work, automated checks are sufficient").

---

## Code Quality

| Principle | Status |
| --- | --- |
| Minimum code | ✅ - blast radius matches design's stated scope exactly (client.go, retry.go, poller.go, their 4 test files, 1 new test file, STATE.md) |
| Surgical changes | ✅ - `SearchSLOs`/`ValidateCredentials` untouched; `OpenOrExtend`/`UpdateStatus`/interval-writer contracts untouched |
| No scope creep | ✅ - no config/schema/migration added, matching spec's Out of Scope table |
| Matches patterns | ✅ - `sloHistoryResponse` follows the same "don't hardcode a map/list key" pattern as the pre-existing `sloSearchResponse` |
| Spec-anchored outcome check (asserted values match spec) | ✅ - see AC table above, exact values (e.g. `RequestCount: 3`/`10`, specific `CurrentStatus` transitions) asserted, not just "no error" |
| Per-layer Coverage Expectation met (domain 1:1 ACs; routes happy+edge+error) | ✅ - `datadog.Client` and `poller.Poller` both hit all branches per the Test Coverage Matrix in `tasks.md` |
| Every test maps to a spec requirement - no unclaimed tests | ✅ - every new test in `poller_recent_window_test.go`/`client_test.go` traces to an RSW ID or listed edge case (see AC table) |
| Documented guidelines followed | ✅ - `AGENTS.md` §3 (`go build`/`go vet`/`go test`/`gofmt`, integration gate scoped to DB-touching changes only) |

---

## Edge Cases

- [x] Zero data points (`denominator.sum = 0`) treated as below-threshold (carry-forward): `internal/poller/poller_recent_window_test.go:124-143` (`RequestCount: 0`)
- [x] Missing/malformed `state` in history response → `normalizeStatus` default (`degraded`), never silently `operational`: client passthrough at `internal/connectors/datadog/client_test.go:116-135` (`TestFetchSLOStatus_MalformedState_PassedThroughAsIs`, state `"totally-unknown"` passed through unmodified); mapping itself at `internal/poller/poller_status_test.go:12` (`"no_data"` → `"degraded"`, unchanged default branch)
- [x] SLO deleted/`sloID` not found → `ErrNotFound`, unchanged failure path: `internal/connectors/datadog/client_test.go:151-163`
- [x] `POLL_INTERVAL_SECONDS` reconfigured → next poll immediately uses the new interval: falls out of `poller.go:161` computing `from` fresh from `p.interval` on every call, no stored/cached window value; no dedicated test needed (not a stored value, not a distinct AC) - inspected directly, no counter-evidence found

---

## Gate Check

- **Gate command**: `go build ./... && go vet ./... && go test ./... && gofmt -l <changed files>`
- **Result**: all 16 testable packages passed (`ok`), 0 failed, 0 skipped; `gofmt -l` on all 8 changed Go files produced no output
- **Additional check run**: `go build -tags=integration ./... && go vet -tags=integration ./...` - clean (per T4/T5's own compile-only scope, no DB-touching code in this feature)
- **Package-level pass counts** (`go test -v ./internal/poller/... ./internal/connectors/datadog/...`): 23 subtests passed, 0 failed
- **Test count before/after feature**: not independently measurable from this diff alone (no pre-feature snapshot available to this Verifier), but no test deletions were observed in the diff - `poller_recent_window_test.go` is wholly new (5 tests), existing files gained fixture updates (`RequestCount` fields) without removing assertions
- **Skipped tests**: none observed in the executed run
- **Failures**: none

---

## Fix Plans

None - no issues found.

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| --- | --- | --- |
| RSW-01 | Pending | ✅ Verified |
| RSW-02 | Pending | ✅ Verified |
| RSW-03 | Pending | ✅ Verified |
| RSW-04 | Pending | ✅ Verified |
| RSW-05 | Pending | ✅ Verified |
| RSW-06 | Pending | ✅ Verified |
| RSW-07 | Pending | ✅ Verified |
| RSW-08 | Pending | ✅ Verified |
| RSW-09 | Pending | ✅ Verified |
| RSW-10 | Pending | ✅ Verified |
| RSW-11 | Pending | ✅ Verified (compile-checked integration test, behavior unchanged - see AC table note) |

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 11/11 ACs matched spec-defined outcomes, 0 spec-precision gaps
**Sensor**: 3/3 mutations killed
**Gate**: all packages passed, 0 failed, 0 skipped, `gofmt` clean

**What works**: Window computation (`[now-interval, now)`), the 10-request low-volume carry-forward guard (including the zero-request and first-ever-poll cases), the `ok`/`warning`/`breached`/other → `operational`/`degraded`/`outage`/`degraded` mapping fed by the new history endpoint, and the entire retry/failure-path contract (timeout/5xx retried, 401/403 not retried, exhausted retries leave `current_status` untouched) are all covered by tests whose assertions target the exact spec-defined outcome, not just "no error." The discrimination sensor confirms these assertions are not shallow - all 3 injected behavior-level faults (guard boundary, window length, error-budget sign) were caught.

**Issues found**: None blocking. One process note (not a code/test defect): `spec.md`/`design.md` for this feature are untracked in git - only `tasks.md` and the code/STATE.md changes were committed across the 6 feature commits.

**Next steps**: None required for this feature. Recommend the orchestrator/user `git add` and commit `spec.md`/`design.md` alongside this `validation.md` so the feature's full paper trail is versioned, per `AGENTS.md`'s expectation that `.specs/` is the source of truth for *why*.
