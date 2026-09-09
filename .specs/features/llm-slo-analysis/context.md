# LLM SLO Analysis Context

**Gathered:** 2026-09-09
**Spec:** `.specs/features/llm-slo-analysis/spec.md`
**Status:** Ready for design

---

## Feature Boundary

An LLM (OpenAI first, adapter-based for future providers) turns the poller's own SLO status transitions into human-readable text: a cached tooltip on `degraded`, an auto-created formal incident on `outage`, and a proposed (never auto-applied) closing comment on recovery to `operational`. Configuration lives in a new Settings area (owner/operator only), following the existing `email_providers`/`email_settings` connect/activate shape.

---

## Implementation Decisions

### Incident auto-close risk

- LLM never closes an incident directly. It writes a proposed closing comment; the incident stays visibly open until an owner/operator explicitly confirms it.
- Rationale given: mirrors the flapping risk `AD-19 addendum 3` (breach hysteresis) was written to contain — a wrongly-closed public incident is worse than a delayed one.

### LLM failure / unavailability handling

- Incident creation on `outage` never depends on the LLM call succeeding — a fixed generic description is written synchronously, the LLM-authored text replaces it later if/when the call succeeds within its bounded timeout.
- Same principle for the closing-comment proposal: if the call fails/times out, the incident just stays open with no proposal — recovery detection itself is never delayed by it.
- Same for the `degraded` tooltip: no tooltip content is shown (not an error, not a placeholder) until a real analysis completes.

### Recovery boundary for proposing closure

- Only `outage` → `operational` proposes a closing comment. `outage` → `degraded` leaves the incident open — `degraded` is still a real problem.

### Default model

- `gpt-4o-mini` when an operator connects only an API key without picking a model explicitly. Chosen for cost/latency fit to a short factual summary task, not text quality maximization.

### Incident granularity

- One incident per service per transition, never auto-grouped/correlated across services even if simultaneous. No correlation heuristic in scope.

### Tooltip re-analysis cadence

- Generated once per transition into `degraded`, cached until the next status change (away from `degraded`). Not re-run every poll cycle while the service stays `degraded`.

### Agent's Discretion

- Exact LLM-call timeout value and the detached-dispatch mechanism (mirrors `AD-014`'s detached password-reset email send) — left for Design, bounded by the hard constraint that a slow/hung LLM call must never stall `pollService`'s loop over the remaining services in a poll cycle.
- Idempotency guard ("at most one open incident per service at a time") — logical consequence of "one incident per service transition," not separately discussed, implemented as a straightforward invariant.
- Role gating for the new Settings endpoints (`writeRoles` for connect/activate, `anyRole` for read/status) — resolved by reading the existing `/api/integrations/email/*` route wiring in `internal/cli/routes.go`, not asked.
- Credential storage mechanism — resolved by reading `internal/crypto` secretbox + the existing `integrations`/`email_providers` migrations, not asked.

---

## Specific References

- Explicit build-order request: "que seja criado de forma que possamos expandir fácil e que funcione com múltiplas LLM's" — direct instruction to design the adapter first even though only OpenAI ships.
- Explicit UX reference for the degraded case: "essa análise serviria para ser mostrada em um tooltip na status page pública quando o usuário passar o mouse em cima" — matches the existing hourly-history tooltip pattern already shipped (`title` + `tabIndex`, native browser tooltip, no new component).

---

## Deferred Ideas

- Auto-grouping/correlating simultaneous multi-service outages into one incident (explicitly deferred — see spec's Out of Scope).
- Editing/regenerating LLM-authored text via the UI after the fact (explicitly deferred).
- Shipping a second provider (Gemini) now rather than just the adapter (explicitly deferred).
