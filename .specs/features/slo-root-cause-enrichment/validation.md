# SLO Root-Cause Enrichment Validation

**Date**: 2026-09-22
**Spec**: `.specs/features/slo-root-cause-enrichment/spec.md`
**Diff range**: `c22db07..HEAD` (14 commits, `ff00ad3`..`dad22ea`)
**Verifier**: independent sub-agent (author ≠ verifier)

---

## Task Completion

| Task | Status | Notes |
| --- | --- | --- |
| T1 | ✅ Done | `internal/connectors/datadog/client.go` - `SearchErrorTrackingIssues` + `post` helper |
| T2 | ✅ Done | `SLOSummary.SLOType`/`ServiceTag` decoding in `SearchSLOs` |
| T3 | ✅ Done | Migration `0039` applies/reverts cleanly (proven by `make`-equivalent integration run below) |
| T4 | ✅ Done | `db.Service.SLOType`/`DatadogServiceTag` + repository CRUD |
| T5 | ✅ Done | `RootCauseEnrichmentEnabled`/`SetRootCauseEnrichmentEnabled` on LLM settings repo |
| T6 | ✅ Done | `services_handler.go` accepts/returns the two new fields |
| T7 | ✅ Done | `llm.Service.SetRootCauseEnrichmentEnabled` |
| T8 | ✅ Done | `PATCH /api/integrations/llm/settings`, wired under `writeRoles` |
| T9 | ✅ Done | `AnalysisInput.CauseType/CauseMessage` + prompt extension + doc-comment fix |
| T10 | ✅ Done | `errorCauseProvider`/`enrichmentSettingsReader` + `resolveCauseHint` |
| T11 | ✅ Done | Wired into `dispatchDegradedEnrichment`/`dispatchOutageEnrichment` |
| T12 | ✅ Done | `internal/cli/serve.go` boot wiring |
| T13 | ✅ Done (with SPEC_DEVIATION, see below) | `AddServiceDrawer.tsx` capture + necessary backend read-path plumbing |
| T14 | ✅ Done (with SPEC_DEVIATION, see below) | `IntegrationsPage.tsx` toggle + necessary backend read-path plumbing |

All 14 tasks marked `[x]` in `tasks.md`; none blocked or partial.

---

## Spec-Anchored Acceptance Criteria

### P1: Real cause data in metric-type SLO enrichment

| Criterion (WHEN X THEN Y) | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| RCA-01 (AC1): metric SLO + toggle on + dispatch THEN query Error Tracking scoped to `service`/`env:production`, 10-min window, before `Generate*` | Query uses `svc.DatadogServiceTag`, fixed env `"production"`, 10-minute window, only for `slo_type == "metric"` | `internal/poller/analyzer_test.go:955-980` (`TestSLOAnalyzer_ResolveCauseHint_Success_ReturnsCauseTypeAndMessage`) - asserts `provider.lastService == "checkout-api"`, `provider.lastEnv == "production"`, `provider.lastTo.Sub(provider.lastFrom) == errorTrackingWindow` (10m, `analyzer.go:85`); `analyzer.go:449-457` calls `resolveCauseHint` before `buildAnalysisInput`'s result reaches `GenerateDegradedAnalysis` | ✅ PASS |
| RCA-02 (AC2): found issue THEN populate cause fields, top-1 by `total_count` | `AnalysisInput.CauseType`/`CauseMessage` = `hint.ErrorType`/`ErrorMessage`, ordered by `TOTAL_COUNT desc` at the connector | `internal/poller/analyzer_test.go:963-966` (`causeType != "MongooseError"` check); `internal/connectors/datadog/client_test.go:439` (`TestSearchErrorTrackingIssues_ValidResponse_ReturnsTopCauseHint`) asserts request body carries `"order_by":"TOTAL_COUNT"` and returned hint matches `data[0]`/`included[]` join | ✅ PASS |
| RCA-03 (AC3): query fails/times out/empty THEN cause field stays empty, no delay/failure | `resolveCauseHint` returns `("","")` on provider error and on `found=false`, goroutine proceeds unaffected | `internal/poller/analyzer_test.go:925-935` (`TestSLOAnalyzer_ResolveCauseHint_ProviderError_ReturnsEmpty`), `:939-949` (`TestSLOAnalyzer_ResolveCauseHint_NotFound_ReturnsEmpty`) | ✅ PASS |
| RCA-04 (AC4): toggle false THEN never issue the query, zero extra API calls | `provider.calls == 0` when disabled or unset | `internal/poller/analyzer_test.go:868-881` (`TestSLOAnalyzer_ResolveCauseHint_ToggleOff_ReturnsEmptyAndSkipsProvider`, asserts `provider.calls != 0` fails), `:856-864` (`..._Unset_ReturnsEmpty`) | ✅ PASS |
| RCA-05 (AC5): query runs inside existing bounded goroutine/`dctx`, no new timeout context | No second `context.WithTimeout` call added around the cause lookup | `internal/poller/analyzer.go:449-457` (degraded) and `:501-507` (outage) - `resolveCauseHint(dctx, svc)` reuses the same `dctx` from the single `context.WithTimeout` call already in each dispatch method (comment explicitly notes "no second timeout context is created"); `internal/poller/analyzer_test.go:1036-1061` (`..._StillRespectsCooldownAndConcurrency`) is a regression check that `maxConcurrentEnrichments`/`enrichmentCooldown` still gate the enriched path | ✅ PASS |
| RCA-06 (AC6): non-empty cause THEN extend prompt to allow reflecting practical impact, tone/jargon constraints unchanged | Cause sentence appended to **user** prompt (not system prompt - see Spec-Precision Gap below); `analysisSystemPrompt` untouched | `internal/llm/prompts_test.go:79-88` (`TestBuildDegradedTooltipPrompt_WithCause_IncludesCauseText`), `:112-121` (outage), `:90-107`/`:126-139` (identical-to-baseline when empty), `:62-74` (`TestPromptBuilders_SystemPromptsAreShortFactualNonAlarmist` - same system prompt text for all 3 builders, unchanged) | ⚠️ PASS with spec-precision gap (see below) |

