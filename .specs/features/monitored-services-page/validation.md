# Monitored Services Page Validation

**Date**: 2026-09-14
**Spec**: `.specs/features/monitored-services-page/spec.md`
**Diff range**: `a0f64e6~1..3102254` (develop) — T1 through T11
**Verifier**: independent sub-agent (author ≠ verifier)

---

## Task Completion

| Task | Status  | Notes |
| ---- | ------- | ----- |
| T1   | ✅ Done | `internal/db/migrations/0033_service_slo_name.up/down.sql` + `service_slo_name_migration_test.go` |
| T2   | ✅ Done | `internal/db/service_repository.go` `SLOName`, `Get` |
| T3   | ✅ Done | `internal/db/incident_repository.go:263` `CountByServiceSince` |
| T4   | ✅ Done | `internal/api/services_handler.go` `List`/`Create` extended |
| T5   | ✅ Done | `internal/api/services_handler.go:223` `Get` + `internal/cli/routes.go` wiring |
| T6   | ✅ Done | `web/src/features/services/statusMeta.ts` |
| T7   | ✅ Done | `web/src/features/services/hooks.ts` |
| T8   | ✅ Done | `web/src/features/services/AddServiceDrawer.tsx` |
| T9   | ✅ Done | `web/src/features/services/ServiceListPage.tsx` |
| T10  | ✅ Done | `web/src/features/services/ServiceDetailDrawer.tsx` |
| T11  | ✅ Done | `web/src/features/services/ServicesPage.tsx` |

---

## Spec-Anchored Acceptance Criteria

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| SVC-01: table columns for every service | Status, Serviço(+SLO), Uptime 30d, Última verificação, all rows | `web/src/features/services/ServiceListPage.test.tsx:104-114` — asserts all 4 fixture names render; backing data from `internal/api/services_handler.go:148-156` (`List`) | ✅ PASS |
| SVC-02: `operational` → "Operacional" green | exact label | `ServiceListPage.test.tsx:123` — `within(rows[0]).getByText("Operacional")` | ✅ PASS |
| SVC-03: `degraded` → "Degradado" amber | exact label | `ServiceListPage.test.tsx:124` | ✅ PASS |
| SVC-04: `outage` → "Inativo" red | exact label (SPEC_DEVIATION: was "Inoperante") | `ServiceListPage.test.tsx:125`; `statusMeta.ts:17` | ✅ PASS |
| SVC-05: `not_configured` → "Não configurado" neutral | exact label | `ServiceListPage.test.tsx:126` | ✅ PASS |
| SVC-06: no open interval → "—" for uptime/last-check | literal "—", 2 occurrences | `ServiceListPage.test.tsx:129-138` — `dashes).toHaveLength(2)` | ✅ PASS |
| SVC-07: no Latência column anywhere | absent | `ServiceListPage.test.tsx:113` — `queryByText(/Latência/i)).not.toBeInTheDocument()` | ✅ PASS |
| SVC-08: Pager `totalPages=ceil(total/page_size)` | exact formula | `ServiceListPage.test.tsx:204-211` — total 25/pageSize 20 → "Página 1 de 2"; `ServiceListPage.tsx:55` | ✅ PASS |
| SVC-09: chip click filters; "Todos" shows all | exact behavior | `ServiceListPage.test.tsx:140-153` | ✅ PASS |
| SVC-10: search narrows by name/SLO, case-insensitive | exact | `ServiceListPage.test.tsx:169-178` — types "CHECKOUT" (uppercase), matches "Checkout" | ✅ PASS |
| SVC-11: filter+search AND, not OR | exact | `ServiceListPage.test.tsx:180-192` — Operacional chip + "Checkout" query yields 0 rows | ✅ PASS |
| SVC-12: empty-state string | literal "Nenhum serviço encontrado com esses filtros." | `ServiceListPage.test.tsx:191,201` | ✅ PASS |
| SVC-13: chip counts scoped to current page | exact counts (4/1/1/1/1) | `ServiceListPage.test.tsx:155-167` | ✅ PASS |
| SVC-14: drawer shows uptime/last-check/incidents | exact values | `ServiceDetailDrawer.test.tsx:72-81` — 99.87%, formatted date, "3" | ✅ PASS |
| SVC-15: note renders when degraded + non-empty analysis | exact text | `ServiceDetailDrawer.test.tsx:83-89` | ✅ PASS |
| SVC-16: note absent otherwise (both branches) | absent | `ServiceDetailDrawer.test.tsx:91-98` (degraded, empty analysis) and `:100-107` (operational, non-empty analysis) | ✅ PASS |
| SVC-17: exactly 24 bars, backend BuildBuckets | literal 24, not implementation constant | `ServiceDetailDrawer.test.tsx:109-116` — `toHaveLength(24)`; backend `services_handler_test.go:517-540` `TestGetService_ZeroHistory_Exactly24BucketsAllNoData` — `len(detail.HourlyBuckets) != 24` | ✅ PASS |
| SVC-18: close control + backdrop both close | exact | `ServiceDetailDrawer.test.tsx:118-143` (two tests) | ✅ PASS |
| SVC-19: no "Pausar monitoramento"/"Editar configuração" | absent | `ServiceDetailDrawer.test.tsx:145-153` | ✅ PASS |
| SVC-20: "Adicionar serviço" opens drawer with name+SLO fields | exact | `ServiceListPage.test.tsx:226-237` (button triggers callback); `ServicesPage.test.tsx:81-98` (end-to-end through route); `AddServiceDrawer.test.tsx:34-35` (fields present) | ✅ PASS |
| SVC-21: SLO search queries `GET .../slos?query=`, lists matches | functional match; "debounced" timing itself not independently asserted | `AddServiceDrawer.test.tsx:41-43` (typed query surfaces "Checkout" option) | ⚠️ Spec-precision gap (debounce timing not asserted, reuses pre-existing untouched `useSLOSearch`) |
| SVC-22: submit calls `POST /api/services {name, slo_id}` (+ `slo_name`), closes, refreshes | exact body | `AddServiceDrawer.test.tsx:48-70` — `expect(body).toEqual({name, slo_id, slo_name})`; `ServicesPage.test.tsx:81-98` — new service visible without reload | ✅ PASS |
| SVC-23: submit disabled until name+SLO both present | exact | `AddServiceDrawer.test.tsx:30-46` (`toBeDisabled()`/`toBeEnabled()`); edge case `:72-90` | ✅ PASS |
| SVC-24: failed submit → inline error, drawer stays open | exact | `AddServiceDrawer.test.tsx:92-110` — 500 response → `role="alert"` text "erro interno", `onOpenChangeCalls === 0` | ✅ PASS |
| SVC-25: no "Polling manual"/"New Relic" anywhere | absent | `AddServiceDrawer.test.tsx:121-127` | ✅ PASS |

