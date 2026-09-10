// Package dbtest holds small test-only helpers shared across this
// project's integration tests. It is not imported by any production code
// path.
package dbtest

import (
	"context"
	"net/url"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
)

// datadogIntegrationLockKey is an arbitrary constant identifying the
// Postgres advisory lock guarding the Datadog integration singleton row
// (integrations.provider = 'datadog', unique). Its value has no meaning
// beyond being a stable key both sides of the lock agree on.
const datadogIntegrationLockKey = 727100001

// usersTableLockKey is an arbitrary constant identifying the Postgres
// advisory lock guarding the shared `users` table. Its value has no
// meaning beyond being a stable key both sides of the lock agree on, and
// it is deliberately distinct from datadogIntegrationLockKey so the two
// locks never contend with each other.
const usersTableLockKey = 727100002

// tenantsTableLockKey is an arbitrary constant identifying the Postgres
// advisory lock guarding the shared `tenants` table (which absorbed the
// old company_settings singleton).
// Its value has no meaning beyond being a stable key both sides of the
// lock agree on, and it is deliberately distinct from the other keys in
// this file so none of these locks ever contend with each other.
const tenantsTableLockKey = 727100003

// LockDatadogIntegration serializes access to the Datadog integration
// singleton row for the duration of the calling test. `go test ./...` runs
// separate packages' test binaries in parallel, and internal/db,
// internal/api, and internal/poller each have tests that insert, update, or
// delete that same unique row - without serialization those tests race each
// other's writes.
//
// The lock is held on its own dedicated connection (opened directly via
// dsn, independent of any *db.Pool the caller's test uses) and released via
// t.Cleanup. It deliberately does not share a connection acquired from the
// test's own pool: advisory locks are session-scoped, and a test that
// explicitly closes its own pool mid-test (to simulate a downstream
// failure) would otherwise deadlock - pool.Close waits for every
// connection it handed out to be returned, including one this helper is
// still holding for the lock.
func LockDatadogIntegration(t *testing.T, ctx context.Context, dsn string) {
	t.Helper()
	lockAdvisoryKey(t, ctx, dsn, datadogIntegrationLockKey)
}

// LockUsersTable serializes access to the shared `users` table for the
// duration of the calling test. `go test ./...` runs separate packages'
// test binaries in parallel, and internal/db, internal/api, and
// internal/cli each have tests that bulk `DELETE FROM users` (to get a
// known-empty table for BootstrapFirst/BootstrapHandler/route tests) or
// depend on the table's exact row count (e.g. counting active owners) -
// without serialization, one package's clear/restore window races
// another package's inserts, deletes, or counts against the same shared
// TEST_DATABASE_URL Postgres instance.
//
// Every test that either performs such a bulk clear/restore of `users`
// or asserts an exact row/owner count against it must call this helper,
// not just the tests that themselves clear the table - otherwise a
// lock-holding clear can still run concurrently with a non-locking
// count and corrupt it.
//
// See LockDatadogIntegration's doc comment for why the lock is held on
// its own dedicated connection rather than one borrowed from the
// caller's pool.
func LockUsersTable(t *testing.T, ctx context.Context, dsn string) {
	t.Helper()
	lockAdvisoryKey(t, ctx, dsn, usersTableLockKey)
}

// LockTenantsTable serializes access to the shared `tenants` table for
// the duration of the calling test. `go test ./...`
// runs separate packages' test binaries in parallel, and internal/db,
// internal/api, and internal/cli each have tests that reset, update, or
// assert the exact content of the installation's tenant row - without
// serialization, one package's reset-to-blank window races another
// package's read-and-assert or update against the same shared
// TEST_DATABASE_URL Postgres instance.
//
// See LockDatadogIntegration's doc comment for why the lock is held on
// its own dedicated connection rather than one borrowed from the
// caller's pool.
func LockTenantsTable(t *testing.T, ctx context.Context, dsn string) {
	t.Helper()
	lockAdvisoryKey(t, ctx, dsn, tenantsTableLockKey)
}

// heldLocksMu guards heldLocks, which tracks which (test, advisory-key)
// pairs already hold their lock. This makes lockAdvisoryKey idempotent
// per *testing.T: a test (or a shared helper it calls more than once,
// directly or via sub-helpers) can call LockUsersTable/
// LockDatadogIntegration any number of times without deadlocking itself
// on a second dedicated connection waiting for the first one - which it
// would otherwise never release, since release only happens at that same
// test's Cleanup. This intentionally does not cover the case of a
// subtest's *testing.T taking a lock its parent's *testing.T already
// holds (those are different sessions and will genuinely serialize on
// the parent, which is what we want) - callers with that shape should
// take the lock at only one level, not both.
var (
	heldLocksMu sync.Mutex
	heldLocks   = map[*testing.T]map[int64]bool{}
)

// lockAdvisoryKey opens a dedicated connection, takes the given Postgres
// advisory lock on it for the duration of the calling test, and releases
// it via t.Cleanup. Calling it more than once for the same (t, key) pair
// is a safe no-op after the first call.
func lockAdvisoryKey(t *testing.T, ctx context.Context, dsn string, key int64) {
	t.Helper()

	heldLocksMu.Lock()
	if heldLocks[t] == nil {
		heldLocks[t] = map[int64]bool{}
	}
	if heldLocks[t][key] {
		heldLocksMu.Unlock()
		return
	}
	heldLocks[t][key] = true
	heldLocksMu.Unlock()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("dbtest: failed to open dedicated lock connection: %v", err)
	}

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", key); err != nil {
		_ = conn.Close(ctx)
		t.Fatalf("dbtest: pg_advisory_lock failed: %v", err)
	}

	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", key)
		_ = conn.Close(context.Background())

		heldLocksMu.Lock()
		delete(heldLocks[t], key)
		if len(heldLocks[t]) == 0 {
			delete(heldLocks, t)
		}
		heldLocksMu.Unlock()
	})
}

// TenantScopedDSN returns dsn with the Postgres connection option
// `-c app.tenant_id=<tenantID>`, so every session opened from it starts
// with that setting already in place. Since multi-tenancy-core every
// tenant-scoped table's tenant_id column defaults from
// current_setting('app.tenant_id', true) and is NOT NULL, so a fixture
// INSERT made outside any tenant transaction is rejected; this lets an
// integration suite pin a fixture tenant for a whole pool instead of
// wrapping every individual call in a transaction.
//
// It is a test-only convenience. Production never sets app.tenant_id per
// connection - it sets it per transaction (db.Pool.BeginTenantTx), which
// is what makes a pooled connection safe to hand to the next request.
func TenantScopedDSN(dsn, tenantID string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	q := u.Query()
	q.Set("options", "-c app.tenant_id="+tenantID)
	u.RawQuery = q.Encode()
	return u.String()
}
