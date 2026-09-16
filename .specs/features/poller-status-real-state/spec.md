# Poller Status Real State Specification

## Problem Statement

The redesigned Poller Status screen (`handoff-new-layout/Poller Status.dc.html`, tracked in `.specs/features/new-layout-migration/gap-analysis.md`) was mocked as a multi-region poller fleet — per-node CPU/mem, queue depth, throughput, and a "restart poller" action. Vane's real poller architecture (`ha-multi-replica`, `internal/cli/poller_manager.go`) is a single active poller elected via a Postgres advisory lock (`pollerLeaderLockKey`) — there is no fleet, no per-node region, no queue, and no CPU/mem telemetry. This spec replaces the mocked fleet view with a screen that reports what actually exists: whether a poller is currently running, which replica is leading it, and real per-tenant polling activity derived from data already written by the poller.

## Goals

- [ ] `GET /api/poller/status` reports the real, live leadership state (is a poller running, which replica holds the lock) instead of a fake fleet.
- [ ] The response includes real per-tenant polling activity (checks recorded in the last minute, error state) derived from `status_intervals` and `integrations` — no invented metrics.
- [ ] No new database table and no new schema for leadership history — leadership state is read live from Postgres (`pg_locks`/`pg_stat_activity`), never persisted separately.
- [ ] A replica is identifiable in that live state by hostname, so "which replica is leading" is answerable without guessing from an IP.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Leadership/failover history (past leader changes over time) | Decided with the user: live-only state, no new table, no persisted event log. |
| CPU/mem/throughput/queue-depth telemetry | Doesn't exist in this architecture (no worker queue, no metrics pipeline) and building one is infra work far beyond this screen's gap. Decided with the user: derive only what's computable from existing data. |
| Multi-region / multi-node fleet display | The real architecture has exactly one active poller at a time; there is no "region" concept to display. |
| "Reiniciar poller" action | Decided with the user: removed. No safe generic restart exists today — `PollerManager.Restart` only takes effect on the replica that currently holds leadership, and an HTTP request can land on any replica; the poller already self-heals via leader election and already restarts automatically when a Datadog integration is connected/rotated. |
| Any change to `PollerManager`'s leader-election mechanism itself | This spec only adds read-side reporting on top of the existing, already-verified (`ha-multi-replica` HA-01..HA-07) mechanism. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Leadership state source | Query Postgres directly for the current holder of `pollerLeaderLockKey` (`SELECT pid, granted FROM pg_locks WHERE locktype='advisory' AND objid=727200001`, joined to `pg_stat_activity` for `application_name`/`backend_start`) — no new table | User decision: live-only, zero persistence. | y |
| Replica identity | Every connection `pglock.TryAcquire` opens for the poller leader lock sets `application_name` to the replica's hostname (`os.Getenv("HOSTNAME")`, which Kubernetes sets to the pod name; falls back to a fixed placeholder like `"unknown"` outside Kubernetes) | `pg_stat_activity.application_name` is exactly the field meant for this and requires no new connection parameter beyond what `pgx.Connect` already accepts in the DSN; without it the only available signal is `client_addr` (a pod IP, meaningless to a human operator). | y — agent default, no objection raised |
| Scope of the change to `pglock` | `PollerManager` appends `application_name` to the DSN it already builds before calling `pglock.TryAcquire` — the shared `internal/pglock` package's function signatures do not change, so CertMagic's own use of the same package (`internal/tls`) is unaffected | Keeps blast radius to the one caller that needs the identity; `pglock` stays a generic advisory-lock primitive. | y |
| "Poller is running" vs. "leadership is held" | These are reported as two distinct booleans: `leader_elected` (someone holds the advisory lock) and `poller_running` (that leader also has a stored Datadog integration and its `Poller.Run` loop is actually active) | `RunLeaderLoop` acquires the lock unconditionally, but `Restart` is a no-op until a Datadog integration exists (`poller_manager.go` `Restart`/`newPollerFromStoredIntegration`) — collapsing these into one boolean would hide the real "leader elected, nothing to poll yet" state the UI needs to render correctly (matches the existing self-hosted bootstrap window before Datadog is connected). | y — agent default, no objection raised |
| "Verificações/min" replacement | Count of `status_intervals` rows whose `last_seen_at` falls in the last 60 seconds, scoped to the caller's tenant via the existing RLS/`app.tenant_id` path (no new cross-tenant query, no `SystemTenantLister` involved) | Directly measures real polling activity already written by the poller for that tenant; requires one new repository method (`StatusIntervalRepository.CountUpdatedSince`), no new table. | y — agent default, no objection raised |
| "Taxa de erro" replacement | Reuses the existing per-`Integration` `status`/`last_error` already exposed by `GET /api/poller/status` today (Datadog integration is `"invalid"` with `last_error` set, or `"active"`) — no new "24h error rate" percentage is computed, since no time-series error log exists to compute one from | Computing a genuine 24h error rate would require a new error-events table (out of scope, no such log exists); the existing field already answers "is polling currently failing and why" without inventing a number that isn't backed by real historical data. | y — agent default, no objection raised |
| Endpoint & RBAC | Same route, `GET /api/poller/status`, same `anyRole` gate as today — response shape changes, path/role does not | This is a read-only ops-health view already visible to any authenticated admin (`internal/cli/routes.go:196`); nothing about redesigning its payload changes who should see it. | y — agent default, no objection raised |
| Behavior when Postgres reports nobody holds the lock (e.g., all replicas mid-restart) | `leader_elected: false`, `poller_running: false`, `replica: null` — a legitimate, momentary state, not an error | Matches reality: between a lock release and the next `TryAcquire` succeeding elsewhere, there is a real (usually sub-second) window with no leader. The endpoint reports what's true right now rather than papering over it. | y — agent default, no objection raised |

