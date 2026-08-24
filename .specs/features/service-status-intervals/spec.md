# Service Status Intervals Specification

## Problem Statement

`status_snapshots` grows one row per service per poll cycle (~1,440 rows/day/service) with no pruning routine anywhere in the codebase - disk grows without bound for the life of any self-hosted instance. Separately, the public status page's hourly bars (`internal/history.BuildHourly`) resolve each hour by "last snapshot seen wins," which can mask a real incident (e.g. 55 minutes down, 5 minutes up paints the hour green). Neither problem can be fixed by tuning the snapshot model - both trace back to storing one row per *check* instead of one row per *status interval*. This feature replaces the snapshot table with an interval model (one row per status change, open-ended until the next change), which fixes the growth problem structurally, corrects the hourly-bucket semantics to worst-status-wins, and adds a real uptime % the product has never had.

## Goals

- [x] `status_snapshots` no longer grows one row per poll cycle; writes only happen on status change or as an in-place update to the open interval
- [x] Public hourly bars reflect the worst status observed within each hour, not just the last one
- [x] Public status page displays an uptime % for the same 24h window already shown by the hourly bars
- [x] Closed intervals older than 35 days are pruned automatically, bounding disk growth long-term
- [x] At most one open interval exists per service at any time, enforced by the database - not just by application logic

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
| --- | --- |
| Configurable retention window (env var) | User chose a fixed 35-day default for this MVP; making it configurable is a P3 follow-up, not blocking |
| Uptime % over 30/90-day windows | The product only shows a fixed 24h window today; extending the window is a separate feature once the interval model is proven |
| Multi-provider status sources (Grafana, New Relic, etc.) | MVP stays Datadog-only (confirmed by user); the `integrations` schema question is deferred to the monitor-type feature (SHU is provider-agnostic at the interval-storage layer, so it does not block that later work) |
| Backfilling historical bars from the old `status_snapshots` data | The migration starts the interval table empty; the 24h of bars visible before deploy is a one-time, accepted loss (see Assumptions) |
| Exposing `error_budget_remaining` / burn rate on any dashboard | Tracked as a separate future feature; this spec only preserves the field so it isn't lost, it does not add any new consumer of it |
| Manual/heartbeat monitor types | Separate feature (top-5 item 4); this spec's interval writer is called by whatever writes a status today (the Datadog poller), regardless of how future monitor types produce a status |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Merge scope: fold the snapshot-retention fix into the interval-model migration instead of shipping retention as an isolated short-term fix | Single merged feature | User explicitly chose this over the isolated fix, to avoid migrating the schema twice | y |
| Retention window for closed intervals | 35 days, hardcoded | User chose the 35-day option over 7 days and over an env-var-configurable default | y |
| Hourly bucket resolution semantics | Worst-status-wins (`outage` > `degraded` > `operational`), replacing today's last-status-wins | User confirmed; matches the existing frontend severity order already used for the page-level banner (`web/src/features/public-status/PublicStatusPage.tsx:84-86`) | y |
| Fate of `status_snapshots` | Dropped entirely by migration; replaced by the new interval table as the only source for history and uptime % | User chose full replacement over dual-write | y |
| Uptime % downtime definition | Only `outage` counts as downtime in the denominator; `degraded` counts as "up" | User confirmed | y |
| Historical backfill on migration day | None - the interval table starts empty; every service reads as `no_data` for its history until fresh polls populate it | Self-hosted OSS product, no production users yet identified for this repo; a one-time 24h gap of historical bars on upgrade day is judged acceptable versus writing throwaway one-time backfill code. Flagged here for the user to override if wrong. | n - flagged, not explicitly asked |
| Pruning job placement | New, independent ticker in the `serve` process (not reusing the per-service poller ticker) | User chose the dedicated-ticker option to keep polling and pruning responsibilities separate | y |
| Pruning cadence | Every 1 hour | Matches the granularity of the data (hourly bars); frequent enough that pruning lag never becomes visible, cheap enough to not need justification for a tighter interval | n - inferred default, no disagreement expected |
| Concurrent writers to the same service's open interval | Possible only transiently during a `PollerManager.Restart` (old and new poller goroutine briefly overlapping); the unique partial index is the safety net, not the primary correctness mechanism (the poller itself polls all services sequentially in one goroutine per `pollOnce` cycle) | Confirmed by reading `internal/poller/poller.go`: `Run` has a single ticker, `pollOnce` iterates services sequentially in the same goroutine | y (inferred from code, not asked) |
| Uptime % undefined state | A service with zero recorded intervals ever (still `not_configured`, or created less than a poll cycle ago) shows no uptime % (a dash / N/A), not `0%` or `100%` | Consistent with existing `not_configured` handling elsewhere in the codebase (`internal/api/public_status_handler.go:194`) | n - inferred, no disagreement expected |

