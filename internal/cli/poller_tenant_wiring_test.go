//go:build integration

package cli

import (
	"context"
	"testing"

	"go.uber.org/zap"
)

// TestNewPollerFromStoredIntegration_EnablesTenantIteration closes the
// wiring half of T15: the capability existed on Poller and was unit
// tested in isolation, but production still built a single-install
// poller, so no deployment ever iterated tenants. This asserts on the
// real production construction path - the same function
// PollerManager.Restart calls - rather than on a poller assembled by the
// test itself.
func TestNewPollerFromStoredIntegration_EnablesTenantIteration(t *testing.T) {
	pool := newServeTestPool(t)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM integrations WHERE provider = 'datadog'") })
	storeTestDatadogIntegration(t, pool)

	p, started, err := newPollerFromStoredIntegration(context.Background(), pool, pollerManagerTestConfig(), zap.NewNop())
	if err != nil {
		t.Fatalf("newPollerFromStoredIntegration() returned unexpected error: %v", err)
	}
	if !started {
		t.Fatal("newPollerFromStoredIntegration() started = false, want true - a stored integration exists")
	}
	if !p.TenantIterationEnabled() {
		t.Error("poller built by newPollerFromStoredIntegration has tenant iteration disabled, want enabled (TENANT-04) - every production poll cycle must iterate tenants with app.tenant_id set per tenant")
	}
}

// TestPoolTenantTx_SetsAppTenantIDAndCommits proves the adapter
// newPollerFromStoredIntegration hands the poller really produces a
// tenant-scoped session: the context it returns runs queries on a
// transaction whose app.tenant_id is the requested tenant, and its commit
// function succeeds. Without this, TenantIterationEnabled could be true
// while every tenant's poll ran unscoped.
func TestPoolTenantTx_SetsAppTenantIDAndCommits(t *testing.T) {
	pool := newServeTestPool(t)

	var tenantID string
	if err := pool.QueryRow(context.Background(), "INSERT INTO tenants (name) VALUES ($1) RETURNING id", "poller-tenant-tx-test").Scan(&tenantID); err != nil {
		t.Fatalf("seeding tenant returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM tenants WHERE id = $1", tenantID) })

	tenantCtx, commit, rollback, err := poolTenantTx(pool)(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("poolTenantTx() returned unexpected error: %v", err)
	}
	defer rollback(context.Background())

	var got string
	if err := pool.QueryRow(tenantCtx, "SELECT COALESCE(current_setting('app.tenant_id', true), '')").Scan(&got); err != nil {
		t.Fatalf("reading app.tenant_id returned unexpected error: %v", err)
	}
	if got != tenantID {
		t.Errorf("app.tenant_id on the poller's per-tenant transaction = %q, want %q", got, tenantID)
	}

	var isSystem string
	if err := pool.QueryRow(tenantCtx, "SELECT COALESCE(current_setting('app.is_system', true), '')").Scan(&isSystem); err != nil {
		t.Fatalf("reading app.is_system returned unexpected error: %v", err)
	}
	if isSystem == "true" {
		t.Errorf("app.is_system on the poller's per-tenant transaction = %q, want it unset - the flag belongs to the enumeration step only (AD-024)", isSystem)
	}

	if err := commit(context.Background()); err != nil {
		t.Fatalf("commit() returned unexpected error: %v", err)
	}
}
