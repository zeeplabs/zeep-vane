# Rate Limiter Store Fallback Design

**Spec**: `.specs/features/ratelimit-store-fallback/spec.md`
**Supersedes (policy)**: `HA-10` in `.specs/features/ha-multi-replica/` — the historical spec is left unchanged; the superseding decision is recorded as `AD-029` in `.specs/STATE.md`.

## Decision Summary

`IPLimiter` keeps a `bucketStore` primary (Postgres, shared across replicas) and gains a `fallback` (`memoryBucketStore`, per process). On a primary error the request is evaluated by the fallback rather than allowed unconditionally. A small circuit breaker (`breakerCooldown = 5s`) quarantines a failing primary so it is called at most once per cooldown, with a single half-open probe. The only unconditional-allow path left is the double-failure last resort, logged and documented as such. Per-replica fallback state (effective ~N× limit while degraded) and a transient ~2× window at the error boundary are accepted and documented.

## Architecture

Two stores behind the existing `bucketStore` interface (`allow`, `cleanup`) — no interface change:

| Component | File | Role |
| --- | --- | --- |
| `postgresBucketStore` | `internal/ratelimit/postgres_bucket_store.go` (unchanged) | Primary; shared token-bucket state in `rate_limit_buckets`. |
| `memoryBucketStore` (new) | `internal/ratelimit/memory_bucket_store.go` | Fallback; per-process map guarded by `sync.Mutex`, same refill-then-consume formula, same idle `cleanup`. |

`IPLimiter` changes:

| Field | Type | Meaning |
| --- | --- | --- |
| `store` | `bucketStore` | Primary (unchanged field). |
| `fallback` | `bucketStore` | New; selected on primary error / open circuit. |
| `breakerOpenUntil` | `time.Time` | Zero = closed; when `now < breakerOpenUntil` the circuit is open. |
| `breakerProbing` | `bool` | True while the single half-open probe is in flight. |

`breakerCooldown` is a new package constant (`5 * time.Second`). Production `NewIPLimiter(pool, perMinute, burst, idleTTL)` builds `primary = newPostgresBucketStore(pool)` and `fallback = newMemoryBucketStore()` and passes both. The internal test constructor becomes `newIPLimiterWithStore(primary, fallback bucketStore, perMinute, burst, idleTTL)`.

`memoryBucketStore` mirrors the fake currently used only in `ip_limiter_test.go` (map of `{tokens, lastRefill}` under a mutex, brand-new bucket starts full, refill `perMinute/60` per second, clamp `[0, burst]`, refill-then-consume) but is production code — the test fake is not imported by production. `x/time/rate` is deliberately **not** used (see spec Out of Scope).

## Circuit-Breaker State Machine

State is decided and updated under the existing `l.mu`; the actual `allow` call happens **outside** the lock (as today) so the store round-trip never serializes all requests.

| State | Condition | `allow` routing | Transition out |
| --- | --- | --- | --- |
| Closed | `breakerOpenUntil` is zero (or in the past) and `!breakerProbing` | Primary | Primary error → Open |
| Open | `now < breakerOpenUntil` | Fallback | Cooldown elapse → Half-open |
| Half-open (probe in flight) | `now >= breakerOpenUntil` and `breakerProbing` | Fallback | — |
| Half-open (probe starts) | `now >= breakerOpenUntil` and `!breakerProbing` | Primary (mark `breakerProbing = true`) | Probe success → Closed; probe failure → Open |

`allow` sequence:

1. Lock `l.mu`. Increment `callCount`; decide whether this call is the sweep call. Decide routing (primary / half-open-probe / fallback) per the table, setting `breakerProbing` when starting a probe. Capture the chosen store. Unlock.
2. If this is the sweep call, best-effort `cleanup(ctx, idleTTL)` on the chosen store (log and skip on error, as today).
3. If routing is fallback: call `fallback.allow`. On error → log and return `true` (last resort, RLF-03). On success → return its verdict.
4. If routing is primary: call `store.allow`.
   - Success → lock; clear `breakerOpenUntil` and `breakerProbing`; unlock; return the verdict.
   - Error → lock; set `breakerOpenUntil = now.Add(breakerCooldown)`, clear `breakerProbing`; unlock; log; then call `fallback.allow` (same error handling as step 3) and return its verdict.

Concurrency notes:

