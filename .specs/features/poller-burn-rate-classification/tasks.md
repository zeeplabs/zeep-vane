# Poller Burn-Rate Classification Tasks

**Design**: `.specs/features/poller-burn-rate-classification/design.md`
**Status**: In execution (2026-10-22) - T1-T8 done; T9 full gate green (2026-10-22), staging check pending deploy

Gate commands (AGENTS.md §3, no `.specs/codebase/TESTING.md` in this repo):

- **quick (Go)**: `go build ./... && go vet ./... && go test ./internal/<pkg>/... && gofmt -l <changed files>`
- **quick (web)**: `cd web && npx tsc -b --noEmit && npm run test -- <changed test files>`
- **full**: quick for all packages + `make test-integration` (disposable Postgres, never `vane-dev-pg`) + `cd web && npm run test`

Commits: one per task with the message listed, **only when the user asks to commit** (AGENTS.md §2).

---

## Execution Plan

### Phase 1: Foundations (parallel)

```
T1 [P]   T2 [P]   T3 [P]
```

### Phase 2: Wiring

```
T1 + T2 + T3 ──→ T4 ──→ T5
T1 ──→ T6 ──→ T7
```

### Phase 3: Docs + gate

```
T4 + T5 + T7 ──→ T8 ──→ T9
```

---

## Task Breakdown

### T1: Persist latest reading on `services` [P]

**What**: Migration `0052_service_latest_reading` (up/down), `Service` struct fields, `ServiceReading` type and `ServiceRepository.UpdateStatusAndReading`; scan the new columns wherever `Service` rows are loaded.
**Where**: `internal/db/migrations/0052_service_latest_reading.{up,down}.sql`, `internal/db/service_repository.go`
**Depends on**: None
**Reuses**: `UpdateStatus` (same tenant tx/RLS pattern), existing nullable-column scan pattern (`StatusAnalysis`)
**Requirement**: BURN-13, BURN-17 (restart-safe grace)
**Done when**:
- [ ] Six nullable columns added; down drops them
- [ ] `UpdateStatusAndReading` writes status + `last_status_change_at` (same semantics as `UpdateStatus`) + reading in one `UPDATE`
- [ ] Integration test: write/read under tenant RLS; other tenant cannot see it; migration up/down clean
**Tests**: integration
**Gate**: full (`make test-integration`)
**Commit**: `feat(db): store each service's latest burn-rate reading`

---

### T2: Connector `FetchSLOWindows` [P]

**What**: Fetch one 1h `/history` window (+1 bucket), parse per-bucket series into short (last 5m) and long (1h) request/error counts; fallback second call when `series.interval > 60s`; `HasSeries=false` when no series; rate-limit header logging.
**Where**: `internal/connectors/datadog/client.go`, `internal/connectors/datadog/client_test.go`
**Depends on**: None
**Reuses**: `sloHistoryResponse`, threshold pick, request helpers
**Requirement**: BURN-01, BURN-17 (clamp), NB-7 fallback signal
**Done when**:
- [ ] `SLOWindows`/`WindowCounts` types exported; `FetchSLOWindows` added to the provider interface used by the poller
- [ ] `httptest` cases with float-valued JSON: 60s series with partial first bucket, errors > requests clamped, coarse interval triggers fallback call, missing series → `HasSeries=false`, 5xx/decode errors returned
- [ ] Existing `FetchSLOStatus` tests still pass (kept for other callers until T4 removes poller usage)
**Tests**: unit
**Gate**: quick (Go)
**Commit**: `feat(datadog): fetch short and long SLO windows from one history call`

---

### T3: Pure burn-rate classifier [P]

