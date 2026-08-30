# HA Multi-Replica Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/ha-multi-replica/design.md`
**Status**: Draft

---

## Test Coverage Matrix

> Generated from codebase (`AGENTS.md` §3, `Makefile`, existing `_test.go`/`*_integration_test.go` samples) and spec. Guidelines found: `AGENTS.md` (backend gate: `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l <changed files>`; integration gate only against a disposable Postgres container, never `vane-dev-pg`).

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| `internal/pglock` (new primitive) | unit + integration | All branches (acquire/already-held/release/healthy-after-kill); every HA edge case touching lock behavior | `internal/pglock/pglock_test.go` (unit, fakeable parts), `internal/pglock/pglock_integration_test.go` (`//go:build integration`) | `go test ./internal/pglock/...` (unit), `TEST_DATABASE_URL=... go test -tags=integration ./internal/pglock/...` |
| `internal/cli.PollerManager` leader election (modified) | integration (needs real Postgres for advisory-lock semantics) | 1:1 to HA-01..HA-07 | `internal/cli/poller_manager_test.go` (`//go:build integration`, already the pattern for this file) | `TEST_DATABASE_URL=... go test -tags=integration ./internal/cli/...` |
| `internal/ratelimit.IPLimiter` (modified) | unit (fake store) + integration (real Postgres, cross-instance) | 1:1 to HA-08..HA-12, plus existing burst/idle-eviction cases (floor for thoroughness) | `internal/ratelimit/ip_limiter_test.go` (unit), `internal/ratelimit/ip_limiter_integration_test.go` (new, `//go:build integration`) | `go test ./internal/ratelimit/...` (unit), `TEST_DATABASE_URL=... go test -tags=integration ./internal/ratelimit/...` |
| `internal/tls.PostgresStorage` (new) | integration (Postgres-backed `certmagic.Storage`, no meaningful fake) | Every `certmagic.Storage`/`Locker` method; 1:1 to HA-13..HA-18 | `internal/tls/postgres_storage_integration_test.go` (new, `//go:build integration`) | `TEST_DATABASE_URL=... go test -tags=integration ./internal/tls/...` |
| `internal/tls.NewManager` (modified signature) | integration (existing pattern for this file) | Existing coverage preserved, updated for `PostgresStorage` instead of `FileStorage` assertions | `internal/tls/manager_test.go`, `internal/tls/manager_integration_test.go` | `TEST_DATABASE_URL=... go test -tags=integration ./internal/tls/...` |
| Migrations (`0017`, `0018`) | integration (via `db.MigrateUpEmbedded`, exercised transitively by every integration test above) | Migration applies cleanly up/down | Exercised transitively; no standalone migration test file in this repo's existing convention | `TEST_DATABASE_URL=... go test -tags=integration ./internal/db/...` |
| `internal/cli/serve.go`, `internal/cli/routes.go` wiring (modified) | build gate + existing integration coverage of `serve`/`routes` (unchanged call shape) | No new test required beyond compiling and existing `serve_test.go`/`routes_test.go` continuing to pass | `internal/cli/serve_test.go`, `internal/cli/routes_test.go` (existing) | `TEST_DATABASE_URL=... go test -tags=integration ./internal/cli/...` |
| `charts/zeep-vane/` (chart cleanup) | none (build/lint gate only) | `helm lint` and `helm template` clean across existing flag combinations | `charts/zeep-vane/**` | `helm lint charts/zeep-vane`, `helm template charts/zeep-vane` |
| `docker-compose.yml`, `.env.example`, `README.md` (config cleanup) | none (no test layer) | Grep-verified: zero remaining `CERTMAGIC_STORAGE_PATH` references | n/a | `grep -rn CERTMAGIC_STORAGE_PATH .` (must return nothing) |
| `CHANGELOG.md` | none | Entry present under `[Unreleased]` | `CHANGELOG.md` | manual review |

**Coverage Expectation values**: taken from `AGENTS.md`'s own testing rules (unit + integration split, disposable-Postgres-only integration gate) — no generic strong default needed, the project already documents this.

## Gate Check Commands

