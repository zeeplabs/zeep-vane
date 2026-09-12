# Poller Breach Threshold Rescaling Specification

## Problem Statement

`Poller.pollService` (`internal/poller/poller.go:319`) commits a service to `"outage"` whenever Datadog's `overall.state` for the requested window is `"breached"`. Datadog computes that `state` by comparing the window's SLI against the SLO's **configured timeframe target** (typically 99.5% over 30 days) **without rescaling the target to the window width**. Applying a 30-day-calibrated threshold to a 5-minute bucket is drastically stricter: on a high-traffic service a single unlucky burst of errors, well inside normal variance for a 30-day budget, trips `"breached"` on its own. Confirmed live (2026-09-08) against a real, 30-day-healthy SLO (`api-gateway`, 99.95% SLI, 90.9% budget remaining): **2 of 6 consecutive 5-minute windows reported `"breached"`** despite 8k-13k requests each. The current mitigation, `breachHysteresisCycles = 2` (AD-019 addendum 3), is an explicitly named stopgap: it absorbs single-window noise but does not correct the root statistical mismatch, and `internal/history`'s worst-status-wins hourly bucketing still paints an entire hour red from two unlucky windows. The root fix belongs in **what the window's SLI is compared against**, not in how many times a wrong comparison is repeated.

## Goals

- [ ] A high-traffic, healthy service no longer flips to `"outage"` from a 5-minute window whose SLI is consistent with the SLO's own error budget, even when Datadog's `overall.state` says `"breached"`.
- [ ] A genuine outage (SLI far below the window-appropriate bound) still surfaces, within the existing hysteresis latency.
- [ ] No new configuration surface, no migration, no DB schema change.
- [ ] The low-volume carry-forward guard and the fetch-failure contract stay unchanged.

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
| --- | --- |
| Per-service / per-SLO configurable recent-window threshold (schema + API + UI) | User chose the statistical-band approach (Design Option A) over adding a config surface; keeps this a single-constant, no-migration change like every prior AD-019 step. |
| Multi-window burn-rate alerting (short + long window combination) | More robust but needs historical per-service samples and a calibration pass; out of scope for a mechanism-only change. |
| Live calibration of `breachThresholdSigmas` against real Datadog traffic | This environment has no Datadog credential. The constant ships documented as uncalibrated, exactly like `minRecentWindowRequests`, `recentWindowWidth`, and `recentWindowLag` did; a future session with real data owns the calibration. |
| Changing `recentWindowWidth` / `recentWindowLag` / `minRecentWindowRequests` / `breachHysteresisCycles` | All four are prior AD-019 decisions; this feature only changes the comparison the window feeds. |
| Exposing the computed bound, SLI, or target on any API or UI | The bound is an internal classification input; no user-facing surface is requested or needed. |
| Re-deriving `ErrorBudgetRemaining` semantics | The column stays DB-only and write-only (AD-019 addendum 2); untouched. |
| Removing hysteresis | AD-019 addendum 3 item 4: a correctly-rescaled threshold reduces false breaches at the source, but hysteresis remains cheap insurance against genuine short blips once the threshold stops being systematically wrong. Kept. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Breach signal | Compare the window's `SLI` against a window-appropriate bound; do **not** use Datadog's `overall.state == "breached"` as the breach trigger. | AD-019 addendum 3 item 1: `overall.state` is Datadog's compliance verdict against a target calibrated for a different window width - the fix belongs in what is compared. | y |
| Bound formula | `p0 = (100 - Target) / 100`; `se = sqrt(p0 * (1 - p0) / RequestCount)`; `breachBound = 100 - 100 * (p0 + z * se)`. A window breaches when `SLI < breachBound`. | First-order binomial tolerance band around the SLO's own allowed error rate: the same error-rate mean, widened by the sampling variance the window width actually has. Requires only `Target` and `RequestCount`, both already on `SLOStatus` - no history, no new config. | y |
| Tolerance factor `breachThresholdSigmas` | `3` (≈0.13% false-breach rate per window if the true rate equals the target). | Smallest conventional "3-sigma" starting point; conservative enough to remove the observed 2-of-6 noise, loose enough that a real outage (SLI near 0) trips regardless of `z`. Ships uncalibrated - a single constant, easy to tune, no migration. | n - flagged for a future real-data calibration pass |
| Threshold source when the SLO has multiple thresholds | Reuse the existing deterministic pick (`FetchSLOStatus` sorts timeframe keys and takes the first). | No new behavior; the band consumes whatever `Target` the client already resolved, avoiding a second source of truth. | y |
| `Target <= 0` (no thresholds in the response) | Fall back to the existing `state`-based classification, unchanged. | An SLO with no usable target cannot produce a bound; preserving today's behavior is safer than inventing one. Documented compatibility fallback, not a silent path. | y |
| `RequestCount == 0` or below the floor | Carry the previous status forward, unchanged (`minRecentWindowRequests` guard runs first). | Prior AD-019 decision; the band is meaningless without a sample size. | y |
| Independence assumption | Errors are modeled as independent Bernoulli trials within a window. | Simplification: real errors are often bursty (correlated), which inflates variance and can make the band optimistic. Flagged as a known limitation; the hysteresis layer absorbs residual single-window noise, and calibration is deferred. | n - known modeling limitation |
| Hysteresis interaction | A window classified as a breach still passes through `breachHysteresisCycles` before `"outage"`. | AD-019 addendum 3 item 4: threshold rescaling and hysteresis are complementary, not exclusive. | y |
| Below-target-but-within-band windows | Classified as `"degraded"`, never `"outage"`. | Preserves a visible "not fully healthy" signal for windows genuinely below the SLO's target without claiming an outage the data does not support. | y |
| `state == "warning"` | Still maps to `"degraded"`. | Existing `normalizeStatus` behavior and any warning threshold the SLO configures are preserved. | y |

