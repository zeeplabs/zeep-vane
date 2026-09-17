# Billing & Plans Page Specification

## Problem Statement

`handoff-new-layout/Planos e Faturamento.dc.html` shows a full billing product: SaaS plan tiers (Free/Starter/Scale) with Stripe-style checkout (card number/expiry/CVC fields, "Pagamentos processados... pela Stripe"), upgrade/downgrade, payment-method management, an invoices list, and a parallel self-hosted flow (buy an annual license, paste a license key to activate, license-status banner). None of this exists in the backend today:

- **No Stripe integration anywhere in the repo** - no client, no webhook handler, no `stripe` dependency. `tenants.plan` is a free-text column (default `'free'`) with zero enforcement code anywhere (confirmed this session while investigating `AD-025`'s seat-limit pause).
- **No license-key system**. `zeep-license-server` (`/Users/juliosousa/Projects/ZeepLabs/baas/zeep-license-server`) exists as a separate Zeep-internal repo meant to centralize plan/pricing/feature data and issue offline-verifiable (Ed25519) licenses across Zeep products - but **no product consumes it yet**, not even `zeep-orbit` (verified directly in `zeep-orbit`'s code this session, despite that server's own README claiming otherwise). `AD-025` (2026-09-10, `.specs/STATE.md`) already paused any plan/license work in Vane pending a dedicated cross-repo integration spec that has not been written.

Decision (Julio, 2026-09-15, via `AskUserQuestion`): build this screen as a **decorative showcase** - visually complete per the mock, but every action that would need real money movement or a real license grant shows a "coming soon" state instead of firing a real request. This mirrors the same decision already made this session for the mock's New Relic card (`dashboard-integrations-page`) and OAuth buttons (`auth-pages-redesign`): show the real visual, never fabricate the backend behind it.

## Goals

- [ ] New route `/billing` (sidebar item "Planos & Faturamento", Organização group, after "Usuários" - matching the mock's nav position) rendering a new `BillingPage`.
- [ ] Deployment-mode-aware layout (reusing the existing `deploymentMode` from `AuthProvider`, `deployment-mode` feature): `saas` mode shows the 3-tier plan grid + payment-method/invoices cards; `self_hosted` mode shows the license banner + "buy license"/"activate license" cards - exactly the mock's own `installMode` branching, now driven by the app's real signal instead of an editor toggle.
- [ ] Plan tiers (Free/Starter/Scale) and the license tier rendered as static, real content (name/price/feature list ported verbatim from the mock's `PLAN_DEFS`/`LICENSE_DEF`) - this part is just copy, not a fabricated capability.
- [ ] Current plan badge in the sidebar (mock's `planBadgeLabel`) driven by the tenant's real `plan` field (`GET /api/auth/me`'s membership data, already exposed - `tenant_membership_repository.go`'s `Plan` field) - real data, not a placeholder.
- [ ] Every action that would need a real integration (upgrade/downgrade a SaaS plan, submit a card, buy or activate a license) is **decorative**: clicking shows a "Em breve" toast/banner, no checkout drawer opens, no request is sent, no `currentPlan`/`licenseActive` state changes client-side. No card-number input is ever rendered (collecting raw PAN in a UI with no real Stripe Elements/tokenization behind it would be a fake-but-functional-looking payment form - a stronger anti-pattern than simply omitting the capability).
- [ ] Invoices/payment-method cards render their mock's own "nothing yet" copy unconditionally (`noPaymentMethodNote`/`invoicesNote` - the mock's own empty-state text), never the "has a card"/"has invoices" branch, since no real payment method or invoice can exist.

## Out of Scope

| Item | Reason |
| --- | --- |
| Real Stripe checkout (card fields, subscription creation, webhooks) | No Stripe account/keys/dependency exists in this codebase; building it is its own multi-week integration, not a UI reskin. Decorative-only per this session's decision. |
| Real license-key issuance/activation | Blocked on `AD-025` - the Vane↔`zeep-license-server` integration is an explicit, undecided cross-repo architecture decision, out of reach of a single UI spec. |
| Plan-based feature gating/enforcement anywhere in the app (seat limits, status-page limits, etc.) | Same `AD-025` blocker. This spec only *displays* the current plan; it does not enforce anything new. |
| Payment method management ("Atualizar" card) | No real payment method can exist without Stripe; the button is part of the decorative showcase, not wired to any flow. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Overall scope: real billing vs. decorative showcase vs. paused | Decorative showcase | Explicit user decision via `AskUserQuestion`, matching the New Relic/OAuth precedent already set this session for mock capabilities the backend can't back. | y |
| Self-hosted vs. SaaS branching signal | Reuse `AuthProvider.deploymentMode` (`deployment-mode` feature) | Already exists, already the exact `self_hosted`/`saas` binary the mock's own `installMode` prop models - no new signal needed. | y - codebase (`web/src/auth/AuthProvider.tsx`) |
| Current plan badge/name | Real `tenant.plan` string, rendered as-is (no enum validation, no "unknown plan" fallback UI beyond a plain label) | Column already exists and is already exposed via `ListForUser`'s `Plan` field; showing the raw value is honest, whereas inventing a fixed enum→label map would silently hide a plan value nobody has decided how to sunset yet. | y - codebase (`internal/db/tenant_membership_repository.go`) |
| Card-number-style inputs | Never rendered, at all | A fake-looking payment form that "works" (accepts input, shows a spinner, "succeeds") but sends nothing anywhere is a worse anti-pattern than an honest "Em breve" state - it can mislead a real customer into thinking they're subscribed. | y |
| Sidebar item visibility by mode (2026-09-16 revision, supersedes original AC1) | Hidden entirely WHILE `deploymentMode === "self_hosted"`; visible only WHILE `deploymentMode === "saas"` | User decision: self-hosted has nothing actionable to show yet — no upgrade/license flow exists in the sidebar today. The eventual self-hosted flow is a future feature (license-block UI with a "Comprar licença"/"Registrar licença" action that redirects to the Vane marketing site to purchase, then the user pastes the resulting license key back into the dashboard to activate) — out of scope here, tracked as a follow-up, not built now. | y |

**Open questions:** none — todas resolvidas acima.

---

## User Stories

### P1: Decorative billing/plans showcase ⭐ MVP

**User Story**: Como usuário autenticado, quero ver a tela de Planos & Faturamento com o mesmo visual do handoff, para entender o que existe hoje e o que vem a seguir, sem ser levado a acreditar que pode de fato assinar ou ativar uma licença agora.

**Acceptance Criteria**:

1. The system SHALL add a `/billing` route and a sidebar entry "Planos & Faturamento" (Organização group, after "Usuários"), reachable by any authenticated role (read-only showcase, no role gate needed since nothing mutates). **Revised 2026-09-16**: the sidebar entry itself SHALL only render WHILE `deploymentMode === "saas"` — it SHALL NOT render WHILE `deploymentMode === "self_hosted"` (no role gate either way, only the deployment-mode gate is new). The `/billing` route itself is unaffected by this revision.
2. WHILE `deploymentMode === "saas"`, the page SHALL render the current-plan banner + 3-tier plan grid (Free/Starter/Scale, static content from the mock) + payment-method card (always showing the "no card" empty state) + invoices card (always showing the "no invoices yet" empty state).
3. WHILE `deploymentMode === "self_hosted"`, the page SHALL render the license-status banner (always "Nenhuma licença ativa" / "Recursos padrão bloqueados" - no real license state exists) + the two license cards (buy / activate), matching the mock's `licenseInactive` branch only.
4. Clicking "Fazer upgrade"/"Fazer downgrade" (SaaS) or "Comprar licença"/"Ativar licença" (self-hosted) SHALL show a decorative "Em breve" toast (same pattern as `OAuthButtons`/the New Relic card) and SHALL NOT open a checkout UI, SHALL NOT render any card-number/CVC/expiry input anywhere, and SHALL NOT change any client-side state (no plan switch, no license activation).
5. The sidebar's plan badge (next to the tenant name) SHALL show the real `tenant.plan` value from the authenticated membership data, not a hardcoded "Free".
6. The page SHALL NOT gate or hide any other existing feature based on the displayed plan (no new enforcement anywhere else in the app).

**Independent Test**: as a `saas`-mode tenant, open `/billing`, confirm the 3 plan cards render with real copy and clicking "Fazer upgrade" shows a toast with no checkout UI appearing; switch a test fixture to `self_hosted`, reload, confirm the license cards render instead and "Comprar licença"/"Ativar licença" both show the same toast-only behavior.

---

## Edge Cases

- IF `tenant.plan` is an unrecognized string (not `free`/`starter`/`scale`) THEN the sidebar badge SHALL render it verbatim (no crash, no silent fallback to "Free") - honest display of unknown backend state beats fabricating a default.
- IF the tenant has no `plan` value at all (empty string, legacy row) THEN the badge SHALL render the same empty-string behavior already established by `ListForUser`'s existing contract (no backend default invented client-side either).

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| BILLPG-01 | P1: Decorative showcase | - | Verified |
| BILLPG-02 | P1: Decorative showcase | - | Verified |
| BILLPG-03 | P1: Decorative showcase | - | Verified |
| BILLPG-04 | P1: Decorative showcase | - | Verified |
| BILLPG-05 | P1: Decorative showcase | - | Verified |
| BILLPG-06 | P1: Decorative showcase | - | Verified |

**ID format:** `BILLPG-[NUMBER]`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 6 total, 6 mapeados a tarefas (implícitas, escopo Medium), 0 unmapped

---

## Success Criteria

- [ ] Page visually matches the mock's plan grid / license cards for both deployment modes.
- [ ] No real Stripe/license request ever fires from this page; every mutating action is decorative.
- [ ] Sidebar plan badge reflects real `tenant.plan`.
- [ ] `tsc -b --noEmit` and the full frontend suite green; new tests cover both deployment-mode branches and the decorative-action no-op guarantee.
