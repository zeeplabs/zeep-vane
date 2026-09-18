# Poller Zero-Request False-Outage Validation

**Date**: 2026-09-17
**Spec**: `.specs/features/poller-zero-request-false-outage/spec.md`
**Diff range**: `6828969^..6828969`
**Verifier**: independent sub-agent (author ≠ verifier)

---

## Task Completion

No `tasks.md` exists for this feature — small fix executed inline per AGENTS.md ("Small fixes/UI tweaks don't need a spec[/tasks phase]"). Verified directly against `spec.md`'s 6 ACs and the commit diff.

| Task | Status  | Notes |
| ---- | ------- | ----- |
| N/A  | ✅ Done | No tasks.md; single commit `6828969` implements the fix + tests inline. |

---

## Spec-Anchored Acceptance Criteria

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| **ZEROREQ-01**: `RequestCount<=0` + `CurrentStatus=="not_configured"` → classify via `normalizeStatus(status.State)`, no `breachBound` eval, no `breachStreak` increment | `current_status` = `normalizeStatus("no_data")` = `"degraded"`; zero calls that touch `breachStreak` | `internal/poller/poller_recent_window_test.go:462` - `if len(statuses.calls) != 1 \|\| statuses.calls[0].status != "degraded"` (test at :449-465, `State:"no_data", RequestCount:0, CurrentStatus:"not_configured"`) | ✅ PASS |
| **ZEROREQ-02**: `RequestCount<=0` + `CurrentStatus` is `"outage"`/`"degraded"` → reclassify via `normalizeStatus(status.State)` instead of carry-forward, `breachStreak` unchanged | `current_status` = `normalizeStatus("ok")` = `"operational"` | `internal/poller/poller_recent_window_test.go:484` - `if len(statuses.calls) != 1 \|\| statuses.calls[0].status != "operational"` (test at :471-487, `CurrentStatus:"outage"`); degraded-origin variant at `internal/poller/poller_recent_window_test.go:518` - `if len(statuses.calls) != 1 \|\| statuses.calls[0].status != "operational"` (test at :494-525, `CurrentStatus:"degraded"`) | ✅ PASS |
| **ZEROREQ-03**: `RequestCount<=0` + `CurrentStatus=="operational"` → unchanged (`current = svc.CurrentStatus` via existing low-volume guard) | `current_status` stays `"operational"` even with `State:"breached"` (a state that would flip result if the new branch fired) | `internal/poller/poller_recent_window_test.go:545` - `if len(statuses.calls) != 1 \|\| statuses.calls[0].status != "operational"` (test at :532-548, `State:"breached", RequestCount:0, CurrentStatus:"operational"`) | ✅ PASS |
| **ZEROREQ-04**: `0 < RequestCount < minRecentWindowRequests` unchanged for every `CurrentStatus` | Carry-forward when already classified; normal classification via 086d678 when `not_configured` | Pre-existing, untouched tests: `internal/poller/poller_recent_window_test.go:141` (`RequestCount:3, CurrentStatus:"operational"` → carries `"operational"` forward) and `internal/poller/poller_recent_window_test.go:294` (`RequestCount:1, CurrentStatus:"operational"` → carries forward) and `internal/poller/poller_recent_window_test.go:253` (`RequestCount:1, CurrentStatus:"not_configured"` → classifies to `"operational"`, SLOTRAF-01 path). None of these were modified by `6828969` (diff only appended new tests + one new `switch` case). | ✅ PASS |
| **ZEROREQ-05**: reclassification under ZEROREQ-01/02 that changes status still fires `p.analyzer.HandleTransition` exactly as before | `HandleTransition` dispatches once via the unmodified `transitioned` gate | `internal/poller/poller_recent_window_test.go:522` - `if len(calls) != 1 \|\| calls[0] != nil` (test at :494-525, real `SLOAnalyzer` wired via `NewSLOAnalyzer`, `services.snapshot()` shows exactly 1 `UpdateStatusAnalysis` call with `nil`, proving `HandleTransition` ran through the pre-existing dispatch path, not a new mechanism) | ✅ PASS |
| **ZEROREQ-06**: fetch error → `pollService` returns error, `current_status` untouched, unchanged from today | Error path returns before the `switch` block; `current_status` stays at pre-poll value | Not modified by this diff (`internal/poller/poller.go:314-319` is outside the changed hunk). Covered by pre-existing integration test `internal/poller/poller_test.go:178-225` (`//go:build integration`, `TestPoller_PollOnce_ConnectionFailure_MarksIntegrationInvalidAndKeepsLastStatus`) - `internal/poller/poller_test.go:224` - `if found.CurrentStatus != "operational"`. Not exercised by the default `go test ./internal/poller/...` run (integration-tagged, requires `TEST_DATABASE_URL`); traceability table itself marks this "unchanged code path, covered by pre-existing tests" rather than claiming new coverage. | ✅ PASS (unchanged path; pre-existing coverage is integration-gated, not exercised in this gate run) |

