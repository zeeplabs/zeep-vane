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

// TestIntegrationsMigration_AppliesClean_AndEnforcesUniqueProviderPerTenant
// covers 0040_integrations_tenant_scope: the unique constraint moved from a
// bare "provider" to "(tenant_id, provider)" - a tenant still can't connect
// the same provider twice (this test), but a *different* tenant connecting
// the same provider must not collide with it at all (proven separately by
// TestIntegrationRepository_UpsertDatadog_Reconnect_OverwritesSameRow and
// TestRLS_Integrations_TenantA_NeverSeesTenantB_EvenWithUnfilteredQuery -
// both rely on two tenants each successfully holding their own "datadog"
// row, which a still-global unique constraint would make impossible).
func TestIntegrationsMigration_AppliesClean_AndEnforcesUniqueProviderPerTenant(t *testing.T) {
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

	// integrations.tenant_id is NOT NULL + FORCE ROW LEVEL SECURITY since
	// 0040 - both inserts below need a real tenant and app.tenant_id set,
	// same as seedTenantWithIntegration in rls_test.go. tenantID's cleanup
	// (registered first, so it runs last/LIFO) would otherwise violate
	// tenant_id's FK if it ran before the integrations row is deleted.
	tenantID := seedPlainTenant(t, pool)
	const provider = "datadog"
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM integrations WHERE provider = $1 AND tenant_id = $2", provider, tenantID)
	})

	tx1, err := pool.BeginTenantTx(ctx, "", tenantID)
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	_, err = pool.Exec(WithTenantTx(ctx, tx1),
		"INSERT INTO integrations (provider, encrypted_api_key, encrypted_app_key) VALUES ($1, $2, $3)",
		provider, []byte("cipher-1"), []byte("cipher-1"))
	if err != nil {
		t.Fatalf("first insert returned unexpected error: %v", err)
	}
	if err := tx1.Commit(ctx); err != nil {
		t.Fatalf("commit returned unexpected error: %v", err)
	}

	tx2, err := pool.BeginTenantTx(ctx, "", tenantID)
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	defer func() { _ = tx2.Rollback(context.Background()) }()
	_, err = pool.Exec(WithTenantTx(ctx, tx2),
		"INSERT INTO integrations (provider, encrypted_api_key, encrypted_app_key) VALUES ($1, $2, $3)",
		provider, []byte("cipher-2"), []byte("cipher-2"))
	if err == nil {
		t.Fatal("second insert with duplicate provider for the same tenant returned nil error, want unique constraint violation")
	}
}
