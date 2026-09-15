# Dashboard Integrações (new-layout-migration) Validation

**Date**: 2026-09-15
**Spec**: `.specs/features/dashboard-integrations-page/spec.md`
**Diff range**: `b408168^..HEAD` (develop) — 2 commits: `feat(integrations): add IntegrationCard for category grid`, `feat(integrations): rewrite Integrações page as category grid with drawers`
**Verifier**: independent sub-agent (author ≠ verifier)

---

## Spec-Anchored Acceptance Criteria

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| INTG-01: 3 categorized sections w/ counts | "APM & Observabilidade" (2), "IA" (1), "E-mail" (2), each with "N integrações"/"N integração" | `IntegrationsPage.test.tsx:40-49` — asserts all 3 headings + `"2 integrações"` (×2) + `"1 integração"` present; counts hardcoded in `IntegrationsPage.tsx:172,177,181` (not derived from data — see Code Quality) | ✅ PASS |
| INTG-02: Datadog connected → green "Conectado" + "Sincronizado há N min" | exact | `IntegrationsPage.test.tsx:51-57` — seeded owner has Datadog connected; asserts `/^Sincronizado/` and "Conectado" within the card. Logic: `IntegrationsPage.tsx:60-61,70` (`formatSyncedAgo`) | ✅ PASS |
| INTG-03: Datadog 404/never-connected → "Não conectado" + "Não configurado", no crash | exact | Not independently tested with a fresh MSW 404 fixture in these two test files — the seeded fixture is always "connected". `hooks.ts:24-38` (`useIntegrationStatus`) does catch a 404 and returns `{connected:false}`, and `formatSyncedAgo(null)` returns "Não configurado" (`IntegrationsPage.tsx:23`), but no assertion targets this exact path in the new/changed test files. | ⚠️ NOT independently covered (evidence-or-zero: no citation in `IntegrationCard.test.tsx`/`IntegrationsPage.test.tsx` exercises the 404 branch) |
| INTG-04: LLM `active_provider` non-null → "Conectado" + "{Provider} · {Model}" | exact | Not exercised in `IntegrationsPage.test.tsx` (only the not-connected default fixture is used); code path exists at `IntegrationsPage.tsx:101` (`connected ? "OpenAI · ${status?.model}" : ...`) but no test asserts it renders when `status="connected"`. | ⚠️ NOT independently covered |
| INTG-05: LLM `active_provider` null → "Não conectado" + "Conectar", **no Free/Pro badge or upgrade CTA regardless of `plan_tier`** | exact | `IntegrationsPage.test.tsx:64-69` covers the not-connected/"Conectar" half. Absence of Free/Pro badge confirmed by code inspection: `grep -rniE "free\|upgrade\|pro plan\|plan_tier\|paywall" web/src/features/integrations/` returns zero matches — no plan-tier logic exists anywhere in the new files, so there is nothing to gate. No test explicitly asserts `queryByText(/free\|upgrade/i)).not.toBeInTheDocument()` for a Pro-tenant fixture, but the feature is genuinely absent from the code, not just untested. | ✅ PASS (code-verified; test coverage of the negative assertion is thin but the underlying claim — deliberate correction actually landed — holds) |
| INTG-06: Email providers list-driven status/action per card (Resend/SendGrid) | exact | `IntegrationsPage.test.tsx:64-76` (both start not-connected) + `:87-105` (Resend connects via drawer, card flips to "Conectado"). Logic: `IntegrationsPage.tsx:137-138,147-148` | ✅ PASS |
| INTG-07: New Relic **always** "Em breve", never clickable, regardless of backend | exact | `IntegrationsPage.test.tsx:59-61` — `within(newRelicCard).getByText("Em breve")` + `queryByRole("button")).not.toBeInTheDocument()`. `NewRelicCard` (`IntegrationsPage.tsx:76-86`) is hardcoded `status="coming_soon"`, no `action` prop passed at all — no backend call feeds it, so "regardless of backend response" is structurally true (no fetch exists to vary). Confirmed live via discrimination sensor (see below). | ✅ PASS |
| INTG-08: Datadog Conectar/Editar opens `Drawer` reusing `useConnectDatadog` | exact | `IntegrationsPage.test.tsx:107-116` — click "Editar conexão" opens drawer titled "Conectar Datadog". `ConnectDatadogDrawer.tsx:16` imports `useConnectDatadog` from `./hooks` | ✅ PASS |
| INTG-09: LLM Conectar/Editar opens `Drawer` reusing `useConnectLLMProvider` | partial | No end-to-end IntegrationsPage test opens the LLM drawer (only Datadog and Resend flows are exercised). `ConnectLLMProviderDrawer.tsx:19` does import and call `useConnectLLMProvider` from `settings/hooks`, so the wiring exists, but no test asserts the drawer actually opens from the LLM card's button. | ⚠️ NOT independently covered by an IntegrationsPage-level test (component exists and is wired, wiring itself untested end-to-end) |
| INTG-10: Resend/SendGrid Conectar/Editar opens `Drawer` reusing `email-providers/hooks` | exact | `IntegrationsPage.test.tsx:87-105` — full flow: click Conectar on Resend, fill form, submit, card updates. `ConnectEmailProviderDrawer.tsx:6,22` imports/uses `useConnectEmailProvider` | ✅ PASS |
| INTG-11: viewer sees zero action buttons across all 5 cards | exact | `IntegrationsPage.test.tsx:78-85` — logs in as viewer, asserts no "Editar conexão"/"Conectar" buttons anywhere on the page (not scoped per-card, but covers the full page which includes all 5 cards). `actionButton()` (`IntegrationsPage.tsx:49-56`) returns `null` when `!canManage`; `NewRelicCard` never renders an action at all. | ✅ PASS |
| INTG-12: cards use `Card elevation="none"` + `border border-divider`, no shadow | outcome (no shadow, bordered) | `IntegrationCard.tsx:42` — plain `<div className="... border border-divider bg-surface">`. **Does not literally use the `Card` component** (`components/ui/Card.tsx`) as spec text specifies; it hand-rolls an equivalent div. Net visual result (no shadow, `border-divider`) matches, but the literal component-reuse instruction was not followed. | ⚠️ Spec-precision gap — functionally equivalent, not literally `Card elevation="none"` |
| INTG-13: dot+pill badge (`Tag` + dot span) matching `DomainStatusTag` pattern | exact | `IntegrationCard.tsx:45-56` — `Tag` component + `<span className="h-1.5 w-1.5 rounded-full">` dot, same composition documented in the file's own comment (`:34-39`). `IntegrationCard.test.tsx:6-33` asserts the three status labels render. | ✅ PASS |

