# Dashboard Overview Page Specification

## Problem Statement

Vane has no post-login landing page. Today `/` redirects straight to `/domains` — there is no at-a-glance summary of the tenant's monitoring state (uptime, open incidents, unhealthy services, verified domains). The redesign handoff (`handoff-new-layout/README.md` §0, `Visao Geral.dc.html`) defines this screen as "Visão Geral" and expects login to redirect here instead of any list screen. This is also the first fully-new page in the `new-layout-migration` line (no prior mocked version exists to migrate — see `.specs/features/new-layout-migration/gap-analysis.md`, which flagged this screen as "previously-undocumented").

## Goals

- [x] Authenticated users land on a real aggregation screen instead of `/domains` after login/tenant-selection
- [x] The four summary cards (uptime médio 30d, incidentes abertos, serviços com problema, domínios verificados) show real backend-computed values, not mock data — including their subtext lines (OVW-17..20, added 2026-09-14)
- [x] The 14-day uptime bar chart and "incidentes recentes" list show real data
- [x] "Atalhos rápidos" links navigate to the correct existing screens

## Out of Scope

| Feature | Reason |
| --- | --- |
| "Atividade recente do team" card (activity feed) | Requires a new `ActivityEvent` model + fan-out across invites/domains/incidents that doesn't exist anywhere in the backend — same category of gap that already caused `Notificações` (screen 11) to be deferred in `gap-analysis.md`. User decision (this session): defer: the card does not ship in this pass; UI omits it rather than showing a placeholder that promises unbuilt data. |
| Free-plan upsell banner | Depends on `Tenant.Plan` semantics that don't exist yet (`AD-025` paused, seat-limit enforcement also blocked on the same gap) and links to Planos & Faturamento, which is out of this cycle entirely (`gap-analysis.md` item 8). User decision (this session): hide entirely, no plan-tier branching in this spec. |
| ~~Incident severity breakdown on this screen~~ **Superseded 2026-09-14** | Original call: mock's "Incidentes abertos" card is a single count, no split rendered — see `Visao Geral.dc.html`. Reversed during the handoff visual-parity pass (same session as OVW-17/18/19/20 below): a literal line-by-line comparison against `Visao Geral.dc.html`'s card subtext row found the mock *does* render a second line under each summary card (uptime trend, incident severity/status breakdown, service/domain denominators) that the first pass had missed. User decision when asked (`AskUserQuestion`, "real data where cheap vs. hardcode"): all four subtexts ship as real backend-computed values, not hardcoded strings. See P1 ACs OVW-17..OVW-20. |
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
| **(added 2026-09-14)** Uptime trend subtext | "Uptime médio (30d)" card's second line compares the current 30d average against the prior 30d average (60d-30d-ago window), rendered as `+X.X% vs mês anterior` / `-X.X% vs mês anterior`; omitted (no second line) when either average is unavailable or the delta rounds to `0.0` | Matches the mock's card subtext row; the prior-window average reuses the exact same `UptimePercent` aggregation as OVW-03, just shifted 30 days back — no new math. | y |
| **(added 2026-09-14)** Incident breakdown subtext | "Incidentes abertos" card's second line shows `{critical} crítico, {monitoring} monitorando` among currently-open incidents, where "critical" = `Severity == "critical"` and "monitoring" = `Status == "monitoring"` (independent, non-exclusive filters — not required to sum to the open total); omitted when both counts are 0 | Reverses the original Out-of-Scope call (row above) — the mock does render this line; both filters reuse fields already used elsewhere in the codebase (`Incident.Severity`, `Incident.Status`), no new vocabulary. | y |
| **(added 2026-09-14)** Unhealthy-services denominator subtext | "Serviços com problema" card's second line always shows `de {total} serviços monitorados`, where total = count of all services for the tenant (not just unhealthy ones) | Matches the mock's card subtext; always rendered (unlike the other three subtexts), since a denominator is meaningful even at 0/0. | y |
| **(added 2026-09-14)** Verified-domains pending subtext | "Domínios verificados" card's second line shows `{pending} pendente de verificação` where pending = `total_domains - verified_domains`, only rendered when pending > 0 | Matches the mock; omitted at 0 pending since "0 pendente" is redundant with the card's own `verified/total` value. | y |

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
10. **(added 2026-09-14, OVW-17)** WHEN both the current and prior 30d uptime averages are available and their rounded delta is non-zero THEN the "Uptime médio (30d)" card SHALL render a second line, `+X.X% vs mês anterior` if the current average is higher or `-X.X% vs mês anterior` if lower; otherwise the system SHALL render no second line.
11. **(added 2026-09-14, OVW-18)** The system SHALL compute, among currently-open incidents, the count with `Severity == "critical"` and the count with `Status == "monitoring"` (independent filters); WHEN either count is greater than 0 THEN the "Incidentes abertos" card SHALL render a second line `{critical} crítico, {monitoring} monitorando`; otherwise no second line.
12. **(added 2026-09-14, OVW-19)** The "Serviços com problema" card SHALL always render a second line `de {total} serviços monitorados`, where total is the count of all services for the tenant regardless of health.
13. **(added 2026-09-14, OVW-20)** WHEN `total_domains - verified_domains > 0` THEN the "Domínios verificados" card SHALL render a second line `{pending} pendente de verificação`; otherwise no second line.

**Independent Test**: Log in against a tenant with ≥1 service/incident/domain, land on `/`, confirm the 4 cards (including their subtext lines), chart, and incident list match values independently queried from the same tables via `psql`/existing list endpoints.

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
| OVW-17 | P1 | Verified | Verified |
| OVW-18 | P1 | Verified | Verified |
| OVW-19 | P1 | Verified | Verified |
| OVW-20 | P1 | Verified | Verified |

**ID format:** `OVW-[NUMBER]` (Overview)

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 20 total, 20 verified. OVW-01..16 verified at original Execute (`validation.md`, diff `7ca2a64..29f6e57`); OVW-17..20 added 2026-09-14 during the handoff visual-parity pass (reverses the incident-severity-breakdown Out-of-Scope call above) and verified in the same pass's validation addendum (diff `29f6e57..28ae93d`).

---

## Success Criteria

- [x] `/` renders the Overview page for an authenticated user with a resolved tenant, with zero redirect to `/domains`
- [x] All 4 summary cards (including subtext lines, OVW-17..20), the 14-day chart, and the incidents list show values that match direct DB queries for the same tenant/window
- [x] `npm run test` and `npx tsc -b --noEmit` green; backend gate (`go build/vet/test`, integration) green
