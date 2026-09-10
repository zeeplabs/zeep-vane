# Verifier Validation — poller-status-real-state

**Result**: PASS

Diff range verified: `7e9050d..69d6e9c` (commits `aea8b50`, `2db899a`, `a52dab4`, `69d6e9c`).

## Per-AC evidence

| AC | File:Line | Assertion | Spec outcome | Covered? |
| --- | --- | --- | --- | --- |
| POLLST-01 (leader_elected + replica identity) | `internal/db/poller_leadership_repository_test.go:31-67` | Session holds advisory lock with `application_name` X → `CurrentLeader()` returns `ApplicationName==X`, non-zero `BackendStart` | AC1 | Yes |
| POLLST-01 (API surface) | `internal/api/poller_status_test.go:283-309` | Lock held, no integration → `leader_elected:true`, `replica.application_name` set, non-zero `BackendStart` | AC1 | Yes |
| POLLST-02 (poller_running true) | `internal/api/poller_status_test.go:314-339` | Lock held + `UpsertDatadog` → `poller_running:true` | AC2 | Yes |
| POLLST-03 (poller_running false, no integration) | `internal/api/poller_status_test.go:283-309` | Lock held, no integration → `poller_running:false` | AC3 | Yes |
| POLLST-04 (no leader) | `internal/api/poller_status_test.go:256-278`, `internal/db/poller_leadership_repository_test.go:15-26` | No lock holder → `leader_elected:false`, `poller_running:false`, `replica:nil` | AC4 | Yes |
| POLLST-05 (live query, no cache) | `internal/api/poller_status_test.go` (every request re-derives from DB; no persistence layer added — confirmed by absence of any new table/write path in `poller_leadership_repository.go`) | Every call queries `pg_locks`/`pg_stat_activity` fresh | AC5 | Yes (structural, not a dedicated test — reasonable given no cache exists to test against) |
| POLLST-05/06 (checks_last_minute) | `internal/db/status_interval_repository_test.go:408-447`, `internal/api/poller_status_test.go:346-386` | Rows inside 60s window counted, outside window not counted; API reflects `+1` after a fresh write | Story 2 AC1 | Yes |
| POLLST-06 (integration fields unchanged) | `internal/api/poller_status_test.go:73-142` | `status`/`last_error`/`last_checked_at` reflect persisted integration state exactly as before | Story 2 AC2 | Yes |
| POLLST-07/edge (0 checks is valid) | `internal/db/status_interval_repository_test.go:456-480` | Stale row outside window doesn't increase count; 0 is not an error | Story 2 AC3 | Yes |
| Replica hostname identity | `internal/cli/poller_manager_test.go:315-346,349-358` | `HOSTNAME` env sets `application_name`; unset falls back to `"unknown"` | Assumption row / Edge case | Yes |

**Cross-check against `killPollerLeaderBackend` reference shape** (`internal/cli/poller_manager_test.go:262-278`): the new query in `poller_leadership_repository.go:49-54` uses the identical `classid = 0 AND objid = $1 AND objsubid = 1` predicate — confirmed matching.

**Response shape**: `pollerStatusResponse` (`internal/api/poller_status.go:79-92`) contains only `leader_elected`, `poller_running`, `replica`, `checks_last_minute`, `items`, `total`, `page`, `page_size`. No `region`/`cpu`/`mem`/`queue_depth`/node-list fields present (`grep` for those terms in the file returns nothing). No restart-poller route added (`internal/cli/routes.go` diff only rewires the existing handler's constructor args).

**`poller_running` requires both conditions**: `internal/api/poller_status.go:138-152` — `PollerRunning` is only set inside the `if leader != nil` branch, and only when `GetDatadog` succeeds; confirmed correct in the shipped code.

## Spec-precision gaps

- POLLST-05's "live, never persisted" requirement (AC5) has no dedicated negative test (e.g., asserting two rapid calls reflect two different lock holders without any caching layer in between) — coverage here is structural/by-absence-of-cache-code rather than an explicit assertion. Low risk given there's no cache to test.

## Discrimination sensor (mutation testing)

Ran in an isolated `/private/tmp/.../scratchpad/vane-mutant` copy (discarded after testing; working tree/git state untouched).

| # | Mutant | File | Result |
| --- | --- | --- | --- |
| 1 | Removed `AND l.granted = true` from the `pg_locks` query (would report a *waiting*, not-yet-granted lock request as if it were the current leader) | `internal/db/poller_leadership_repository.go` | **Survived** — all `TestPollerLeadershipRepository_*` and `TestPollerStatus_*` tests still passed. No test constructs a contended/waiting lock scenario (all use `pg_try_advisory_lock`, which is only ever "granted" or absent). |
| 2 | Made `poller_running` unconditionally `true` whenever `leader != nil`, ignoring the stored Datadog integration check | `internal/api/poller_status.go` | **Caught** — `TestPollerStatus_LeaderElectedNoIntegration_PollerRunningFalse` failed as expected (`PollerRunning = true, want false`). |
| 3 | Changed `CountUpdatedSince`'s `last_seen_at >= $1` to `>` (exclusive boundary) | `internal/db/status_interval_repository.go` | **Survived** — `TestCountUpdatedSince_CountsOnlyRowsInsideWindow` and `TestCountUpdatedSince_StaleRowOutsideWindow_DoesNotIncreaseCount` both passed; no test writes a row with `last_seen_at` exactly equal to the cutoff to exercise the boundary. |

**Caught: 1/3. Survived: 2/3.**

Both survivors are genuine spec-precision gaps, not code bugs — the shipped code is correct (`>=` and `granted = true` are both present and correct in the real diff), but the test suite doesn't pin these exact behaviors down, so a future regression on either point would go undetected.

## Build/vet/format

- `go build ./...` — clean.
- `go vet ./...` — clean.
- `gofmt -l` on all 9 changed/added Go files — no output (all formatted).
- Integration tests: `TEST_DATABASE_URL=postgres://vane:vane@localhost:5436/vane?sslmode=disable go test -tags=integration -p 1 ./internal/db/... ./internal/cli/... ./internal/api/...` — all packages **PASS** (disposable container `verifier-test-pg2`, stopped and removed after the run; `vane-dev-pg` never touched).

## Verdict rationale

All 6 acceptance criteria (POLLST-01..06) are covered by tests that assert the spec's exact expected outcomes, cross-checked against the existing `pg_locks` query reference shape, and the response shape has no leftover mocked fleet fields or restart endpoint. Full test suite and static checks pass. The two surviving mutants are recorded as gaps for follow-up (boundary/contention edge cases) but do not indicate an actual defect in the shipped implementation — hence **PASS** with noted gaps, not FAIL.
