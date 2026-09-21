# Incidents Page Validation

**Result**: PASS ✅ (2026-09-21 fix pass) — all 3 fix items from iteration 2 closed.

---

## Fix Pass (2026-09-21)

Julio asked to run the outstanding fix plan (3 items) from iteration 2, below.

- **Fix 1 (INCPG-01, Major)** and **Fix 3 (unknown-severity fallback, Minor)** were already closed by commit `9cb88dd` (`fix(incidents): match detail drawer layout and fix severity color regression`, 2026-09-15) — a session after this report was written but never fed back into it. `severityColor`/`severityLabel` (`incidentStatusMeta.ts`) restore the mock's color mapping (as bold colored text, matching the mock exactly — not a `Tag`/badge as this report's Fix 1 originally suggested, a deliberate visual-parity choice) with a `?? severity` / `?? "var(--color-text-muted)"` fallback for unrecognized values. `IncidentsPage.test.tsx:252` (`mostra severidade colorida por linha na tabela (INCPG-01)`) now binds label to row and asserts `color` style; a dedicated edge-case test at `IncidentsPage.test.tsx:269` covers the fallback. Verified as still passing, not re-implemented.
- **Fix 2 (INCPG-09, Minor)** — closed this pass. Discovered during the fix that the same `9cb88dd` session moved the AI-summary rendering: `IncidentDetailDrawer.tsx` now pulls any `is_ai_summary` update out of the regular timeline (`timelineUpdates = updates.filter((u) => !u.is_ai_summary)`) and renders it in its own accent-tinted box above "Linha do tempo" (`aiSummaryText`, sourced from `incident.pending_close_comment` or the resolved `is_ai_summary` entry) — the fix plan's original target, `TimelineEntry.tsx`'s `is_ai_summary` branch, is therefore dead code today (never reached, since the drawer filters those entries out before mapping to `TimelineEntry`). Added `data-testid="ai-summary-box"` to the real live container (`IncidentDetailDrawer.tsx`) and strengthened `IncidentsPage.test.tsx`'s INCPG-09..11 test to assert `backgroundColor`/`borderColor` on it, not just the label text.
- `TimelineEntry.tsx`'s unreachable `is_ai_summary` branch left untouched — out of scope for this fix plan (not a listed finding), flagged here as a minor cleanup candidate for whoever next touches that file.

**Gate**: `web/` — `npx tsc -b --noEmit` clean (0 errors); `npm run test -- --run` — **95 test files, 670 tests, all passed, 0 skipped**.

---

## Original Report (iteration 2, 2026-09-15) — superseded above

**Date**: 2026-09-15
**Spec**: `.specs/features/incidents-page/spec.md`
**Diff range**: `73d8f6f..9ad58d3` (this report supersedes iteration 1, which covered `73d8f6f` alone)
**Verifier**: independent sub-agent (author ≠ verifier), fresh dispatch — did not reuse iteration 1's worktree or findings without re-deriving them

**Context**: `9ad58d3` (`fix(incidents): match handoff mock visually`) fully rewrote the screen after iteration 1's report: table+filter-chips replace tabs+cards, a new `IncidentDetailDrawer.tsx` replaces the inline-expand timeline, `IncidentStatusTag.tsx` (dot+pill) replaces a plain status `Tag`, `TimelineEntry.tsx` was extracted, and `incidentStatusMeta.ts` centralizes status/severity label+color maps. `IncidentsPage.test.tsx` was fully rewritten for the new UI (10 → 13 `it()` blocks), including a new test explicitly targeting INCPG-06.

---

## Task Completion

