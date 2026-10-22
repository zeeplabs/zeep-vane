# Poller Burn-Rate Classification Context

**Gathered:** 2026-10-22
**Spec:** `.specs/features/poller-burn-rate-classification/spec.md`
**Status:** Ready for design

---

## Feature Boundary

Replace the poller's single-window, 30-day-target comparison (and the AD-019 heuristics built around it) with multi-window burn-rate classification computed by Vane itself, fix the zero/low-traffic stuck states, and expose the numbers to admins and to the LLM prompts. Public page payload unchanged.

---

## Implementation Decisions

### Status source

- Vane computes burn rate itself from `/api/v1/slo/{id}/history`; it does not read Datadog `slo alert` monitor state (self-hosted tenants must not depend on monitors created outside Vane).

### Thresholds

- Same as Starbem's production burn-rate monitors: outage at 14.4x, degraded at 7.2x, in both the 5m and 1h windows.
- Fixed defaults (package constants), not configurable per tenant/service.

### Zero traffic after an incident

- Grace period: keep the current `outage`/`degraded` status for 15 minutes without new errors, then return to `operational`. Not instant (a crashed pod emits no traces), not "until traffic returns" (today's stuck behavior).

### Low traffic

- Minimum absolute errors in the short window (default 3) before degraded/outage. One isolated error never opens an incident. Replaces the `minRecentWindowRequests` carry-forward.

### Where the numbers show up

- Admin service detail + LLM prompt input (incident description, degraded tooltip, closing comment). Not on the public status page.

### Agent's Discretion

- Keep/reduce/remove `breachHysteresisCycles`, decided from the replay.
- One vs two `/history` calls per service per cycle, decided from API research and rate limits.
- Where the latest reading is persisted (service row columns vs status interval), as long as it survives restart/HA handover.
- Exact values of minimum errors (default 3) and grace period (default 15 min) may move if the replay justifies it; any change gets reported back before implementation.

---

## Specific References

- Starbem production monitors (`infra/datadog/terraform/flows_slo.tf`): `burn_rate("<slo>").over("30d").long_window("1h").short_window("5m")`, `critical = "14.4"`, `warning = "7.2"`.
- AD-019 addendum 3, items 1-5: the definitive fix described there.
- Staging evidence (2026-10-22): 40 metric-based SLOs; `stela-emotional-ms` `breached` from ~1 error in 44 requests over 30 days; 3 SLOs `no_data`.

---

## Deferred Ideas

- Per-tenant configurable thresholds.
- Burn rate / error rate on the public status page.
- Synthetic checks to detect services that are down without emitting traces (partly covered by `manual-polling-monitoring`).
