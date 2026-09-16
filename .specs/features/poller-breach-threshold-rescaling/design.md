# Poller Breach Threshold Rescaling Design

## Approach Exploration

### Option A - Statistical tolerance band from the SLO's own target (recommended)

Replace the `state == "breached"` trigger with a comparison of the window's `SLI` against a bound derived from the SLO's configured `Target` and the window's `RequestCount`:

```
p0 = (100 - Target) / 100                 // allowed error fraction
se = sqrt(p0 * (1 - p0) / RequestCount)   // binomial standard error of the error rate
breachBound = 100 - 100 * (p0 + z * se)   // SLI below this is a breach
```

A window breaches when `SLI < breachBound`. The bound is always `<= Target` (since `z*se >= 0`), so it is strictly more lenient than Datadog's fixed-timeframe verdict, and it converges to `Target` as `RequestCount` grows - variance is removed, real degradation is not.

**Pros**: no new config, no migration; uses only fields already on `SLOStatus`; converges correctly at high volume; pure function, trivially unit-testable with synthetic inputs.
**Cons**: models errors as independent (real errors are bursty, so the band can be optimistic); `z` ships uncalibrated because this environment has no Datadog credential.

### Option B - Configurable recent-window threshold per service/SLO

Add a per-service (or per-SLO) threshold column + API + admin UI, defaulting to a documented value.

**Pros**: statistically honest per deployment; tunable in production without a code change.
**Cons**: new schema, API, UI, and validation surface; heavy for a classification heuristic that the user explicitly wanted to keep config-free. Rejected by the user during brainstorming.

### Option C - Multi-window burn-rate (short + long window)

Compare a short-window burn rate against a long-window budget burn rate, as Google SRE describes.

**Pros**: the most robust signal, naturally window-relative.
**Cons**: needs multiple history queries per poll, historical per-service samples to calibrate the burn thresholds, and a cold-start story for newly connected services. Out of scope for a mechanism-only change.

**Decision**: Option A. It corrects the root mismatch (what the window is compared against) with the smallest possible surface, and the existing hysteresis remains as cheap insurance.

## Architecture Overview

The change is confined to `internal/poller` and is a pure classification rewrite:

```
pollService (poller.go)
  ├─ RequestCount < minRecentWindowRequests ──► carry forward        (unchanged)
  ├─ Target <= 0 ───────────────────────────► state-based fallback   (unchanged behavior)
  └─ otherwise
       ├─ SLI < breachBound(...) ───────────► breach → hysteresis    (breachStreak)
       ├─ SLI < Target || state in {warning, breached} ──► "degraded"
       └─ else ─────────────────────────────► normalizeStatus(state)
```

`breachBound` is a new pure helper in its own file, so the classification math is testable without a `Poller`, a provider, or a database. The `SLOStatus` model, the `SLOProvider` contract, `FetchWithRetry`, the window constants, the low-volume guard, and the persistence path are all untouched.

## Code Reuse Analysis

### Existing Components to Leverage

- `datadog.SLOStatus` (`internal/connectors/datadog/client.go`): already carries `SLI`, `Target`, and `RequestCount` - the three inputs the bound needs. No model change.
- `poller.breachHysteresisCycles` and `Poller.breachStreak` (`internal/poller/poller.go`): the existing hysteresis mechanism, reused unchanged; only what feeds it changes.
- `poller.minRecentWindowRequests`: the low-volume guard stays the first branch, unchanged.
- `poller.normalizeStatus` (`internal/poller/poller.go`): still the mapping for `ok`/unknown states; its doc comment about never seeing `"breached"` stays accurate because the breach is intercepted before it.
- The `fakeProvider` in `internal/poller/retry_test.go`: already returns per-call `SLOStatus` values via its `statuses []datadog.SLOStatus` field, so new tests need no new fake.

### Integration Points

- `Poller.pollService` (`internal/poller/poller.go`): the only production caller of the new helper.
- Existing tests in `internal/poller/poller_recent_window_test.go`: the hysteresis tests currently use `Target = 0`, so they exercise the new fallback path and keep passing unmodified; new tests add the bound-driven paths.

## Components

### `breachBound` (new, `internal/poller/breach_threshold.go`)

Pure function:

```go
func breachBound(target float64, requestCount int64, sigmas float64) float64
```

