# Handoff: Public Status Page

## Overview
Public, unauthenticated status page for **Vane** (same product as `design_handoff_admin_dashboard`), served at the client company's own subdomain (e.g. `status.empresa.com`). Shows current service health + incident timeline to an anonymous visitor. No login, no write actions, no branding from Starbem/Vane itself — the page is meant to look like the client company's own product. Backed by `zeep-vane`'s `internal/api/public_status_handler.go`, wired through `router.HostRouter` (Host-header based multi-tenancy — each published `StatusPage` row resolves to its own scoped data).

## About the Design Files
`Status Page Publica.dc.html` is a **design reference built in HTML** — layout, states, and copy are the source of truth; it is not production code to ship as-is. Recreate it in whatever frontend stack serves this page (a small static/SSR page is enough — no auth, no client-side routing beyond the optional incident-detail expand). Reuse the same design tokens as the admin dashboard (`nocturne-styles.css`, bundled with that handoff) — this page is a second, unauthenticated surface of the same design system, not a separate one.

## Fidelity
**High-fidelity.** Colors, spacing, copy and the status/severity color mapping are final. Recreate pixel-close.

## Design system
Same **Nocturne** tokens as the admin handoff — see `design_handoff_admin_dashboard/README.md` for the full token list. Key subset this page uses:
```
--color-bg #161826, --color-text #e9e9ed, --color-accent #9184d9
--color-success  oklch(0.72 0.135 152)   service operational / resolved
--color-warning  oklch(0.78 0.15 80)     service degraded / identified / monitoring
--color-critical oklch(0.685 0.19 25)    service outage / investigating
Card / Tag classes as in the admin handoff (surface bg, 8px radius, tinted status tags via color-mix, never a flat saturated fill).
```
No `Button`/`Input`/`Dialog`/`Table` needed here — this page is read-only: `Card`, `Tag`, and a simple two-column list row are the only components.

### New component: uptime history bar
A row of ~45 thin (aspect roughly 1:14) rounded bars per service, one per day, colored success/warning/critical for that day's status, most-recent on the right. This is the one visual addition beyond the admin design system — not a Nocturne-defined component, follow the same color mapping and restraint (solid color per bar, no gradient, no shadow). Caption below: "N dias atrás" (left) / "hoje" (right). A `title`/tooltip per bar showing the exact day + status is a nice-to-have, not required.

## Screens

