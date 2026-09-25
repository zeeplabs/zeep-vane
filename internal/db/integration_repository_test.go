//go:build integration

package db

import (
	"context"
	"errors"
	"testing"
)

// resetIntegrations clears this tenant's integrations rows, leaving the
// schema in the same clean state a fresh migration would - so each test in
// this file starts from a known baseline regardless of what a previous
// test left behind.
func resetIntegrations(t *testing.T, ctx context.Context, pool *Pool, tenantID string) {
	t.Helper()
	if _, err := pool.Exec(ctx, "DELETE FROM integrations WHERE tenant_id = $1", tenantID); err != nil {
		t.Fatalf("failed to clear integrations: %v", err)
	}
}

func newIntegrationRepoForTest(t *testing.T) (*IntegrationRepository, *Pool) {
	t.Helper()
	pool, tenantID := newTenantScopedPool(t)

	resetIntegrations(t, context.Background(), pool, tenantID)
	t.Cleanup(func() { resetIntegrations(t, context.Background(), pool, tenantID) })

	return NewIntegrationRepository(pool), pool
}

func TestIntegrationRepository_UpsertDatadog_FirstConnect_RoundTrips(t *testing.T) {
	repo, pool := newIntegrationRepoForTest(t)
	ctx := context.Background()

	if err := repo.UpsertDatadog(ctx, []byte("cipher-api-1"), []byte("cipher-app-1")); err != nil {
		t.Fatalf("UpsertDatadog() returned unexpected error: %v", err)
	}

	integration, err := repo.GetDatadog(ctx)
	if err != nil {
		t.Fatalf("GetDatadog() returned unexpected error: %v", err)
	}
	if integration.Status != "active" {
		t.Errorf("Status = %q, want %q", integration.Status, "active")
	}
	if string(integration.EncryptedAPIKey) != "cipher-api-1" {
		t.Errorf("EncryptedAPIKey = %q, want %q", integration.EncryptedAPIKey, "cipher-api-1")
	}
	if string(integration.EncryptedAppKey) != "cipher-app-1" {
		t.Errorf("EncryptedAppKey = %q, want %q", integration.EncryptedAppKey, "cipher-app-1")
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM integrations WHERE provider = 'datadog'").Scan(&count); err != nil {
		t.Fatalf("count query returned unexpected error: %v", err)
	}
	if count != 1 {
		t.Fatalf("row count for datadog = %d, want exactly 1", count)
	}
}

// TestIntegrationRepository_UpsertDatadog_Reconnect_OverwritesSameRow
// covers the fix in 0040_integrations_tenant_scope: the ON CONFLICT target
// changed from (provider) to (tenant_id, provider) when the unique
// constraint became per-tenant - a regression back to the old target would
// make a reconnect INSERT a second row (constraint violation) instead of
// overwriting, for any tenant beyond the first ever seeded.
func TestIntegrationRepository_UpsertDatadog_Reconnect_OverwritesSameRow(t *testing.T) {
	repo, pool := newIntegrationRepoForTest(t)
	ctx := context.Background()

	if err := repo.UpsertDatadog(ctx, []byte("cipher-api-old"), []byte("cipher-app-old")); err != nil {
		t.Fatalf("first UpsertDatadog() returned unexpected error: %v", err)
	}
	if err := repo.UpsertDatadog(ctx, []byte("cipher-api-new"), []byte("cipher-app-new")); err != nil {
		t.Fatalf("second UpsertDatadog() returned unexpected error: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM integrations WHERE provider = 'datadog'").Scan(&count); err != nil {
		t.Fatalf("count query returned unexpected error: %v", err)
	}
	if count != 1 {
		t.Fatalf("row count for datadog = %d, want exactly 1 (reconnect must overwrite, not insert a second row)", count)
	}

	integration, err := repo.GetDatadog(ctx)
	if err != nil {
		t.Fatalf("GetDatadog() returned unexpected error: %v", err)
	}
	if string(integration.EncryptedAPIKey) != "cipher-api-new" {
		t.Errorf("EncryptedAPIKey = %q, want %q", integration.EncryptedAPIKey, "cipher-api-new")
	}
	if string(integration.EncryptedAppKey) != "cipher-app-new" {
		t.Errorf("EncryptedAppKey = %q, want %q", integration.EncryptedAppKey, "cipher-app-new")
	}
}

func TestIntegrationRepository_GetDatadog_NeverConnected_ReturnsErrNotFound(t *testing.T) {
	repo, _ := newIntegrationRepoForTest(t)
	ctx := context.Background()

	_, err := repo.GetDatadog(ctx)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetDatadog() error = %v, want ErrNotFound", err)
	}
}

func TestIntegrationRepository_ListPaginated_NoneConnected_ReturnsEmptySlice(t *testing.T) {
	repo, _ := newIntegrationRepoForTest(t)
	ctx := context.Background()

	integrations, total, err := repo.ListPaginated(ctx, 1, 20)
	if err != nil {
		t.Fatalf("ListPaginated() returned unexpected error: %v", err)
	}
	if len(integrations) != 0 {
		t.Fatalf("ListPaginated() returned %d integrations, want 0", len(integrations))
	}
	if total != 0 {
		t.Fatalf("ListPaginated() total = %d, want 0", total)
	}
}

func TestIntegrationRepository_MarkDatadogInvalid_SetsStatusAndLastError(t *testing.T) {
	repo, _ := newIntegrationRepoForTest(t)
	ctx := context.Background()

	if err := repo.UpsertDatadog(ctx, []byte("cipher-api"), []byte("cipher-app")); err != nil {
		t.Fatalf("UpsertDatadog() returned unexpected error: %v", err)
	}

	if err := repo.MarkDatadogInvalid(ctx, "invalid credentials"); err != nil {
		t.Fatalf("MarkDatadogInvalid() returned unexpected error: %v", err)
	}

	integration, err := repo.GetDatadog(ctx)
	if err != nil {
		t.Fatalf("GetDatadog() returned unexpected error: %v", err)
	}
	if integration.Status != "invalid" {
		t.Errorf("Status = %q, want %q", integration.Status, "invalid")
	}
	if integration.LastError == nil || *integration.LastError != "invalid credentials" {
		t.Errorf("LastError = %v, want %q", integration.LastError, "invalid credentials")
	}
	if integration.LastCheckedAt == nil {
		t.Error("LastCheckedAt is nil, want a timestamp set by MarkDatadogInvalid")
	}
}

func TestIntegrationRepository_MarkDatadogChecked_ClearsInvalidState(t *testing.T) {
	repo, _ := newIntegrationRepoForTest(t)
	ctx := context.Background()

	if err := repo.UpsertDatadog(ctx, []byte("cipher-api"), []byte("cipher-app")); err != nil {
		t.Fatalf("UpsertDatadog() returned unexpected error: %v", err)
	}
	if err := repo.MarkDatadogInvalid(ctx, "seeded failure"); err != nil {
		t.Fatalf("MarkDatadogInvalid() returned unexpected error: %v", err)
	}

	if err := repo.MarkDatadogChecked(ctx); err != nil {
		t.Fatalf("MarkDatadogChecked() returned unexpected error: %v", err)
	}

	integration, err := repo.GetDatadog(ctx)
	if err != nil {
		t.Fatalf("GetDatadog() returned unexpected error: %v", err)
	}
	if integration.Status != "active" {
		t.Errorf("Status = %q, want %q", integration.Status, "active")
	}
	if integration.LastError != nil {
		t.Errorf("LastError = %q, want nil (must clear the seeded failure)", *integration.LastError)
	}
}
