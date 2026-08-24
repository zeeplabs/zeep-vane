# Design: Public Status Hourly History

## Architecture

```
status_snapshots (existing table, existing index (service_id, fetched_at))
        │
        ▼
StatusSnapshotRepository.ListRecentByServices(ctx, serviceIDs, since)  [new]
        │  flat []StatusSnapshot, ordered by fetched_at ASC
        ▼
history.BuildHourly(snapshots []StatusSnapshot, now time.Time, loc *time.Location)  [new, pure Go]
        │  per service: 24 ordered hourly buckets, last-status-wins, no_data when empty
        ▼
PublicStatusHandler.composeResponse  [extended]
        │  adds HourlyHistory []publicHourlyStatusResponse to each publicServiceResponse
        ▼
GET / (production, Host-routed) and GET /api/status-pages/{id}/public-preview (admin preview)
        │  same response shape, both go through composeResponse - no divergence (UPT-08)
        ▼
web/src/features/public-status: hooks.ts (typed response) → PublicStatusPage.tsx (renders 24 bars + tooltip)
        │  history.ts (fake seed) deleted entirely
```

## Components

### 1. `internal/db/status_snapshot_repository.go` — `ListRecentByServices`

```go
func (r *StatusSnapshotRepository) ListRecentByServices(ctx context.Context, serviceIDs []string, since time.Time) ([]StatusSnapshot, error)
```

`SELECT service_id, status, fetched_at FROM status_snapshots WHERE service_id = ANY($1) AND fetched_at >= $2 ORDER BY service_id, fetched_at ASC`. Reuses the existing `idx_status_snapshots_service_id_fetched_at` index - no migration needed. `error_budget_remaining` is not selected (unused by this feature, keeps the row narrow). Returns a flat slice; the caller groups by `ServiceID` (a public status page rarely has more than a handful of services, so an in-memory group-by is simpler and just as fast as a second SQL round trip).

`since` is computed by the caller as `now - 24h` (see below), with a little slack unnecessary: the bucketing function only trusts what's inside a bucket's own `[start, end)` window regardless of what the query returned, so an exact 24h cutoff is fine - any bucket-cutoff edge row either falls in the oldest bucket or is silently unused, never mis-bucketed.

### 2. `internal/history` (new package) — `BuildHourly`

A new small package rather than adding this to `internal/api` or `internal/db`: the bucketing algorithm is pure (no I/O, no DB, no HTTP), so it gets its own unit tests with zero database dependency - matching the project's existing convention of isolating pure logic (`internal/poller/retry.go`'s `isTransient`, `normalizeStatus`) from I/O-bound code.

```go
package history

type HourlyBucket struct {
    Start  time.Time // bucket's local (America/Sao_Paulo) start instant, as a real time.Time in that location
    Status string    // "operational" | "degraded" | "outage" | "no_data"
}

// BuildHourly returns exactly windowHours buckets covering
// [now-windowHours, now], one per hour, oldest first, in loc's local time.
// Each bucket's status is the status of the snapshot with the latest
// FetchedAt that falls within that bucket's [start, end) window; a bucket
// with no snapshot in its window is "no_data". snapshots need not be
// pre-filtered to the window or sorted - BuildHourly only uses what falls
// inside each bucket.
func BuildHourly(snapshots []db.StatusSnapshot, now time.Time, loc *time.Location, windowHours int) []HourlyBucket
```

Algorithm: truncate `now` to its local hour in `loc` (`now.In(loc)`, zero out minute/sec/nsec) to get the current (right-most) bucket's start; the left-most bucket start is `windowHours-1` hours before that. Build `windowHours` buckets up front, each `no_data`. For each snapshot, compute its bucket index via `int(snapshot.FetchedAt.In(loc).Sub(leftmostStart) / time.Hour)`; if the index falls in `[0, windowHours)`, and this snapshot's `FetchedAt` is the latest seen so far for that index, set that bucket's status (last-status-wins - UPT-03). This is a single linear pass, no sorting required (tracks latest-seen-per-bucket as it goes), and never runs even a partial DB scan more than once regardless of `snapshots` ordering - though the repository already returns them ordered as a matter of query cleanliness.

