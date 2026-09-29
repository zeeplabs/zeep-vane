package pglock

// Reserved advisory-lock keys for production cross-replica coordination
// (see the package doc for the 727200000-727299999 block convention).
//
// Postgres advisory locks are a single namespace shared by both the
// session-scoped (pg_advisory_lock / pg_try_advisory_lock) and the
// transaction-scoped (pg_advisory_xact_lock / pg_try_advisory_xact_lock)
// variants. Two unrelated jobs that pick the same key do not error - the
// loser's TryAcquire simply reports "not acquired", so its work is silently
// skipped for that cycle (an hourly prune misses a tick, a daily health
// check misses a day, a boot-time backfill misses a replica). That makes a
// duplicate key a silent-bug class, not a loud one.
//
// Every anonymous (int64) lock in this project must therefore take its key
// from this list, and this list must stay unique. TestReservedLockKeys_Unique
// enforces that. Add a new key by appending the next free value here and
// aliasing it at the call site - never by hard-coding a number there.
const (
	// PollerLeaderLockKey guards the Datadog poller's leader election.
	PollerLeaderLockKey int64 = 727200001
	// DigestLeaderLockKey guards the weekly digest send.
	DigestLeaderLockKey int64 = 727200002
	// PruneLeaderLockKey guards the status-interval retention prune.
	PruneLeaderLockKey int64 = 727200003
	// DomainHealthLeaderLockKey guards the daily domain RDAP/NS health check.
	DomainHealthLeaderLockKey int64 = 727200004
	// TLSKeyBackfillLockKey serializes the legacy-key encryption backfill
	// across replicas booting at the same time (AD-030). Distinct from
	// PruneLeaderLockKey even though both once used 727200003.
	TLSKeyBackfillLockKey int64 = 727200005
)

// reservedLockKeys maps each reserved key to its owner, for the uniqueness
// guard. Keep in sync with the const block above.
var reservedLockKeys = map[string]int64{
	"poller":        PollerLeaderLockKey,
	"digest":        DigestLeaderLockKey,
	"retention":     PruneLeaderLockKey,
	"domain-health": DomainHealthLeaderLockKey,
	"tls-backfill":  TLSKeyBackfillLockKey,
}
