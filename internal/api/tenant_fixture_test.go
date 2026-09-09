//go:build integration

package api

import (
	"context"
	"testing"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// seedTestTenant creates a throwaway tenant so service fixtures across this
// package's handler tests can satisfy the tenant_id NOT NULL constraint
// 0024 added to services.
func seedTestTenant(t *testing.T, pool *db.Pool) string {
	t.Helper()
	ctx := context.Background()

	var tenantID string
	if err := pool.QueryRow(ctx, "INSERT INTO tenants (name) VALUES ($1) RETURNING id", "api-test-tenant").Scan(&tenantID); err != nil {
		t.Fatalf("seeding test tenant returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM tenants WHERE id = $1", tenantID) })

	return tenantID
}

// withTenantTx runs fn with a context whose pool.* calls execute inside a
// transaction with app.tenant_id set to tenantID, committing immediately
// after fn returns so the fixture it creates is visible to later calls in
// the same test that use a plain context.Background() connection.
func withTenantTx(t *testing.T, pool *db.Pool, tenantID string, fn func(ctx context.Context)) {
	t.Helper()
	ctx := context.Background()

	tx, err := pool.BeginTenantTx(ctx, "", tenantID)
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}

	fn(db.WithTenantTx(ctx, tx))

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit returned unexpected error: %v", err)
	}
}
