# SLO Root-Cause Enrichment Specification

## Problem Statement

Today, `internal/llm.AnalysisInput` carries only SLI/target/timeframe/error-budget numbers into the LLM prompts that generate degraded-tooltip, outage-description, and closing-comment text (`internal/llm/prompts.go`). No data about *what actually broke* is ever passed in, and the system prompt explicitly forbids the model from speculating beyond the given data. Result: generated text is structurally generic and near-identical across unrelated services, confirmed by the user across multiple real degradation events. Verified live against the Starbem org's real Datadog account this session: Error Tracking Issues search already returns concrete, usable cause data (e.g. `error_type: MongooseError`, `error_message: "Operation psychology_chat_sessions.aggregate() buffering timed out after 10000ms"`) for a currently-breached service — the raw material exists, it's just never fetched.

## Goals

- [ ] For a tenant that opts in, a degraded/outage transition on a `metric`-type SLO enriches `AnalysisInput` with a real error signal (type + message) from Datadog Error Tracking, pulled from the SLO's own service/env scope, before the existing async LLM call runs.
- [ ] Tenants that do not opt in see zero behavior change and zero extra Datadog API calls — this feature is additive, never a default-on cost increase.
- [ ] The toggle is a per-tenant setting (`llm_settings`), consistent with AD-022's multi-tenant model (self-hosted 1-tenant installs and Zeep's own SaaS deployment both get their own on/off state).

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
| --- | --- |
| New Relic / Grafana / Elastic connectors | User explicitly deferred this to a separate future initiative; `internal/connectors/datadog` is the only connector touched here. |
| Per-service toggle granularity | Confirmed with user: account-wide (tenant-wide) toggle only. A per-service flag can be added later without changing this feature's shape. |
| UI to browse/inspect the raw Error Tracking issue | Only the enrichment text the LLM produces is user-facing; no new admin screen shows raw Datadog issue data. |
| Backfilling cause data into past/already-closed incidents | Enrichment only applies to transitions that happen after this feature ships. |
| Full APM span/trace inspection | Error Tracking Issues search is the chosen data source (verified live, gives clean `error_type`/`error_message`); raw span search is not used. |

---

## Assumptions & Open Questions

Every ambiguity is resolved or recorded here - nothing is left silently unclear.

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Toggle scope | Account-wide (tenant-wide), one boolean on `llm_settings` | Simpler schema/UI; no signal yet that per-service control is needed | y |
| Data sent to the LLM | Top-1 Error Tracking issue (by `total_count`) in the query window, `error_type` + `error_message` only — no stack trace, no `file_path` | Keeps the prompt small and avoids ever routing internal file paths into a visitor-facing text generator, even though the system prompt already forbids jargon | y |
| Fallback when Error Tracking call fails/times out/returns nothing | `AnalysisInput`'s cause field stays empty; prompt behaves exactly like today (SLO-numbers-only) | Enrichment is best-effort by design — must never block, delay, or degrade the analysis that already works | y |
| `monitor`-type SLO path | Deferred to P2 | Live check this session: 0 of 46 real SLOs in the org's Datadog account are `slo_type: "monitor"` — building it for P1 would ship code with no real data to validate it against | y |
| Error Tracking query time window | Last 10 minutes ending at the moment `HandleTransition` dispatches the enrichment goroutine | Not user-confirmed — chosen because the transition just happened (no meaningful "interval so far" to look back on yet), and 10 minutes is long enough to catch a burst of errors that caused the breach without pulling in stale, unrelated issues from hours earlier. Revisit in Design if a longer/shorter window proves wrong empirically. | n |
| Exact Datadog REST endpoint/query shape for the Go connector (`internal/connectors/datadog`) | To be confirmed in Design against Datadog's public Error Tracking API docs (this session verified behavior via an MCP tool, not the raw REST contract the Go client must implement) | The MCP tool used to verify this session wraps Datadog's API; the connector needs the actual public REST endpoint, which wasn't independently checked | n |
| Toggle disabled state when no LLM provider is connected | Toggle is disabled/greyed with an explanatory tooltip, mirroring how the rest of the AI settings UI already gates on an active provider (to be verified in Design against the real `SettingsPage`/`llmProviderHooks` behavior) | Consistent with existing UX pattern rather than inventing a new one | n |

**Open questions:** none — all resolved above (2 default-with-rationale entries flagged `n` are Design-phase verification items, not open product decisions).

---

## User Stories

### P1: Real cause data in metric-type SLO enrichment ⭐ MVP

**User Story**: As a status-page visitor (and the admin who configured the account), I want a degraded/outage description that reflects what's actually happening, so that the text is more than generic boilerplate repeated across every incident.

**Why P1**: This is the entire point of the feature — the `metric`-type path is 100% of real SLOs in the reference org today, so it's the only path that has real value on day one.

**Acceptance Criteria**:

