# HA Multi-Replica Context

**Gathered:** 2026-08-30
**Spec:** `.specs/features/ha-multi-replica/spec.md`
**Status:** Ready for design

---

## Feature Boundary

Close the three concrete gaps that force `charts/zeep-vane/values.yaml`'s `replicaCount` to default to 1: poller leader election, cross-replica rate limiting, and CertMagic certificate storage — all resolved via Postgres, the project's only mandatory dependency.

---

## Implementation Decisions

### Poller lock-loss behavior

- On losing the advisory lock lease mid-cycle, the replica aborts the in-flight poll cycle immediately — it does not let the current cycle finish and write results.
- Rationale: prevents two replicas both having written `status_intervals` around a fast failover window.

### Rate limiter fail mode

- If the Postgres check errors or times out, the request is allowed through (fail-open), with the failure logged.
- Rationale: Postgres already backs every other part of the app; a limiter-specific outage shouldn't be a second, independent availability failure mode layered on top of an already-degraded instance.

### CertMagic storage caching

- No local-disk cache layer — every CertMagic storage operation reads/writes Postgres directly.
- Rationale: eliminates the `ReadWriteOnce` PVC entirely (a win even for `replicaCount: 1`); ACME/cert traffic volume for a self-hosted install is low enough that per-operation Postgres round-trips are not a bottleneck.

### Spec grouping

- One feature (`ha-multi-replica`) with three P1 stories, not three separate feature specs.
- Rationale: all three problems share the same trigger (running N replicas) and the same solution shape (Postgres-backed coordination instead of new infra) — validating them together as one coherent delivery is more useful than three disjoint spec/design/tasks/validation cycles for work that ships together anyway.

### Agent's Discretion

- Poller advisory-lock lease duration (30s, renewed each successful cycle) — no objection raised when presented as the spec default.
- Rate limiter storage shape (single `rate_limit_buckets` table, token-bucket re-implemented server-side to match existing `golang.org/x/time/rate` semantics) — no objection raised.
- CertMagic storage schema (single `certmagic_storage` key/value table implementing the full `certmagic.Storage` interface, `Lock`/`Unlock` via `pg_advisory_lock`) — no objection raised.
- Exact advisory lock key namespace/hashing scheme to avoid collision with existing locks (`LockDatadogIntegration`, `LockAdminsTable`, `LockCompanySettings`) — left to Design.

### Declined / Undiscussed Gray Areas → Assumptions

None declined — all four gray areas presented were answered directly. The three "agent's discretion" items above were not separately asked (they're implementation-shape details, not product decisions) and are recorded as assumptions in spec.md's Assumptions & Open Questions table.

---

## Specific References

None — this feature is an internal reliability/scaling fix with no user-facing surface; no product reference points were given.

---

## Deferred Ideas

- Raising the chart's `replicaCount` default itself, and Deployment `strategy` tuning for N>1 rollouts — explicitly out of scope in spec.md, left for a follow-up decision once this feature ships.
