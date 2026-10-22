# Poller Burn-Rate Classification Specification

## Problem Statement

`Poller.pollService` (`internal/poller/poller.go:368`) classifies each service from a single 5-minute window of `GET /api/v1/slo/{id}/history`, comparing that window's SLI against a target calibrated for the SLO's 30-day timeframe. Four layered heuristics (AD-019 and addenda 1-5: low-volume carry-forward, binomial `breachBound` with an uncalibrated `breachThresholdSigmas = 3`, `breachHysteresisCycles = 2`, Datadog-state fallback for low-volume first classification) patch the symptoms of that mismatch, but the result still disagrees with Datadog in the cases operators actually hit. Confirmed by code reading and a live read of the Starbem staging SLOs (2026-10-22):

1. **Zero-traffic windows pin a recovered service to `degraded`.** With `RequestCount == 0` after an incident, `pollService` classifies by Datadog's window `state`, which is `no_data` when the denominator is 0; `normalizeStatus("no_data")` returns `"degraded"` (`poller.go:545`). A low-traffic service (every staging service: 0 to ~6.5k requests per 30 days) stays orange indefinitely while Datadog shows the SLO as `ok`.
2. **Low-volume windows freeze an outage.** With 1-9 requests, the previous status is carried forward (`minRecentWindowRequests = 10`), so a service that recovered stays in `outage` until a window with 10+ requests arrives.
3. **`degraded` uses the raw 30-day target.** The `breachBound` band only guards `outage`; `degraded` fires on `SLI < Target`, so at 99.5% one failed request in ~150 marks a healthy service degraded.
4. **No burn rate anywhere.** Datadog's SLO object does not expose burn rate (only `sli`, `error_budget_remaining`, `raw_error_budget_remaining`, `state`); burn rate exists only inside `slo alert` monitors. Starbem's production monitors use multi-window burn rate (`burn_rate(...).long_window("1h").short_window("5m")`, critical 14.4x / warning 7.2x), so Vane and the alerts in Slack disagree by construction.

AD-019 addendum 3 already named the definitive fix (stop comparing a short window against a 30-day target; multi-window burn rate is listed there as the more robust option). This feature implements it.

## Goals

- [ ] A service's public status is derived from multi-window burn rate (short 5m + long 1h), the same signal Starbem's production `slo alert` monitors use, so Vane and Datadog alerting agree on when a service is degraded/down.
- [ ] A service that has recovered returns to `operational` within one short window plus poll latency (target: ≤ ~8 minutes at `POLL_INTERVAL_SECONDS=120`), including when traffic is low or absent.
- [ ] A single isolated failed request never opens an incident or degrades a service, regardless of traffic volume.
- [ ] Admins see the numbers behind a status (burn rate per window, errors, requests) and the LLM-generated incident text can cite them.
- [ ] Thresholds are validated against real production traffic (replay) before shipping, closing the "uncalibrated constant" debt carried since AD-019.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Reading the state of Datadog `slo alert` monitors as the status source | Decided 2026-10-22: Vane computes burn rate itself so self-hosted tenants work without depending on monitors created outside Vane. Monitors stay the Slack-alerting path. |
| Per-tenant / per-service configurable thresholds (UI, API, migration) | User chose fixed defaults matching Starbem's production monitors. Constants stay single-line tunable, as every prior AD-019 constant. |
| Showing burn rate / error numbers on the public status page | User chose admin + LLM prompt only; visitors keep seeing status and incident text. |
| Automatic incident closing without owner/operator confirmation | AI-20 product rule (recovery proposes a closing comment, a human confirms). Unchanged. |
| Monitor-based SLOs (`slo_type == "monitor"`, NB-7) | No `series` in `/history` for them; 0% of real usage in the reference org. Keep today's state-based behavior. |
| Detecting a service that is fully down and emits no traces at all | An SLO built on tracer metrics cannot see requests that never reach the tracer. The grace period (BURN-06) limits the blind spot; real detection belongs to synthetic checks / manual polling (`manual-polling-monitoring`). |
| Changing Datadog SLO / monitor definitions | Done separately in the Starbem `infra` repo (staging targets aligned to prod, 2026-10-22). |
| Changing the public hourly history bucketing (`internal/history`) | Consumes status intervals unchanged; fewer false transitions fix it at the source. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Burn rate definition | `burn = (errors / requests) / (1 - Target/100)` per window, `errors = denominator - numerator` from `/history` `series` sums | Same definition Datadog uses for `burn_rate()`; only needs data the connector already reads plus the numerator sum. | y |
| Windows | Short = 5m, long = 1h, both ending `recentWindowLag` (60s) behind now | Mirrors the production monitors (`short_window("5m")`, `long_window("1h")`); the 60s lag is the existing aggregation-delay fix (AD-019 addendum 1). | y |
| Outage threshold | short ≥ 14.4 AND long ≥ 14.4 | Matches production `critical = "14.4"`. | y (user, 2026-10-22) |
| Degraded threshold | short ≥ 7.2 AND long ≥ 7.2 (and not outage) | Matches production `warning = "7.2"`. | y (user, 2026-10-22) |
| Minimum errors | Short window must hold ≥ 3 failed requests for degraded/outage | 1 isolated error never opens an incident; exact value confirmed by the replay (BURN-12). | y (user chose the mechanism; value 3 confirmed by replay 2026-10-22) |
| Zero-traffic grace period | 15 minutes without new errors before an `outage`/`degraded` service returns to `operational` when the short window has 0 requests | Avoids both "stuck forever" (today) and "green the instant traffic vanishes" (a crashed pod emits no traces). | y (user chose the mechanism; value 3 confirmed by replay 2026-10-22) |
| Hysteresis (`breachHysteresisCycles`) | Removed | Replay (design.md): 0 false episodes without it, +2 min outage latency with it, in-memory streak lost on restart. | y (replay 2026-10-22) |
| `minRecentWindowRequests` floor and `breachBound` | Replaced by BURN-03/BURN-05/BURN-06 | They exist only to compensate for the window/target mismatch this feature removes. | y (replay 2026-10-22) |
| Datadog API cost | One 1h `/history` call per service per cycle (same count as today); short window derived from its 60s series; fallback second call if the series is coarser | Live-checked: 1h window returns 60s buckets with per-bucket numerator/denominator. Endpoint rate limit undocumented - logged, not enforced. | y (live check 2026-10-22) |
| `Target <= 0` | Keep the existing state-based fallback (`classifyByState`) | No target, no budget, no burn rate. | y |

