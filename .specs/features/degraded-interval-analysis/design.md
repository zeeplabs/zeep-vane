# Degraded-Interval Analysis Design

## Architecture overview

```
poller.pollService (transition to "degraded")
  └─ StatusIntervalRepository.OpenOrExtend(...)  → returns intervalID (NEW return value, DEGINT-02)
  └─ SLOAnalyzer.HandleTransition
       └─ dispatchDegradedEnrichment(ctx, svc, sloStatus, intervalID)  (NEW param)
            └─ go func() {
                 analysis, err := llmSvc.GenerateDegradedAnalysis(...)   (unchanged call)
                 statusIntervals.SetIntervalAnalysis(ctx, intervalID, analysis)  (NEW repo method, DEGINT-04/05)
                 services.UpdateStatusAnalysis(ctx, svc.ID, &analysis)   (unchanged, parallel)
               }()

history.BuildBuckets(intervals, ...)
  └─ (unchanged) resolves each bucket's Status by priority
  └─ (NEW) attaches Episodes: every interval with Status=="degraded" overlapping the bucket,
           carrying its own Analysis (may be nil), sorted StartsAt DESC

public_status_handler.go
  └─ toPublicHistoryResponses(buckets) → publicHistoryBucketResponse{ Start, Status, Episodes []episodeResponse }

PublicStatusPage.tsx
  └─ hourly bar: if bucket.episodes.length > 0 → wrap in <DegradedEpisodesPopover episodes=.../>
                 else → unchanged div with title=hourlyTooltip(bucket)
```

No new HTTP endpoint, no new poll cycle behavior, no change to bucket status-priority resolution.

## 1. Backend: schema

