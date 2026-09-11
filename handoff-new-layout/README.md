# Handoff: Vane Dashboard — Full Layout Migration

## Overview
Vane is a status-monitoring / incident-management SaaS (with a self-hosted deployment mode). This bundle contains the **complete redesigned application shell and all screens** — auth, the logged-in dashboard shell (sidebar + topbar), and every feature area (monitored services, domains & status pages, incidents, poller health, users, billing/licensing, settings, profile, notifications).

Goal of this handoff: **replace the current production layout with this new design**, screen by screen, reusing the target codebase's existing frontend stack (React/Vue/etc.), state management, routing, and API client conventions — not by embedding this HTML.

## ⚠️ Do this FIRST: backend gap analysis

**Before writing any new screen code**, go through every screen below and the "Data model & backend requirements" section, and cross-reference it against the current backend (models, migrations, endpoints, jobs). These mocks were built as click-through prototypes with all data **hardcoded/seeded on the frontend** — every entity, field, status, and action described below needs to actually exist server-side before the new frontend can be wired up for real.

Concretely, for each item below, check the existing backend and produce a short gap list before coding:
1. Does the **data model / table** exist? If not, what migration is needed?
2. Does an **API endpoint** exist for every read/write action implied by the screen (list, get, create, update, delete, invite, activate, etc.)? If not, list the endpoints to build.
3. Does any **background job / integration** exist to populate the data (e.g. poller heartbeats, Stripe webhooks, DNS verification, AI summarization)? If not, flag it as infra work, not just an API.
4. Are there **permission/role checks** implied (admin vs member vs viewer; plan-gated features) that need enforcing server-side, not just hidden in the UI?

Do NOT treat the UI as ground truth for "this already works" — every interactive element in these mocks (search, filters, drawers, forms, toggles) is currently backed by local component state and seed arrays only. Treat this document as the spec for what the backend must support, and only start building screens once the gaps above are resourced or explicitly deferred with the user's sign-off.

## About the Design Files
The `.dc.html` files in this bundle are **design references** — interactive HTML/JS prototypes built to communicate exact layout, color, copy, and behavior. They are not production code and should not be embedded or copied as-is. Recreate each screen in the target codebase's existing framework and component library, following its conventions for routing, forms, state, and data fetching. If the target codebase has no established frontend framework yet, choose the most appropriate one for the stack (e.g. React + a router + a query/cache library) and set up the screens there.

Each `.dc.html` file is self-contained and can be opened directly in a browser to click through the real interaction (filters, drawers, dark mode toggle, checkout flow, etc.) — use that as the source of truth for behavior alongside this document.

## Fidelity
**High-fidelity.** Every screen uses final copy (in Brazilian Portuguese), a finished color system (light + dark themes), a finished type system (Manrope), and fully interactive prototype behavior (state, filters, drawers, modals, simulated async flows). Recreate pixel-accurately using the design tokens below.

---

## Global shell (present on every logged-in screen)

All screens except `Login Bootstrap.dc.html` share one shell: a collapsible left sidebar + a topbar + a scrollable content area. Build this once as a layout/shell component.

### Sidebar
- Width: `72px` collapsed, `240px` expanded. Expands on mouse-enter, collapses on mouse-leave, unless **pinned** (pin toggle at the bottom, "Fixar menu" — persists expanded state).
- Top: tenant switcher — a 28×28 rounded-square avatar (tenant initials, purple `#5A46C7` bg) + tenant name + plan badge (`Free` grey pill or plan-name purple-tinted pill). Click opens a popover (only if the account is multi-tenant): list of tenants (avatar + name, checkmark on current), divider, "Sair" (logout, opens confirm modal — see Modals below).
  - **Backend note:** tenant list, current tenant, and plan tier must come from the authenticated session/account API, not be hardcoded.
- Below a divider: three nav groups, each with an uppercase 10.5px label when expanded:
  - **Monitoramento**: Serviços monitorados, Domínios & Status, Incidentes
  - **Plataforma**: Integrações, Poller Status
  - **Organização**: Usuários, Planos & Faturamento
- Footer (pinned to bottom, above a divider): Configurações (gear icon), then "Fixar menu" pin toggle.
- Current-page nav item: purple-tinted background `rgba(90,70,199,0.08)`, text/icon `#5A46C7`, no link (static). Other items: icon + label, `color: sidebarTextMuted`, hover background `sidebarHoverBg`.

