# Public Status Hourly History Specification

## Problem Statement

The public status page's per-service uptime bar row is currently decorative: `web/src/features/public-status/history.ts` seeds 45 fake daily bars from a hardcoded map keyed by service name (`"Checkout"`, `"API pública"`), always showing "operational" for any other name. No backend endpoint or query ever aggregates real `status_snapshots` data. A visitor sees a bar row that looks real but reflects nothing the poller actually observed - the whole point of a status page.

## Goals

- [x] Each service's bar row reflects real, poller-observed status data - not seeded/fake data.
- [x] Bars are per-hour (not per-day), covering the last 24 hours, so a visitor can see recent incidents at the granularity they actually happened.
- [x] Hovering a bar shows the exact local date/hour range and status.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Configurable window length (other than the fixed 24h decided here) | Not requested; a fixed window keeps the UI and query simple. |
| Multi-timezone support (per-visitor timezone detection) | Vane is Brazil-only by design (AD scope); America/Sao_Paulo is the one timezone that matters. |
| Changing the incident-history section's own 90-day text retention window | Unrelated - that's a separate, already-shipped feature; this spec only touches the per-service bar row. |
| Changing poll interval or snapshot retention/cleanup policy | Out of scope - this feature only reads existing `status_snapshots` rows, it doesn't change how or how often they're written or purged. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Per-hour status resolution rule | Last snapshot's status wins for that hour (not worst-of-hour, not an average) | User-confirmed explicitly | y |
| Timezone for hour boundaries | `America/Sao_Paulo` | User-confirmed explicitly (product is Brazil-only) | y |
| Window length | 24 hours | User-confirmed explicitly | y |
| Hover/focus detail | Required (not a nice-to-have) - shows local date, hour range, and status label in Portuguese | User explicitly asked for this as part of the core request | y |
| Whether the current (partial, still-accumulating) hour gets its own bar | Yes - the right-most bar is always the current local hour, showing whatever status has been observed so far this hour (or `no_data` if the poller hasn't ticked yet this hour) | Matches the page's existing "atualiza automaticamente a cada 2 minutos" live feel; excluding the current hour would make the right-most bar always one hour stale for no benefit | n (reasonable default, not explicitly asked - logged here per the closure gate) |
| A service with zero snapshots ever (poller hasn't reached it) | Renders all 24 bars as `no_data` (gray) rather than omitting the bar row | Consistent with the existing "never show a fabricated status" principle (SP-08/SP-09) already applied to `LastUpdatedAt` | y (follows an existing, already-confirmed project principle) |

**Open questions:** none — all resolved or logged above.

---

## User Stories

### P1: Real per-hour uptime bars with hover detail ⭐ MVP

**User Story**: As a status page visitor, I want to see each service's actual hour-by-hour health for the last day, so that I can tell when and how badly something broke instead of looking at a decorative row that always says everything's fine.

**Why P1**: This is the entire ask - the current bars are fake and actively misleading (always green regardless of real status). Nothing about this feature is demoable in pieces smaller than "real data, real colors, real hover detail" without still shipping something misleading.

**Acceptance Criteria**:

1. WHEN a visitor loads the public status page THEN the system SHALL show, for each listed service, exactly 24 hourly status bars covering the last 24 hours ending at the current local hour, oldest on the left and the current (possibly partial) hour on the right.
2. The system SHALL color each bar green for `operational`, yellow for `degraded`, red for `outage`, and light gray for `no_data` (no snapshot recorded for that service in that hour).
3. WHILE an hour contains more than one `status_snapshot` for a service, the system SHALL use the status of the snapshot with the latest `fetched_at` within that hour as that hour's status (last-status-wins), never the worst status or a computed average.
4. The system SHALL compute hour boundaries in the `America/Sao_Paulo` timezone, so each bar corresponds to a real Brasília-local clock hour.
5. WHEN a visitor hovers or keyboard-focuses a bar THEN the system SHALL show a tooltip with that bar's local date, hour range (e.g. "24/08, 14h–15h"), and a Portuguese status label (Operacional/Degradado/Interrupção/Sem dados).
6. IF a service has never received any `status_snapshot` THEN the system SHALL render all 24 bars as `no_data` rather than omitting the bar row.
7. The system SHALL NOT call Datadog directly to build this history - it SHALL only read `status_snapshots` already persisted by the poller, consistent with the existing guarantee that a Datadog outage never takes the public page down.
8. WHILE viewing the authenticated preview endpoint (`GET /api/status-pages/{id}/public-preview`), the system SHALL show the same per-hour bars as the real public page (same underlying data/composition), so an admin previews exactly what a visitor will see.

**Independent Test**: With two real services in the dev database (one that has been `operational` since the poller connected, one manually forced through `degraded`/`outage` via `MarkDatadogInvalid`-style test data at specific past hours), load the public status page and confirm: 24 bars per service, correct color per hour, gray for hours before the service existed/before the poller first ran, and a tooltip on hover/focus showing the right date+hour+status.

---

## Edge Cases

- IF the poller has been down (Datadog integration invalid) for several hours THEN the system SHALL show `no_data` for those hours, not the last known status carried forward - an hour with zero snapshots is genuinely unknown, not assumed unchanged.
- WHEN the current hour has just started and the poller hasn't ticked yet THEN the system SHALL show the right-most bar as `no_data` until the first snapshot of that hour lands.
- IF two services have different histories (one older, one newly created) THEN the system SHALL compute each service's 24 bars independently from its own snapshots - a newly created service's earlier hours (before it existed) are `no_data`, unaffected by another service's data.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| UPT-01 | P1: Real per-hour uptime bars | T2, T4, T6 | Implemented |
| UPT-02 | P1: Real per-hour uptime bars | T2, T6 | Implemented |
| UPT-03 | P1: Real per-hour uptime bars | T2 | Implemented |
| UPT-04 | P1: Real per-hour uptime bars | T2, T3 | Implemented |
| UPT-05 | P1: Real per-hour uptime bars | T6 | Implemented |
| UPT-06 | P1: Real per-hour uptime bars | T2, T4, T6 | Implemented |
| UPT-07 | P1: Real per-hour uptime bars | T1, T4 | Implemented |
| UPT-08 | P1: Real per-hour uptime bars | T4 | Implemented |

**Coverage:** 8 total, 8 mapped to tasks, 0 unmapped

---

## Success Criteria

- [x] Each service's bar row on the public status page reflects real `status_snapshots` data, not the hardcoded seed in `history.ts` (`history.ts` deleted in T5; `internal/history.BuildHourly` + `ListRecentByServices` wired in T1/T2/T4).
- [x] Colors match status per hour: green/yellow/red/gray exactly per the mapping above (`PublicStatusPage.tsx`'s `hourlyColorVar`, T6).
- [x] Hovering/focusing a bar shows correct local date, hour range, and Portuguese status label (`hourlyTooltip`, T6, verified by `PublicStatusPage.test.tsx`).
- [x] A service that has never been polled shows 24 gray bars, not a missing row (verified by `TestPublicStatusGet_ServiceWithNoSnapshotsEver_AllHourlyBucketsNoData` and its preview counterpart).
- [x] Existing behavior (current status, incidents, company branding, SP-06/08/09 guarantees) is unchanged - regression-free (full existing integration + Vitest suites green after every task).
