# Degraded-Interval Analysis — Validation

**Verdict: PASS**

**Diff range verified**: `aeea402^..627aec4` (commits `aeea402, dad8e8d, e1eac6d, 2fb343f, 1aa02a7, ede9e12, 1628cfb, 627aec4`; `b2f8072`/`40c06a2` are traceability-doc-only, skipped). Interleaved `provider-disconnect`/`audit-log-expansion` commits and files were excluded from scope per instructions.

## AC-by-AC evidence

| AC | Status | Evidence |
|---|---|---|
| DEGINT-01 | PASS | `internal/db/migrations/0038_status_interval_analysis.{up,down}.sql` — nullable `analysis TEXT`, matching down. |
| DEGINT-02 | PASS | `OpenOrExtend` returns `(string, error)` in all 3 branches, `internal/db/status_interval_repository.go:72-128`. |
| DEGINT-03 | PASS | `HandleTransition(... intervalID string)` threads ID to `dispatchDegradedEnrichment(ctx, svc, sloStatus, intervalID)` before the goroutine starts (`internal/poller/analyzer.go:207-228,335-345`). |
| DEGINT-04 | PASS | Goroutine calls `SetIntervalAnalysis(dctx, intervalID, analysis)` alongside `UpdateStatusAnalysis` (`analyzer.go:364-371`). |
| DEGINT-05 | PASS | Discriminating tests at both layers force the race: `internal/poller/analyzer_test.go:507-535` (`TestSLOAnalyzer_DegradedEnrichment_WritesToIntervalCapturedAtDispatch` — delays LLM 100ms, transitions interval-A→B mid-flight, asserts write lands on "interval-A") and `internal/db/status_interval_repository_test.go:589-611` (`TestSetIntervalAnalysis_ClosedInterval_StillPersists` — real UPDATE against a closed row). Mutation-killed (see below). |
| DEGINT-06 | PASS | Empty/whitespace/error results return before either write (`analyzer.go:352-363`); covered by `TestSLOAnalyzer_DegradedEnrichment_Failure_...` and `_EmptyResult_...`. |
| DEGINT-07 | PASS | `UpdateStatusAnalysis` calls/tests are untouched and still pass; `SetIntervalAnalysis` is a strictly additive second call, never replacing the first. |
| DEGINT-08 | PASS | `internal/history/hourly.go:130-139` appends an `Episode` per overlapping bucket when `interval.Status == "degraded"`, same overlap loop as status resolution. Test: `TestBuildBuckets_DegradedInterval_AttachesEpisode`. |
| DEGINT-09 | PASS | Per-bucket `sort.Slice` by `StartsAt` descending (`hourly.go:143-147`); `TestBuildBuckets_TwoDegradedEpisodesSameBucket_OrderedMostRecentFirst`. |
| DEGINT-10 | PASS | `TestBuildBuckets_OutageWinsColorButDegradedKeepsEpisode` (`hourly_test.go:500-523`): bucket status resolves to `"outage"` but still carries the degraded interval's episode. Mutation-killed. |
| DEGINT-11 | PASS | `episodeResponse{StartsAt,EndsAt,Analysis}` on `publicHistoryBucketResponse.Episodes` (`internal/api/public_status_handler.go:119-138,334-347`). |
| DEGINT-12 | PASS | No new route added (`internal/cli/routes.go` diff only threads the existing dependency change, confirmed by inspection). |
| DEGINT-13 | PASS | `PublicStatusPage.tsx:455-484` branches on `bucket.episodes.length > 0`, renders `Popover` listing `episodeTimeRange` + analysis-or-placeholder, most-recent-first (array pre-sorted by backend). Test: `PublicStatusPage.test.tsx` DEGINT-13 case. |
| DEGINT-14 | PASS | `Popover.tsx` (Radix), tests cover re-click, Escape, outside-click all closing (`Popover.test.tsx`). |
| DEGINT-15 | PASS | Zero-episode branch keeps the plain `<div title=... tabIndex={0}>` (`PublicStatusPage.tsx:486-495`); `PublicStatusPage.test.tsx` DEGINT-15 case. Mutation-killed. |
| DEGINT-16 | PASS | `Popover.test.tsx` "abre e fecha via teclado" — Tab focuses, Enter opens, Escape closes. |

## Mutation sensor (4/4 killed)

Run in isolated worktree (`git worktree add --detach /tmp/degint-verify-scratch develop`), disposable Postgres on port 5441 (`degint-verify-pg`), removed after use; real tree confirmed unaffected (`git status --porcelain` clean of my changes throughout).

1. **`analyzer.go`**: `SetIntervalAnalysis(dctx, intervalID, ...)` → `SetIntervalAnalysis(dctx, svc.ID, ...)`. `TestSLOAnalyzer_DegradedEnrichment_WritesToIntervalCapturedAtDispatch` failed as expected.
2. **`hourly.go`**: `if interval.Status == "degraded"` → `if true`. `TestBuildBuckets_NonDegradedInterval_NoEpisodes` failed as expected.
3. **`status_interval_repository.go`**: `SetIntervalAnalysis`'s UPDATE gained `AND ends_at IS NULL`. `TestSetIntervalAnalysis_ClosedInterval_StillPersists` (against the disposable container) failed as expected.
4. **`PublicStatusPage.tsx`**: episode-branch condition gated with `false &&`. The DEGINT-13 click test failed as expected (`npx vitest run .../PublicStatusPage.test.tsx`), done directly in the real tree with immediate revert + diff verification (no node_modules in the scratch worktree, per task fallback instructions).

## Full gate

- `go build ./...`, `go vet ./...`: clean.
- `gofmt -l` on every touched `.go` file: no output.
- `npx tsc -b --noEmit`: clean.
- `npm run test` (web): 97 files / 666 tests passed.
- Integration gate (`-tags=integration -p 1`, disposable containers, never `vane-dev-pg`): `internal/db`, `internal/poller`, `internal/api`, `internal/history` all pass **when run in isolation** (fresh, single-use containers). A combined single-process full-suite run reproduced `TestServiceRepository_ListPaginated_ReturnsSLONameForMixOfPreAndPostMigrationRows`, `TestUpdateAdminRole_ValidChange_...`, and `TestChangePassword_CorrectCurrentAndValidNew_...` failing alongside the two known pre-existing flakes (`TestEmailProviderRepository_SetActiveProvider_UpdatesSingletonRow`, `TestLLMProviderRepository_SetActiveProvider_ThenGetActiveProvider_RoundTrips`) — re-running each affected package alone against a fresh container passed clean, and none of the three extra failures touch any file this feature changed (admin role/session/service-pagination, all pre-existing/concurrent-session territory, matching the cross-package test-isolation gap explicitly logged in commit `410978f docs(provider-disconnect): mark T6 done, note pre-existing cross-package test-isolation gap`). Treated as pre-existing/environmental, not a regression from this feature.

## Gaps found

None blocking. Minor observations, not defects:
- `internal/history/hourly.go`'s `Episode` and API's `episodeResponse` are exact field-for-field duplicates of each other's shape — acceptable per design.md, no action needed.
- DEGINT-10's spec wording is convoluted but the design.md clarification and the dedicated test resolve the ambiguity unambiguously; no gap in the actual implementation.
