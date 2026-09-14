# Dashboard Overview Page Specification

## Problem Statement

Vane has no post-login landing page. Today `/` redirects straight to `/domains` — there is no at-a-glance summary of the tenant's monitoring state (uptime, open incidents, unhealthy services, verified domains). The redesign handoff (`handoff-new-layout/README.md` §0, `Visao Geral.dc.html`) defines this screen as "Visão Geral" and expects login to redirect here instead of any list screen. This is also the first fully-new page in the `new-layout-migration` line (no prior mocked version exists to migrate — see `.specs/features/new-layout-migration/gap-analysis.md`, which flagged this screen as "previously-undocumented").

## Goals

- [ ] Authenticated users land on a real aggregation screen instead of `/domains` after login/tenant-selection
- [ ] The four summary cards (uptime médio 30d, incidentes abertos, serviços com problema, domínios verificados) show real backend-computed values, not mock data
- [ ] The 14-day uptime bar chart and "incidentes recentes" list show real data
- [ ] "Atalhos rápidos" links navigate to the correct existing screens

## Out of Scope

| Feature | Reason |
| --- | --- |
| "Atividade recente do team" card (activity feed) | Requires a new `ActivityEvent` model + fan-out across invites/domains/incidents that doesn't exist anywhere in the backend — same category of gap that already caused `Notificações` (screen 11) to be deferred in `gap-analysis.md`. User decision (this session): defer: the card does not ship in this pass; UI omits it rather than showing a placeholder that promises unbuilt data. |
| Free-plan upsell banner | Depends on `Tenant.Plan` semantics that don't exist yet (`AD-025` paused, seat-limit enforcement also blocked on the same gap) and links to Planos & Faturamento, which is out of this cycle entirely (`gap-analysis.md` item 8). User decision (this session): hide entirely, no plan-tier branching in this spec. |
| Incident severity breakdown on this screen | Mock's "Incidentes abertos" card is a single count, no severity split is rendered anywhere on this screen (confirmed against `Visao Geral.dc.html`) — building a breakdown response field with no UI consumer would be speculative. |
| Planos & Faturamento, Notificações screens themselves | Separate specs per `gap-analysis.md`, unaffected by this change. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| New route path | `/overview` | Matches existing route-naming convention (`/domains`, `/services`, `/incidents` — English nouns, no accents/spaces) rather than `/visao-geral`; i18n handles the label. | y |
| Login/root redirect target | `/` (via `RootRoute`) now renders the overview page directly instead of `<Navigate to="/domains" />`; `/overview` also exists as an explicit path for direct links/back-nav. | Mock (`README.md` §0/§1) says redirect goes to Visão Geral after login — matches `RootRoute`'s existing role as the authenticated landing slot without adding a second redirect hop. | y |
| Uptime aggregation scope | Average of `UptimePercent` (existing `internal/history` package) over the last 30 days across all services that have at least one interval in that window; services with `ok=false` (no data) are excluded from the average, not counted as 0%. | Reuses `service-status-intervals`' existing per-service uptime math (`internal/history/uptime.go`) unchanged; treating "no data" as 0% would misrepresent a brand-new service as an outage. | y |
| "Serviços com problema" definition | Count of services where `CurrentStatus != "operational"` (i.e., `degraded` or `outage`) | Matches the field already used everywhere else in the codebase (`Service.CurrentStatus`) — no new status vocabulary. | y |
| "Domínios verificados" definition | Count of domains where `Status == "verified"` (from `domain-verification-state`) | Reuses the exact field the mock's own backend note points at ("domain verification results"). | y |
| 14-day uptime series | One bucket per day, last 14 days, value = the same tenant-wide average used for the summary card but computed per-day instead of once over the full window | Reuses `internal/history.BuildBuckets`-style bucketing (already proven for the public status page) rather than inventing a second aggregation mechanism; "aggregated" in the mock's own label ("Uptime agregado — últimos 14 dias") supports a single tenant-wide series, not one bar chart per service. | y (confirmed with user during Specify) |
| "Incidentes recentes" | Up to 3 most recent incidents (any status, ordered by `created_at desc`) — not filtered to open-only | Mock shows status text per row ("status · relative time"), implying resolved incidents can appear too; matches `IncidentRepository`'s existing list-ordering convention. | y |
| Empty-tenant behavior (zero services/domains/incidents) | All 4 cards render `0` (or uptime card renders "—" per the "no data" rule above); chart renders 14 empty/no-data bars; incidents list renders an empty-state message; shortcuts grid always renders (not data-dependent) | No spec-defined alternative exists in the mock for a brand-new tenant; zero/empty-state is the only defensible default and matches how every other list screen in this codebase already handles zero rows. | y |

**Open questions:** none — all resolved or logged above.

---

## User Stories

### P1: Real aggregation summary on login ⭐ MVP

**User Story**: As an authenticated tenant member (any role), I want to land on a page that summarizes my tenant's current monitoring health so that I don't have to visit 3 separate screens to know if anything is wrong.

**Why P1**: This is the entire point of the screen — without it there is nothing to ship.

**Acceptance Criteria**:

