# LLM SLO Analysis Specification

## Problem Statement

When a service's SLO degrades or breaks, the public status page today shows only a bare status label ("degraded"/"outage") with no explanation of what happened, and every outage requires an admin to manually write an incident. Julio wants an LLM (starting with OpenAI, architected for other providers later) to turn the poller's own state transitions into a short, readable explanation — shown as a tooltip for a degradation, and as an auto-created formal incident (with an auto-proposed closing comment on recovery) for a full outage — without making incident creation itself depend on an external AI call succeeding.

## Goals

- [ ] Owner/operator can connect an OpenAI API key and pick a model from a new Settings area, following the same connect/activate pattern already used for email providers.
- [ ] A service transitioning to `outage` gets a formal incident automatically, always (never blocked on the LLM), with an LLM-authored description enriching it when available.
- [ ] A service transitioning to `degraded` gets a short LLM-authored explanation surfaced as a tooltip on the public status page, cached until the next transition.
- [ ] A service recovering to `operational` after an open incident gets an LLM-authored closing comment proposed for owner/operator confirmation, never auto-closed.
- [ ] The provider integration is built behind an adapter interface so a second provider (e.g. Gemini) can be added later without touching the analysis/incident business logic.

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
| --- | --- |
| Shipping a second provider (Gemini, etc.) now | Goal is an adapter that *supports* adding one later, not shipping one in this MVP — mirrors how `email.Provider` shipped with SendGrid+Resend together but this feature only needs OpenAI to prove the abstraction. |
| Auto-grouping/correlating simultaneous multi-service outages into one incident | Confirmed with Julio: one incident per service transition, no correlation heuristic. Manual merging by an admin, if ever needed, is a separate feature. |
| Manually editing/regenerating an LLM-authored incident description or tooltip via the UI | Incidents already support admin-authored `incident_updates` as a separate mechanism; rewriting the LLM's own text is not requested. |
| Re-analyzing a service that stays `degraded` across multiple poll cycles | Confirmed with Julio: analysis runs once per transition and is cached, not re-run every cycle. |
| LLM-driven analysis of anything other than SLO state transitions (e.g. summarizing incident_updates threads, chat-style Q&A) | Not requested; out of this feature's boundary. |

---

## Assumptions & Open Questions

Every ambiguity is resolved or recorded here - nothing is left silently unclear.

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Auto-close vs. propose-close | LLM never closes an incident directly; it writes a proposed closing comment and the incident is marked pending confirmation until owner/operator confirms | Mirrors the flapping risk that motivated `AD-19 addendum 3`'s 2-cycle hysteresis — a wrongly-closed incident is publicly visible and harder to walk back than a delayed one | y |
| LLM call failure/unconfigured at outage time | Incident is created immediately with a fixed generic description (e.g. "Interrupção detectada em {service}"); incident creation never blocks on or depends on the LLM call succeeding | Public incident visibility must never depend on a third-party AI provider's uptime | y |
| Recovery boundary that allows proposing incident closure | Only `outage` → `operational` (a full recovery) proposes closure; `outage` → `degraded` leaves the incident open | Confirmed with Julio — `degraded` is still a real problem, closing on a partial recovery would misrepresent status | y |
| Default OpenAI model when operator sets only an API key | `gpt-4o-mini` | Small, cheap, sufficient for a 1-2 sentence factual summary; confirmed with Julio | y |
| Incident granularity for simultaneous multi-service outages | One incident per service transition, never auto-grouped | Confirmed with Julio — matches the poller's existing per-service transition granularity, avoids a correlation heuristic | y |
| Tooltip re-analysis cadence while a service stays `degraded` | Generated once on the transition into `degraded`, cached until the next status change | Confirmed with Julio — avoids unbounded LLM cost scaling with time-in-degraded × service count | y |
| LLM call must not block the poll cycle for other services | The LLM call (analysis or closing-comment generation) runs detached from `pollService`'s synchronous per-service loop, with a bounded timeout; the incident/tooltip row is written with a placeholder first and updated in place once the call completes or times out | Same detached-dispatch precedent as `AD-014`'s password-reset email send — a slow/hung external call must not stall the poller's iteration over the remaining services in the cycle | n — agent's discretion, exact timeout value to be set in Design |
| Who can configure LLM provider credentials/model | `owner`/`operator` only for connect/activate (write), `viewer` included for read-only status — same role split as `/api/integrations/email/*` | Existing convention (`AD-003` roles), no reason to diverge for this integration | n — resolved by reading existing route wiring, not asked |
| Credential storage | Same `internal/crypto` secretbox encryption + master-key pattern already used for Datadog/email provider credentials — never plaintext | Existing project convention (`internal/db/migrations/0003_integrations.up.sql`, `0016_email_providers.up.sql`) | n — resolved by reading existing code, not asked |
| Idempotency of auto-created incidents | At most one open (non-resolved) incident per service at a time; a service that flaps outage→degraded→outage while already having an open incident does not create a second one | Prevents duplicate/overlapping public incidents for the same ongoing problem | n — agent's discretion, logical consequence of "one incident per service transition" |

