# HA Multi-Replica Specification

## Problem Statement

The Helm chart (`charts/zeep-vane/`) defaults to `replicaCount: 1` because three parts of the codebase are single-process assumptions: the poller has no leader election (N replicas would poll Datadog and write `status_intervals` N times concurrently), the per-IP rate limiter (`internal/ratelimit`) is an in-memory map with no cross-process state (protection against login/reset/invite brute force weakens by a factor of N behind a LoadBalancer), and CertMagic's certificate storage (`internal/tls/manager.go`) writes to local disk (a `ReadWriteOnce` PVC only one pod can mount, blocking real horizontal scaling and risking duplicate/racing ACME issuance if worked around). This blocks safely running more than 1 Kubernetes replica.

## Goals

- [ ] The poller runs on at most one replica at a time, self-electing without operator configuration.
- [ ] The per-IP rate limiter enforces the same limit regardless of how many replicas receive the client's requests.
- [ ] CertMagic's certificate state is readable and writable by every replica with no local-disk dependency, and the `ReadWriteOnce` PVC is removed from the Helm chart.
- [ ] All three fixes depend only on Postgres (already the project's sole mandatory dependency) — no new infrastructure (Redis, etcd, NFS, object storage).

## Out of Scope

| Feature | Reason |
| --- | --- |
| Raising `replicaCount` default in `values.yaml` | Separate decision once this feature ships and is validated; not required to close the 3 code gaps. |
| Rolling-update strategy tuning in the Deployment template | Chart-level concern, independent of the code fixes here. |
| Distributed tracing / cross-replica observability of poller elections | Nice-to-have, not required for correctness. |
| Rate limiting anything other than the 4 existing unauthenticated routes (login, password-reset, invite-accept, bootstrap) | No new routes are in scope; the mechanism just becomes cross-process for the existing ones. |
| Migrating away from CertMagic itself | Analyzed and rejected earlier (`AD-`-pending discussion, see project vault) — CertMagic still solves runtime-registered-domain TLS that static ingress/cert-manager cannot. Only its storage backend changes. |

---

## Assumptions & Open Questions

| Assumption / decision | Chosen default | Rationale | Confirmed? |
| --- | --- | --- | --- |
| Poller lock-loss behavior | Abort the in-flight poll cycle and stop polling; another replica may pick up the advisory lock | Prevents two replicas writing `status_intervals` after a lease handoff | y |
| Rate limiter fail mode on Postgres error/timeout | Fail-open (allow the request) | Postgres already backs the whole app; a limiter outage souldn't add an extra availability failure mode on top of an already-degraded instance | y |
| CertMagic storage caching | No local disk cache — Postgres is the sole store, read on every access | Eliminates the PVC entirely (benefits single-replica too); cert/ACME traffic volume per self-hosted install is low, not a bottleneck | y |
| Spec grouping | One feature, three P1 stories | All three fixes share the same trigger (N replicas) and the same solution shape (Postgres-backed coordination) | y |
| Poller lock lease duration | 30s lease, renewed every successful poll cycle (default poll interval is 60s+, per `POLL_INTERVAL_SECONDS`) | Short enough that a crashed replica's lock is reclaimed quickly; long enough to survive one slow Datadog call without churn | n — agent default, no objection raised |
| Rate limiter storage shape | Single Postgres table `rate_limit_buckets` (key = client IP + route family, columns for token count + last refill timestamp), token-bucket algorithm re-implemented server-side (matches existing `golang.org/x/time/rate` semantics: `perMinute`/`burst`) instead of a generic sliding-window log | Keeps the exact same limiter semantics already in production (`internal/ratelimit`), least behavior change; a table row per (IP, route family) is bounded and prunable the same way the in-memory map already evicts idle entries | n — agent default, no objection raised |
| CertMagic storage schema | Single Postgres table `certmagic_storage` (key TEXT PRIMARY KEY, value BYTEA, modified_at TIMESTAMPTZ) implementing the full `certmagic.Storage` interface (Store/Load/Delete/Exists/List/Stat/Lock/Unlock) | `certmagic.Storage` is a well-defined interface; a flat key-value table is the direct Postgres analog of `certmagic.FileStorage`'s path-based layout, and `Lock`/`Unlock` map onto `pg_advisory_lock` the same way the poller's own leader election does | n — agent default, no objection raised |

**Open questions:** none — all resolved or logged above.

---

## User Stories

### P1: Poller runs on exactly one replica ⭐ MVP

**User Story**: As an operator running Vane with multiple Kubernetes replicas, I want only one replica to poll Datadog and write status intervals at a time, so that scaling out doesn't multiply API calls to Datadog or corrupt `status_intervals` with concurrent writes.

**Why P1**: Without this, `replicaCount > 1` produces N pollers hitting Datadog and racing on the same rows — a correctness bug, not just an efficiency one.

**Acceptance Criteria**:

1. WHEN a replica's `PollerManager` starts THEN it SHALL attempt to acquire a named Postgres advisory lock (`pg_try_advisory_lock`) before running any poll cycle.
2. IF a replica fails to acquire the lock THEN it SHALL NOT run the poller, and SHALL retry acquiring the lock on a fixed interval for as long as the process runs.
3. WHILE a replica holds the lock, it SHALL assert liveness at least once per `PollerManager` heartbeat interval, via an independent heartbeat ticker checked through `Handle.Healthy()` rather than tied to poll-cycle completion (supersedes this criterion's original "renewed... on every successful poll cycle" framing — the session-scoped `pg_advisory_lock` already dies automatically with its holding connection, so heartbeating the session's liveness on its own timer is sufficient and simpler than reimplementing per-cycle lease renewal; see design.md's Tech Decisions table, "Leader election mechanism").
4. IF the lock-holding replica fails to renew before its lease expires (crash, GC pause, network partition) THEN another replica SHALL be able to acquire the lock and begin polling within one renewal interval of the failure.
5. WHEN a replica loses the lock mid-cycle (renewal fails) THEN it SHALL abort the in-flight poll cycle without writing partial results, rather than letting it complete.
6. The system SHALL require no new environment variable or operator configuration for this behavior — it activates automatically whenever more than one replica is running against the same database.
7. WHEN only one replica exists (default `replicaCount: 1`) THEN behavior SHALL be observably unchanged from before this feature (single poller, same polling cadence).

**Independent Test**: Run two `PollerManager` instances against the same Postgres database in a test; assert exactly one runs poll cycles at a time, and that killing the active one lets the other take over.

---

### P1: Rate limiter enforces per-IP limits across replicas ⭐ MVP

**User Story**: As an operator, I want login/password-reset/invite-accept/bootstrap rate limiting to hold the same limit no matter which replica receives a given client IP's requests, so that horizontal scaling doesn't weaken brute-force protection.

**Why P1**: `internal/ratelimit.IPLimiter`'s own doc comment states "no cross-process/cross-replica state is needed" — false once `replicaCount > 1`; this is a real security regression path, not just a rough edge.

**Acceptance Criteria**:

1. The system SHALL enforce the existing per-IP rate limit (same `perMinute`/`burst` values already configured) for the 4 existing unauthenticated routes, counting requests across all replicas sharing one Postgres database.
2. WHEN a client IP exceeds its limit on any replica THEN a request routed to a different replica SHALL also be rejected with 429 until the limit window allows more requests, matching current single-process behavior byte-for-byte in the response body.
3. IF the Postgres check for a given request errors or times out THEN the system SHALL allow the request through (fail-open) and SHALL log the failure, rather than blocking legitimate traffic.
4. The system SHALL bound the storage table's growth the same way the current in-memory map does (idle-entry eviction), so a churn of distinct client IPs does not grow the table unbounded.
5. WHEN only one replica exists THEN the externally observable rate-limit behavior (thresholds, 429 body, header if any) SHALL be unchanged from before this feature.

**Independent Test**: Two limiter instances backed by the same test database; hammer one past its burst, assert the other also rejects for the same IP; assert a different IP is unaffected.

---

### P1: CertMagic certificate storage lives in Postgres, no shared volume required ⭐ MVP

**User Story**: As an operator, I want certificate state stored in the database Vane already requires, so that any replica can serve TLS for any registered domain without a `ReadWriteMany` volume, and so the Helm chart's `ReadWriteOnce` PVC can be removed.

**Why P1**: `certmagic.FileStorage` writing to local disk is the hardest blocker of the three — it isn't just weaker under scale, it actively breaks (or silently duplicates ACME issuance) the moment more than one pod exists, since `ReadWriteOnce` disallows sharing the volume across pods on most CSI drivers.

**Acceptance Criteria**:

1. The system SHALL implement `certmagic.Storage` (Store, Load, Delete, Exists, List, Stat, Lock, Unlock) backed by a Postgres table, and SHALL configure CertMagic to use it in place of `certmagic.FileStorage`.
2. WHEN any replica issues, renews, or reads a certificate for a registered hostname THEN the result SHALL be immediately visible to every other replica via Postgres (no local caching layer).
3. WHILE CertMagic holds a storage-level lock for a given key (its own internal coordination for concurrent issuance of the same hostname) THEN the Postgres-backed `Lock`/`Unlock` SHALL provide the same mutual-exclusion guarantee `certmagic.FileStorage`'s file-lock does today, implemented via `pg_advisory_lock` keyed by a hash of the storage key.
4. IF a replica crashes while holding a CertMagic storage lock THEN the lock SHALL be released automatically once that replica's Postgres connection closes (advisory-lock-per-session semantics), so no permanent deadlock survives a crash.
5. The system SHALL NOT require a `PersistentVolumeClaim` for certificate storage — `charts/zeep-vane/templates/pvc.yaml` and the `persistence.*` values SHALL be removed, and `deployment.yaml`'s volume mount for CertMagic storage SHALL be removed.
6. WHEN `replicaCount: 1` (current default) THEN certificate issuance/renewal/serving SHALL behave identically to today's `FileStorage`-backed behavior, with the one accepted trade-off that certificates are no longer human-inspectable as files on disk.

**Independent Test**: Two `certmagic.Storage` instances (Postgres-backed) against the same test database; Store from one, Load from the other, assert identical bytes; concurrent Lock from both for the same key, assert exactly one succeeds until Unlock.

---

## Edge Cases

- IF the advisory lock key collides with an unrelated lock acquired elsewhere in the codebase (e.g. an existing per-table/per-row advisory lock like `LockDatadogIntegration`) THEN the system SHALL use a distinct, documented lock key namespace to avoid an accidental cross-feature deadlock.
- IF the rate-limit table's cleanup query itself times out or errors THEN the system SHALL log and skip cleanup for that cycle rather than blocking the request path (cleanup is best-effort, not correctness-critical).
- IF `certmagic_storage.List` is called with a prefix matching zero rows THEN the system SHALL return an empty slice, not an error (matches `certmagic.Storage` interface contract).
- WHEN the Postgres connection pool is exhausted THEN all three mechanisms SHALL degrade per their own fail mode (poller: no lock acquired, stays idle; rate limiter: fail-open; cert storage: CertMagic's own retry/backoff surfaces the error to the ACME flow, same as any other transient storage failure today).

---

## Requirement Traceability

| Requirement ID | Story | Phase | Status |
| --- | --- | --- | --- |
| HA-01 | P1: Poller single-replica | Design | Pending |
| HA-02 | P1: Poller single-replica | Design | Pending |
| HA-03 | P1: Poller single-replica | Design | Pending |
| HA-04 | P1: Poller single-replica | Design | Pending |
| HA-05 | P1: Poller single-replica | Design | Pending |
| HA-06 | P1: Poller single-replica | Design | Pending |
| HA-07 | P1: Poller single-replica | Design | Pending |
| HA-08 | P1: Rate limiter cross-replica | Design | Pending |
| HA-09 | P1: Rate limiter cross-replica | Design | Pending |
| HA-10 | P1: Rate limiter cross-replica | Design | Pending |
| HA-11 | P1: Rate limiter cross-replica | Design | Pending |
| HA-12 | P1: Rate limiter cross-replica | Design | Pending |
| HA-13 | P1: CertMagic Postgres storage | Design | Pending |
| HA-14 | P1: CertMagic Postgres storage | Design | Pending |
| HA-15 | P1: CertMagic Postgres storage | Design | Pending |
| HA-16 | P1: CertMagic Postgres storage | Design | Pending |
| HA-17 | P1: CertMagic Postgres storage | Design | Pending |
| HA-18 | P1: CertMagic Postgres storage | Design | Pending |

**ID format:** `HA-NN`

**Status values:** Pending → In Design → In Tasks → Implementing → Verified

**Coverage:** 18 total, 0 mapped to tasks, 18 unmapped ⚠️ (mapped during Design/Tasks phases)

---

## Success Criteria

- [ ] `go test -tags=integration ./...` (disposable Postgres) passes with 2+ concurrent `PollerManager`/`IPLimiter`/CertMagic-storage instances in new tests proving mutual exclusion / shared state.
- [ ] `charts/zeep-vane/values.yaml`'s `replicaCount` default can be raised above 1 with no known code-level correctness gap remaining (chart default change itself is out of scope, see above).
- [ ] `persistence.*` values and `templates/pvc.yaml` removed from the Helm chart with `helm lint`/`helm template` still clean.
- [ ] No new external dependency introduced (Redis/etcd/NFS/object storage) — Postgres remains the only mandatory dependency.
