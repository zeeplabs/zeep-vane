# Integrations Card Actions Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.**

**Design**: skipped - extends the existing `IntegrationCard`/`EmailProviderCard`/`LLMProviderCard` pattern already in `IntegrationsPage.tsx`, reusing hooks and dialog logic `provider-disconnect` already built and tested. No new architecture.
**Status**: Draft

---

## Test Coverage Matrix

> Sampled `web/src/features/integrations/IntegrationsPage.test.tsx`, `web/src/features/email-providers/EmailProvidersPage.test.tsx` (the logic being migrated), AGENTS.md §5.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Component (`IntegrationsPage.tsx` card actions) | unit | Ativar shown iff connected && not active; Ativo indicator iff active; Desconectar shown iff connected; dialog cancel = no call/unchanged; dialog confirm = call + card reverts; mutation failure = error shown, card unchanged - for both email providers and the LLM provider | `web/src/features/integrations/IntegrationsPage.test.tsx` | `npm run test` (from `web/`) |
| Dead-code removal | none | Verified by grep, not a test | - | build gate only |

## Gate Check Commands

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick | After a task touching only component/test code | `cd web && npx tsc -b --noEmit && npm run test` |
| Build | End of the feature | Quick, plus `grep -rn "EmailProvidersPage\|AISettings" web/src` (expect empty output) |

---

## Execution Plan

### Phase 1: Email provider card actions

T1 → T2

### Phase 2: LLM provider card actions

T3 → T4

### Phase 3: Cleanup

T5 → T6

---

## Task Breakdown

### T1: Add active-provider awareness to `EmailProviderCard`