**Open questions:** none - all resolved or logged above.

---

## User Stories

### P1: Configure an LLM provider ⭐ MVP

**User Story**: As an owner/operator of a self-hosted Vane instance, I want to connect my OpenAI API key and choose a model in Settings, so that the system can generate SLO analysis using my own account.

**Why P1**: Nothing else in this feature works without a connected, active provider — same foundational role `email_providers`/`email_settings` played for transactional email.

**Acceptance Criteria**:

1. WHEN an owner/operator submits a valid OpenAI API key THEN the system SHALL validate it against OpenAI (a real, cheap capability/models call, not a stored assumption) before persisting it, mirroring `email.Provider.ValidateCredentials`.
2. IF the submitted API key fails validation (401/403) THEN the system SHALL reject the request with a 422 and never persist the invalid key.
3. WHEN a valid API key is connected without an explicit model choice THEN the system SHALL default the active model to `gpt-4o-mini`.
4. WHEN an owner/operator selects a different supported model for the active provider THEN the system SHALL persist that choice and use it for all subsequent LLM calls.
5. The system SHALL store the API key only in encrypted form (`internal/crypto` secretbox, same as Datadog/email credentials) — never plaintext, never returned in any API response.
6. IF a `viewer`-role admin calls the connect/activate endpoints THEN the system SHALL reject with 403, consistent with `writeRoles` on `/api/integrations/email/*`.
7. WHEN no LLM provider is connected or active THEN the system SHALL behave exactly as it does today for outage/degraded transitions except for the fallback description text (AI-05/AI-08), never erroring or blocking the poll cycle.

**Independent Test**: Connect an OpenAI key with a wrong value → see 422, nothing stored. Connect a real key → status shows `connected`/`active`, `gpt-4o-mini` selected by default.

---

### P1: Outage transition auto-creates a formal incident ⭐ MVP

**User Story**: As a visitor of the public status page, I want a real outage to show up in the incidents area automatically with a plain-language description, so that I understand what happened without waiting for someone to write it up.

**Why P1**: This is the feature's core value proposition — the reason the user asked for it.

**Acceptance Criteria**:

1. WHEN `pollService` computes a service's new status as `outage` and the service's previously persisted status was not `outage` THEN the system SHALL create a new incident linked to that service, with status `investigating`, immediately and synchronously within the poll cycle — independent of whether an LLM call ever completes.
2. WHILE no LLM provider is active OR the LLM call has not yet completed/failed THEN the newly created incident's description SHALL be a fixed generic message identifying the affected service.
3. WHEN the LLM call for that transition completes successfully THEN the system SHALL update the incident with the LLM-authored description in place of the generic one.
4. IF the LLM call fails or exceeds its bounded timeout THEN the system SHALL leave the generic description in place and SHALL NOT retry automatically for that same transition.
5. IF a service already has an open (non-resolved) incident THEN a repeated `outage` transition for that same service SHALL NOT create a second incident.
6. The system SHALL NOT call the LLM more than once per state transition per service (no re-analysis while the service remains in the same status).

