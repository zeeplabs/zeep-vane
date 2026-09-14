# Monitored Services Page Specification

## Problem Statement

`new-layout-migration` item 3 ("Serviços Monitorados") redesigns the existing `/services` screen to match `handoff-new-layout/Servicos Monitorados.dc.html`: a filterable/searchable service list with per-row status/uptime/last-check, and a detail drawer with 30d stats, a degraded-reason note, and a 24-hour status history strip. `gap-analysis.md` already scoped this screen down to SLO-based monitoring only (Datadog) — manual HTTP/TCP/Ping polling is explicitly out of scope, a future feature.

## Goals

- [ ] Admin can see every monitored service's current status, 30d uptime, and last-checked time in one list, filterable by status and searchable by name.
- [ ] Admin can open a service to see 30d uptime, last-checked time, 30d incident count, a degraded-reason note (when present), and a 24-hour status history strip.
- [ ] Admin can add a new service backed by a Datadog SLO from this screen (existing `POST /api/services` + `GET /api/integrations/datadog/slos`), without leaving the page.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Manual polling (HTTP/TCP/Ping) as a monitoring mode | `gap-analysis.md` decision (2026-09-10): hidden this cycle, no scheduler/model exists, future feature with its own spec. |
| New Relic as an SLO source | Only Datadog is an integrated SLO provider (`internal/connectors/datadog`); the mock's "New Relic" chip has no backing integration. |
| Per-service latency (table column + drawer stat) | Confirmed: Datadog's SLO history response (`internal/connectors/datadog/client.go` `SLOStatus`) carries no latency field — nothing to source it from without a new connector endpoint. User decision (this session): hide the column/stat entirely this cycle rather than build new Datadog integration surface. |
| "Pausar monitoramento" / "Editar configuração" (drawer actions) | No enable/disable flag or update endpoint exists on `Service` today (only `Create`/`ListPaginated`). User decision (this session): hide both buttons; the drawer is read-only this cycle. Pause/edit become their own future spec. |
| Deleting a service | Not in the mock, no existing endpoint; unchanged from today. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Latency column/stat | Hidden entirely (table + drawer) | No data source exists (see Out of Scope); a fabricated value would be worse than absence. | y |
| Drawer "Pausar monitoramento" / "Editar configuração" buttons | Hidden; drawer is read-only | No backend support exists; adding it inflates this cycle's scope. | y |
| `not_configured` status (SLO linked, poller hasn't reported yet) | 4th filter chip "Não configurado" + its own neutral badge color, on top of the mock's 3 (Operacional/Degradado/Inativo) | The mock only models 3 states; forcing `not_configured` into one of them would misrepresent a real, common state (every freshly-added service starts here). | y |
| "Últimas 24 verificações" drawer strip | 24 local-hour buckets via `internal/history.BuildBuckets(intervals, now, asOf, loc, 24, time.Hour)` — same function and rule (worst-status-wins, last-seen-vs-poller-staleness via `asOf`) already used by `public-status-hourly-history` | Reuses a already-tested, already-decided bucketing rule instead of inventing a second one; "24 verificações" in the mock is cosmetic copy for what is really the last 24 hours. | y |
| Uptime 30d (table + drawer) | `internal/history.UptimePercent(intervals, windowStart, now)` per service, same helper `OverviewHandler` already uses for `uptime_avg_30d` | Existing, tested helper; avoids a second uptime formula in the codebase. | y |
| "Última verificação" (table + drawer) | The service's open `StatusInterval.LastSeenAt` (falls back to `LastStatusChangeAt` if no open interval — i.e. `not_configured` with zero polls yet) | `LastSeenAt` is the poller's last confirmed read for the service; `LastStatusChangeAt` only moves on a status *change*, not on every poll, so it under-represents freshness once implemented but is the only value available before the first poll. | y |
| Incidentes (30d) (drawer stat) | New repository method counting incidents linked (via `incident_services`) to the service, `created_at >= now-30d`, any status | Mirrors `IncidentRepository.CountOpenedResolvedBetween`'s existing time-window pattern; needed because no such per-service count exists today (confirmed gap). | y |
| "Fonte" / SLO picker in Add-service drawer | Datadog only (no source chips) — search box hitting the existing `GET /api/integrations/datadog/slos?query=` | Only integration that exists; showing a "Fonte" selector with one option is dead UI. | y |
| Service row subtext (mock shows a URL, e.g. `api.acme.health`) | Show the linked Datadog SLO name instead (`Service.SLOID`'s human name, resolved via the same SLO search/lookup used at creation, or stored at creation time — see Design) | SLO-based services have no URL/host concept; the SLO's own name is the closest real analog to "what am I monitoring." | y |
| List pagination | Keep `GET /api/services` paginated (20/page, already `PAG-08`) and add the shared `Pager` component, even though the mock shows no pager | `AGENTS.md` §5 hard rule: every list screen backed by a paginated endpoint uses `Pager`; status filter chips and counts operate within the current page like the mock's static 6-row example, not scoped to "no pager" cosmetics of a design mock built before pagination existed for this screen. |

**Open questions:** none — all resolved above.

---

## User Stories

### P1: See monitored services at a glance ⭐ MVP

**User Story**: As an admin, I want to see every monitored service's status, uptime, and last-check time in one list, so I can quickly spot what's unhealthy.

**Why P1**: Core value of the screen — matches Overview's "reach the data" bar for MVP.

**Acceptance Criteria**:

1. WHEN an authenticated admin (any role) navigates to `/services` THEN the system SHALL render a table with columns Status, Serviço (name + SLO name), Uptime 30d, Última verificação, for every service on the current page.
2. WHEN a service's `CurrentStatus` is `operational` THEN the system SHALL render its status badge as "Operacional" (green).
3. WHEN a service's `CurrentStatus` is `degraded` THEN the system SHALL render its status badge as "Degradado" (amber).
4. WHEN a service's `CurrentStatus` is `outage` THEN the system SHALL render its status badge as "Inativo" (red).
5. WHEN a service's `CurrentStatus` is `not_configured` THEN the system SHALL render its status badge as "Não configurado" (neutral gray).
6. WHEN a service has no open `StatusInterval` (never polled) THEN the system SHALL render its Uptime 30d and Última verificação as "—" instead of a computed value.
7. The system SHALL NOT render a Latência column or field anywhere on this screen.
8. WHEN the list has more services than fit one page THEN the system SHALL render the shared `Pager` component with `totalPages = Math.max(1, Math.ceil(total / page_size))`.

**Independent Test**: Seed 3+ services with different `CurrentStatus` values (including one `not_configured`), load `/services`, confirm the table renders every row with the right badge/label and no Latência column.

---

### P2: Filter and search the service list

**User Story**: As an admin, I want to filter by status and search by name/SLO, so I can find a specific service or focus on unhealthy ones.

**Why P2**: Directly in the mock, materially improves usability once the list grows past a handful of services, but the page is usable without it.

**Acceptance Criteria**:

1. WHEN the admin clicks a status filter chip (Todos/Operacional/Degradado/Inativo/Não configurado) THEN the system SHALL show only services on the current page matching that status, and SHALL show all of them when "Todos" is selected.
2. WHEN the admin types into the search box THEN the system SHALL show only services on the current page whose name or SLO name contains the typed text, case-insensitively.
3. WHILE both a status filter and a search query are active THEN the system SHALL show only services matching both conditions (AND, not OR).
4. Each filter chip SHALL display a count of matching services scoped to the current page's results.
5. WHEN no service on the current page matches the active filter/search combination THEN the system SHALL render the empty state "Nenhum serviço encontrado com esses filtros."

**Independent Test**: With services in at least 2 different statuses, click each chip and confirm row counts change accordingly; type a name substring and confirm only matches remain.

---

### P3: Inspect a service's detail drawer

**User Story**: As an admin, I want to click a service row to see its 30d stats, a degraded-reason note when relevant, and its recent status history, so I can understand what's going on before digging into Datadog.

**Why P3**: Adds depth beyond the list but the list alone (P1/P2) already delivers the screen's core value; independently testable and shippable after P1/P2.

**Acceptance Criteria**:

1. WHEN the admin clicks a service row THEN the system SHALL open a drawer showing that service's Uptime 30d, Última verificação, and Incidentes (30d).
2. WHEN the service's `CurrentStatus` is `degraded` AND it has a non-empty `StatusAnalysis` THEN the drawer SHALL render that text as a note.
3. IF the service's `CurrentStatus` is not `degraded` OR `StatusAnalysis` is empty THEN the drawer SHALL NOT render the note block.
4. WHEN the drawer is open THEN the system SHALL render exactly 24 status bars, one per local hour bucket for the last 24 hours, built via the existing `internal/history.BuildBuckets` bucketing rule.
5. WHEN the admin clicks the drawer's close control, or clicks the backdrop outside the drawer THEN the system SHALL close the drawer.
6. The system SHALL NOT render "Pausar monitoramento" or "Editar configuração" controls in the drawer.

**Independent Test**: Click a service with `degraded` status and a non-null `StatusAnalysis`, confirm the note renders with the right copy; click an `operational` service and confirm no note block renders; confirm 24 bars always render regardless of status.

---

### P3: Add a new SLO-based service

**User Story**: As an admin, I want to add a new service linked to a Datadog SLO from this screen, so I don't have to leave the page to register monitoring for a new service.

**Why P3**: Creation already exists as a backend capability (`POST /api/services`, `GET /api/integrations/datadog/slos`); this story is pure frontend wiring of an existing contract, lower risk than P1/P2's read paths, appropriate to ship last.

**Acceptance Criteria**:

1. WHEN the admin clicks "Adicionar serviço" THEN the system SHALL open a drawer with a name field and an SLO search field.
2. WHEN the admin types into the SLO search field THEN the system SHALL query `GET /api/integrations/datadog/slos?query=` (debounced) and list matching SLOs by name.
3. WHEN the admin selects an SLO and submits with a non-empty name THEN the system SHALL call `POST /api/services` with `{name, slo_id}`, close the drawer on success, and refresh the current page's list.
4. IF the name is empty OR no SLO is selected THEN the system SHALL disable the submit action.
5. IF `POST /api/services` fails THEN the system SHALL show an inline error in the drawer and SHALL NOT close it.
6. The system SHALL NOT render a "Polling manual" or "New Relic" option anywhere in this drawer.

**Independent Test**: Open the add drawer, search an SLO name, select it, submit, confirm the new service appears in the list; submit with SLO search returning nothing and confirm submit stays disabled.

---

## Edge Cases

- IF a service has zero `StatusInterval` rows at all (just created, poller hasn't run once) THEN the system SHALL show `not_configured` badge and "—" for uptime/last-check (P1 AC6), not an error.
- IF the Datadog SLO search returns zero results for a query THEN the add-drawer SHALL show "Nenhum SLO encontrado" instead of an empty list with no explanation.
- IF the admin navigates directly to `/services` unauthenticated THEN `RequireAuth` SHALL redirect to `/login` (existing behavior, unchanged).
- WHEN a service linked to a Datadog SLO whose name later changes upstream THEN the system SHALL show the SLO name as it was recorded at link time (see Design: resolved-at-creation vs live-lookup), not silently break if Datadog is unreachable.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| SVC-01 | P1 | Tasks (T1/T2/T4: migration + repository + handler) | In Progress |
| SVC-02 | P1 | Design | Pending |
| SVC-03 | P1 | Design | Pending |
| SVC-04 | P1 | Design | Pending |
| SVC-05 | P1 | Design | Pending |
| SVC-06 | P1 | Tasks (T4: handler uptime/last-seen) | In Progress (backend only, pending frontend T9) |
| SVC-07 | P1 | Design | Pending |
| SVC-08 | P1 | Design | Pending |
| SVC-09 | P2 | Design | Pending |
| SVC-10 | P2 | Design | Pending |
| SVC-11 | P2 | Design | Pending |
| SVC-12 | P2 | Design | Pending |
| SVC-13 | P2 | Design | Pending |
| SVC-14 | P3 | Tasks (T2/T3/T5: repository + detail endpoint) | In Progress (backend done, pending frontend T10) |
| SVC-15 | P3 | Tasks (T5: status_analysis pass-through) | In Progress (backend done, pending frontend T10) |
| SVC-16 | P3 | Tasks (T5: status_analysis pass-through) | In Progress (backend done, pending frontend T10) |
| SVC-17 | P3 | Tasks (T5: hourly_buckets, always 24) | In Progress (backend done, pending frontend T10) |
| SVC-18 | P3 | Design | Pending (frontend-only, drawer close control) |
| SVC-19 | P3 | Design | Pending (frontend-only, no drawer actions) |
| SVC-20 | P3 (add) | Design | Pending |
| SVC-21 | P3 (add) | Design | Pending |
| SVC-22 | P3 (add) | Design | Pending |
| SVC-23 | P3 (add) | Design | Pending |
| SVC-24 | P3 (add) | Design | Pending |
| SVC-25 | P3 (add) | Design | Pending |

**ID format:** `SVC-[NUMBER]`, sequential across all stories in file order (P1 ACs 1-8 → SVC-01..08, P2 ACs 1-5 → SVC-09..13, P3 drawer ACs 1-6 → SVC-14..19, P3 add ACs 1-6 → SVC-20..25).

**Coverage:** 25 total, 0 mapped to tasks yet, 25 unmapped ⚠️ (Design/Tasks not yet run).

---

## Success Criteria

- [ ] `/services` renders real service data (status, uptime, last-check) with no Latência anywhere.
- [ ] Filter chips + search narrow the current page's rows correctly and in combination.
- [ ] Clicking a row opens a read-only drawer with 30d stats, conditional note, and a real 24-hour history strip (no fabricated data).
- [ ] A new Datadog-backed service can be added from this screen end-to-end without a page reload losing filter/search state unexpectedly.