> Generated from `Makefile`/`AGENTS.md`. Confirmed before Execute.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick | After a task touching only unit-testable code (`internal/pglock` unit half, `internal/ratelimit` fake-backed unit half) | `go build ./... && go vet ./... && gofmt -l <changed files> && go test ./...` |
| Full | After any task touching Postgres-backed behavior (advisory locks, rate limiter table, certmagic storage, migrations, poller wiring) | Disposable Postgres container gate (`AGENTS.md` §3): <br>`docker run -d --rm --name vane-test-pg -p 5433:5432 -e POSTGRES_USER=vane -e POSTGRES_PASSWORD=vane -e POSTGRES_DB=vane postgres:16-alpine -c max_connections=300` <br>`TEST_DATABASE_URL="postgres://vane:vane@localhost:5433/vane?sslmode=disable" go test -tags=integration ./...` <br>`docker stop vane-test-pg` |
| Chart | After Helm chart cleanup tasks | `helm lint charts/zeep-vane && helm template charts/zeep-vane --set secrets.databaseUrl=x --set secrets.vaneMasterKey=x --set secrets.vaneSessionSecret=x >/dev/null` |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Foundation - migrations + shared lock primitive

```
T1 → T2
T3
```

Note: T3 (`internal/pglock`) has no data dependency on T1/T2 - it runs third in this phase for cohesion (Foundation groups everything later phases build on), not because it needs the migrations.

### Phase 2: Poller leader election

```
T3 → T4
```

### Phase 3: Cross-replica rate limiter

```
T1 → T5 → T6
```

### Phase 4: CertMagic Postgres storage

```
T2 → T7
T3 → T8
T7 → T8
T7 → T9
T8 → T9
T9 → T10
```

### Phase 5: Chart, config, and docs cleanup

```
T10 → T11 → T12 → T13
```

---

## Task Breakdown

### T1: Add `rate_limit_buckets` migration

**What**: New migration `0017_rate_limit_buckets` (`.up.sql`/`.down.sql`) creating the table per design.md's Data Models section.
**Where**: `internal/db/migrations/0017_rate_limit_buckets.up.sql`, `internal/db/migrations/0017_rate_limit_buckets.down.sql`
**Depends on**: None
**Reuses**: Migration file naming/format convention from `internal/db/migrations/0016_email_providers.up.sql`
**Requirement**: HA-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `.up.sql` creates `rate_limit_buckets (ip TEXT PRIMARY KEY, tokens DOUBLE PRECISION NOT NULL, last_refill TIMESTAMPTZ NOT NULL DEFAULT now())`
- [x] `.down.sql` drops the table
- [x] Full gate passes (migration applies cleanly as part of `db.MigrateUpEmbedded` in any integration test run)

**Tests**: integration (exercised transitively)
**Gate**: Full

**Commit**: `feat(db): add rate_limit_buckets migration`

---

### T2: Add `certmagic_storage` migration

**What**: New migration `0018_certmagic_storage` (`.up.sql`/`.down.sql`) creating the table + prefix index per design.md's Data Models section.
**Where**: `internal/db/migrations/0018_certmagic_storage.up.sql`, `internal/db/migrations/0018_certmagic_storage.down.sql`
**Depends on**: T1 (sequential migration numbering)
**Reuses**: Same migration convention as T1
**Requirement**: HA-13

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `.up.sql` creates `certmagic_storage (key TEXT PRIMARY KEY, value BYTEA NOT NULL, modified_at TIMESTAMPTZ NOT NULL DEFAULT now())` and `certmagic_storage_key_prefix_idx` on `key text_pattern_ops`
- [x] `.down.sql` drops the table (index drops implicitly)
- [x] Full gate passes (migration applies cleanly)

**Tests**: integration (exercised transitively)
**Gate**: Full

**Commit**: `feat(db): add certmagic_storage migration`

---

### T3: Build `internal/pglock` package