### P2: Admin-facing toggle + monitor-type SLO path

| Criterion | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| RCA-07 (AC7): settings screen shows toggle reflecting `llm_settings.root_cause_enrichment_enabled` | Toggle renders current tenant value | `web/src/features/integrations/IntegrationsPage.test.tsx:221-235` (`"root-cause enrichment toggle renders reflecting the current setting when the LLM provider is active"`); backing read: `internal/api/llm_providers_handler_test.go:544-563` (`TestLLMList_IncludesRootCauseEnrichmentEnabled`) | ✅ PASS |
| RCA-08 (AC8): switching toggle persists via tenant-scoped update endpoint, effective next poll cycle | `PATCH /api/integrations/llm/settings` persists, no restart needed (no restart mechanism exists in this codebase's poll loop - toggle is read fresh via `RootCauseEnrichmentEnabled` each dispatch) | `web/src/features/integrations/IntegrationsPage.test.tsx:236-252` (switch → PATCH call → reflects new state); `internal/api/llm_providers_handler_test.go:584-604` (`TestLLMUpdateSettings_ValidRequest_200Toggles`); `internal/db/llm_provider_repository_test.go:183-200`/`:317` (set-then-read roundtrip, proving no caching/restart needed) | ✅ PASS |
| RCA-09 (AC9): default false for every tenant | Column `DEFAULT false`; repository read with no prior write returns `false` | `internal/db/migrations/0039_slo_root_cause_enrichment.up.sql:3` (`BOOLEAN NOT NULL DEFAULT false`); `internal/db/llm_provider_repository_test.go:166-182` (`TestLLMProviderRepository_RootCauseEnrichmentEnabled_EmptySettingsRow_ReturnsFalse`) | ✅ PASS |
| RCA-10 (AC10): monitor-type SLO + enrichment enabled THEN resolve `monitor_ids` from `SearchSLOs`-time metadata, use monitor's alert message | Deferred to P2 per spec's Assumptions table ("0 of 46 real SLOs... building it for P1 would ship code with no real data") | No implementation found (`grep` for `monitor_ids`/monitor-alert-message path in `internal/poller`, `internal/llm`, `internal/connectors/datadog` returns nothing beyond `SLOType` decoding). `tasks.md` never schedules a task for this - confirmed correctly out of scope for this task set, not silently half-done | ✅ PASS (correctly deferred, not implemented) |
| RCA-11 (AC11): monitor-type with no usable monitor message THEN same empty-cause fallback as AC3 | N/A - AC10's mechanism doesn't exist yet, so this fallback is moot for now | `internal/poller/analyzer_test.go:885-900` (`TestSLOAnalyzer_ResolveCauseHint_WrongSLOType_ReturnsEmptyAndSkipsProvider`) proves a `slo_type: "monitor"` service already falls back to empty cause via the *same* early-out that will need updating in P2 - i.e. the current code doesn't crash or misbehave for monitor-type SLOs today | ✅ PASS (fallback for the P1 codepath verified; true AC11 mechanism is P2 scope) |

**Status**: ✅ All ACs covered, one spec-precision gap flagged (RCA-06).

### Spec-Precision Gap: RCA-06's "system prompt SHALL be extended"

Spec.md's literal text: *"the system prompt SHALL be extended to allow (not require) the model to reflect..."* The implementation extends the **user** prompt (`causeSentence` appended in `buildDegradedTooltipPrompt`/`buildOutageDescriptionPrompt`'s `userPrompt`), while `analysisSystemPrompt` is verified byte-identical across all three builders and unchanged from before this feature (`prompts_test.go:62-74`).