Returns the SLI below which a window with `requestCount` requests is a breach, for an SLO with the given `target` and tolerance `sigmas`. Total over its inputs (no panic on low `requestCount`, `target <= 0`, or out-of-range values) - callers apply the `Target <= 0` and low-volume guards before calling it.

### `breachThresholdSigmas` (new constant, same file)

```go
const breachThresholdSigmas = 3.0
```

Documented as uncalibrated, in the same style as the other AD-019 constants.

### `Poller.pollService` (modified, `internal/poller/poller.go`)

The `switch` gains the bound comparison and the below-target-within-band case:

```go
case status.RequestCount < minRecentWindowRequests:
    current = svc.CurrentStatus
case status.Target <= 0:
    // compatibility fallback: no usable target, keep the old state-based gate
    current = breachFallbackState(status, svc, p)   // existing hysteresis + normalizeStatus
case status.SLI < breachBound(status.Target, status.RequestCount, breachThresholdSigmas):
    p.breachStreak[svc.ID]++
    if p.breachStreak[svc.ID] >= breachHysteresisCycles {
        current = "outage"
    } else {
        current = svc.CurrentStatus
    }
case status.State == "warning" || status.State == "breached" || status.SLI < status.Target:
    p.breachStreak[svc.ID] = 0
    current = "degraded"
default:
    p.breachStreak[svc.ID] = 0
    current = normalizeStatus(status.State)
```

The fallback branch factors the pre-change logic (state breach + hysteresis, else `normalizeStatus`) into a small helper so the `Target <= 0` path is demonstrably the old behavior rather than a near-copy.

## Data Models

No data model changes.

- `datadog.SLOStatus` is unchanged (`SLI`, `Target`, `RequestCount` already present).
- No DB schema, no migration, no API field.
- The helper's contract is float-in/float-out; no new exported type.

## Error Handling Strategy

- The helper is pure and total: no errors, no panics. Guards for `Target <= 0` and low volume live in `pollService` before the call.
- A malformed response (`SLI` outside `[0,100]`, `Target > 100`, negative `RequestCount`) still yields a finite bound; the arithmetic cannot panic.
- The fetch-failure contract is untouched: an exhausted `FetchWithRetry` returns early before any classification, exactly as today.
- `no_data`/unknown `state` with `SLI >= Target` falls through to `normalizeStatus`'s `degraded` default (never `operational` on indeterminate data).

## Risks & Concerns

- **Bursty errors break the independence assumption.** The binomial band underestimates variance when errors cluster, so it can still call a breach on a genuinely healthy service more often than the nominal 0.13%. Mitigation: hysteresis absorbs single-window noise, and the `z` constant is documented as uncalibrated with a real-data pass as the follow-up. This is the same "unconfirmed constant, easy to tune" posture the AD-019 lineage already accepted three times.
- **`Target` is the alphabetically-first timeframe when the SLO has several.** Pre-existing behavior from AD-019 addendum 2; the band inherits it rather than adding a second tie-break. Flagged, not changed here.
- **The `Target <= 0` fallback preserves the bug for SLOs with no thresholds.** Deliberate: without a target there is no honest bound. Documented; a future config surface (Option B) is the path for those SLOs.
- **Detection latency is unchanged but now relative to a looser threshold.** A real outage whose SLI dips just below `Target` but not below `breachBound` is classified `degraded`, not `outage` - by design, since that dip is within sampling noise. A sustained real outage drives SLI far below the bound and still trips within `breachHysteresisCycles`.

## Tech Decisions

- **Bound, not burn-rate.** Option A is the smallest change that corrects the comparison; burn-rate is deferred.
- **Hysteresis stays.** AD-019 addendum 3 item 4 explicitly frames rescaling and hysteresis as complementary; removing the stopgap now would trade one known failure mode for another before calibration.
- **Pure helper in its own file.** Keeps `poller.go` focused and lets the classification math be tested 1:1 against the ACs without infrastructure.
- **No clamp on a negative bound.** A negative bound means "no plausible SLI breaches" for a tiny sample; treating every non-negative SLI as inside the band is the correct degenerate behavior, not an error.

## Tips reminder (not part of the doc - for Tasks phase)

Blast radius: `internal/poller/breach_threshold.go` (new), `internal/poller/breach_threshold_test.go` (new), `internal/poller/poller.go`, `internal/poller/poller_recent_window_test.go`, and a `.specs/STATE.md` append. Do not touch `internal/connectors/datadog`, the window constants, the persistence path, or any integration-tagged file (no signature changes, so they keep compiling).