**Timezone/tzdata note:** `time.LoadLocation("America/Sao_Paulo")` needs the IANA tzdata database. Vane ships as a single low-footprint binary (AD-001) that may run in a minimal container image (e.g. `scratch`/distroless/alpine without the `tzdata` OS package installed) - relying on the *host's* tzdata would make this feature silently break (or panic) on exactly the kind of deployment this project is built for. `cmd/vane/main.go` gets a blank import `_ "time/tzdata"` so the IANA database is compiled into the binary itself (~450KB, acceptable for AD-001's "low footprint" bar) - `LoadLocation` then always succeeds, with no OS dependency, on every platform this binary runs on.

### 3. `internal/api/public_status_handler.go` — response contract extension

```go
type publicHourlyStatusResponse struct {
    Start  time.Time `json:"start"`  // RFC3339, America/Sao_Paulo offset
    Status string    `json:"status"`
}
```

`publicServiceResponse` gains `HourlyHistory []publicHourlyStatusResponse`. `composeResponse` calls `snapshots.ListRecentByServices(ctx, serviceIDs, time.Now().Add(-24*time.Hour))` once (not once per service - a single query, then grouped in Go by `ServiceID`, mirroring the existing `LatestFetchedAtByService` map-by-service-id pattern already used two lines above it), then `history.BuildHourly` once per service from that service's slice (or `nil` for a service with zero rows - `BuildHourly` handles an empty slice by returning all-`no_data` buckets, satisfying UPT-06 with no special-case branch). `saoPauloLocation` is loaded once at handler construction (`NewPublicStatusHandler`) via `time.LoadLocation`, not per-request - `LoadLocation` does a filesystem/embedded-db lookup that has no reason to repeat 	on every request; a load failure at construction time is a startup-time `serve` error (fail fast), not a per-request 500.

No new endpoint: `GET /` and `GET /api/status-pages/{id}/public-preview` already share `composeResponse` (existing code, `public_status_preview_handler.go`) - this extension is free for both, satisfying UPT-08 automatically rather than needing separate wiring.

### 4. Frontend — `web/src/features/public-status/`

- **Delete** `history.ts` entirely (the fake seed) and its test file.
- `types`/`hooks.ts`: extend the service type with `hourly_history: { start: string; status: "operational" | "degraded" | "outage" | "no_data" }[]`.
- `PublicStatusPage.tsx`: replace the `buildServiceHistory(...)` call with a direct render of `service.hourly_history` - 24 `<div>` bars, colored via the existing Nocturne status-color tokens (`--color-success`/`--color-warning`/`--color-critical`) plus one new neutral-gray token for `no_data` (`--color-neutral-500`ish, already in the Nocturne neutral ramp per `dashboard-handoff/README.md`'s token list - no new color needs inventing). Each bar gets a native `title` attribute (instant, accessible, keyboard-focusable via `tabIndex=0` + the same `title` - satisfies UPT-05's hover/focus requirement with a browser-native tooltip, no new UI dependency) formatted as `"{dd/MM}, {HH}h–{HH+1}h · {status label PT-BR}"`, computed client-side from `start` (already carrying the correct `America/Sao_Paulo` offset from the backend, so no client-side timezone math is needed - `new Date(start)` plus `Intl.DateTimeFormat` with an explicit `timeZone: "America/Sao_Paulo"` for the label, so the label is correct even if a visitor's own browser/OS is in a different timezone).

## Risks & Concerns

- **tzdata footprint**: `time/tzdata` blank import adds ~450KB to the binary. Accepted - AD-001's "low footprint" is about avoiding a sidecar proxy/multi-process footprint, not byte-shaving the binary; correctness on minimal containers outweighs this.
- **Query volume growth**: `ListRecentByServices` scans up to 24h of snapshots per service on every public page load (every ~2 minutes per visitor tab, per the existing polling footer). At a busy default poll interval (60s) this is at most ~1440 rows per service per query - trivial for Postgres with the existing index, and bounded regardless of how long the installation has been running (unlike a naive `SELECT *` with no time filter). No caching layer added - out of scope; the existing 2-minute client poll interval already caps request frequency per visitor.