This is documented and reasoned about at design time: design.md's Integration Points row states *"`prompts.go`'s three builders append one extra sentence to the **user prompt** when both are non-empty, system prompt's jargon/tone constraints untouched"* - i.e. Design already reinterpreted AC6's literal wording during the Design phase, and T9's own Reuses note ("RCA-06's constraint is 'extend the user prompt', not restructure the builders") carries the same reasoning through to Tasks. The code comment at `internal/llm/prompts.go:29-35` restates this interpretation explicitly.

**Judgment**: this reading is defensible, not a stretch. The system prompt in this codebase is a fixed, shared instruction-only string (tone/jargon rules) reused verbatim across all three builders; the *data* (cause type/message) is inherently per-call and belongs in the user prompt alongside every other per-call fact (`ServiceName`, `SLI`, `Target`, etc. - all of which already live in the user prompt, never the system prompt). Putting `CauseType`/`CauseMessage` into `analysisSystemPrompt` would have required templating a previously-static constant per call, which breaks the existing "system prompt is shared, user prompt is the call-specific payload" pattern this file already established before this feature. The literal AC6 wording is imprecise (conflating "the model's allowed behavior is extended" with "the system-prompt *string* is extended"), and the design-time interpretation resolves that imprecision in the direction consistent with the codebase's actual architecture. Flagged as a spec-precision gap, not a FAIL - functionally, RCA-06's actual intent (let the model use the cause data, keep tone/jargon constraints) is fully met and tested.

---

## Discrimination Sensor

Sensor run in an isolated `git worktree` at `/tmp/vane-sensor-scratch` (`git worktree add /tmp/vane-sensor-scratch HEAD`), never touching the real tree. Pre-sensor baseline (`git status --porcelain` on the real tree): `M .specs/STATE.md` + `?? .specs/features/saas-transactional-email/` (pre-existing, unrelated to this feature). Post-sensor-cleanup porcelain matched exactly.