- Closed state: concurrent calls all route to the primary (no `breakerProbing` set), preserving current concurrency.
- Half-open: the `breakerProbing` check under the lock guarantees exactly one probe; others observe `breakerProbing == true` and take the fallback.
- The probe flag is cleared in both success and failure transitions, so a crashed/panicking probe path cannot wedge the circuit (the store call is context-bounded; `allow` returns).

## Error Handling

| Situation | Behavior | Rationale |
| --- | --- | --- |
| Primary `allow` error (any cause) | Open circuit; request goes to fallback | Uniform handling; all causes mean "no shared decision" (spec assumption). |
| Fallback `allow` error | Log; allow the request | Last resort only; the in-memory store has no expected failure mode. |
| Primary error log | Keep the existing `ratelimit: store error...` log; add a distinct "using in-memory fallback" line | Retains the operator's signal that the shared store failed. |
| Probe success | Clear breaker; log recovery once | Operators can see the store came back. |
| Cleanup error | Log and skip (unchanged) | Best-effort; never on the request path. |
| `clientIP` / middleware | Unchanged | No routing change. |

## Limits and Accepted Trade-offs

- **Per-replica fallback**: while degraded, the effective limit is ~N× with N replicas (each replica has its own fallback buckets). The shared-store path remains exact. Accepted (user decision).
- **Transient ~2× at the boundary**: primary and fallback buckets are independent, so an IP can briefly consume a fallback burst in addition to its primary burst. Bounded, never unbounded.
- **Cooldown recovery delay**: a store that recovers mid-cooldown is not used until the next probe; up to ~5s of fallback-only limiting. Accepted to avoid re-flooding.
- **Shared breaker across IPs**: one IP's lock-timeout opens the circuit for all IPs that replica until the probe. Accepted vs. per-IP breaker state (complexity).
- **No metrics**: only logs; consistent with the rest of the project.

## Testing Strategy

Backend-only, no frontend, no schema. Per `AGENTS.md` §3 the limiter is unit-testable with fakes; the existing integration test (`ip_limiter_integration_test.go`) covers the real Postgres path.

| Layer | Type | Cases |
| --- | --- | --- |
| `memoryBucketStore` | unit | Brand-new bucket full; refill; burst clamp; floor at 0; idle `cleanup` evicts only stale entries. Parity with the primary formula. |
| `IPLimiter` fallback | unit | Store error → fallback enforced (burst 1: 1st pass, 2nd `429`); fallback denial → `429` with identical body; fallback error → last-resort allow + log. |
| `IPLimiter` breaker | unit | Error opens circuit; requests within cooldown do not call the primary (spy count); single half-open probe with two concurrent callers (exactly one reaches the spy); probe success closes and uses primary again; probe failure re-opens. Tests manipulate `breakerOpenUntil` in-package instead of sleeping. |
| Sweep | unit | In-use store (primary path and fallback path) is the one swept at `sweepThreshold`. |
| Real store | integration | With a real `db.Pool` forced into error (context canceled / pool closed), `NewIPLimiter` falls back and still returns `429` after the burst, then recovers once the pool is usable again. |
| Regression | unit | The rewritten `HA-10` test now asserts fallback enforcement, not unconditional allow. |

Discrimination sensor (per the project's Execute flow): revert the fallback branch to `return true` and confirm the new tests fail; revert the breaker skip and confirm the spy-count test fails; revert the single-probe gate and confirm the concurrent-probe test fails.

## Risks

- **Behavioral change in a security control**: this *strengthens* the limiter (removes an attacker-inducible bypass), but it can 429 a legitimate client during a store outage that the old policy would have let through. This is the intended, user-approved trade-off; the fallback keeps the limit per-process, so a genuine outage does not remove protection.
- **Test coupling to time**: the breaker uses `time.Now()`. Tests set `breakerOpenUntil`/`breakerProbing` directly (same package) rather than sleeping, so there is no wall-clock flake.
- **Interface drift**: `memoryBucketStore` and `postgresBucketStore` must keep the same formula; a parity unit test guards this (RLF-09).

## Files Touched

- `internal/ratelimit/memory_bucket_store.go` (new) + `memory_bucket_store_test.go` (new)
- `internal/ratelimit/ip_limiter.go` (fallback + breaker + `breakerCooldown`, constructor signature)
- `internal/ratelimit/ip_limiter_test.go` (rewrite `HA-10` test; new fallback/breaker tests)
- `internal/ratelimit/ip_limiter_integration_test.go` (real-store fallback + recovery)
- `.specs/STATE.md` (`AD-029`)
