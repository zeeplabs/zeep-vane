# Poller Recent SLO Window Specification

## Problem Statement

`Poller.pollService` (`internal/poller/poller.go:151-174`) derives a service's `current_status` from `datadog.Client.FetchSLOStatus` (`internal/connectors/datadog/client.go:138-169`), which reads `status.state` from `GET /api/v1/slo/search`. That field reflects the SLO's **fixed configured timeframe** (30d in every SLO observed in this org). Once an error burst pushes the 30-day error budget negative, `state` stays `breached` for the rest of that 30-day window even after the service has had zero errors for hours or days — confirmed live against `service:conversations-service` (2026-09-08: `error_budget_remaining: -4.932%`, 24h prior to the check had zero errors, `sliValue` for a 1h custom window was `100`). The public status page reads this stale cached value (SP-06/SP-07), so visitors see "interrupção" long after the service recovered.

## Goals

- [ ] `current_status` reflects the service's health over a short recent window (the poll interval) instead of a 30-day rolling compliance window.
- [ ] A recovered service's public status flips back to `operational` within one poll cycle of having enough recent traffic to prove it, not after the old error burst ages out of a 30-day window.
- [ ] Low-traffic services do not flap between statuses because a short window caught only 1-2 requests.

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature                                                                 | Reason                                                                                                                                          |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| Per-service configurable window length or volume threshold (UI/schema)   | User picked the fixed/global option over configurable in triage; adds a migration + admin UI surface not requested.                             |
| Removing/changing `SearchSLOs` or `ValidateCredentials`                  | Both still use `GET /api/v1/slo/search` for name lookup and credential checks (I14, SP-01.2) — untouched, independent of `current_status` logic. |
| Multi-provider SLO support (Grafana, New Relic, ...)                     | Out of scope per prior decision (`.specs/STATE.md`, Datadog-only for now); this change stays inside the existing `datadog.SLOProvider` contract. |
| Exposing `error_budget_remaining` on the public API or admin UI          | Column exists in `status_intervals`/`status_snapshots` but is not read by any handler or frontend code (verified: only `client.go`, `poller.go`, `status_interval_repository.go`, migrations reference it) — stays internal-only, unchanged surface. |
| Changing `public-status-hourly-history` bucketing (last-status-wins)     | That feature already resolved its own design decision independently; this change only affects what status gets written per poll, not how hours are bucketed. |
| Formal 30-day SLO compliance reporting for admins                       | User chose full replacement over dual-tracking; a future admin-facing compliance view is a separate, unrequested feature. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Window length | **Superseded 2026-09-08, see `AD-019 addendum` in `.specs/STATE.md`.** Originally `POLL_INTERVAL_SECONDS` (window `[now - POLL_INTERVAL_SECONDS, now)`); live-tested post-implementation against real Datadog traffic and found too narrow — the freshest ~60s under-reports request volume because Datadog's trace-metric aggregation lags behind real time, reproducing the original stuck-status bug. Replaced with a fixed `recentWindowWidth = 5m` window ending `recentWindowLag = 60s` behind `now`, decoupled from `POLL_INTERVAL_SECONDS` (which now only controls poll cadence, default example 120s to match the public page's refresh). | User's explicit choice: no new config, "now" tracks the actual poll cadence instead of an arbitrary fixed duration. | y (original) → corrected, see addendum |
| Low-volume guard | If total requests in the window (`denominator.sum` from `GET /api/v1/slo/{id}/history`) is below a minimum, do **not** recompute status this cycle — carry forward the service's current `CurrentStatus` unchanged. | User's explicit choice over "always recompute" and over "auto-expand window". Prevents a single request/error in a short window from producing a statistically meaningless status flip. | y |
| Minimum volume threshold value | 10 requests in the window | Below 10 samples, a single error already swings the observed rate by ≥10 percentage points — too noisy to act on. No existing project precedent for this number; picked as a conservative floor, not derived from data. | n — flagged for user review; easy to tune post-launch since it is a single constant, not a stored/migrated value. |
| 30d SLO state retirement | `current_status` stops using `/slo/search`'s `status.state`/`error_budget_remaining` (30d) entirely. No parallel 30d compliance field is kept. | User's explicit choice ("substitui totalmente"). `public-status-hourly-history` already gives trend visibility; a second stale-by-design signal isn't needed for the public page. | y |
| First-ever poll for a brand new service (never polled, `current_status = 'not_configured'`, no prior real status to carry forward) | If the first poll's window also fails the volume guard, `current_status` stays `not_configured` (the DB default) — carrying forward "not configured" is the correct behavior, not a special case. | Falls out naturally from the carry-forward rule; `not_configured` already means "no evidence yet" in the existing schema (`internal/db/migrations/0004_services.up.sql:5`). | y |
| Datadog-computed `state` in the history response | Reuse Datadog's own `overall.state` from `GET /slo/{id}/history` (verified live: without passing an explicit `target` param, Datadog still returns `state` computed against the SLO's own configured target — e.g. a 1h window at 100% SLI against a 99.5% target returned `state: "ok"`) instead of vane re-implementing target/warning threshold comparison itself. | Confirmed via live API call (SDK discovery + `execute_code`, 2026-09-08) — the response embeds the SLO's own thresholds (`additionalProperties.slo.thresholds`) and computed `state`, so vane doesn't need a second call or its own comparison logic. | y |
| `error_budget_remaining` value stored per interval for the recent window | Approximate as `sliValue - target` (percentage points above/below target for the window), since Datadog only returns an exact remaining-budget figure when an explicit `target` is passed (which would need a value vane doesn't have without an extra call). | Field is DB-only, never read by any handler/frontend (see Out of Scope) — an approximation is acceptable for a value nothing currently consumes. If this changes later, revisit before trusting the column for anything user-facing. | y |
| Retry/error handling on the history call | Unchanged from today: `FetchWithRetry` (`internal/poller/retry.go`) retries transient failures (timeout/5xx) up to `maxFetchAttempts`; `ErrUnauthorized` never retried; on exhausted retries `pollService` returns early without touching `current_status` (SP-08 preserved). | This is existing, already-specified behavior (SP-05/SP-08) — the fetch call changes what it asks Datadog, not the retry/failure contract around it. | y |

**Open questions:** none — all resolved or logged above.

---

## User Stories

### P1: Recent-window status replaces 30-day SLO state ⭐ MVP

**User Story**: As a status page visitor, I want the shown service status to reflect its recent health, so that I stop seeing "interrupção" for a service that has already recovered.

**Why P1**: This is the entire point of the change — the bug the user reported.

**Acceptance Criteria**:

1. WHEN the poller runs a poll cycle for a service THEN the system SHALL request the service's SLO status for a short recent window instead of the SLO's configured fixed timeframe (30d). **Corrected 2026-09-08** (`AD-019 addendum`): the window is a fixed 5 minutes wide, ending 60 seconds behind "now" — not `[now - POLL_INTERVAL_SECONDS, now)` as originally specified, which live-tested too narrow against real Datadog traffic.
2. WHEN the requested window's total request volume is at least the minimum volume threshold (10) THEN the system SHALL derive `current_status` from Datadog's `state` for that window, mapped via the existing `normalizeStatus` table (`ok`→`operational`, `warning`→`degraded`, `breached`→`outage`, anything else→`degraded`).
3. WHEN a service's window `state` maps to `operational` after a prior cycle had it as `degraded` or `outage` THEN the system SHALL update `current_status` to `operational` on that same poll cycle (no additional delay or cooldown beyond the poll interval itself).
4. The system SHALL stop reading `status.state` / `error_budget_remaining` from `GET /api/v1/slo/search` for the purpose of computing `current_status`.
5. WHEN a poll cycle's status differs from the previous `current_status` THEN the system SHALL open or extend a status interval and update `services.current_status` exactly as `pollService` does today (unchanged interval/audit behavior — see `service-status-intervals`).

**Independent Test**: Point a test service's SLO at a metric query with zero errors in the last `POLL_INTERVAL_SECONDS` but a breached 30d error budget (reproducible with the live `conversations-service` SLO used in this investigation); after one poll cycle, `current_status` SHALL be `operational`.

---

### P1: Low-volume guard prevents noise-driven status flapping ⭐ MVP

**User Story**: As an operator, I want a low-traffic service's status to stay stable instead of flapping on 1-2 requests, so the public page doesn't cry wolf.

**Why P1**: Required together with the story above — recent-window status without this guard is a known regression (a single failed request in a short window would swing an otherwise-healthy low-traffic service to `outage`).

**Acceptance Criteria**:

1. WHEN the requested window's total request volume (`denominator.sum`) is below the minimum volume threshold (10) THEN the system SHALL leave `current_status` unchanged from its previous value for that poll cycle (carry-forward), instead of recomputing from Datadog's `state`.
2. WHILE carrying forward an unchanged status because of low volume THEN the system SHALL still open or extend the service's status interval with that carried-forward value, so `public-status-hourly-history` sees a continuous sample for the hour (no gap).
3. IF a service has never been polled successfully before (`current_status = 'not_configured'`) AND its first poll's window also fails the volume guard THEN the system SHALL leave `current_status` as `not_configured` (no special-cased default).

**Independent Test**: Poll a service whose window has 3 requests, 1 error (33% failure rate) — `current_status` SHALL remain whatever it was before this poll cycle, not flip to `outage`.

---

### P2: Failure handling stays unchanged

**User Story**: As an operator, I want a Datadog outage or misconfigured SLO to behave exactly as it does today, so this change doesn't introduce a new failure mode.

**Why P2**: Not new behavior — this story exists to make explicit that the retry/failure contract is preserved, not touched.

**Acceptance Criteria**:

1. IF the recent-window history request times out or Datadog returns a 5xx THEN the system SHALL retry it exactly as `FetchWithRetry` does today (up to `maxFetchAttempts`), unchanged.
2. IF the recent-window history request returns 401/403 THEN the system SHALL NOT retry (existing `ErrUnauthorized` short-circuit, unchanged).
3. IF every retry attempt for a service's recent-window request fails THEN the system SHALL leave `current_status` untouched for that cycle (existing SP-08 behavior, unchanged) and SHALL NOT open/extend a status interval for that cycle.

**Independent Test**: Existing `poller_test.go`/`retry_test.go` failure-path tests SHALL still pass unmodified in behavior (only the fetch call's target endpoint/window changes, not what happens when it errors).

---

## Edge Cases

- IF the SLO's underlying metric query has zero data points in the requested window (denominator.sum = 0) THEN system SHALL treat it identically to "below minimum volume threshold" (carry-forward) — 0 is `< 10`.
- IF Datadog's history response omits `state` (unexpected/malformed response) THEN system SHALL treat it as an unrecognized state, mapped to `degraded` per the existing `normalizeStatus` default (never silently `operational` on indeterminate data — same invariant the current code already documents).
- WHEN the SLO configured in Datadog is deleted or the `sloID` stops resolving THEN system SHALL return the existing `ErrNotFound` behavior (unchanged), triggering the existing failure-path handling (P2 above).
- WHEN `POLL_INTERVAL_SECONDS` is reconfigured (operator changes it and restarts) THEN it SHALL change poll cadence only — the fetch window's width (5m) and lag (60s) are fixed package constants, independent of the interval (corrected 2026-09-08, `AD-019 addendum`).

---

## Requirement Traceability

| Requirement ID | Story                                  | Phase  | Status  |
| --------------- | --------------------------------------- | ------ | ------- |
| RSW-01          | P1: Recent-window status                | Design | Pending |
| RSW-02          | P1: Recent-window status                | Design | Pending |
| RSW-03          | P1: Recent-window status                | Design | Pending |
| RSW-04          | P1: Recent-window status                | Design | Pending |
| RSW-05          | P1: Recent-window status                | Design | Pending |
| RSW-06          | P1: Low-volume guard                    | Design | Pending |
| RSW-07          | P1: Low-volume guard                    | Design | Pending |
| RSW-08          | P1: Low-volume guard                    | Design | Pending |
| RSW-09          | P2: Failure handling unchanged          | Design | Pending |
| RSW-10          | P2: Failure handling unchanged          | Design | Pending |
| RSW-11          | P2: Failure handling unchanged          | Design | Pending |

**ID format:** `RSW-[NUMBER]` (Recent SLO Window)

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 11 total, 0 mapped to tasks, 11 unmapped ⚠️ (expected — Design/Tasks phases not run yet)

---

## Success Criteria

- [ ] A service that has had zero errors for at least `POLL_INTERVAL_SECONDS` × (enough cycles to exceed the volume threshold) shows `operational` on the public status page, regardless of an older error burst still inside a 30-day window.
- [ ] A low-traffic service (fewer than 10 requests per poll interval) never flips status from a single request's outcome.
- [ ] Zero regressions in existing retry/failure-path tests (`internal/poller/poller_test.go`, `retry_test.go`, `poller_abort_test.go`, `poller_status_test.go`).
- [ ] `.specs/STATE.md` gains an `AD-NNN` entry recording this as a superseding decision over the original "mirror Datadog's SLO state 1:1" design (SP-06/SP-07).