**Open questions:** none - all resolved or logged above.

---

## User Stories

### P1: Breach decision derives from the window's own SLI vs a rescaled bound ⭐ MVP

**User Story**: As a status page visitor, I want a healthy high-traffic service to stay "operational", so that normal error-rate variance inside the SLO's budget does not paint the page red.

**Why P1**: This is the root fix - the entire reason the feature exists.

**Acceptance Criteria**:

1. WHEN the poller evaluates a service's window with `RequestCount >= minRecentWindowRequests` AND `Target > 0` THEN the system SHALL compute `breachBound = 100 - 100 * (p0 + breachThresholdSigmas * sqrt(p0*(1-p0)/RequestCount))` where `p0 = (100 - Target)/100`.
2. WHEN `SLI < breachBound` THEN the system SHALL classify the window as a breach and route it through the existing hysteresis, regardless of Datadog's `overall.state`.
3. WHEN `SLI >= breachBound` AND the window is below target (`SLI < Target` OR `overall.state` is `"warning"` OR `overall.state` is `"breached"`) THEN the system SHALL set the service's status to `"degraded"`, never `"outage"`.
4. WHEN `SLI >= Target` AND `overall.state` is `"ok"` THEN the system SHALL set the service's status to `"operational"`.
5. The system SHALL NOT use Datadog's `overall.state == "breached"` as the sole trigger for `"outage"`.

**Independent Test**: Feed `pollService` a window with `Target = 99.5`, `RequestCount = 10000`, `State = "breached"`, `SLI = 99.4` (below target, inside the 3-sigma band). The resulting status SHALL be `"degraded"`, not `"outage"`, even on the first cycle.

---

### P1: Genuine outages still surface through hysteresis ⭐ MVP

**User Story**: As a status page visitor, I want a real outage to still be shown, so that rescaling the threshold does not blind the page to a service that is actually down.

**Why P1**: Required together with the story above - a threshold change that suppresses real outages is a worse failure than the noise it fixes.

**Acceptance Criteria**:

1. WHEN a window is classified as a breach (`SLI < breachBound`) for `breachHysteresisCycles` consecutive cycles THEN the system SHALL set `current_status` to `"outage"`.
2. WHEN a window is classified as a breach for fewer than `breachHysteresisCycles` consecutive cycles THEN the system SHALL carry the previous `current_status` forward instead of flipping to `"outage"`.
3. WHEN a non-breach window is observed between breaches THEN the system SHALL reset the breach streak to zero, exactly as today.

