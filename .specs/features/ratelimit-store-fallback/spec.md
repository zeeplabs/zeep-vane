# Rate Limiter Store Fallback Specification

**Scope: Small.** One new pure in-memory store, one behavioral change to `IPLimiter.allow` (fallback + circuit breaker), and tests. No schema change, no new config surface, no new dependency. It closes a known, attacker-inducible weakness in the current fail-open policy (`HA-10`) and records a superseding decision in `.specs/STATE.md`.

## Problem Statement

`internal/ratelimit.IPLimiter` guards the credential-sensitive routes (login, password-reset, invite-accept, signup) with a per-IP token bucket whose state lives in Postgres (`rate_limit_buckets`, shared across replicas per `AD-013`). On any store error — Postgres unreachable, or the `SET LOCAL lock_timeout = '3s'` wait expiring under row-lock contention — `allow` returns `true` unconditionally (fail-open, `HA-10`, `internal/ratelimit/ip_limiter.go:98-104`). Because the lock the request waits on is the *attacker's own* IP row, a single client can deliberately saturate its bucket and drive the wait past the 3s timeout, converting the limiter into an unlimited path for exactly the traffic it exists to bound. The original `HA-10` rationale ("a limiter outage shouldn't add an availability failure on top of an already-degraded instance") holds for a genuine Postgres outage, but it does not justify a bypass that the protected client can induce at will. This spec replaces unconditional fail-open with a per-process in-memory fallback that keeps enforcing the *same* rule, plus a circuit breaker so a failing store is not hammered once per request.

## Goals

- [ ] When the Postgres store errors, the request is evaluated against an in-memory token bucket with identical parameters and semantics — never allowed unconditionally.
- [ ] A failing store is quarantined for a short cooldown; during it, requests skip the store entirely and use the fallback, so a down/slow Postgres does not cost one failed round-trip per credential request.
- [ ] Exactly one probe request tests the store after the cooldown (half-open); success resumes the shared store, failure re-arms the cooldown. No thundering herd on recovery.
- [ ] The fallback's memory is bounded the same way the store's is (idle sweep), so an address-cycling attacker cannot grow it forever.
- [ ] The only remaining unconditional-allow path is the impossible last resort where *both* the store and the in-memory fallback error — logged, and explicitly documented as last-resort rather than policy.
- [ ] The change is recorded as a new `AD-NNN` that supersedes `HA-10`'s fail-open policy without rewriting the historical `ha-multi-replica` spec.

## Out of Scope

