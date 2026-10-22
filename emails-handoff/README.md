# Handoff: Vane — Transactional Email Templates (6)

## Overview
Six transactional emails for Vane (status-monitoring / incident SaaS). The files in `templates/` are **final, send-ready HTML** (table layout, inline styles, bulletproof buttons, hidden preheader, 600px container, mobile media query) populated with **sample data**. Your job: turn each into a real template in the target codebase, replace sample values with variables, and wire each one to the event that triggers it.

| Key | File | Trigger |
|---|---|---|
| `admin_invite` | `templates/admin_invite.html` | Admin invites a user (Usuários screen) |
| `password_reset` | `templates/password_reset.html` | "Esqueci minha senha" on login |
| `signup_verification` | `templates/signup_verification.html` | New account signup |
| `incident_opened` | `templates/incident_opened.html` | Incident created (auto via poller or manual) |
| `incident_resolved` | `templates/incident_resolved.html` | Incident status → resolved |
| `weekly_digest` | `templates/weekly_digest.html` | Scheduled job, weekly (Monday ~09:00 tenant TZ) |

## ⚠️ Do this FIRST
1. **Inspect the existing email setup** in the codebase: provider (SES, SendGrid, Resend, Postmark, SMTP/nodemailer…), template engine (Handlebars, MJML, React Email, Go `html/template`, Jinja…), where templates live, how they're rendered and sent. **Reuse it** — do not introduce a new provider or engine.
2. If templates already exist for any of these 6 keys, **replace their markup** with these designs, keeping the existing send pipeline and variable names where possible.
3. Check whether each trigger event already exists in the backend (invite tokens, reset tokens, verification codes, incident events, digest aggregation). List missing pieces before coding.

## Implementation rules (email-client safety — do not "modernize")
- Keep the **table-based layout and inline styles exactly as-is**. No flexbox/grid, no external CSS, no `<div>` layout, no CSS variables. Outlook/Gmail break otherwise.
- If the codebase uses a component engine (React Email / MJML), port faithfully and compare rendered output against the HTML files side by side.
- Font stack is intentionally `Arial, Helvetica, 'Segoe UI', sans-serif` — do not swap to Manrope/web fonts.
- **Logo**: the files embed `vane-icon.png` as a base64 data URI. **Replace it** with a hosted absolute URL (`{{assets_base_url}}/vane-icon.png`, upload `assets/vane-icon.png`) — Gmail and several ESPs strip data-URI images. Render at `width="30" height="24"`, always keep `alt="Vane"`.
- Every CTA has a plain-text fallback link below it (where present) — keep both in sync with the same URL variable.
- Preheader is the first hidden `<div>` in `<body>` — make it a variable per template.
- Generate a **plain-text version** of each email (multipart/alternative) from the same variables.
- Escape all user-provided values (names, incident titles, notes).
- Dates/times: format in the **tenant's timezone**, pt-BR (`10 set às 16:02`, `8–14 set 2026`).

## Shared structure (all 6)
```
[page bg #F1EFF7, 32px padding]
  [card 600px, white, radius 16px]
    top bar 4px (color varies per template)
    header: icon 30×24 + "Vane" 19px bold
    body: kicker/badges → H1 → paragraph → content block → CTA → fine print
    divider + footer (help/prefs links, company address)
  [below card: "Enviado por Vane · Monitoramento de infraestrutura em tempo real"]
```
Recommend extracting a shared **layout/partial** (head, preheader, header, footer) and one partial per reusable block (button, detail table, badge, quote box).

## Design tokens (hex — use literally)
- Purple (CTA, kicker, links): `#5A46C7`, hover `#4C3AAE`, tint bg `#F2F0FA`
- Ink `#1C1526` · Muted `#6E6779` · Muted light `#9A93A8` · Border `#ECE9F2` · Page bg `#F1EFF7`
- Success: dot `#3DD68C`, text `#146C4A`, bg `#E7F6EF`
- Critical: `#D6395B`, bg `#FBE9ED`
- Warning: `#B45309`, bg `#FEF3E2`
- Type: H1 24px (22px on incident emails) bold, -0.01em · body 14.5px/1.65 · kicker 11px bold uppercase 0.09em · fine print 12–12.5px
- Button: radius 9px, padding 13×28, 14px bold white
- Top bar color: purple for account emails + digest, **red** `incident_opened`, **green** `incident_resolved`

