# Validation Report — profile-page-redesign

**Verifier**: independent (fresh read, no inherited assumptions from the author of the working-tree diff).
**Scope reviewed**: uncommitted working-tree changes on branch `develop` — `web/src/layout/AppShell.tsx` (route-title map entry), `web/src/layout/AppShell.test.tsx` (new test only), `web/src/features/profile/PersonalInfoCard.tsx`, `web/src/features/profile/SecurityCard.tsx`, `web/src/features/sessions/SessionsSection.tsx`, `web/src/features/notifications/NotificationsSection.tsx`. Compared directly against `handoff-new-layout/Meu Perfil.dc.html` and `.specs/features/profile-page-redesign/spec.md`.

**Result**: PASS

---

## Per-AC Evidence

| AC | Requirement | Evidence | Status |
| --- | --- | --- | --- |
| PROFRD-01 | Topbar shows "Meu Perfil" as the page title on `/profile` | `AppShell.tsx:23` adds `["/profile", "profile.title"]` to `routeTitleKeys`; `titleKeyFor` (`:26-29`) does a prefix-match lookup and falls back to `sidebar.brand` only if no entry matches. `profile.title` resolves to `"Meu Perfil"` in pt-BR and `"My Profile"` in en (`web/src/lib/i18n.ts:277`, `:761`). New test `AppShell.test.tsx:66-73` renders the shell at `/profile` and asserts the `<header>` shows "Meu Perfil". | Met |
| PROFRD-02 | `PersonalInfoCard`, `SecurityCard`, `SessionsSection`, `NotificationsSection` render as bordered cards with mock's 24px padding, 14px/700 title, 12.5px muted subtitle | All four use `<Card ... className="... p-[24px]">` (`PersonalInfoCard.tsx:70`, `SecurityCard.tsx:60`, `SessionsSection.tsx:77`, `NotificationsSection.tsx:57`) — matches the mock's `padding:24px` (`Meu Perfil.dc.html:192/213/252/271`). Every title uses `text-sm font-bold` = 14px/700 (mock: `font-size:14px; font-weight:700`), every subtitle uses `text-[12.5px] text-neutral-400` (mock: `font-size:12.5px`). Confirmed at `PersonalInfoCard.tsx:73-74`, `SecurityCard.tsx:62-63`, `SessionsSection.tsx:79-80`, `NotificationsSection.tsx:59-60`. | Met |
| PROFRD-03 | `SecurityCard`'s password form: "Senha atual" full-width row above a 2-column grid of "Nova senha"/"Confirmar nova senha" | `SecurityCard.tsx:67-73` renders the current-password `Field` outside any grid wrapper (full-width row); `:74-89` wraps new/confirm passwords in `<div className="grid grid-cols-2 gap-4">`. Matches the mock's layout exactly — current password is `grid-template-columns:1fr 1fr` col 1 with an empty col 2 (i.e. visually full-row), new/confirm sit in the row below (`Meu Perfil.dc.html:217-231`). | Met |
| PROFRD-04 | No prop/hook/mutation-key/`data-testid`/test-observable behavior change in any of the 4 cards | Diffed each of the 4 changed components' own test file against `develop`: `PersonalInfoCard.test.tsx`, `SecurityCard.test.tsx`, `SessionsSection.test.tsx`, `NotificationsSection.test.tsx` are all byte-identical (`git diff develop -- <file>` empty for all four). Only `AppShell.test.tsx` gained a new test (additive only, verified via diff — see Gate Results). All `data-testid`s (`profile-avatar`, `revoke-button`, `session-row`, etc.) and hook calls (`useUpdateProfileName`, `useChangePassword`, `useSessions`, `useRevokeSession`, `useNotificationPreferences`, `useUpdateNotificationPreference`) are unchanged in the diff — only Tailwind className strings and JSX structural wrappers (grid/fragment) changed. | Met |
| PROFRD-05 | `PersonalInfoCard`'s avatar remains the existing 48px initials-only circle, no upload control | `PersonalInfoCard.tsx:77-84` — avatar `div` unchanged from `develop` other than being moved below the new title/subtitle block; still `h-12 w-12` (48px) rendering `initialsOf(...)`, `data-testid="profile-avatar"`, no file input / upload affordance added anywhere in the diff. | Met |

---

## Gate Results

| Gate | Result |
| --- | --- |
| `cd web && npx tsc -b --noEmit` | Clean, no output |
| `cd web && npm run test -- --run` | 91 files / 563 tests passed |
| Pre-existing `PersonalInfoCard.test.tsx` / `SecurityCard.test.tsx` / `SessionsSection.test.tsx` / `NotificationsSection.test.tsx` | Unmodified — `git diff develop -- <each file>` returns empty for all four |
| `AppShell.test.tsx` | Additive only — one new `it(...)` block for the `/profile` title assertion, no existing assertion touched |

(No backend files touched by this diff — the Go build/test/vet gate was not re-run since nothing in `internal/*` or `cmd/*` changed.)

---

## Discrimination Sensor (isolated detached git worktree at `<scratchpad>/vane-verify-worktree`, `HEAD` = `be871b8`, uncommitted feature files copied in manually — never `git stash`, real working tree never touched, worktree removed after use)

