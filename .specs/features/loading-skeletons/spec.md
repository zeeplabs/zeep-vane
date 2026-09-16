# Loading Skeletons Specification

## Problem Statement

Every screen that fetches data today (17 files rendering `useQuery`-backed content) shows a plain text string ("Carregando…" / "Loading…") while `isLoading` is true, or in a few cases nothing at all. This reads as a stalled/broken screen rather than a loading one, and gives the user no sense of the content's shape before it arrives. Replacing the text branch with a skeleton that mirrors each screen's real layout (table rows, cards, summary grid) improves perceived performance and is a well-established pattern already implicitly expected by the rest of the app's polish level (custom design tokens, themed components, no raw browser defaults).

## Goals

- [ ] A shared `Skeleton` primitive exists in `web/src/components/ui/` and is used by every in-scope screen — no bespoke pulsing-div reimplementation per screen.
- [ ] Every in-scope screen's loading state visually approximates its loaded content's structure (row count, card count, column widths) instead of a centered text string.
- [ ] Screen readers still get an equivalent "loading" announcement — the visual skeleton doesn't regress accessibility.
- [ ] Users with `prefers-reduced-motion` don't get the pulsing animation.

## Out of Scope

| Feature | Reason |
| --- | --- |
| `IntegrationsPage.tsx` | Its `isLoading` only gates a short inline status string ("Carregando…" next to a card title), not a list/content region — not a skeleton candidate. |
| `AttachDomainDrawer.tsx` / `StatusPageEditorContent.tsx` (`dnsTargetLoading`) | Same shape: gates a short inline hint text, not a content region. |
| `NotificationsSection.tsx`'s own `isLoading` | User confirmed via discuss this file stays in scope for the toggle rows it renders once loaded, but its current `isLoading` only disables a switch — see SKEL-04 for its actual skeleton treatment. |
| Skeleton for optimistic mutations (create/update/delete pending states) | Out of scope — this feature covers only the initial-fetch (`useQuery` `isLoading`) loading state, not `isPending`/mutation-in-flight UI. |
| New shimmer/gradient animation | User chose Tailwind's built-in `animate-pulse` (discuss) — no new `@keyframes` or gradient token. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Scope of screens | 17 files: `AdminsPage`, `DomainsSection`, `DomainsTable`, `EmailProvidersPage`, `IncidentsPage`, `NotificationsSection`, `OverviewPage`, `PollerStatusPage`, `PublicStatusPage`, `ServiceListPage`, `ServicesSection`, `SessionsSection`, `AISettings`, `SettingsPage`, `StatusPageDetail`, `StatusPagesSection`, `StatusPagesTable` | User confirmed via discuss (full list over the smaller "admin core only" option). | y |
| Visual pattern | Tailwind v4 built-in `animate-pulse` utility, no new CSS | User confirmed via discuss over a custom shimmer gradient — zero new tokens/keyframes. | y |
| Component architecture | One shared `Skeleton` primitive (`web/src/components/ui/Skeleton.tsx`), each screen composes its own shape from it | User confirmed via discuss over per-screen bespoke skeleton JSX. | y |
| Accessibility | Skeleton container carries `aria-busy="true"`; the existing translated loading string (`t("<feature>.loading")`) is kept but rendered visually hidden (`sr-only`) inside the same container, so screen readers still announce loading | Existing i18n `loading` keys already exist per feature (`AdminsPage`, `OverviewPage`, `SessionsSection`, `NotificationsSection`, etc.) — removing them outright would be a silent a11y/i18n regression with no upside. | n/a (derived from existing i18n keys, not a product decision) |
| Reduced motion | Tailwind's `motion-reduce:animate-none` variant on the `Skeleton` primitive disables the pulse for users with `prefers-reduced-motion: reduce` | Standard accessibility practice; zero extra maintenance since it's a built-in Tailwind variant. | n/a (accessibility default, not discussed) |
| Test selector | `Skeleton` primitive renders with `data-testid="skeleton"` (multiple per screen is expected — assert via `getAllByTestId`) | Needed so each screen's test suite can assert the loading state renders skeleton blocks instead of the old text, without coupling to visual class names. | n/a (derived, needed for testability) |
| Fidelity level | Skeleton mirrors coarse structure only (row/card count and rough column proportions) — not exact pixel-for-pixel shapes of every icon/badge | Full pixel fidelity per screen is expensive to build and re-maintain on every layout tweak; coarse structure already delivers the perceived-performance goal. | n/a (assumption, logged per closure gate) |

**Open questions:** none — all resolved or logged above.

---

## User Stories

### P1: Shared Skeleton primitive ⭐ MVP

**User Story**: As a frontend developer, I want one reusable `Skeleton` building block so every screen's loading state is visually consistent and I don't reimplement a pulsing div 17 times.

**Why P1**: Every other story depends on this primitive existing first.