**Independent Test**: Force a service to `outage` in an integration test with a fake LLM provider — assert an incident row exists immediately (before the fake LLM resolves), and is updated afterward with the fake's output.

---

### P1: Degraded transition surfaces an LLM tooltip ⭐ MVP

**User Story**: As a visitor of the public status page, I want to hover over a degraded service and see a short explanation of what might be happening, so that I have more context than just the word "degraded".

**Why P1**: Explicitly requested as the first, lower-stakes half of the feature (no formal incident, just an informational tooltip).

**Acceptance Criteria**:

1. WHEN `pollService` computes a service's new status as `degraded` and the previously persisted status was not `degraded` THEN the system SHALL request an LLM analysis for that transition and cache the result against the service.
2. WHILE the cached analysis for the current `degraded` streak is not yet available (LLM pending, failed, or no provider active) THEN the public status page SHALL show the plain "degraded" label with no tooltip content — never an error or placeholder text presented as an explanation.
3. WHEN the cached analysis becomes available THEN the public status page's tooltip for that service SHALL show it on hover/focus, matching the existing hourly-history tooltip's accessibility pattern (`title` + `tabIndex`, `AD` precedent from `public-status-hourly-history`).
4. WHEN the service's status changes away from `degraded` (to `operational` or `outage`) THEN the cached analysis SHALL be cleared/superseded so a later re-entry into `degraded` generates a fresh one.
5. The system SHALL NOT call the LLM again for a service that remains `degraded` across consecutive poll cycles without an intervening status change.

**Independent Test**: Force a service to `degraded` with a fake LLM provider — assert the public status JSON carries the cached analysis text once the fake resolves, and that a second consecutive `degraded` poll makes no additional fake-provider call.

---

### P1: Recovery proposes an incident closing comment ⭐ MVP

**User Story**: As an owner/operator, I want the system to draft a closing comment when Datadog reports a service back to normal, so that I only have to review and confirm instead of writing it myself — without the incident silently closing on its own.

**Why P1**: This closes the loop the user described (Datadog reports normalized → LLM comments and helps close), with the auto-close risk mitigated per the Assumptions table.

**Acceptance Criteria**:

1. WHEN `pollService` computes a service's new status as `operational` and that service has an open incident THEN the system SHALL request an LLM-authored closing comment and attach it to the incident as a pending proposal, without changing the incident's status.
2. WHILE a proposed closing comment is pending THEN the incident SHALL remain visibly open (`investigating`/current status) on both the admin dashboard and the public incidents area.
3. WHEN an owner/operator confirms the proposed closing comment THEN the system SHALL append it as a normal `incident_update`, set the incident's status to resolved, and set `resolved_at`.
4. WHEN an owner/operator instead edits or discards the proposal THEN the system SHALL let them close the incident through the existing manual incident-update/resolve flow, unaffected by this feature.
5. IF the LLM call for the closing comment fails or times out THEN the system SHALL leave the incident open with no proposal, requiring the existing manual close flow — recovery detection itself (service status flips to `operational`) SHALL NOT be blocked or delayed by the failed call.
6. IF a `viewer`-role admin attempts to confirm a proposed closing comment THEN the system SHALL reject with 403.

**Independent Test**: Open an incident, force the service back to `operational` with a fake LLM provider, confirm the proposal via the API, assert the incident is `resolved` with the LLM text as its final `incident_update`.

---

### P2: Adapter groundwork for a second provider

**User Story**: As the project maintainer, I want the LLM integration built behind a provider-agnostic interface, so that adding Gemini or another provider later is a connector-only change.

**Why P2**: Explicitly requested ("fácil expandir"), but only OpenAI ships now — this story is about the shape of the code, not new user-facing behavior.

**Acceptance Criteria**:

1. The system SHALL define an `llm.Provider`-shaped interface (mirroring `email.Provider`) that all analysis/description-generation call sites depend on, never a concrete OpenAI client type.
2. WHERE a second provider implementation exists in the future THEN it SHALL be addable by implementing that interface and registering it in the same `ProviderFactory`-style switch used for email providers — no changes required to the poller, incident, or tooltip logic.

**Independent Test**: Code review — confirm no file outside `internal/connectors/openai` and the factory wiring imports an OpenAI-specific type.