**Status**: ✅ All ACs covered (24/25 clean PASS, 1 spec-precision gap on SVC-21's "debounced" nuance — functionality is covered, timing is not).

### Edge cases (spec.md)

- [x] Zero `StatusInterval` rows → `not_configured` + "—", not an error: `services_handler_test.go:328-352` (`TestListServices_NeverPolled_UptimeAndLastSeenNil`), `services_handler_test.go:517-540` (zero-history detail)
- [x] Datadog SLO search returns zero results → "Nenhum SLO encontrado.": `AddServiceDrawer.test.tsx:112-119`
- [x] Unauthenticated `/services` → redirect to `/login`: `ServicesPage.test.tsx:49-57`
- [x] Mixed not_configured/polled same page (design.md Risks row 3 regression): `services_handler_test.go:360-417` (`TestListServices_MixedNotConfiguredAndPolled_SamePage`)

---

## Discrimination Sensor

Isolated in a temporary `git worktree` at `/tmp/vane-sensor-scratch` (never the real tree). Baseline `git status --porcelain` was empty before and after.

| Mutation | File:line | Description | Killed? |
| -------- | --------- | ------------ | ------- |
| 1 | `web/src/features/services/ServiceDetailDrawer.tsx:52` | `current_status === "degraded" && !!status_analysis` → `\|\|` | ✅ Killed (`ServiceDetailDrawer.test.tsx` — 4 failures across the note-rendering tests) |
| 2 | `web/src/features/services/ServiceListPage.tsx:82` | `matchesStatus && matchesQuery` → `\|\|` | ✅ Killed (`ServiceListPage.test.tsx` — 1 failure, the AND/empty-state test) |
| 3 | `internal/api/services_handler.go:30` | `servicesHourlyBucketCount = 24` → `23` | ✅ Killed (`TestGetService_ZeroHistory_Exactly24BucketsAllNoData` fails: `len = 23, want 24`) |

**Sensor depth**: lightweight (default tier)
**Result**: 3/3 killed — ✅ PASS

Worktree removed after the run (`git worktree remove --force`); `web/node_modules` symlink used for the frontend runs was deleted before removal. `git status --porcelain` on the real tree confirmed identical before/after (empty both times).

---

## Code Quality

| Principle | Status |
| --- | --- |
| Minimum code | ✅ — batch-interval reuse (`OverviewHandler` pattern), no new abstractions beyond what design.md specified |
| Surgical changes | ✅ — `ServicesSection.tsx` untouched beyond the `statusMeta.ts` import swap (T6), its own tests pass unmodified |
| No scope creep | ✅ — `canManage` prop on `ServiceListPage` (SPEC_DEVIATION c) is a real regression guard (pre-existing role gate), not new scope; documented in task context |
| Matches existing patterns | ✅ — `Page[T]` envelope kept for `List`, flat DTO for `Get` (matches `OverviewResponse` precedent); `Pager`/`Card`/`Tag`/`Drawer` reused from `components/ui` |
| Spec-anchored outcome check | ✅ — see AC table above, values not just "assertion exists" |
| Per-layer Coverage Expectation met | ✅ — migration/repository/handler all integration-tested with happy+edge+error paths (404 fixed body, mixed-fixture regression, both note branches); frontend unit tests 1:1 to ACs |
| Every test maps to an AC/edge case | ✅ — spot-checked `ServiceListPage.test.tsx` and `services_handler_test.go`; every test carries an SVC-tag or edge-case comment |
| Documented guidelines followed | ✅ — `AGENTS.md` §3 (gate commands), §4 (404 fixed body, no `err.Error()` leak), §5 (`Pager` caller-computed `totalPages`, MSW mirrors `Page<T>`, i18n strings) |

---

## Gate Check

- **Gate command**: `go build ./... && go vet ./... && go test ./... && gofmt -l $(git diff --name-only --diff-filter=ACM -- '*.go') && make test-integration && cd web && npx tsc -b --noEmit && npm run test`
- **Result**: all stages passed, 0 failed
  - `go build ./...` — clean
  - `go vet ./...` — clean
  - `gofmt -l` on changed `.go` files — no output (clean)
  - `go test ./...` (unit) — all packages `ok`
  - Integration gate (disposable `postgres:16-alpine` container on port 5433, `-p 1`, destroyed after) — all packages `ok`, including `internal/api` (40.2s) and `internal/db` (31.0s)
  - `cd web && npx tsc -b --noEmit` — clean, no errors
  - `npm run test` — **81 test files passed, 463 tests passed**, 0 failed
- **Skipped tests**: none
- **Failures**: none

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| --- | --- | --- |
| SVC-01..20, SVC-22..25 | Done (implementer-reported) | ✅ Verified |
| SVC-21 | Done (implementer-reported) | ✅ Verified (functional), ⚠️ debounce timing not independently test-asserted |

---

## Visual Parity Observation (not a pass/fail gate item)

Compared `handoff-new-layout/Servicos Monitorados.dc.html` against `ServiceListPage.tsx`, `ServiceDetailDrawer.tsx`, `AddServiceDrawer.tsx`:

- **Column order**: mock is Status / Serviço / Uptime 30d / Latência / Última verificação; the implementation drops Latência (per spec Out of Scope) and keeps the remaining four in the same left-to-right order. Structural parity holds.
- **Filter chips + search row**: same layout (chips left, search box right, same row), plus a 5th "Não configurado" chip the mock doesn't have (a documented, deliberate spec extension, not a deviation from intent).
- **Detail drawer**: mock's 2×2 stat grid (Uptime/Latência/Última verificação/Incidentes) becomes a 3-stat grid with Latência dropped; the "Últimas 24 verificações" strip is reproduced almost pixel-for-pixel (`gap-[3px]`, `w-[12px]`/`h-[22px]` bars matching the mock's `gap:3px`/`width:12px;height:22px`). "Pausar monitoramento"/"Editar configuração" buttons correctly omitted per spec.
- **Add drawer**: intentionally simplified relative to the mock (no "Como monitorar"/"Fonte" chips, no polling fields) — correct per spec's Out of Scope (SLO-only, Datadog-only). Copy ("Vincular serviço"/"Salvar") differs from the mock's "Adicionar serviço" wording, a deliberate, documented SPEC_DEVIATION to avoid breaking `ServicesSection`'s existing tests; SVC-20's literal "Adicionar serviço" trigger label lives on `ServiceListPage`'s own button, which is the actually-specified requirement.

**Opinion**: structural parity is good — column order, chip row, and drawer section layout all track the mock where the spec didn't explicitly diverge; the copy deviation is well-justified and narrowly scoped, not a sign of drift.

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 24/25 ACs matched spec outcome exactly; 1 spec-precision gap (SVC-21 debounce timing, functionally covered)
**Sensor**: 3/3 mutations killed
**Gate**: all stages passed (backend build/vet/test/integration, frontend tsc/test — 463 tests)

**What works**: Full list/filter/search/drawer/add flow works end-to-end against real integration-tested backend endpoints; no Latência anywhere; all 4 status badges correct; mixed not_configured/polled regression explicitly guarded; discrimination sensor confirms tests actually catch behavior-level regressions in the two highest-risk conditionals (degraded-note AND, filter/search AND) and the 24-bucket contract.

**Issues found**: None blocking. SVC-21's debounce timing is asserted only by inference (existing `useSLOSearch` hook, untouched by this feature) — no fix task warranted, since the hook itself predates this feature and its own test suite (`integrations/hooks.test.ts`) already covers its behavior.

**Next steps**: None required. Feature ready to close out.
