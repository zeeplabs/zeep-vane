# Incident Severity and Timeline Attribution Specification

## Problem Statement

The redesigned Incidentes screen (`handoff-new-layout/Incidentes.dc.html`, tracked in `.specs/features/new-layout-migration/gap-analysis.md`) shows a severity badge per incident and a timeline of updates attributed to an author (human or AI). Today `incidents` has no `severity` column, `incident_updates` has no way to say who wrote an entry, and manual incident creation cannot record an initial description even though the mock's create drawer asks for one. The AI-assisted closing flow (`ConfirmPendingClose`) already appends its summary as a normal `incident_updates` row — that part needs no new plumbing, only a way to mark it as AI-authored so the UI can render it differently.

## Goals

- [ ] Every incident carries a `severity` (`minor`/`moderate`/`critical`), settable at creation and changeable afterward.
- [ ] Every `incident_updates` row is attributable: a human author (the admin who wrote it) or the system/AI (no human author).
- [ ] The AI-generated closing summary already written by `ConfirmPendingClose` is distinguishable from a manually-written update.
- [ ] Manual incident creation accepts an optional initial description.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Editing/deleting an existing `incident_updates` entry | Not requested; timeline is append-only today and stays that way. |
| Severity affecting SLA timers, escalation, or notification routing | No such mechanism exists yet in this codebase; this spec only adds the field and its CRUD surface. |
| Severity on auto-created (`AutoCreated`) incidents from `SLOAnalyzer` | `SLOAnalyzer` has no signal today to infer severity from an SLO breach; auto-created incidents get the same default as any other incident and an owner/operator can change it same as always. Inferring severity from SLO data is a future feature. |
| Backfilling severity on pre-existing incidents with anything but the default | No production data exists yet (confirmed with the user) — a flat default for all pre-migration rows is sufficient. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Severity values | `minor` \| `moderate` \| `critical` (DB CHECK constraint, mirrors the `incidents.status` pattern already in use) | Matches the mock's three options (Menor/Moderado/Crítico) 1:1; labels are a frontend i18n concern, not a backend enum concern. | y |
| Severity column nullability | `NOT NULL`, `DEFAULT 'moderate'` | No production data exists yet (user confirmed) — safe to make it mandatory outright instead of designing around a backfill. New incidents must pass an explicit value; the column default only exists so the migration itself doesn't fail on any pre-existing dev/test rows. | y |
| Severity mutability | Editable after creation, via a dedicated endpoint (not folded into `Transition`, which is status-only and validates against `validIncidentStatuses`) | User decision: severity can change later. Keeping it a separate `PATCH /api/incidents/{id}/severity` endpoint/repo method matches the codebase's existing pattern of one narrow method per state change (`SetPendingCloseComment`, `DiscardCloseProposal`) rather than overloading `Transition`. | y |
| `incident_updates` authorship shape | Two new nullable columns: `author_id UUID NULL REFERENCES users(id)` and `is_ai_summary BOOLEAN NOT NULL DEFAULT false`. `author_id NULL` + `is_ai_summary = true` = AI-authored; `author_id NULL` + `is_ai_summary = false` = system-authored with no human (reserved, unused today); `author_id` set = the human who wrote it. | Mirrors the mock's timeline ("time + author + text") while keeping the AI-summary flag independent of authorship — an AI entry has no human author by definition, but the flag is what the UI actually branches on for styling (purple-tinted card vs. plain entry), not the presence/absence of `author_id` alone. | y |
| Who is `author_id` on a manual `AddUpdate` call | The authenticated actor from `UserFromContext(r.Context())`, same pattern already used by `DomainsHandler.Delete` for audit records | `AddUpdate`'s route is `writeRoles`-gated (`internal/cli/routes.go:179`), so an authenticated owner/operator is always present; no new auth plumbing needed. | y |
| `ConfirmPendingClose`'s insert | `author_id = NULL`, `is_ai_summary = true` | The comment text itself is LLM-drafted (AI-19/AI-20); the human only confirms it, they don't author it. | y |
| Initial description on manual creation | `createIncidentRequest` gains an optional `description` field; when present, `Create` persists it the same way `SetDescription` does for auto-created incidents | Closes the gap between the mock's create drawer ("descrição inicial") and today's `POST /api/incidents`, which only accepts `title`+`service_ids`. Reuses the existing `Incident.Description` column — no schema change needed for this part. | y |
| RBAC on the new severity-update endpoint | `writeRoles` (owner/operator), same as every other incident mutation | Consistent with `Create`/`Transition`/`AddUpdate` already being `writeRoles`-gated. | y |

**Open questions:** none — all resolved or logged above.

---

## User Stories

### P1: Admin sets severity when creating an incident ⭐ MVP

**User Story**: As an owner/operator, I want to set a severity when I open a manual incident, so the team can triage by impact at a glance.

**Why P1**: The mock's create drawer requires this field; without it the new Incidentes screen cannot be built.

**Acceptance Criteria**:

