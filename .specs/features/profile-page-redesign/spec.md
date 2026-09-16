# Profile Page Redesign Specification

## Problem Statement

`ProfilePage.tsx` (Meu Perfil) already has 100% real backend behind it — name update, password change, 2FA enroll/disable, active sessions, and notification preferences are all implemented and `Verified` (`profile-self-service`, `auth-2fa-totp`, `user-sessions`, `notification-preferences`). It already uses the current design-token classes (`text-text`, `border-divider`, etc.), but was never given the same pixel-parity pass against `handoff-new-layout/Meu Perfil.dc.html` that Overview/Users/Settings/Auth/Poller Status/Incidents already got this session. The gap is visual layout, not data or logic.

Two concrete, real findings from comparing the mock against the live code (not cosmetic guesses):

1. **Missing route title**: `AppShell.tsx`'s route→title map has no entry for `/profile` — visiting the page shows the Topbar's fallback title ("Vane") instead of "Meu Perfil". Same bug class already found and fixed for `/overview` earlier this session (`AD-032`'s sibling finding).
2. **Password fields layout diverges from the mock**: the mock lays out "Senha atual" (full row) then "Nova senha"/"Confirmar nova senha" side by side in a 2-column grid; `SecurityCard.tsx` currently stacks all three fields in a single column.

## Goals

- [ ] Fix `AppShell.tsx`'s route-title map: add `/profile` → "Meu Perfil" (i18n key, matching the existing map's pattern).
- [ ] `PersonalInfoCard`, `SecurityCard` (password form + embedded `TwoFactorCard`), `SessionsSection`, `NotificationsSection` restyled to match the mock's card padding (24px), header typography (14px/700 title, 12.5px muted subtitle), and field/button sizing (11px/12px padding, 9px radius) — without changing any prop, hook, mutation, or test-observable behavior of any of them.
- [ ] `SecurityCard`'s password form restyled to the mock's 2-column grid (current password full-width row, new/confirm password side by side below it).
- [ ] Avatar: keep the existing 48px initials-only circle (no upload) — mock's 64px image-upload slot is decorative demo content for a capability (`image-slot`) this app doesn't have; already an established Out of Scope call, not reopened here.

## Out of Scope

| Item | Reason |
| --- | --- |
| Avatar image upload | No backend endpoint for it; mock's `image-slot` is unimplemented UI sugar in the handoff tool itself, not a real feature request. |
| Single page-wide "Descartar / Salvar alterações" bottom bar + inline "Alterações salvas" pill | The mock treats the whole page as one form, but name-change, password-change, and notification-toggle are 3 independent backend mutations with 3 different validation/error contracts (422 empty name, 401 wrong current password, 422 password policy, per-toggle PATCH). Forcing them behind one shared submit button would misrepresent partial failure (e.g. name saves, password fails) as a single all-or-nothing action it isn't. Keep the existing per-card submit buttons + `sonner` toast feedback (already the app's established success/error pattern everywhere else - Users, Settings, Integrations all use it too). |
| Sessions row content (device+IP+last-seen vs. mock's device+location+lastActive) | `Session` has no geolocation field; `location` in the mock is fabricated demo data. Already resolved by `user-sessions`'s own spec - not reopened here. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Per-card submit vs. one page-wide form | Keep per-card submits (existing) | 3 independent mutations with different error contracts - see Out of Scope. Same reasoning already applied by `profile-self-service`'s own design. | y - codebase (3 distinct hooks: `useUpdateProfileName`, `useChangePassword`, `useUpdateNotificationPreference`) |
| Route title fix scope | Fix only `/profile`, don't audit every other route | Out of caution against scope creep - this spec's Independent Test explicitly covers `/profile`; a full route-title audit is a separate, smaller task if more gaps are suspected. | y |

**Open questions:** none — todas resolvidas acima.

---

## User Stories

### P1: Pixel-parity reskin against the handoff ⭐ MVP

**User Story**: Como usuário autenticado, quero que a tela Meu Perfil tenha o mesmo acabamento visual do handoff (espaçamento, tipografia, layout de campos), para que a experiência seja consistente com o resto do app já redesenhado.

**Acceptance Criteria**:

1. The Topbar SHALL show "Meu Perfil" as the page title when `/profile` is the active route (fixing the missing `AppShell.tsx` route-title map entry).
2. `PersonalInfoCard`, `SecurityCard`, `SessionsSection`, `NotificationsSection` SHALL each render as a bordered card with the mock's padding/radius/typography (24px padding, 14px/700 card title, 12.5px muted subtitle) - matching the visual language `border border-divider` cards already use elsewhere in the redesigned app.
3. `SecurityCard`'s password form SHALL render "Senha atual" as a full-width row above a 2-column grid containing "Nova senha" and "Confirmar nova senha", matching the mock's layout.
4. No prop, hook call, mutation key, `data-testid`, or test-observable behavior of any of the 4 cards SHALL change - this is a pure visual reskin on top of already-`Verified` backend/frontend logic.
5. `PersonalInfoCard`'s avatar SHALL remain the existing 48px initials-only circle (no upload control added).

**Independent Test**: navigate to `/profile`, confirm the Topbar shows "Meu Perfil"; confirm the password form's 3 fields render in the mock's row/grid layout; run the full existing test suite (`ProfilePage.test.tsx` and every subcomponent's own test file) and confirm 100% pass with zero assertion changes.

---

## Edge Cases

- IF a future route is added without a title-map entry THEN the Topbar SHALL still fall back to "Vane" (existing behavior, unchanged) - this spec only closes the `/profile` gap, not a systemic guard against future omissions.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| PROFRD-01 | P1: Pixel-parity reskin | - | Verified |
| PROFRD-02 | P1: Pixel-parity reskin | - | Verified |
| PROFRD-03 | P1: Pixel-parity reskin | - | Verified |
| PROFRD-04 | P1: Pixel-parity reskin | - | Verified |
| PROFRD-05 | P1: Pixel-parity reskin | - | Verified |

**ID format:** `PROFRD-[NUMBER]`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 5 total, 5 mapeados a tarefas (implícitas, escopo Medium), 0 unmapped

---

## Success Criteria

- [ ] Reskin visually matches the handoff (spacing/typography/layout), zero regression in any of the 4 subcomponents' existing test suites.
- [ ] `/profile` route shows the correct Topbar title.
- [ ] `tsc -b --noEmit` and the full frontend suite green.