**Open questions:** none - all resolved or logged above.

---

## User Stories

### P1: Interval-based status storage replaces per-poll snapshot growth ⭐ MVP

**User Story**: As the operator of a Vane instance, I want the poller to stop writing a new row on every single poll when nothing changed, so that disk usage stops growing unboundedly with every deployment's lifetime.

**Why P1**: This is the structural fix for a live, unbounded resource leak. Without it, the feature only fixes cosmetics (bucket semantics, uptime %) while the underlying growth bug remains.

**Acceptance Criteria**:

1. WHEN the poller observes a service's first-ever status THEN the system SHALL insert one open interval row (`starts_at = fetched_at`, `ends_at = NULL`, `status`, `error_budget_remaining`).
2. WHEN the poller observes a status equal to the service's currently open interval's status THEN the system SHALL update that open interval's `error_budget_remaining` in place and SHALL NOT insert a new row.
3. WHEN the poller observes a status different from the service's currently open interval's status THEN the system SHALL close the open interval (`ends_at = fetched_at`) and insert a new open interval starting at the same `fetched_at`.
4. The system SHALL enforce, via a database constraint, that a given service has at most one interval row with `ends_at IS NULL` at any time.
5. IF two writers race to open an interval for the same service THEN the system SHALL let exactly one insert succeed and SHALL surface the constraint violation as an error to the loser, never silently duplicating an open interval.

**Independent Test**: Poll a service twice with the same status - assert only one row exists in the interval table for that service, with an updated `error_budget_remaining`. Poll again with a different status - assert the first row now has `ends_at` set and a second open row exists.

---

### P1: Public hourly bars resolve by worst status observed in the hour ⭐ MVP

**User Story**: As a visitor of a public status page, I want an hour that contained a real outage to show as down, even if the service recovered before the hour ended, so that the status page doesn't hide incidents.

**Why P1**: This is the whole reason the interval model matters for the product surface the user actually looks at; without this story the migration would be invisible to end users.

**Acceptance Criteria**:

1. WHEN a bucket's hour overlaps one or more intervals with different statuses THEN the system SHALL resolve that bucket to the highest-priority status among them, using the order `outage` > `degraded` > `operational`.
2. WHEN a bucket's hour overlaps no recorded interval for a service THEN the system SHALL resolve that bucket to `no_data`.
3. The system SHALL continue to render exactly 24 hourly buckets in the `America/Sao_Paulo` timezone, unchanged from the existing `public-status-hourly-history` contract.
4. WHEN an interval spans a bucket boundary (starts in one hour, still open or ends in a later hour) THEN the system SHALL count that interval's status toward every bucket it overlaps, not only the bucket containing its `starts_at`.

**Independent Test**: Seed one service with an interval `operational` from hour-start to hour-start+55min, then `outage` from +55min to end of hour. Query the public status endpoint and assert that hour's bucket resolves to `outage`, not `operational`.

---

### P1: Public status page shows 24h uptime % ⭐ MVP

**User Story**: As a visitor of a public status page, I want to see an uptime percentage for the period already shown by the hourly bars, so that I don't have to eyeball 24 colored blocks to know how reliable the service has been.

**Why P1**: The single most-requested missing capability identified against comparable products; low implementation cost once the interval model exists.

**Acceptance Criteria**:

1. WHEN computing uptime % for a service over the 24h window THEN the system SHALL sum the duration of all intervals with status `outage` within that window as downtime, and SHALL treat every other status (`operational`, `degraded`) as uptime.
2. WHEN the window's start point is later than the service's earliest recorded interval `starts_at` THEN the system SHALL use the full 24h window as the denominator.
3. WHEN the service's earliest recorded interval `starts_at` falls inside the 24h window (service is newer than the window) THEN the system SHALL use `now - earliest starts_at` as the denominator instead of the full 24h, clipped so it never exceeds the window.
4. The system SHALL clamp the computed percentage to the range `[0, 100]` before rounding.
5. The system SHALL round the clamped percentage down (floor) to one decimal place, never up.
6. IF a service has zero recorded intervals within the window THEN the system SHALL report uptime % as undefined (rendered as a dash, not `0%` or `100%`) rather than computing a value.

