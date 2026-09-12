# Rate Limiter Store Fallback Tasks

## Execution Protocol (MANDATORY -- do not skip)

Implement these tasks with the `tlc-spec-driven` skill: **activate it by name and follow its Execute flow and Critical Rules.** Do not search for skill files by filesystem path. The skill is the source of truth for the full flow (per-task cycle, sub-agent delegation, adequacy review, Verifier, discrimination sensor).

**If the skill cannot be activated, STOP and tell the user - do not proceed without it.**

---

**Design**: `.specs/features/ratelimit-store-fallback/design.md`
**Spec**: `.specs/features/ratelimit-store-fallback/spec.md`
**Status**: Approved (2026-09-12).

---

## Test Coverage Matrix

> Generated from codebase (`AGENTS.md` §3, sampled `internal/ratelimit/ip_limiter_test.go`, `postgres_bucket_store_integration_test.go`) and spec ACs - confirm before Execute. No schema, no frontend, no new config. The package is pure Go with fakes; one integration test exercises the real Postgres store.

| Code Layer | Required Test Type | Coverage Expectation | Location Pattern | Run Command |
| --- | --- | --- | --- | --- |
| `memoryBucketStore` (pure in-memory store) | unit | Every branch of refill-then-consume; clamp floor/ceiling; brand-new bucket full; idle `cleanup`; formula parity with the primary | `internal/ratelimit/memory_bucket_store_test.go` | `go test ./internal/ratelimit/...` |
| `IPLimiter` fallback + breaker (orchestration) | unit | 1:1 to RLF-01..RLF-07; last-resort path; single-probe concurrency (spy store call count); sweep chooses the in-use store | `internal/ratelimit/ip_limiter_test.go` | `go test ./internal/ratelimit/...` |
| `NewIPLimiter` against real Postgres | integration | Store forced into error → fallback enforces `429`; recovery → primary used again | `internal/ratelimit/ip_limiter_integration_test.go` (`//go:build integration`) | `TEST_DATABASE_URL=... go test -tags=integration ./internal/ratelimit/...` |
| Decision record | none | Build/format only | `.specs/STATE.md` | `go build ./...` |

## Gate Check Commands

> Generated from `AGENTS.md` §3 - confirm before Execute.

| Gate Level | When to Use | Command |
| --- | --- | --- |
| Quick (Go, no DB) | After T1, T2, T3 | `go build ./... && go vet ./... && gofmt -l <changed files> && go test ./internal/ratelimit/...` |
| Full (Go, DB-touching) | After T4 | Spin disposable Postgres per `AGENTS.md` §3, then `TEST_DATABASE_URL=... go test -tags=integration -count=1 -p 1 ./internal/ratelimit/...`, then destroy the container |
| Build (phase completion) | End of every phase | Quick + Full, both green |

---

## Execution Plan

Phases are ordered and run sequentially - each phase completes before the next begins, and tasks within a phase execute in order.

### Phase 1: Fallback store

```
T1
```

### Phase 2: Limiter wiring and circuit breaker

```
T1 → T2
T2 → T3
```

### Phase 3: Real-store verification

```
T2 → T4
```

### Phase 4: Decision record

```
T2 → T5
```

---

## Task Breakdown

### T1: `memoryBucketStore`

**What**: New production in-memory `bucketStore` (map + `sync.Mutex`) implementing the exact refill-then-consume formula `postgresBucketStore` uses, plus idle `cleanup`.
**Where**: `internal/ratelimit/memory_bucket_store.go`
**Depends on**: None
**Reuses**: The token-bucket math from `postgresBucketStore` (`internal/ratelimit/postgres_bucket_store.go`) and the `bucketStore` interface.
**Requirement**: RLF-09, RLF-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] `allow` matches the primary exactly: brand-new bucket starts full, refill `refillPerSec`/s, clamp `[0, burst]`, refill-then-consume
- [x] `cleanup` evicts only buckets idle longer than `idleTTL`, under the mutex
- [x] Concurrency-safe (mutex held for the whole read-modify-write)
- [x] Formula-parity unit test against the primary's documented semantics
- [x] Gate check passes: `go build ./... && go vet ./... && gofmt -l internal/ratelimit/memory_bucket_store.go && go test ./internal/ratelimit/...`
- [x] Test count: ≥5 new subtests

**Tests**: unit
**Gate**: quick

