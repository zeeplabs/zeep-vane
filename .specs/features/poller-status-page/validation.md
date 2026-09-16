# Poller Status Page Validation

**Date**: 2026-09-15
**Spec**: `.specs/features/poller-status-page/spec.md`
**Diff range**: `a231a49` (feat(poller): redesign poller status page to real leadership state)
**Verifier**: independent sub-agent (author ≠ verifier)

---

## Task Completion

No `tasks.md` for this feature (Execute inline, Medium scope per spec.md's own Requirement Traceability note). Single commit `a231a49` implements all 14 ACs.

---

## Spec-Anchored Acceptance Criteria

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| POLLPG-01: `leader_elected:true`+`poller_running:true` | badge "Ativo" + `replica.application_name` | `PollerStatusPage.test.tsx:107` - `expect(screen.getByText("Ativo · vane-0")).toBeInTheDocument()` | ✅ PASS |
| POLLPG-02: `leader_elected:true`+`poller_running:false` | badge "Aguardando integração" + replica name + alert banner | `PollerStatusPage.test.tsx:119-120` - `findByText("Aguardando integração · vane-0")`, `getByText("Réplica líder ativa, mas nenhuma integração Datadog conectada.")` | ✅ PASS |
| POLLPG-03: `leader_elected:false` | badge "Sem líder no momento", no replica name, no crash | `PollerStatusPage.test.tsx:130` - `findByText("Sem líder no momento")` (exact match rules out an appended replica name) | ✅ PASS |
| POLLPG-04: `checks_last_minute` incl. `0` as valid | value shown as-is, `0` never treated as error/empty | `PollerStatusPage.test.tsx:109` - `getByText("4")` only; no test sets `checks_last_minute: 0` | ⚠️ Spec-precision gap — the `0`-specific requirement (the AC's actual point) is untested |
| POLLPG-05: "Integrações conectadas" = `total` | card shows `total` | `PollerStatusPage.test.tsx:111` - `getByText("1")` | ✅ PASS |
| POLLPG-06: row badge + formatted `last_checked_at` | "Sucesso"/"Falha" dot+pill, formatted timestamp | `PollerStatusPage.test.tsx:47-49` - `getByText("Sucesso")`, `getAllByText("Última execução")` | ✅ PASS |
| POLLPG-07: `status !== "active"` shows `last_error` | error text below provider name | `PollerStatusPage.test.tsx:58-59` - `getByText("Falha")`, `getByText("Credenciais inválidas")` | ✅ PASS |
| POLLPG-08: `poller_running:false`+`leader_elected:true` banner | exact text "Réplica líder ativa, mas nenhuma integração Datadog conectada." | `PollerStatusPage.test.tsx:120` | ✅ PASS |
| POLLPG-09: failing integration banner names providers | reuses `PollerBanner.tsx`'s `failureMessage` text | `PollerStatusPage.test.tsx:141` - `getByText("Falha ao verificar a integração Datadog — última tentativa não teve sucesso.")`; text matches `PollerBanner.tsx:25` verbatim (duplicated function, not imported — see Code Quality) | ✅ PASS |
| POLLPG-10: both conditions true → both banners | two banners, neither omitted/overlapping | `PollerStatusPage.test.tsx:152,154` - both banner texts asserted present in same render | ✅ PASS |
| POLLPG-11: neither condition → no banner | zero banners rendered | `PollerStatusPage.test.tsx:163-166` - `queryByText(...).not.toBeInTheDocument()` for both | ✅ PASS |
| POLLPG-12: no shadow, `border-divider` | `elevation="none"`, `border-divider` on cards/list | No RTL assertion; confirmed by source read: `PollerStatusPage.tsx:27` (`border border-divider`), `:90` (`elevation="none"` + `border border-divider`) | ⚠️ Spec-precision gap — spec itself scopes this to a screenshot-based Independent Test, no automated assertion exists |
| POLLPG-13: dot+pill badge (`Tag`+dot span) | same pattern as `IntegrationCard`/`DomainStatusTag` | No RTL assertion of the dot span itself; confirmed by source read: `PollerStatusPage.tsx:96-102`, `:111` | ⚠️ Spec-precision gap — same as above, screenshot-scoped by spec |
| POLLPG-14: semantic text tokens (`text-text-muted`, not `text-neutral-400`) | no `text-neutral-*` in the file | `grep -n "text-neutral" PollerStatusPage.tsx` → no matches | ⚠️ Spec-precision gap — verified via static grep, not an automated test assertion |

**Status**: ⚠️ Spec-precision gaps flagged (4 of 14) — no functional GAP (0 uncovered ACs), but 4 ACs lack a test-level assertion matching the spec's precise outcome (one is a real coverage miss — POLLPG-04's `0` case; three are visual ACs the spec itself scopes to screenshot UAT, not unit tests).

---

## Discrimination Sensor

Isolated `git worktree add` at `/private/tmp/.../scratchpad/sensor-worktree` (detached HEAD at `a231a49`), `node_modules` symlinked in (no `npm install`), `PollerStatusPage.tsx` mutated in place, tests run against the worktree, mutation reverted, worktree removed with `git worktree remove --force`, `node_modules` symlink deleted first.

| Mutation | File:line | Description | Killed? |
| --- | --- | --- | --- |
| 1 | `PollerStatusPage.tsx:60` | Flipped `data.poller_running` → `!data.poller_running` in the pollerStatusLabel ternary (Ativo/Aguardando integração swap) | ✅ Killed (2 tests failed) |
| 2 | `PollerStatusPage.tsx:86` | Changed `failing.length > 0` → `failing.length > 1` (failing-integration banner never shows for a single failure) | ✅ Killed (2 tests failed) |
| 3 | `PollerStatusPage.tsx:79` | Changed `checks_last_minute` display to `data?.total` (checks/min card shows wrong field) | ✅ Killed (1 test failed) |

**Sensor depth**: lightweight (3 targeted mutations)
**Result**: 3/3 killed — PASS ✅

**Isolation check**: baseline `git status --porcelain` (real tree) was empty before the sensor ran; after worktree removal, `git status --porcelain` and `git worktree list` confirm the real tree is unchanged (clean, single worktree entry at `a231a49`/`develop`).

---

## Interactive UAT Results

Not performed — user-facing but no explicit "validate"/"UAT" trigger in this dispatch; orchestrator may schedule a follow-up UAT pass for the P3 visual ACs (12-14), which have no automated coverage.

---

## Code Quality

| Principle | Status |
| --- | --- |
| No features beyond what was asked | ✅ |
| No abstractions for single-use code | ✅ |
| No unnecessary "flexibility" added | ✅ |
| Only touched files required for task | ✅ (types/api.ts, hooks.ts, PollerStatusPage.tsx/.test.tsx, mockData.ts, msw handlers.ts — all directly implicated) |
| Didn't "improve" unrelated code | ✅ |
| Matches existing patterns/style | ✅ |
| Would senior engineer approve? | ⚠️ Minor: `failureMessage`/`providerLabel` are duplicated verbatim from `PollerBanner.tsx` instead of extracted/shared, and the two `providerLabel` implementations diverge (`PollerBanner.tsx` has a `PROVIDER_LABELS` map producing "SendGrid"/"Resend"; `PollerStatusPage.tsx`'s naive capitalize would render "Sendgrid"). Not exercised by any test (only "datadog" is tested in both places), so it's latent, not a regression. |
| Tests map to acceptance criteria and are non-shallow (spot-check one story) | ✅ — spot-checked P2 (banner combination test at line 145-156 asserts both banner texts together, not just presence of a generic banner) |
| Spec-anchored outcome check: each test's asserted value matches the spec-defined outcome (or gap flagged) | ⚠️ 4 gaps flagged above |
| Per-layer Coverage Expectation met | ✅ — frontend component/hook coverage; no backend routes in scope (endpoint pre-existing, no contract change per spec Out of Scope) |
| Every test in scope maps to a spec AC, listed edge case, or Done-when criterion (no unclaimed tests) | ✅ — reviewed all 11 tests in `PollerStatusPage.test.tsx`, each traces to a POLLPG-NN or a listed edge case |
| Documented project quality/testing guidelines followed | AGENTS.md §5 (MSW mirrors backend shape, `Pager`/`queryKey` conventions) — followed: `hooks.ts:7` includes `page` in `queryKey`; `handlers.ts:1905-1911` keeps leadership fields loose alongside the paginated envelope, matching the `email-providers` precedent AGENTS.md §4 documents |

---

## Edge Cases

- [x] `GET /api/poller/status` 5xx/network error → isolated error state: `PollerStatusPage.test.tsx:169-175` asserts `findByText("Não foi possível carregar o status do poller.")` after a 500 override — PASS
- [ ] `items` empty → "Nenhuma integração conectada." with stat cards still rendering (`checks_last_minute:0`, `total:0`) — **NOT tested**. Code path exists (`PollerStatusPage.tsx:91-92`) but no test drives an empty `items` fixture through MSW to exercise it.
- [x] `replica: null` while `leader_elected: true` (shouldn't happen per backend contract, but type allows it) → treated as "Sem líder no momento" fallback, never a null-access crash — **NOT directly tested** (the only `replica: null` test also sets `leader_elected: false`, so it doesn't isolate this specific combination), but the code is provably safe by construction: `replicaName` is independently gated on `data?.leader_elected` (line 63) and the stat-card ternary is gated on `!data.leader_elected` before it ever reaches `data.poller_running`/replica logic, so `leader_elected:true`+`replica:null` cannot reach a null-dereference — `data.replica?.application_name ?? null` optional-chains safely regardless. Marking as code-verified but test-uncovered.

---

## Gate Check

- **Gate command**: `cd web && npx tsc -b --noEmit && npx vitest run`
- **Result**: `tsc -b --noEmit` clean (no output/errors). `vitest run`: 90 test files passed, 526 tests passed, 0 failed, 0 skipped.
- **Test count before feature** (parent commit `a231a49^`, `PollerStatusPage.test.tsx`): 4 `it(...)` blocks
- **Test count after feature** (`a231a49`): 11 `it(...)` blocks
- **Delta**: +7 new tests, all mapped to POLLPG-01/02/03/08/09/10/11 above
- **Skipped tests**: none
- **Failures**: none

---

## Fix Plans

No blocking gaps — nothing rises to a required fix. Two optional follow-ups (not blocking PASS):

### Fix 1 (optional, Minor): POLLPG-04's `checks_last_minute: 0` case is untested
- **Root cause**: The existing default fixture (`pollerLeadership.checks_last_minute = 4`) never gets overridden to `0` in any test, so the AC's explicit "0 is valid, not an error" requirement has no assertion.
- **Fix task**: Add a test that sets `pollerLeadership.checks_last_minute = 0` and asserts the "Verificações/min" card shows `"0"` (not `"-"`, not an error state).
- **Priority**: Minor

### Fix 2 (optional, Minor): empty `items` edge case untested
- **Root cause**: No test overrides the MSW handler to return `items: []`.
- **Fix task**: Add a test with `items: []`, `total: 0` and assert `"Nenhuma integração conectada."` renders while the stat cards still show `0`/`0`.
- **Priority**: Minor

### Fix 3 (optional, Cosmetic/code-quality): duplicated `failureMessage`/`providerLabel` diverge from `PollerBanner.tsx`
- **Root cause**: Copy-pasted instead of extracted to a shared module; `PollerStatusPage.tsx`'s `providerLabel` doesn't use `PollerBanner.tsx`'s `PROVIDER_LABELS` map, so non-"datadog" providers would render inconsistently between the banner and the detail page (e.g. "Sendgrid" vs "SendGrid").
- **Fix task**: Extract `failureMessage`/`providerLabel`/`PROVIDER_LABELS` to a shared `poller/format.ts` (or similar) imported by both `PollerBanner.tsx` and `PollerStatusPage.tsx`.
- **Priority**: Cosmetic

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| --- | --- | --- |
| POLLPG-01 | Verified (author-claimed) | ✅ Verified (independently confirmed) |
| POLLPG-02 | Verified (author-claimed) | ✅ Verified (independently confirmed) |
| POLLPG-03 | Verified (author-claimed) | ✅ Verified (independently confirmed) |
| POLLPG-04 | Verified (author-claimed) | ⚠️ Verified with spec-precision gap (0-case untested) |
| POLLPG-05 | Verified (author-claimed) | ✅ Verified (independently confirmed) |
| POLLPG-06 | Verified (author-claimed) | ✅ Verified (independently confirmed) |
| POLLPG-07 | Verified (author-claimed) | ✅ Verified (independently confirmed) |
| POLLPG-08 | Verified (author-claimed) | ✅ Verified (independently confirmed) |
| POLLPG-09 | Verified (author-claimed) | ✅ Verified (independently confirmed) |
| POLLPG-10 | Verified (author-claimed) | ✅ Verified (independently confirmed) |
| POLLPG-11 | Verified (author-claimed) | ✅ Verified (independently confirmed) |
| POLLPG-12 | Verified (author-claimed) | ⚠️ Verified via source read only, no automated test (spec-scoped to screenshot UAT) |
| POLLPG-13 | Verified (author-claimed) | ⚠️ Verified via source read only, no automated test (spec-scoped to screenshot UAT) |
| POLLPG-14 | Verified (author-claimed) | ⚠️ Verified via static grep only, no automated test |

---

## Summary

**Overall**: ✅ Ready (PASS, with flagged spec-precision gaps — none blocking)

**Spec-anchored check**: 10/14 ACs matched spec outcome exactly with a discriminating test; 4 spec-precision gaps flagged (1 real coverage miss — POLLPG-04's `0` case; 3 visual ACs the spec itself scopes to screenshot UAT rather than unit tests)
**Sensor**: 3/3 mutations killed
**Gate**: 526 passed, 0 failed (tsc clean)

**What works**: All 4 combined poller-leadership states (active/awaiting/no-leader × integrations ok/failing) render correctly per fixture-driven tests; both alert banners and their simultaneous-display case are covered; error and pagination paths (pre-existing, retained) still pass; the discrimination sensor confirms the tests actually catch regressions in the three highest-risk behaviors (leadership label logic, failure banner condition, checks/min field binding).

**Issues found**:
1. POLLPG-04's "0 is a valid value" requirement — the AC's actual point — has no test exercising `checks_last_minute: 0`. See Fix 1.
2. The "empty items list" edge case (spec.md Edge Cases §2) has no test. See Fix 2.
3. `failureMessage`/`providerLabel` duplicated between `PollerBanner.tsx` and `PollerStatusPage.tsx` with diverging provider-label logic (latent, not currently exercised by any failing provider other than "datadog"). See Fix 3.

**Next steps**: None blocking. Optionally route Fix 1/2/3 as low-priority follow-up tasks; re-verification not required for a PASS.