No `tasks.md` exists for this Medium-scope feature (tasks implicit in Execute per spec.md's Requirement Traceability note). spec.md's traceability table still marks all 11 INCPG IDs "Verified" (author-claimed, unchanged since iteration 1) — this report re-derives that verdict independently against the current tree.

---

## Spec-Anchored Acceptance Criteria

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| INCPG-01: severity badge on active/resolved rows, label+color mapping (Menor=neutral, Moderado=warning, Crítico=critical) | A colored badge, not plain text | `IncidentsPage.tsx:180` `<div className="text-[12.5px] font-bold text-text">{incidentSeverityLabel[incident.severity]}</div>` — plain `<div>`, no `Tag`, no variant/color. `IncidentDetailDrawer.tsx:117` same pattern (`<span className="font-bold text-text">`). `incidentSeverityVariant` (`incidentStatusMeta.ts:37-41`) is exported but **never imported or used anywhere** (`grep -rn "incidentSeverityVariant" web/src/features/incidents/` returns only its own definition). Test: `IncidentsPage.test.tsx:221-228` only asserts the label text is present. | ❌ GAP (regression, not just untested) — the badge/color half of AC1 was removed during the `9ad58d3` rewrite. `73d8f6f` had a real `SeverityBadge` component wrapping a `Tag` with `variant` (`git show 73d8f6f:web/src/features/incidents/IncidentsPage.tsx:52-56`); `9ad58d3` deleted that component and inlined plain text instead, leaving the color-mapping constant as dead code. This is worse than iteration 1's "spec-precision gap" finding — there is now no code path that could pass a color/variant assertion even if one were added. |
| INCPG-02: `Incident.severity: "minor"\|"moderate"\|"critical"` | Type present | `web/src/types/api.ts` — `Incident.severity: IncidentSeverity`; `npx tsc -b --noEmit` clean (0 errors) | ✅ PASS (type-level AC) |
| INCPG-03: create drawer defaults severity to Moderado, no unselected state possible | Default = Moderado | `IncidentsPage.test.tsx:259` `expect(screen.getByRole("tab", { name: "Moderado" })).toHaveAttribute("aria-selected", "true")` | ✅ PASS |
| INCPG-04: selected severity sent in `POST /api/incidents` body | `severity: "critical"` when Crítico selected | `IncidentsPage.test.tsx:268-270` `expect(capturedBody).toEqual(expect.objectContaining({ severity: "critical", description: "Impacto total no checkout" }))` | ✅ PASS |
| INCPG-05: typed description sent in body | `description: "<typed text>"` | Same assertion, `IncidentsPage.test.tsx:268-270` | ✅ PASS |
| INCPG-06: empty description omitted/sent empty, no frontend block | Submission succeeds with description left blank; no validation blocks it | `IncidentsPage.test.tsx:281-294` (`"criar incidente sem descrição envia o form normalmente (INCPG-06)"`) — submits with description textarea untouched against the real MSW `POST /api/incidents` handler (`handlers.ts:1558-1588`, which enforces `severity` required but accepts missing/empty `description`), asserts the drawer closes and the created incident opens with `Moderado` in its detail subtitle | ✅ PASS — previously the sole real coverage gap (iteration 1), now closed. Note: the test proves submission isn't blocked and the incident renders correctly, but does not directly assert `capturedBody.description` is `undefined`/empty (no body-capturing override is used here) — accepted as sufficient since a frontend block would have left the dialog open and the incident unrendered. |
| INCPG-07: after successful create, severity resets to Moderado, description clears | Severity → Moderado, description → "" | `IncidentsPage.test.tsx:272-274` | ✅ PASS |
| INCPG-08: `IncidentUpdate.author_id: string\|null`, `is_ai_summary: boolean` | Types present | `web/src/types/api.ts`; `tsc -b --noEmit` clean | ✅ PASS (type-level AC) |
| INCPG-09: `is_ai_summary: true` renders visually distinct (accent-tinted bg/border) + label "Resumo gerado por IA" | Distinct container styling AND the label | Implementation: `TimelineEntry.tsx:14-27` — accent-tinted `<div>` branch (`color-mix(...var(--color-accent)...)` background/border) is present and correctly gated on `is_ai_summary`. Test: `IncidentsPage.test.tsx:340` `expect(await screen.findByText("Resumo gerado por IA")).toBeInTheDocument()` — label only, no assertion on the container's class/style. | ⚠️ Spec-precision gap (unchanged from iteration 1) — implementation satisfies the AC, test coverage does not confirm the "visually distinct" half. |
| INCPG-10: `is_ai_summary: false` + `author_id` set → "Equipe" | Label = "Equipe" | `IncidentsPage.test.tsx:341` `expect(screen.getAllByText("Equipe").length).toBeGreaterThan(0)` | ✅ PASS |
| INCPG-11: `is_ai_summary: false` + `author_id: null` → "Sistema" | Label = "Sistema" | `IncidentsPage.test.tsx:342` `expect(screen.getByText("Sistema")).toBeInTheDocument()` | ✅ PASS |

**Status**: ❌ 1 real regression (INCPG-01) + ⚠️ 1 spec-precision gap (INCPG-09) + 1 minor edge-case regression (below)

---

## Discrimination Sensor

Isolated worktree: `git worktree add --detach /tmp/incidents-page-sensor2 9ad58d3` (fresh worktree, not reused from iteration 1's `/tmp/incidents-page-sensor`). `node_modules` symlinked from the real `web/` to avoid reinstall. `git stash` was never used against the real tree. Test runner: `npm run test -- --run src/features/incidents/IncidentsPage.test.tsx`.

| # | File:line | Description | Killed? |
| --- | --- | --- | --- |
| M1 | `IncidentsPage.tsx:169` | Removed `onClick={() => setSelectedId(incident.id)}` from the incident row (breaks row-click-opens-drawer wiring) | ✅ Killed (6/13 tests failed) |
| M2 | `IncidentsPage.tsx:97` | Inverted the filter predicate `i.status === statusFilter` → `i.status !== statusFilter` (swaps filter-chip include/exclude logic) | ✅ Killed (1/13 failed — the status-filter test) |
| M3 | `TimelineEntry.tsx:8-10` (`authorLabel`) | Inverted `is_ai_summary`/`author_id` truthiness checks | ✅ Killed (1/13 failed — the timeline-attribution test) |
| M4 | `IncidentsPage.tsx:65-67` (`toggleService`) | Made `toggleService` a no-op (breaks the create-drawer service checklist toggle) | ✅ Killed (2/13 failed — create-flow tests, since submit with no `service_ids` gets rejected by the real 422 handler) |
| M5 | `incidentStatusMeta.ts:31-35` (`incidentSeverityLabel`) | Swapped the `moderate`/`critical` label strings | ✅ Killed overall (2/13 failed — but notably **not** by the dedicated INCPG-01 test itself (`IncidentsPage.test.tsx:221-228`), which only asserts both label strings exist somewhere on the page and does not bind them to a specific row/severity; it was caught incidentally by the drawer-subtitle assertions in two other tests). Confirms INCPG-01's own test is non-discriminating for label-to-severity mapping, on top of asserting no color at all. |

**Sensor depth**: lightweight (default tier, 5 mutations)
**Result**: 5/5 killed overall, with the caveat noted on M5 that the AC's own dedicated test did not catch it (a different test did, incidentally)

**Isolation check**: `git status --porcelain` on the real tree was empty before sensor work and empty after `git worktree remove --force /tmp/incidents-page-sensor2` — confirmed via `git status --porcelain` producing no output post-cleanup.

---

## Interactive UAT Results

Not performed — no explicit "validate"/"UAT" request beyond the standing Verifier dispatch; the automated checks (DOM-text-level RTL queries) adequately proxy the testable surface, though the INCPG-01 finding below is exactly the kind of visual gap human UAT would likely have caught immediately (a plain-text severity value where the mock shows a colored badge).

---

## Code Quality

| Principle | Status |
| --- | --- |
| Minimum code | ✅ — rewrite is scoped to the incidents feature files |
| Surgical changes | ✅ — touched files match the stated scope (`IncidentsPage.tsx`, new `IncidentDetailDrawer.tsx`/`IncidentStatusTag.tsx`/`TimelineEntry.tsx`/`incidentStatusMeta.ts`, test file) |
| No scope creep | ✅ — matches the commit's stated intent (visual parity with the handoff mock), documented deviations are explained in the commit message |
| Matches existing patterns/style | ⚠️ — `IncidentStatusTag` correctly follows the app's dot+pill `Tag` convention for status, but severity was left as unstyled plain text instead of following the same convention (or even the `73d8f6f` `SeverityBadge` precedent), making severity inconsistent with every other badge in this same screen |
| Spec-anchored outcome check (asserted values match spec) | ⚠️ — 9/11 ACs match precisely; INCPG-01 fails at the implementation level (not just the test level); INCPG-09 is implementation-correct but test-incomplete |
| Per-layer Coverage Expectation met | ⚠️ — INCPG-06 is now covered (closes iteration 1's gap); INCPG-01/INCPG-09 remain under-verified for different reasons (one is a real bug, one is a test gap) |
| Every test maps to a spec requirement | ✅ — all `it()` blocks cite INCPG IDs or existing requirement IDs (PAG-07/PAG-11, AI-12) in comments |
| Documented guidelines followed | ✅ — AGENTS.md §5 (MSW mirrors backend shape; `paginatedPage()` reused; no new hardcoded translatable strings beyond existing pt-BR convention in this file) |

---

## Edge Cases (from spec.md)

- [ ] Unknown severity value falls back to `neutral-outline` + raw string: **regressed**. `73d8f6f` had `severityMeta[severity] ?? { label: severity, variant: "neutral-outline" }` (`git show 73d8f6f:.../IncidentsPage.tsx:53`). Current code (`incidentStatusMeta.ts:31-35` + `IncidentsPage.tsx:180`) does a direct `incidentSeverityLabel[incident.severity]` lookup with no fallback — an out-of-enum value would render `undefined` (blank) instead of the raw string the spec requires, though it would not crash (React silently drops `undefined` children). Low risk (backend validates severity), not exercised by any test in either iteration, but explicitly named in spec.md's Edge Cases and previously implemented correctly.
- [x] Reopening create drawer after prior submission shows Moderado: `IncidentsPage.test.tsx:272-273`.
- [x] Resolved incident with zero timeline updates renders empty-safe: `IncidentDetailDrawer.tsx:137-141` (`updates.length === 0 ? <p>Nenhuma atualização ainda.</p> : ...`), unchanged behavior, not newly at risk from this diff.

---

## Gate Check

- **Gate command**: `cd web && npx tsc -b --noEmit && npm run test -- --run`
- **Result**: tsc — 0 errors. Vitest — **90 test files passed, 535 tests passed, 0 failed, 0 skipped.**
- **Test count before this round** (`73d8f6f`, `IncidentsPage.test.tsx` only): 10 `it()` blocks / 532 total suite tests
- **Test count after this round** (`9ad58d3`, `IncidentsPage.test.tsx` only): 13 `it()` blocks / 535 total suite tests
- **Delta**: +3 new tests in `IncidentsPage.test.tsx` (net, after the full rewrite), +3 in the suite total — consistent, no silent test deletions
- **Skipped tests**: none
- **Failures**: none

---

## Fix Plans

### Fix 1: INCPG-01 — severity is no longer rendered as a badge/color at all (regression)
- **Root cause**: The `9ad58d3` visual rewrite replaced the `73d8f6f` `SeverityBadge` (a `Tag` with `variant`) with plain, unstyled text in both `IncidentsPage.tsx:180` (table row) and `IncidentDetailDrawer.tsx:117` (drawer subtitle). `incidentSeverityVariant` (`incidentStatusMeta.ts:37-41`) was carried over into the new file but never wired up anywhere — it is dead code.
- **Fix task**: Wrap the severity label render in a `Tag variant={incidentSeverityVariant[incident.severity]}` (or a small `SeverityTag` component mirroring `IncidentStatusTag`'s dot+pill pattern) in both `IncidentsPage.tsx:180` and `IncidentDetailDrawer.tsx:117`. Then strengthen `IncidentsPage.test.tsx:221-228` to assert `data-variant` (e.g. `expect(screen.getByText("Crítico").closest('[data-variant]')).toHaveAttribute("data-variant", "critical")`) scoped to the correct row, since the current text-only assertion doesn't even bind label to row (confirmed by sensor mutation M5).
- **Priority**: Major (an explicit, numbered AC — "SHALL show a severity badge using the label/color mapping" — is not met by the shipped implementation, not just under-tested).

### Fix 2: INCPG-09 — AI-summary "visually distinct" styling is unverified
- **Root cause**: `IncidentsPage.test.tsx:340` asserts only the "Resumo gerado por IA" text; the accent-tinted container branch (`TimelineEntry.tsx:16-27`) that satisfies "visually distinct" per spec has no assertion.
- **Fix task**: Add an assertion on the AI-summary entry's container (e.g. `screen.getByText("Resumo gerado por IA").closest("div")` and check its inline `style` reflects the accent-tinted background), or add a `data-ai-summary="true"` attribute to make it directly assertable.
- **Priority**: Minor (same rationale as iteration 1 — label already covers most of the user-facing distinction risk; implementation is correct, only the test is thin).

### Fix 3: Unknown-severity fallback regressed
- **Root cause**: `73d8f6f`'s `severityMeta[severity] ?? { label: severity, variant: "neutral-outline" }` fallback was dropped in the `9ad58d3` rewrite; `incidentSeverityLabel[incident.severity]` now has no `??` fallback.
- **Fix task**: Restore a fallback in the severity lookup (or in the new `SeverityTag` component from Fix 1) so an out-of-enum value renders the raw string with `neutral-outline` styling instead of silently rendering blank.
- **Priority**: Minor (backend validates severity today; this is defensive-only per spec, but it was correct before and is now silently wrong).

---

## Requirement Traceability Update

| Requirement | Previous Status (iteration 1) | New Status (this iteration) |
| --- | --- | --- |
| INCPG-01 | ⚠️ Verified with spec-precision gap | ❌ Needs Fix (implementation regression — no badge/color at all) |
| INCPG-02 | ✅ Verified | ✅ Verified |
| INCPG-03 | ✅ Verified | ✅ Verified |
| INCPG-04 | ✅ Verified | ✅ Verified |
| INCPG-05 | ✅ Verified | ✅ Verified |
| INCPG-06 | ❌ Needs Fix (no test evidence) | ✅ Verified (test added and confirmed) |
| INCPG-07 | ✅ Verified | ✅ Verified |
| INCPG-08 | ✅ Verified | ✅ Verified |
| INCPG-09 | ⚠️ Verified with spec-precision gap | ⚠️ Verified with spec-precision gap (unchanged, implementation correct) |
| INCPG-10 | ✅ Verified | ✅ Verified |
| INCPG-11 | ✅ Verified | ✅ Verified |

---

## Summary

**Overall**: FAIL ❌ — routing 3 fix tasks back to an implementer; re-verify after fixes (iteration 2 of the standard 3 fix→re-verify budget)

**Spec-anchored check**: 9/11 ACs cleanly matched; 1/11 real regression (INCPG-01); 1/11 spec-precision gap carried over (INCPG-09)
**Sensor**: 5/5 mutations killed overall (with a caveat on M5 — the dedicated INCPG-01 test itself did not catch its own mutation; a different test did incidentally)
**Gate**: 535/535 tests passed, tsc clean, 0 skipped

**What works**: create-drawer severity+description are sent correctly and reset after success (including the newly-covered empty-description path, INCPG-06); timeline correctly distinguishes AI-summary/human/system labels; types are sound; status badges (a separate concern from severity) correctly use the dot+pill `IncidentStatusTag` pattern; gate is fully green.

**Issues found**:
1. INCPG-01 (severity badge/color) — the visual rewrite silently dropped the badge/Tag entirely, leaving unstyled plain text and dead color-mapping code. This is the headline finding of this round — a real regression introduced by a commit whose stated purpose was visual fidelity to the mock.
2. INCPG-09 (AI-summary visual distinctness) — still only label-asserted, unchanged from iteration 1.
3. Unknown-severity fallback (spec edge case) — silently regressed from a working fallback to no fallback.

**Next steps**: Route the 3 fix tasks above to an implementer, prioritizing Fix 1 (Major). Re-verify afterward.
