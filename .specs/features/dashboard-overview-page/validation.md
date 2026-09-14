# Dashboard Overview Page Validation

**Date**: 2026-09-14
**Spec**: `.specs/features/dashboard-overview-page/spec.md`
**Diff range**: `7ca2a64..HEAD` (`38ca3fc` T1 … `29f6e57` test-hardening)
**Verifier**: standalone fresh-eyes pass (no general-purpose sub-agent available in this harness; author ≠ verifier maintained by re-deriving coverage from `spec.md` + the diff, not from the author's mental model)

---

## Task Completion

| Task | Status  | Notes |
| ---- | ------- | ----- |
| T1 `IncidentRepository.CountOpen` | ✅ Done | `38ca3fc` |
| T2 `DomainRepository.CountVerified` | ✅ Done | `15eba56` |
| T3 `OverviewHandler.Get` | ✅ Done | `d7d861c` |
| T4 route wiring | ✅ Done | `8e2e36a` |
| T5 frontend types | ✅ Done | `3efa36c` |
| T6 `useOverview` + MSW | ✅ Done | `607852e` |
| T7 `OverviewPage.tsx` + i18n | ✅ Done | `e2c7b6b` |
| T8 `App.tsx` routing | ✅ Done | `6d368d8` (1 SPEC_DEVIATION, below) |
| T9 Sidebar nav item | ✅ Done | `c6cd8dc` |
| — test hardening (sensor fix) | ✅ Done | `29f6e57` |

---

## Spec-Anchored Acceptance Criteria

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| OVW-01 authenticated `/` renders Overview, not `/domains` | Overview page on `/` | `web/src/App.test.tsx:136` - `getByRole("heading",{level:1,name:"Visão geral"})` | ✅ PASS |
| OVW-01 direct `/overview` renders Overview | same | `web/src/App.test.tsx:149` - same heading assertion | ✅ PASS |
| OVW-02 single `GET /api/overview` feeds 4 cards + chart + list | one request, 6 fields | `web/src/features/overview/hooks.test.ts:23-29` - `toHaveProperty` for all 6 + `uptime_series` length; `internal/cli/routes_test.go:555` - real router 200 for viewer | ✅ PASS |
| OVW-03 30d avg = mean of `UptimePercent` for `ok=true`; "—" when none | 95.0 with 100/90 services; nil when no data | `internal/api/overview_handler_test.go:213` - `*UptimeAvg30d != 95.0`; `:283` - `UptimeAvg30d != nil` (dash case) | ✅ PASS |
| OVW-04 "Incidentes abertos" = count `status <> 'resolved'` | 2 (2 open, 1 resolved) | `internal/db/incident_repository_test.go:563` - `want 2 (two investigating, one resolved excluded)`; `internal/api/overview_handler_test.go:217` - `OpenIncidents == 2` | ✅ PASS |
| OVW-05 "Serviços com problema" = count `CurrentStatus != "operational"` | 2 (outage + degraded) | `internal/api/overview_handler_test.go:220` - `UnhealthyServices != 2 (outage + degraded)` | ✅ PASS (⚠️ spec-precision, see below) |
| OVW-06 "Domínios verificados" = count `status='verified'` | 2 (2 verified, 1 pending) | `internal/db/domain_repository_test.go:342` - `want 2 (one still pending)`; `internal/api/overview_handler_test.go:223` - `VerifiedDomains == 1` | ✅ PASS |
| OVW-07 exactly 14 daily buckets, oldest first, per-day | 14 buckets, oldest→today | `internal/api/overview_handler_test.go:313` - `len != 14`; `:317-322` - consecutive `wantDate` + last == today; `web/src/features/overview/OverviewPage.test.tsx:77` - `toHaveLength(14)` | ✅ PASS |
| OVW-08 ≤3 incidents `created_at DESC`, resolved included | 3 newest, resolved present | `internal/api/overview_handler_test.go:368` - `len == 3`; `:371,374,377` - newest/third-newest IDs + `Status == "resolved"` | ✅ PASS |
| OVW-09 zero tenant → documented empty state, no 500 | "—", 0/0/0, empty list | `internal/api/overview_handler_test.go:245-269` - nil + 0s + 14 nil buckets + len 0; `web/src/features/overview/OverviewPage.test.tsx:64-68` | ✅ PASS |
| OVW-10 shortcut → `/services` | `/services` | `web/src/features/overview/OverviewPage.test.tsx:108` - `toHaveAttribute("href","/services")` | ✅ PASS |
| OVW-11 shortcut → `/domains` (Status Pages tab) | `/domains` | `OverviewPage.test.tsx:109` | ✅ PASS |
| OVW-12 shortcut → `/admins` | `/admins` | `OverviewPage.test.tsx:110` | ✅ PASS |
| OVW-13 shortcut → `/domains` | `/domains` | `OverviewPage.test.tsx:111` | ✅ PASS |
| OVW-14 all 4 shortcuts render regardless of role | 4 links, no page-level gate | `OverviewPage.test.tsx:108-111` (rendered as viewer/owner; page has no role branch) | ✅ PASS |
| OVW-15 standalone "Visão geral" first, above groups | first nav item | `web/src/layout/Sidebar.test.tsx:187-193` - link before first grouped item (`compareDocumentPosition`) | ✅ PASS |
| OVW-16 active on `/` and `/overview` | accent class both routes | `Sidebar.test.tsx:198-207` - `className` contains `text-accent` on both | ✅ PASS |

**Status**: ✅ All 16 ACs covered.

### Edge cases

- Endpoint failure → shared error state: `OverviewPage.test.tsx:124` asserts the `alert` message on 500. ✅
- Partial-data window: inherited from `history.UptimePercent` clipped denominator (unchanged). ✅ (no new logic)
- >3 incidents → exactly 3 + "Ver todos": `overview_handler_test.go:368` (backend cap) + `OverviewPage.test.tsx:88` ("Ver todos" → `/incidents`). ✅

### Flagged

- **⚠️ Spec-precision gap (OVW-05)**: AC5 says `CurrentStatus != "operational"` while its parenthetical says "(i.e., degraded or outage)". Implemented to the literal AC (`!=`), so a `not_configured` (never-polled) service currently counts as "com problema". Confirmed against the mock: no `not_configured` guidance. Not a defect against the AC as written; flagged so a product decision can tighten it to degraded/outage in a follow-up if desired.
- **SPEC_DEVIATION (T8)**: design.md's Integration Points said `RootRoute` renders `<OverviewPage />` directly. `RootRoute` is not inside `AuthenticatedLayout`, so a direct render drops the `AppShell` (sidebar/topbar). Implemented the same single replace hop as the previous `<Navigate to="/domains" />` (`App.tsx:88-96`, deviation comment in code). Functionally satisfies OVW-01; documented per `AGENTS.md`/coding-principles ("surface, don't silently deviate").

---

## Discrimination Sensor

Scratch: temporary `git worktree` at `/tmp/vane-sensor` (Go) + single-file backup/restore (frontend). Baseline `git status --porcelain` verified unchanged after cleanup (see Isolation).

| # | File:line | Mutation | Killed? |
| - | --------- | -------- | ------- |
| M1 | `internal/db/incident_repository.go` | `WHERE status <> 'resolved'` → `= 'resolved'` | ✅ Killed (`CountOpen() = 1, want 2`) |
| M2 | `internal/db/domain_repository.go` | `WHERE status = 'verified'` → `<> 'verified'` | ✅ Killed (`CountVerified() = 1, want 2`) |
| M3 | `internal/api/overview_handler.go` | `CurrentStatus != "operational"` → `== "operational"` | ❌ Survived → fix → ✅ Killed |
| M4 | `internal/api/overview_handler.go` | `overviewUptimeSeriesDays = 14` → `13` | ❌ Survived → fix → ✅ Killed |
| M5 | `internal/api/overview_handler.go` | `overviewRecentIncidents = 3` → `2` | ✅ Killed (`len(RecentIncidents) = 2, want 3`) |
| M6 | `web/src/App.tsx` | `Navigate to="/overview"` → `"/domains"` | ✅ Killed (App `/` test fails) |
| M7 | `web/src/features/overview/OverviewPage.tsx` | null `uptime_avg_30d` → always numeric | ✅ Killed (empty-state `—` fails) |

**Sensor depth**: lightweight, expanded to 7 mutations across backend + frontend.
**Survivor fixes (commit `29f6e57`)**:
- M3: the happy path had exactly one operational + one non-operational service, so both branches yielded `1`. Added a third `degraded` service (no intervals) so `!=`→2 vs `==`→1 distinguishes; re-run kills.
- M4: Go assertions derived the expected count from the same constant. Replaced with literal `14` in both tests; re-run kills (`len = 13, want 14`).

**Result**: 7/7 killed after the fix round - PASS ✅.

**Isolation**: pre-sensor porcelain == post-sensor porcelain (`.specs/STATE.md` modified, `design.md` untracked - both pre-existing). Worktree removed, disposable container destroyed.

---

## Code Quality

| Principle | Status |
| --------- | ------ |
| Minimum code / no speculative features | ✅ (no cache/materialized summary; Option A only) |
| Surgical changes | ✅ (new files added; existing files touched only for route/sidebar/i18n wiring) |
| No scope creep | ✅ (out-of-scope activity card + upsell banner explicitly omitted) |
| Matches existing patterns | ✅ (`statusIntervalReader` reuse, `Card.tsx`, `writeInternalError`, MSW conventions, `tlc` i18n) |
| Spec-anchored outcome check | ✅ all asserted values match the spec AC value |
| Per-layer Coverage Expectation | ✅ repository (happy+zero), handler (happy+empty+error), route (200+401), hook/page/sidebar unit |
| Every test maps to a requirement | ✅ no unclaimed tests |
| Guidelines followed | `AGENTS.md` §3/§4/§5, `.specs/STATE.md` AD-012 (Page[T] scope), AD-022 (RLS) |

---

## Gate Check

- **Backend gate**: `go build ./...`, `go vet ./...`, `gofmt -l` clean; `make test-integration` (disposable Postgres, `-p 1`, `max_connections=300`) - **all 20 packages ok, 0 failures**.
- **Frontend gate**: `npx tsc -b --noEmit` clean; `npm run test` - **436 passed, 0 failed** (78 files).
- **Test count before feature**: 423 frontend / backend baseline green.
- **Test count after feature**: frontend 436 (+13: 2 hooks, 7 page, 2 App, 2 Sidebar); backend +12 (4 `internal/db`, 6 `internal/api`, 2 `internal/cli`).
- **Skipped**: none. **Deleted**: none.

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 16/16 ACs matched spec outcome, 1 spec-precision gap flagged (OVW-05 `not_configured`).
**Sensor**: 7/7 mutations killed (2 after a fix round).
**Gate**: backend full integration green; frontend 436/436.

**What works**: tenant-wide aggregation endpoint (`GET /api/overview`) mounted on the real router; 4 summary cards, 14-day uptime chart, recent-incidents list, shortcuts grid; `/` and `/overview` route to the page inside the shell; sidebar entry.

**Issues found**: none blocking. Two non-blocking items surfaced above (OVW-05 precision; T8 SPEC_DEVIATION).

**Next steps**: none required for PASS. Optional follow-up: tighten OVW-05 to degraded/outage (product decision).