### Topbar
- Height `60px`, bottom border, `28px` horizontal padding.
- Left: current page title (15px, bold).
- Right, in order: **dark-mode toggle** (moon/sun icon), **notification bell** (links to `Notificacoes.dc.html`; small red unread-dot badge, top-right of the bell), **avatar menu** (32×32 rounded-square, user initials, purple-tinted). Avatar menu popover: user name + email, "Meu perfil" (link), "Configurações" (link), "Sair" (opens logout confirm modal).

### Content area
- Scrollable, `32px 40px` padding, content max-width `1200px` (centered) on dashboard/list screens, `800px` on Notificações.
- Standard page header pattern: `h1` (20px/700) + one-line grey subtitle paragraph, often with a primary action button top-right (e.g. "Adicionar serviço").

### Modals (shared across all screens)
- **Logout confirm**: centered modal, icon (logout glyph) in a neutral tinted square, "Sair da conta?" heading, one-line body copy, Cancelar (secondary) / Sair (dark button) — confirming should call the real sign-out endpoint and redirect to the login screen.
- **Delete-account / destructive confirms** (e.g. "Excluir conta" in Configurações, "Encerrar sessão" in Meu Perfil): same centered-modal pattern, red icon tint + red confirm button.

### Dark mode
Every screen supports a light/dark theme toggle (state persisted client-side in the prototype; should be persisted per-user server-side or in localStorage in production). Two token sets — recreate using **CSS variables or a theme object**, not the prototype's inline-style holes:

| Token | Light | Dark |
|---|---|---|
| Page background | `#FFFFFF` | `#15111F` |
| Page text (primary) | `#1C1526` | `#EDEBF3` |
| Page text (muted) | `#6E6779` | `#9A93B0` |
| Sidebar background | `#FAFAFC` | `#1B1730` |
| Sidebar / card border | `#ECE9F2` | `#2C2645` |
| Card / drawer background | `#FFFFFF` | `#1F1A35` |
| Card header / hover background | `#FAFAFC` / `#F2F0F6` | `#241E3D` |
| Sidebar hover background | `#F2F0F6` | `rgba(255,255,255,0.06)` |
| Icon color (topbar) | `#6E6779` | `#B4AEC2` |

Status/severity/role accent colors (green `#1A9E6B`, amber `#B45309`, red `#D6395B`, purple `#5A46C7`) are **not** theme-dependent — they stay the same in both modes (only their light background tints shift, e.g. badge fills).

---

## Design tokens

- **Font**: Manrope (400/500/600/700), loaded from Google Fonts. (Space Grotesk is loaded in `<helmet>` but not actually applied anywhere — safe to drop when rebuilding.)
- **Brand accent**: `#5A46C7` (purple), hover/active `#4C3AAE`.
- **Radius**: 8–9px controls, 12px cards/tables, 16px drawer/modal-adjacent large cards, 999px pills/badges/switches.
- **Shadows**: popovers/dropdowns `0 8px 24px rgba(28,21,38,0.12)`; drawers `-8px 0 24px rgba(28,21,38,0.1)`; modals `0 20px 48px rgba(28,21,38,0.22)`.
- **Status colors**: OK/resolved/active green `#1A9E6B` (bg `#E7F6EF`); warn/degraded/pending amber `#B45309` (bg `#FEF3E2`); down/critical/error red `#D6395B` (bg `#FBE9ED`).
- **Drawers**: right-side, fixed, `440–460px` wide, full height, `24px` padding, dark scrim backdrop (`rgba(28,21,38,0.32)` for a single drawer, `0.4` for confirm modals which sit above everything at `z-index:70`).

---

## Screens

### 1. Login Bootstrap — `Login Bootstrap.dc.html`
**Purpose:** Auth entry point — login, signup, forgot-password, covering both SaaS and self-hosted install modes.
**Layout:** Split screen — dark brand panel left (logo, tagline), light form panel right, centered form (max-width ~380px). A small "Self-hosted" label top-right of the form panel appears only in self-hosted mode.
**States/screens inside this one file:** Login, Signup, Forgot password, Forgot-password-sent confirmation. Self-hosted mode hides signup/social auth and shows "Acesso restrito a administradores desta instância."
**Backend:** real auth (login, signup, password-reset-request, password-reset-confirm) endpoints per install mode; session/JWT issuance; redirect to tenant selection or dashboard based on account's tenant count.