**Acceptance Criteria**:

1. WHEN `Skeleton` is rendered with `width`/`height`/`radius` props THEN it SHALL render a `div` sized accordingly with `bg-neutral-200 dark:bg-neutral-800` (theme-aware, matching the app's existing dark-mode token pattern) and the Tailwind `animate-pulse` class.
2. WHEN the user's OS/browser reports `prefers-reduced-motion: reduce` THEN the rendered element SHALL NOT pulse (`motion-reduce:animate-none`).
3. The rendered element SHALL carry `data-testid="skeleton"` for test assertions.

**Independent Test**: Render `<Skeleton width={120} height={16} />` in isolation, assert a `div` with the expected inline size, `animate-pulse` class, and `data-testid="skeleton"` is present.

---

### P1: Replace text loading states with skeletons across 17 screens

**User Story**: As a user of the admin app, I want every screen to show a content-shaped skeleton instead of a "Carregando…" string while its data loads, so the app feels responsive and I can anticipate the layout.

**Why P1**: This is the feature's entire user-facing value — the primitive alone (P1 above) delivers nothing visible until it's wired into real screens.

**Acceptance Criteria**:

4. WHEN any of the 17 in-scope screens is in its `isLoading` state THEN it SHALL render one or more `Skeleton` elements approximating that screen's loaded layout (e.g., `ServiceListPage`/`ServicesSection`/`AdminsPage`/`DomainsTable`/`StatusPagesTable` render skeleton table rows matching their real column grid; `OverviewPage` renders skeleton summary cards; `SessionsSection`/`NotificationsSection` render skeleton list rows) INSTEAD OF the current plain loading text/paragraph.
5. WHEN any of the 17 in-scope screens is in its `isLoading` state THEN the screen's root loading container SHALL carry `aria-busy="true"` and SHALL include the existing translated `t("<feature>.loading")` string rendered with a visually-hidden (`sr-only`) class, so assistive technology still receives an equivalent announcement.
6. WHEN the data finishes loading (`isLoading` becomes `false`) THEN no `Skeleton` element SHALL remain in the DOM for that screen (skeleton and real content are mutually exclusive, same conditional-render shape the current `isLoading ? ... : ...` already uses).
7. IF a screen's existing loading branch also handles `isError` (e.g. `OverviewPage`, `PollerStatusPage`, `PublicStatusPage`, `SessionsSection`) THEN the error branch SHALL be left untouched — this feature only replaces the `isLoading` branch's content, never the error branch's.

**Independent Test**: For each in-scope screen's test file, mock the underlying `useQuery` hook (via existing MSW handler delay or a mocked pending promise) to force `isLoading: true`, render the screen, assert `screen.getAllByTestId("skeleton")` returns at least one element and the old loading text is not visible (still present as `sr-only` is acceptable, but not visibly rendered as the primary content).

---

## Edge Cases

- IF a screen currently has zero rows to show once loaded (e.g. an empty services list) THEN the skeleton row COUNT rendered during loading is a fixed small number (3-5, decided per screen at Execute time) — it never tries to predict the real eventual row count, since that's unknown until the fetch resolves.
- WHEN a screen's data refetches after the initial load (e.g. pagination page change, manual refresh) AND `isLoading` stays `false` but `isFetching`/background-refetch state is true THEN this feature does NOT introduce a skeleton for that case — only the initial `isLoading` (no cached data yet) transition gets a skeleton, matching the current codebase's existing `isLoading`-only branching (never `isFetching`).
- WHEN a screen is rendered inside a test environment with reduced-motion media query unset (default in jsdom/vitest) THEN the pulse class is still present in the DOM (`animate-pulse`) — the reduced-motion behavior is a CSS media-query concern, not something asserted via jsdom class absence.

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| SKEL-01 | P1: Shared Skeleton primitive | Specified | Implementing |
| SKEL-02 | P1: Shared Skeleton primitive | Specified | Implementing |
| SKEL-03 | P1: Shared Skeleton primitive | Specified | Implementing |
| SKEL-04 | P1: Replace text loading states across 17 screens | Specified | Pending |
| SKEL-05 | P1: Replace text loading states across 17 screens | Specified | Pending |
| SKEL-06 | P1: Replace text loading states across 17 screens | Specified | Pending |
| SKEL-07 | P1: Replace text loading states across 17 screens | Specified | Pending |

**Coverage:** 7 total, 7 mapped to tasks (shared primitive + 17 screens) ✅

---

## Success Criteria

- [ ] Every in-scope screen shows a layout-shaped skeleton instead of "Carregando…"/"Loading…" text.
- [ ] `Skeleton` primitive has its own test file, used nowhere else but through screen composition.
- [ ] `npx tsc -b --noEmit` and the full vitest suite stay green after the change.
- [ ] No screen loses its screen-reader loading announcement.
