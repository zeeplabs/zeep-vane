# Validation Report — billing-plans-page

**Verifier**: independent (fresh read, no inherited assumptions from the author of the working-tree diff).
**Scope reviewed**: uncommitted working-tree changes on branch `develop` — `web/src/features/billing/BillingPage.tsx` (new), `web/src/features/billing/BillingPage.test.tsx` (new), `web/src/App.tsx` (route), `web/src/layout/AppShell.tsx` (route-title map entry), `web/src/layout/Sidebar.tsx` (nav item + moved group label), `web/src/layout/Sidebar.test.tsx` (replaced omission test with presence tests), `web/src/lib/i18n.ts` (`sidebar.billing` + `billing.*` keys). Compared directly against `.specs/features/billing-plans-page/spec.md` (6 ACs, BILLPG-01..06). `web/src/layout/TenantSwitcher.tsx`'s pre-existing `PlanBadge` was read to confirm AC5, not re-verified as new work — it predates this diff.

**Result**: PASS

---

## Per-AC Evidence

| AC | Requirement | Evidence | Status |
| --- | --- | --- | --- |
| BILLPG-01 | `/billing` route + sidebar entry "Planos & Faturamento" (Organização group, after "Usuários"), reachable by any authenticated role, no role gate | `App.tsx:205` adds `<Route path="/billing" element={<BillingPage />} />` outside any `RequireRole` wrapper. `Sidebar.tsx:166-179`: the "Organização" group label (`:166`) and the `hasRole(["owner"])`-gated "Usuários" link (`:167-172`) sit above an ungated `<NavLink to="/billing">` (`:176-179`) — no `hasRole` check wraps it. `Sidebar.test.tsx:77-88` asserts the link is present and points to `/billing` for both an owner and a non-owner (`viewer@vane.app`) login. `AppShell.tsx:24` maps `/billing` → `sidebar.billing` for the Topbar title. | Met |
| BILLPG-02 | `saas` mode renders current-plan banner + 3-tier grid (Free/Starter/Scale) + payment-method card (always empty) + invoices card (always empty) | `BillingPage.tsx:141-179` (`SaasBilling`): banner at `:147-155` reads `planNameFor(planTier)`; grid at `:157-161` maps all 3 `PLAN_DEFS` unconditionally; payment card (`:164-169`) and invoices card (`:170-175`) both render fixed "Nenhum cartão cadastrado" / "Suas faturas aparecerão aqui" copy with no conditional branch for a populated state anywhere in the file. `BillingPage.test.tsx:61-85` covers both the 3-card render and the empty-state copy. | Met |
| BILLPG-03 | `self_hosted` mode renders license-inactive banner + 2 license cards (buy/activate), matching mock's `licenseInactive` branch only | `BillingPage.tsx:181-244` (`SelfHostedBilling`): banner hardcodes "Nenhuma licença ativa" / "Recursos padrão bloqueados" (`:184-190`) with no branch for an active-license state; two `Card`s for buy (`:193-218`) and activate (`:220-240`). `BillingPage.test.tsx:100-109` asserts the banner text and both `data-testid`s. | Met |
| BILLPG-04 | Every upgrade/downgrade/buy/activate button shows a decorative toast only — no checkout UI, no card-number/CVC/expiry input anywhere, no client-side state mutation | All 5 action buttons (`PlanCard`'s button `:129-136`, license-buy `:215-217`, license-activate `:237-239`) call `onClick={() => showComingSoon(t)}` and nothing else; `showComingSoon` (`:91-93`) is exactly `toast.info(t("billing.comingSoon"))` — no `useState`/`setState` call anywhere in `BillingPage.tsx`, no `apiFetch`/`fetch`/mutation-hook import (grep of the file for `apiFetch\|fetch(\|useMutation\|axios` returns nothing). The only `<input>` in `web/src/features/billing/` is the license-key field (`:229-235`), which carries `type="text"` and `disabled` — no `card`/`cvc`/`expiry`-labeled input exists anywhere in the directory (grep for card/cvc/expiry wording only matches benign hits: `Card` component imports/JSX and CSS class names like `card-header-bg`). `BillingPage.test.tsx:87-98` clicks a plan button and asserts a toast appears with no card-style input (`input[placeholder*="1234"]`) and no "Informações de pagamento" text; `:111-121` clicks buy-license and asserts the banner still reads "Nenhuma licença ativa" afterward (no state change). | Met |
| BILLPG-05 | Sidebar's plan badge reflects real `tenant.plan`, not a hardcoded value | `TenantSwitcher.tsx:25-38` (`PlanBadge`, pre-existing, NOT part of this diff) renders `planLabel(planTier, t)` where `planLabel` (`:11-13`) is `planTier ? planTier : t("tenantSwitcher.freePlan")` — i.e. the raw membership `plan_tier` string is displayed verbatim, falling back to a localized "Free" label only when the string is empty/falsy. `TenantSwitcher.tsx:76,102,153` wire this from `admin.memberships`/`active_tenant_id`, the same real `GET /api/auth/me` data `BillingPage.tsx:249-251` also reads. No hardcoded "Free" string exists in the badge's non-empty path. | Met |
| BILLPG-06 | No new feature gating/enforcement elsewhere in the app based on the displayed plan | Grep across `web/src/` for `plan_tier`/`deploymentMode` usage outside `BillingPage.tsx`/`TenantSwitcher.tsx`/`AuthProvider.tsx` turns up nothing that conditionally hides/disables any other feature; this diff's only behavioral touch outside the billing feature itself is the Sidebar's nav-item visibility change (BILLPG-01, additive, not enforcement) and the i18n/route-title entries. | Met |

---

## Deployment-mode signal trace (AC2/AC3 branching correctness)

`BillingPage.tsx:252` reads `const { deploymentMode } = useAuth();`. Traced in `AuthProvider.tsx`: the type is declared at `:89` as `"self_hosted" | "saas"`, defaulted to `"saas"` at `:131`, and set at `:161-169` from `/api/bootstrap/status`'s `deployment_mode` field via the same boot fetch already used by the pre-existing `deployment-mode` feature (comment at `:83` explicitly documents this reuse). This is the same real signal, not a new/parallel one — confirmed by reading the provider directly rather than trusting the spec's own claim.

---

## Gate Results

| Gate | Result |
| --- | --- |
| `cd web && npx tsc -b --noEmit` | Clean, no output |
| `cd web && npm run test -- --run` | 92 files / 572 tests passed (includes `profile-page-redesign`'s already-committed additions from the same tree; nothing failing) |
| Grep for card-number/CVC/expiry-style input in `web/src/features/billing/` | Only one `<input>` exists in the directory (the disabled license-key field); no other input, and no card/cvc/expiry-labeled field, anywhere |
| Grep for `apiFetch`/`fetch`/`useMutation`/`axios` in `BillingPage.tsx` | No matches |

(No backend files touched by this diff — the Go build/test/vet gate was not re-run since nothing in `internal/*` or `cmd/*` changed.)

---

## Discrimination Sensor (isolated detached git worktree at `/tmp/vane-sensor-wt`, branched from `HEAD` = `fc81051`, uncommitted feature files copied in manually — `node_modules` symlinked, never `git stash`, real working tree never touched, worktree + branch removed after use)

| # | Mutation | Result | Evidence |
| --- | --- | --- | --- |
| 1 | Invert `deploymentMode === "saas"` to `deploymentMode !== "saas"` in `BillingPage.tsx` | **Killed** | All 6 `BillingPage.test.tsx` tests fail — `saas`-mode tests now render the license cards instead of plan cards and vice versa, so `findByText`/`findByTestId` assertions time out. |
| 2 | Remove `disabled` attribute from the license-key `<input>` | **Survived** | Full `BillingPage.test.tsx` (6/6) stays green — no test asserts the input's `disabled` state, only its presence via the surrounding card/button testids. |
| 3 | Make `showComingSoon` also call `fetch("/api/billing/upgrade", { method: "POST" })` before the toast | **Killed** | Vitest exits with code 1 — MSW's `onUnhandledRequest: "error"` strategy throws an unhandled-request error for the unmocked network call, which fails the run even though individual `expect()` assertions still pass. A CI gate would fail on this exit code. |
| 4 | Make `TenantSwitcher.tsx`'s `planLabel` always return the localized "Free" label regardless of `planTier` | **Killed** | Pre-existing `TenantSwitcher.test.tsx` ("lista todos os memberships com nome e badge de plano...") fails: expects "scale" text, receives "Free". Note: this mutation was killed by the pre-existing `TenantSwitcher` test suite, not by any test added in this diff — `BillingPage.test.tsx`'s own "badge de plano" test (`:123-133`) only checks `BillingPage`'s own current-plan banner text, not the sidebar `PlanBadge`, so it does not independently kill this mutation. |
| 5 | Remove the `["/billing", "sidebar.billing"]` entry from `AppShell.tsx`'s route-title map | **Survived** | `AppShell.test.tsx` (6/6) stays green — no existing or new test asserts the Topbar title while navigated to `/billing`. |
| 6 | Force the Free plan card's button to render as `isCurrent={true}` unconditionally (simulating a fake successful "downgrade") | **Killed** | `BillingPage.test.tsx`'s first test fails: `plan-card-free-button` is asserted `.not.toBeDisabled()` but the mutation makes it always disabled (the `isCurrent` styling/disable path). |

**4/6 mutations killed, 2/6 survived.**

---

## Ranked Gap List

1. **(Low severity, test-coverage gap, AC4)** No test asserts the license-key `<input>`'s `disabled` attribute directly (mutation #2 survives). This is the one gap closest to the spec's own non-negotiable ("never rendered, at all" for card-style inputs / never wired to a submit) — while the input staying disabled is currently true in the code, nothing in the test suite would catch a regression that silently re-enables it (e.g. during a future refactor of the license-activate flow). A one-line `expect(screen.getByLabelText("Código de licença")).toBeDisabled()` would close this cheaply.
2. **(Low severity, test-coverage gap, BILLPG-01)** No test asserts the Topbar page title while on `/billing` (mutation #5 survives) — `AppShell.test.tsx` has titled-route tests for other routes (e.g. `/profile` per the `profile-page-redesign` validation) but none for `/billing`. Cosmetic-only; the route and sidebar-link tests already cover reachability.
3. **(Informational, not a defect)** AC5's real-badge guarantee is protected by `TenantSwitcher.test.tsx`, a pre-existing suite outside this diff's own new tests — `BillingPage.test.tsx`'s "badge de plano" test name implies it verifies the sidebar badge but in fact only checks `BillingPage`'s own banner copy. Not misleading enough to fail the AC (the sidebar badge behavior is real and independently tested elsewhere), but the test's docstring/name in `BillingPage.test.tsx:123-133` slightly overstates what it covers.

None of the 3 gaps represent a functional, security, or data-fabrication regression — the two survivable mutations (missing `disabled`, missing title assertion) are coverage gaps on already-correct code, not incorrect code; the third is a naming/scope mismatch in a test's own docstring, not a missing guarantee. All 6 ACs have direct, file:line evidence against both the code and the spec; both required gates (`tsc`, `npm run test`) are green; the "no card-number input, no real mutation" non-negotiable is independently confirmed by direct grep of the shipped code, not merely by absence of a test.

---

## Addendum (post-Verifier fix pass, same session)

All 3 gaps closed in a follow-up commit:

1. New test asserts the license-key input is `disabled` via `getByLabelText("Código de licença")` — kills mutation #2.
2. New `AppShell.test.tsx` test navigates to `/billing` and asserts the Topbar shows "Planos & Faturamento" — kills mutation #5.
3. Renamed the misnamed "badge de plano no sidebar" test to "banner de plano atual (BillingPage)" and added a comment clarifying AC5's sidebar-badge guarantee is covered by the pre-existing `TenantSwitcher.test.tsx`, not by this test — no behavior change, just an honest docstring.

Full gate re-run: `tsc -b --noEmit` clean; `npm run test -- --run` → 92 files / 574 tests passed.