| # | File:line | Mutation | Killed? |
| --- | --- | --- | --- |
| 1 | `internal/poller/analyzer.go:224` | `if svc.SLOType != "metric" \|\| svc.DatadogServiceTag == ""` → `&&` (flipped boolean combinator on the metric-type/service-tag early-out, RCA-01) | ✅ Killed - `TestSLOAnalyzer_ResolveCauseHint_WrongSLOType_ReturnsEmptyAndSkipsProvider` and `..._NoServiceTag_...` both failed |
| 2 | `internal/poller/analyzer.go:220` | `if !enabled { return "","" }` → `if enabled { return "","" }` (flipped toggle-off early-out, RCA-04) | ✅ Killed - `TestSLOAnalyzer_ResolveCauseHint_ToggleOff_ReturnsEmptyAndSkipsProvider` and `..._Success_ReturnsCauseTypeAndMessage` both failed |
| 3 | `internal/llm/prompts.go:37` | `if in.CauseType == "" \|\| in.CauseMessage == ""` → `&&` (causeSentence's empty-check combinator, RCA-06) | ✅ Killed - `TestBuildDegradedTooltipPrompt_WithoutCause_IdenticalToBaseline` failed (cause sentence leaked in with only `CauseType` set) |

**Sensor depth**: lightweight (3 targeted mutations, standard-tier feature - no payment/auth/data-integrity path here).
**Result**: 3/3 killed - PASS ✅. Real worktree porcelain confirmed unchanged after scratch worktree removal.

---

## Code Quality

| Principle | Status |
| --- | --- |
| Minimum code | ✅ - every change traces to a task/spec ID; no speculative abstractions |
| Surgical changes | ✅ - touched files match `tasks.md`'s per-task file scope, plus the two documented SPEC_DEVIATIONs |
| No scope creep | ✅ - both deviations are minimal read-path plumbing required to make their own task's stated AC achievable, not new features (see below) |
| Matches patterns | ✅ - `SetErrorCauseEnrichment` mirrors `SetNotifier` exactly; narrow interfaces match `incidentStore`/`llmGenerator` convention; repository methods match `GetActiveProvider`/`SetActiveProvider` shape |
| Spec-anchored outcome check (asserted values match spec) | ✅ - see AC table above; only gap is the documented RCA-06 precision note |
| Per-layer Coverage Expectation met (domain 1:1 ACs; routes happy+edge+error) | ✅ - `resolveCauseHint` has 1:1 test coverage for every early-out branch (unset, toggle-off, wrong-type, no-tag, provider-error, not-found, success); API handlers cover happy/malformed/service-error, role enforcement via the shared `AllWriteRoutes` table test |
| Every test maps to a spec requirement - no unclaimed tests | ✅ - spot-checked; every new test's name/comment references an RCA-NN or a task number |
| Documented guidelines followed | `AGENTS.md` §3 (gate commands), §4 (no raw `err.Error()` leaks - verified `writeInternalError` used in `UpdateSettings`), §5 (i18n via `react-i18next`, `Switch` UI component reused - verified `IntegrationsPage.tsx:139`) |

### SPEC_DEVIATION assessment (T13, T14)

Both deviations follow the same shape: a task's own "Done when" criterion was literally unimplementable without a small backend read-path `tasks.md`/`design.md` had omitted.

- **T13 (`b50acad`)**: `AddServiceDrawer`'s "capture `slo_type`/`datadog_service_tag` from the search result" requires the SLO-search endpoint to expose those fields; `internal/api/integrations_handler.go`'s `sloSummaryResponse` never had them, even though `datadog.SLOSummary` (T2) already decoded them. Fix: added `slo_type`/`datadog_service_tag` to `sloSummaryResponse` with matching field names (no renaming needed downstream), test coverage in `internal/api/integrations_handler_test.go` (verified: `TestSLOSummary...` shape tests exist per `git show b50acad --stat`, +37 lines in that test file). **Verdict: justified and correctly scoped** - genuinely a missing prerequisite, not new functionality; the field was already being decoded one layer down, this just plumbs it one more hop to the response DTO.
- **T14 (`dad22ea`)**: The toggle's AC1 ("reflecting `root_cause_enrichment_enabled` for the current tenant") requires a read path; only a setter (T5/T7) existed. Fix: added `RootCauseEnrichmentEnabled` read method mirrored at every layer the setter already has one (`LLMProviderStore` adapter → `llm.Service` → `llmProviderService` handler interface → `List`'s response field), confirmed in the diff reviewed directly (`internal/api/llm_providers_handler.go`, `internal/llm/service.go`). Test coverage added at each layer (`TestLLMList_IncludesRootCauseEnrichmentEnabled`, `TestLLMList_RootCauseEnrichmentEnabledError_500`, plus `internal/llm/service_test.go` and `internal/db/llm_provider_repository_test.go` roundtrip tests). **Verdict: justified and correctly scoped** - a pure read mirror of an already-approved write path on the same tenant-scoped row, no new endpoint, no new table.

Neither deviation expanded scope beyond what RCA-01/RCA-07/RCA-08's own acceptance criteria already required; both are the minimum necessary plumbing, both are tested at every layer touched, and both are called out explicitly in their commit messages per this skill's SPEC_DEVIATION convention.

---

## Edge Cases (spec.md)