1. WHEN an authenticated user with a resolved tenant loads `/` THEN the system SHALL render the Overview page instead of redirecting to `/domains`.
2. WHEN the Overview page mounts THEN the system SHALL call a single aggregation endpoint (`GET /api/overview`) and render its response in the 4 summary cards, the 14-day uptime chart, and the recent-incidents list.
3. The system SHALL compute "Uptime médio (30d)" as the average of `UptimePercent(intervals, now-30d, now)` across all services with `ok=true` for that window; the system SHALL render "—" WHEN no service has `ok=true` (zero data points).
4. The system SHALL compute "Incidentes abertos" as the count of incidents whose `Status` is the tenant's open-incident status value (matching `IncidentRepository`'s own definition of "open", not a new one).
5. The system SHALL compute "Serviços com problema" as the count of services whose `CurrentStatus != "operational"`.
6. The system SHALL compute "Domínios verificados" as the count of domains whose `Status == "verified"`.
7. WHEN the aggregation endpoint is called THEN the system SHALL return exactly 14 daily buckets for the uptime series, oldest first, each bucket independently computed the same way as AC3 but scoped to that single day.
8. WHEN the aggregation endpoint is called THEN the system SHALL return up to 3 incidents ordered by `created_at DESC`, each with `id`, `title`, `status`, `created_at` (resolved incidents included per the Assumptions table).
9. IF a tenant has zero services, zero incidents, or zero domains THEN the system SHALL render the corresponding card/section in its documented empty state (AC3's "—" rule for uptime; `0` for the two count cards; an empty-state message for the incidents list) instead of erroring.

**Independent Test**: Log in against a tenant with ≥1 service/incident/domain, land on `/`, confirm the 4 cards, chart, and incident list match values independently queried from the same tables via `psql`/existing list endpoints.

---

### P2: Quick shortcuts navigation

**User Story**: As a tenant member, I want one-click shortcuts to the screens I use most from setup, so that I don't have to hunt through the sidebar right after landing.

**Why P2**: Convenience layer on top of P1's data — the page is fully functional and demoable without it, but the mock specifies it as a first-class section.

**Acceptance Criteria**:

1. WHEN the user clicks "Adicionar serviço" THEN the system SHALL navigate to `/services`.
2. WHEN the user clicks "Criar status page" THEN the system SHALL navigate to `/domains` (Status Pages tab — matches the existing `DomainsStatusPagesPage` tab structure per `gap-analysis.md` §4).
3. WHEN the user clicks "Convidar usuário" THEN the system SHALL navigate to `/admins`.
4. WHEN the user clicks "Ver domínios" THEN the system SHALL navigate to `/domains`.
5. The system SHALL render all 4 shortcut links regardless of role (no `RequireRole` gating on this screen itself) — the destination screens enforce their own role checks (e.g. `/admins` already requires `owner`).

**Independent Test**: Click each of the 4 shortcut cards from a fresh Overview load and confirm the resulting route.

---

### P3: Sidebar navigation entry

**User Story**: As a tenant member navigating away from Overview and back, I want a persistent "Visão geral" sidebar link, so that I don't have to type `/` manually.

**Why P3**: Nice-to-have wiring — the page is reachable via `/` and direct `/overview` links without it, but the mock places it as a standalone, always-first sidebar item (`README.md` line 38).

**Acceptance Criteria**:

1. The system SHALL render a standalone "Visão geral" nav item above the existing nav groups in `Sidebar.tsx`, linking to `/overview`.
2. WHEN the current route is `/` or `/overview` THEN the system SHALL render that nav item in its active state (same convention as every other `NavLink` in `Sidebar.tsx`).

**Independent Test**: Load the app authenticated, confirm the sidebar shows "Visão geral" first and highlights when on that route.

---

## Edge Cases

- IF the aggregation endpoint call fails (network/5xx) THEN the system SHALL show the existing shared error-state pattern used by other list screens in this codebase (not a blank page).
- IF a service's uptime window has partial data (service created < 30 days ago) THEN the system SHALL use `UptimePercent`'s existing clipped-denominator behavior unchanged (no new logic here — inherited from `service-status-intervals`).
- WHEN the tenant has more than 3 incidents THEN the system SHALL render exactly 3 (per AC8) plus the existing "Ver todos" link to `/incidents`.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| OVW-01 | P1 | Design | Verified |
| OVW-02 | P1 | Design | Verified |
| OVW-03 | P1 | Design | Verified |
| OVW-04 | P1 | Design | Verified |
| OVW-05 | P1 | Design | Verified |
| OVW-06 | P1 | Design | Verified |
| OVW-07 | P1 | Design | Verified |
| OVW-08 | P1 | Design | Verified |
| OVW-09 | P1 | Design | Verified |
| OVW-10 | P2 | Design | Verified |
| OVW-11 | P2 | Design | Verified |
| OVW-12 | P2 | Design | Verified |
| OVW-13 | P2 | Design | Verified |
| OVW-14 | P2 | Design | Verified |
| OVW-15 | P3 | Design | Verified |
| OVW-16 | P3 | Design | Verified |

**ID format:** `OVW-[NUMBER]` (Overview)

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 16 total, 0 mapped to tasks, 16 unmapped ⚠️ (Tasks phase not yet run)

---

## Success Criteria

- [ ] `/` renders the Overview page for an authenticated user with a resolved tenant, with zero redirect to `/domains`
- [ ] All 4 summary cards, the 14-day chart, and the incidents list show values that match direct DB queries for the same tenant/window
- [ ] `npm run test` and `npx tsc -b --noEmit` green; backend gate (`go build/vet/test`, integration) green