---

## User Stories

### P1: Status follows multi-window burn rate ⭐ MVP

**User Story**: As a status page visitor, I want a service shown as degraded/down only when it is really burning its error budget, and shown as operational again soon after it recovers, so that the page matches reality.

**Why P1**: This is the bug being reported: Datadog says `ok` and Vane still shows the incident.

**Acceptance Criteria**:

1. WHEN a poll cycle runs for a service with a metric-based SLO and `Target > 0` THEN the poller SHALL fetch the short (5m) and long (1h) windows, both ending `recentWindowLag` before now, and compute requests, errors and burn rate for each.
2. WHEN short burn ≥ 14.4 AND long burn ≥ 14.4 AND short-window errors ≥ the minimum-errors constant THEN the service SHALL be classified `outage`.
3. WHEN the outage condition does not hold AND short burn ≥ 7.2 AND long burn ≥ 7.2 AND short-window errors ≥ the minimum-errors constant THEN the service SHALL be classified `degraded`.
4. WHEN the short window has requests AND neither condition above holds THEN the service SHALL be classified `operational`, regardless of the long window (recovery is driven by the short window, as in Datadog's multi-window alerts).
5. WHEN the short window has fewer failed requests than the minimum-errors constant THEN the service SHALL NOT be classified `degraded` or `outage` by that cycle, whatever the burn rate.
6. WHEN the short window has 0 requests AND the current status is `outage` or `degraded` THEN the service SHALL keep its current status until 15 minutes have passed since the last cycle whose short window contained a failed request, and SHALL then become `operational`.
7. WHEN the short window has 0 requests AND the current status is `operational` THEN the service SHALL stay `operational`.
8. WHEN Datadog reports `no_data` for a window THEN the poller SHALL treat it as "no evidence" per criteria 6-7, and SHALL NOT map it to `degraded`.
9. WHEN `Target <= 0` THEN the poller SHALL use the existing state-based fallback unchanged.

**Independent Test**: Unit tests on the classifier with fixture windows (healthy, burst, sustained outage, recovery, low volume, zero traffic within/after grace). Live: stop errors on a staging service after an incident and see it return to `operational` within ~8 minutes on the admin and public page.

---

### P1: First classification and edge transitions stay honest ⭐ MVP

**User Story**: As an admin linking a new service, I want its first status to reflect real data, and never a placeholder or a false alarm.

**Why P1**: The rewrite replaces the paths that SLOTRAF-01/ZEROREQ (AD-019 addenda 5 and `slo-low-traffic-not-configured`) fixed; those guarantees must survive.

**Acceptance Criteria**:

1. WHEN a service is still `not_configured` AND the long window has requests THEN it SHALL be classified by criteria 2-5 of the previous story, using the long window when the short window is empty.
2. WHEN a service is still `not_configured` AND both windows have 0 requests THEN it SHALL stay `not_configured`.
3. WHEN the Datadog fetch fails after retries THEN the poller SHALL keep today's contract (error logged, no status write for that service in that cycle).
4. WHEN a transition to `outage`/`degraded`/`operational` happens THEN the existing analyzer hooks (incident open, degraded tooltip, closing proposal) SHALL fire exactly as today.

**Independent Test**: Unit tests for each branch; integration test that a recovered service with an open incident produces a pending closing comment.

---

### P2: Admins and the LLM see the numbers

**User Story**: As an owner/operator, I want to see the burn rate, errors and requests behind a service's status, and have the generated incident text cite them, so I can judge severity without opening Datadog.

**Why P2**: Valuable for triage, not required to fix the wrong-status bug.

**Acceptance Criteria**:

1. WHEN a poll cycle classifies a service THEN the poller SHALL persist that cycle's short/long burn rate, short-window errors and requests as the service's latest reading.
2. WHEN an owner/operator/viewer opens the service detail in the admin THEN the UI SHALL show the latest reading (burn rate per window, errors/requests in the short window, reading time), with all strings through `react-i18next` (pt-BR and English).
3. WHEN an incident description, degraded tooltip or closing comment is generated THEN `AnalysisInput` SHALL carry the burn rates, errors and requests, and the prompt SHALL be allowed to cite them.
4. WHEN a service has no reading yet THEN the admin SHALL show an empty state instead of zeros.
5. WHEN the public status page is served THEN its response SHALL NOT include any of these numbers.

**Independent Test**: Open a service detail after a poll cycle and see the numbers; inspect a generated incident description containing the error rate; confirm the public endpoint payload is unchanged.

---

### P1: Thresholds validated on real traffic before shipping ⭐ MVP

**User Story**: As the maintainer, I want the new classifier replayed against real production history before it replaces the current one, so we don't ship another uncalibrated constant.

**Why P1**: AD-019 addendum 3 item 3 requires a real-traffic measurement pass across more than one SLO before shipping a new classification rule.

**Acceptance Criteria**:

1. WHEN the design is finalized THEN a replay over at least 5 production SLOs (mixing high- and low-traffic services and flows) and at least 7 days of `/history` data SHALL compare transitions of the current classifier against the new one.
2. WHEN the replay shows the new classifier missing a real incident that the current one caught, or adding false transitions THEN the constants (minimum errors, grace period, hysteresis) SHALL be adjusted and the replay re-run before implementation.
3. WHEN the replay is done THEN its method, SLOs used and results SHALL be recorded in `design.md` and in the new `AD-NNN` entry.

**Independent Test**: The replay report exists, lists the SLOs/period, and shows transition counts for both classifiers.

---

## Edge Cases

- WHEN the long window is empty but the short one is not (service just started receiving traffic) THEN the long burn SHALL be treated as equal to the short burn for that cycle (no division by zero, no automatic pass).
- WHEN errors exceed requests in a window (metric aggregation skew) THEN errors SHALL be clamped to requests (burn capped at `1 / (1 - Target/100)`).
- WHEN `Target` is 100 THEN the budget is 0; any failed request at or above the minimum-errors constant SHALL count as a breach, without dividing by zero.
- WHEN the poller restarts or HA leadership moves THEN the grace-period timer SHALL survive (derived from persisted data, not in-memory state such as today's `breachStreak`).
- WHEN a service is linked to a monitor-based SLO (no `series`) THEN it SHALL keep today's state-based behavior (NB-7 out of scope).

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| BURN-01 | P1 burn rate: fetch short/long windows, compute requests/errors/burn | T2, T4 | Implemented |
| BURN-02 | P1 burn rate: outage rule (14.4x both windows + min errors) | T3, T4 | Implemented |
| BURN-03 | P1 burn rate: degraded rule (7.2x both windows + min errors) | T3, T4 | Implemented |
| BURN-04 | P1 burn rate: operational / short-window-driven recovery | T3, T4 | Implemented |
| BURN-05 | P1 burn rate: minimum errors guard | T3, T4 | Implemented |
| BURN-06 | P1 burn rate: zero-traffic grace period (15 min) | T1, T3, T4 | Implemented |
| BURN-07 | P1 burn rate: zero traffic while operational stays operational | T3, T4 | Implemented |
| BURN-08 | P1 burn rate: `no_data` is no evidence, never `degraded` | T3, T4 | Implemented |
| BURN-09 | P1 burn rate: `Target <= 0` fallback unchanged | T4 (hysteresis removed, see AD-049) | Implemented |
| BURN-10 | P1 first classification: `not_configured` rules | T3, T4 | Implemented |
| BURN-11 | P1 first classification: fetch failure and analyzer hooks unchanged | T4, T5 | Implemented |
| BURN-12 | P1 replay calibration before implementation | Design | Verified (replay 2026-10-22) |
| BURN-13 | P2 persist latest reading | T1, T4, T6 | Implemented |
| BURN-14 | P2 admin service detail shows reading (i18n) | T6, T7 | Implemented |
| BURN-15 | P2 LLM `AnalysisInput` carries the numbers | T5 | Implemented |
| BURN-16 | P2 public payload unchanged | T6 | Implemented |
| BURN-17 | Edge: empty long window, clamp, target 100, restart-safe grace, monitor SLOs | T1-T4, T6 | Implemented |

**Coverage:** 17 total, 0 mapped to tasks, 17 unmapped (pre-Design).

---

## Success Criteria

- [ ] In the replay (BURN-12), the new classifier produces fewer false transitions than the current one on every sampled SLO and misses no incident the current one caught.
- [ ] After deploying to staging: a service with no errors for 15+ minutes is never shown as `degraded`/`outage` (zero stuck services, checked against the poller's debug log).
- [ ] A recovered service returns to `operational` within ~8 minutes on the public page.
- [ ] Gates green: `go build/test/vet`, `gofmt`, `make test-integration` (disposable DB), `npx tsc -b --noEmit`, `npm run test`.