**Status**: ✅ All ACs covered. One note flagged below (not a gap, a labeling precision issue in the test suite).

**Spec-precision note (not a functional gap)**: `TestPollService_ZeroRequestWindow_NeitherAdvancesNorResetsBreachStreak` (`poller_recent_window_test.go:558-593`) is commented as "the ZEROREQ-04 test," but its actual assertion (breach streak surviving a zero-request cycle untouched, in both directions) is evidence for the "sem incrementar/alterar `p.breachStreak[svc.ID]`" clauses of **ZEROREQ-01/02**, not for ZEROREQ-04's actual claim (the `0 < RequestCount < minRecentWindowRequests` range being unaffected — that range never appears in this test; every `RequestCount` used is either `0` or `5000`). ZEROREQ-04 itself is still adequately covered, just by the pre-existing tests cited above, not by this new test. Mislabeling only, no behavior gap.

---

## Discrimination Sensor

Ran in an isolated git worktree (`git worktree add <scratch> HEAD`, mutated there, `git worktree remove --force` after each), never `git stash`. Baseline `git status --porcelain` on the real tree was empty before and after all three mutations.

| Mutation | File:line | Description | Killed? |
| --- | --- | --- | --- |
| 1 | `internal/poller/poller.go:323` | Dropped the `svc.CurrentStatus` guard, leaving only `case status.RequestCount <= 0:` | ✅ Killed - `TestPollService_ZeroRequestWindow_OperationalCarriesForwardUnchanged` failed (got `"outage"` instead of `"operational"`), correctly exposing that an `"operational"` service with `RequestCount<=0` would now be reclassified by state instead of carried forward (ZEROREQ-03 violation). |
| 2 | `internal/poller/poller.go:338` | Changed `current = normalizeStatus(status.State)` to a fixed `current = "outage"` | ✅ Killed - 4 tests failed: `TestPollService_ZeroRequestFirstPoll_ClassifiesViaStateInsteadOfOutage`, `TestPollService_ZeroRequestWindow_RecoversStuckOutage`, `TestPollService_ZeroRequestRecovery_StillDispatchesTransitionHandling`, `TestPollService_ZeroRequestWindow_NeitherAdvancesNorResetsBreachStreak` (ZEROREQ-01/02/05 violated). |
| 3 | `internal/poller/poller.go:322-338` | Moved the zero-request `case` to after the low-volume-guard `case` (wrong evaluation order) | ✅ Killed - 3 tests failed: `TestPollService_ZeroRequestWindow_RecoversStuckOutage`, `TestPollService_ZeroRequestRecovery_StillDispatchesTransitionHandling`, `TestPollService_ZeroRequestWindow_NeitherAdvancesNorResetsBreachStreak` (the low-volume guard's `svc.CurrentStatus != "not_configured"` now intercepts `"outage"`/`"degraded"` first and carries forward instead of reclassifying - ZEROREQ-02 violated). |

**Sensor depth**: lightweight (3 targeted behavior-level mutations, proportional to a narrow bug-fix on a non-payment/non-auth path)
**Result**: 3/3 killed - PASS ✅
**Isolation check**: `git status --porcelain` on the real worktree was empty before sensor work and remained empty after `git worktree remove --force` for all three mutation rounds; `git worktree list` shows only the primary worktree after cleanup.

---

## Code Quality

| Principle | Status |
| --- | --- |
| No features beyond what was asked | ✅ - single new `switch` case, no new abstractions |
| No abstractions for single-use code | ✅ |
| No unnecessary "flexibility" added | ✅ |
| Only touched files required for task | ✅ - `poller.go`, its test file, and `spec.md` |
| Didn't "improve" unrelated code | ✅ |
| Matches existing patterns/style | ✅ - mirrors `classifyByState`'s existing state-fallback pattern and comment density |
| Would senior engineer approve? | ✅ |
| Tests map to acceptance criteria and are non-shallow | ✅ - each test asserts the exact resulting status string, not just "no error" |
| Spec-anchored outcome check | ✅ - see table above; asserted values match spec-defined outcomes exactly |
| Per-layer Coverage Expectation met | ✅ - domain logic (`pollService`'s `switch`) has direct 1:1 test coverage for every new branch condition |
| Every test in scope maps to a spec AC | ⚠️ - all 5 new tests map to real ACs, but one (`TestPollService_ZeroRequestWindow_NeitherAdvancesNorResetsBreachStreak`) is mislabeled as ZEROREQ-04 in its comment when its actual evidentiary value is for ZEROREQ-01/02's breachStreak clause (see spec-precision note above) |
| Documented project quality/testing guidelines followed | ✅ - `AGENTS.md` §3 gate commands run clean; no project-specific Go testing style doc beyond that, strong defaults applied |

---

## Edge Cases

- [x] `RequestCount==0` + unrecognized/empty `status.State` → falls to `normalizeStatus` default (`"degraded"`) - unchanged code, no new test needed per spec (behavior already existing, only reachability changed)
- [x] Real outage that also zeros traffic in the same cycle → shows `"degraded"` instead of frozen `"outage"` - documented and accepted trade-off in spec.md, matches ZEROREQ-02's reclassify-via-state behavior; no code path exists to distinguish this case from a benign zero-traffic window (correct per spec, since this is stated as accepted, not a defect)

---

## Gate Check

- **Gate command**: `go build ./...`, `go vet ./...`, `gofmt -l internal/poller/poller.go internal/poller/poller_recent_window_test.go`, `go test ./internal/poller/...`
- **Result**: all 4 commands clean (build succeeds, vet clean, gofmt reports zero files, `go test ./internal/poller/...` → `ok`, 0 failures)
- **Test count before feature** (package `internal/poller`, non-integration): 70 (75 current − 5 new)
- **Test count after feature**: 75
- **Delta**: +5 new tests (`poller_recent_window_test.go`, lines 449, 471, 494, 532, 558)
- **Skipped tests**: none
- **Failures**: none

---

## Fix Plans

None — no functional gaps found. One optional low-priority cleanup noted below (not blocking).

### Optional Fix 1: Mislabeled test comment

- **Root cause**: `TestPollService_ZeroRequestWindow_NeitherAdvancesNorResetsBreachStreak`'s doc comment claims it is "the ZEROREQ-04 test," but its assertions target the breachStreak-untouched clause of ZEROREQ-01/02, not ZEROREQ-04's actual `0 < RequestCount < minRecentWindowRequests` range.
- **Fix task**: Update the comment at `internal/poller/poller_recent_window_test.go:550-557` to reference ZEROREQ-01/ZEROREQ-02 instead of ZEROREQ-04.
- **Priority**: Cosmetic — does not affect coverage or correctness; ZEROREQ-04 remains adequately covered by pre-existing tests.

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| --- | --- | --- |
| ZEROREQ-01 | Verified | ✅ Verified (confirmed) |
| ZEROREQ-02 | Verified | ✅ Verified (confirmed) |
| ZEROREQ-03 | Verified | ✅ Verified (confirmed) |
| ZEROREQ-04 | Verified | ✅ Verified (confirmed, via pre-existing tests, not the mislabeled new one) |
| ZEROREQ-05 | Verified | ✅ Verified (confirmed) |
| ZEROREQ-06 | Verified (unchanged code path, covered by pre-existing tests) | ✅ Verified (confirmed - integration-gated coverage, not exercised in this unit-gate run) |

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 6/6 ACs matched spec outcome, 0 spec-precision gaps, 1 cosmetic test-comment mislabeling noted
**Sensor**: 3/3 mutations killed
**Gate**: 4/4 commands passed (build, vet, gofmt, test)

**What works**: The new `case status.RequestCount <= 0 && (...)` branch correctly intercepts zero-request windows for `not_configured`/`outage`/`degraded` services, reclassifies via `normalizeStatus(status.State)`, never touches `breachStreak`, and still dispatches `HandleTransition` through the existing unmodified mechanism. The `operational` case and the `0 < RequestCount < min` range are provably untouched (guard ordering matters and is correctly first in the `switch`).

**Issues found**: None functional. Cosmetic: one test's doc comment cites the wrong AC ID (see Fix Plans).

**Next steps**: None required to ship. Optionally correct the test comment mislabel in a follow-up commit.
