# Incidentes Page Specification

## Problem Statement

The redesigned Incidentes screen (`handoff-new-layout/Incidentes.dc.html`, `.specs/features/new-layout-migration/gap-analysis.md` item 5) shows a severity badge per incident, a create drawer with a severity picker and initial description, and a timeline where each entry is attributed to a human author or rendered as a distinct AI-generated closing summary. The backend already supports all of this — `incident-severity-and-timeline` (Verified) added `severity` to `Incident`, `author_id`/`is_ai_summary` to `IncidentUpdate`, and an optional `description` on create — but `IncidentsPage.tsx` never reads any of it: `Incident`/`IncidentUpdate` types omit the fields, the create form has no severity or description input, and rendered updates show only body+timestamp with no author/AI distinction. This is the same shape of gap as `poller-status-page` and `dashboard-integrations-page`: backend already returns the data, frontend never consumes it.

## Out of Scope

| Item | Reason |
| --- | --- |
| Rebuilding the timeline as a full detail drawer matching the mock's modal layout | Current page uses an inline expand-in-card pattern for the active-incident timeline (`ActiveIncidentCard`), not a drawer. Swapping the interaction model is a visual-parity concern, not a data-consumption gap — follows the same two-step pattern used for `poller-status-page`/`dashboard-integrations-page` (data first, then a mock-fidelity pass if requested). |
| `PATCH /api/incidents/{id}/severity` (INCSEV-04, change severity after creation) | Backend endpoint exists and is wired, but the mock's detail view has no severity-change control — same conclusion the backend spec itself already reached ("mock's detail drawer doesn't show a severity control today"). Not needed to close this screen's gap. |
| Severity affecting sort order, filtering, or SLA/escalation | Not in the mock; `incident-severity-and-timeline` explicitly scoped severity as display-only for now. |
| Severity for auto-created incidents beyond the existing default | `incident-severity-and-timeline` already decided this (no inference from SLO data); this page just renders whatever severity the incident carries. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Severity badge visual mapping | `minor` → `neutral-outline` Tag, `moderate` → `warning` Tag, `critical` → `critical` Tag | Matches the mock's `SEVERITY_META` color intent (neutral grey / amber / red) using the app's existing `Tag` variants — no new variant needed. | Assumed, low-risk (pure display mapping, matches existing token system) |
| Severity picker in create drawer | 3-way toggle (button group), default `moderate`, required before submit | Mirrors the mock's `severityMinorStyle`/`severityModerateStyle`/`severityCriticalStyle` segmented control; backend requires `severity` on create (`422` if missing/invalid) so the frontend must always send one. | Assumed from backend contract (non-negotiable — omitting `severity` fails create) |
| Description field in create drawer | Optional multi-line `textarea`-style input added below the service picker | Matches the mock's "descrição inicial" field; backend already treats it as optional (`INCSEV-03`/edge case: empty string stored as `NULL`). | Assumed from mock + backend contract |
| Timeline entry attribution display | Human update (`author_id` set, `is_ai_summary: false`): "Equipe" label, no name lookup. AI-generated entry (`is_ai_summary: true`): render as visually distinct (accent-tinted) with the label "Resumo gerado por IA", no author name shown. | The mock hardcodes author display names (`'Ana Silva'`, `'Sistema'`) that don't exist as real data — the app has no admin-name-by-ID lookup wired into this list endpoint. Fetching real names would require a new N+1 lookup or backend join, out of this Medium spec's scope. Confirmed via `AskUserQuestion`: generic label over fabricating/fetching names. | y |
| Manually-authored update where `author_id` is present but not resolvable to a display name | Same "Equipe" fallback as above (not "Sistema" — that label is reserved for entries with no author at all, which today only exist as pre-existing mock data, not real backend output) | Backend never produces `author_id: null, is_ai_summary: false` today (`incident-severity-and-timeline`'s Assumptions: "reserved, unused today") — every real non-AI update has an `author_id`. The fallback exists for forward-compatibility, not because the app expects to need it now. | Assumed |

**Open questions:** none — author-name-fallback confirmed via `AskUserQuestion` (generic "Equipe" label, no `/api/admins` lookup).

---

## User Stories

### P1: Severity is visible on every incident ⭐ MVP

**User Story**: As an owner/operator/viewer, I want to see each incident's severity at a glance, so I can triage by impact without opening it.

**Why P1**: The mock shows a severity badge on every list row (active and resolved); without reading `severity` from the API this can't render at all.

**Acceptance Criteria**:

1. WHEN the incidents list renders an active or resolved incident THEN the system SHALL show a severity badge using the label/color mapping (Menor=neutral, Moderado=warning, Crítico=critical).
2. The `Incident` TypeScript type SHALL include `severity: "minor" | "moderate" | "critical"`.

**Independent Test**: seed an incident with `severity: "critical"`, render the list, assert a Tag with `critical` styling and label "Crítico" is present for that row.

---

### P1: Creating an incident sets severity and optional description ⭐ MVP

**User Story**: As an owner/operator, I want to pick a severity and optionally describe the incident when I create it, so the team has triage context from the start.

**Why P1**: `POST /api/incidents` rejects creation with `422` if `severity` is missing/invalid (`INCSEV-02`) — the create drawer cannot function without sending one.

**Acceptance Criteria**:

1. WHEN the create-incident drawer opens THEN the system SHALL default the severity picker to "Moderado" and SHALL NOT allow submission with no severity selected.
2. WHEN the operator selects "Menor"/"Moderado"/"Crítico" THEN the system SHALL send the corresponding `severity` value (`minor`/`moderate`/`critical`) in the `POST /api/incidents` body.
3. WHEN the operator types text into the description field and submits THEN the system SHALL send it as `description` in the request body.
4. WHEN the operator leaves the description field empty and submits THEN the system SHALL omit or send an empty `description`, matching the backend's NULL-on-empty convention (no frontend-side validation blocks empty description).
5. WHEN the create request succeeds THEN the drawer SHALL reset the severity picker to its default and clear the description field, consistent with existing title/service-picker reset behavior.

**Independent Test**: open the create drawer, select "Crítico", type a description, submit; assert the POST body includes `severity: "critical"` and the typed `description`; repeat with description left blank and assert the incident is still created.

---

### P1: Timeline entries show who wrote them ⭐ MVP

**User Story**: As an owner/operator/viewer, I want to see whether an update came from a teammate or was AI-generated, so I can trust the timeline's provenance.

**Why P1**: The mock visually distinguishes the AI-generated closing summary (accent-tinted card, "Resumo gerado por IA" label) from regular updates; `IncidentUpdate` already carries `author_id`/`is_ai_summary` but the type and rendering ignore both today.

**Acceptance Criteria**:

1. The `IncidentUpdate` TypeScript type SHALL include `author_id: string | null` and `is_ai_summary: boolean`.
2. WHEN an update has `is_ai_summary: true` THEN the system SHALL render it visually distinct from regular entries (accent-tinted background/border) with the label "Resumo gerado por IA".
3. WHEN an update has `is_ai_summary: false` and a non-null `author_id` THEN the system SHALL render it as a regular timeline entry with an "Equipe" author label (per the Assumptions table — no name-resolution endpoint exists).
4. WHEN an update has `is_ai_summary: false` and a null `author_id` THEN the system SHALL render the author label as "Sistema".

**Independent Test**: seed an incident with 3 updates — one `is_ai_summary: true`, one `author_id` set + `is_ai_summary: false`, one `author_id: null` + `is_ai_summary: false` — expand the timeline and assert each renders with its distinct label/styling.

---

## Edge Cases

- IF an incident's `severity` value is somehow outside the three known values (should never happen given backend validation, but defensively) THEN the system SHALL fall back to rendering it as `neutral-outline` with the raw string, rather than crashing.
- WHEN the create drawer is reopened after a prior submission THEN the severity picker SHALL show "Moderado" (the default), not the previously-selected value.
- WHEN a resolved incident has zero timeline updates (only possible for pre-severity-feature legacy data, not new incidents) THEN the timeline section SHALL render its existing empty-safe behavior (no crash on an empty array).

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| INCPG-01 | P1: Severity visible on every incident | Execute | Verified |
| INCPG-02 | P1: Severity visible on every incident (type) | Execute | Verified |
| INCPG-03 | P1: Creating an incident sets severity/description (default+required) | Execute | Verified |
| INCPG-04 | P1: Creating an incident sets severity/description (severity sent) | Execute | Verified |
| INCPG-05 | P1: Creating an incident sets severity/description (description sent) | Execute | Verified |
| INCPG-06 | P1: Creating an incident sets severity/description (empty description) | Execute | Verified |
| INCPG-07 | P1: Creating an incident sets severity/description (reset after success) | Execute | Verified |
| INCPG-08 | P1: Timeline entries show who wrote them (type) | Execute | Verified |
| INCPG-09 | P1: Timeline entries show who wrote them (AI styling) | Execute | Verified |
| INCPG-10 | P1: Timeline entries show who wrote them (author label) | Execute | Verified |
| INCPG-11 | P1: Timeline entries show who wrote them (system label) | Execute | Verified |

**Coverage:** 11 total, 11 mapped to tasks, 0 unmapped (Medium scope — tasks implicit in Execute, no formal `tasks.md`)

---

## Success Criteria

- [x] `Incident`/`IncidentUpdate` types carry `severity`/`author_id`/`is_ai_summary`.
- [x] Every incident row (active or resolved) shows a severity badge.
- [x] Create drawer has a required severity picker (default Moderado) and an optional description field, both wired into `POST /api/incidents`.
- [x] Timeline entries visually distinguish AI-generated summaries from human/system updates.