## Templates & variables
Sample values in the HTML → variable to replace them with.

### 1. admin_invite
Subject: `{{inviter_name}} convidou você para o Vane`
- `Ana Silva` → `inviter_name`; `ana.silva@acme.health` → `inviter_email`
- `Acme Health` → `org_name`; `Administrador` → `role_label` (map role enum → pt-BR label)
- `https://app.vane.io/invite/8f2c1a` → `invite_url` (signed, single-use token)
- `7 dias` → `expires_in` (match backend token TTL)

### 2. password_reset
Subject: `Redefinir sua senha do Vane`
- `ana.silva@acme.health` → `user_email`
- reset URL → `reset_url`; `1 hora` → `expires_in`
- Never reveal whether an email exists — always respond 200 on the request endpoint.

### 3. signup_verification
Subject: `Confirme seu email no Vane`
- verify URL → `verify_url`; `482913` → `verification_code` (6 digits); `15 minutos` → `code_expires_in`
- If the backend supports only link **or** only code, remove the other block (including the "ou" divider).

### 4. incident_opened
Subject: `[Crítico] Novo incidente: {{incident_title}}`
- `Webhook Receiver fora do ar` → `incident_title`
- `CRÍTICO` badge / `Crítico` row → `severity_label` (badge + top bar color by severity: critical = red; major/minor = warning amber `#B45309`/`#FEF3E2`)
- `10 set às 16:02` → `opened_at`; `Acme Health` → `org_name`
- Quote box → latest timeline entry: `first_update_time` + `first_update_message`
- Detail rows: `service_name`, `severity_label`, `status_label` (e.g. Investigando), `duration` (omit row if sent at open time)
- `https://app.vane.io/incidentes/i2` → `incident_url`
- Recipients: tenant admins + users subscribed in Notificações/Meu Perfil preferences.

### 5. incident_resolved
Subject: `Resolvido: {{incident_title}}`
- `incident_title`, `resolved_at`, `org_name`
- Quote box → `resolution_summary` (postmortem/resolution note; hide box if empty)
- Detail rows: `service_name`, `total_duration`, `opened_at`, `resolved_at`
- `incident_url`. Same recipient rules as incident_opened.

### 6. weekly_digest
Subject: `Seu resumo semanal · {{period_label}}`
- `8–14 set 2026` → `period_label`; `org_name`
- Stats: `avg_uptime` (green text), `incident_count`, `mttr`
- **Services list** (loop): `name`, `uptime_pct` (2 decimals), `status_label`, `status_color` (green `#146C4A` if ≥ 99.5%, amber `#B45309` otherwise). Bar = 2-cell table, 100px wide: filled cell width = round(uptime) (100 if ≥ 99.9), fill green `#3DD68C` / amber `#B45309`. Last row has no bottom border. Cap at ~10 services, then "Ver todos" via CTA.
- **Incidents list** (loop): `title`, `service_name`, `duration`, status pill (RESOLVIDO = green, MONITORANDO = amber, INVESTIGANDO = red). Hide section if zero incidents (replace with a short "Nenhum incidente nesta semana." line).
- `dashboard_url`
- Footer extras: `frequency_settings_url`, `unsubscribe_url` (required — also send `List-Unsubscribe` header).

### Footer (all)
- `help_url`, `notification_prefs_url`, company address line (confirm real address — current one is placeholder).

## Assets
- `assets/vane-icon.png` — logo mark for the email header (host it; see logo rule above).

## Files in this bundle
- `templates/*.html` — the 6 final designs with sample data (open in a browser to view).
- `assets/vane-icon.png`
- `README.md` — this spec.

## Acceptance checklist
- [ ] All 6 render identically to the reference HTML (desktop + 375px mobile)
- [ ] Tested in Gmail (web + app), Outlook (desktop), Apple Mail
- [ ] Logo served from absolute URL, no data URIs
- [ ] Plain-text alternative for every email
- [ ] Variables escaped; dates in tenant TZ, pt-BR
- [ ] Each template wired to its trigger + covered by a render/snapshot test