**Status**: 8/13 clean PASS, 1 code-verified-but-thinly-tested PASS (INTG-05), 4 gaps (INTG-03, INTG-04, INTG-09 not independently test-covered; INTG-12 literal-component deviation).

### Edge cases (spec.md)

- [x] `GET /api/integrations/email` empty list → both Resend/SendGrid show "Não conectado", never 404/hidden: `IntegrationsPage.test.tsx:64-76` (default fixture has no email providers connected, both cards render "Não conectado", section stays visible) — covered by construction, no dedicated 404 test but the code path (`byProvider.get(id)` returns `undefined` → falls to not-connected branch) is straightforward and low-risk.
- [x] `active_provider: null` even on Pro plan → treated as "Não conectado", not error: code-verified — no `plan_tier` read anywhere in `LLMProviderCard` (`IntegrationsPage.tsx:88-105`), so plan tier cannot affect this branch either way; not independently tested with an explicit Pro-tenant fixture.
- [ ] One of the 3 API calls fails (network/5xx) → the corresponding section shows an **isolated error state**, not crash other sections: **NOT implemented as specified**. `useIntegrationStatus` (`hooks.ts:24-38`) only swallows 404s into `{connected:false}`; any other error (5xx, network) is rethrown and left as `isError` on the react-query result, but `DatadogCard`/`LLMProviderCard`/`EmailProviderCard` (`IntegrationsPage.tsx:58-151`) only destructure `data`/`isLoading` — never `isError` — so a genuine failure silently renders as "Não conectado" with no error indication, rather than the "estado de erro isolado" the spec calls for. Isolation between sections is achieved as a side effect of each hook being its own independent react-query call (one query erroring doesn't throw in another's render tree), so the "doesn't crash the other sections" half is true; the "shows an error state" half is not built.