**Independent Test**: Seed a service with a single `outage` interval of exactly 6 hours within an otherwise `operational` 24h window; assert uptime % equals `75.0`. Seed a service created 2 hours ago as always-`operational`; assert the denominator used is 2 hours, not 24, and uptime % equals `100.0`.

---

### P2: Closed intervals older than 35 days are pruned automatically

**User Story**: As the operator of a Vane instance, I want old, closed status history to be deleted automatically, so that disk usage stays bounded even after months or years of uptime.

**Why P2**: Real risk, but lower urgency than P1 - the interval model alone already reduces growth by orders of magnitude versus the snapshot model; pruning is the long-term backstop, not the immediate fix.

**Acceptance Criteria**:

1. The system SHALL run a pruning routine on its own ticker, independent of the per-service poller ticker, in the `serve` process.
2. WHEN the pruning routine runs THEN the system SHALL delete every interval row where `ends_at IS NOT NULL AND ends_at < now() - 35 days`.
3. The system SHALL run the pruning routine once per hour.
4. The system SHALL NOT delete any interval row where `ends_at IS NULL` (the currently open interval), regardless of how old `starts_at` is.
5. IF the pruning routine's delete query fails THEN the system SHALL log the error and SHALL retry on its next scheduled tick, without crashing the `serve` process.

**Independent Test**: Insert a closed interval with `ends_at` 40 days in the past and one with `ends_at` 10 days in the past; run the pruning routine once; assert only the 40-day-old row is gone.

---

## Edge Cases

- IF the poller's very first poll for a brand-new service fails (Datadog fetch error) THEN no interval row is created at all - the service stays `not_configured`/no interval history, consistent with existing failure handling in `pollService` (no snapshot is written today on fetch failure either).
- IF `PollerManager.Restart` briefly overlaps an old and new poller goroutine polling the same service THEN the unique-open-interval constraint (P1, AC4-5) prevents a duplicate open interval; the losing writer's error is logged, not surfaced to any user-facing response.
- WHEN a service transitions status and back within the same poll cycle (impossible today since polling is not sub-cycle-granular, but guards the invariant) THEN each transition still closes/opens its own interval row - no coalescing of same-status intervals separated by a different status in between.
- WHEN the 24h window's clipped denominator (AC3 of the uptime story) would be zero (service created less than a few seconds ago) THEN the system SHALL report uptime % as undefined, same as the zero-intervals case.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| SHU-01 | P1: Interval-based storage | Implementing | Verified |
| SHU-02 | P1: Interval-based storage | Implementing | Verified |
| SHU-03 | P1: Interval-based storage | Implementing | Verified |
| SHU-04 | P1: Interval-based storage | Implementing | Verified |
| SHU-05 | P1: Interval-based storage | Implementing | Verified |
| SHU-06 | P1: Worst-status hourly bars | Implementing | Verified |
| SHU-07 | P1: Worst-status hourly bars | Implementing | Verified |
| SHU-08 | P1: Worst-status hourly bars | Implementing | Verified |
| SHU-09 | P1: Worst-status hourly bars | Implementing | Verified |
| SHU-10 | P1: 24h uptime % | Implementing | Verified |
| SHU-11 | P1: 24h uptime % | Implementing | Verified |
| SHU-12 | P1: 24h uptime % | Implementing | Verified |
| SHU-13 | P1: 24h uptime % | Implementing | Verified |
| SHU-14 | P1: 24h uptime % | Implementing | Verified |
| SHU-15 | P1: 24h uptime % | Implementing | Verified |
| SHU-16 | P2: Automatic pruning | Implementing | Verified |
| SHU-17 | P2: Automatic pruning | Implementing | Verified |
| SHU-18 | P2: Automatic pruning | Implementing | Verified |
| SHU-19 | P2: Automatic pruning | Implementing | Verified |
| SHU-20 | P2: Automatic pruning | Implementing | Verified |

**ID format:** `SHU-[NUMBER]` (Service status History & Uptime)

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 20 total, 20 mapped to tasks, 0 unmapped

---

## Success Criteria

- [x] A service polled every 60s with a constant status produces exactly one row in the interval table, not 1,440/day
- [x] An hour containing a real outage renders as `outage` on the public status page even if the service recovered before the hour ended
- [x] The public status page displays a numeric uptime % (or a dash for undefined) next to the existing hourly bars
- [x] No interval row with `ends_at` older than 35 days survives more than 1 hour past that threshold
- [x] `status_snapshots` table and its repository are removed from the codebase with no remaining references
