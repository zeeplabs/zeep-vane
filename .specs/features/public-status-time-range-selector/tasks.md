# Public Status Time-Range Selector Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/public-status-time-range-selector/design.md`
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase sampling. Guidelines found: `AGENTS.md` §3 ("Before considering any change done") and §5 (frontend rules: MSW mocks must mirror real backend shape). No repo-wide coverage-percentage threshold found (no `.nycrc`/coverage gate in CI) - depth target is the strong default (spec ACs 1:1, all edge cases) tempered by the existing per-layer sampling below.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| `internal/history` (pure bucketing logic) | unit | All branches; regression test proving `bucketWidth=1h` output is byte-identical to today's `BuildHourly`, plus one test per new bucket width (6h, 24h) covering boundary alignment and the worst-status-wins rule | `internal/history/*_test.go` (sampled: `hourly_test.go`, no build tag) | `go test ./internal/history/...` |
| `internal/api` query-param parsing (`time_range.go`) | unit | Every valid tier + missing param (default) + invalid value (rejected), 1:1 to TRS-07/Assumptions tier table | `internal/api/*_test.go` with no `//go:build integration` tag (sampled: `pagination_test.go`) | `go test ./internal/api/...` |
| `internal/api` HTTP handlers (`PublicStatusHandler`, `PublicStatusPreviewHandler`) | integration | Every P1 AC exercised end-to-end against real Postgres: all 4 tiers' bucket count/window, uptime % tracking the selected range, preview parity, invalid range → error, `no_data` for out-of-coverage buckets | `internal/api/*_test.go` with `//go:build integration` (sampled: `public_status_handler_test.go`) | `TEST_DATABASE_URL=<dsn> go test -tags=integration ./internal/api/...` (disposable Postgres per `AGENTS.md` §3, never `vane-dev-pg`) |
| `internal/retention` (`Pruner`) | integration | TRS-08/09's exact scenario: a closed interval 90 days old survives one `prune()` cycle at the new retention value; open intervals still never pruned (existing coverage, unchanged) | `internal/retention/pruner_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=<dsn> go test -tags=integration ./internal/retention/...` |
| `internal/cli` (`pruneRetention` constant value) | unit | The constant equals the spec-confirmed 95-day value - a plain assertion, since the integration-level pruning *behavior* is already covered one layer down in `internal/retention` | new `internal/cli/*_test.go`, no build tag | `go test ./internal/cli/...` (only the new file's test runs without `-tags=integration`; full suite needs the tag per existing convention) |
| `web/src/lib/publicStatus.ts` (types only) | none | - (build gate only, no behavior to unit-test in a type-only rename) | - | `npx tsc -b --noEmit` |
| `web/src/test/msw/handlers.ts` (global fixtures) | none directly, but consumed by every test below | - (this file's correctness is exercised transitively by `hooks.test.ts`/`PublicStatusPage.test.tsx`, not tested standalone - matches how the existing `hourly_history` fixture helper is handled today) | `web/src/test/msw/handlers.ts` | `npm run test` (transitively) |
| `web/src/features/public-status/hooks.ts` | unit (vitest + MSW, existing pattern) | Every existing test still passes with the new `range` parameter defaulted; new test proving `range` is part of the React Query cache key (distinct range → distinct fetch, per `AGENTS.md` §5's paginated-`queryKey` rule applied to `range`) | `web/src/features/public-status/hooks.test.ts` | `npm run test` |
| `web/src/features/public-status/PublicStatusPage.tsx` | unit (vitest + Testing Library, existing pattern) | Default 24h render unchanged; each of the 3 additional `Seg` options renders the tier's bar count/labels (TRS-02/03); uptime % updates with range (TRS-04) | `web/src/features/public-status/PublicStatusPage.test.tsx` | `npm run test` |

## Gate Check Commands

> Generated from `AGENTS.md` §3 and `web/package.json`'s `scripts` block.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick (Go unit) | After a task touching only `internal/history` or a non-integration `internal/api`/`internal/cli` test | `go build ./... && go vet ./... && go test ./...` |
| Full (Go integration) | After a task touching `internal/api` HTTP handlers or `internal/retention` | Quick gate, plus: spin up a disposable Postgres (`docker run -d --rm --name vane-test-pg -p 5433:5432 -e POSTGRES_USER=vane -e POSTGRES_PASSWORD=vane -e POSTGRES_DB=vane postgres:16-alpine -c max_connections=300`), then `TEST_DATABASE_URL="postgres://vane:vane@localhost:5433/vane?sslmode=disable" go test -tags=integration ./...`, then `docker stop vane-test-pg` - never against `vane-dev-pg` |
| Frontend | After any `web/` task | `cd web && npx tsc -b --noEmit && npm run test` |
| Format | Every Go task, before commit | `gofmt -l <changed .go files>` (must be empty output) |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Backend domain logic

```
T1
```

### Phase 2: Backend API layer

```
T2 → T3
```

### Phase 3: Retention

```
T4
```

### Phase 4: Frontend

```
T5 → T6 → T7 → T8
```

---

## Task Breakdown

### T1: Generalize `BuildHourly` into `BuildBuckets` with variable bucket width

**What**: Rename `internal/history.HourlyBucket` → `Bucket` and `BuildHourly` → `BuildBuckets`, adding a `bucketWidth time.Duration` parameter (replacing the hardcoded `time.Hour` used for truncation and index math). Apply the day-start-plus-floor alignment formula from `design.md`'s Tech Decisions. Rewrite `internal/history/hourly_test.go` around the new signature.
**Where**: `internal/history/hourly.go`, `internal/history/hourly_test.go`
**Depends on**: None
**Reuses**: The entire worst-status-wins resolution loop and `asOf` stall-clamp logic in `BuildHourly` - only the truncation/index-math lines change.
**Requirement**: TRS-02, TRS-06

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `Bucket`/`BuildBuckets` exist with the design's signature; old names removed (no compatibility alias - `grep` found one caller outside the package, `internal/api/public_status_handler.go`, updated to the new names as a minimal identifier fix only, no behavior/signature change, required to keep `go build ./...` green; that file's real T3 rework is untouched)
- [x] Regression test: calling `BuildBuckets(intervals, now, asOf, loc, 24, time.Hour)` on the exact fixtures `hourly_test.go` used against the old `BuildHourly(intervals, now, asOf, loc, 24)` produces byte-identical output (same `Start`/`Status` per bucket) - written and passing *before* any 6h/24h-specific test is added, per `design.md`'s Risks & Concerns mitigation
- [x] New test: `bucketWidth=6h` aligns bucket starts to 00:00/06:00/12:00/18:00 local time, not an arbitrary offset from `now`
- [x] New test: `bucketWidth=24h` aligns the "today" bucket to local midnight and correctly represents a partial day, mirroring how the old code represented a partial current hour
- [x] New test: an interval spanning multiple wide buckets still contributes its status to every bucket it overlaps (worst-status-wins), generalizing the existing 1h-width test of the same property
- [x] `no_data` still renders for a bucket with zero overlapping intervals, at any bucket width (TRS-06)
- [x] `gofmt -l internal/history/*.go` empty

**Tests**: unit
**Gate**: quick (`go test ./internal/history/...`)

**Commit**: `feat(history): generalize BuildHourly into BuildBuckets with variable bucket width`

---

### T2: Add `internal/api/time_range.go` (range parsing)

**What**: Create `rangeSpec`, the `rangeSpecs` map (24h/7d/30d/90d per `design.md`'s Data Models), `parseRange(r *http.Request) (rangeSpec, bool)`, and `writeInvalidRangeError(w http.ResponseWriter)` (422, matching the `write<Foo>Error` convention).
**Where**: `internal/api/time_range.go`, `internal/api/time_range_test.go`
**Depends on**: None
**Reuses**: `write<Foo>Error` pattern (e.g. `internal/api/email_providers_handler.go:181`); `pagination.go`'s file-per-query-param-concern precedent.
**Requirement**: TRS-07

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [x] `rangeSpecs["24h"/"7d"/"30d"/"90d"]` match `design.md`'s Data Models table exactly (window/bucketWidth/bucketCount)
- [x] Test: `bucketCount * bucketWidth == window` holds for every entry (the invariant `design.md` calls out) - written as a table-driven assertion, not eyeballed
- [x] Test: missing/empty `range` query param → `parseRange` returns the `24h` spec, `ok=true`
- [x] Test: each of the 4 valid values → `parseRange` returns the matching spec, `ok=true`
- [x] Test: an unrecognized value (e.g. `"5d"`, `""` after trim edge cases, `"90D"` wrong case) → `ok=false`
- [x] `writeInvalidRangeError` writes `422` + `{"error":"range must be one of 24h, 7d, 30d, 90d"}` + `Content-Type: application/json`, asserted via `httptest.NewRecorder()`
- [x] `gofmt -l internal/api/time_range*.go` empty

**Tests**: unit
**Gate**: quick (`go test ./internal/api/...`, no `-tags=integration` needed for this file)

**Commit**: `feat(api): add time-range query param parsing for the public status endpoint`

---

### T3: Parameterize `composeResponse`, wire both `Get` handlers, rename response DTO field

**What**: `composeResponse` gains a `rng rangeSpec` parameter; `windowStart := now.Add(-rng.window)` replaces the hardcoded `historyWindowHours * time.Hour`; the `BuildBuckets`/`UptimePercent` calls pass `rng.bucketCount`/`rng.bucketWidth`/`windowStart`. `publicServiceResponse.HourlyHistory` (JSON `hourly_history`) renamed to `History` (JSON `history`); `publicHourlyStatusResponse` renamed to `publicHistoryBucketResponse`. Both `PublicStatusHandler.Get` and `PublicStatusPreviewHandler.Get` call `parseRange(r)` before `composeResponse`, writing `writeInvalidRangeError` and returning early on `ok=false`.
**Where**: `internal/api/public_status_handler.go`, `internal/api/public_status_preview_handler.go`, `internal/api/public_status_handler_test.go`, `internal/api/public_status_preview_handler_test.go`
**Depends on**: T1, T2
**Reuses**: `ListOverlapping`/`UptimePercent` unchanged, per `design.md`'s Code Reuse Analysis - no signature changes to either.
**Requirement**: TRS-01, TRS-02, TRS-03, TRS-04, TRS-05, TRS-07

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Every existing integration test in `public_status_handler_test.go` still passes, updated for the `hourly_history`→`history` rename where they assert on the field name/JSON key
- [ ] New test: request with no `range` param (or `range=24h`) behaves identically to today - 24 buckets, 1h wide (TRS-01)
- [ ] New test: `range=7d` returns 28 buckets, 6h wide, correct `windowStart`
- [ ] New test: `range=30d` returns 30 buckets, 24h wide
- [ ] New test: `range=90d` returns 90 buckets, 24h wide, and (seed data covering it) confirms history beyond 24h/beyond the old 35-day retention is actually retrievable
- [ ] New test: `uptime_percent` for a service with a real outage differs between the `24h` and `90d` ranges for the same underlying data (TRS-04 - proves it's not pinned to 24h)
- [ ] New test: `range=` an invalid value (e.g. `5d`) → `422`, response body matches `writeInvalidRangeError`, and no DB query for services/intervals is issued (assert via a spy/counting fake, per `design.md`'s Error Handling Strategy - "no partial work")
- [ ] New test on `public_status_preview_handler_test.go`: the same range parameter produces the same bucket shape on the preview endpoint as the production endpoint for identical seed data (TRS-05 preview parity)
- [ ] New test: a service/window combination where part of the range predates any data → leading buckets are `no_data`, not an error (TRS-06 at the handler level, generalizing the existing 24h-only version of this test)
- [ ] `gofmt -l internal/api/public_status*.go` empty

**Tests**: integration
**Gate**: full (`TEST_DATABASE_URL=<dsn> go test -tags=integration ./internal/api/...` against disposable Postgres)

**Commit**: `feat(api): parameterize public status history by selected time range`

---

### T4: Bump `status_intervals` retention from 35 to 95 days

**What**: Change `internal/cli/serve.go`'s `pruneRetention` constant from `35 * 24 * time.Hour` to `95 * 24 * time.Hour`. Add a plain unit test asserting the constant's value. Add an integration test to `internal/retention/pruner_test.go` proving a 90-day-old closed interval survives a prune cycle at the new retention value (the spec's own Independent Test for P2).
**Where**: `internal/cli/serve.go`, new `internal/cli/retention_constant_test.go`, `internal/retention/pruner_test.go`
**Depends on**: None (independent of T1-T3; sequenced after Phase 2 only because `design.md` lists it after the main backend change, not because of a real dependency)
**Reuses**: `Pruner`/`NewPruner` unchanged - retention is already a plain `time.Duration` parameter, no interface change needed.
**Requirement**: TRS-08, TRS-09

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `pruneRetention == 95 * 24 * time.Hour`, asserted by the new `internal/cli` unit test (no build tag, runs without `TEST_DATABASE_URL`)
- [ ] New `internal/retention/pruner_test.go` test: seed a closed interval with `ends_at` = now - 90 days, construct `NewPruner(..., 95*24*time.Hour, ...)`, run `prune()` once, assert the row still exists (TRS-08's exact spec scenario)
- [ ] Existing `internal/retention` tests (all hardcoded to `35*24*time.Hour` as a test-local value, independent of the production constant - confirmed via `grep`) remain unchanged and passing - they test `Pruner`'s generic behavior, not the specific production retention value
- [ ] Existing open-interval-never-pruned coverage (TRS-09) still passes unmodified - no code change to that path
- [ ] `gofmt -l internal/cli/*.go internal/retention/*.go` empty

**Tests**: integration (for the pruner behavior test) + unit (for the constant-value test) - see Coverage Matrix, both required
**Gate**: full (retention test needs `-tags=integration` + disposable Postgres; the constant-value test runs under the quick gate too)

**Commit**: `feat(retention): raise status_intervals retention to 95 days for the 90d range tier`

---

### T5: Rename frontend types for the generalized history field

**What**: In `web/src/lib/publicStatus.ts`: rename `PublicHourlyBucket` → `PublicHistoryBucket`, rename `PublicServiceStatus`'s `hourly_history` field → `history`. Add and export `type RangeKey = "24h" | "7d" | "30d" | "90d"`.
**Where**: `web/src/lib/publicStatus.ts`
**Depends on**: None (pure type file, no runtime behavior - could run in any order relative to T1-T4, placed first in Phase 4 since T6/T7/T8 all import from it)
**Reuses**: Existing `PublicServiceStatus` union pattern in the same file, for `RangeKey`'s shape.
**Requirement**: TRS-02 (supports)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] `PublicHistoryBucket`/`RangeKey` exported; `PublicHourlyBucket` removed (no alias - nothing outside this file's own consumers uses the old name once T6-T8 land, and this task's own gate is build-only so a stale reference elsewhere fails loudly)
- [ ] `npx tsc -b --noEmit` passes (expected: fails until T6-T8 also update their imports - if `tsc` is clean before those, the rename is either incomplete or something still references the old name; note the actual pass/fail expectation against the state after this task lands is "will show errors in files not yet updated," which is expected and resolved by T6-T8, not a gate failure specific to this task's own file)

**Tests**: none (type-only layer, per Coverage Matrix)
**Gate**: build (`npx tsc -b --noEmit` - errors elsewhere in the tree from this rename are expected until T6-T8 land; this task's own gate check is that `publicStatus.ts` itself defines the new types correctly, not that the whole tree compiles yet)

**Commit**: `refactor(web): rename PublicHourlyBucket to PublicHistoryBucket, add RangeKey`

---

### T6: Generalize MSW fixtures for both public-status endpoints

**What**: In `web/src/test/msw/handlers.ts`: rename the `hourly_history` field in both the `/api/public-status` and `/api/status-pages/:id/public-preview` mock responses to `history`; generalize `buildFixtureHourlyHistory` into a `buildFixtureHistory(status, bucketCount)` helper; both handlers read `?range=` off the request URL and return the bucket count matching that tier (defaulting to the 24h/24-bucket shape when `range` is absent), mirroring `parseRange`'s default behavior.
**Where**: `web/src/test/msw/handlers.ts`
**Depends on**: T5
**Reuses**: The existing `?page=` reading pattern in this same file (`URL(request.url).searchParams`), applied to `range`.
**Requirement**: TRS-02 (supports, per AGENTS.md §5's mock-mirrors-backend rule)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Both handlers' response bodies use `history` (not `hourly_history`)
- [ ] Both handlers return the correct bucket count for `range=24h/7d/30d/90d` and for a missing `range` (defaults to 24h/24 buckets, matching the real backend's default)
- [ ] `npx tsc -b --noEmit` passes for this file
- [ ] `npm run test` - no regression in any test that relies on the *default* global mock (as opposed to a per-test `server.use()` override) for these two endpoints

**Tests**: none directly (Coverage Matrix: exercised transitively by T7/T8's tests)
**Gate**: build + full frontend test run (`npx tsc -b --noEmit && npm run test`)

**Commit**: `test(web): generalize MSW public-status fixtures for the range parameter`

---

### T7: Thread `range` through `usePublicStatusPage`

**What**: `usePublicStatusPage(id?, range: RangeKey = "24h")`; `fetchPublicStatusPage` appends `&range=${range}` to both the production and preview URLs and reads `data.services[].history` (not `.hourly_history`); `useQuery`'s `queryKey` becomes `["public-status-page", id ?? "production", range]`.
**Where**: `web/src/features/public-status/hooks.ts`, `web/src/features/public-status/hooks.test.ts`
**Depends on**: T5, T6
**Reuses**: The existing `queryKey`/`refetchInterval` structure - only the key gains a segment, `refetchInterval: 120_000` is untouched (per `design.md`, it keeps polling whichever range is currently selected).
**Requirement**: TRS-02, TRS-03 (fetch-triggering half)

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Every existing `hooks.test.ts` test passes unmodified in behavior (default `range="24h"` preserves today's URLs/shape) - field references updated from `hourly_history` to `history` where a test's fixture body sets it
- [ ] New test: calling `usePublicStatusPage(id, "90d")` fetches `...&range=90d` (asserted via an MSW handler that echoes back which range it received, or via `server.use()` inspecting `request.url`)
- [ ] New test: changing the `range` argument between renders produces a distinct `queryKey` / triggers a distinct fetch (not served from the `24h` entry's cache) - the direct proof for the "rapid range switching never shows stale data" edge case in spec.md
- [ ] `npx tsc -b --noEmit` passes

**Tests**: unit
**Gate**: frontend (`npx tsc -b --noEmit && npm run test`)

**Commit**: `feat(web): thread selected time range through usePublicStatusPage`

---

### T8: Add the `Seg` range selector and dynamic labels to `PublicStatusPage`

**What**: Add `const [range, setRange] = useState<RangeKey>("24h")`; render `<Seg options={RANGE_OPTIONS} value={range} onChange={setRange} aria-label="Selecionar período" />` above the service history section; pass `range` into `usePublicStatusPage`; replace the hardcoded `"24h atrás"` label with a lookup keyed by `range` (`{ "24h": "24h atrás", "7d": "7 dias atrás", "30d": "30 dias atrás", "90d": "90 dias atrás" }`); update `bucket`/`mockPublicPreview` test helpers in `PublicStatusPage.test.tsx` for the `history` field rename.
**Where**: `web/src/features/public-status/PublicStatusPage.tsx`, `web/src/features/public-status/PublicStatusPage.test.tsx`
**Depends on**: T5, T6, T7
**Reuses**: `Seg` component as-is (no new UI component code, per `design.md`'s Code Reuse Analysis).
**Requirement**: TRS-01, TRS-02, TRS-03

**Tools**:

- MCP: NONE
- Skill: NONE

**Done when**:

- [ ] Every existing `PublicStatusPage.test.tsx` test passes, `mockPublicPreview`/`bucket` helpers updated for `history` (not `hourly_history`)
- [ ] New test: page loads with `24h` selected by default and the existing 24-bar chart, unchanged from today (TRS-01)
- [ ] New test: clicking each of the 3 other `Seg` options (`aria-selected`/role=`tab` per `Seg`'s existing a11y shape) triggers a new fetch (assertable via an MSW handler override) and the leftmost label text updates to match (`"7 dias atrás"` etc.)
- [ ] New test: selecting a different range changes every service's chart on the page, not just one (TRS-03 - page-wide, not per-service) - assert with ≥2 services in the fixture
- [ ] New test: `uptime_percent` displayed changes when range changes, using an MSW override that returns a different `uptime_percent` per range (TRS-04, frontend-side proof that the number rendered actually reflects `data.services[].uptime_percent` per fetch, not a stale cached figure)
- [ ] `npx tsc -b --noEmit` passes

**Tests**: unit
**Gate**: frontend (`npx tsc -b --noEmit && npm run test`)

**Commit**: `feat(web): add time-range selector to the public status page`

---

## Phase Execution Map

```
Phase 1 → Phase 2 → Phase 3 → Phase 4

Phase 1:  T1 -------------------------→ T3
Phase 2:  T2 ------→ T3
Phase 3:  T4
Phase 4:  T5 ------→ T6 ------→ T7 ------→ T8
          T5 -------------------------→ T7
          T5 -------------------------→ T8
          T6 -------------------------→ T8
```

Execution is strictly sequential - there is no intra-phase parallelism. 8 tasks total, ≤ ~8-task single-batch threshold - **executed inline, no sub-agent batches offered.**

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: Generalize `BuildHourly`/`BuildBuckets` | 1 function + its rename, 1 file pair | ✅ Granular |
| T2: `time_range.go` | 1 new file (3 tightly-related declarations: type, map, 2 functions) | ✅ Granular (cohesive - all exist only to answer "what does this range param mean") |
| T3: `composeResponse` + both `Get` handlers + DTO rename | 3 files, one dependency-locked unit (Go requires all three to compile together - `design.md` calls this out explicitly) | ✅ Granular for its class - see Diagram-Definition Cross-Check note below on why this isn't split further |
| T4: Retention constant + its tests | 3 files (1 const change + 2 test files), one cohesive behavior | ✅ Granular |
| T5: Type renames | 1 file | ✅ Granular |
| T6: MSW fixtures | 1 file | ✅ Granular |
| T7: `usePublicStatusPage` | 2 files (impl + its own test) | ✅ Granular |
| T8: `PublicStatusPage` selector UI | 2 files (impl + its own test) | ✅ Granular |

**Note on T3's size**: `design.md`'s Components section is explicit that `composeResponse`'s signature change and both `Get` handlers' `parseRange` wiring cannot be split into separate commits without an intermediate non-compiling state (Go's type system forces both call sites to update atomically with the signature they call). This is the one task in the breakdown sized above "one function/one file" - accepted as the correct atomic unit for this specific change, not a granularity smell to fix.

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | No incoming edge | ✅ Match |
| T2 | None | No incoming edge | ✅ Match |
| T3 | T1, T2 | T2 → T3 (Phase 2 internal edge); T1 is a cross-phase dependency satisfied by Phase 1 completing before Phase 2 starts (phase ordering itself enforces it, not an intra-diagram arrow) | ✅ Match |
| T4 | None | No incoming edge | ✅ Match |
| T5 | None | No incoming edge | ✅ Match |
| T6 | T5 | T5 → T6 | ✅ Match |
| T7 | T5, T6 | T6 → T7 (T5's dependency satisfied transitively via T6, and directly via phase-internal ordering T5 → T6 → T7) | ✅ Match |
| T8 | T5, T6, T7 | T7 → T8 (T5/T6 satisfied transitively via the same phase-ordered chain) | ✅ Match |

**Rule check**: no task depends on a task in a later phase. T3 (Phase 2) depending on T1 (Phase 1) is backward across phases, which is allowed (phases run in order); nothing in Phase 1-3 depends on anything in Phase 4. Clean.

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1: `BuildBuckets` | `internal/history` | unit | unit | ✅ OK |
| T2: `time_range.go` | `internal/api` (non-integration) | unit | unit | ✅ OK |
| T3: `composeResponse` + handlers | `internal/api` HTTP handlers | integration | integration | ✅ OK |
| T4: retention constant + pruner test | `internal/retention` (integration) + `internal/cli` (unit) | integration + unit | integration + unit | ✅ OK |
| T5: type renames | `web/src/lib/publicStatus.ts` | none | none | ✅ OK |
| T6: MSW fixtures | `web/src/test/msw/handlers.ts` | none (transitive) | none | ✅ OK |
| T7: `usePublicStatusPage` | `web/src/features/public-status/hooks.ts` | unit | unit | ✅ OK |
| T8: `PublicStatusPage` | `web/src/features/public-status/PublicStatusPage.tsx` | unit | unit | ✅ OK |

No violations. No task uses "tested in another task" as a substitute for its own required tests - T3, the one multi-file task, includes its own full integration test list in its `Done when` rather than deferring to T1/T2's already-passing unit tests.
