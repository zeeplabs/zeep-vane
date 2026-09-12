# Poller Breach Threshold Rescaling — Validation

**Result**: PASS (12/12 acceptance criteria covered; 3/3 behavior-level mutants killed; no surviving mutant)

**Verifier**: independent read-only pass run inline (standalone fallback). Author = verifier limitation recorded below.
**Date**: 2026-09-12
**Feature**: `poller-breach-threshold-rescaling`
**Diff range**: `7938646~1..bb65fec` (4 commits: `7938646`, `9c58c7b`, `fa4e6c8`, `bb65fec`)
**Spec**: `.specs/features/poller-breach-threshold-rescaling/spec.md`

---

## Per-AC evidence

Each row cites the assertion, not proximity. The asserted value is the spec-defined outcome.

| AC | Requirement (spec) | Evidence (file:line) | Asserted value matches spec? |
| --- | --- | --- | --- |
| **BTR-01** | Compute `breachBound` from target, request count, sigmas | `internal/poller/breach_threshold.go:42` (`func breachBound`), formula at `:54` (`100 - 100*(p0+sigmas*se)`); `internal/poller/breach_threshold_test.go:12` asserts `math.Abs(got-99.2884) <= 0.001` for `(99.5, 10000, 3)` | ✅ Exact (hand-computed 99.2884) |
| **BTR-02** | `SLI < breachBound` → breach (hysteresis) | `internal/poller/poller.go:324`; `internal/poller/poller_recent_window_test.go:303` feeds `SLI=50` and asserts the second cycle flips to `"outage"` | ✅ Exact (`"outage"`) |
| **BTR-03** | `SLI >= bound` and below target → `"degraded"`, never `"outage"` | `internal/poller/poller.go:336`; `poller_recent_window_test.go:241` (`State="breached"`, `SLI=99.4`) asserts `"degraded"`; `:262` (`State="ok"`, `SLI=99.4 < Target`) asserts `"degraded"` | ✅ Exact (`"degraded"`) |
| **BTR-04** | `SLI >= Target` and `state=="ok"` → `"operational"` | `internal/poller/poller.go:340` (default → `normalizeStatus`); `poller_recent_window_test.go:282` (`SLI=99.9`, prior `"outage"`) asserts `"operational"` | ✅ Exact (`"operational"`) |
| **BTR-05** | `state=="breached"` is not the sole outage trigger | `internal/poller/poller.go:324` decides on `SLI < breachBound`; `poller_recent_window_test.go:241` has `State="breached"` yet asserts `"degraded"` | ✅ Exact (breached state alone does not produce outage) |
| **BTR-06** | `breachHysteresisCycles` consecutive breaches → `"outage"` | `internal/poller/poller.go:325-326`; `poller_recent_window_test.go:303` asserts cycle 2 `"outage"` | ✅ Exact |
| **BTR-07** | Single breach → carry previous status forward | `internal/poller/poller.go:327-329`; `poller_recent_window_test.go:303` asserts cycle 1 `"operational"` (carry-forward) | ✅ Exact |
| **BTR-08** | Non-breach resets the streak | `internal/poller/poller.go:337` (within-band reset) and `:339` (default reset); `poller_recent_window_test.go:328` asserts call 2 `"degraded"` then call 3 carry-forward `"operational"` | ✅ Exact (streak reset proven by call 3 not flipping) |
| **BTR-09** | `Target <= 0` → state-based fallback | `internal/poller/poller.go:319` + `classifyByState` at `:385`; `poller_recent_window_test.go:356` (`Target=0`) asserts cycle 2 `"outage"` | ✅ Exact |
| **BTR-10** | Low-volume carry-forward unchanged | `internal/poller/poller.go:314`; pre-existing tests `poller_recent_window_test.go:112`, `:133`, `:219` pass unmodified | ✅ Exact (unmodified) |
| **BTR-11** | Fetch-failure path unchanged | `internal/poller/poller.go:305-310` untouched; `internal/poller/retry_test.go:112`, `:126` and `poller_test.go` failure paths pass unmodified | ✅ Exact (unmodified) |
| **BTR-12** | No env var, migration, or schema change | Diff range touches only `internal/poller/{breach_threshold.go,poller.go}` + their test files + `.specs`; no `internal/db/migrations` or config file appears in `git diff --name-only 7938646~1..bb65fec` | ✅ Confirmed by diff scope |

