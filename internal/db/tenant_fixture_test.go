//go:build integration

package db

import (
	"context"
	"testing"

	"github.com/zeeplabs/zeep-vane/internal/dbtest"
)

// seedPlainTenant inserts and returns the id of a throwaway tenant row for
// tests that don't care about RLS enforcement itself (they run through the
// disposable test container's bootstrap role, which is a superuser and so
// already bypasses RLS - see rls_test.go's rlsTestRole for the one suite
// that specifically proves enforcement). Its only purpose here is to give
// domain-table fixtures (services, ...) a valid tenant_id to satisfy the
// NOT NULL constraint 0024 added.
func seedPlainTenant(t *testing.T, pool *Pool) string {
	t.Helper()
	ctx := context.Background()

	var id string
	if err := pool.QueryRow(ctx,
		"INSERT INTO tenants (name) VALUES ($1) RETURNING id", "fixture-tenant",
	).Scan(&id); err != nil {
		t.Fatalf("seeding fixture tenant returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM tenants WHERE id = $1", id)
	})

	return id
}

// withTenantTx runs fn with a context whose pool.* calls execute inside a
// transaction with app.tenant_id set to tenantID, committing immediately
// after fn returns - fixtures created through it must be visible to later
// calls in the same test that use a plain context.Background() (a separate,
// autocommit connection checked out from the pool), which only happens once
// the insert is committed, not while the tx that made it is still open.
func withTenantTx(t *testing.T, pool *Pool, tenantID string, fn func(ctx context.Context)) {
	t.Helper()
	ctx := context.Background()

	tx, err := pool.BeginTenantTx(ctx, "", tenantID)
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}

	fn(WithTenantTx(ctx, tx))

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit returned unexpected error: %v", err)
	}
}

// newTenantScopedPool returns a migrated pool whose every connection has
// app.tenant_id preset (at session level, via the connection's `options`
// parameter) to a throwaway fixture tenant, plus that tenant's id.
//
// Every tenant-scoped table's tenant_id column defaults to
// NULLIF(current_setting('app.tenant_id', true), ”)::uuid and is NOT NULL
// (0024), so a fixture INSERT made outside any tenant context is rejected.
// Presetting the session setting is what lets the repository suites below
// keep inserting through plain context.Background() calls - what they are
// testing is the repository's own behaviour, not tenant resolution, which
// has its own suite in rls_test.go (which deliberately does NOT use this
// helper: it needs the setting genuinely unset to prove fail-closed).
func newTenantScopedPool(t *testing.T) (*Pool, string) {
	t.Helper()
	dsn := testDatabaseURL(t)
	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx := context.Background()
	bootstrapPool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	tenantID := seedPlainTenant(t, bootstrapPool)
	bootstrapPool.Close()

	pool, err := NewPool(ctx, dbtest.TenantScopedDSN(dsn, tenantID))
	if err != nil {
		t.Fatalf("NewPool() (tenant-scoped) returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool, tenantID
}