| Feature | Reason |
| --- | --- |
| Distributed / shared fallback state | A shared fallback would have to live in Postgres, which is the dependency that just failed. Per-replica fallback is the accepted, documented cost (effective limit ~N× with N replicas while degraded). |
| Migrating fallback bucket state back into Postgres on recovery | The shared bucket and the fallback bucket are independent by design; reconciling them is complexity with no security value (it would only tighten a transient ~2× window back to 1×). |
| Changing the configured limit (`10/min`, burst `10`, idle TTL `10min`) | This spec changes *what happens on store failure*, not the limit itself. |
| Metrics / alerting / a health endpoint for breaker state | No metrics backend exists in this project; the transition is logged like every other limiter event. Adding observability is a separate concern. |
| Any change to the other `AD-013` HA mechanisms (poller leader election, CertMagic storage) | Unrelated; each degrades per its own fail mode. |
| A `golang.org/x/time/rate` dependency | The fallback re-implements the exact refill-then-consume formula `postgresBucketStore` already uses, so primary and fallback semantics stay byte-for-byte identical; adding the dependency (currently indirect only) would introduce a second definition to keep in sync. |

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Fallback mechanism | A per-process in-memory `bucketStore` (`memoryBucketStore`, map + `sync.Mutex`) with the same refill-then-consume math as `postgresBucketStore` | Confirmed with the user (2026-09-12): keep enforcing a real limit under store failure instead of fail-open. Same math, not `x/time/rate`, so the two stores cannot drift. | y (user) |
| Circuit-breaker inclusion | Included, not deferred | Confirmed with the user (2026-09-12): without it, a down Postgres pays one failed `Begin`/transaction per credential request; the cooldown is cheap and bounded. | y (user) |
| Cooldown value | `breakerCooldown = 5 * time.Second`, a package constant | Close to the existing `lock_timeout` of 3s (the shortest error this masks) and small enough that recovery is prompt; a constant, matching the package's existing tuned-constant convention (no new env var). | y — agent default, no objection raised |
| Half-open probe concurrency | At most one in-flight probe; every other request keeps using the fallback until the probe resolves | Prevents a herd of probes at cooldown expiry (the same surge that would re-overload a recovering store). Implemented with a `breakerProbing` flag under the existing `l.mu`. | y — agent default, no objection raised |
| Error classification | Every `allow` error from the store is treated identically (open the circuit); no distinction between `Begin`, lock-timeout, seed, select, update, or commit failures | All of them mean "the shared limiter could not make a decision"; the fallback is the correct response for all, and distinguishing them adds branching that is hard to test and easy to get wrong. | y — agent default, no objection raised |
| Where the fallback lives | A second `bucketStore` field on `IPLimiter`, selected inside `allow` | Reuses the existing interface exactly; the Postgres store, the test fake, and the new memory store are interchangeable, and no caller-facing signature changes in production. | y — agent default, no objection raised |
| Cleanup during fallback | The periodic idle sweep runs against whichever store handled the request (primary or fallback), on the existing `sweepThreshold` cadence | The fallback map needs the same bounding the Postgres table does; reusing the existing sweep trigger avoids a second timer/goroutine. | y — agent default, no objection raised |
| Recovery after the last-resort path | Not attempted within a request; the breaker's half-open probe is the only recovery mechanism | Keeps a single, testable recovery path. | y — agent default, no objection raised |
| Process / artifacts | A short feature spec under `.specs/features/ratelimit-store-fallback/` + a superseding `AD-NNN` in `STATE.md`; the historical `ha-multi-replica/spec.md` is left unchanged | `AGENTS.md` §1/§6: architectural decisions are logged as `AD-NNN` with the *why*; rewriting a shipped spec's history is not the convention. | y — agent default, no objection raised |

**Open questions:** none — all resolved or logged above.

## User Stories

### P1: Requests stay limited when the shared store fails ⭐ MVP

**User Story**: As the operator of an instance under attack, I want the credential-route limiter to keep enforcing its rule even when the Postgres-backed store errors, so a client cannot bypass the limit by inducing store failures.

**Why P1**: This is the security fix; without it the limiter is decorative against an attacker who understands the fail-open path.

**Acceptance Criteria**:

1. WHEN the primary (Postgres) bucket store returns an error for a request THEN the system SHALL evaluate that request against the per-process in-memory fallback store with the same per-minute rate, burst, and idle TTL, instead of allowing it unconditionally. <!-- event-driven -->
2. WHEN the in-memory fallback denies a request THEN the system SHALL respond `429` with the same status, headers, and body as a primary-store denial. <!-- event-driven -->
3. The in-memory fallback store SHALL implement the same refill-then-consume semantics as the primary store: a brand-new bucket starts full, refill is `perMinute/60` tokens per second, and tokens are clamped to `[0, burst]`. <!-- ubiquitous -->
4. IF the in-memory fallback itself returns an error THEN the system SHALL allow the request (last-resort fail-open, preserving pre-change behavior for a case that should not occur) and SHALL log it. <!-- unwanted-behavior -->

**Independent Test**: Configure a limiter with a store that always errors and burst `1`; the first request must pass and the second must return `429` (fallback enforced), not `200`. Separately, configure a fallback that also errors and confirm the request passes and a log line is emitted.

### P1: A failing store is quarantined behind a circuit breaker ⭐ MVP

**User Story**: As the operator of an instance whose Postgres is degraded or down, I want the limiter to stop hammering the failing store on every request, so the store's recovery is not delayed and credential requests do not each pay a failed round-trip.

**Why P1**: Without quarantine, the fallback still calls the Postgres store first on every request; under a real outage that is one failing `Begin` per credential request, exactly the kind of self-inflicted load a breaker exists to prevent.

**Acceptance Criteria**:

