//go:build integration

package db

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

// datadogIntegrationLockKey must match dbtest.LockDatadogIntegration's
// constant of the same name - this package can't import internal/dbtest
// (dbtest imports db, which would cycle), so the lock is taken inline here
// instead, guarding the same integrations.provider = 'datadog' singleton
// row that internal/api and internal/poller's tests also mutate.
const datadogIntegrationLockKey = 727100001

func TestIntegrationsMigration_AppliesClean_AndEnforcesUniqueProvider(t *testing.T) {
	dsn := testDatabaseURL(t)

	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx := context.Background()
	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	// A dedicated connection independent of pool, not one acquired from it:
	// advisory locks are session-scoped, and sharing a pool connection here
	// would risk pool.Close (if a future test added one mid-test) deadlocking
	// while waiting for this held connection to be returned.
	lockConn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to open dedicated lock connection: %v", err)
	}
	if _, err := lockConn.Exec(ctx, "SELECT pg_advisory_lock($1)", datadogIntegrationLockKey); err != nil {
		t.Fatalf("pg_advisory_lock failed: %v", err)
	}
	t.Cleanup(func() {
		_, _ = lockConn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", datadogIntegrationLockKey)
		_ = lockConn.Close(context.Background())
	})

	const provider = "datadog"
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM integrations WHERE provider = $1", provider)
	})

	_, err = pool.Exec(ctx,
		"INSERT INTO integrations (provider, encrypted_api_key, encrypted_app_key) VALUES ($1, $2, $3)",
		provider, []byte("cipher-1"), []byte("cipher-1"))
	if err != nil {
		t.Fatalf("first insert returned unexpected error: %v", err)
	}

	_, err = pool.Exec(ctx,
		"INSERT INTO integrations (provider, encrypted_api_key, encrypted_app_key) VALUES ($1, $2, $3)",
		provider, []byte("cipher-2"), []byte("cipher-2"))
	if err == nil {
		t.Fatal("second insert with duplicate provider returned nil error, want unique constraint violation")
	}
}