1. WHEN an owner/operator submits `POST /api/incidents` with `severity` set to `"minor"`, `"moderate"`, or `"critical"` THEN the system SHALL create the incident with that severity and respond `201` with it in the body.
2. IF `POST /api/incidents` omits `severity` or sends a value outside the three allowed THEN the system SHALL respond `422` with an error body and SHALL NOT create the incident.
3. WHEN an owner/operator submits `POST /api/incidents` with a non-empty `description` THEN the system SHALL persist it as the incident's `Description`, identically to how `SetDescription` stores it for auto-created incidents.
4. The system SHALL treat `description` on create as optional — an empty or absent value creates the incident with `Description` unset (`NULL`), unchanged from today's behavior.

**Independent Test**: `POST /api/incidents` with `{title, service_ids, severity: "critical", description: "..."}`, confirm `201` and the response echoes `severity` and `description`; repeat with a missing/invalid `severity` and confirm `422` with no row created.

---

### P2: Admin changes an incident's severity after creation

**User Story**: As an owner/operator, I want to re-classify an incident's severity as I learn more, so the badge stays accurate without needing to recreate the incident.

**Why P2**: Useful but not required for the create-and-list flows to work; the mock's detail drawer doesn't show a severity control today, so this is additive surface, not a rendering blocker.

**Acceptance Criteria**:

1. WHEN an owner/operator sends `PATCH /api/incidents/{id}/severity` with a valid severity value THEN the system SHALL update the incident's severity and respond `200` with the updated incident.
2. IF the request body's severity is outside the three allowed values THEN the system SHALL respond `422` and SHALL NOT change the stored severity.
3. IF `{id}` does not match an existing incident THEN the system SHALL respond `404`.
4. WHILE an incident is in any status (including `resolved`) the system SHALL allow its severity to be changed — severity is independent of the status state machine.

**Independent Test**: Create an incident with `severity: "minor"`, `PATCH .../severity` with `"critical"`, `GET` the incident and confirm the new value; repeat against a non-existent ID and confirm `404`.

---

### P1: Timeline entries are attributable to a human or the AI ⭐ MVP

**User Story**: As an owner/operator, I want to see who wrote each incident update — a teammate or the AI-generated closing summary — so I can trust the timeline's provenance.

**Why P1**: The mock's timeline explicitly shows "time + author + text" per entry and renders the AI summary in a distinct purple-tinted card; neither is possible without this attribution.

**Acceptance Criteria**:

1. WHEN an owner/operator calls `POST /api/incidents/{id}/updates` THEN the system SHALL record the authenticated actor's user ID as the update's `author_id` and SHALL set `is_ai_summary` to `false`.
2. WHEN `ConfirmPendingClose` appends the pending close comment to the timeline THEN the system SHALL record that entry with `author_id = NULL` and `is_ai_summary = true`.
3. WHEN an incident's timeline is read via `GET /api/incidents/{id}/updates` or the response of `POST /api/incidents/{id}/updates` THEN the system SHALL include `author_id` (nullable) and `is_ai_summary` for every entry.
4. The system SHALL NOT require a human-authored update to carry any value in `author_id` other than the actual authenticated actor — no update is ever created without either a resolved `author_id` or `is_ai_summary = true`.

**Independent Test**: As an authenticated operator, add a manual update and confirm the returned entry has `author_id` = that operator's ID and `is_ai_summary: false`; separately, run the confirm-close flow on an incident with a pending proposal and confirm the resulting timeline entry has `author_id: null` and `is_ai_summary: true`.

---

## Edge Cases

- IF `POST /api/incidents` sends `severity` as an empty string THEN the system SHALL treat it the same as an invalid value (`422`), not as "use the default".
- IF `PATCH /api/incidents/{id}/severity` is called with the incident's current severity (no-op change) THEN the system SHALL still respond `200` — this is not treated as an error.
- WHEN `description` is sent as an empty string on `POST /api/incidents` THEN the system SHALL store `NULL`, not an empty string, matching `SetDescription`'s existing NULL/absent convention elsewhere in the codebase.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| INCSEV-01 | P1: Admin sets severity when creating an incident | Execute | Verified |
| INCSEV-02 | P1: Admin sets severity when creating an incident | Execute | Verified |
| INCSEV-03 | P1: Admin sets severity when creating an incident (description) | Execute | Verified |
| INCSEV-04 | P2: Admin changes severity after creation | Execute | Verified |
| INCSEV-05 | P1: Timeline entries attributable | Execute | Verified |
| INCSEV-06 | P1: Timeline entries attributable (AI entry) | Execute | Verified |

**Coverage:** 6 total, 6 mapped to tasks, 0 unmapped (Medium scope — tasks were implicit in Execute, not a formal `tasks.md`)

---

## Success Criteria

- [x] `POST /api/incidents` requires and persists `severity`; rejects invalid values with `422`.
- [x] `PATCH /api/incidents/{id}/severity` lets an owner/operator change severity on an incident in any status.
- [x] Every `incident_updates` row (manual or AI-generated) carries `author_id`/`is_ai_summary` and both are visible in the API response.
- [x] `POST /api/incidents` accepts an optional `description` and persists it identically to `SetDescription`.
