# Degraded-Interval Analysis Tasks

**Spec**: `.specs/features/degraded-interval-analysis/spec.md`
**Design**: `.specs/features/degraded-interval-analysis/design.md`
**Status**: In progress (T1-T4 done)

---

## Test Coverage Matrix

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| Migration | integration | up/down apply cleanly on a disposable DB, `analysis` nullable, no default | `internal/db/migrations/*_test.go` pattern (if one exists for recent migrations) or covered implicitly by repository tests | `go test -tags=integration ./internal/db` |
| `StatusIntervalRepository` (`OpenOrExtend` ID return, `SetIntervalAnalysis`) | integration | ID returned in all 3 branches (new/extend/close+insert); `SetIntervalAnalysis` persists on open AND closed intervals; nullable `analysis` round-trips via `Get`/`OpenIntervalsByService`/whatever feeds `BuildBuckets` | `internal/db/status_interval_repository_test.go` | mesmo |
| `poller.pollService` + `SLOAnalyzer` (race-safety) | integration | degraded transition captures interval ID at dispatch; goroutine writes to that ID even after the interval closed or a second degraded interval opened before the LLM call finished (the discriminating test for DEGINT-05) | `internal/poller/poller_recent_window_test.go` / `internal/poller/analyzer_test.go` | `go test -tags=integration ./internal/poller` |
| `history.BuildBuckets` | unit | episodes attached only to degraded intervals; sorted most-recent-first; outage-wins-color bucket still carries its degraded episode(s) (DEGINT-10); non-degraded bucket has zero episodes | `internal/history/hourly_test.go` | `go test ./internal/history` |
| `public_status_handler.go` DTO | integration | `episodes` field present with right shape, `omitempty` when empty | `internal/api/public_status_handler_test.go` | `go test -tags=integration ./internal/api` |
| Frontend types/data | unit | new fields parsed, MSW mock updated to the real shape | `web/src/test/msw/handlers.ts`, relevant `*.test.ts` | `cd web && npm run test` |
| `Popover.tsx` | component | opens on click, closes on outside-click/Escape/re-click, focus-trap/keyboard reachable | `web/src/components/ui/Popover.test.tsx` | mesmo |
| `PublicStatusPage.tsx` wiring | component | bar with episodes is interactive and shows the right content; bar without episodes stays non-interactive with unchanged `title` behavior | `web/src/features/public-status/PublicStatusPage.test.tsx` | mesmo |

## Gate Check Commands

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick | Any Go task | `go build ./... && go vet ./... && gofmt -l <changed files>` |
| Full (backend) | After any task touching `internal/db`/`internal/poller`/`internal/history`/`internal/api` | disposable Postgres per AGENTS.md §3: `TEST_DATABASE_URL=... go test -tags=integration -p 1 ./internal/db/... ./internal/poller/... ./internal/history/... ./internal/api/...` |
| Frontend | Any frontend task | `cd web && npx tsc -b --noEmit && npm run test` |
| Release gate | End of Execute | `go build ./... && go test ./... && go vet ./...`, full integration gate, frontend gate |

---

## Execution Plan

```
T1 → T2 → T3 → T4 → T5 → T6 → T7 → T8 → T9 → T10
```

Strictly sequential: each backend task depends on the previous layer's shape being final (schema → repo → poller/analyzer → history → API → frontend types → frontend components).

---

## Task Breakdown

### T1: Migration - `status_intervals.analysis` column

**What**: New migration (next number after the latest in `internal/db/migrations/` at execution time - do not assume a fixed number), `ALTER TABLE status_intervals ADD COLUMN analysis TEXT` + matching `.down.sql` dropping it.
**Where**: `internal/db/migrations/NNNN_status_interval_analysis.{up,down}.sql`
**Depends on**: None
**Reuses**: existing migration numbering/naming convention
**Requirement**: DEGINT-01

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] `up.sql` applies cleanly on a disposable DB (fresh + already-populated `status_intervals`)
- [ ] `down.sql` reverts cleanly
- [ ] Gate check passes: full

**Tests**: integration
**Gate**: full

---

### T2: `StatusIntervalRepository` - ID-returning `OpenOrExtend` + `SetIntervalAnalysis`

**What**: `OpenOrExtend` signature becomes `(ctx, serviceID, status, errorBudgetRemaining, at) (string, error)`, returning the open/extended interval's ID in all 3 branches (`insertOpenInterval` also gains `RETURNING id` + changes its own signature to `(string, error)`). New method `SetIntervalAnalysis(ctx, intervalID, analysis string) error` (unconditional `UPDATE ... WHERE id = $2`, works on closed intervals too - DEGINT-05). `StatusInterval` struct gains `Analysis *string`; every `SELECT` building a full `StatusInterval` (`OpenIntervalsByService` and whichever method feeds `public_status_handler.go`'s window query) adds `analysis` to its column list/Scan.
**Where**: `internal/db/status_interval_repository.go`, `internal/db/status_interval_repository_test.go`
**Depends on**: T1
**Reuses**: existing transaction/lock pattern in `OpenOrExtend`, existing nullable-column `Scan` convention
**Requirement**: DEGINT-01, DEGINT-02, DEGINT-04, DEGINT-05, DEGINT-06

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] `OpenOrExtend` returns the right ID for: no-open-interval, extend-same-status, close-and-open-different-status
- [ ] `SetIntervalAnalysis` persists text on a still-open interval
- [ ] `SetIntervalAnalysis` persists text on an already-closed interval (the DEGINT-05 discriminating test)
- [ ] `analysis` round-trips as NULL when never set
- [ ] Gate check passes: full