### 2. Dashboard Integrações — `Dashboard Integracoes.dc.html`
**Purpose:** Connect/manage third-party integrations the platform depends on.
**Layout:** Card grid (3 columns), grouped by category header + count ("Observabilidade · 1 integração", etc.).
**Cards:** icon tile (color-tinted), status badge (connected/plan-gated), description, and a contextual action:
  - **Datadog**: SLO/monitoring source for services.
  - **LLM Provider**: gated behind paid plans (Free shows an upsell note + "Fazer upgrade" linking to Planos & Faturamento); when connected, shows provider/model and "Editar conexão".
  - **Email (SMTP)**: self-hosted-only section (hidden entirely in SaaS mode) — outbound email + inbound webhook cards.
**Backend:** per-tenant integration-credentials storage (encrypted secrets), connection test endpoints, plan-gate check for LLM.

### 3. Serviços Monitorados — `Servicos Monitorados.dc.html`
**Purpose:** List and manage monitored services; core monitoring resource.
**Layout:** Header + status filter chips (Todos/Operacional/Degradado/Inativo, with counts) + search box + dense table + detail drawer + add drawer.
**Table columns:** Status badge, Serviço (name + url), Uptime 30d, Latência, Última verificação, chevron.
**Detail drawer:** status badge, name/url, 4-stat grid (uptime, latency, last check, incidents 30d), optional note/alert box, 24-bar check-history sparkline, "Pausar monitoramento" / "Editar configuração".
**Add drawer:** Nome, monitoring mode (SLO-based vs Polling manual — two selectable boxes), if SLO: source (Datadog/New Relic) + SLO picker; if polling: check type (HTTP/TCP/Ping), target field, interval (30s/1m/5m).
**Backend:** Service CRUD; HealthCheck/uptime history table (for the sparkline + uptime%); SLO import from Datadog/New Relic API; scheduler for manual polling at the chosen interval; incident linkage.

### 4. Domínios & Status Pages — `Dominios e Status Pages.dc.html`
**Purpose:** Manage verified domains and public/private status pages, via tabs.
**Layout:** Two tabs (Domínios / Status Pages), each its own table + detail drawer; one add drawer whose content switches by active tab.
**Domínios tab:** table (Status, Domínio, Tipo, Aponta para, SSL, Verificado). Detail drawer: status, type, SSL, verified-at, DNS config (CNAME row) for custom domains or an info note for Vane subdomains, error alert if verification failed, "Verificar novamente" / "Remover domínio".
**Status Pages tab:** table (Visibilidade, Página, URL pública, Serviços, Atualizado). Detail drawer: visibility badge, slug/URL, list of included services, associated domain, "Ver página pública" / "Editar página".
**Add drawer:** Domínios — type (subdomain vs custom, live preview of the resulting subdomain), custom-domain CNAME instructions. Status Pages — name, visibility (Público/Privado), service checklist.
**Backend:** Domain model (type, status, ssl_status, verified_at, cname target) + DNS verification job (real DNS lookups); StatusPage model (slug, visibility, services m2m, domain fk) + the actual public-facing status page renderer (not in this bundle — only the admin management UI is).

### 5. Incidentes — `Incidentes.dc.html`
**Purpose:** Track and resolve incidents, including AI-assisted closure.
**Layout:** Status filter chips (Todos/Investigando/Monitorando/Resolvido) + table + detail drawer + add drawer.
**Table columns:** Status, Incidente, Serviço, Severidade, Aberto em, Duração.
**Detail drawer:** status/severity, AI-summary card (purple-tinted, shown once resolved-with-AI), vertical timeline of updates (time + author + text), for open incidents: add-update textarea + "Resolver com resumo de IA" button (shows a spinner, then flips status to resolved and appends an AI-generated summary + timeline entry).
**Add drawer:** título, serviço afetado, severidade (Menor/Moderado/Crítico), descrição inicial.
**Backend:** Incident model (status, severity, service fk, timestamps) + IncidentUpdate model (ordered timeline); an actual **LLM call** to generate the closure summary (the prototype fakes this with a timeout — this is real integration work depending on the connected LLM Provider); notification fan-out on open/resolve (see Notificações).

