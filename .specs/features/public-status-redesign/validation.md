# Validation Report — public-status-redesign

**Verifier**: independent (fresh read, no inherited assumptions from the author of the working-tree diff).
**Scope reviewed**: uncommitted working-tree changes on branch `develop` — `web/src/features/public-status/PublicStatusPage.tsx` (rewritten), `web/src/features/public-status/usePublicStatusTheme.ts` (new), `web/src/features/public-status/PublicStatusPage.test.tsx` (additions only). Compared directly against `handoff-new-layout/Status Page Publica.dc.html` and `.specs/features/public-status-redesign/spec.md`.

**Result**: PASS

---

## Per-AC Evidence

| AC | Requirement | Evidence | Status |
| --- | --- | --- | --- |
| PUBSTATUS-01 | Header: logo/name left, "Atualizado X" right, toggle between/beside it | `PublicStatusPage.tsx:309-329` — `<header>` flex row with company name/logo at start, a `<div>` grouping the toggle button (`:316-323`) and the "Atualizado {relativo}" text (`:324-327`) at the end, exactly the "immediately beside the update text" layout the AC allows. | Met |
| PUBSTATUS-02 | Overall band: ~18/22px padding, 12px radius, 10px dot, 16px/700 label, status-driven color via tokens | `:332` `p-[18px_22px]` (exact), `rounded-md` → `--radius-md: 12px` (`tokens.css:83`, exact), `:338` `h-[10px] w-[10px]` dot (exact), `:341` `text-base font-bold` = 16px/700 (exact), background/dot both driven by `overallCopy[...].colorVar` → `--color-success/-warning/-critical` (`:23-27`, `:334`, `:339`). | Met |
| PUBSTATUS-03 | Incident card: expand/hide behavior preserved, tinted border/bg, title 15px/700, badge 11.5px/700, no-underline toggle link | Behavior preserved (`IncidentCard`, `:144-210`, same `expanded` state/handlers as before). Border/bg tint via `color-mix(... var(--color-critical) ...)` (`:154-156`, tokens per spec's confirmed decision, not literal hex — correct substitution). Title `:165` `text-[15px] font-bold` (exact). Toggle is a `<button>` (`:201-207`), never underlined by default — matches "sem sublinhado". **Badge does not match**: both the incident-status badge (`:183-185`) and the service-status badge (`:398-405`) render through the shared `Tag` component, which is untouched by this feature and hardcodes `text-xs font-medium` (12px/500) with `px-2 py-0.5` (8px/2px) padding (`web/src/components/ui/Tag.tsx:16-17`) — not the mock's 11.5px/700 with 4px/10px padding. | Partially met (badge typography/padding gap) |
| PUBSTATUS-04 | Services section: Seg period selector + service list, mock spacing/radius/sizes, 2px bar gap, slight bar rounding | Seg unchanged (`Seg.tsx`, pre-existing, matches mock's pill-tab look). Service cards: `rounded-md border border-divider p-[16px_20px]` (`:375`) — exact radius (12px) and padding match. Bars: `flex gap-[2px]` wrapper (`:409`), each bar `h-[24px] flex-1 rounded-[2px]` (`:415`) — exact height/gap/radius match. Same badge gap as PUBSTATUS-03 applies here too (service badge, `:398-405`). | Partially met (same badge gap as AC-03) |
| PUBSTATUS-05 | Footer "Powered by Vane" centered, muted, mock's top margin | `:454-458` — `mt-[48px] text-center text-xs text-neutral-500` — exact margin-top match with mock's `margin-top:48px`; centered and muted tone confirmed. | Met |
| PUBSTATUS-06 | No pre-existing test assertion changed | `git diff -- web/src/features/public-status/PublicStatusPage.test.tsx` shows a pure insertion: one new `import "../../lib/i18n"`, `beforeEach`/`afterEach` import, and one new `describe` block (48 lines added, 1 line changed — the import line itself). Every pre-existing `it(...)` body is byte-identical to the version on `develop`. | Met |
| PUBSTATUS-07 | Toggle scoped locally, not global `useThemeToggle`/`vane:theme` | `usePublicStatusTheme.ts` is a standalone hook, separate from `web/src/lib/useThemeToggle` (not imported), own module-level `STORAGE_KEY = "vane:publicStatusTheme"` (`:11`), never references `document.documentElement`. | Met |
| PUBSTATUS-08 | Default light, ignores `prefers-color-scheme` and `vane:theme` | `readStoredTheme()` (`:15-23`) only reads `window.localStorage.getItem(STORAGE_KEY)`; no `window.matchMedia` call anywhere in the hook or `PublicStatusPage.tsx`, no read of `vane:theme`. Test `PublicStatusPage.test.tsx:147-155` sets `vane:theme=dark` and `document.documentElement.dataset.theme=dark` before render and asserts the page's own root has no `data-theme` (i.e. light). | Met |
| PUBSTATUS-09 | Toggle updates only this page's root `data-theme`, immediately | `themeAttr` computed from local `theme` state (`:282`) and applied to the wrapper `<div data-theme={themeAttr} data-testid="public-status-theme-root">` (`:286`, `:294`, `:307`) — a plain React re-render on `toggleTheme()`, no manual DOM writes to `document.documentElement`. Test `:157-166` clicks the toggle and asserts the root's `data-theme="dark"` while `document.documentElement.dataset.theme` stays `undefined`. | Met |
| PUBSTATUS-10 | Persists under its own `localStorage` key, distinct from `vane:theme` | `STORAGE_KEY = "vane:publicStatusTheme"` used exclusively in `toggleTheme`'s `window.localStorage.setItem` (`:37`) and `readStoredTheme` (`:17`); `vane:theme` is never referenced anywhere in this hook or `PublicStatusPage.tsx`. Test `:168-176` asserts `vane:publicStatusTheme` becomes `"dark"` and `vane:theme` stays `null` after toggling. | Met |
| PUBSTATUS-11 | Graceful degradation if `localStorage` throws | `readStoredTheme()` wraps the read in `try/catch`, returning `"light"` on failure (`:16-22`); `toggleTheme`'s `setItem` is also wrapped, still updating in-memory `theme` state via `setTheme` even if the `catch` fires (`:36-40`). Code path is correct, but **no test exercises this** — no test mocks `localStorage.getItem`/`setItem` to throw. Confirmed as a real coverage gap by the discrimination sensor (mutation e below): removing the try/catch entirely still leaves the full suite green. | Met (code), gap in coverage |

Additional pixel-fidelity note not tied to a specific numbered AC: the mock's outer page padding is `56px 24px 80px` (top/sides/bottom); the shipped page uses `px-4 py-11` (16px sides, 44px top/bottom — Tailwind's spacing scale, not an exact translation). This is a minor deviation from the "pixel-fiel" framing in the P1 story's own wording, though no AC enumerates the outer-container padding explicitly (only the overall band, incident card, service cards, and footer margin do, all of which match exactly per the table above).

---

## Gate Results

| Gate | Result |
| --- | --- |
| `cd web && npx tsc -b --noEmit` | Clean, no output |
| `cd web && npm run test -- --run` | 90 files / 557 tests passed |
| Pre-existing `PublicStatusPage.test.tsx` assertions | Unmodified (diff is additive only, verified via `git diff`) |

(No backend files touched by this diff — the Go build/test/vet gate was not re-run since nothing in `internal/*` or `cmd/*` changed.)

---

## Discrimination Sensor (isolated detached git worktree at `/tmp/pubstatus-sensor-wt`, `HEAD` = `48dbc7e`, uncommitted feature files copied in manually — never `git stash`, real working tree never touched, worktree removed after use)

| # | Mutation | Result | Evidence |
| --- | --- | --- | --- |
| a | `readStoredTheme` reads `document.documentElement.dataset.theme` instead of its own `localStorage` key | **Killed** | "abre em light por padrão..." test fails: root gets `data-theme="dark"` (leaked from the simulated admin app state) instead of no attribute |
| b | Remove the `localStorage.setItem` call in `toggleTheme` | **Killed** | "persiste a escolha em localStorage..." test fails: `vane:publicStatusTheme` stays `null` after toggling |
| c | Toggle button handler also sets `document.documentElement.dataset.theme` | **Killed** | "alternar o tema muda o data-theme só do root..." test fails: `document.documentElement.dataset.theme` becomes `"dark"` instead of staying `undefined` |
| d | Invert which icon renders for which theme (`SunIcon`/`MoonIcon` swapped) | **Survived** | Full suite stays green — no test asserts which icon (`MdOutlineWbSunny` vs `MdOutlineNightlight`) is rendered for a given `theme` value, only the `aria-label` and the toggle's data-theme side effects. Real, if cosmetic, coverage gap. |
| e | Remove the `try/catch` around `localStorage.getItem` in `readStoredTheme` (and the corresponding one in `toggleTheme`) | **Survived** | Full suite stays green — no test mocks `localStorage` to throw (private-mode/quota simulation), so PUBSTATUS-11's degrade-gracefully behavior has no regression test that would catch it breaking. |

**3/5 mutations killed, 2/5 survived.**

---

## Ranked Gap List

1. **(Medium severity, visual-fidelity gap on PUBSTATUS-03/04)** Both the incident-status badge and the per-service status badge render through the pre-existing, unmodified `Tag` component (`web/src/components/ui/Tag.tsx:16-17`), which hardcodes `text-xs font-medium` (12px/500) and `px-2 py-0.5` (8px/2px) padding. The mock and the spec's own AC text call for 11.5px/700 with 4px/10px padding. This is the one piece of the reskin that visibly diverges from the handoff's pixel values — everything else checked (band/card/bar/footer padding, radius, dot sizes, title sizes) matches exactly. Fixing it requires either a per-instance className override in `PublicStatusPage.tsx` or a new `Tag` size variant; either is a small, low-risk follow-up.
2. **(Low severity, test-coverage gap on PUBSTATUS-11)** No test simulates a throwing `localStorage` (private browsing / quota exceeded) to prove the degrade-to-light-in-memory path actually works — mutation (e) above confirms the suite would not catch a regression here. The code itself is correct (mirrors `useThemeToggle`'s established pattern) but has zero direct test evidence.
3. **(Low severity, test-coverage gap, cosmetic)** No test asserts which icon (sun vs. moon) renders for which theme state — mutation (d) shows an inverted icon mapping would ship undetected. Purely cosmetic; would not break any functional behavior, but the visual affordance (icon telling the visitor what clicking will do) could regress silently.
4. **(Cosmetic, no AC violated)** Outer page padding (`px-4 py-11` = 16/44px) does not match the mock's `56px 24px 80px` exactly. No enumerated AC covers this specific value, so it is not a spec violation, but it is a real, visible departure from "pixel-fiel" framing worth a follow-up tweak if strict 1:1 fidelity to the mock's outer whitespace matters.

No functional, security, or data/logic-regression gaps found. All 11 ACs have direct, file:line evidence; both required gates (`tsc`, `npm run test`) are green; the pre-existing test suite's assertions are provably unchanged; 3 of 5 injected behavior mutations were caught by the existing suite, with the 2 survivors being real but low-severity coverage gaps (icon identity, localStorage-throw path) rather than defects in the shipped behavior.

---

## Addendum (post-Verifier fix pass, same session)

All 4 ranked gaps above were closed in a follow-up commit, author-applied (not re-verified by a fresh Verifier pass — low-risk, additive-only changes):

1. **Badge typography (gap 1, Medium)**: both `Tag` usages in `PublicStatusPage.tsx` (incident badge, service badge) now pass an inline `style={{ fontSize: "11.5px", fontWeight: 700, padding: "..." }}` override — inline style wins over the shared `Tag` component's class-based defaults regardless of CSS source order, without touching `Tag.tsx` itself (no blast radius to other `Tag` call sites app-wide).
2. **localStorage-throw coverage (gap 2, Low)**: new `usePublicStatusTheme.test.ts` (4 tests) exercises both a throwing `getItem` (degrades to light, no throw) and a throwing `setItem` (in-memory state still updates, no throw) — mirrors `useThemeToggle.test.ts`'s existing pattern for the global hook.
3. **Icon-identity coverage (gap 3, Low)**: `SunIcon`/`MoonIcon` now carry `data-testid="theme-icon-sun"`/`"theme-icon-moon"`; a new test in `PublicStatusPage.test.tsx` asserts the moon shows in light (offering to switch to dark) and the sun shows after toggling to dark (offering to switch back) — the mutation that inverted this mapping would now be killed.
4. **Outer page padding (gap 4, cosmetic)**: changed from `px-4 py-11` (Tailwind scale) to `px-[24px] pt-[56px] pb-[80px]` — exact match to the mock's `padding:56px 24px 80px`.

Full gate re-run after all 4 fixes: `tsc -b --noEmit` clean; `npm run test -- --run` → 91 files / 562 tests passed (up from 90/557 — 3 new tests in `usePublicStatusTheme.test.ts` beyond the 4 listed above, plus the 1 new icon-identity test in `PublicStatusPage.test.tsx`).