---

## Edge Cases

- IF the connected OpenAI API key is later revoked/expired (calls start failing) THEN the system SHALL mark the LLM integration `invalid` (mirroring `IntegrationRepository.MarkDatadogInvalid`) after a failed call, fall back to generic incident descriptions and no tooltips, and continue polling/creating incidents normally.
- IF a service has no SLO transition at all (stays `operational` the whole time) THEN the system SHALL make zero LLM calls for that service.
- IF two different services transition to `outage` in the same poll cycle THEN the system SHALL create two independent incidents (AI-Granularity assumption).
- WHEN the LLM provider is disconnected (credentials removed) while an analysis or closing-comment call is in flight THEN the system SHALL treat the in-flight call's result as best-effort — if it still completes, apply it; if the provider row is gone, discard the result and keep the fallback content.
- IF the LLM returns an empty or clearly malformed response THEN the system SHALL treat it the same as a failed call (fallback description / no tooltip / no closing proposal), never publish empty or garbled text.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| AI-01 | P1: Configure an LLM provider | Design | Implementing |
| AI-02 | P1: Configure an LLM provider | Design | Implementing |
| AI-03 | P1: Configure an LLM provider | Design | Implementing |
| AI-04 | P1: Configure an LLM provider | Design | Implementing |
| AI-05 | P1: Configure an LLM provider | Design | Implementing |
| AI-06 | P1: Configure an LLM provider | Design | Implementing |
| AI-07 | P1: Configure an LLM provider | Design | Implementing |
| AI-08 | P1: Outage transition auto-creates a formal incident | Design | Implementing |
| AI-09 | P1: Outage transition auto-creates a formal incident | Design | Implementing |
| AI-10 | P1: Outage transition auto-creates a formal incident | Design | Implementing |
| AI-11 | P1: Outage transition auto-creates a formal incident | Design | Implementing |
| AI-12 | P1: Outage transition auto-creates a formal incident | Design | Implementing |
| AI-13 | P1: Outage transition auto-creates a formal incident | Design | Implementing |
| AI-14 | P1: Degraded transition surfaces an LLM tooltip | Design | Implementing |
| AI-15 | P1: Degraded transition surfaces an LLM tooltip | Design | Implementing |
| AI-16 | P1: Degraded transition surfaces an LLM tooltip | Design | Implementing |
| AI-17 | P1: Degraded transition surfaces an LLM tooltip | Design | Implementing |
| AI-18 | P1: Degraded transition surfaces an LLM tooltip | Design | Implementing |
| AI-19 | P1: Recovery proposes an incident closing comment | Design | Implementing |
| AI-20 | P1: Recovery proposes an incident closing comment | Design | Implementing |
| AI-21 | P1: Recovery proposes an incident closing comment | Design | Implementing |
| AI-22 | P1: Recovery proposes an incident closing comment | Design | Implementing |
| AI-23 | P1: Recovery proposes an incident closing comment | Design | Implementing |
| AI-24 | P1: Recovery proposes an incident closing comment | Design | Implementing |
| AI-25 | P2: Adapter groundwork for a second provider | Design | Implementing |
| AI-26 | P2: Adapter groundwork for a second provider | Design | Implementing |

**ID format:** `AI-[NUMBER]`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 26 total, 26 mapped to tasks (T1-T25), 0 unmapped — all `Implementing` as of T25 (Batch 4/4); final AC-by-AC `Verified` stamp is the independent Verifier's job, not this task's.

---

## Success Criteria

How we know the feature is successful:

- [ ] An owner/operator can connect OpenAI and see it active within the same Settings flow shape as email providers, with no plaintext key ever visible after submit.
- [ ] A real outage always produces a visible public incident within one poll cycle, with or without a working LLM call.
- [ ] A real degradation shows an informative tooltip once the LLM call succeeds, with zero duplicate calls while the status doesn't change.
- [ ] No incident is ever auto-closed without an explicit owner/operator confirmation.
- [ ] Adding a second provider later touches only a new connector package + one factory switch case — confirmed by code review, not a runtime test.
