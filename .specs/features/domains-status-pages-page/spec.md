# Domínios & Status Pages Page Specification

## Problem Statement

`/domains` (`DomainsStatusPagesPage.tsx`) still renders the pre-`new-layout-migration` flat-section UI (`DomainsSection` + `StatusPagesSection` stacked) instead of the redesigned tabbed screen (`handoff-new-layout/Dominios e Status Pages.dc.html`). Unlike most `new-layout-migration` screens, the backend for the Domínios tab is *already* almost complete — `domain-verification-state` shipped `domain_type`/`status`/`ssl_status`/`verified_at`/`last_error` and a real persisted `Verify` (re-check) + `Delete` flow. `gap-analysis.md` item 4 is stale on this point (it predates that feature). The one real backend gap is that nothing today tells the frontend *which status page a domain is attached to* — the mock's "Aponta para" column has no backing query. This spec closes that one backend gap and redesigns the frontend to the mock (two tabs, table layout, detail drawers, add drawers), reusing established shell patterns (`Drawer`, `ModeCard`, `ChipButton`) from `monitored-services-page`/`manual-polling-monitoring`.

## Goals

- [ ] Domínios tab matches the mock: table (Status/Domínio/Tipo/Aponta para/SSL/Verificado), row click opens a read-only detail drawer (DNS config for custom domains, re-verify/remove actions), add-domain drawer offers domain type choice with "Subdomínio Vane" disabled/decorative
- [ ] Status Pages tab matches the mock: table (Visib./Página/URL pública/Serviços/Atualizado), row click opens a detail drawer (service list, linked domain, link to existing edit screen), add-page drawer (name + visibility choice + service checklist)
- [ ] Backend exposes, per domain, the name of the first status page attached to it (if any) plus a count of how many are attached, with zero new persisted state (pure read-side join)

## Out of Scope

| Feature | Reason |
| --- | --- |
| "Subdomínio Vane" domain type (zero-DNS, Vane-hosted, e.g. `acme.vane.app`) | Needs a shared base domain + wildcard TLS issuance — infra that doesn't exist. User decision: ship as a disabled/decorative option (same pattern as "New Relic" in `manual-polling-monitoring`), no backend work. |
| Status page visibility (Público/Privado) | `public_status_handler.go`'s route has no auth check today — "Privado" is a real feature (route-level auth gate), not just a UI toggle. User decision: defer; UI ships "Público" only, visibility toggle in the add-page drawer is decorative/disabled. |
| Attaching a domain to a status page from this screen | Already covered by the existing `AttachDomainDrawer` (status-page-domain-attach); this screen's add-page drawer only creates the page (name + services), matching the mock exactly (its add-page drawer has no domain field). |
| Editing an existing status page's name/services/domain inline in this screen's detail drawer | User decision: "Editar página" navigates to the existing `/status-pages/{id}` screen instead of duplicating that form here. |
| Domain rows attached to more than one status page showing every page name | User decision: show the first attached page's name + a "+N" counter (e.g. "Status Público +1"), not a full list. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Domain-type toggle in add-domain drawer | "Subdomínio Vane" tile renders disabled (opacity, no `onClick`, same `aria-disabled` pattern as `ModeCard`'s SLO-disabled state), "Domínio próprio" is the only selectable/default tile | Matches `manual-polling-monitoring`'s established disabled-tile pattern; backend still only ever creates `domain_type = 'custom'` (existing DB CHECK) | y |
| Status-page visibility toggle in add-page drawer | "Público" tile is selected/default and the only functional one; "Privado" tile renders disabled, same pattern | Visibility is out of scope this cycle | y |
| Multi-page-per-domain display | Sort status pages attached to a domain by `created_at ASC`; show the first one's name, and if count > 1 append ` +N` (N = count − 1) | User decision | y |
| "Editar página" action | `<Link>` to `/status-pages/{id}` (existing route/screen) | User decision, avoids duplicating the edit form | y |
| "Aponta para" for a domain with zero attached pages | Render `—` (same convention the mock itself uses for `d.target` on unattached domains, e.g. `api-status.acme.health`) | Matches mock literal | y |
| New backend field name | `domainResponse` gains `attached_page_name *string` and `attached_page_count int` (0 when nothing attached) | Keeps the join result explicit and typed rather than overloading `target` as a free-form string only meaningful with a count alongside it | y |
| Verify/Delete/Create endpoints for domains | No changes — `DomainsHandler.Verify`/`Delete`/`Create`/`List` already implement everything the redesigned Domínios tab needs (persisted `Verify`, cooldown, `ErrDomainInUse` 409 on delete) | Confirmed by reading `internal/api/domains_handler.go` | y |
| Status Pages tab's create/detail data | `StatusPagesHandler.Create`/`List`/`SetServices` already cover name + service list; only the domain-name join and the frontend redesign are new | Confirmed by reading `internal/api/status_pages_handler.go`, `internal/db/status_page_repository.go` | y |

**Open questions:** none — all resolved above.

---

## User Stories

### P1: Domínios tab redesign ⭐ MVP

**User Story**: As an owner/operator, I want the Domínios tab to show status, type, attached page, SSL, and last-verified at a glance (matching the new design), so I can spot a broken domain without opening every row.

**Why P1**: This is the primary content of the screen and the tab gap-analysis flagged as needing work; the backend is already 95% there.

**Acceptance Criteria**:

