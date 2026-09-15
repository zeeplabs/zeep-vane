# Manual Polling Monitoring Specification

## Problem Statement

`monitored-services-page` shipped SLO-based (Datadog) monitoring only — `gap-analysis.md` and that feature's own spec explicitly deferred "Polling manual" (HTTP/TCP/Ping, admin-configured interval) as future work with no backend model or scheduler. The user now wants it built for real: a second, independent monitoring mode a `Service` can use instead of a Datadog SLO, matching the "Como monitorar" toggle already drawn (but disabled) in `handoff-new-layout/Servicos Monitorados.dc.html`'s add-service drawer.

## Goals

- [ ] Admin can register a service monitored by direct HTTP(S), TCP, or "Ping" (TCP-reachability) checks instead of a Datadog SLO, with a configurable check interval (30s/1m/5m).
- [ ] A polling-manual service's status (operational/degraded/outage) is computed from real check results, using the same `status_intervals`/uptime/history machinery every other service already uses (Overview cards, the services list/drawer, the public status page).
- [ ] Polling-manual checks run under the same single-active-poller leadership model as the Datadog poller (`ha-multi-replica`) — never duplicated across replicas.
- [ ] A polling-manual service's target cannot be used to probe the deployment's own internal network (SSRF).

## Out of Scope