**What**: New package implementing `TryAcquire`, `Acquire`, `(*Handle) Healthy`, `(*Handle) Release` exactly per design.md's Components section (dedicated `pgx.Connect` per handle, `pg_try_advisory_lock`/`pg_advisory_lock(hashtextextended(name,0))`/`pg_advisory_unlock`, context-cancellation-aware blocking acquire).
**Where**: `internal/pglock/pglock.go`
**Depends on**: None
**Reuses**: Dedicated-connection pattern from `internal/dbtest/lock.go:117` (not imported - `dbtest` stays test-only; same shape reimplemented here for production use)
**Requirement**: HA-01, HA-02, HA-03, HA-04, HA-05, HA-15, HA-16, HA-17

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `TryAcquire(ctx, dsn string, key int64) (*Handle, bool, error)` returns `(handle, true, nil)` on success, `(nil, false, nil)` when already held, `(nil, false, err)` on connection failure
- [x] `Acquire(ctx, dsn, name string) (*Handle, error)` blocks until acquired or `ctx` is canceled (canceled `ctx` returns promptly, not after the full Postgres wait)
- [x] `(*Handle) Healthy(ctx) bool` returns `false` once the handle's connection is closed/broken
- [x] `(*Handle) Release(ctx) error` unlocks and closes the connection; safe to call once per successful acquire
- [x] Unit test: N/A - no meaningful fake-free branch exists (all behavior requires a real Postgres session); covered by the integration tests below instead, per this task's own Note.
- [x] Integration test: two `TryAcquire` calls for the same `key` against the same `TEST_DATABASE_URL` - second fails while first holds; releasing the first lets the second succeed
- [x] Integration test: `Healthy()` returns `false` after the handle's own connection is closed out-of-band (simulating a crash)
- [x] Integration test: `Acquire`/blocking variant honors context cancellation (canceled context returns an error promptly instead of hanging)
- [x] Full gate passes (disposable Postgres container)

**Note**: `pg_advisory_lock` semantics require a real Postgres connection - there is no meaningful in-memory fake for the acquire/contend/release behavior itself. If no unit-testable branch exists (e.g. input validation), mark unit as N/A in the task's own test list rather than fabricating a trivial unit test; the coverage matrix's "unit + integration" applies over the whole layer, not necessarily every function.

**Tests**: unit (where a meaningful fake-free branch exists) + integration
**Gate**: Full

**Commit**: `feat: add internal/pglock Postgres advisory-lock primitive`

---

### T4: Wire poller leader election into `PollerManager`