### 6. Poller Status — `Poller Status.dc.html`
**Purpose:** Internal ops view of the monitoring infrastructure's own health (read-mostly).
**Layout:** Summary metric cards (pollers ativos, verificações/min, fila total, taxa de erro 24h) + degraded/offline alert banner + node table + detail drawer.
**Table columns:** Status, Região, Verificações/min, Fila, Latência média, Heartbeat.
**Detail drawer:** status/note, CPU/mem, throughput sparkline (24 bars), "Reiniciar poller" action.
**Backend:** this is infra telemetry, not user data — needs a metrics pipeline from the actual poller fleet (heartbeats, queue depth, CPU/mem) feeding an API this screen reads; "Reiniciar poller" needs a real orchestration action (e.g. restart a pod/process), not just a UI button.

### 7. Usuários — `Usuarios.dc.html`
**Purpose:** Manage tenant members, roles, and invitations.
**Layout:** Role filter chips (Todos/Admin/Membro/Leitura) + search + table + detail drawer + invite drawer. Free-plan gate: banner + disabled invite button once the user count hits the plan limit (3 on Free), with a "Fazer upgrade" CTA.
**Table columns:** Usuário (avatar + name + email), Papel, Status (Ativo/Pendente), Último acesso.
**Detail drawer:** role selector (Admin/Membro/Leitura boxes, changes immediately), "Reenviar convite" (pending only), "Remover usuário".
**Invite drawer:** email, role selection.
**Backend:** Membership model (user, tenant, role, status, invited_at, last_access); invite-email sending; plan-based seat limit enforcement (must be enforced server-side, not just the disabled button); role-based authorization must actually gate the actions elsewhere in the app.

### 8. Planos & Faturamento — `Planos e Faturamento.dc.html`
**Purpose:** Plan comparison + Stripe checkout (SaaS) OR annual license purchase + activation (self-hosted) + subscription management.
**Layout:** Current plan/license status banner. **SaaS mode:** 3-column plan grid (Free/Starter/Scale — R$0, R$49, R$149 per month in this mock; real prices TBD with the user), each with feature checklist and a contextual button (Plano atual / Fazer upgrade / Fazer downgrade); below, "Forma de pagamento" + "Faturas" cards. **Self-hosted mode:** single license card (R$1.490/ano) + "Ativar licença" card (paste emailed code to unlock).
**Checkout flow:** full-screen overlay styled like a hosted Stripe Checkout — dark order-summary panel (plan/license name, price, feature list) + light payment form (email, card number/expiry/cvc, name) → processing spinner → success screen. For a **subscription** this activates the plan immediately; for a **license purchase** it instead shows an issued license key and requires the separate activation step.
**Backend — the big one:** real Stripe integration (Checkout Sessions or Payment Intents for the one-time license, Subscriptions for SaaS plans, webhooks for renewal/failure/cancellation), a Subscription/License model per tenant, license-key generation + validation endpoint, payment-method + invoice retrieval from Stripe (not hardcoded), plan-based feature flags actually enforced across the app (user limits, AI-closure gate, domain/status-page limits, history retention) rather than just displayed.

### 9. Configurações — `Configuracoes.dc.html`
**Purpose:** Company profile + fiscal/billing-address data + tenant deletion.
**Layout:** "Perfil da empresa" card (logo upload, name, site, timezone, language) + "Dados fiscais" card (PJ/PF toggle, razão social/nome, CNPJ/CPF, inscrição estadual for PJ, full address) + Salvar/Descartar row + red "Zona de perigo" delete-tenant card (with confirm modal).
**Backend:** Tenant/Company profile model with the fiscal fields above; these fields likely need to flow into Stripe customer/invoice metadata for real invoicing; tenant deletion needs a real (probably soft-delete + grace period) cascade across services/incidents/status pages/users.

