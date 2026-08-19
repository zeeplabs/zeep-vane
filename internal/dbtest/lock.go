// Package dbtest holds small test-only helpers shared across this
// project's integration tests. It is not imported by any production code
// path.
package dbtest

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

// datadogIntegrationLockKey is an arbitrary constant identifying the
// Postgres advisory lock guarding the Datadog integration singleton row
// (integrations.provider = 'datadog', unique). Its value has no meaning
// beyond being a stable key both sides of the lock agree on.
const datadogIntegrationLockKey = 727100001

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

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("dbtest: failed to open dedicated lock connection: %v", err)
	}

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", datadogIntegrationLockKey); err != nil {
		_ = conn.Close(ctx)
		t.Fatalf("dbtest: pg_advisory_lock failed: %v", err)
	}

	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", datadogIntegrationLockKey)
		_ = conn.Close(context.Background())
	})
}
