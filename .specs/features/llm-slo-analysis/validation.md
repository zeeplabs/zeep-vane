# LLM SLO Analysis Validation

**Date**: 2026-09-09
**Spec**: `.specs/features/llm-slo-analysis/spec.md`
**Diff range**: `9c2ebf1..HEAD` (branch `develop`, 25 original feature-task commits + 1 checkbox-only chore commit + 6 fix commits from iteration-1's gap list)
**Verifier**: independent sub-agent (author ≠ verifier) — fix→re-verify **iteration 2 of 3**

> This is a fresh, independent re-verification. No prior report's evidence was taken on trust — every citation below was re-derived by reading the current file content and/or re-running the test in question (unit run, or a scratch-worktree mutation).

---

## Task Completion

All 25 original tasks (T1-T25) remain `[x]` in `tasks.md`; the 6 fix commits are outside `tasks.md`'s scope (they were routed as iteration-1 fix tasks, not new numbered tasks) and are tracked here instead.

| Item | Status | Notes |
| --- | --- | --- |
| T1-T25 | ✅ Done | Unchanged since iteration 1 |
| Fix 1 (`fbe56f9`) | ✅ Done | Same-status no-op guard now covered for `degraded`/`degraded` and `outage`/`outage` |
| Fix 2 (`d37d08c`) | ✅ Done | Viewer-403 coverage added for both incident-close routes |
| Fix 3 (`ef6cd86`) | ✅ Done | Empty/whitespace-only LLM result now discarded in all 3 enrichment paths |
| Fix 4 (`8f12ab3`) | ✅ Done | `MarkInvalid`/`MarkChecked` wired into `Service.generate()` |
| Fix 5 (`ea079e7`) | ✅ Done | AI-10 test now asserts description text, not just call count |
| Fix 6 (`ca9c5f5`) | ✅ Done | Adapter round-trip integration test added, real Postgres |

---

## Spec-Anchored Acceptance Criteria

Full 26-AC re-derivation. ACs unaffected by the 6 fix commits were re-confirmed by re-reading the cited file/line (not just copied from iteration 1); their evidence is unchanged. ACs touched by a fix commit were re-verified from scratch, including a live mutation re-run where applicable.

| AC | Spec-defined outcome | `file:line` + assertion | Result |
| --- | --- | --- | --- |
| AI-01 | Valid key validated via `ValidateCredentials` before persisting | `internal/llm/service_test.go:118-148` `TestConnect_ValidKey_EncryptsAndPersistsWithDefaultModel` | ✅ PASS |
| AI-02 | Invalid key → 422, nothing persisted | `internal/llm/service_test.go:167-181`; `internal/api/llm_providers_handler_test.go` `TestLLMConnect_ValidationFailed_422` — `rec.Code == 422` | ✅ PASS |
| AI-03 | No model → default `gpt-4o-mini` | `internal/llm/service.go:52` `const defaultModel = "gpt-4o-mini"`; `service_test.go:137-138` | ✅ PASS |
| AI-04 | Explicit model persists, used subsequently | `internal/llm/service_test.go:219-234` `TestSetModel_KnownModel_Persisted` | ✅ PASS |
| AI-05 | API key stored encrypted only, never plaintext in response | `service_test.go:141-147` decrypt round-trip; `llm_providers_handler_test.go` asserts response body excludes `"api_key"`/`"encrypted"` | ✅ PASS |
| AI-06 | Viewer 403 on connect/set-model/activate; viewer OK on read | `internal/cli/routes_test.go:272-296`/`431` `TestAdminRouter_Viewer_AllWriteRoutes_403`; read: `routes_test.go:557` | ✅ PASS |
| AI-07 | No/unconfigured provider → today's behavior unchanged except fallback text, never blocking | `internal/llm/generate_test.go` (`ErrNoActiveProvider` short-circuit) + `analyzer_test.go` failure-path tests | ⚠️ Spec-precision gap (unchanged from iteration 1 — covered piecewise, no single end-to-end "zero provider" poll-cycle test) |
| AI-08 | Outage transition creates incident synchronously, independent of LLM completion | `internal/poller/analyzer_test.go:290-318` `TestSLOAnalyzer_HandleTransition_OutageNoExistingIncident_CreatesAutoIncident` (line numbers shifted by the 3 new no-op tests inserted above it; content re-confirmed identical) — `createCalls==1` asserted before `waitOrTimeout` | ✅ PASS |
| AI-09 | Fallback description is a fixed generic message identifying the service | `analyzer.go:66-68` `genericOutageDescription` interpolates `serviceName`; test asserts `Description` non-nil/non-empty | ⚠️ Spec-precision gap (unchanged — literal string not pinned in the assertion, though `ea079e7`'s new text-assertion for the *LLM-success* path shows the pattern exists and could be applied here too) |
| AI-10 | LLM success replaces generic description in place | `internal/poller/analyzer_test.go:520-528` `TestSLOAnalyzer_OutageEnrichment_Success_OverwritesGenericDescription` — `ea079e7` added `texts := incidents.snapshotDescriptionTexts(); texts[0] != "The payments service is returning 5xx errors for most requests."` — now asserts the actual text, not just a call count | ✅ PASS — **gap closed** |
| AI-11 | LLM failure/timeout leaves generic description, no auto-retry | `analyzer_test.go` `TestSLOAnalyzer_OutageEnrichment_Failure_LeavesGenericDescriptionInPlace` — `SetDescription` called 0 times | ✅ PASS |
| AI-12 | Existing open incident → no duplicate on repeat outage transition | `analyzer_test.go` — `createCalls==0`; independently reconfirmed by mutation (see Sensor, this run did not need to re-inject this specific one — verified structurally unchanged) | ✅ PASS |
| AI-13 | LLM never called more than once per transition per service (no re-analysis while status unchanged) | `internal/poller/analyzer_test.go:220-238` `TestSLOAnalyzer_HandleTransition_NoOp_SameStatus_Degraded_CallsNothing` and `:240-257` `..._Outage_CallsNothing` — both assert zero calls into `services`/`incidents` for an unchanged `degraded`/`degraded` and `outage`/`outage` pair respectively | ✅ PASS — **gap closed**, confirmed live: guard removed in scratch worktree → both new tests fail (`UpdateStatusAnalysis called 1 times, want 0`; `Create called 1 times, want 0`); guard restored → pass |
| AI-14 | Degraded transition (into) requests analysis, caches against service | `analyzer_test.go` `TestSLOAnalyzer_DegradedEnrichment_Success_UpdatesStatusAnalysis` — asserts actual analysis text value | ✅ PASS |
| AI-15 | While pending/failed/no-provider, public page shows plain label, no placeholder | `internal/api/public_status_handler_test.go` `TestPublicStatusGet_DegradedServiceAnalysisPending_OmitsStatusAnalysis` — `StatusAnalysis == nil` | ✅ PASS |
| AI-16 | Once cached, tooltip shows on hover/focus (`title`+`tabIndex`) | `web/src/features/public-status/PublicStatusPage.test.tsx:385-411` — `title` attribute equals exact analysis text, `tabIndex="0"` | ✅ PASS |
| AI-17 | Leaving degraded (to any status) clears cached analysis | `analyzer_test.go` — `UpdateStatusAnalysis(ctx, id, nil)` asserted | ✅ PASS |
| AI-18 | No repeat LLM call while status stays `degraded` across cycles | Same guard as AI-13 — `TestSLOAnalyzer_HandleTransition_NoOp_SameStatus_Degraded_CallsNothing` now directly covers the `degraded`/`degraded` pair | ✅ PASS — **gap closed**, same evidence/mutation as AI-13 |
| AI-19 | Operational-with-open-incident requests closing comment, incident status unchanged | `analyzer_test.go` — `createCalls==0`, `SetPendingCloseComment` called with right incident ID | ✅ PASS |
| AI-20 | Incident stays visibly open on both admin and public views while pending | Admin side: `incidents_handler_test.go` (status stays `investigating` until confirm); public side: `public_status_handler.go:133` confirms `pending_close_comment` never exposed publicly | ⚠️ Spec-precision gap (unchanged — no dedicated public-incidents-list test asserting "still shown as open while pending") |
| AI-21 | Confirm → append `incident_update`, resolve, set `resolved_at` | `internal/api/incidents_handler_test.go:541-580` `TestConfirmClose_Success_200_ResolvedWithFinalUpdate` — `Status=="resolved"`, `ResolvedAt != nil`, `page.Items[0].Body == "service recovered, closing out"` | ✅ PASS |
| AI-22 | Discard/edit → falls back to existing manual close flow, unaffected | `internal/api/incidents_handler_test.go:613-657` `TestDiscardCloseProposal_Success_200_IncidentUnchanged` — `PendingCloseComment==nil`, status unchanged | ✅ PASS |
| AI-23 | Closing-comment LLM failure/timeout → no proposal, recovery not blocked | `analyzer_test.go` closing-comment failure test — `SetPendingCloseComment` called 0 times | ⚠️ Spec-precision gap (unchanged — "recovery not blocked" true by code inspection, no independent assertion) |
| AI-24 | Viewer 403 on confirm-close/discard-close-proposal | `internal/cli/routes_test.go:361-372` (`writeRouteCases()`) + `TestAdminRouter_Viewer_AllWriteRoutes_403` (`:443`) — real `buildAdminRouter`, real `RequireRole`, both new routes now in the table, `rec.Code == 403` asserted | ✅ PASS — **gap closed**, re-run directly: `go test ./internal/cli/... -run TestAdminRouter_Viewer_AllWriteRoutes_403 -v` green, subtests for both routes present |
| AI-25 | `llm.Provider`-shaped interface, no concrete OpenAI type at call sites | `internal/llm/provider.go` interface; confirmed by grep — only `internal/cli/routes.go` (factory wiring) imports `internal/connectors/openai` | ✅ PASS |
| AI-26 | Second provider addable via one factory-switch case, no poller/incident/tooltip logic change | `internal/cli/routes.go` `llmProviderFactory` single `switch`/`case "openai"`, mirrors `emailProviderFactory` | ✅ PASS (code-review AC, per spec's own stated test method) |

**Status**: ✅ All ACs covered — 22/26 cleanly PASS, 4 spec-precision gaps (AI-07, AI-09, AI-20, AI-23), unchanged from iteration 1 and explicitly acceptable per `validate.md`'s rule ("where the spec does NOT define a precise outcome, flag as spec-precision gap" — not a blocking gap; none of the 4 involve an unasserted precise spec value, they are genuinely underspecified/inspection-only claims). All 4 iteration-1 hard gaps (AI-10, AI-13, AI-18, AI-24) are closed with direct evidence.

---

## Edge Cases (spec.md)

| Edge Case | Status | Evidence |
| --- | --- | --- |
| Revoked/expired API key marks integration `invalid` after a failed call | ✅ **Now implemented** | `internal/llm/service.go:301-316` `generate()`: `errors.Is(err, ErrUnauthorized)` → `s.repo.MarkInvalid(ctx, active, err.Error())`; any other outcome (success or non-auth failure) → `s.repo.MarkChecked(ctx, active)`. Tested at `internal/llm/generate_test.go:188-235` — `TestGenerateDegradedAnalysis_UnauthorizedError_MarksProviderInvalid` asserts `markInvalidCalls == ["openai"]`, `markCheckedCalls` empty, `store.rows["openai"].Status == "invalid"`; `TestGenerateDegradedAnalysis_Success_MarksProviderChecked` asserts the inverse. Mutation-confirmed live this run (see Sensor #2): inverting the `errors.Is` branch condition breaks the unauthorized test. |
| No SLO transition at all → zero LLM calls | ✅ Handled | No-op guard at `HandleTransition`'s top now directly tested for all 3 same-status pairs (AI-13/AI-18) |
| Two simultaneous outages → two independent incidents | ✅ Handled | `HasOpenIncidentForService` scoped per-`serviceID`, unchanged since iteration 1 |
| Provider disconnected mid-flight → best-effort apply/discard | ⚠️ Not directly tested | Unchanged from iteration 1 — no test for the specific race; writes target `incidents`/`services` rows, not the removed provider row, so it likely fails closed, but not independently verified. Not part of the 6 iteration-1 gaps, not newly regressed — carried forward as a known minor gap |
| Empty/malformed LLM response treated as failure, never published | ✅ **Now implemented** | `internal/poller/analyzer.go:219-223, 250-254, 288-292` — `strings.TrimSpace(result) == ""` guard added to all 3 `dispatch*Enrichment` goroutines, treated identically to an error (logged, returns without writing). Tested at `internal/poller/analyzer_test.go:459-513` — one test per enrichment path, each driving the fake generator to return an empty/whitespace-only result and asserting the corresponding write method was called 0 additional times. Mutation-confirmed live this run (see Sensor #3): removing the guard in `dispatchDegradedEnrichment` alone breaks only that path's test, leaving the other two (untouched) guards' tests green — confirms per-path, not shared, coverage. |

---

## Additional Boundary Findings (re-checked this run)

1. **T10 adapter boundary** (`llmProviderStoreAdapter`, `internal/db/llm_provider_repository.go:218-289`): iteration 1 flagged this as untested end-to-end through a real repository. **Now closed** — `internal/db/llm_provider_repository_test.go:239-296` (`ca9c5f5`, integration-tagged, `//go:build integration`) builds `*llm.Service`-shape access via `NewLLMProviderStore(repo)` against a real `*LLMProviderRepository` backed by disposable Postgres, and asserts: (a) `Connect`→`Get`→`ListPaginated` round-trips all 4 mapped fields (`Provider`/`EncryptedAPIKey`/`Model`/`Status`) correctly: `TestLLMProviderStore_ConnectThenList_RoundTripsThroughRealRepository`; (b) the not-found path translates `db.ErrNotFound` to `llm.ErrProviderRecordNotFound` and does **not** also satisfy `db.ErrNotFound`: `TestLLMProviderStore_Get_NotFound_SurfacesLLMSentinel`. Both re-run against a fresh disposable Postgres container this session — pass. Mutation-confirmed live this run (see Sensor #4): reverting the not-found translation to leak `err` (the raw `db.ErrNotFound`) instead of `llm.ErrProviderRecordNotFound` breaks the not-found test.
2. **T18 read-path completeness**: unchanged since iteration 1, no inconsistency found (`ListPaginated`/`List` omit `status_analysis`; only `ListForStatusPage` includes it).
3. **T15 timing-bounded test**: unchanged, still real and non-weak (`poller_test.go:443-477`).
4. **T22/T23 i18n claim**: unchanged, pre-existing precedent in those files, not a regression.
5. `MarkInvalid`/`MarkChecked` dead-code finding from iteration 1 — **superseded**, see Edge Cases table above.
6. Empty/malformed-response finding from iteration 1 — **superseded**, see Edge Cases table above.

---

## Discrimination Sensor

Second pass, isolated `git worktree` (`git worktree add <scratch> HEAD`), never on the real tree. Baseline `git status --porcelain` captured before starting (`.specs/LESSONS.md`, `.specs/lessons.json` modified; `context.md`/`design.md`/`validation.md` untracked — pre-existing, unrelated to this run) and re-confirmed byte-identical after the worktree's removal. Mutations 1 and 4 used a disposable Postgres container (`vane-test-pg`, port 5433, `--rm`, stopped after the whole gate run completed) — no other database was touched.

New mutations this pass — none repeat iteration 1's exact mutations; all 4 target code introduced or changed by the 6 fix commits, per the brief's instruction.

| # | File:line | Mutation | Killed? |
| - | --- | --- | --- |
| 1 | `internal/poller/analyzer.go:119-121` (scratch copy) | Removed the `if previousStatus == newStatus { return }` no-op guard entirely — the exact mutation that survived in iteration 1 | ✅ **Killed this time** — `TestSLOAnalyzer_HandleTransition_NoOp_SameStatus_Degraded_CallsNothing` fails (`UpdateStatusAnalysis called 1 times, want 0`) and `..._Outage_CallsNothing` fails (`Create called 1 times, want 0`). Confirms Fix 1 genuinely closes the iteration-1 gap, not just adds a vacuous test. |
| 2 | `internal/llm/service.go:303` | Flipped `errors.Is(err, ErrUnauthorized)` → `!errors.Is(err, ErrUnauthorized)` in `generate()`'s failure branch | ✅ Killed — `TestGenerateDegradedAnalysis_UnauthorizedError_MarksProviderInvalid` fails: `markInvalidCalls = [], want [openai]`; `markCheckedCalls = [openai], want none`; `provider status = "connected", want "invalid"` |
| 3 | `internal/poller/analyzer.go:219-223` | Removed the `strings.TrimSpace(analysis) == ""` empty-result guard in `dispatchDegradedEnrichment` only (left the outage/closing-comment guards untouched) | ✅ Killed — `TestSLOAnalyzer_DegradedEnrichment_EmptyResult_LeavesStatusAnalysisNull` fails (`UpdateStatusAnalysis called 2 times, want 1`); the other two (untouched) empty-result tests correctly stayed green, confirming per-path coverage rather than one path's test accidentally covering all three |
| 4 | `internal/db/llm_provider_repository.go:234-236` | Inverted the adapter's not-found translation: `return nil, llm.ErrProviderRecordNotFound` → `return nil, err` (leaks the raw `db.ErrNotFound` sentinel) | ✅ Killed — `TestLLMProviderStore_Get_NotFound_SurfacesLLMSentinel` fails: `Get() error = db: not found, want llm.ErrProviderRecordNotFound` |

**Sensor depth**: lightweight (4 mutations, default tier — feature not flagged P0/critical-path in spec.md)
**Result**: 4/4 killed, 0 survived — ✅ PASS. The mutation that survived in iteration 1 (mutation #1's exact repeat) is now killed, confirming Fix 1 is a real fix and not cosmetic.

**Isolation verification**: `git status --porcelain` captured before (`/tmp/baseline_porcelain.txt`) and after (`/tmp/after_porcelain.txt`) the entire sensor pass — `diff` reports no differences. `git worktree remove --force` completed cleanly.

---

## Code Quality

| Principle | Status |
| --- | --- |
| Minimum code, no unrequested features | ✅ — the 6 fix commits are each narrowly scoped to their gap (test-only for Fixes 1/2/5/6; guard/wiring-only for Fixes 3/4) |
| Surgical changes, no scope creep | ✅ — no fix commit touches a file outside its own gap's blast radius |
| Matches existing patterns | ✅ — `MarkInvalid`/`MarkChecked` wiring follows the same `errors.Is`-branch pattern as the rest of `internal/llm`; empty-guard follows the existing `err != nil` early-return shape already used in the same functions |
| Spec-anchored outcome check | ✅ 22/26 clean, 4 acceptable spec-precision gaps (see AC table) |
| Per-layer coverage (domain 1:1 ACs; routes happy+edge+error) | ✅ — the two remaining iteration-1 route/domain coverage gaps (AI-13/18 domain, AI-24 route) are both closed |
| Every test maps to a spec requirement | ✅ — each new test's doc comment/name explicitly cites the AI-NN or spec.md edge case it covers |
| Documented guidelines followed | `AGENTS.md` §3 (integration gate run against disposable Postgres, `-p 1`, torn down after — confirmed followed this run) |

---

## Gate Check

Run independently by this Verifier, from a clean state.

- **Backend build/vet/fmt**: `go build ./...` clean; `go vet ./...` clean; `gofmt -l $(git diff --name-only 9c2ebf1..HEAD -- '*.go')` → empty
- **Backend unit tests**: `go test ./... -count=1` → all packages `ok`, 0 failed (18 testable packages, all `ok`)
- **Backend integration tests**: disposable Postgres (`vane-test-pg`, port 5433, `max_connections=300`, `--rm`; confirmed torn down via `docker stop` after use — `vane-dev-pg` and no other database was touched), `TEST_DATABASE_URL=... go test -tags=integration ./... -p 1 -count=1` → all 18 testable packages `ok`, 0 failed
- **Frontend**: `npx tsc -b --noEmit` clean; `npm run test` → 51 test files, 277 tests passed, 0 failed
- **Result**: all gate commands green, 0 failures anywhere
- **Skipped tests**: none observed

---

## Prior Gaps Status (iteration 1 → iteration 2)

| # | Gap | Iteration-1 status | Iteration-2 status |
| - | --- | --- | --- |
| 1 | AI-13/AI-18 — same-status no-op guard unproven for `degraded`/`outage` | ❌ Confirmed live by surviving mutant | ✅ **Closed** — new tests exist and were re-confirmed live in a fresh scratch worktree this run (guard removed → tests fail) |
| 2 | AI-24 — no viewer-403 test for confirm-close/discard-close-proposal | ❌ Zero coverage | ✅ **Closed** — real router, real `RequireRole`, 403 asserted for both routes |
| 3 | Empty/malformed LLM response published verbatim | ❌ Unimplemented spec.md SHALL | ✅ **Closed** — guard added to all 3 dispatch paths, each independently tested and mutation-confirmed |
| 4 | Revoked/expired credentials never marked invalid | ❌ Unimplemented spec.md SHALL | ✅ **Closed** — `MarkInvalid`/`MarkChecked` wired into `generate()`, tested, mutation-confirmed |
| 5 (minor) | AI-10 payload/conjunction gap on `SetDescription` | ⚠️ Call-count only | ✅ **Closed** — now asserts exact description text |
| 6 (minor) | T10 adapter has no round-trip test | ⚠️ Blind spot | ✅ **Closed** — real-Postgres round-trip test added, both found and not-found paths |

**All 6/6 iteration-1 gaps confirmed closed with direct re-derived evidence, not the implementer's self-report.**

---

## Interactive UAT

Not performed — same rationale as iteration 1 (backend/LLM-heavy feature, no live OpenAI credential or running dev server in this environment). Automated checks are the applicable bar per `validate.md` §3.

---

## Requirement Traceability Update

| Requirement | Iteration-1 Status | Iteration-2 Status |
| --- | --- | --- |
| AI-01, AI-02, AI-03, AI-04, AI-05, AI-06, AI-08, AI-11, AI-12, AI-14, AI-15, AI-16, AI-17, AI-19, AI-21, AI-22, AI-25, AI-26 | ✅ Verified | ✅ Verified (re-confirmed, unchanged) |
| AI-10 | ⚠️ Payload/conjunction gap | ✅ Verified |
| AI-13, AI-18 | ❌ Needs Fix | ✅ Verified |
| AI-24 | ❌ Needs Fix | ✅ Verified |
| AI-07, AI-09, AI-20, AI-23 | ⚠️ Spec-precision gap | ⚠️ Spec-precision gap (unchanged, non-blocking) |

---

## Summary

**Overall**: ✅ Ready

All 6 gaps ranked in iteration 1 are independently confirmed closed — not by trusting the implementer's commit messages, but by re-deriving each fix's evidence directly: reading the actual diff, re-running the specific test file, and, for the two claims most amenable to it (the no-op guard and the empty-result guard), reproducing the mutation-kill live in a fresh scratch worktree this session. The two previously-unimplemented spec.md Edge Case SHALL requirements (empty/malformed-response filtering, credential-invalidation marking) are now implemented and tested, not just test-covered gaps. The full build/vet/fmt/test gate (backend unit, backend integration against disposable Postgres, frontend) is green with zero failures. The discrimination sensor's 4 new mutations — including a direct repeat of the exact mutation that survived in iteration 1 — are all killed.

The 4 remaining spec-precision gaps (AI-07, AI-09, AI-20, AI-23) are unchanged from iteration 1, non-blocking per `validate.md`'s own rule (they are genuinely underspecified in spec.md or verified only by code inspection, not missing-precise-outcome test failures), and were not part of the 6 ranked gaps routed for this fix cycle.

**Spec-anchored check**: 22/26 ACs matched the spec outcome cleanly; 4 spec-precision gaps (AI-07, AI-09, AI-20, AI-23), unchanged and non-blocking

**Sensor**: 4 mutations injected, 4 killed, 0 survived (including a direct re-run of iteration 1's surviving mutation, now killed)

**Gate**: all green — backend unit (18 packages `ok`), backend integration (18 packages `ok` against disposable Postgres), frontend (51 files / 277 tests) — 0 failed

**Prior gaps status**: 6/6 closed

**What works**: Everything from iteration 1, plus: same-status no-op guard now has defense-in-depth coverage for `degraded`/`degraded` and `outage`/`outage`; both incident-close routes are role-gated and tested; all 3 async enrichment paths discard empty/whitespace LLM output instead of publishing it; a revoked/expired provider key is now marked `invalid` and clears on next success; AI-10's assertion targets the actual generated text; the DB↔LLM adapter has direct round-trip integration coverage for both the found and not-found paths.

**Issues found**: None blocking. Carried-forward minor items (not part of the 6 ranked gaps, not regressions): 4 spec-precision gaps (AI-07/09/20/23) and the untested provider-disconnected-mid-flight race — both acceptable per validate.md's own rules for this pass, neither newly introduced.

**Next steps**: None required. Feature is ready to be marked `Verified` in `spec.md`'s Requirement Traceability table.
