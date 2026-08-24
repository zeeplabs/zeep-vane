# Public Status Hourly History Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/public-status-hourly-history/design.md`
**Status**: Approved

---

## Test Coverage Matrix

> Generated from codebase, project guidelines, and spec. Guidelines found: none dedicated (no `AGENTS.md`/lint-coverage config) - strong defaults applied, floored by existing test depth sampled from `internal/poller/retry_test.go` (pure-logic unit tests), `internal/db/status_snapshot_repository_test.go`-style integration tests, `internal/api/public_status_handler_test.go` (integration, real Postgres), and `web/src/features/public-status/*.test.tsx` (Vitest + Testing Library + MSW).

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| `internal/history` (new pure domain logic) | unit | All branches; 1:1 to UPT-01/02/03/04/06 + every listed edge case (poller-down gap, brand-new service, current partial hour) | `internal/history/*_test.go` | `go test ./internal/history/...` |
| `internal/db` repository (`ListRecentByServices`) | integration | Key query path (service+time range filter) + empty-result case | `internal/db/status_snapshot_repository_test.go` | `TEST_DATABASE_URL=postgres://zeep:zeep@localhost:5434/zeep?sslmode=disable go test -tags integration ./internal/db/...` |
| `internal/api` (`PublicStatusHandler`/preview) | integration | Happy path with real data + UPT-06/07/08 edge cases + regression on existing (non-history) response fields | `internal/api/public_status_handler_test.go`, `internal/api/public_status_preview_handler_test.go` | `TEST_DATABASE_URL=postgres://zeep:zeep@localhost:5434/zeep?sslmode=disable go test -tags integration ./internal/api/...` |
| `cmd/vane/main.go` (tzdata blank import) | none | build gate only - a blank import has no branch to unit test | - | `go build ./...` |
| `web/src/features/public-status` rendering (`PublicStatusPage.tsx`) | unit (Vitest + Testing Library) | Happy path (4 colors render correctly) + tooltip content per bar + `no_data` gray edge + zero-history edge | `web/src/features/public-status/PublicStatusPage.test.tsx` | `cd web && npm run test` |
| `web/src/features/public-status` types/hooks extension | none | type-check only, no new branching logic | - | `cd web && npx tsc -b --noEmit` |

## Gate Check Commands

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick | After T1-T3 (pure Go, no DB/HTTP) | `go build ./... && go vet ./... && gofmt -l . && go test ./internal/history/...` |
| Full | After T4 (integration, needs Postgres) | `TEST_DATABASE_URL=postgres://zeep:zeep@localhost:5434/zeep?sslmode=disable go test -tags integration -count=1 ./internal/db/... ./internal/api/... ./internal/cli/... ./internal/poller/...` |
| Build | After T5-T6 (frontend) and at feature completion | `go build ./... && go vet -tags integration ./... && gofmt -l . && (cd web && npx tsc -b --noEmit && npm run test)` |

---

## Execution Plan

### Phase 1: Backend foundations

```
T1  T2  T3   (independent - no dependency between them, executed in this order)
```

### Phase 2: API wiring

```
T1 → T4
T2 → T4
T3 → T4
```

### Phase 3: Frontend

```
T4 → T5 → T6
```

---

## Task Breakdown

### T1: Add `ListRecentByServices` to `StatusSnapshotRepository`

**What**: New repository method returning every `status_snapshots` row for a set of service IDs since a given time, ordered by `service_id, fetched_at ASC`, using the existing `(service_id, fetched_at)` index.
**Where**: `internal/db/status_snapshot_repository.go`
**Depends on**: None
**Reuses**: `LatestFetchedAtByService`'s existing query/scan pattern in the same file
**Requirement**: UPT-01, UPT-07

**Tools**: MCP: NONE. Skill: NONE.

**Done when**:
- [x] `ListRecentByServices(ctx, serviceIDs []string, since time.Time) ([]StatusSnapshot, error)` implemented
- [x] Empty `serviceIDs` or no matching rows returns an empty (non-nil-error) slice, not an error
- [x] Gate check passes: `TEST_DATABASE_URL=postgres://zeep:zeep@localhost:5434/zeep?sslmode=disable go test -tags integration ./internal/db/...`
- [x] Test count: at least 3 new subtests (rows within window, rows outside window excluded, empty result)

**Tests**: integration
**Gate**: full

**Commit**: `feat(db): add ListRecentByServices for hourly status history`

---

### T2: Add `internal/history` package with `BuildHourly`