**What**: `burnRate` and `classifyBurn` with the design's rules and constants (`14.4`, `7.2`, min errors `3`, grace `15m`, recovery hold `0`).
**Where**: `internal/poller/burn_rate.go`, `internal/poller/burn_rate_test.go`
**Depends on**: None (defines its own `WindowCounts` mirror or imports T2's type if T2 lands first; same field names)
**Reuses**: Nothing (new pure logic), doc-comment style of `breach_threshold.go`
**Requirement**: BURN-02..08, BURN-10, BURN-17
**Done when**:
- [ ] Table test covers: healthy; gateway hovering just under/over 7.2x; sustained outage; 2-error noise ignored; 3-error incident caught; recovery on first clean short window; zero traffic while operational; zero traffic in outage before/after 15 min (with `LastErrorAt` nil → `LastChangeAt`); `not_configured` with empty/non-empty long window; empty long window uses short burn; target 100; errors > requests
- [ ] Constants documented with replay numbers (design.md)
**Tests**: unit
**Gate**: quick (Go)
**Commit**: `feat(poller): add multi-window burn-rate classifier`

---

### T4: Wire classifier into `pollService`, remove AD-019 heuristics

**What**: `pollService` calls `FetchSLOWindows`, uses `classifyByState` for `Target <= 0` or `!HasSeries`, else `classifyBurn`; tracks `LastErrorAt`; persists via `UpdateStatusAndReading`; new debug log fields. Delete `breach_threshold.go(+test)`, `breachHysteresisCycles`, `breachStreak`, `minRecentWindowRequests`, `recentWindowWidth` and their tests; update `FetchWithRetry`/fake provider.
**Where**: `internal/poller/poller.go`, `internal/poller/retry.go`, `internal/poller/*_test.go`
**Depends on**: T1, T2, T3
**Reuses**: `classifyByState`, `OpenOrExtend`, analyzer dispatch block (unchanged)
**Requirement**: BURN-01, BURN-06, BURN-09, BURN-11, BURN-17
**Done when**:
- [ ] Fetch failure contract unchanged (test)
- [ ] Transition still dispatched exactly once, after persistence (test)
- [ ] Restart-safety: a fresh `Poller` honors the grace from persisted `last_error_at` (integration test)
- [ ] No reference to removed identifiers remains (`grep`)
**Tests**: unit + integration
**Gate**: full
**Commit**: `feat(poller): classify services by multi-window burn rate`

---

### T5: Burn numbers in LLM prompts

**What**: `AnalysisInput` gains `BurnRateShort`, `BurnRateLong`, `WindowErrors`, `WindowRequests`; `burnSentence` appended in outage description, degraded tooltip and closing-comment prompts when `WindowRequests > 0`; `buildAnalysisInput` fills them from the cycle's windows.
**Where**: `internal/llm/prompts.go`, `internal/llm/prompts_test.go`, `internal/poller/analyzer.go`, `internal/poller/analyzer_test.go`
**Depends on**: T4 (call site passes windows)
**Reuses**: `causeSentence` pattern
**Requirement**: BURN-15
**Done when**:
- [ ] Prompt tests: sentence present with numbers; absent when no requests; system prompt unchanged
- [ ] Analyzer test: input carries the cycle's values
**Tests**: unit
**Gate**: quick (Go)
**Commit**: `feat(llm): give incident prompts the burn rate behind a status`

---

### T6: `latest_reading` in admin service detail

**What**: Service detail response adds `latest_reading` (`null` when no reading); public status handler untouched with a regression test.
**Where**: `internal/api/services_handler.go`, `internal/api/services_handler_test.go`, `internal/api/public_status_handler_test.go`
**Depends on**: T1
**Reuses**: detail response assembly (`Get`, line ~393)
**Requirement**: BURN-13, BURN-14 (API), BURN-16
**Done when**:
- [ ] Handler test: populated and null reading shapes
- [ ] Public payload test asserts no `latest_reading`/burn keys
**Tests**: unit (+ integration if the handler test suite for detail is integration-tagged)
**Gate**: full
**Commit**: `feat(api): expose a service's latest burn-rate reading to admins`

---

### T7: "Última leitura" block in `ServiceDetailDrawer`

**What**: Types, MSW fixture mirror, drawer block (error rate in 5 min, burn 5m/1h as `12.3x`, relative time), empty state, pt-BR/en keys.
**Where**: `web/src/types/api.ts`, `web/src/test/msw/handlers.ts`, `web/src/features/services/ServiceDetailDrawer.tsx`, `web/src/features/services/*.test.tsx`, `web/src/locales/{pt-BR,en}.json`
**Depends on**: T6
**Reuses**: `Stat` component, existing `services.detail.*` i18n namespace
**Requirement**: BURN-14
**Done when**:
- [ ] Vitest: renders values; renders empty state for `null`
- [ ] MSW mirrors backend shape exactly
- [ ] No hardcoded strings; i18n check passes
**Tests**: unit (vitest)
**Gate**: quick (web)
**Commit**: `feat(web): show the latest burn-rate reading on service detail`

---

### T8: Record the decision

**What**: `AD-049` in `.specs/STATE.md` (decision, replay evidence in aggregate, trade-offs: grace unvalidated by prod data, flapping on near-threshold services, undocumented rate limit; status) and mark AD-019 addenda 3-5 classification rules superseded; `CHANGELOG.md` `[Unreleased]`; spec traceability statuses updated.
**Where**: `.specs/STATE.md`, `CHANGELOG.md`, `.specs/features/poller-burn-rate-classification/spec.md`
**Depends on**: T4, T5, T7
**Reuses**: AD entry format
**Requirement**: all (traceability)
**Done when**:
- [ ] AD-049 added; AD-019 addenda statuses updated
- [ ] No customer-identifying data in repo docs (aggregates only)
**Tests**: none
**Gate**: build
**Commit**: `docs(state): record AD-049 burn-rate classification`

---

### T9: Full gate + staging check

**What**: Run full gate; after the user deploys to staging, check the poller debug log for one low-traffic and one high-traffic service against the Success Criteria.
**Where**: n/a
**Depends on**: T8
**Requirement**: Success Criteria
**Done when**:
- [ ] `go build/test/vet`, `gofmt`, `make test-integration`, `npx tsc -b --noEmit`, `npm run test` all green, outputs reported
- [ ] Staging check reported (or explicitly marked pending deploy)
**Tests**: all
**Gate**: full

---

## Validation

### Check 1: Granularity

| Task | One deliverable? | Notes |
| --- | --- | --- |
| T1 | ✅ | Storage for one concept (reading) |
| T2 | ✅ | One connector method |
| T3 | ✅ | One pure function pair |
| T4 | ✅ | One call site swap + dead-code removal of what it replaces |
| T5 | ✅ | Prompt input only |
| T6 | ✅ | One endpoint field |
| T7 | ✅ | One UI block |
| T8 | ✅ | Docs |
| T9 | ✅ | Verification |

### Check 2: Diagram ↔ Depends on

| Task | Depends on (definition) | Diagram | Match |
| --- | --- | --- | --- |
| T1 | None | Phase 1 | ✅ |
| T2 | None | Phase 1 | ✅ |
| T3 | None | Phase 1 | ✅ |
| T4 | T1, T2, T3 | T1+T2+T3 → T4 | ✅ |
| T5 | T4 | T4 → T5 | ✅ |
| T6 | T1 | T1 → T6 | ✅ |
| T7 | T6 | T6 → T7 | ✅ |
| T8 | T4, T5, T7 | T4+T5+T7 → T8 | ✅ |
| T9 | T8 | T8 → T9 | ✅ |

### Check 3: Test co-location

| Task | Layer | Required tests (AGENTS.md §3) | In task | Match |
| --- | --- | --- | --- | --- |
| T1 | DB | integration | integration | ✅ |
| T2 | connector | unit | unit | ✅ |
| T3 | poller logic | unit | unit | ✅ |
| T4 | poller + DB | unit + integration | unit + integration | ✅ |
| T5 | llm/poller | unit | unit | ✅ |
| T6 | API | unit/integration | unit (+integration) | ✅ |
| T7 | web | vitest | vitest | ✅ |
| T8 | docs | none | none | ✅ |
| T9 | all | full gate | full gate | ✅ |

`[P]` on T1-T3: separate packages, no shared files. T1's integration tests need the disposable DB; T2/T3 are pure unit, so they don't contend for it.