**Independent Test**: Feed two consecutive windows with `Target = 99.5`, `RequestCount = 10000`, `SLI = 50` (far below any plausible bound). The second cycle SHALL set `"outage"`.

---

### P2: Fallbacks and the failure contract stay unchanged

**User Story**: As an operator, I want an SLO with no usable target, a low-traffic window, or a Datadog failure to behave exactly as it does today, so this change introduces no new failure mode.

**Why P2**: Not new behavior - this story makes the preserved contracts explicit.

**Acceptance Criteria**:

1. IF `Target <= 0` (the history response carried no usable threshold) THEN the system SHALL fall back to the existing `state`-based classification, unchanged.
2. IF `RequestCount < minRecentWindowRequests` THEN the system SHALL carry the previous `current_status` forward and SHALL still write the interval/status, unchanged.
3. IF every fetch retry fails THEN the system SHALL leave `current_status` untouched and SHALL NOT write an interval for that cycle, unchanged.
4. The system SHALL NOT add any environment variable, database migration, or schema change.

**Independent Test**: A window with `Target = 0`, `State = "breached"`, `RequestCount = 5000` SHALL still route through the state-based fallback and, after two cycles, produce `"outage"` - the pre-change behavior.

---

## Edge Cases

- WHEN `RequestCount` is large enough that `se` approaches zero THEN `breachBound` SHALL approach `Target`, so a sustained deviation below the SLO's own target still breaches - the band removes variance, not real degradation.
- WHEN `breachBound` computes below `0` (very low `Target` with a small sample) THEN the system SHALL treat every non-negative `SLI` as within the band (no breach) rather than clamp or panic.
- WHEN `overall.state` is `"no_data"` or any unrecognized value AND `SLI >= Target` THEN the system SHALL map it to `"degraded"` via the existing `normalizeStatus` default (never silently `"operational"`).
- WHEN the SLO has multiple configured thresholds THEN `Target` SHALL remain the client's existing deterministic pick (no new tie-break logic).
- WHEN `Target > 100` or `SLI` outside `[0,100]` (malformed response) THEN the system SHALL NOT panic; the bound arithmetic SHALL remain total over the given floats.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| BTR-01 | P1: Breach decision from rescaled bound | Specify | Implementing |
| BTR-02 | P1: Breach decision from rescaled bound | Specify | Implementing |
| BTR-03 | P1: Breach decision from rescaled bound | Specify | Implementing |
| BTR-04 | P1: Breach decision from rescaled bound | Specify | Implementing |
| BTR-05 | P1: Breach decision from rescaled bound | Specify | Implementing |
| BTR-06 | P1: Genuine outages still surface | Specify | Implementing |
| BTR-07 | P1: Genuine outages still surface | Specify | Implementing |
| BTR-08 | P1: Genuine outages still surface | Specify | Implementing |
| BTR-09 | P2: Fallbacks and failure contract | Specify | Implementing |
| BTR-10 | P2: Fallbacks and failure contract | Specify | Implementing |
| BTR-11 | P2: Fallbacks and failure contract | Specify | Implementing |
| BTR-12 | P2: Fallbacks and failure contract | Specify | Implementing |

**ID format:** `BTR-[NUMBER]` (Breach Threshold Rescaling)

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 12 total, 12 mapped to tasks, 0 unmapped

---

## Success Criteria

- [ ] A window with a high request count and an SLI inside the band (even with Datadog `state: "breached"`) never flips a service to `"outage"`.
- [ ] Two consecutive windows with an SLI far below the bound still flip to `"outage"`.
- [ ] `Target <= 0`, low-volume, and fetch-failure paths are byte-for-byte unchanged in behavior.
- [ ] `.specs/STATE.md` gains an `AD-019 addendum 4` recording this as the root fix superseding the addendum-3 stopgap, with the uncalibrated `breachThresholdSigmas` flagged.
- [ ] Zero regressions in `internal/poller` and `internal/connectors/datadog` test suites.