**What**: `PollerManager` gains `dsn` (via `NewPollerManager`'s new parameter) and a `runLeaderLoop` goroutine per design.md - `TryAcquire` retry loop (`leaderRetryInterval`), on success calls existing `Restart`, then heartbeats via `Healthy()` each cycle (`leaderHeartbeatInterval`), aborting (`stopLocked`) and returning to the acquire loop on heartbeat failure. `internal/cli/serve.go`'s `RunE` starts this loop instead of (or wrapping) today's unconditional boot-time `Restart` call, and passes `cfg.DatabaseURL` as the new `dsn` argument.
**Where**: `internal/cli/poller_manager.go` (modify), `internal/cli/serve.go` (modify, `RunE` wiring only - merged forward per Tasks resolving-compilation-dependencies rule, since the wiring change alone has no independently testable behavior)
**Depends on**: T3
**Reuses**: `PollerManager.Restart`/`stopLocked` (unchanged), `internal/pglock`
**Requirement**: HA-01, HA-02, HA-03, HA-04, HA-05, HA-06, HA-07

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `pollerLeaderLockKey = 727200001` defined with a doc comment explaining the distinct namespace from `internal/dbtest`'s `727100001`-`727100003`
- [x] Single-replica case (HA-07): one `PollerManager` against a fresh test database acquires the lock immediately and polls exactly as before this feature
- [x] Two-replica case (HA-01, HA-02): two `PollerManager` instances against the same test database - only one runs poll cycles at a time; the other retries acquisition without polling
- [x] Failover case (HA-04): killing/stopping the lock-holding `PollerManager` (or closing its lock connection out-of-band) lets the other acquire the lock and start polling within one `leaderHeartbeatInterval`
- [x] Mid-cycle loss case (HA-05): simulated heartbeat failure aborts the in-flight cycle without completing/writing it
- [x] No new environment variable introduced (HA-06) - confirmed by `grep`
- [x] Full gate passes

**Tests**: integration
**Gate**: Full

**Commit**: `feat: add leader election to PollerManager via internal/pglock`

---

### T5: Rewrite `IPLimiter` internals to Postgres-backed token buckets

**What**: Introduce an internal `bucketStore` interface (`allow(ctx, ip string, burst int, refillPerSec float64) (bool, error)`, `cleanup(ctx, idleTTL time.Duration) error`) with a `postgresBucketStore` implementation running the atomic UPSERT from design.md against `rate_limit_buckets`, and a fake implementation for unit tests. `IPLimiter.allow` delegates to the store; `NewIPLimiter` gains a `*db.Pool` first parameter (used to build the default `postgresBucketStore`). Public `Middleware`/429-body behavior is unchanged.
**Where**: `internal/ratelimit/ip_limiter.go` (modify), `internal/ratelimit/postgres_bucket_store.go` (new)
**Depends on**: T1
**Reuses**: Existing `sweepThreshold`-triggered-cleanup pattern (now against the table instead of the map), `Middleware`/`clientIP`/`rateLimitedBody` (unchanged)
**Requirement**: HA-08, HA-09, HA-10, HA-11, HA-12

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `NewIPLimiter(pool *db.Pool, perMinute, burst int, idleTTL time.Duration) *IPLimiter` compiles and existing burst/idle-eviction unit tests pass against a fake `bucketStore` (no real Postgres needed for these)
- [ ] Unit test (HA-10): fake store returning an error from `allow` causes `Middleware` to let the request through (fail-open) and does not panic
- [ ] Unit test: 429 response body is byte-for-byte identical to before this feature
- [ ] `postgresBucketStore.allow` implements the exact UPSERT from design.md (refill-then-consume, clamped at 0)
- [ ] Full gate passes

**Tests**: unit
**Gate**: Quick

**Commit**: `feat: make IPLimiter's rate-limit state Postgres-backed`

---

### T6: Wire `IPLimiter` call site and add cross-replica integration test

**What**: Update `internal/cli/routes.go:98`'s `ratelimit.NewIPLimiter(...)` call to pass `pool`. Add an integration test proving two `IPLimiter` instances backed by the same test database enforce one shared limit per IP.
**Where**: `internal/cli/routes.go` (modify), `internal/ratelimit/ip_limiter_integration_test.go` (new)
**Depends on**: T5
**Reuses**: `internal/cli/routes_test.go`'s existing test-database wiring conventions
**Requirement**: HA-08, HA-09, HA-11

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `routes.go` compiles with the updated call site; existing `routes_test.go` suite passes unchanged
- [ ] Integration test (HA-08, HA-09): two `IPLimiter` instances, same DB, same IP - hammering one past its burst causes the other to also reject with 429 for that IP; a different IP on the second instance is unaffected
- [ ] Integration test (HA-11): table growth is bounded - after triggering a cleanup cycle, idle rows older than `idleTTL` are gone
- [ ] Full gate passes

**Tests**: integration
**Gate**: Full

**Commit**: `feat: wire cross-replica rate limiting into routes`

---

### T7: Implement `PostgresStorage`'s data methods (Store/Load/Delete/Exists/List/Stat)

**What**: New type `PostgresStorage` in `internal/tls` implementing `certmagic.Storage`'s non-`Locker` methods exactly per design.md (prefix semantics for Delete/Exists/List/Stat, `fs.ErrNotExist` wrapping for missing keys).
**Where**: `internal/tls/postgres_storage.go` (new)
**Depends on**: T2
**Reuses**: `*db.Pool` (existing), `certmagic.KeyInfo`/`certmagic.Storage` (external interface)
**Requirement**: HA-13, HA-14, HA-18

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `Store`/`Load` round-trip identical bytes for a given key
- [ ] `Load` on a missing key returns an error satisfying `errors.Is(err, fs.ErrNotExist)`
- [ ] `Delete` on an exact key removes it; `Delete` on a "directory" prefix removes every key under `prefix/`
- [ ] `Exists` is `true` for both an exact key and a prefix match, `false` otherwise
- [ ] `List(ctx, path, recursive=true)` returns every key under `path/`; `recursive=false` returns only the immediate next segment, deduplicated
- [ ] `Stat` returns correct `KeyInfo` (including `IsTerminal`) for both a file key and a directory-only prefix
- [ ] Full gate passes

**Tests**: integration
**Gate**: Full

**Commit**: `feat: add PostgresStorage data methods for certmagic.Storage`

---

### T8: Implement `PostgresStorage.Lock`/`Unlock`

**What**: `Lock(ctx, name)`/`Unlock(ctx, name)` on `PostgresStorage` using `internal/pglock.Acquire`/`Release`, tracked in a mutex-protected `map[string]*pglock.Handle`.
**Where**: `internal/tls/postgres_storage.go` (modify)
**Depends on**: T3, T7
**Reuses**: `internal/pglock.Acquire`/`Release`
**Requirement**: HA-15, HA-16, HA-17

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `Lock` for a name already locked by another `PostgresStorage` instance (same DB) blocks until `Unlock`
- [ ] `Unlock` releases the handle and removes it from the internal map
- [ ] Integration test (HA-17): killing/closing a lock-holder's underlying connection out-of-band releases the lock, allowing a second `Lock` call for the same name to proceed
- [ ] Full gate passes

**Tests**: integration
**Gate**: Full

**Commit**: `feat: add PostgresStorage Lock/Unlock via internal/pglock`

---

### T9: Update `tls.NewManager` signature and its existing tests

**What**: Change `NewManager(store StatusPageStore, storagePath string)` to `NewManager(store StatusPageStore, storage certmagic.Storage)`, replacing `certmagic.Default.Storage = &certmagic.FileStorage{Path: storagePath}` with `certmagic.Default.Storage = storage`. Update `manager_test.go`/`manager_integration_test.go` to construct and pass a `PostgresStorage` instead of asserting file-path behavior.
**Where**: `internal/tls/manager.go` (modify), `internal/tls/manager_test.go` (modify), `internal/tls/manager_integration_test.go` (modify)
**Depends on**: T7, T8
**Reuses**: `HostPolicy`/`OnEvent` (untouched)
**Requirement**: HA-13

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `NewManager`'s signature matches design.md exactly
- [ ] Every existing assertion in `manager_test.go`/`manager_integration_test.go` that depended on file-path/`FileStorage` behavior is rewritten against `PostgresStorage` with equivalent intent (no test silently deleted or weakened)
- [ ] Full gate passes; test count is equal to or greater than before this task

**Tests**: integration
**Gate**: Full

**Commit**: `refactor: switch tls.NewManager to certmagic.Storage parameter`

---

### T10: Wire `PostgresStorage` into `serve.go`, remove `CERTMAGIC_STORAGE_PATH`

**What**: `newHTTPSServer` gains a `dsn` parameter, builds `tls.NewPostgresStorage(pool, dsn)`, and passes it to `vanetls.NewManager`. Delete the `storagePath`/`defaultCertMagicStoragePath` local variables/constant and the `os.Getenv("CERTMAGIC_STORAGE_PATH")` read. Update `RunE`'s call to `newHTTPSServer` accordingly.
**Where**: `internal/cli/serve.go` (modify)
**Depends on**: T9
**Reuses**: `pool` (already constructed earlier in `RunE`)
**Requirement**: HA-13

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `grep -n CERTMAGIC_STORAGE_PATH internal/` returns nothing
- [ ] `defaultCertMagicStoragePath` constant removed
- [ ] Existing `serve_test.go` integration suite passes unchanged (or updated minimally if it referenced the removed path/env var)
- [ ] Full gate passes

**Tests**: integration
**Gate**: Full

**Commit**: `feat: wire PostgresStorage into serve.go, drop CERTMAGIC_STORAGE_PATH`

---

### T11: Remove `CERTMAGIC_STORAGE_PATH` from config surfaces

**What**: Remove the env var from `docker-compose.yml` (`environment:` entry and the `certmagic:` named volume + its mount under `app.volumes`), `.env.example`, and `README.md`'s Configuration table row.
**Where**: `docker-compose.yml` (modify), `.env.example` (modify), `README.md` (modify)
**Depends on**: T10
**Reuses**: None
**Requirement**: HA-13 (operator-facing cleanup)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `grep -rn CERTMAGIC_STORAGE_PATH .` (repo root) returns nothing
- [ ] `docker-compose.yml`'s top-level `volumes:` no longer declares the now-unused `certmagic` named volume (confirm no other service references it before removing)
- [ ] README's Configuration table has no dangling reference to the removed variable

**Tests**: none
**Gate**: Quick (grep verification, no code to compile)

**Commit**: `docs: remove CERTMAGIC_STORAGE_PATH from compose/env/README`

---

### T12: Remove PVC from Helm chart

**What**: Delete `charts/zeep-vane/templates/pvc.yaml`; remove `values.yaml`'s `persistence.*` block (and its explanatory comment referencing local-disk CertMagic storage); remove `deployment.yaml`'s `CERTMAGIC_STORAGE_PATH` env entry and the CertMagic volume mount/volume declaration (all three `{{- if .Values.persistence.enabled }}` blocks at deployment.yaml:95-96, :104, :129 per current line numbers).
**Where**: `charts/zeep-vane/templates/pvc.yaml` (delete), `charts/zeep-vane/values.yaml` (modify), `charts/zeep-vane/templates/deployment.yaml` (modify)
**Depends on**: T11
**Reuses**: None
**Requirement**: HA-13, HA-18

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `templates/pvc.yaml` no longer exists
- [ ] `values.yaml` has no `persistence:` key
- [ ] `deployment.yaml` has no reference to `.Values.persistence` or `CERTMAGIC_STORAGE_PATH`
- [ ] `helm lint charts/zeep-vane` passes
- [ ] `helm template charts/zeep-vane --set secrets.databaseUrl=x --set secrets.vaneMasterKey=x --set secrets.vaneSessionSecret=x` renders cleanly (both `replicaCount: 1` and `replicaCount: 2` in a `--set` override, to confirm nothing downstream still assumes the PVC)

**Tests**: none
**Gate**: Chart

**Commit**: `feat(chart): remove CertMagic PVC, no longer needed with Postgres storage`

---

### T13: CHANGELOG entry

**What**: Add an `[Unreleased]` entry documenting the HA fix (poller leader election, cross-replica rate limiting, Postgres-backed CertMagic storage, PVC removal).
**Where**: `CHANGELOG.md` (modify)
**Depends on**: T12
**Reuses**: Existing `[Unreleased]` heading, Keep a Changelog format
**Requirement**: N/A (documentation)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `[Unreleased]` gains an `### Added` or `### Changed` bullet (as fits Keep a Changelog conventions) describing the three fixes and the PVC removal, in plain factual language, no invented version number

**Tests**: none
**Gate**: none (manual review)

**Commit**: `docs: changelog entry for ha-multi-replica`

---

## Phase Execution Map

Visual representation of task ordering. Phases run in sequence, and tasks within a phase run in order:

```
Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5

Phase 1:  T1 → T2
Phase 2:  T3 → T4
Phase 3:  T1 → T5 → T6
Phase 4:  T2 → T7 → T8 → T9 → T10
          T3 → T8
          T7 → T9
Phase 5:  T10 → T11 → T12 → T13
```

Execution is strictly sequential - there is no intra-phase parallelism. A single agent (or batch worker) works one task at a time, in order.

13 total tasks - fits within a single ~7-task-per-worker budget doubled (13 ≤ ~2 batches); packed as Phase 1-3 (7 tasks) / Phase 4-5 (6 tasks) if sub-agent delegation is used, or executed inline as one continuous run if the user prefers no sub-agents (offered at Execute per the skill's Sub-Agent Delegation trigger, since 13 > ~8).

---

## Task Granularity Check

| Task | Scope | Status |
| --- | --- | --- |
| T1: `rate_limit_buckets` migration | 1 migration pair | ✅ Granular |
| T2: `certmagic_storage` migration | 1 migration pair | ✅ Granular |
| T3: `internal/pglock` package | 1 package, 4 functions/methods, one cohesive primitive | ✅ Granular (cohesive - splitting `TryAcquire`/`Acquire`/`Healthy`/`Release` into separate tasks would break each other's tests) |
| T4: Poller leader election wiring | 1 behavior (leader loop) across 2 files, merged per compilation-dependency rule | ✅ Granular (merge justified: `serve.go` wiring alone is untestable) |
| T5: `IPLimiter` internals rewrite | 1 component (interface + 2 implementations, cohesive) | ✅ Granular |
| T6: `IPLimiter` call site + cross-replica test | 1 file change + 1 new test file | ✅ Granular |
| T7: `PostgresStorage` data methods | 1 type, 6 methods, one cohesive interface half | ✅ Granular (cohesive - `certmagic.Storage`'s non-Locker methods share one table/semantics) |
| T8: `PostgresStorage` Lock/Unlock | 1 type (same), 2 methods, distinct concern (locking vs data) from T7 | ✅ Granular |
| T9: `NewManager` signature change | 1 function signature + its 2 existing test files | ✅ Granular |
| T10: `serve.go` wiring | 1 file, 1 function modified | ✅ Granular |
| T11: Config surface cleanup | 3 files, same single concern (remove one env var) | ✅ Granular (cohesive - splitting by file would leave the repo in a temporarily inconsistent documented state) |
| T12: Helm chart PVC removal | 3 files, same single concern (remove one feature from the chart) | ✅ Granular (cohesive, same reasoning as T11) |
| T13: CHANGELOG entry | 1 file | ✅ Granular |

---

## Diagram-Definition Cross-Check

| Task | Depends On (task body) | Diagram Shows | Status |
| --- | --- | --- | --- |
| T1 | None | (start of Phase 1) | ✅ Match |
| T2 | T1 | T1 → T2 | ✅ Match |
| T3 | None | T2 → T3 (sequential within phase, no data dependency - T3 doesn't need T2's table) | ✅ Match (intra-phase ordering, not a data dependency - both are valid to run in the file order shown) |
| T4 | T3 | Phase 1 → Phase 2 | ✅ Match |
| T5 | T1 | Phase 1 → Phase 3 | ✅ Match |
| T6 | T5 | T5 → T6 | ✅ Match |
| T7 | T2 | Phase 1 → Phase 4 | ✅ Match |
| T8 | T3, T7 | T7 → T8 (T3 satisfied by Phase 1 already completing) | ✅ Match |
| T9 | T7, T8 | T8 → T9 | ✅ Match |
| T10 | T9 | T9 → T10 | ✅ Match |
| T11 | T10 | Phase 4 → Phase 5, T11 first | ✅ Match |
| T12 | T11 | T11 → T12 | ✅ Match |
| T13 | T12 | T12 → T13 | ✅ Match |

No task depends on a task in a later phase.

---

## Test Co-location Validation

| Task | Code Layer Created/Modified | Matrix Requires | Task Says | Status |
| --- | --- | --- | --- | --- |
| T1: migration | Entity/config (migration) | none (transitively exercised) | integration (exercised transitively) | ✅ OK |
| T2: migration | Entity/config (migration) | none (transitively exercised) | integration (exercised transitively) | ✅ OK |
| T3: `internal/pglock` | Domain primitive | unit + integration | unit (partial) + integration | ✅ OK |
| T4: poller leader election | Domain / service | integration | integration | ✅ OK |
| T5: `IPLimiter` internals | Domain / service | unit | unit | ✅ OK |
| T6: `IPLimiter` wiring | Route wiring + integration test | integration | integration | ✅ OK |
| T7: `PostgresStorage` data methods | Repository / data-access | integration | integration | ✅ OK |
| T8: `PostgresStorage` Lock/Unlock | Repository / data-access | integration | integration | ✅ OK |
| T9: `NewManager` signature | Domain / service (existing tests updated) | integration | integration | ✅ OK |
| T10: `serve.go` wiring | Wiring only | none beyond existing coverage | integration (existing suite) | ✅ OK |
| T11: config cleanup | Config/docs | none | none | ✅ OK |
| T12: chart cleanup | Config (Helm) | none (chart gate only) | none | ✅ OK |
| T13: CHANGELOG | Docs | none | none | ✅ OK |

No violations - no task defers its required tests to a later task.