**Tests**: integration
**Gate**: full

---

### T3: Update `OpenOrExtend` callers + test doubles for the new signature

**What**: `internal/poller/poller.go`'s `pollService` captures the returned ID (discards for non-degraded branches); `internal/poller/manual_scheduler.go` discards it (`_, err :=`). Every test fake implementing this call (`fakeIntervalWriter` in `poller_recent_window_test.go` and any other) updates its method signature to return `(string, error)`.
**Where**: `internal/poller/poller.go`, `internal/poller/manual_scheduler.go`, `internal/poller/poller_recent_window_test.go`, any other fake found via `grep -rn "OpenOrExtend" internal/poller`
**Depends on**: T2
**Reuses**: n/a - mechanical signature-change propagation
**Requirement**: DEGINT-02 (consumer side)

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] `go build ./...` compiles with the new signature everywhere
- [ ] Existing poller tests still pass unmodified in behavior (only signatures changed)
- [ ] Gate check passes: full

**Tests**: integration
**Gate**: full

---

### T4: `SLOAnalyzer` - capture interval ID, write via `SetIntervalAnalysis`

**What**: `dispatchDegradedEnrichment` gains an `intervalID string` parameter, threaded from `HandleTransition`'s degraded branch (sourced from T3's captured `OpenOrExtend` return value - confirm exact call order between `pollService` and `HandleTransition` at implementation time). New narrow dependency interface (e.g. `intervalAnalysisWriter { SetIntervalAnalysis(ctx, intervalID, analysis string) error }`) wired into `SLOAnalyzer`'s constructor (update every `NewSLOAnalyzer` call site, production and test). Inside the goroutine, after a successful non-empty `GenerateDegradedAnalysis`, call `a.statusIntervals.SetIntervalAnalysis(dctx, intervalID, analysis)` alongside the existing `a.services.UpdateStatusAnalysis` call - independent outcomes, both log-and-continue on error.
**Where**: `internal/poller/analyzer.go`, `internal/poller/analyzer_test.go`, `internal/poller/poller.go` (constructor wiring), `internal/cli/routes.go` or wherever `SLOAnalyzer`/`Poller` are constructed in production
**Depends on**: T3
**Reuses**: existing detached-goroutine/timeout/cooldown pattern in `dispatchDegradedEnrichment`
**Requirement**: DEGINT-03, DEGINT-04, DEGINT-05, DEGINT-06, DEGINT-07

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] The interval ID captured at dispatch time is the one `SetIntervalAnalysis` writes to, even when the discriminating race test closes that interval (or opens a second degraded one) before the goroutine finishes
- [ ] A failed/empty/timed-out LLM call leaves `analysis` NULL (unchanged behavior, now also verified for the interval column)
- [ ] `services.status_analysis` read/write behavior is provably unchanged (existing tests for it still pass untouched) - DEGINT-07
- [ ] Gate check passes: full

**Tests**: integration
**Gate**: full

---

### T5: `history.BuildBuckets` - attach degraded episodes per bucket

**What**: `Bucket` gains `Episodes []Episode` (`StartsAt`, `EndsAt *time.Time`, `Analysis *string`). Inside the existing per-interval/per-bucket overlap loop, when `interval.Status == "degraded"` and it overlaps a bucket, append an `Episode` (reusing the same `asOf`-clamped end-time logic already computed for open intervals). After the loop, sort each bucket's `Episodes` by `StartsAt` descending.
**Where**: `internal/history/hourly.go`, `internal/history/hourly_test.go`
**Depends on**: T4
**Reuses**: existing overlap-detection loop, existing `asOf` clamp logic
**Requirement**: DEGINT-08, DEGINT-09, DEGINT-10

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] A bucket overlapping one degraded interval gets exactly one `Episode` with the right start/end/analysis
- [ ] A bucket overlapping two degraded intervals gets both, most-recent-first
- [ ] A bucket whose resolved/displayed status is `"outage"` (because an outage interval also overlaps, out-prioritizing degraded for color) still carries its degraded interval's episode(s) - DEGINT-10
- [ ] A bucket with no degraded interval overlap has zero episodes, regardless of its resolved status
- [ ] Gate check passes: quick (`go test ./internal/history` - no DB dependency, per this package's existing dependency-free design)

**Tests**: unit
**Gate**: quick

---

### T6: `public_status_handler.go` - expose episodes in the DTO

**What**: New `episodeResponse{StartsAt, EndsAt, Analysis}` type. `publicHistoryBucketResponse` gains `Episodes []episodeResponse \`json:"episodes,omitempty"\``. `toPublicHistoryResponses` maps `bucket.Episodes` through.
**Where**: `internal/api/public_status_handler.go`, `internal/api/public_status_handler_test.go`
**Depends on**: T5
**Reuses**: existing DTO/`omitempty` convention in this file
**Requirement**: DEGINT-11, DEGINT-12

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] A degraded bucket's response JSON includes a populated `episodes` array with the right fields
- [ ] A non-degraded (or degraded-with-zero-episodes, shouldn't happen post-T5 but defensively covered) bucket's response omits `episodes` entirely, not `episodes: []` or `episodes: null`
- [ ] No new route added to `routes.go` - same existing endpoint (DEGINT-12)
- [ ] Gate check passes: full

**Tests**: integration
**Gate**: full

---

### T7: Frontend types + MSW mock update

**What**: `PublicHistoryBucket`-equivalent frontend type gains `episodes: PublicDegradedEpisode[]`; new `PublicDegradedEpisode` type `{ starts_at: string; ends_at: string | null; analysis: string | null }`. `web/src/test/msw/handlers.ts`'s mock for this endpoint mirrors the real backend shape exactly (AGENTS.md §5 - MSW mocks must match real response shape).
**Where**: `web/src/lib/publicStatus.ts` (or wherever the type lives), `web/src/test/msw/handlers.ts`
**Depends on**: T6
**Reuses**: existing type/mock conventions
**Requirement**: DEGINT-11 (frontend consumer side)

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] `npx tsc -b --noEmit` clean
- [ ] MSW mock's shape matches the real backend `episodeResponse` shape field-for-field
- [ ] Gate check passes: frontend