**What**: Pass `activeProvider` (from `emailData.active_provider`) into `EmailProviderCard`; render an "Ativo" pill/meta instead of plain "Conectado" when `id === activeProvider`; keep everything else (icon, banner, description) unchanged.
**Where**: `web/src/features/integrations/IntegrationsPage.tsx`
**Depends on**: None
**Reuses**: `IntegrationStatusKind`/`Tag` pattern already in `IntegrationCard.tsx` (no changes needed there - card already renders whatever `status`/`meta` it's given).
**Requirement**: INTGCARD-02

**Tools**: MCP: NONE / Skill: NONE

**Done when**:
- [x] `EmailProviderCard` receives and uses `isActive` (derived from `emailData.active_provider` at the page level).
- [x] Active provider's card shows a distinct indicator ("Ativo" meta) instead of the current always-"Conectado"/"Verificado" meta.
- [x] Test added in `IntegrationsPage.test.tsx`: connected+active provider shows "Ativo"; connected+inactive shows "Verificado".
- [x] Gate check passes: quick

**Tests**: unit
**Gate**: quick

---

### T2: Add Ativar + Desconectar actions to `EmailProviderCard`

**What**: Render "Ativar" (only when connected && not active, calling `useActivateEmailProvider`) and "Desconectar" (whenever connected, opening a confirmation `Dialog` before calling `useDisconnectEmailProvider`) alongside the existing "Conectar"/"Editar conexão" button, in the card's `action` slot (a `flex gap-2` row of buttons).
**Where**: `web/src/features/integrations/IntegrationsPage.tsx`
**Depends on**: T1
**Reuses**: `useActivateEmailProvider`/`useDisconnectEmailProvider` (`web/src/features/email-providers/hooks.ts`, unchanged); the `Dialog`-based confirmation flow already built in `EmailProvidersPage.tsx` (migrate its JSX/state pattern, not its test file); the existing `emailProviders` i18n keys `provider-disconnect` added for the dialog copy.
**Requirement**: INTGCARD-01, INTGCARD-03, INTGCARD-04

**Tools**: MCP: NONE / Skill: NONE

**Done when**:
- [ ] "Ativar" button shown only when connected && not active; calls the activate mutation; card updates to the active indicator on success.
- [ ] "Desconectar" button shown whenever connected; opens confirmation dialog naming the provider; cancel makes no call; confirm calls the disconnect mutation and the card reverts to not-connected on success.
- [ ] Activate/disconnect failure shows an error state, card unchanged.
- [ ] Test added/extended in `IntegrationsPage.test.tsx` covering all of the above for both `resend` and `sendgrid`.
- [ ] Gate check passes: quick

**Tests**: unit
**Gate**: quick

---

### T3: Add active-provider awareness to `LLMProviderCard`

**What**: Mirror T1 for `LLMProviderCard` using `llmData.active_provider` (single provider, `openai`, but the active/connected distinction still applies the same way).
**Where**: `web/src/features/integrations/IntegrationsPage.tsx`
**Depends on**: T2
**Reuses**: T1's pattern.
**Requirement**: INTGCARD-02

**Tools**: MCP: NONE / Skill: NONE

**Done when**:
- [ ] `LLMProviderCard` shows the active indicator when `openai` is `active_provider`, plain connected meta otherwise.
- [ ] Test added in `IntegrationsPage.test.tsx` mirroring T1's cases for the LLM card.
- [ ] Gate check passes: quick

**Tests**: unit
**Gate**: quick

---

### T4: Add Ativar + Desconectar actions to `LLMProviderCard`

**What**: Mirror T2 for `LLMProviderCard` using `useActivateLLMProvider`/`useDisconnectLLMProvider` (`web/src/features/settings/hooks.ts`) and the existing LLM-settings i18n keys `provider-disconnect` added.
**Where**: `web/src/features/integrations/IntegrationsPage.tsx`
**Depends on**: T3
**Reuses**: T2's pattern.
**Requirement**: INTGCARD-01, INTGCARD-03, INTGCARD-04

**Tools**: MCP: NONE / Skill: NONE

**Done when**:
- [ ] Mirrors all of T2's `Done when` items for the LLM card.
- [ ] Test added/extended in `IntegrationsPage.test.tsx` mirroring T2's coverage for `openai`.
- [ ] Gate check passes: quick

**Tests**: unit
**Gate**: quick

---

### T5: Delete the orphaned pages

**What**: Confirm zero remaining references (`grep -rn "EmailProvidersPage\|AISettings" web/src`), then delete `web/src/features/email-providers/EmailProvidersPage.tsx`, `EmailProvidersPage.test.tsx`, `web/src/features/settings/AISettings.tsx`, `AISettings.test.tsx`.
**Where**: (deletions only, no edits) `web/src/features/email-providers/EmailProvidersPage.tsx`, `web/src/features/email-providers/EmailProvidersPage.test.tsx`, `web/src/features/settings/AISettings.tsx`, `web/src/features/settings/AISettings.test.tsx`
**Depends on**: T4
**Reuses**: N/A
**Requirement**: INTGCARD-05

**Tools**: MCP: NONE / Skill: NONE

**Done when**:
- [ ] Grep for both names across `web/src` returns nothing.
- [ ] The four files are deleted.
- [ ] Gate check passes: quick (confirms nothing else imported them)

**Tests**: none (removal verified by grep + the quick gate not failing on a missing import)
**Gate**: quick

---

### T6: Final build gate

**What**: Run the full frontend build gate to confirm the feature is complete and nothing else references the deleted files.
**Where**: N/A (verification only)
**Depends on**: T5
**Reuses**: N/A
**Requirement**: (verification of INTGCARD-01..05)

**Tools**: MCP: NONE / Skill: NONE

**Done when**:
- [ ] `npx tsc -b --noEmit` passes.
- [ ] `npm run test` passes, full suite.
- [ ] `grep -rn "EmailProvidersPage\|AISettings" web/src` returns nothing.

**Tests**: none (verification task)
**Gate**: build

---

## Phase Execution Map

```
T1 → T2
T2 → T3
T3 → T4
T4 → T5
T5 → T6
```

Grouped: Phase 1 = T1, T2. Phase 2 = T3, T4. Phase 3 = T5, T6.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1 | 1 component change, 1 file | ✅ Granular |
| T2 | 1 component change, 1 file | ✅ Granular |
| T3 | 1 component change, 1 file | ✅ Granular |
| T4 | 1 component change, 1 file | ✅ Granular |
| T5 | 4 file deletions, 0 edits | ✅ Granular |
| T6 | 0 files, verification only | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | (none) | ✅ Match |
| T2 | T1 | T1 → T2 | ✅ Match |
| T3 | T2 | T2 → T3 | ✅ Match |
| T4 | T3 | T3 → T4 | ✅ Match |
| T5 | T4 | T4 → T5 | ✅ Match |
| T6 | T5 | T5 → T6 | ✅ Match |

---

## Test Co-location Validation

| Task | Code Layer | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1 | Component | unit | unit | ✅ OK |
| T2 | Component | unit | unit | ✅ OK |
| T3 | Component | unit | unit | ✅ OK |
| T4 | Component | unit | unit | ✅ OK |
| T5 | Dead-code removal | none | none | ✅ OK |
| T6 | none (verification) | - | none | ✅ OK |

No violations.
