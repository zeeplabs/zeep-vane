# Handoff: Vane Admin Dashboard

## Overview
Admin SPA for **Vane** — a status-page / uptime-monitoring product (the design's working name; rename if the product has an official one). The admin connects Datadog, registers domains, publishes public status pages with automatic TLS, and manages incidents, all from this authenticated panel. Backed by the Go service at `zeep-vane` (see `internal/api/*_handler.go`) — this dashboard is the frontend that service currently lacks.

## About the Design Files
The files in this bundle (`Admin Frontend.dc.html`, `nocturne-styles.css`) are **design references built in HTML** — they show the intended layout, states, copy, and interaction model, not production code to copy verbatim. Your task is to **recreate this design in the target codebase's environment**. The `zeep-vane` repo is currently Go-only (`internal/api`, `internal/db`, `internal/poller`, …) with no frontend scaffold, so pick the frontend stack that best fits how this Go service will serve/embed it — a React or Vue SPA built separately and served as static assets by the Go binary is the natural default if the team has no existing preference. Use the HTML file's DOM structure and inline styles as the ground truth for spacing, color, and copy; do not literally ship the `.dc.html` file.

## Fidelity
**High-fidelity.** Colors, type, spacing, radii and copy are final — recreate pixel-close using your framework's styling approach (CSS modules, styled-components, Tailwind config, etc.), driven by the same design tokens listed below rather than hard-coded values.

## Design system
Built on **Nocturne**, a small dark-mode design system (bundled here as `nocturne-styles.css`). Nocturne ships as one plain CSS file — no component library/JS — with the classes below. Recreate these classes' visual output in your framework's component layer (e.g. a `Button`, `Card`, `Tag`, `Table`, `Dialog` component each), reading colors/spacing from the token list, not by linking the CSS file into a real app.

### Design tokens
```
Ground:      --color-bg #161826   (page background)
Surface:     --color-surface #232532  (cards, inputs, dialogs)
Text:        --color-text #e9e9ed
Accent:      --color-accent #9184d9  (blurple — outlines, links, active states, focus ring)
Accent-2:    --color-accent-2 #a7a1db (same hue, treat as one role with accent)
Divider:     color-mix(in srgb, #e9e9ed 16%, transparent)

Neutral ramp (100→900, light→dark): #f3f5fe #e4e7f5 #cfd3e5 #b2b6ca #9397ab #75798c #595d6c #3f424d #292b31
Accent ramp (100→900):              #f5f4ff #e7e5fe #d2cefd #b5abfc #968ae0 #796cbf #5d5294 #423a6a #2b2741

Extended semantic colors added for this dashboard (Nocturne itself is mono-accent;
these 3 were derived in OKLCH at the same lightness/chroma family as the accent,
used ONLY for status severity — never as large fills, same restraint as the accent):
  --color-success  oklch(0.72 0.135 152)   (operational / published / poller OK)
  --color-warning  oklch(0.78 0.15 80)     (degraded)
  --color-critical oklch(0.685 0.19 25)    (down / failed / poller failure / destructive)

Font:        Inter — headings weight 500 (never bolder), body weight 400
Type scale:  h1 42px, h2 32px, h3 25px, h4 20px, h5 16px, h6 13px (uppercase, tracked)
Spacing:     2.8 / 5.6 / 8.4 / 11.2 / 16.8 / 22.4 px (4px grid × 0.7 density)
Radius:      sm 4px, md 8px, lg 14px
Shadows:     sm  0 0 0 1px #3f424d
             md  0 0 0 1px #595d6c, 0 6px 18px rgba(0,0,0,.55)
             lg  0 0 0 1px #9397ab, 0 16px 40px rgba(0,0,0,.65)  — NOTE: for modals we
             override the ring to `var(--color-divider)` (not neutral-500) so it doesn't
             read as a bright/white border on the dark ground.
Icons:       Phosphor (regular weight), inline SVG, currentColor, ~15–17px in UI
```

### Core components to build
- `Button` — variants primary (accent 1px outline, transparent fill, never solid), secondary (divider outline), ghost (no border, accent text), icon (36×36 square, no label). Hover = tint fill one step; press = deeper tint; disabled = 45% opacity.
- `Input` / `Field` — label above, 36px min-height, surface background, divider border, accent border + no outline-offset on focus.
- `Tag` — small pill, 11px text, variants: accent (filled), neutral (filled), outline (1px accent border). Status tags use the 3 extended semantic colors via `color-mix()` tinted background + tinted text, never a flat saturated fill.
- `Table` — plain rows, uppercase 11px tracked headers, hairline row rules that fade to transparent 48px from each edge (a Nocturne signature — don't use a hard full-width border).
- `Card` — surface background, 8px radius, `elev-sm/md/lg` shadow steps.
- `Dialog` + backdrop — centered, 28px internal padding, divider-ring shadow (see note above), max-width ~360–440px depending on content.
- `Seg` (segmented control) — used for the role switcher and Ativos/Resolvidos tabs; 1px divider border, active segment gets accent text + inset accent ring.
- Icon-only role indicators (see Admins screen) — three inline-SVG buttons (shield / wrench / eye) per row; the admin's current role is the one rendered in accent color + accent-tinted background, the other two sit at 40% opacity.

## Screens / Views

All authenticated screens share a persistent **shell**: a 236px-wide left sidebar (brand mark "Vane" + lightning-bolt-style star icon, then nav) and a content column to its right. The content column can carry a full-width banner above the scrollable content area.

### 0. Login
- **Purpose**: authenticate before reaching the app shell.
- **Layout**: full-viewport centered column; brand mark, then a `card elev-md` (max-width 380px) containing the form.
- **Components**: e-mail field, password field with an eye-icon visibility toggle, "Esqueci minha senha" link (accent, 12.5px), primary block-width "Entrar" button, and a small "Pré-visualizar estado de erro" ghost text toggle purely for this design's review purposes (drop it in the real build).
- **Error state**: generic inline alert above the form — critical-tinted background + border, warning-circle icon, copy: "E-mail ou senha inválidos." Per the backend (`auth_handler.go`), never reveal which field was wrong.

### 1. Integrações (Datadog)
- **Purpose**: connect the Datadog integration, then link services to Datadog SLOs.
- **Layout**: single column, max content width ~920px.
- **Datadog connection card**: icon tile + "Datadog" title + masked-key caption ("Chave: •••• •••• •••• 8f2a") + last-checked timestamp + a status tag ("Conectado", success-tinted). "Rotacionar chave" button (hidden for `viewer`) expands an inline two-field form (API key / App key, both `type=password`) with a caption noting keys are validated against Datadog and never re-shown after saving (`integrations_handler.go`: `ConnectDatadog` never echoes the key back).
- **Empty state** (not built in the reference, but required): when no integration exists yet, this card becomes a single CTA "Conectar Datadog" opening the same two-field form — no metadata to show yet.
- **Services table**: columns Serviço, SLO vinculado, Status, Última mudança. Status is one of 4 states — Operacional (success), Degradado (warning), Inoperante (critical), Não configurado (neutral tag) — mirroring `service.CurrentStatus` (`not_configured` is the DB default until the poller runs once). "Vincular serviço" button (hidden for `viewer`) opens a dialog: service name field + an SLO search input + a filtered result list to pick one SLO.

### 2. Domínios & Status Pages
- **Purpose**: register root domains, then publish a status page under a subdomain of one of them.
- **Layout**: two stacked sections, same column width as Integrações.
- **Domínios table**: Hostname, Cadastrado em. "Adicionar domínio" opens a dialog with a hostname field; demonstrate the 409-conflict inline error state (duplicate hostname) as a persistent example under the field — red icon + "Esse hostname já está cadastrado." (`domains_handler.go` returns 409 `duplicateDomainBody` on exact-duplicate hostname only).
- **Status pages table**: Nome, Subdomínio, Estado. Four state renderings map 1:1 to `status_page.State`:
  - `draft` → neutral "Rascunho" tag
  - `issuing` → accent "Emitindo certificado" tag (this is the async/polling state — consider a subtle pulsing dot)
  - `published` → success "Publicada" tag + a link to the live public URL
  - `failed` → critical "Falha" tag + the failure reason from `tls_last_error` shown as a small caption under the tag
  "Criar status page" opens a dialog: name, subdomain, domain picker, and a services multi-select rendered as toggleable outline tags.

### 3. Incidentes
- **Purpose**: create/manage incidents, most-recent-update-first timelines, reopen resolved incidents.
- **Layout**: single column, full width (matches Integrações' width discipline).
- **Tabs**: a `seg` control — Ativos / Resolvidos.
- **Ativos**: one card per incident — status tag (Investigando/Identificado/Monitorando, all rendered as an accent tag; only the label text changes per `incident.Status`), title, affected-service tags, created timestamp. "Ver timeline (n)" expands an inline panel: reverse-chronological update list (dot + body + timestamp) matching `ListUpdates` order, an add-update input + "Publicar" (POST `/api/incidents/{id}/updates`), and quick status-transition buttons (Identificado / Monitorando / Marcar como resolvido → `PATCH /api/incidents/{id}`). All write affordances hidden for `viewer`.
- **Empty state**: centered check-circle icon + "Nenhum incidente ativo" + "Todos os serviços monitorados estão operando normalmente."
- **Resolvidos**: same card shape, neutral "Resolvido" tag, resolved date, and (non-viewer) a "Reabrir incidente" ghost button with a reload icon — `Transition` explicitly allows moving a resolved incident back to `investigating` (see code comment in `incidents_handler.go`).
- **Novo incidente** dialog: title field + a services multi-select (outline tags, click to toggle).

### 4. Admins (RBAC) — owner-only
- **Purpose**: manage who has dashboard access and at what role. Router-level `RequireRole` in the backend restricts this whole screen (and its nav entry) to `owner` — hide the nav item entirely for non-owners, don't just disable the buttons.
- **Ativos table**: E-mail, Papel (see icon-role component below), and a trash-icon-only remove button that opens a confirmation dialog ("Remover admin" / "Remover o acesso de {email}? Esta ação não pode ser desfeita.") before calling `DELETE /api/admins/{id}`. The backend can reject with 409 `adminLockoutBody` if this would leave zero owners — surface that as a toast/inline error, don't just fail silently.
- **Papel column**: NOT a `<select>`. Three small icon buttons per row — shield (Owner), wrench (Operator), eye (Viewer) — the admin's current role renders in accent color with an accent-tinted background and accent border; the other two icons sit at ~40% opacity with a neutral border. Clicking a different icon should trigger the same confirmation-then-`PATCH /api/admins/{id}/role` flow (the reference build applies it optimistically for demo purposes only).
- **Convites pendentes table**: E-mail, Papel, a "Pendente" outline tag, Reenviar / Cancelar actions — Cancelar needs the same destructive-confirmation pattern.
- **Convidar admin** dialog: e-mail field + a role `seg` control (Owner/Operator/Viewer) → `POST /api/admins`.

### 5. Status do Poller
- **Purpose**: read-only view of each connected integration's last poll (`GET /api/poller/status`).
- **Layout**: single table — Integração, Última execução, Resultado (Sucesso = success tag / Falha = critical tag), Mensagem de erro (populated only on failure, from `last_error`). No write affordances for any role.

### 6. Global poller-failure banner
- Not a screen — a full-width strip pinned above the content area on every authenticated route, shown only while at least one integration's last poll failed. Critical-tinted background, warning-triangle icon, one-line copy, and a "Ver detalhes" ghost button that navigates to Status do Poller.

### 7. "Sessão expirada" modal
- Blocking overlay over any screen (highest z-index), centered dialog, lock icon, single primary CTA "Ir para o login" that clears the session and returns to Login. No dismiss-by-backdrop-click (unlike the other dialogs) — this one is intentionally inescapable except via its CTA.

### 8. Logout confirmation modal
- Triggered from the sidebar's "Sair" control. Standard dialog: title "Sair do painel", body "Tem certeza que deseja encerrar sua sessão?", Cancelar (secondary) / Sair (primary) actions.

## RBAC rules (apply everywhere, not just on Admins)
Three roles, from `internal/db/admin_repository.go`: `owner`, `operator`, `viewer`.
- `viewer` sees **no write affordance anywhere** — no buttons that create/edit/delete/invite/reopen/rotate keys. This is a hide, not a disable.
- `operator` has every write capability except admin management.
- `owner` has everything, including the Admins screen and its nav entry (hidden entirely for the other two roles, not just gated on click).
- Every destructive action (remove admin, cancel invite) requires a confirmation dialog before executing — never fire on first click.

## Interactions & Behavior
- Dialogs open centered over a scrim; clicking the backdrop or a "Cancelar" closes them (except the session-expired modal, see above).
- 409 (conflict) and 422 (validation) API errors must keep the user's typed input in place and show the error inline near the offending field when the API names a specific field (see the domain hostname example); otherwise use a toast and keep the last valid view rendered — never blank the screen on a network/API error.
- Incident timelines render most-recent-update-first (already the order `ListUpdates`/`AddUpdate` return).
- Motion: short (~150–250ms) ease-out fades/slides only, per Nocturne's own motion guidance — no bounce, no decorative looping animation.

## State Management
Minimum state to model per the reference build:
- `session`: authenticated / expired / logged-out (drives Login vs. shell vs. session modal)
- `currentAdmin.role`: drives every RBAC check above
- Per-screen list data (services, domains, status pages, incidents active/resolved, admins, pending invites, poller rows) — fetch on route entry, refetch after any mutating action
- Per-row/panel UI state: which incident's timeline is expanded, which dialog is open, segmented-tab selection (Ativos/Resolvidos, role switcher in the reference is dev-only and should not exist in production)
- Poller-failure banner visibility is derived from the poller-status list (`some row not ok`), not its own flag

## Backend endpoints already implemented (zeep-vane, Go)
Wire the UI to these — do not invent new response shapes without confirming against the handler source:

| Action | Endpoint | Handler file |
|---|---|---|
| Connect Datadog | `POST /api/integrations/datadog` | `integrations_handler.go` |
| Datadog status | `GET /api/integrations/datadog/status` | `integrations_handler.go` |
| Link service to SLO | `POST /api/services` | `services_handler.go` |
| List services | `GET /api/services` | `services_handler.go` |
| Register domain | `POST /api/domains` | `domains_handler.go` |
| Create status page | `POST /api/status-pages` | `status_pages_handler.go` |
| Create incident | `POST /api/incidents` | `incidents_handler.go` |
| Add incident update | `POST /api/incidents/{id}/updates` | `incidents_handler.go` |
| Transition/reopen incident | `PATCH /api/incidents/{id}` | `incidents_handler.go` |
| Invite admin | `POST /api/admins` (owner) | `admins.go` |
| Accept invite | `POST /api/admins/invite/{token}/accept` (public) | `admins.go` |
| Change admin role | `PATCH /api/admins/{id}/role` (owner) | `admins.go` |
| Remove admin | `DELETE /api/admins/{id}` (owner) | `admins.go` |
| List admins | `GET /api/admins` (owner) | `admins.go` |
| Poller status | `GET /api/poller/status` | `poller_status.go` |
| Login / password reset | — | `auth_handler.go`, `password_reset_handler.go` |

Notable backend behaviors the UI must respect: API keys are never re-sent after saving; role changes/removals are rejected with 409 if they'd leave zero active owners; incidents can transition back to `investigating` from `resolved` (reopening is a normal transition, not a special case).

## Assets
No photography or custom illustration — the design is entirely typographic/tabular, using inline Phosphor-style SVG icons (hand-drawn to match Phosphor's regular weight/proportions for this prototype; swap in the real `@phosphor-icons` package in production). No external image assets to hand off.

## Files in this bundle
- `Admin Frontend.dc.html` — the full interactive reference covering all 7 screens + banner + 2 modals + the RBAC role switcher (dev-only control, not for production).
- `nocturne-styles.css` — the Nocturne design-system stylesheet the reference is built on; read it for exact token values and class behavior, don't ship it as-is into a component framework.