| # | Mutation | Result | Evidence |
| --- | --- | --- | --- |
| 1 | Remove `["/profile", "profile.title"]` from `AppShell.tsx`'s `routeTitleKeys` | **Killed** | `AppShell.test.tsx`'s new `/profile` test fails: `findByText("Meu Perfil")` inside `<header>` times out (falls back to "Vane"/brand title). |
| 2 | Revert `PersonalInfoCard`'s Nome/Email row from `grid grid-cols-2 gap-4` to `flex flex-col gap-4` (single column) | **Survived** | Full `PersonalInfoCard.test.tsx` + `ProfilePage.test.tsx` stay green — no test asserts the wrapper's layout className, only field values/labels/callbacks. |
| 3 | Revert `SecurityCard`'s Nova/Confirmar senha row from `grid grid-cols-2 gap-4` to `flex flex-col gap-4` (single column) | **Survived** | Full `SecurityCard.test.tsx` + `ProfilePage.test.tsx` stay green — same class of gap as #2; PROFRD-03's grid layout has no regression test, only field presence/values are asserted. |
| 4 | Move `SessionsSection`'s `<Dialog>` back inside the `<Card>` and drop the outer `<>...</>` fragment | **Survived** | Full `SessionsSection.test.tsx` + `ProfilePage.test.tsx` stay green — `Dialog` renders through a portal, so no test observes its DOM position relative to `Card`; nothing asserts the fragment/sibling structure the reskin introduced. |
| 5 | Revert `PersonalInfoCard`'s title/subtitle classes from `text-sm font-bold` / `text-[12.5px]` back to plain `text-text` / `text-[13.5px]` | **Survived** | Full `PersonalInfoCard.test.tsx` + `ProfilePage.test.tsx` stay green — no test asserts title/subtitle typography classes; this is consistent with the spec's own framing of the work as "pure visual reskin" with behavior-only test coverage. |

**1/5 mutations killed, 4/5 survived.**

---

## Ranked Gap List

1. **(Low severity, test-coverage gap, PROFRD-03)** No test asserts the 2-column grid layout of "Nova senha"/"Confirmar nova senha" in `SecurityCard`, nor the analogous Nome/Email grid in `PersonalInfoCard`. Mutations #2 and #3 show either could silently regress to a single column and the full suite would stay green. This is the AC with the most concrete, mock-derived layout claim (PROFRD-03 explicitly calls out the grid), so it is the most notable of the four survivors — a `data-testid` on the grid wrapper or a class assertion would close it cheaply.
2. **(Low severity, test-coverage gap, structural)** `SessionsSection`'s `Dialog`-inside-vs-outside-`Card` restructuring (the Fragment split) has no regression test — mutation #4 shows moving the Dialog back inside the Card and collapsing the fragment ships undetected, because `Dialog` portals its content regardless of JSX nesting. Not a functional risk (portals make DOM position invisible to the user), but if `Card`'s own styling ever assumed no non-`Card` children, this could resurface as a real bug with zero test signal.
3. **(Cosmetic, no AC violated in a testable way)** None of the four cards' typography/padding restyle (14px/700 titles, 12.5px subtitles, 24px padding) is covered by any assertion — mutation #5 confirms a full revert of this styling on `PersonalInfoCard` passes the suite untouched. This matches the spec's explicit framing (pure visual reskin, kept deliberately outside the existing behavior-only test suites), so it is not treated as a defect, but it does mean AC-02's pixel values have zero regression protection going forward — a future refactor could silently drift from the mock.

All 5 ACs have direct, file:line evidence against both the code and the mock; both required gates (`tsc`, `npm run test`) are green; all 4 pre-existing subcomponent test files are provably byte-identical to `develop`; the only new test (`AppShell.test.tsx`'s `/profile` title assertion) is additive and correctly kills its corresponding mutation. The 4 survivors are all visual/structural-only gaps consistent with this feature's explicit scope (a pure reskin with no behavior change) — none represent a functional, security, or data-logic regression.

---

## Addendum (post-Verifier fix pass, same session)

Gap 1 (the most notable survivor, PROFRD-03's own grid layout) closed in a follow-up commit:

- New test in `PersonalInfoCard.test.tsx`: asserts the Nome field's closest `.grid` ancestor carries `grid-cols-2` and contains the Email field too — kills mutation #2 (single-column revert).
- New test in `SecurityCard.test.tsx`: asserts the Nova senha field's closest `.grid` ancestor carries `grid-cols-2`, contains Confirmar nova senha, and explicitly does NOT contain Senha atual (proving the full-width-row-above-the-grid layout, not just "a grid exists somewhere") — kills mutation #3.

Gaps 2 and 3 (Dialog/Fragment structural gap, typography/padding gap) were left as-is: both are genuinely non-functional (Dialog portals make DOM position invisible to users; typography-only classes have no behavioral contract to protect) and match the spec's own "pure visual reskin" framing — adding brittle className assertions for pixel values would cost more in future maintenance than the regression risk they'd catch.

Full gate re-run after the fix: `tsc -b --noEmit` clean; `npm run test -- --run` → 92 files / 572 tests passed (up from 91/563 — 2 new tests, plus the unrelated concurrent `billing-plans-page` work also landed in the same working tree by this point).