**Tests**: unit
**Gate**: frontend

---

### T8: New `Popover.tsx` component

**What**: Add `@radix-ui/react-popover` dependency. New `web/src/components/ui/Popover.tsx`, thin wrapper around Radix's `Root`/`Trigger`/`Portal`/`Content`, styled to match this codebase's existing floating-panel look (border/divider/surface/shadow tokens, `Dialog.tsx` as closest precedent), minimal `{ trigger, children }`-shaped API. `Tooltip.tsx` untouched.
**Where**: `web/package.json`, `web/src/components/ui/Popover.tsx`, `web/src/components/ui/Popover.test.tsx`
**Depends on**: T7
**Reuses**: `Dialog.tsx`'s visual styling as precedent, existing Radix dependency pattern
**Requirement**: DEGINT-13, DEGINT-14, DEGINT-16

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] Opens on trigger click
- [ ] Closes on outside click, Escape, and re-clicking the trigger
- [ ] Reachable and dismissible via keyboard alone (Tab to trigger, Enter/Space to open, Escape to close) - DEGINT-16
- [ ] Gate check passes: frontend

**Tests**: component
**Gate**: frontend

---

### T9: `PublicStatusPage.tsx` wiring

**What**: The hourly-bar `<div>` branches: `bucket.episodes.length > 0` → wrapped as `Popover`'s trigger, `Content` lists each episode (reusing `hourlyTooltip`-style time formatting + `episode.analysis ?? t("publicStatus.episodePopover.noReasonRecorded")`), most-recent-first (already sorted by the backend - DEGINT-09). Otherwise → unchanged `title={hourlyTooltip(bucket)}` div, non-interactive. New i18n keys (pt-BR + en) for the popover's placeholder/heading text.
**Where**: `web/src/features/public-status/PublicStatusPage.tsx`, `web/src/features/public-status/PublicStatusPage.test.tsx`, `web/src/lib/i18n.ts`
**Depends on**: T8
**Reuses**: existing `hourlyTooltip` time-formatting logic, i18n convention
**Requirement**: DEGINT-13, DEGINT-14, DEGINT-15, DEGINT-16

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] A bucket with episodes renders as clickable, opens the popover with the right content on click
- [ ] A bucket with zero episodes keeps today's exact behavior (hover title only, no click affordance)
- [ ] Missing analysis text on an episode shows the placeholder, not blank/undefined/null literal
- [ ] Gate check passes: frontend

**Tests**: component
**Gate**: frontend

---

### T10: Full-suite regression pass + Verifier

**What**: Run the complete gate (backend build/vet/gofmt, full integration suite via disposable Postgres, frontend tsc+tests) once across the whole diff. This is the mandatory closing step before the independent Verifier - not a new code task, just the final release-gate confirmation before handoff.
**Where**: n/a (verification only)
**Depends on**: T9
**Reuses**: n/a
**Requirement**: all (regression confirmation)

**Tools**: MCP NONE · Skill NONE

**Done when**:
- [ ] `go build ./... && go vet ./... && gofmt -l <all touched .go files>` clean
- [ ] Full integration suite green (disposable Postgres, destroyed after, per AGENTS.md §3) - no new failures beyond the already-documented pre-existing flakes (`internal/tls`'s `TestPostgresStorage_Lock_OutOfBandKill_AutoReleases`, `internal/db`'s two `SetActiveProvider`-singleton tests)
- [ ] `cd web && npx tsc -b --noEmit && npm run test` clean

**Tests**: n/a (aggregate gate)
**Gate**: full
