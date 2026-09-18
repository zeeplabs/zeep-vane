# Integrations Card Actions Specification

## Problem Statement

`provider-disconnect` built working Activate/Disconnect logic (hooks, confirmation dialog, tests) inside `EmailProvidersPage.tsx` and `AISettings.tsx` - but neither component is mounted on any route. The real, user-reachable provider-management screen is `IntegrationsPage.tsx` (`/integrations`), whose `IntegrationCard`-based UI only ever offers "Conectar" / "Editar conexão" - no way to activate a connected-but-inactive provider, and no way to disconnect one. This was a scoping miss in `provider-disconnect`'s spec (it assumed the existing Connect/Activate UI was already live and only needed a Disconnect action added next to it - that assumption was wrong and went unverified). This feature closes that gap by moving the actions into the real screen and removing the now-fully-orphaned pages.

## Goals

- [ ] An owner/operator can activate a connected-but-inactive email or LLM provider directly from `/integrations`.
- [ ] An owner/operator can disconnect a connected email or LLM provider directly from `/integrations`, with the same confirmation-dialog safeguard `provider-disconnect` already specified.
- [ ] `EmailProvidersPage.tsx` and `AISettings.tsx` (and their tests) are deleted once their logic is not needed anywhere else - no orphaned, unreachable code left behind.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Datadog / New Relic activate-or-disconnect actions | Neither has a multi-provider "active" concept (single integration, binary connected/not) - `provider-disconnect` never touched Datadog and this feature doesn't either. |
| Changing `IntegrationCard`'s props/layout | The card's `action` slot already accepts an arbitrary `ReactNode` - a `<div>` with two buttons fits without changing the shared component. |
| A visual redesign of the integrations grid | Only the card footer's actions and a new "Ativo" state gain new UI; everything else (banners, grid, categories) is unchanged. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Where the actions land | Extend the existing `EmailProviderCard`/`LLMProviderCard` (in `IntegrationsPage.tsx`) rather than mounting the orphaned pages as new routes | User's explicit choice - keeps one UI (cards) for provider management instead of two coexisting UIs (cards for connect, a separate table page for activate/disconnect). | y |
| Distinguishing "connected" from "active" | A provider that is connected but not the `active_provider` shows status `connected` (existing "Conectado" pill, unchanged) plus a distinct "Ativar" button; the currently active one shows a new "Ativo" pill/meta and no Ativar button (nothing to activate) | Today's `IntegrationStatusKind` only has connected/not_connected/coming_soon and no card exposes `active_provider` at all - this is a real gap the current cards already have, now surfaced because Activate/Disconnect need to distinguish these states to decide which buttons to show. | n - reasonable default, flag if a different visual treatment is wanted |
| Card footer with 2 actions | Render a `flex gap-2` row: "Ativar" (secondary, only when connected && not active) and "Desconectar" (destructive/tertiary, whenever connected) side by side; "Editar conexão" (the existing reconnect action) stays available too, connected or not | Mirrors the confirmed `provider-disconnect` decision (disconnecting the active provider is allowed) plus keeps the existing reconnect path working - nothing from the current Connect flow is removed. | y (Ativar/Desconectar coexistence follows directly from provider-disconnect's confirmed decisions) |
| Disconnect confirmation dialog | Reuse the `Dialog` component the same way `EmailProvidersPage.tsx`/`AISettings.tsx` already do - move that logic into `IntegrationsPage.tsx`, one dialog instance per card (or one shared dialog keyed by which provider is pending disconnect) | Direct migration of already-built, already-tested logic - no new UX decision needed, `provider-disconnect`'s confirmation requirement (spec Assumptions) still applies verbatim. | y |
| Deleting the orphaned pages | Delete `EmailProvidersPage.tsx`, `EmailProvidersPage.test.tsx`, `AISettings.tsx`, `AISettings.test.tsx` once `IntegrationsPage.tsx`/`IntegrationsPage.test.tsx` cover the same behavior those tests covered | They have zero other importers (confirmed by grep across `web/src`) and would otherwise be permanently-dead, permanently-unreachable code - a maintenance trap that looks like real UI to a future reader of the file tree. | y |
| i18n keys | Reuse the same `emailProviders`/LLM-settings i18n keys `provider-disconnect` already added (dialog title/body/confirm/cancel) rather than creating new ones, since the copy itself doesn't change - only which component renders it | Avoids duplicate/drifting translation strings for the same dialog copy. | y (inferred - no new copy is introduced) |

**Open questions:** none - all resolved or logged above.

---

## User Stories

### P1: Activate a connected provider from the Integrations screen ⭐ MVP

**User Story**: As an owner/operator, I want to activate a connected (but not yet active) email or LLM provider directly from `/integrations` so that I don't need an unreachable page to do it.

**Why P1**: This closes the reachability gap for the Activate action that `provider-disconnect` incorrectly assumed was already live.

**Acceptance Criteria**:

1. WHEN a provider's status is connected and it is NOT the account's `active_provider` THEN the card SHALL show an "Ativar" button.
2. WHEN the admin clicks "Ativar" THEN the system SHALL call the existing activate mutation (`useActivateEmailProvider` / `useActivateLLMProvider`) for that provider and, on success, refresh the card's status.
3. WHEN a provider IS the active provider THEN the card SHALL NOT show an "Ativar" button (nothing to activate) and SHALL show a distinct "Ativo" indicator instead of the plain "Conectado" pill.
4. IF the activate call fails THEN the card SHALL show an error state and the provider list SHALL remain unchanged (same failure posture as the existing Connect flow).

**Independent Test**: Connect SendGrid (not yet active), see "Ativar" on its card; click it; card now shows "Ativo" and Resend/others show their prior state unaffected.

---

### P1: Disconnect a connected provider from the Integrations screen ⭐ MVP

**User Story**: As an owner/operator, I want to disconnect a connected email or LLM provider directly from `/integrations`, with a confirmation step, so that the Disconnect capability `provider-disconnect` built is actually usable.

**Why P1**: Same reachability gap as Activate, for Disconnect - this is the actual deliverable the user asked for in `provider-disconnect` and never got a working UI for.

**Acceptance Criteria**:

1. WHEN a provider's status is connected (active or not) THEN the card SHALL show a "Desconectar" button.
2. WHEN the admin clicks "Desconectar" THEN the system SHALL show a confirmation dialog naming the provider before calling the disconnect mutation.
3. WHEN the admin confirms THEN the system SHALL call `useDisconnectEmailProvider` / `useDisconnectLLMProvider` and, on success, the card SHALL revert to its not-connected state (icon/description unchanged, status pill and meta reset, no Ativar/Desconectar buttons shown).
4. WHEN the admin cancels the dialog THEN the system SHALL make no API call and leave the card unchanged.
5. IF the disconnect call fails THEN the system SHALL show an error message and leave the card's connected state unchanged.

**Independent Test**: Click "Desconectar" on a connected OpenAI card, cancel - card still shows connected; click again, confirm - card reverts to "Não configurado" / not_connected.

---

### P2: Remove the orphaned pages

**User Story**: As a maintainer, I want the dead `EmailProvidersPage.tsx`/`AISettings.tsx` components removed once their behavior lives in `IntegrationsPage.tsx`, so a future reader doesn't mistake unreachable code for the real UI.

**Why P2**: Cleanup, not user-facing - sequenced after the P1 migrations so nothing is deleted before its behavior is proven to exist elsewhere.

**Acceptance Criteria**:

1. WHEN the P1 stories' behavior is implemented and tested in `IntegrationsPage.tsx` THEN `EmailProvidersPage.tsx`, `EmailProvidersPage.test.tsx`, `AISettings.tsx`, and `AISettings.test.tsx` SHALL be deleted.
2. The system SHALL have zero remaining references to these four files (verified by grep) before they are deleted.

**Independent Test**: `grep -rn "EmailProvidersPage\|AISettings" web/src` returns nothing after this story is done.

---

## Edge Cases

- IF a provider has no row at all (never connected) THEN the card SHALL behave exactly as it does today (only "Conectar", no Ativar/Desconectar) - this feature only adds actions to the connected states.
- IF disconnecting the currently active provider THEN the card SHALL still allow it (per `provider-disconnect`'s confirmed decision) and, after success, show not-connected with no active-provider indicator anywhere in that category.
- WHEN both email providers are connected but only one is active THEN the inactive-but-connected one SHALL show "Ativar" + "Desconectar" + "Editar conexão"; the active one SHALL show "Ativo" + "Desconectar" + "Editar conexão".

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| INTGCARD-01 | P1: Activate from Integrations screen (AC1-2) | Design | Pending |
| INTGCARD-02 | P1: Activate from Integrations screen (AC3-4) | Design | Pending |
| INTGCARD-03 | P1: Disconnect from Integrations screen (AC1-2) | Design | Pending |
| INTGCARD-04 | P1: Disconnect from Integrations screen (AC3-5) | Design | Pending |
| INTGCARD-05 | P2: Remove the orphaned pages | Design | Pending |

**ID format:** `INTGCARD-[NUMBER]`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 5 total, 0 mapped to tasks, 5 unmapped ⚠️ (tasks not yet created)

---

## Success Criteria

- [ ] `/integrations` lets an owner/operator activate and disconnect any connected email or LLM provider, with a confirmation dialog on disconnect.
- [ ] `grep -rn "EmailProvidersPage\|AISettings" web/src` returns nothing (files deleted).
- [ ] `npx tsc -b --noEmit` and `npm run test` stay green with tests covering: Ativar shown only when connected-and-inactive, Ativo shown when active, Desconectar always shown when connected, cancel vs confirm on the dialog, success reverts the card, failure leaves it unchanged.