- [x] Datadog App Key lacking Error Tracking scope logged distinctly from a timeout - `internal/poller/analyzer.go:230-241` (`errors.Is(err, datadog.ErrUnauthorized)` branch logs `zap.String("reason","unauthorized")` separately from the generic failure branch)
- [x] Long `error_message` capped before entering `AnalysisInput` - `internal/connectors/datadog/client_test.go:645` (`TestSearchErrorTrackingIssues_LongErrorMessage_TruncatedTo500Chars`)
- [x] `maxConcurrentEnrichments`/`enrichmentCooldown` still apply to the enriched path - `internal/poller/analyzer_test.go:1036-1061`

---

## Gate Check

- **Gate command**: `go build ./... && go test ./... && go vet ./... && gofmt -l <changed .go files>` (Quick backend); `TEST_DATABASE_URL=... go test -tags=integration -p 1 ./...` against a disposable `postgres:16-alpine` container on port 5433 (Full, per `AGENTS.md`'s hard rule - never `vane-dev-pg`); `cd web && npx tsc -b --noEmit && npm run test && npm run i18n:check` (Quick frontend)
- **Result**:
  - `go build ./...` - clean, no output
  - `go vet ./...` - clean, no output
  - `gofmt -l` on every changed `.go` file in the diff range - clean, no output
  - `go test ./...` (no integration tag) - all packages `ok`
  - `go test -tags=integration -p 1 ./...` against disposable container - all packages `ok`, including `internal/api` (44.6s, integration-tagged handler tests) and `internal/db` (35.0s). The previously-flagged flaky test (`TestTenantInviteRepository_InvalidatePendingForEmail_MarksPendingUsed_LeavesOthersAlone`) passed clean on this run - not re-triggered, no action needed.
  - Frontend: `npx tsc -b --noEmit` clean; `npm run test` - 98 test files, 706 tests, all passed; `npm run i18n:check` - "i18n key parity OK (pt-BR, en): 854 keys each"
- **Skipped tests**: none
- **Failures**: none

---

## Fix Plans

None - no gaps, no surviving mutants.

---

## Requirement Traceability Update

| Requirement | Previous Status | New Status |
| --- | --- | --- |
| RCA-01 | Implementing | ✅ Verified |
| RCA-02 | Implementing | ✅ Verified |
| RCA-03 | Implementing | ✅ Verified |
| RCA-04 | Design (Pending) | ✅ Verified |
| RCA-05 | Design (Pending) | ✅ Verified |
| RCA-06 | Design (Pending) | ✅ Verified (spec-precision gap noted, judged defensible) |
| RCA-07 | Implementing | ✅ Verified |
| RCA-08 | Design (Pending) | ✅ Verified |
| RCA-09 | Implementing | ✅ Verified |
| RCA-10 | Design (Pending) | ✅ Verified (correctly deferred to P2, not implemented) |
| RCA-11 | Design (Pending) | ✅ Verified (P1 fallback path verified; true mechanism is P2 scope) |

---

## Summary

**Overall**: ✅ Ready

**Spec-anchored check**: 11/11 ACs covered with file:line evidence; 1 spec-precision gap (RCA-06, judged defensible - see above)
**Sensor**: 3/3 mutations killed
**Gate**: All backend (unit + integration) and frontend (tsc, vitest, i18n) checks pass, 0 failures

**What works**: The full P1 enrichment pipeline (toggle-gated Error Tracking lookup → `CauseHint` → `AnalysisInput` → prompt extension) is wired, tested at every early-out branch, and proven to short-circuit to byte-identical pre-feature behavior when disabled. The P2 admin toggle round-trips end-to-end (UI → API → repository → back to UI) with default-false confirmed at the schema and repository layers. The monitor-type SLO path (RCA-10/11) is correctly left unimplemented, matching spec.md's explicit P2 deferral - not silently half-built.

**Issues found**: None requiring a fix task. One spec-precision gap (RCA-06's literal "system prompt" wording vs. the user-prompt implementation) is flagged for the record but judged correctly reasoned and consistent with both design.md's stated interpretation and this codebase's existing prompt-construction pattern.

**Next steps**: None required to close this feature. Optional forward note (already captured in design.md's own Risks table, not a new finding): flow-type/multi-service SLOs get no enrichment coverage under this design - accepted scope, revisit only if that becomes a real product ask.