| Feature | Reason |
| --- | --- |
| "New Relic" as an SLO source | Explicit user decision (this session): frontend-only disabled/"Em breve" chip, zero backend work. Unrelated to this feature (SLO-based mode, not polling-manual). |
| Editing a service's configuration after creation (mode, target, interval, SLO) | `monitored-services-page`'s spec already decided "Editar configuração" is out of scope for every service regardless of mode (no update endpoint exists) — this feature doesn't reopen that. |
| Pausing/resuming monitoring | Same prior decision (`monitored-services-page` Out of Scope) — still holds for polling-manual services. |
| Real ICMP echo for "Ping" | User decision (this session): TCP-connect reachability instead — portable across Docker/K8s without `NET_RAW`, no dependency on the host allowing ICMP. |
| Per-check latency history/graphing | Not in the mock beyond the existing "Latência" table column, itself already out of scope (`monitored-services-page`, no data source for Datadog-mode services). Polling-manual *could* measure real latency, but exposing it needs its own UI decision — deferred. |
| Changing `ServiceRepository.List`'s poller-facing behavior for Datadog-mode services | Zero change to the existing Datadog poll path; this feature only adds a second, parallel path for polling-manual services. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| "Ping" check type | TCP-connect to the given host (default port 80 if the admin's target has no explicit port) | User decision: real ICMP needs `NET_RAW` (Dockerfile/docker-compose change, often blocked in K8s anyway) — TCP-connect is portable everywhere this app already deploys. | y |
| Consecutive-failure threshold before "outage" | 2 consecutive failed checks (mirrors the Datadog poller's `breachHysteresisCycles`, AD-019) | User decision: same lesson already learned the hard way for the Datadog poller applies here — a single transient network blip must not flip a service to outage. | y |
| SSRF protection | Reject a target whose resolved IP falls in `127.0.0.0/8`, `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, or `169.254.0.0/16` (covers loopback, private ranges, and cloud metadata endpoints), checked at creation **and** re-checked every poll cycle (DNS can change after creation - "TOCTOU"/DNS-rebinding) | User decision: multi-tenancy (AD-022) means an admin creating this target isn't necessarily trusted with access to the deployment's own internal network. | y |
| Hysteresis counter persistence | In-memory only (per-service `map[string]int` on the scheduler, like the Datadog poller's own `breachStreak`), reset on leader change/restart | Matches the existing Datadog poller's own precedent exactly (`internal/poller/poller.go`'s `breachStreak` field) - not a new pattern, no extra migration/table needed for a counter that only needs to survive within one leader's tenure. | y |
| Per-check timeout | 10 seconds for HTTP(S)/TCP/Ping alike | No value specified in the mock; 10s is generous enough to avoid false failures on a slow-but-healthy target while still bounding one check's worst case. Reasonable default, not derived from measured data - flagged here as adjustable later, same posture as the Datadog poller's own `minRecentWindowRequests`. | y |
| HTTP(S) status-code success range | 2xx and 3xx = operational; anything else (4xx/5xx, connection error, timeout) = failed check | Standard "is this endpoint reachable and not erroring" semantics; the mock gives no explicit success-criteria field to configure. | y |
| Degraded state for polling-manual services | Never reached automatically - only `operational`/`outage` (binary), same as any status a human/future feature could still set, but this poller itself never computes `degraded` | The mock's UI gives no per-check signal (like Datadog's SLO warning threshold) that would justify a `degraded` state; unlike Datadog's SLO-warning tier, a plain reachability check is pass/fail. Keeps the state machine honest instead of inventing a fake "degraded" trigger. | y |
| Scheduling model | One goroutine + ticker per polling-manual service, each on its own configured interval (30s/1m/5m), instead of one shared tick for every service like the Datadog poller | Different services can have different intervals in this mode (the Datadog poller has exactly one shared `interval` for all services) - the natural, simplest way to honor a per-service interval that varies is a per-service ticker. See design.md for the full approach comparison. | y |
| Manual-poller lifecycle vs. Datadog poller | Starts/stops independently of whether a Datadog integration is connected, but still gated by the same poller leadership (only the elected leader replica runs it) | The existing `PollerManager.Restart` only starts the Datadog poller when a Datadog integration exists (`internal/cli/poller_manager.go`) - polling-manual services have nothing to do with Datadog and must work in an install with zero Datadog integration configured. | y |
| Target field validation format per check type | HTTP(S): a full URL (`https://...`); TCP: `host:port`; Ping: bare host (optional `:port`, defaults to 80) | Matches the mock's own per-type placeholder text (`https://api.acme.health/health`, `db.acme.health:5432`, `cache.acme.health`) exactly. | y |

**Open questions:** none — all resolved above.

---

## User Stories

### P1: Register a polling-manual service ⭐ MVP

**User Story**: As an admin, I want to add a service monitored by direct HTTP/TCP/Ping checks (no Datadog SLO required), so I can monitor something that has no SLO configured upstream.

**Why P1**: Without creation, nothing else in this feature has any data to operate on - the vertical slice starts here.

**Acceptance Criteria**:

1. WHEN the admin selects "Polling manual" in the add-service drawer THEN the system SHALL show a check-type selector (HTTP(S) / TCP / Ping), a target field whose label and placeholder match the selected check type, and an interval selector (30s / 1min / 5min).
2. WHEN the admin submits with a valid target for the selected check type and an interval selected THEN the system SHALL create the service with `monitor_mode = "polling"` and the given `poll_type`/`poll_target`/`poll_interval_seconds`, and SHALL NOT require an `slo_id`.
3. IF the target resolves to an IP in `127.0.0.0/8`, `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, or `169.254.0.0/16` THEN the system SHALL reject creation with a 422 and SHALL NOT persist the service.
4. IF the target's format doesn't match the selected check type (e.g. a bare host for HTTP(S), which requires a URL) THEN the system SHALL reject creation with a 422 identifying the malformed field.
5. The system SHALL NOT require or accept an `slo_id`/`slo_name` when `monitor_mode = "polling"`, and SHALL NOT require or accept `poll_type`/`poll_target`/`poll_interval_seconds` when `monitor_mode = "slo"`.

**Independent Test**: Submit the add-service form with "Polling manual" + HTTP(S) + a real reachable URL + 30s interval; confirm the service is created and appears in the list with `current_status = "not_configured"` until the first check runs. Submit again with a target resolving to `127.0.0.1`; confirm 422, no service created.

---

### P2: Polling-manual services get checked and their status updates

**User Story**: As an admin, I want a polling-manual service's status to reflect real check results, so the dashboard tells me the truth about it just like a Datadog-backed service.

**Why P2**: The point of the feature - P1 alone only creates inert rows.

**Acceptance Criteria**:

1. WHEN a polling-manual service's configured interval elapses THEN the system SHALL run one check of its configured type against its target, with a 10-second timeout.
2. WHEN an HTTP(S) check receives a 2xx or 3xx response THEN the system SHALL treat it as succeeded; any other response, connection error, or timeout SHALL be treated as failed.
3. WHEN a TCP or Ping check successfully opens a TCP connection to the target (Ping: host on port 80 if none given) within the timeout THEN the system SHALL treat it as succeeded; a connection error or timeout SHALL be treated as failed.
4. WHEN a check succeeds THEN the system SHALL record the service's status as `operational` and reset its consecutive-failure count to 0.
5. WHILE a service has fewer than 2 consecutive failed checks, a failed check SHALL NOT change its recorded status (the previous status is carried forward).
6. WHEN a service reaches 2 consecutive failed checks THEN the system SHALL record its status as `outage`.
7. Every recorded status change SHALL open/extend a `status_intervals` row exactly as the Datadog poller already does, so uptime/history/Overview aggregates include polling-manual services automatically, with no separate code path in `internal/history` or `OverviewHandler`.
8. WHILE the deployment runs multiple replicas (`ha-multi-replica`) THE system SHALL run polling-manual checks on the elected leader replica only, never on more than one replica concurrently.
9. WHILE zero polling-manual services exist THE system SHALL NOT start the polling-manual scheduler's checks (no-op, not an error).
10. WHEN a polling-manual service's target re-resolves to a blocked IP range (assumption: SSRF check re-run every cycle) after having been created with a non-blocked one THEN the system SHALL skip that cycle's check (treated as failed, contributing to the consecutive-failure count) rather than connecting to the now-blocked address.

**Independent Test**: Create a polling-manual HTTP service pointed at a real local test server; confirm it flips to `operational` within one interval. Stop that test server; confirm it stays `operational` after 1 failed check, then flips to `outage` after the 2nd consecutive failed check. Confirm its `status_intervals` rows and uptime/history read paths behave identically to a Datadog-mode service's.

---

### P3: Add-service drawer matches the mock's full toggle UI

**User Story**: As an admin, I want the add-service drawer to look and behave like the mock (mode toggle, per-mode fields, disabled "New Relic" chip), so the screen matches the design system across every monitoring mode.

**Why P3**: Builds on P1's backend contract with the exact UI the mock specifies; independently reviewable/shippable after the API exists.

**Acceptance Criteria**:

1. WHEN the drawer opens THEN the system SHALL show the "Como monitorar" toggle (Baseado em SLO / Polling manual), defaulting to "Baseado em SLO" (today's existing behavior, unchanged for users who never touch the toggle).
2. WHEN "Baseado em SLO" is selected THEN the system SHALL show the existing "Fonte" chips (Datadog selected/enabled, "New Relic" visible but disabled/non-interactive) and the existing SLO search field, exactly as today.
3. WHEN "Polling manual" is selected THEN the system SHALL show the check-type chips, the target field (label/placeholder switching with the selected type), and the interval chips, and SHALL hide the "Fonte"/SLO-search fields.
4. IF the admin clicks the disabled "New Relic" chip THEN the system SHALL NOT change the selected source and SHALL NOT submit any New-Relic-related value.
5. The submit button SHALL be disabled until the current mode's required fields are all filled (name + SLO selected for SLO mode; name + valid target + interval for polling mode).

**Independent Test**: Toggle between the two modes repeatedly, confirm the right fields show/hide each time and stale values from the other mode don't leak into submission; click "New Relic" and confirm nothing happens.

---

## Edge Cases

- IF the admin's HTTP(S) target has no scheme (`api.acme.health` instead of `https://api.acme.health`) THEN the system SHALL reject it as a format error (P1 AC4), not silently prepend a scheme.
- IF a TCP/Ping target's host is a bare IP address that itself falls in a blocked range THEN the system SHALL reject it the same as a hostname that resolves there (P1 AC3 applies to the literal address too, not only DNS resolution).
- IF two consecutive checks fail for different reasons (e.g. timeout then a 500) THEN the system SHALL still count them as 2 consecutive failures (P2 AC6) - the failure *reason* doesn't reset or affect the streak.
- WHEN the leader replica changes mid-cycle (an existing leader loses its lock) THEN the new leader's polling-manual scheduler SHALL start every service's consecutive-failure count at 0 (Assumption: in-memory hysteresis, matches the Datadog poller's own restart behavior - never a bug report against that poller's identical behavior, so no stricter guarantee is owed here either).

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| MP-01 | P1 | Execute (T1/T2/T5) | Done |
| MP-02 | P1 | Execute (T1/T2/T5) | Done |
| MP-03 | P1 | Execute (T1/T4/T5) | Done |
| MP-04 | P1 | Execute (T3/T5) | Done |
| MP-05 | P1 | Execute (T1/T5) | Done |
| MP-06 | P2 | Execute (T2) | Partial (repository plumbing only - poller.ManualScheduler is T6) |
| MP-07 | P2 | Execute (T4) | Partial (checks.RunCheck only - wired into a scheduler at T6) |
| MP-08 | P2 | Design | Pending |
| MP-09 | P2 | Design | Pending |
| MP-10 | P2 | Design | Pending |
| MP-11 | P2 | Design | Pending |
| MP-12 | P2 | Design | Pending |
| MP-13 | P2 | Design | Pending |
| MP-14 | P2 | Design | Pending |
| MP-15 | P2 | Execute (T4) | Partial (RunCheck's own dial-time guard only - full scheduler wiring is T6) |
| MP-16 | P3 | Design | Pending |
| MP-17 | P3 | Design | Pending |
| MP-18 | P3 | Design | Pending |
| MP-19 | P3 | Design | Pending |
| MP-20 | P3 | Design | Pending |

**ID format:** `MP-[NUMBER]`, sequential in file order (P1 ACs 1-5 → MP-01..05, P2 ACs 1-10 → MP-06..15, P3 ACs 1-5 → MP-16..20).

**Coverage:** 20 total, 5 fully done (P1, T1-T5), 3 partially covered (T2/T4 groundwork for T6), 12 pending (T6-T9 not yet executed).

---

## Success Criteria

- [ ] A polling-manual service can be created via the redesigned drawer and reaches a real, check-derived status without any Datadog SLO involved.
- [ ] Polling-manual services show up correctly everywhere an SLO-based service already does (services list, detail drawer, Overview, public status page) with zero special-casing in those read paths.
- [ ] No polling-manual check ever runs against a private/internal-network target.
- [ ] Only one replica ever runs polling-manual checks at a time in a multi-replica deployment.