New migration `NNNN_status_interval_analysis.up.sql` / `.down.sql` (next number after the latest existing migration - check `internal/db/migrations/` at Execute time, don't hardcode a stale number here):

```sql
-- up
ALTER TABLE status_intervals ADD COLUMN analysis TEXT;

-- down
ALTER TABLE status_intervals DROP COLUMN analysis;
```

Nullable, no default, no backfill (spec Assumptions: history stays as-is).

## 2. Backend: `internal/db/status_interval_repository.go`

- `StatusInterval` struct gains `Analysis *string`.
- `OpenOrExtend`'s signature changes from `(...) error` to `(...) (string, error)` - returns the interval's ID in every branch:
  - no open interval → ID from the new `INSERT ... RETURNING id` in `insertOpenInterval` (also changes signature to `(string, error)`).
  - open interval, same status (extend) → `open.ID` (already known from the `SELECT ... FOR UPDATE`).
  - open interval, different status (close + insert) → the new interval's ID from `insertOpenInterval`.
- New method `SetIntervalAnalysis(ctx context.Context, intervalID string, analysis string) error`:
  ```go
  func (r *StatusIntervalRepository) SetIntervalAnalysis(ctx context.Context, intervalID, analysis string) error {
      _, err := r.pool.Exec(ctx, "UPDATE status_intervals SET analysis = $1 WHERE id = $2", analysis, intervalID)
      ...
  }
  ```
  Deliberately unconditional on `ends_at` - DEGINT-05 requires this to work whether or not the interval is still open. An `UPDATE ... WHERE id = $2` matching zero rows (interval since hard-deleted, which nothing in this codebase does) is not an error - same "best-effort, log only" posture as every other post-hoc write in this codebase (audit.Record, etc.) - actually simpler: no row-count check needed at all, a no-op UPDATE is harmless.
- `OpenIntervalsByService` and any other `SELECT ... FROM status_intervals` that builds a full `StatusInterval` needs `analysis` added to its column list and `Scan` target (nullable → `*string`, same pattern already used for other nullable columns in this file/package).
- `ListOverlapping`-style method (whatever `public_status_handler.go` actually calls to fetch `[]StatusInterval` for the window - confirm the exact method name in this repository at Execute time) needs `analysis` in its `SELECT`/`Scan` too, since `history.BuildBuckets` needs it.

## 3. Backend: callers of `OpenOrExtend`

- `internal/poller/poller.go`'s `pollService`: capture the returned interval ID. Only degraded transitions need it (`dispatchDegradedEnrichment` call site) - other branches can discard it (`_`).
- `internal/poller/manual_scheduler.go`: discard the new return value (`_, err := ...`) - manual polling never generates degraded analysis text (it has no SLO/LLM enrichment path at all, confirmed by this session's earlier investigation of `MonitorMode == "polling"` being skipped by the SLO poller entirely).
- Every test double implementing the `statusIntervalWriter`-shaped interface (`fakeIntervalWriter` in `internal/poller/poller_recent_window_test.go`, and any other test file with its own fake) needs its `OpenOrExtend` method signature updated to return `(string, error)` - return a fixed/counter-based fake ID, whatever the existing test's assertions need (most don't care about the ID at all, only call count/args).

## 4. Backend: `internal/poller/analyzer.go`

- `dispatchDegradedEnrichment(ctx, svc, sloStatus)` gains a 4th parameter, `intervalID string`, captured by its caller (`HandleTransition`'s degraded branch) from `OpenOrExtend`'s new return value - `HandleTransition` itself needs the same threading, since it's the function that calls `pollService`'s branch that opens the interval **before** the transition dispatch happens today (confirm exact call order at Execute time reading `poller.go`'s `pollService` in full - the interval must already be open, with its ID known, before `dispatchDegradedEnrichment` is called, which the existing code order already guarantees since `OpenOrExtend` runs before `HandleTransition` is invoked).
- Inside the goroutine, after a successful `GenerateDegradedAnalysis` call, add a call to `a.statusIntervals.SetIntervalAnalysis(dctx, intervalID, analysis)` right alongside the existing `a.services.UpdateStatusAnalysis(dctx, svc.ID, &analysis)` call - both run, neither depends on the other's success (log-and-continue on either's error, matching this file's existing error-handling posture throughout).
- `SLOAnalyzer` needs a new narrow dependency interface (e.g. `intervalAnalysisWriter`) exposing just `SetIntervalAnalysis`, following this file's existing narrow-interface convention (`serviceStatusAnalysisWriter` etc.) - wired from `NewSLOAnalyzer`'s constructor, which needs a new parameter (update every call site: production wiring in `poller.go`'s `NewPoller`/wherever `SLOAnalyzer` is constructed, and every test's `NewSLOAnalyzer(...)` call).

## 5. Backend: `internal/history/hourly.go`

- `Bucket` struct gains `Episodes []Episode`, new type:
  ```go
  type Episode struct {
      StartsAt time.Time
      EndsAt   *time.Time // nil = still open as of asOf
      Analysis *string
  }
  ```
- Inside `BuildBuckets`'s existing per-interval, per-bucket overlap loop (the same loop that already resolves `statusPriority` today): when `interval.Status == "degraded"` and it overlaps the bucket, append an `Episode` built from that interval (reusing the same `asOf`-clamped end-time logic the function already computes for open intervals) to that bucket's `Episodes`.
- After processing all intervals, sort each bucket's `Episodes` by `StartsAt` descending (DEGINT-09) - a small in-place sort per bucket, negligible cost given the existing bucket counts (≤90 buckets).
- This is additive to the existing status-priority resolution - DEGINT-10's clarification: a bucket whose *winning* color is `"outage"` (because an outage interval overlaps too, with higher priority) still gets its degraded interval's `Episodes` attached, since episode attachment is keyed to `interval.Status == "degraded"` directly, not to the bucket's resolved/displayed color.

## 6. Backend: `internal/api/public_status_handler.go`

- New DTO:
  ```go
  type episodeResponse struct {
      StartsAt time.Time  `json:"starts_at"`
      EndsAt   *time.Time `json:"ends_at"`
      Analysis *string    `json:"analysis"`
  }
  ```
- `publicHistoryBucketResponse` gains `Episodes []episodeResponse \`json:"episodes,omitempty"\`` - omitted (not `null`) when empty, matching this file's existing `omitempty` convention for optional fields.
- `toPublicHistoryResponses` maps `bucket.Episodes` into `episodeResponse`s (same shape, direct field copy).

## 7. Frontend: types + data

- `web/src/lib/publicStatus.ts`: `PublicHistoryBucket` (or wherever the bucket type lives) gains `episodes: PublicDegradedEpisode[]`, new type `{ starts_at: string; ends_at: string | null; analysis: string | null }`.

## 8. Frontend: new Popover component

- Add `@radix-ui/react-popover` (new dependency, same family as the already-used `@radix-ui/react-dialog`) to `web/package.json`.
- New `web/src/components/ui/Popover.tsx`: thin wrapper around `@radix-ui/react-popover`'s `Root`/`Trigger`/`Portal`/`Content`, styled to match this codebase's existing floating-panel look (`Dialog.tsx`'s classes as the closest precedent - border/divider/surface/shadow tokens, not the handoff-mock-specific one-off styling). Exposes a minimal `{ trigger: ReactNode; children: ReactNode }`-shaped API - not a general-purpose kitchen-sink component, scoped to what this feature needs (spec Out of Scope).
- `Tooltip.tsx` stays completely untouched.

## 9. Frontend: `PublicStatusPage.tsx` wiring

- The existing hourly-bar `<div>` (currently unconditionally rendering with `title={hourlyTooltip(bucket)}`) branches: `bucket.episodes.length > 0` → render the bar as the `Popover`'s `trigger`, with `Content` listing each episode (`hourlyTooltip`-style time-range formatting reused, plus `episode.analysis ?? t("...noReasonRecorded")`), most-recent-first (array already arrives sorted from the backend, DEGINT-09 - no client-side re-sort needed). Otherwise → unchanged `title`-only `<div>`.
- New i18n keys for the popover's placeholder text and any label/heading it needs (pt-BR + en, same pattern as every other user-facing string in this codebase).

## Test strategy pointers (elaborated fully in tasks.md)

- `internal/db`: `OpenOrExtend` returns the right ID in all 3 branches; `SetIntervalAnalysis` persists correctly and works on a closed interval.
- `internal/poller`: the degraded-transition path calls `SetIntervalAnalysis` with the ID captured at dispatch time, not a re-fetched "currently open" one - the discriminating test simulates the interval closing (or a second degraded interval opening) between dispatch and the goroutine's completion, then asserts the text landed on the *original* interval's row, not the new one.
- `internal/history`: multi-episode-per-bucket ordering, outage-wins-color-but-degraded-keeps-episodes case (DEGINT-10), no-episodes-for-non-degraded-intervals.
- `internal/api`: DTO shape, `omitempty` behavior.
- Frontend: Popover opens on click, closes on outside-click/Escape/re-click, keyboard-reachable; non-interactive bar unaffected for zero-episode buckets.