**What**: New pure-Go package computing, from a flat slice of `db.StatusSnapshot`, exactly `windowHours` hourly buckets (oldest first) in a given `time.Location`, each bucket's status resolved by last-snapshot-wins within its `[start, end)` window, `no_data` when empty.
**Where**: `internal/history/hourly.go` (new package, new file)
**Depends on**: None
**Reuses**: None (new, deliberately dependency-free pure logic - see design.md's rationale for isolating it from `internal/db`/`internal/api`)
**Requirement**: UPT-01, UPT-02, UPT-03, UPT-04, UPT-06

**Tools**: MCP: NONE. Skill: NONE.

**Done when**:
- [x] `BuildHourly(snapshots []db.StatusSnapshot, now time.Time, loc *time.Location, windowHours int) []HourlyBucket` implemented per design.md's algorithm
- [x] Handles unsorted input snapshots correctly (tracks latest-seen-per-bucket, doesn't assume caller pre-sorted)
- [x] Handles a snapshot exactly on a bucket boundary correctly (falls in the bucket it starts, not the previous one)
- [x] Gate check passes: `go test ./internal/history/...`
- [x] Test count: at least 6 tests covering UPT-01 (24 buckets, correct order), UPT-02 (all 4 status values map through), UPT-03 (last-status-wins within one bucket with 2+ conflicting snapshots), UPT-04 (a timestamp near a DST-adjacent or just-past-midnight `America/Sao_Paulo` boundary buckets correctly), UPT-06 (empty snapshot slice → all `no_data`), and the current-partial-hour assumption (a snapshot in the current, not-yet-complete hour still lands in the right-most bucket)

**Tests**: unit
**Gate**: quick

**Commit**: `feat(history): add BuildHourly pure hourly-bucket aggregation`

---

### T3: Embed IANA tzdata in the binary

**What**: Add the blank import so `time.LoadLocation("America/Sao_Paulo")` works on any deployment target regardless of host OS tzdata availability (design.md's tzdata-footprint decision).
**Where**: `cmd/vane/main.go`
**Depends on**: None
**Reuses**: None
**Requirement**: UPT-04

**Tools**: MCP: NONE. Skill: NONE.

**Done when**:
- [x] `_ "time/tzdata"` blank import added to `cmd/vane/main.go`
- [x] Gate check passes: `go build ./...`

**Tests**: none (build gate only - see coverage matrix)
**Gate**: quick

**Commit**: `build: embed IANA tzdata so LoadLocation works on minimal containers`

---

### T4: Wire hourly history into `PublicStatusHandler`

**What**: `publicServiceResponse` gains `HourlyHistory []publicHourlyStatusResponse`; `composeResponse` calls `ListRecentByServices` once (grouped in Go by service ID) and `history.BuildHourly` per service; `NewPublicStatusHandler` loads and stores the `America/Sao_Paulo` `*time.Location` once at construction (fail-fast on `LoadLocation` error), not per-request. Update every call site of `NewPublicStatusHandler` (`internal/cli/serve.go`'s `newHTTPSServer`, and `internal/cli/routes.go` if it constructs one for the preview handler) and every existing test that constructs one.
**Where**: `internal/api/public_status_handler.go` (primary), `internal/cli/serve.go`, `internal/cli/routes.go`, `internal/api/public_status_handler_test.go`, `internal/api/public_status_preview_handler_test.go`, `internal/cli/serve_test.go` (constructor call-site fixups)
**Depends on**: T1, T2, T3
**Reuses**: The existing `LatestFetchedAtByService` map-by-service-id pattern already in `composeResponse`
**Requirement**: UPT-01 through UPT-08 (all - this is where they become observable end-to-end)

**Tools**: MCP: NONE. Skill: NONE.

**Done when**:
- [x] `publicHourlyStatusResponse{Start time.Time; Status string}` added; `publicServiceResponse.HourlyHistory` populated for every service in the response
- [x] A service with zero snapshots ever gets 24 `no_data` buckets, not an omitted field or a 500 (UPT-06)
- [x] The preview endpoint (`public_status_preview_handler.go`, shared `composeResponse`) returns the identical shape with no separate wiring (UPT-08)
- [x] No new call to any Datadog client anywhere in this path (UPT-07) - grep confirms `composeResponse`'s only new dependency is `StatusSnapshotRepository`/`history.BuildHourly`
- [x] Existing fields (`Name`, `Status`, `LastUpdatedAt`, incidents, company) are unchanged - no regression
- [x] Gate check passes: `TEST_DATABASE_URL=postgres://zeep:zeep@localhost:5434/zeep?sslmode=disable go test -tags integration -count=1 ./internal/db/... ./internal/api/... ./internal/cli/... ./internal/poller/...`
- [x] Test count: at least 5 new/updated tests across `public_status_handler_test.go`/`public_status_preview_handler_test.go` (24-bucket happy path with real inserted snapshots at known hours and known statuses, zero-snapshot service, preview-vs-production shape parity, no-Datadog-call assertion via existing test doubles, existing-fields regression)

**Tests**: integration
**Gate**: full

**Commit**: `feat(api): serve real per-hour status history on public status pages`

---

### T5: Delete the fake history seed and extend frontend types

**What**: Delete `web/src/features/public-status/history.ts` and its test file entirely (the hardcoded per-service-name seed). Extend the public-status response type and `hooks.ts` to carry `hourly_history: { start: string; status: "operational" | "degraded" | "outage" | "no_data" }[]` per service, matching T4's JSON shape exactly.
**Where**: `web/src/features/public-status/history.ts` (delete), `web/src/features/public-status/history.test.ts` (delete, if present), `web/src/features/public-status/hooks.ts` or wherever the response type is declared
**Depends on**: T4
**Reuses**: Existing MSW handler pattern in `web/src/test/msw` for whatever test fixtures reference the public-status response shape
**Requirement**: UPT-01 (contract prerequisite)

**Tools**: MCP: NONE. Skill: NONE.

**Done when**:
- [x] `history.ts` and its test are deleted; nothing in `web/src/features/public-status` imports `buildServiceHistory` anymore
- [x] Type for a public-status service includes `hourly_history` matching T4's backend shape
- [x] Any MSW fixture/mock response used by existing public-status tests includes a plausible `hourly_history` array so downstream tests (T6) have real fixture data to render against
- [x] Gate check passes: `cd web && npx tsc -b --noEmit`

**Tests**: none (type-only change; rendering behavior is verified in T6)
**Gate**: build

**Commit**: `refactor(public-status): remove fake history seed, add real history type`

---

### T6: Render real per-hour bars with tooltip in `PublicStatusPage`

**What**: Replace the old `buildServiceHistory(...)` render call with a direct render of `service.hourly_history` - 24 bars colored green/yellow/red/gray per status, each with a native `title` attribute (and `tabIndex={0}` for keyboard-focus parity) showing the local date, hour range, and Portuguese status label, computed via `Intl.DateTimeFormat` with an explicit `America/Sao_Paulo` timezone.
**Where**: `web/src/features/public-status/PublicStatusPage.tsx`, `web/src/features/public-status/PublicStatusPage.test.tsx`
**Depends on**: T5
**Reuses**: Existing Nocturne status-color tokens (`--color-success`/`--color-warning`/`--color-critical`) plus one existing neutral-ramp token for `no_data` gray - no new colors invented
**Requirement**: UPT-01, UPT-02, UPT-05, UPT-06

**Tools**: MCP: NONE. Skill: NONE.

**Done when**:
- [x] 24 bars render per service, oldest-left to current-hour-right, each colored per its `status`
- [x] Each bar's `title` (and accessible tooltip content) shows the correct local date, hour range (e.g. "24/08, 14h–15h"), and PT-BR status label (Operacional/Degradado/Interrupção/Sem dados)
- [x] A service with all-`no_data` history still renders 24 gray bars, not an empty/missing row
- [x] Gate check passes: `cd web && npm run test`
- [x] Test count: at least 4 new/updated tests (colors render correctly per status, tooltip text content per bar, all-`no_data` renders gray not blank, 24-bar count assertion)

**Tests**: unit
**Gate**: build

**Commit**: `feat(public-status): render real per-hour uptime bars with tooltip`

---

## Phase Execution Map

```
Phase 1 → Phase 2 → Phase 3

Phase 1:  T1   T2   T3   (independent)
Phase 2:            T1 → T4
                     T2 → T4
                     T3 → T4
Phase 3:                  T4 → T5 → T6
```

Execution is strictly sequential - there is no intra-phase parallelism. T4 depends on all of T1-T3; T5 depends on T4; T6 depends on T5.

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: Add `ListRecentByServices` | 1 repository method, 1 file | ✅ Granular |
| T2: Add `internal/history` package | 1 function, 1 new package | ✅ Granular |
| T3: Embed tzdata | 1 blank import, 1 file | ✅ Granular |
| T4: Wire history into handler | 1 handler + its direct call-site fixups (cohesive - the handler's constructor signature change forces them) | ✅ Granular (cohesive) |
| T5: Delete seed, extend types | 1 deletion + 1 type extension, cohesive (same contract change) | ✅ Granular (cohesive) |
| T6: Render bars + tooltip | 1 component's render logic | ✅ Granular |

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | None (Phase 1, no arrow) | ✅ Match |
| T2 | None | None (Phase 1, no arrow) | ✅ Match |
| T3 | None | None (Phase 1, no arrow) | ✅ Match |
| T4 | T1, T2, T3 | T1→T4, T2→T4, T3→T4 | ✅ Match |
| T5 | T4 | T4→T5 | ✅ Match |
| T6 | T5 | T5→T6 | ✅ Match |

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1 | `internal/db` repository | integration | integration | ✅ OK |
| T2 | `internal/history` domain logic | unit | unit | ✅ OK |
| T3 | `cmd/vane/main.go` (import only) | none | none | ✅ OK |
| T4 | `internal/api` handler | integration | integration | ✅ OK |
| T5 | frontend types/hooks | none | none | ✅ OK |
| T6 | frontend rendering | unit | unit | ✅ OK |