**Commit**: `feat(ratelimit): add in-memory bucket store fallback`

---

### T2: `IPLimiter` fallback and circuit breaker

**What**: Add the `fallback bucketStore` field and the breaker (`breakerOpenUntil`, `breakerProbing`, `breakerCooldown`); route to the fallback on primary error per `design.md`'s state machine; build both stores in `NewIPLimiter`; change the internal constructor signature.
**Where**: `internal/ratelimit/ip_limiter.go`
**Depends on**: T1
**Reuses**: Existing `l.mu`, `bucketStore` interface, `rateLimitedBody`/`Middleware`.
**Requirement**: RLF-01, RLF-02, RLF-03, RLF-04, RLF-05, RLF-06, RLF-07

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [x] Primary error ⇒ fallback evaluated; fallback denial ⇒ `429` with the existing body/headers
- [x] Circuit opens on primary error; requests within `breakerCooldown` do not call the primary
- [x] After cooldown, exactly one probe reaches the primary; concurrent callers use the fallback
- [x] Probe success closes the circuit; probe failure re-opens it
- [x] Fallback error ⇒ last-resort allow + log (RLF-03)
- [x] `TestIPLimiter_StoreError_FailsOpen` rewritten to assert fallback enforcement (was unconditional allow)
- [x] Spy-store tests assert primary call counts for open/half-open/recovery
- [x] Gate check passes: `go build ./... && go vet ./... && gofmt -l internal/ratelimit/ip_limiter.go && go test ./internal/ratelimit/...`
- [x] Test count: ≥7 new/rewritten tests

**Tests**: unit
**Gate**: quick

**Commit**: `feat(ratelimit): fall back to in-memory limiter with circuit breaker`

---

### T3: Sweep the in-use store

**What**: Add tests proving the `sweepThreshold` cleanup runs against the store in use (primary or fallback). The in-use selection itself landed with T2's routing (RLF-08); this task pins it.
**Where**: `internal/ratelimit/ip_limiter_test.go`
**Depends on**: T2
**Reuses**: Existing `callCount`/`sweepThreshold`/`idleTTL` mechanics and `spyBucketStore`.
**Requirement**: RLF-08

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] Crossing `sweepThreshold` on the fallback path sweeps the fallback store (stale entry evicted)
- [ ] Crossing `sweepThreshold` on the primary path sweeps the primary store (behavior unchanged)
- [ ] Cleanup error stays best-effort (logged, never on the request path)
- [ ] Gate check passes: `go test ./internal/ratelimit/...`
- [ ] Test count: ≥2 new subtests

**Tests**: unit
**Gate**: quick

**Commit**: `test(ratelimit): cover sweep targeting the in-use store`

---

### T4: Real-store fallback and recovery

**What**: Integration test proving the fallback engages against a real `db.Pool` forced into error and that recovery resumes the primary.
**Where**: `internal/ratelimit/ip_limiter_integration_test.go`
**Depends on**: T2
**Reuses**: Existing disposable-Postgres integration harness in the package.
**Requirement**: RLF-01, RLF-06

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] With the pool unusable (canceled context / closed pool), burst `1` yields pass-then-`429` via the fallback
- [ ] After the pool is usable again and the cooldown elapses, the primary store is used (asserted via `rate_limit_buckets` row state or a spy)
- [ ] Gate check passes on disposable Postgres: `TEST_DATABASE_URL=... go test -tags=integration -count=1 -p 1 ./internal/ratelimit/...`
- [ ] Test count: ≥2 new integration tests

**Tests**: integration
**Gate**: full

**Commit**: `test(ratelimit): cover fallback and recovery against Postgres`

---

### T5: Decision record

**What**: Add `AD-029` to `.specs/STATE.md` superseding `HA-10`, with the *why*, the trade-off (per-replica effective limit, transient ~2×, cooldown), and status; reference this spec.
**Where**: `.specs/STATE.md`
**Depends on**: T2
**Reuses**: `AD-NNN` entry format.
**Requirement**: (traceability)

**Tools**:
- MCP: NONE
- Skill: NONE

**Done when**:
- [ ] `AD-029` entry recorded with decision, reason, trade-off, scope, date, status
- [ ] The historical `ha-multi-replica/spec.md` is left unchanged
- [ ] Gate check passes: `go build ./...`

**Tests**: none
**Gate**: build

**Commit**: `docs(state): record AD-029 rate limiter store fallback`