---

## Discrimination Sensor

Isolated in a temporary `git worktree` at `/tmp/vane-verify-scratch` (never the real tree, `web/node_modules` symlinked in for `vitest`, symlink removed before `git worktree remove --force`). `git status --porcelain` on the real tree was empty before and confirmed empty after.

| Mutation | File:line | Description | Killed? |
| --- | --- | --- | --- |
| 1 | `web/src/features/integrations/IntegrationsPage.tsx:60` | `const connected = data?.connected ?? false;` → `const connected = !(data?.connected ?? false);` (flip Datadog connected derivation) | ✅ Killed — 2/6 tests in `IntegrationsPage.test.tsx` failed (Datadog "Sincronizado"/"Conectado" assertions) |
| 2 | `web/src/features/integrations/IntegrationsPage.tsx:81` | `status="coming_soon"` → `status="not_connected"` on `NewRelicCard` (INTG-07 regression check) | ✅ Killed — 1/6 tests failed ("Em breve" assertion on New Relic card) |
| 3 | `web/src/features/integrations/IntegrationsPage.tsx:91` | `const connected = status?.status === "connected";` → `!==` (flip LLM connected derivation) | ✅ Killed — 1/6 tests failed ("Não conectado" assertion on LLM card) |

**Sensor depth**: lightweight (default tier)
**Result**: 3/3 killed — ✅ PASS

---

## Code Quality

| Principle | Status |
| --- | --- |
| Minimum code / no scope creep | ✅ — 4 new files (`IntegrationCard`, 3 drawers) plus a rewrite of `IntegrationsPage.tsx`; no backend touched, matches stated diff surface exactly |
| Surgical, matches existing patterns | ✅ — drawers follow the same chrome (`Drawer`, `drawerFooterPrimaryStyle`/`SecondaryStyle`, `Field`, `ApiError` handling) as pre-existing `EditStatusPageDrawer`/`AddServiceDrawer`; reuses `useConnectDatadog`, `useConnectLLMProvider`, `useConnectEmailProvider`, `useIntegrationStatus`, `useLLMProviders`, `useEmailProviders` without modification |
| `Card` component reuse | ⚠️ `IntegrationCard.tsx` hand-rolls a bordered div instead of importing `components/ui/Card` with `elevation="none"` — visually equivalent but a literal deviation from spec wording (INTG-12) |
| Hardcoded category counts | ⚠️ `IntegrationsPage.tsx:172,177,181` pass `count={2}`/`count={1}`/`count={2}` as literals rather than deriving from rendered children — correct today (fixed set of 5 real integrations) but silently drifts if a 6th integration is ever added to a section without updating the literal |
| Error-state handling for failed queries | ⚠️ (see Edge Cases above) — no `isError` consumption anywhere in the 3 card components |
| i18n | Uses hardcoded Portuguese strings directly in JSX rather than `react-i18next` (`t(...)`) — AGENTS.md §5 states "User-facing strings go through react-i18next... No hardcoded strings in components." None of the new strings ("Conectado", "Não conectado", "Em breve", card titles/descriptions, drawer labels) go through `t()`. This appears to be a pre-existing pattern in `IntegrationsPage.tsx` before this change (not introduced fresh by this diff, based on the rewrite nature), but the new drawer files also don't use i18n, extending the same gap into new code. |
| Unused imports / dead code | ✅ — none found on read-through of all 5 changed/new files |
| No `elevation="elev-sm"` anywhere | ✅ — confirmed, no `elev-sm` in `IntegrationCard.tsx` or `IntegrationsPage.tsx` |