1. WHEN the primary store returns an error THEN the system SHALL open the circuit for `breakerCooldown` (`5s`) and SHALL route subsequent requests to the fallback without calling the primary store for the duration. <!-- state-driven -->
2. WHEN the cooldown has elapsed THEN the system SHALL let exactly one in-flight request probe the primary store again (half-open), and SHALL keep routing every other concurrent request to the fallback until that probe resolves. <!-- state-driven -->
3. WHEN the half-open probe succeeds THEN the system SHALL close the circuit and resume routing requests through the primary store. <!-- event-driven -->
4. WHEN the half-open probe fails THEN the system SHALL re-open the circuit for another cooldown. <!-- event-driven -->

**Independent Test**: With a spy store that counts calls, force one error, then issue several requests within the cooldown and assert the spy was called exactly once (the original failing call). Advance past the cooldown (manipulate `breakerOpenUntil` directly in-package) and issue two concurrent requests, asserting exactly one reaches the spy; make the spy succeed and assert subsequent requests all use it again.

### P2: The fallback's memory stays bounded

**User Story**: As the operator, I want the in-memory fallback to evict idle buckets, so an attacker cycling source addresses during a store outage cannot grow it without bound.

**Why P2**: The fallback only exists during degradation, but "degradation" is exactly when an address-cycling flood is most likely — unbounded growth during the incident would be a self-inflicted OOM.

**Acceptance Criteria**:

1. WHEN the `allow` call counter crosses `sweepThreshold` THEN the system SHALL run the idle sweep against the store that is handling the request (primary or fallback), evicting buckets whose `lastRefill` is older than the idle TTL. <!-- event-driven -->

**Independent Test**: Drive a fallback sweep with a stale entry and assert it is evicted, mirroring the existing primary-store sweep test.

## Edge Cases

- WHEN the store has never errored THEN the circuit is closed and behavior is byte-for-byte the pre-change behavior (single store, same math).
- WHEN the store recovers on its own before the cooldown elapses THEN the limiter keeps using the fallback until the single half-open probe confirms recovery — a deliberate delay in exchange for not re-flooding the store.
- WHEN the store is "up" but slow (lock-timeout on one IP) THEN the circuit opens for that brief interval even though other IPs' rows are uncontended; those other IPs are limited by the per-replica fallback until the probe closes the circuit. Accepted: the alternative (per-IP circuit state) multiplies breaker state and complexity for a transient window.
- WHEN the fallback bucket and the shared bucket both hold state for the same IP THEN they evolve independently; crossing the error boundary can grant at most one extra burst (~2× momentarily), never an unbounded path.
- IF the fallback is constructed with the same parameters as the primary THEN no configuration divergence is possible — both derive from the single `NewIPLimiter(pool, perMinute, burst, idleTTL)` call site.

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| RLF-01 | P1: Fallback on store error | T2, T4 | Verified |
| RLF-02 | P1: Fallback denial returns 429 | T2 | Verified |
| RLF-03 | P1: Last-resort fail-open if fallback errors | T2 | Verified |
| RLF-04 | P1: Circuit opens on store error | T2 | Verified |
| RLF-05 | P1: Single half-open probe | T2 | Verified |
| RLF-06 | P1: Probe success closes circuit | T2 | Verified |
| RLF-07 | P1: Probe failure re-opens circuit | T2 | Verified |
| RLF-08 | P2: Sweep applies to the in-use store | T1, T3 | Verified |
| RLF-09 | P1: Fallback token-bucket parity | T1 | Verified |

**Coverage:** 9 total, 9 mapped to tasks, 0 unmapped

## Success Criteria

- [ ] A request is never allowed unconditionally because the Postgres store errored; only the documented double-failure last resort allows it.
- [ ] A failing store is called at most once per `breakerCooldown`, with a single half-open probe on expiry.
- [ ] Fallback and primary stores implement the same token-bucket formula from one set of parameters, guarded by a parity test.
- [ ] The fallback's in-memory map is idle-swept on the same cadence as the Postgres table.
- [ ] `ha-multi-replica`'s historical spec is untouched; the new behavior is recorded as a superseding `AD-NNN` in `.specs/STATE.md`.