### 1. Status page principal
- **Header**: client company's own logo (image, no page title needed) top-left; "Atualizado {relative time}" top-right, with a small clock/cache icon when the page is showing stale data (see cache state below).
- **Overall status band**: one card-level banner summarizing the whole page — a colored dot + short label ("Todos os sistemas operacionais" / "Interrupção parcial em andamento" / "Interrupção em andamento"), colored by the worst service state present. When showing cached data, add a second line: "Mostrando o último dado disponível — atualização em andamento." Never show a technical error to the visitor (SP-06 item 4) — the cache fallback is silent/calm, not an error state.
- **Active incidents** (only rendered when at least one exists) — sits **above** the services list, most prominent element on the page when present:
  - status tag (Investigando/Identificado/Monitorando — same 3-state vocabulary as the admin's incident status, warning/critical tinted per state), title, tags for every linked service.
  - Expand/collapse reveals the full update timeline, **most-recent update first** — matches `ListPublicForStatusPage`'s ordering, don't re-sort.
- **Services list**: one row per service — status dot + name + status tag (Operacional/Degradado/Interrupção), cache icon if that service's own data is stale, and the uptime history bar row described above underneath each row.
  - A service with `not_configured` status is **never shown** — the backend already excludes it (`public_status_handler.go` skips it server-side); don't add client-side logic to re-include it.
- **Resolved incident history**: separate section below services, header "Histórico (últimos 90 dias)" — the backend's retention window (`incidentRetentionDays = 90` in `public_status_handler.go`). Each row: title, "Resolvido {date} · {duration}", expandable timeline (same shape as active incidents). Show an explicit empty state ("Nenhum incidente nos últimos 90 dias") rather than an empty section when there's no history — don't just omit the section, the visitor should be able to tell there's genuinely nothing rather than the section failing to load.
- **Footer**: one line, "Atualiza automaticamente a cada 2 minutos" (client polls; the backend itself never calls Datadog on a visitor request — SP-06 item 3).

### 2. Incident detail
Not built as a separate page in the reference — implemented as the inline expand described above (click an incident card to reveal its timeline). This satisfies the brief's "expansível inline" option. If the real product later wants a shareable per-incident URL, the same card content can move to a route without changing the visual design.

## States to cover
- **Loading** (first paint before data arrives): skeleton blocks in place of header/band/rows — reference file's `isLoading` state.
- **Todos operacionais, sem incidente** — common case, should read as "fine" at a glance, no incident section rendered.
- **Com incidente ativo** — incident card becomes the top-of-page focal element; overall band reflects the worst affected service.
- **Serviço degradado/outage sem incidente manual aberto** — automatic status changed before an admin opened an incident post; band + row show the degraded/outage color with no incident card above (this is a real, expected state, not a bug to hide).
- **Cache fallback** — Datadog fetch failed server-side; page keeps serving the last good snapshot with its real (older) timestamp and a quiet visual cue, never an alarm.

The reference file exposes all four via a design-time-only scenario switcher (top-right pill) plus a "Carregando" option — that switcher is a review aid, remove it in production; drive real state from the API response instead.

## Data shape (from `public_status_handler.go` — build the client against this, not an assumed shape)
```json
GET / (Host-routed to the matching published StatusPage)
{
  "services": [
    { "name": "string", "status": "operational|degraded|outage", "last_updated_at": "RFC3339 timestamp" }
  ],
  "incidents": {
    "active":   [ { "id", "title", "status": "investigating|identified|monitoring", "created_at", "resolved_at": null, "updates": [ { "body", "created_at" } ] } ],
    "resolved": [ { "id", "title", "status": "resolved", "created_at", "resolved_at", "updates": [...] } ]
  }
}
```
Notes:
- `services` omits anything still `not_configured` and omits per-service `last_updated_at` (zero value) if the poller never successfully reached it — render that as "sem dados" rather than a fabricated timestamp, don't backfill with "now".
- Both incident arrays' `updates` already arrive most-recent-first — render in the order received.
- The endpoint requires no auth header; it is scoped per-hostname via the Host header server-side (multi-tenant) — the client never passes a status-page ID itself.
- There is currently **no per-service daily-history endpoint** — the uptime bars in the reference use static/seeded data. Flag this to backend if the real product wants historical bars; until then, either omit the bars or hide them behind a flag once real data exists (the reference's `showHistoryBars` prop shows how to gate it).
- No endpoint currently reports "using cached data because Datadog is down" explicitly — the reference infers it visually from an old `last_updated_at`. Confirm with backend whether the poller/integration status should be exposed here, or whether "old timestamp" is the intended signal.

## Interactions
- Read-only page, no forms, no destructive actions, no confirmation dialogs.
- Incident cards (active and resolved) toggle expand/collapse on click of the title row or an explicit "Ver linha do tempo" button — no navigation needed for the MVP.
- Motion: short ease-out only, consistent with the admin dashboard's motion guidance.

## Out of scope (per brief, don't build)
- Any authenticated/admin view (covered by the admin handoff).
- Uptime history *chart* (P3, no backend data yet) — the bar strip here is a lighter substitute the reference added at the user's request; treat it as decoration until real per-day data exists.
- Multi-idioma, email subscribe/notify — not in the MVP spec; confirm before building if requested later.

## Assets
- Company logo: an image slot in the header (the reference uses a drag-and-drop placeholder) — production should source this per-tenant (status page's owning company), not hardcode one logo.
- No other imagery; icons are inline SVG (Phosphor-style, swap in the real `@phosphor-icons` package in production).

## Files in this bundle
- `Status Page Publica.dc.html` — the interactive reference covering both states-to-cover screens (main page + inline incident detail) and all 5 page states (loading, ok, incident, degraded, cache), plus the uptime-bar component.
- Reuses `nocturne-styles.css` from `design_handoff_admin_dashboard/` — copy it alongside this README if handing this package off separately.