1. WHEN the Domínios tab renders THEN the system SHALL show one row per domain with columns Status (colored pill + dot), Domínio (hostname, monospace), Tipo ("Domínio próprio" for `domain_type = "custom"`), Aponta para (per the join below), SSL (colored label from `ssl_status`), Verificado (`verified_at` formatted, or `—` if null)
2. WHEN a domain has zero status pages attached THEN the system SHALL render "—" in the Aponta para column
3. WHEN a domain has exactly one status page attached THEN the system SHALL render that page's name in the Aponta para column
4. WHEN a domain has more than one status page attached THEN the system SHALL render the earliest-created page's name followed by ` +N` where N is the remaining count
5. WHEN a user clicks a domain row THEN the system SHALL open a detail drawer showing status pill, type + target line, an error banner (if `last_error` is non-null), SSL/Verified-at fields, DNS config block (CNAME target) for custom domains, and "Verificar novamente"/"Remover domínio" actions
6. WHEN the user clicks "Verificar novamente" THEN the system SHALL call `POST /api/domains/{id}/verify` and refresh the drawer with the returned state
7. WHEN the user clicks "Remover domínio" THEN the system SHALL call `DELETE /api/domains/{id}`, closing the drawer and removing the row on success
8. IF `DELETE /api/domains/{id}` returns 409 (domain still attached to a status page) THEN the system SHALL show an inline error in the drawer instead of closing it

**Independent Test**: Load `/domains` with seeded domains in each status/ssl/attachment combination; verify table values and drawer contents match without touching the Status Pages tab.

---

### P2: Add-domain drawer ⭐ MVP

**User Story**: As an owner/operator, I want to register a new domain from this screen with the same two-tile type chooser as the mock, so the flow matches the rest of the app's add-drawers.

**Why P2**: Needed for the tab to be usable end-to-end, but the read path (P1) is the bulk of the redesign risk.

**Acceptance Criteria**:

1. WHEN the user opens the add-domain drawer THEN the system SHALL show two tiles ("Subdomínio Vane", "Domínio próprio") with "Domínio próprio" selected by default
2. WHEN the user clicks the "Subdomínio Vane" tile THEN the system SHALL NOT change selection (tile is disabled, no `onClick` attached, `aria-disabled="true"`)
3. WHILE "Domínio próprio" is selected the system SHALL show a hostname input and submit via `POST /api/domains` with the entered hostname
4. IF the submitted hostname is already registered (409) THEN the system SHALL show the existing duplicate-hostname error inline, matching current `DomainsSection` behavior

**Independent Test**: Open the add-domain drawer, confirm the disabled tile can't be selected, submit a new hostname, confirm it appears in the table; submit a duplicate and confirm the inline error.

---

### P3: Status Pages tab redesign

**User Story**: As an owner/operator, I want the Status Pages tab redesigned to the same table/drawer pattern, so both tabs are visually consistent.

**Why P3**: No backend gap at all — pure frontend redesign reusing existing endpoints; lowest risk, does last.

**Acceptance Criteria**:

1. WHEN the Status Pages tab renders THEN the system SHALL show one row per status page with columns Visib. ("Público", always — see Out of Scope), Página (name), URL pública (hostname built from `domain_id`/`subdomain`, or `—` if none attached), Serviços (`N serviços`), Atualizado (relative/formatted timestamp)
3. WHEN a user clicks a status page row THEN the system SHALL open a detail drawer listing its attached services, its domain (or `—` if none), a "Ver página pública" link (disabled/absent if no domain attached), and an "Editar página" link to `/status-pages/{id}`
4. WHEN the user opens the add-page drawer THEN the system SHALL show a name field, a visibility chooser ("Público" selected/functional, "Privado" disabled per Out of Scope), and the service checklist, submitting via the existing `POST /api/status-pages`

**Independent Test**: Load the Status Pages tab, open a page's detail drawer, confirm the "Editar página" link routes to `/status-pages/{id}`; create a new page via the add-page drawer and confirm it appears in the table.

---

## Edge Cases

- IF a domain's `verified_at` is null (never checked, or created but not yet verified) THEN the system SHALL render "—" in the Verificado column, not a formatted `null` date
- IF `GET /api/domains` returns zero domains THEN the system SHALL show the existing empty-state pattern (reuse `EmptyState`), not an empty table
- IF a status page has no `domain_id` THEN the system SHALL render "—" for URL pública and disable/omit "Ver página pública" in its detail drawer
- WHEN switching tabs THEN the system SHALL close any open detail/add drawer from the previous tab (no drawer state leaking across tabs)

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| DSP-01 | P1 | Design | Pending |
| DSP-02 | P1 | Design | Pending |
| DSP-03 | P1 | Design | Pending |
| DSP-04 | P1 | Design | Pending |
| DSP-05 | P1 | Design | Pending |
| DSP-06 | P1 | Design | Pending |
| DSP-07 | P1 | Design | Pending |
| DSP-08 | P1 | Design | Pending |
| DSP-09 | P2 | Design | Pending |
| DSP-10 | P2 | Design | Pending |
| DSP-11 | P2 | Design | Pending |
| DSP-12 | P2 | Design | Pending |
| DSP-13 | P3 | Design | Pending |
| DSP-14 | P3 | Design | Pending |
| DSP-15 | P3 | Design | Pending |
| DSP-16 | Edge case | Design | Pending |
| DSP-17 | Edge case | Design | Pending |
| DSP-18 | Edge case | Design | Pending |
| DSP-19 | Edge case | Design | Pending |

**Coverage:** 19 total, 0 mapped to tasks, 19 unmapped ⚠️ (expected pre-Design)

---

## Success Criteria

- [ ] `/domains` renders the tabbed mock layout with both tabs functional end-to-end (list, detail, add) against the real backend
- [ ] Zero new migrations (the one real backend gap — domain→page name join — is a read-only query addition, no schema change)
- [ ] `go build/vet/test`, `gofmt`, `tsc -b --noEmit`, `npm run test` all clean
