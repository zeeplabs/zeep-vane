# Service Edit Specification

## Problem Statement

The "Serviços monitorados" table (`ServiceListPage.tsx`) has no way to rename a service after creation — `ServicesHandler`/`ServiceRepository` only expose `Create`/`Get`/`List`/`ListPaginated` (no `Update`). A misnamed service today has no fix short of deleting and recreating it (and delete doesn't exist yet either — see the separate `service-delete` feature).

## Goals

- [ ] Admin (owner) can rename an existing service from the services list, without touching monitoring configuration.
- [ ] Zero risk to in-flight poller state: renaming never changes `monitor_mode`, `slo_id`, `poll_target`/`poll_type`/`poll_interval_seconds`, or `current_status`.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Changing `slo_id` / `poll_target` / `poll_type` / `poll_interval_seconds` after creation | Requires resetting in-memory hysteresis counters (`breachStreak` in `internal/poller/poller.go`, `failureStreak` in `internal/poller/manual_scheduler.go`) and, for polling-mode, restarting that service's long-lived goroutine — real new engineering work the user explicitly deferred to a future feature. |
| Switching `monitor_mode` (SLO ↔ polling) on an existing service | Same risk as above, compounded — different DB columns populated per mode (migration `0034_service_polling_mode`). Deferred. |
| Bulk rename / rename via API for external tooling | No stated need; single-service inline rename only. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Editable fields | `name` only | User confirmed via discuss: MVP-safe scope, no poller/hysteresis risk. | y |
| Authorization | `ownerOnly` | User confirmed via discuss: treat service mutation as higher-risk, same tier as admin/tenant delete. | y |
| Name validation | Same rule as `Create` (non-empty) — no additional uniqueness constraint, since `Create` itself doesn't enforce unique names | `services.name` has no `UNIQUE` constraint in migration `0004_services.up.sql`; adding one now is a schema change outside this feature's stated scope | n/a (inferred from existing schema, not a product decision) |
| Endpoint shape | `PATCH /api/services/{id}` with `{"name": "..."}` body | Matches the codebase's existing PATCH-for-partial-update convention (`PATCH /api/status-pages/{id}/services`, `PATCH /api/status-pages/{id}/domain`) | n/a (codebase convention, not a product decision) |
| Empty name | 422 with the same fixed generic body style as `Create`'s `invalidServiceRequestBody` | Consistency with `Create`'s existing validation error contract (AGENTS.md §4: never leak raw errors) | n/a (codebase convention) |
| UI entry point | Row-level action (kebab menu / edit icon) opening a small inline-edit or a minimal drawer for just the name field | No existing per-row action menu exists in `ServiceListPage.tsx` today; this feature introduces the first one, which `service-delete` will also use | y (declared during discuss as shared UI groundwork) |

**Open questions:** none — all resolved or logged above.

---

## User Stories

### P1: Rename a monitored service ⭐ MVP

**User Story**: As an owner, I want to rename an existing monitored service so that a typo or outdated name doesn't stay wrong forever.

**Why P1**: Only story in this feature; it is the entire MVP.

**Acceptance Criteria**:

1. WHEN an owner sends `PATCH /api/services/{id}` with a non-empty `name` THEN the system SHALL update that service's `name` column and leave `monitor_mode`, `slo_id`, `slo_name`, `poll_type`, `poll_target`, `poll_interval_seconds`, `current_status`, and `last_status_change_at` unchanged.
2. WHEN the rename succeeds THEN the system SHALL respond `200` with the full updated `serviceResponse` body (same shape as `GET /api/services/{id}`'s `serviceResponse` fields).
3. IF the request body's `name` is empty or absent THEN the system SHALL respond `422` with a fixed generic error body and SHALL NOT modify the row.
4. IF `{id}` does not match any existing service THEN the system SHALL respond `404` with a fixed generic error body (`serviceNotFoundBody`, same as `GET`), never leaking whether the ID is malformed vs. absent.
5. IF the requester's role is not `owner` THEN the system SHALL respond `403` and SHALL NOT modify the row.
6. WHEN a rename succeeds THEN the frontend `ServiceListPage` SHALL reflect the new name without a full page reload (react-query cache invalidation/refetch of the current page).

**Independent Test**: Create a service, PATCH its name, confirm `GET /api/services/{id}` returns the new name and every other field byte-identical to before, and confirm the list page shows the new name after the edit UI closes.

---

## Edge Cases

- IF `name` is only whitespace THEN the system SHALL treat it as empty (422) — same trim-and-check the frontend already does for other name fields in this codebase, applied server-side as the authoritative check.
- WHEN the same name is submitted unchanged THEN the system SHALL still return `200` (idempotent no-op update), not an error.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| SVCEDIT-01 | P1: Rename a monitored service | Implementing | Implementing |
| SVCEDIT-02 | P1: Rename a monitored service | Implementing | Implementing |
| SVCEDIT-03 | P1: Rename a monitored service | Implementing | Implementing |
| SVCEDIT-04 | P1: Rename a monitored service | Implementing | Implementing |
| SVCEDIT-05 | P1: Rename a monitored service | Implementing | Pending |
| SVCEDIT-06 | P1: Rename a monitored service | Implementing | Pending |

**Coverage:** 6 total, 4 mapped to tasks (repository layer), 2 unmapped (handler/frontend) ⚠️

---

## Success Criteria

- [ ] Owner can rename a service from the UI in under 10 seconds, with no page reload.
- [ ] Zero regressions: full backend + frontend gate green, existing `Create`/`List`/`Get` behavior byte-for-byte unchanged.