### 10. Meu Perfil — `Meu Perfil.dc.html`
**Purpose:** Personal account settings (distinct from the tenant-level Configurações).
**Layout:** Avatar + name/email card; Segurança card (change-password fields, 2FA status badge + activate/deactivate button); Notificações card (toggle switches per notification type); Sessões ativas card (device/location/last-active list, "Encerrar" per session with a confirm modal).
**2FA activation:** dedicated **drawer** flow, app-based only — step 1 shows a QR code (+ manual secret fallback) to scan with an authenticator app, step 2 asks for the 6-digit generated code, step 3 confirms activation.
**Backend:** real TOTP secret generation + QR provisioning URI, code verification against the secret (not "any 6 digits" like the mock), 2FA-enforcement at login once enabled; NotificationPreference model per user/type; Session model (device/IP/location/last_active) with real revocation (invalidate the token/session, not just remove a row).

### 11. Notificações — `Notificacoes.dc.html`
**Purpose:** Notification center, reached via the topbar bell (with an unread-dot badge) from every screen.
**Layout:** Todas/Não lidas filter chips + "Marcar todas como lidas" + a bordered list (icon tile per type, title, description, relative time, unread dot). Clicking a row marks it read.
**Types shown:** incident opened (critical, red), incident resolved (green), domain verified/failed (purple), user invited (neutral), poller degraded (amber), billing renewal (purple).
**Backend:** Notification model (user fk, type, title, body, read_at, created_at); real generation triggers from every event listed above (incident state changes, domain verification results, poller degradation, Stripe renewal webhooks, user invites) — this needs an event/notification-fan-out system, not just a seeded list; likely also needs a delivery mechanism (in-app poll/websocket at minimum) so the unread badge count is live.

---

## Data model & backend requirements (consolidated)

Cross-check these against the existing backend as the first step (see the callout at the top):

- **Account / Tenant**: id, name, logo, plan_tier (free/starter/scale), install_mode (saas/self_hosted), timezone, language, fiscal fields (person_type, legal_name, tax_id, state_registration, address).
- **Membership**: user_id, tenant_id, role (admin/member/viewer), status (active/pending), invited_at, last_access_at.
- **User**: id, name, email, password_hash, avatar, two_fa_secret, two_fa_enabled.
- **Session**: user_id, device, ip/location, last_active_at, revoked_at.
- **NotificationPreference**: user_id, type, enabled.
- **Integration**: tenant_id, provider (datadog/llm/smtp/webhook), credentials (encrypted), status.
- **Service**: tenant_id, name, target, monitor_mode (slo/polling), poll_type, poll_interval, slo_source, slo_ref, status, uptime, latency, last_check_at.
- **HealthCheck**: service_id, timestamp, result, latency.
- **Domain**: tenant_id, domain, type (subdomain/custom), status, ssl_status, cname_target, verified_at.
- **StatusPage**: tenant_id, name, slug, visibility, domain_id, services (m2m).
- **Incident**: tenant_id, service_id, title, severity, status, opened_at, resolved_at.
- **IncidentUpdate**: incident_id, author, text, created_at, is_ai_summary.
- **PollerNode**: region, status, checks_per_min, queue_depth, avg_latency, last_heartbeat, cpu, mem (infra telemetry, likely read-only from a metrics pipeline).
- **Subscription** (SaaS): tenant_id, stripe_customer_id, stripe_subscription_id, plan, status, current_period_end.
- **License** (self-hosted): tenant_id, key, stripe_payment_id, purchased_at, activated_at, expires_at.
- **Notification**: user_id, type, title, body, read_at, created_at, source_ref.

## Assets
- `assets/vane-icon.png` — app icon/mark.
- `assets/vane-lockup.png` — full logo lockup.
(These are referenced by the earlier auth/branding work in this project; confirm whether the target codebase already has its own brand assets before reusing these.)

## Files in this bundle
- `Login Bootstrap.dc.html`
- `Dashboard Integracoes.dc.html`
- `Servicos Monitorados.dc.html`
- `Dominios e Status Pages.dc.html`
- `Incidentes.dc.html`
- `Poller Status.dc.html`
- `Usuarios.dc.html`
- `Planos e Faturamento.dc.html`
- `Configuracoes.dc.html`
- `Meu Perfil.dc.html`
- `Notificacoes.dc.html`
- `support.js`, `image-slot.js` — runtime helper scripts these prototypes load; not needed in the target app, included only so the files still open standalone.
- `assets/` — the two brand images above.

Each `.dc.html` opens directly in any browser — double-click or drag into a tab — to click through the real interactive prototype (filters, drawers, dark-mode toggle, checkout flow, 2FA drawer, etc.) alongside this document.