---

## Gate Check

- **Gate command**: `cd web && npx tsc -b --noEmit && npx vitest run`
- **Result**: both clean
  - `npx tsc -b --noEmit` — clean, no errors
  - `npx vitest run` — **90 test files passed, 515 tests passed**, 0 failed
- **Skipped tests**: none
- **Failures**: none
- Backend gate not run — no backend files changed in this diff (confirmed via `git diff --stat`, all 8 changed files are under `web/src/features/integrations/` plus the new `spec.md`).

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| --- | --- | --- |
| INTG-01, INTG-02, INTG-05, INTG-06, INTG-07, INTG-08, INTG-10, INTG-11, INTG-13 | Verified (implementer-reported) | ✅ Verified |
| INTG-03, INTG-04, INTG-09 | Verified (implementer-reported) | ⚠️ Downgraded — code path exists and is plausible but not independently exercised by a test in the diff's own test files |
| INTG-12 | Verified (implementer-reported) | ⚠️ Downgraded — functional outcome matches (no shadow, `border-divider`) but literal `Card elevation="none"` component reuse specified in spec.md was not used |

---

## Summary

**Overall**: ⚠️ PASS WITH GAPS (not a clean PASS, not a FAIL — no broken behavior found, but spec-anchored coverage has real holes)

**Spec-anchored check**: 8/13 ACs cleanly matched with direct test evidence; 1 (INTG-05) code-verified but thinly tested; 4 gaps (INTG-03 Datadog-404 path, INTG-04 LLM-connected-render path, INTG-09 LLM-drawer-opens-from-card path — all plausible-but-untested; INTG-12 literal `Card` component not reused).

**Gate**: `tsc -b --noEmit` clean; `vitest run` 515/515 passed across 90 files.

**Sensor**: 3/3 targeted mutations killed (Datadog connected-flip, New Relic coming_soon-flip, LLM connected-flip) — the tests that do exist are behaviorally real, not just assertion-shaped.

**Issues found, ranked**:
1. **Edge case not implemented**: a failed API call (5xx/network, not 404) on any of the 3 endpoints renders silently as "Não conectado" instead of the spec's required isolated error state (`hooks.ts` only special-cases 404; no `isError` is read by any card component). Isolation between sections holds, error surfacing does not.
2. **Untested paths**: INTG-03 (Datadog never-connected/404), INTG-04 (LLM connected render), INTG-09 (LLM drawer opens from card) all have code that looks correct on inspection but no assertion in `IntegrationCard.test.tsx`/`IntegrationsPage.test.tsx` exercises them — evidence-or-zero means these are currently uncovered, not proven wrong.
3. **Spec-precision gap**: INTG-12 specifies literal `Card` component reuse with `elevation="none"`; the implementation hand-rolls an equivalent div instead. No functional impact, but a divergence from the written requirement.
4. **Minor code-quality notes**: hardcoded category counts (drift risk if the fixed integration set ever grows); new drawer strings don't go through `react-i18next` (extends a pre-existing pattern rather than introducing it fresh, but AGENTS.md §5 is explicit about this).

**Next steps**: Add MSW-driven tests for the Datadog-404/never-connected path, the LLM-connected-render path, and the LLM-drawer-open-from-card flow; either wire a minimal error state into the three card components for non-404 failures or explicitly re-scope that edge case out of spec.md with a documented reason; decide whether to swap `IntegrationCard`'s div for the real `Card` component or amend spec.md's INTG-12 wording to describe the outcome rather than the specific component.