1. WHEN a service's linked SLO has `slo_type: "metric"` AND the tenant's `llm_settings.root_cause_enrichment_enabled` is true AND `SLOAnalyzer` dispatches a degraded or outage enrichment goroutine THEN the system SHALL query Datadog Error Tracking Issues scoped to the SLO's `service` and `env:production` tags, for the 10 minutes ending at dispatch time, before calling `llm.Service.Generate*`.
2. WHEN that query returns at least one issue THEN the system SHALL populate `AnalysisInput`'s new cause field with the top issue's `error_type` and `error_message`, ranked by `total_count` descending.
3. IF the Error Tracking query fails, times out, or returns zero issues THEN the system SHALL leave the cause field empty and proceed with today's SLO-numbers-only prompt, without failing or delaying the enrichment goroutine.
4. WHILE `llm_settings.root_cause_enrichment_enabled` is false (the default for every tenant) the system SHALL never issue the Error Tracking query — zero extra Datadog API calls, byte-identical behavior to today.
5. The system SHALL perform the Error Tracking query inside the existing detached enrichment goroutine (`dispatchDegradedEnrichment`/`dispatchOutageEnrichment`), bounded by the same `AnalysisTimeout` (30s) already governing the LLM call — no new synchronous call added to the poll cycle.
6. WHEN the cause field is non-empty THEN the system prompt SHALL be extended to allow (not require) the model to reflect the practical impact implied by that cause, while the existing constraints (no jargon, no service/technical names, 1-2 sentences, calm tone) SHALL remain unchanged.

**Independent Test**: With the toggle enabled and a fake Datadog connector returning a fixed `error_type`/`error_message` for a degraded service, a unit test asserts the built `AnalysisInput` carries that cause data, and that the resulting prompt text sent to the (faked) LLM provider differs from the toggle-off case. With the toggle disabled, the same test asserts the fake connector's Error Tracking method is never called.

---

### P2: Admin-facing toggle + `monitor`-type SLO path

**User Story**: As a tenant admin, I want to control whether Vane makes extra Datadog calls to enrich incident text, so that I can weigh the better output against the added API cost/latency myself.

**Why P2**: Depends on P1's plumbing existing; the setting itself and its UI are a thin layer on top, and `monitor`-type SLOs have zero real usage today so validating that path isn't urgent.

**Acceptance Criteria**:

1. WHEN an owner/operator opens the settings screen that already hosts LLM/AI configuration THEN the system SHALL show a toggle reflecting `llm_settings.root_cause_enrichment_enabled` for the current tenant.
2. WHEN the toggle is switched THEN the system SHALL persist the new value via a tenant-scoped update endpoint, taking effect on the very next poll-cycle transition — no restart required.
3. The system SHALL default `root_cause_enrichment_enabled` to `false` for every existing and newly created tenant (opt-in only).
4. WHEN a service's linked SLO has `slo_type: "monitor"` AND enrichment is enabled THEN the system SHALL resolve `monitor_ids` from the SLO metadata already fetched via `SearchSLOs` at link time (no new Datadog API call for this path) and use the monitor's own alert message as the cause field.
5. IF `slo_type: "monitor"` but no usable monitor message is available THEN the system SHALL fall back to the same empty-cause behavior as P1's AC3.

**Independent Test**: Toggling the setting in the UI and confirming (via a test double / integration test) that the next simulated transition either does or does not issue the Error Tracking query, matching the toggle's state.

---

## Edge Cases

- IF the tenant's Datadog App Key lacks Error Tracking read scope THEN the system SHALL log this distinctly from a network timeout (so an operator can tell "wrong permissions" from "Datadog was slow") and fall back exactly like any other query failure.
- IF the top issue's `error_message` is unusually long (e.g. a multi-field validation error, confirmed live to happen in this org) THEN the system SHALL cap it to a bounded length before it enters `AnalysisInput`, protecting the LLM prompt's token budget and avoiding ever routing a stack-trace-shaped string into a visitor-facing generation call.
- WHEN multiple services transition into degraded within the same poll cycle THEN the existing `maxConcurrentEnrichments` (4) and `enrichmentCooldown` (2 min) bounds already apply to the whole enrichment goroutine including this new query — no new concurrency control is introduced.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| RCA-01 | P1: Real cause data | Tasks | Implementing |
| RCA-02 | P1: Real cause data | Tasks | Implementing |
| RCA-03 | P1: Real cause data | Tasks | Implementing |
| RCA-04 | P1: Real cause data | Design | Pending |
| RCA-05 | P1: Real cause data | Design | Pending |
| RCA-06 | P1: Real cause data | Design | Pending |
| RCA-07 | P2: Admin toggle | Design | Pending |
| RCA-08 | P2: Admin toggle | Design | Pending |
| RCA-09 | P2: Admin toggle | Design | Pending |
| RCA-10 | P2: monitor-type path | Design | Pending |
| RCA-11 | P2: monitor-type path | Design | Pending |

**ID format:** `RCA-NN`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 11 total, 0 mapped to tasks, 11 unmapped ⚠️ (expected pre-Design)

---

## Success Criteria

- [ ] A degraded/outage transition on a `metric`-type SLO, with enrichment enabled and a real Error Tracking issue present, produces LLM output that changes in substance when the underlying error changes — verifiable in a unit test comparing two fixture error signals.
- [ ] A tenant with the toggle off makes exactly the same set of Datadog API calls as before this feature shipped — verifiable by asserting the fake connector's Error Tracking method call count is 0.
- [ ] `AnalysisTimeout` (30s), `maxConcurrentEnrichments` (4), and `enrichmentCooldown` (2 min) remain unchanged in value and continue to bound the new query the same way they bound the existing LLM call.
