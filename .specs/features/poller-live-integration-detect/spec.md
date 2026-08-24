# Poller Live Integration Detection Specification

## Problem Statement

The SLO poller only ever learns about the Datadog integration once, at `serve` boot (`newPollerFromStoredIntegration` in `internal/cli/serve.go`). Connecting Datadog through the admin UI after the process is already running persists the row, but the running poller never picks it up: services stay `Não configurado` indefinitely until an operator manually restarts the binary. Reproduced manually 2026-08-24 against a fresh dev database — two services with SLOs linked and Datadog shown as "Conectado" both stayed `Não configurado` until `serve` was restarted.

## Goals

- [x] An admin connecting Datadog for the first time starts the poller immediately, no process restart.
- [x] An admin rotating the Datadog key (same UI button, same endpoint, `POST /api/integrations/datadog`) makes the poller pick up the new credentials immediately — same root cause (poller state frozen at construction time), same fix.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Auto-discovery of newly-added services mid-run | Already solved — `pollOnce` calls `services.List(ctx)` every tick, so new services are already picked up without this fix. |
| Multi-instance/HA coordination for "who runs the poller" | Vane is single-tenant, single-process by design (AD-002/AD-001); out of scope. |
| New admin-facing UI/status surface | `GET /api/integrations/datadog/status` already exposes poller-observed health; no new endpoint needed. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Where the (re)start trigger lives | `IntegrationsHandler.ConnectDatadog` calls a poller-restart hook right after a successful `UpsertDatadog`, instead of the poller polling the DB for changes on its own timer | Same process, same request already has the confirmed-valid credentials; a DB-polling alternative would add latency and a second polling loop for no benefit | y (only viable approach given single-process architecture) |
| Restart-failure handling | A (re)start failure (e.g. decrypt error) is logged; the HTTP response still reports success | The DB row is the source of truth for "is Datadog connected" — that write already succeeded. Poller health is a separate, already-existing concern surfaced via `/api/integrations/datadog/status` | y |
| Concurrent connect/rotate calls | (Re)start is serialized (single mutex); a second call always tears down whatever poller is currently running before starting the new one | Integration is a DB singleton (`UNIQUE (provider)`) — only one logical poller should ever run at a time regardless of how many requests raced | y |

**Open questions:** none — all resolved above.

---

## User Stories

### P1: Poller starts on first connect, without restart ⭐ MVP

**User Story**: As a company admin, I want the status page to start reflecting real SLO data as soon as I connect Datadog, so that I don't have to know about or perform a server restart to see it work.

**Why P1**: This is the actual bug reported — services stuck on `Não configurado` after a successful connect is the entire problem.

**Acceptance Criteria**:

1. WHEN an admin successfully connects Datadog for the first time (no integration row existed before) THEN the system SHALL start polling services against that integration without any process restart.
2. WHILE the server is running with no Datadog integration connected yet THE system SHALL NOT start a poller (unchanged from current behavior).
3. IF the server starts with a Datadog integration already stored THEN the system SHALL start the poller at boot exactly as it does today (no regression).
4. WHEN the server receives a shutdown signal THEN the system SHALL stop whichever poller is currently running before exiting (same graceful-shutdown guarantee that exists today, now covering a poller started after boot too).

**Independent Test**: Start `serve` against a database with no Datadog integration; confirm poller doesn't run (no snapshots written). Call `POST /api/integrations/datadog` with valid credentials; within one poll interval, confirm `status_snapshots` rows appear for existing services with linked SLOs — no restart in between.

---

### P2: Rotated key takes effect without restart

**User Story**: As a company admin, I want rotating my Datadog key to keep the status page working immediately, so that key rotation isn't a maintenance-window event.

**Why P2**: Same bug class, same fix, but the initial-connect path (P1) is the one actually reported and blocks any real usage; rotation is the natural follow-on so the fix isn't half-applied to one of the two paths that share the same endpoint.

**Acceptance Criteria**:

1. WHEN an admin rotates the Datadog key on an already-connected integration THEN the system SHALL replace the running poller's client with one built from the new key, without a process restart.
2. IF the poller (re)start after a successful connect/rotate fails (e.g. a decrypt error) THEN the system SHALL log the failure and still return the successful `{"status":"connected"}` response — the persisted row is authoritative for that response, not poller state.

**Independent Test**: With Datadog already connected and the poller running, call `POST /api/integrations/datadog` again with a different (still valid) key pair; confirm the next poll cycle uses the new credentials (observable via the connectors/datadog client instance the poller holds) without restarting `serve`.

---

## Edge Cases

- IF two `POST /api/integrations/datadog` calls race THEN the system SHALL serialize the poller (re)start so exactly one poller ends up running, built from whichever write landed last in the database.
- WHEN a (re)start tears down a poller mid-tick THEN the system SHALL wait for that in-flight `Run` goroutine to actually return before starting the replacement, so two pollers never run concurrently against the same services.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| PLD-01 | P1: Poller starts on first connect | Execute | Implemented |
| PLD-02 | P1: Poller starts on first connect | Execute | Implemented |
| PLD-03 | P1: Poller starts on first connect | Execute | Implemented |
| PLD-04 | P1: Poller starts on first connect | Execute | Implemented |
| PLD-05 | P2: Rotated key takes effect | Execute | Implemented |
| PLD-06 | P2: Rotated key takes effect | Execute | Implemented |

**Coverage:** 6 total, 6 mapped, 0 unmapped

---

## Success Criteria

- [x] Connecting Datadog through the admin UI makes linked services leave `Não configurado` within one poll interval, with no `serve` restart. Verified via `TestConnectDatadog_ValidCredentials_RestartsPoller`.
- [x] Rotating the Datadog key keeps the poller running against the new credentials, with no `serve` restart. `Restart` is called on every successful `POST /api/integrations/datadog`, connect or rotate alike - same code path, same coverage.
- [x] Existing boot-time behavior (integration already stored at startup) and graceful shutdown are unchanged (regression-free). Verified via the full `internal/cli` and `internal/poller` suites passing unchanged, plus `TestNewHTTPSServer_*`/`TestAdminRouter_*`.