**Coverage: 12/12 acceptance criteria with value-exact evidence.**

Edge cases from spec.md:
- Large `n` → bound approaches target: `breach_threshold_test.go:23` asserts within 0.1 of 99.5.
- `n <= 0`: `breach_threshold_test.go:53` asserts returns target (no NaN/Inf).
- Target outside `[0,100]`: `breach_threshold_test.go:63` asserts finite.
- `sigmas=0`: `breach_threshold_test.go:75` asserts bound == target.

---

## Discrimination sensor

Isolation method: throwaway `git worktree --detach` at `HEAD` under the session scratchpad. Mutations applied with a Python string replace, tests run, file restored via `git checkout --`, worktree removed afterward. Real-tree `git status --porcelain` was empty before and after; `git worktree list` shows only the main tree.

| # | Mutation (behavior-level) | Layer | Result | Killing tests |
| --- | --- | --- | --- | --- |
| **M1** | Bound case reverted to `case status.State == "breached"` (the addendum-3 gate) | Go, `poller.go:324` | ☠️ **Killed** (2 tests) | `TestPollService_HighVolumeBreachedStateButSLIWithinBand_DegradesNotOutage`, `TestPollService_WithinBandWindowResetsBreachStreak` |
| **M2** | `breachBound` ignores the sigmas term (`return 100 - 100*p0`, i.e. target) | Go, `breach_threshold.go:54` | ☠️ **Killed** (5 tests) | `TestBreachBound_HighVolumeWindow_ExactValue`, `TestBreachBound_NeverExceedsTarget`, `TestPollService_HighVolumeBreachedStateButSLIWithinBand_DegradesNotOutage`, `TestPollService_HighVolumeOKStateButSLIBelowTarget_Degrades`, `TestPollService_WithinBandWindowResetsBreachStreak` |
| **M3** | `Target <= 0` fallback branch removed | Go, `poller.go:319` | ☠️ **Killed** (4 tests) | `TestPollService_SingleBreachedWindow_CarriesForwardInsteadOfOutage`, `TestPollService_TwoConsecutiveBreachedWindows_FlipsToOutage`, `TestPollService_BreachStreak_ResetByAnIntermediateOKWindow`, `TestPollService_NoTarget_FallsBackToStateClassification` |

**3/3 mutants killed. No surviving mutant.** The suite discriminates on the behaviors that matter (bound arithmetic, bound-vs-state decision, streak reset, no-target fallback) rather than on shapes.

---

## Gate results

| Gate | Command | Result |
| --- | --- | --- |
| Go build | `go build ./...` | ✅ clean |
| Go vet | `go vet ./...` | ✅ clean |
| Go unit (all) | `go test ./...` | ✅ all packages ok |
| gofmt | `gofmt -l <4 changed .go files>` | ✅ empty |
| Integration compile | `go build -tags=integration ./... && go vet -tags=integration ./...` | ✅ clean (no DB-touching change, compile-check only per AGENTS.md §3) |
| New tests | `go test -run 'TestBreachBound|TestPollService_(HighVolume|WithinBand|NoTarget)' -v ./internal/poller/` | ✅ 12/12 pass |

---

## Limitations

- **Author = verifier.** No general Verifier sub-agent is available in this harness; the spec-anchored check and the discrimination sensor were run inline by the implementing agent. The sensor ran in an isolated worktree and mutated behavior, so the discrimination evidence is real, but the coverage re-derivation shares the author's mental model. Recorded, not hidden.
- **`breachThresholdSigmas` is uncalibrated.** No Datadog credential in this environment, so the false-breach rate was not measured against real traffic. The constant is documented as uncalibrated in `AD-019 addendum 4`; a future session with real data owns the calibration. This is a known, recorded gap, not a failed AC.

## Summary

The implementation matches the spec: the breach decision is now the window's SLI against a bound rescaled to its sampling variance, with the `Target <= 0` fallback preserving the old behavior verbatim and the hysteresis retained. The suite kills every behavior-level mutation attempted. **PASS.**