**Open questions:** none — all resolved or logged above.

---

## User Stories

### P1: Admin sees the real current poller state ⭐ MVP

**User Story**: As an owner/operator/viewer, I want to see whether a poller is currently running and which replica is leading it, so I can trust the ops view instead of looking at a fabricated fleet.

**Why P1**: This is the entire redesigned screen's data source; without it there is nothing real to render.

**Acceptance Criteria**:

1. WHEN `GET /api/poller/status` is called and some replica currently holds `pollerLeaderLockKey` THEN the system SHALL respond `200` with `leader_elected: true` and a `replica` object containing that replica's `application_name` (hostname) and the lock-holding session's `backend_start` timestamp.
2. WHEN the leading replica also has a stored, connected Datadog integration THEN the system SHALL additionally report `poller_running: true`.
3. IF the leading replica has no stored Datadog integration yet THEN the system SHALL report `leader_elected: true` and `poller_running: false`.
4. IF no replica currently holds the lock THEN the system SHALL respond `200` with `leader_elected: false`, `poller_running: false`, and `replica: null` — never an error.
5. The system SHALL derive this state from a live query against `pg_locks`/`pg_stat_activity` on every call — it SHALL NOT cache or persist leadership state across requests.

**Independent Test**: With one `PollerManager` running and a connected Datadog integration in the test database, call the endpoint and confirm `leader_elected: true`, `poller_running: true`, and a non-null `replica.application_name`. Stop the process holding the lock (or use the existing `ha-multi-replica` test pattern of closing the lock connection) and confirm a subsequent call reports `leader_elected: false`.

---

### P1: Admin sees real per-tenant polling activity ⭐ MVP

**User Story**: As an owner/operator/viewer, I want to see how many checks were actually recorded recently and which integration is failing, so the ops view reflects genuine activity instead of an invented "checks/min" number.

**Why P1**: Required for the summary cards the redesigned screen still shows (Verificações/min, taxa de erro) — both must be computed from real data or dropped, never fabricated.

**Acceptance Criteria**:

1. WHEN `GET /api/poller/status` is called THEN the system SHALL include a `checks_last_minute` count equal to the number of `status_intervals` rows whose `last_seen_at` is within the last 60 seconds, scoped to the caller's own tenant.
2. The system SHALL continue reporting each connected integration's existing `status`/`last_error`/`last_checked_at` fields, unchanged from today's `GET /api/poller/status` response.
3. WHILE no `status_intervals` row has been updated in the last 60 seconds THE system SHALL report `checks_last_minute: 0` — this is a valid state (e.g., poll interval longer than a minute, or SLO-based monitoring between fetch cycles), not an error.

**Independent Test**: Seed `status_intervals` rows with `last_seen_at` inside and outside the last-minute window, call the endpoint, and confirm `checks_last_minute` counts only the ones inside the window.

---

## Edge Cases

- IF the `pg_locks`/`pg_stat_activity` query itself fails (Postgres error) THEN the system SHALL respond `500` with the existing generic internal-error body, logging the real error server-side (matches every other handler's error-handling convention in this codebase).
- WHEN the current process's own replica is the one leading THEN the system SHALL report itself the same way it would report any other replica — no special-casing "is it me".
- WHEN `HOSTNAME` is unset (non-Kubernetes deployment, e.g. a bare Docker Compose self-hosted install) THEN `application_name` SHALL fall back to a fixed placeholder rather than an empty string, so the UI never renders a blank replica name.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| POLLST-01 | P1: Real current poller state | Execute | Verified |
| POLLST-02 | P1: Real current poller state | Execute | Verified |
| POLLST-03 | P1: Real current poller state | Execute | Verified |
| POLLST-04 | P1: Real current poller state | Execute | Verified |
| POLLST-05 | P1: Real per-tenant polling activity | Execute | Verified |
| POLLST-06 | P1: Real per-tenant polling activity | Execute | Verified |

**Coverage:** 6 total, 6 mapped to tasks, 0 unmapped (Medium scope — tasks were implicit in Execute, not a formal `tasks.md`)

---

## Success Criteria

- [x] `GET /api/poller/status` reports live `leader_elected`/`poller_running`/`replica` state with no new table.
- [x] `checks_last_minute` reflects real `status_intervals` activity, tenant-scoped.
- [x] No mocked fleet fields (region, CPU, mem, queue depth, node list) remain in the response shape.
- [x] No "restart poller" endpoint is added.
