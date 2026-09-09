# Public Status Time-Range Selector — Validation

**Verdict: PASS** (with 4 non-blocking findings, ranked below — none of them a failed AC or a surviving mutant)

**Verifier**: independent agent (author ≠ verifier — every claim below was re-derived from the code and from freshly executed commands, not from the implementing agent's report).
**Date**: 2026-09-08
**Feature**: `public-status-time-range-selector`
**Diff range**: `4cbfd2d~1..HEAD` (`62ee6ce..12778f4`)

```
12778f4 feat(web): add time-range selector to the public status page
708b82a feat(web): thread selected time range through usePublicStatusPage
bbf6c5f test(web): generalize MSW public-status fixtures for the range parameter
cc3a8cd refactor(web): rename PublicHourlyBucket to PublicHistoryBucket, add RangeKey
dc316a5 feat(retention): raise status_intervals retention to 95 days for the 90d range tier
7612bdd feat(api): parameterize public status history by selected time range
3406561 feat(api): add time-range query param parsing for the public status endpoint
4cbfd2d feat(history): generalize BuildHourly into BuildBuckets with variable bucket width
```

20 files, +2033/−246 (3 of them the spec/design/tasks artifacts themselves).

---

## Per-AC evidence

Every row was checked by reading the assertion, not by proximity: the cited test must assert the spec's stated value (bucket count, width, status, HTTP code), not merely touch the area.

| AC | Requirement (spec) | Evidence (file:line) | Asserted value matches spec? |
| --- | --- | --- | --- |
| **TRS-01** | Defaults to 24h/1h on initial load, unchanged | `internal/api/public_status_handler_test.go:931` (no `range` param → `len(History) == 24`), `:963` (explicit `range=24h` → 24); `internal/api/time_range_test.go:53` + `:65` (missing and empty → `rangeSpecs["24h"]`, `ok=true`); `web/src/features/public-status/PublicStatusPage.test.tsx:299` (24 bars, `24h` tab `aria-selected=true`, label "24h atrás"); `web/src/features/public-status/hooks.test.ts:136` (default arg fetches `range=24h`) | ✅ Exact (24 buckets, 1h) |
| **TRS-02** | 7d→6h/28 bars, 30d→1d/30, 90d→1d/90, no page reload | Backend: `public_status_handler_test.go:994` (28 buckets **and** `History[1].Start - History[0].Start == 6h`), `:1028` (30 / 24h), `:1068` (90 / 24h); spec table itself: `time_range_test.go:19` (all four tiers, field by field) + `:11` (`bucketCount*bucketWidth == window` invariant). Pure logic: `internal/history/hourly_test.go:207` (6h aligns 00/06/12/18), `:234` (24h aligns to local midnight, partial today), `:269` (wide-bucket spanning). Frontend: `PublicStatusPage.test.tsx:317` (`it.each` 7d/30d/90d → 28/30/90 bars + label, no remount) | ✅ Exact — width is asserted, not just count |
| **TRS-03** | One page-wide selector applies to every service's chart | `PublicStatusPage.test.tsx:333` (2 services in fixture; after clicking 90d **both** go 24→90 bars); single `useState<RangeKey>` in `PublicStatusPage.tsx:223` feeding one `usePublicStatusPage` call (no per-service state exists) | ✅ Exact (≥2 services asserted, as tasks.md required) |
| **TRS-04** | Uptime % recomputed for the selected range, not pinned to 24h | Backend: `public_status_handler_test.go:1107` — same seed data, 24h vs 90d, asserts `*uptime24h != *uptime90d`; `composeResponse` passes the range-derived `windowStart` to `history.UptimePercent` (`public_status_handler.go:221`, `:271`). Frontend: `PublicStatusPage.test.tsx:353` — MSW returns 99.9 for 24h / 95.1 for 90d, asserts rendered text goes `99.90% uptime` → `95.10% uptime` | ✅ Exact. **Note**: the assertion is "differs", not a computed target percentage. Deliberate and defensible (the exact figure depends on wall-clock `now`), and strong enough — mutant M2 (ignore `rng`) is killed by it |
| **TRS-05** | Same range behavior on the authenticated `public-preview` endpoint (AD-008 parity) | `internal/api/public_status_preview_handler_test.go:321` — issues `range=90d` against **both** endpoints with identical seed data and asserts equal `len(History)` **and** equal bucket width, plus `== 90`; `:380` asserts preview also 422s on an invalid range. Structurally, both handlers call the same `parseRange` → `composeResponse` (`public_status_handler.go:161`, `public_status_preview_handler.go`) | ✅ Exact — and it is a real cross-endpoint comparison, not two independent 90-checks |
| **TRS-06** | Buckets with no covering data render `no_data`, not an error | Handler level, wide range: `public_status_handler_test.go:1213` (2 days of real data in a 90d window → `History[0].Status == "no_data"`, trailing bucket ≠ `no_data`, HTTP 200). Pure logic: `hourly_test.go:311` (90×24h, all `no_data`), `:104` (1h width) | ✅ Exact |
| **TRS-07** | Out-of-tier range value rejected, no silent fallback / unbounded query | `time_range_test.go:91` (`5d`, `90D`, `1h`, `24H`, `" 24h"`, `"24h "` → `ok=false`), `:101` (422 + exact body + `Content-Type`); `public_status_handler_test.go:1166` (422, exact error string, **and** a counting `serviceLister` spy proving 0 DB calls — design.md's "no partial work"); `public_status_preview_handler_test.go:380` (preview parity) | ⚠️ **Spec-precision gap**: spec says "(400)", implementation returns **422**. Deliberate, argued in design.md's Tech Decisions (every other validation error in `internal/api` is 422, zero uses of 400), and the spec text was never amended. Behaviorally correct per design; spec.md prose still says 400 |
| **TRS-08** | Closed `status_intervals` retained ≥95 days | `internal/cli/retention_constant_test.go:13` (`pruneRetention == 95*24h`, plain unit, no build tag); `internal/retention/pruner_test.go:127` (real Postgres: closed row with `ends_at = now-90d` survives a full `prune()` cycle at 95d retention → `count == 1`); constant itself `internal/cli/serve.go:34` | ✅ Exact (95 days, and the spec's own Independent Test scenario reproduced verbatim) |
| **TRS-09** | Open intervals (`ends_at IS NULL`) never pruned, any age | `internal/retention/pruner_test.go:62` — 100-day-old **open** interval plus a 40-day-old closed one; asserts the closed one is deleted and `count(*) WHERE ends_at IS NULL == 1`. Pre-existing coverage, unmodified by this feature (correct: no code path changed) | ✅ Exact |
| Edge case | Rapid range switching must never show a stale range's data | `hooks.test.ts:156` — rerender 24h→90d asserts `requestedRanges == ["24h","90d"]` (distinct `queryKey` per range, so no cross-range cache overwrite); `hooks.ts:133` `queryKey: ["public-status-page", id ?? "production", range]` | ✅ Covered (killed mutant M3) |
| Edge case | `asOf` stall-clamp applies to **every** range tier, not just 24h | `hourly_test.go:160` covers the clamp at `bucketWidth=1h` only. No test exercises the clamp at 6h or 24h width | ⚠️ **Gap** — see Finding #3. Code is width-agnostic (the clamp lives in the shared loop), so this is a missing-test gap, not a known defect |

**Result: 9/9 acceptance criteria have real, value-exact evidence.** One spec-precision gap (TRS-07's 400 vs 422, design-documented) and one uncovered spec edge case (wide-width `asOf` clamp).

---

## Reported deviation from the plan — verified

The implementing agent reported that T8 added `uptime_percent` plumbing beyond its literal file list. **Verified as real, necessary, and correctly implemented:**

- `git show 4cbfd2d~1:web/src/lib/publicStatus.ts` and `…:web/src/features/public-status/hooks.ts` contain **no** `uptime_percent` field at all, and `…:PublicStatusPage.tsx` has zero occurrences of `uptime`. The backend has returned the field since before this feature; the frontend simply dropped it on the floor.
- Therefore TRS-04's "recompute **and display**" half was untestable and unimplementable without adding it — the deviation was required by the AC, not scope creep.
- Implementation is correct and consistent: `PublicServiceEntry.uptime_percent: number | null` (`publicStatus.ts:35`), threaded through `PreviewService`/`fetchPublicStatusPage` (`hooks.ts:36`, `:105`), rendered via `formatUptimePercent` (`PublicStatusPage.tsx`) which returns `"—"` for `null` — honoring the backend's documented "null means render a dash, never a fabricated 0 or 100" contract rather than printing `0.00%`. MSW fixture supplies the field for both the global handler and the per-test overrides.

Verdict on the deviation: **reasonable and correct.** It should still be reflected in the artifacts (see Finding #2).

---

## Discrimination sensor

Isolation method: two throwaway `git worktree`s (`--detach` at `HEAD`, and one at `4cbfd2d~1` for the baseline-flake comparison) under the session scratchpad. **No `git stash`, no edit to the real working tree.** For the frontend mutant, the worktree got a symlink to the real `web/node_modules` (gitignored, non-mutating). All worktrees removed afterward; real-tree `git status --porcelain` was empty before the sensor and is empty after (verified, alongside `git worktree list` showing only the main tree).

| # | Mutation (behavior-level) | Layer | Result | Killing tests |
| --- | --- | --- | --- | --- |
| **M1** | `rangeSpecs["90d"].bucketCount` 90 → 30 | Go, `internal/api/time_range.go` | ☠️ **Killed** (5 tests) | `TestRangeSpecs_MatchDesignDataModelsTable`, `TestRangeSpecs_BucketCountTimesBucketWidthEqualsWindow`, `TestPublicStatusGet_Range90d_Returns90Buckets…`, `TestPublicStatusGet_Range90d_PartialCoverageLeadingBucketsNoData`, `TestPublicStatusPreview_Range90d_SameBucketShapeAsProduction` |
| **M2** | `composeResponse` ignores `rng` entirely: `windowStart = now-24h`, `BuildBuckets(..., 24, time.Hour)` (i.e. revert to the old hardcoded window) | Go, `internal/api/public_status_handler.go` | ☠️ **Killed** (6 tests) | `…Range7d_Returns28BucketsSixHoursWide`, `…Range30d…`, `…Range90d…`, `…UptimePercent_DiffersBetween24hAnd90dRanges`, `…Range90d_PartialCoverageLeadingBucketsNoData`, `…Preview_Range90d_SameBucketShapeAsProduction` |
| **M3** | `usePublicStatusPage` `queryKey` drops the `range` segment | TS, `web/src/features/public-status/hooks.ts` | ☠️ **Killed** (6 tests) | `hooks.test.ts` "mudar range entre renders produz um queryKey/fetch distinto"; `PublicStatusPage.test.tsx` all 3 `it.each` tier cases + page-wide test + uptime-per-range test |
| **M4** | `pruneRetention` 95d → 35d (revert T4) | Go, `internal/cli/serve.go` | ☠️ **Killed** (1 test, exact-value) | `TestPruneRetention_Is95Days` (`pruneRetention = 840h0m0s, want 2280h0m0s`) |
| **M5** | `BuildBuckets` bucket alignment: drop the floor-to-`bucketWidth` step (`currentStart = dayStart + elapsed`, i.e. slide from `now` instead of aligning to clock boundaries) | Go, `internal/history/hourly.go` | ☠️ **Killed** (6 tests/subtests) | `TestBuildBuckets_SixHourWidth_AlignsToSixHourBoundaries`, `…TwentyFourHourWidth_AlignsToLocalMidnight…`, `…IntervalSpanningMultipleWideBuckets…`, plus 3 subtests of the 1h byte-identity regression test |

**5/5 mutants killed. No surviving mutant.** The suite discriminates on the behaviors that matter (tier table values, range→window/width propagation, cache-key isolation, retention constant, boundary alignment) rather than merely on shapes.

---

## Gate results (all executed by the verifier, this session)

| Gate | Command | Result |
| --- | --- | --- |
| Go build | `go build ./...` | ✅ `BUILD_OK` |
| Go vet | `go vet ./...` | ✅ `VET_OK` |
| Go unit | `go test ./...` | ✅ all packages `ok` |
| gofmt | `git diff --name-only 4cbfd2d~1..HEAD -- '*.go' \| xargs gofmt -l` (11 files) | ✅ **empty** |
| Go integration | disposable Postgres (`vane-test-pg`, port **5433**, `max_connections=300`) + `TEST_DATABASE_URL=postgres://vane:vane@localhost:5433/vane?sslmode=disable go test -count=1 -tags=integration ./...` | ⚠️ 1 flake on the parallel run (see Finding #4) → ✅ **fully green** on `-p 1`; container stopped afterward. `vane-dev-pg` was never touched |
| Frontend types | `cd web && npx tsc -b --noEmit` | ✅ `TSC_OK` |
| Frontend tests | `cd web && npm run test` | ✅ **49 files / 251 tests passed** |

`-count=1` was used deliberately: the first integration invocation returned entirely cached results from a prior session, which is not evidence of a run.

---

## Findings (ranked — reported, not fixed; a separate cycle owns these)

### 1 — MEDIUM (pre-existing bug, newly amplified by wide buckets): out-of-window interval leaks into the leftmost bucket

`BuildBuckets` computes `endOffset := endLocal.Sub(leftmostStart)` and `lastIndex := int(endOffset / bucketWidth)` (`internal/history/hourly.go:91-93`). For an interval that ended **before** `leftmostStart` but less than one `bucketWidth` before it, `endOffset` is negative and Go's integer division truncates toward zero → `lastIndex == 0`, `endOffset % bucketWidth != 0` (no decrement), `firstIndex` clamps to 0 → the loop paints bucket 0 with a status from **outside** the rendered window.

Reproduced empirically in a throwaway worktree (probe test, discarded):

- `bucketWidth=24h`, `now=2026-08-24 09:15 -03`, outage `08-21 10:00 → 08-21 20:00` (ends 4h before the window) → `bucket[0] (08-22 00:00) = "outage"`, expected `no_data`.
- Same shape at `bucketWidth=1h` also leaks → **the defect predates this feature** (the arithmetic is byte-identical in the old `BuildHourly`, which is exactly why the 1h byte-identity regression test can't catch it).

Why it matters more now: the "leak band" is one `bucketWidth` wide, so it grows from 1 hour to **24 hours** on the 30d/90d tiers, and `composeResponse` feeds `ListOverlapping(now-window, now)` whose lower bound sits up to a full `bucketWidth` below `leftmostStart` — i.e. the band is inside the fetched data set by construction. Existing coverage misses it because `hourly_test.go:188` ("IntervalsOutsideWindowAreIgnored") places its interval 4 **days** outside the window, far past the one-bucket band.

Suggested fix direction (for the fix cycle, not applied): skip intervals with `endOffset <= 0` before index math, or floor-divide instead of truncating. Add a `bucketWidth=24h` regression test for the "ended just before the window" case.

### 2 — MEDIUM (documentation gate, AGENTS.md §6): no `STATE.md` AD entry and no `CHANGELOG.md` entry

`git log 4cbfd2d~1..HEAD -- .specs/STATE.md CHANGELOG.md README.md` is **empty**. Three commitments are currently unmet:

- **AGENTS.md §6** requires an `AD-NNN` entry in `.specs/STATE.md` for a new architectural decision. This feature has several (range-tier table; reject-vs-clamp for `range`; retention 35d→95d, which *supersedes the figure recorded in the existing `service-status-intervals` AD*; the public JSON field rename). Latest AD in the file is `AD-019` + addenda.
- **design.md's own Risks & Concerns mitigation** for the breaking public-JSON change: "Call it out explicitly in `CHANGELOG.md` under `### Changed`". `hourly_history` → `history` is an uncoordinated breaking change for any third-party consumer of the public status JSON, and it is currently undocumented.
- **design.md's closing note**: a clarifying addendum to `AD-012` explaining why `range` rejects while `page` clamps.

Also worth a line: `uptime_percent` is now rendered on the public status page for the first time — a user-visible addition, not just a refactor.

Not a code defect, but it is a documented gate this repo treats as part of "done", and it is the one thing that would make a future reader re-derive all of it from the diff.

### 3 — LOW (test coverage vs. spec edge case): `asOf` stall-clamp untested at wide bucket widths

spec.md's Edge Cases require the `asOf` clamp to hold "for every range tier, not just 24h". The only clamp test is `hourly_test.go:160`, at `bucketWidth=1h`. The implementation is width-agnostic (the clamp is in the shared resolution loop, and mutants M2/M5 confirm the shared path is exercised), so this is a missing test rather than a suspected defect — but it is the one spec-level statement in this feature with no direct assertion. One `bucketWidth=24h` variant of that subtest closes it.

### 4 — LOW (pre-existing environmental flake, NOT feature-related): shared-DB parallel pollution

The parallel integration run failed once with `internal/db`'s `TestDeleteClosedBefore_DeletesOnlyClosedRowsOlderThanCutoff` → *"deleted 2 rows, want 1"*. Investigated rather than waved through:

- The test asserts a **global** row count from `DeleteClosedBefore(cutoff)` (`status_interval_repository_test.go:381`), which is not service-scoped — so any concurrently-running package that leaves an old closed interval in the shared database breaks the count.
- Non-deterministic at HEAD (failed run 1, passed run 2 of an identical `./internal/db/... ./internal/api/...` invocation).
- **Baseline comparison**: a worktree at `4cbfd2d~1` (pre-feature) flakes the same way under the same load — run 1 failed `TestServiceRepository_ListPaginated_Page2_ReturnsRemainder`, runs 2-4 passed. Same class, different victim.
- `-p 1` serial: **every package green**, including `internal/db`, `internal/poller`, `internal/cli`.

Conclusion: pre-existing, environmental, matching what earlier phase agents observed. **Not a regression from this feature** and not a blocker. Worth fixing independently by scoping those assertions to their own `service_id`.

---

## Summary

The implementation matches the spec and the design, the tests assert the spec's exact values rather than merely covering the area, and the suite kills every behavior-level mutation attempted across both backend and frontend. The reported T8 deviation was necessary (the frontend genuinely never rendered `uptime_percent`) and is implemented correctly, including the null → `"—"` contract.

**PASS.** Findings #1 (pre-existing bucket-boundary leak, amplified by 24h buckets) and #2 (missing `STATE.md`/`CHANGELOG.md` entries) should be closed before this ships in a release; #3 and #4 are cleanup.
