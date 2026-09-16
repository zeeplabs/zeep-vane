//go:build integration

package db

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

// decodedJSON unmarshal-compares raw JSON bytes as maps rather than byte
// strings - Postgres's jsonb column re-serializes (whitespace, key order),
// so a passthrough round-trip is never byte-identical to the input.
func decodedJSON(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("json.Unmarshal(%s) returned unexpected error: %v", raw, err)
	}
	return m
}

func newTenantRepoTestPool(t *testing.T) (*TenantRepository, *Pool) {
	t.Helper()
	dsn := testDatabaseURL(t)
	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}
	pool, err := NewPool(context.Background(), dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)
	return NewTenantRepository(pool), pool
}

// createTestTenant creates and returns a committed tenant via Create's
// self-contained path (no pre-existing tenant transaction in ctx),
// registering its cleanup.
func createTestTenant(t *testing.T, repo *TenantRepository, pool *Pool, name string) *Tenant {
	t.Helper()
	tenant := &Tenant{Name: name, ContactEmail: "owner@" + name + ".example"}
	if err := repo.Create(context.Background(), tenant); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM tenants WHERE id = $1", tenant.ID) })
	return tenant
}

func TestTenantRepository_Create_GeneratesIDAndDefaults(t *testing.T) {
	repo, pool := newTenantRepoTestPool(t)
	tenant := createTestTenant(t, repo, pool, "create-generates-id")
	if tenant.ID == "" {
		t.Error("tenant.ID is empty, want a generated uuid")
	}
	if tenant.Plan != "free" {
		t.Errorf("tenant.Plan = %q, want %q (column default)", tenant.Plan, "free")
	}
	if tenant.Status != "active" {
		t.Errorf("tenant.Status = %q, want %q (column default)", tenant.Status, "active")
	}
	if tenant.Locale != "pt-BR" {
		t.Errorf("tenant.Locale = %q, want %q (column default)", tenant.Locale, "pt-BR")
	}
	if tenant.CreatedAt.IsZero() {
		t.Error("tenant.CreatedAt is zero, want a timestamp")
	}
}

func TestTenantRepository_Get_ScopedByRLS_ReturnsCreatedTenant(t *testing.T) {
	repo, pool := newTenantRepoTestPool(t)
	tenant := createTestTenant(t, repo, pool, "get-scoped-by-rls")

	tx, err := pool.BeginTenantTx(context.Background(), "", tenant.ID)
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	ctx := WithTenantTx(context.Background(), tx)

	got, err := repo.Get(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if got.ID != tenant.ID || got.Name != tenant.Name {
		t.Errorf("Get() = %+v, want ID=%q Name=%q", got, tenant.ID, tenant.Name)
	}
}

// Note: a "Get with no tenant context returns ErrNotFound" case is
// deliberately not repeated here - this package's tests run as the
// disposable test container's bootstrap role, a Postgres superuser that
// unconditionally bypasses RLS regardless of FORCE ROW LEVEL SECURITY
// (see rls_test.go's rlsTestRole), so it would pass here for the wrong
// reason. internal/db/rls_test.go already proves the fail-closed
// guarantee itself, under a role RLS actually restricts; re-asserting it
// here through TenantRepository would just be an unfalsifiable duplicate
// of that coverage (Check C - necessary).

func TestTenantRepository_Update_PartialFields_DoesNotClobberUnsetOnes(t *testing.T) {
	repo, pool := newTenantRepoTestPool(t)
	tenant := createTestTenant(t, repo, pool, "update-partial")
	originalContactEmail := tenant.ContactEmail

	tx, err := pool.BeginTenantTx(context.Background(), "", tenant.ID)
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	ctx := WithTenantTx(context.Background(), tx)

	newName := "update-partial-renamed"
	updated, err := repo.Update(ctx, tenant.ID, TenantUpdate{Name: &newName})
	if err != nil {
		t.Fatalf("Update() returned unexpected error: %v", err)
	}
	if updated.Name != newName {
		t.Errorf("updated.Name = %q, want %q", updated.Name, newName)
	}
	if updated.ContactEmail != originalContactEmail {
		t.Errorf("updated.ContactEmail = %q, want unchanged %q", updated.ContactEmail, originalContactEmail)
	}
}

func TestTenantRepository_Update_LegalNameAndValidCPF_Persists(t *testing.T) {
	repo, pool := newTenantRepoTestPool(t)
	tenant := createTestTenant(t, repo, pool, "update-valid-cpf")

	tx, err := pool.BeginTenantTx(context.Background(), "", tenant.ID)
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	ctx := WithTenantTx(context.Background(), tx)

	legalName := "Jane Doe"
	cpf := "12345678901"
	cpfType := "cpf"
	updated, err := repo.Update(ctx, tenant.ID, TenantUpdate{LegalName: &legalName, TaxID: &cpf, TaxIDType: &cpfType})
	if err != nil {
		t.Fatalf("Update() returned unexpected error: %v", err)
	}
	if updated.LegalName == nil || *updated.LegalName != legalName {
		t.Errorf("updated.LegalName = %v, want %q", updated.LegalName, legalName)
	}
	if updated.TaxID == nil || *updated.TaxID != cpf {
		t.Errorf("updated.TaxID = %v, want %q", updated.TaxID, cpf)
	}
	if updated.TaxIDType == nil || *updated.TaxIDType != cpfType {
		t.Errorf("updated.TaxIDType = %v, want %q", updated.TaxIDType, cpfType)
	}
}

func TestTenantRepository_Update_ValidCNPJ_Persists(t *testing.T) {
	repo, pool := newTenantRepoTestPool(t)
	tenant := createTestTenant(t, repo, pool, "update-valid-cnpj")

	tx, err := pool.BeginTenantTx(context.Background(), "", tenant.ID)
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	ctx := WithTenantTx(context.Background(), tx)

	cnpj := "12345678000199"
	cnpjType := "cnpj"
	updated, err := repo.Update(ctx, tenant.ID, TenantUpdate{TaxID: &cnpj, TaxIDType: &cnpjType})
	if err != nil {
		t.Fatalf("Update() returned unexpected error: %v", err)
	}
	if updated.TaxID == nil || *updated.TaxID != cnpj {
		t.Errorf("updated.TaxID = %v, want %q", updated.TaxID, cnpj)
	}
}

func TestTenantRepository_Update_CPFWrongDigitCount_ErrInvalidTaxIDNoPersist(t *testing.T) {
	repo, pool := newTenantRepoTestPool(t)
	tenant := createTestTenant(t, repo, pool, "update-invalid-cpf")

	tx, err := pool.BeginTenantTx(context.Background(), "", tenant.ID)
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	ctx := WithTenantTx(context.Background(), tx)

	shortCPF := "12345"
	cpfType := "cpf"
	_, err = repo.Update(ctx, tenant.ID, TenantUpdate{TaxID: &shortCPF, TaxIDType: &cpfType})
	if !errors.Is(err, ErrInvalidTaxID) {
		t.Fatalf("Update() error = %v, want ErrInvalidTaxID", err)
	}

	got, err := repo.Get(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if got.TaxID != nil {
		t.Errorf("tax_id after rejected update = %v, want nil (not persisted)", got.TaxID)
	}
}

func TestTenantRepository_Update_CNPJWrongDigitCount_ErrInvalidTaxIDNoPersist(t *testing.T) {
	repo, pool := newTenantRepoTestPool(t)
	tenant := createTestTenant(t, repo, pool, "update-invalid-cnpj")

	tx, err := pool.BeginTenantTx(context.Background(), "", tenant.ID)
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	ctx := WithTenantTx(context.Background(), tx)

	shortCNPJ := "123456"
	cnpjType := "cnpj"
	_, err = repo.Update(ctx, tenant.ID, TenantUpdate{TaxID: &shortCNPJ, TaxIDType: &cnpjType})
	if !errors.Is(err, ErrInvalidTaxID) {
		t.Fatalf("Update() error = %v, want ErrInvalidTaxID", err)
	}
}

// TestTenantRepository_Update_WebsiteTimezoneBillingAddress_Persists
// asserts settings-page CFGPG-01/06/07: the 3 new optional fields persist
// and round-trip via Get, without disturbing pre-existing fiscal fields.
func TestTenantRepository_Update_WebsiteTimezoneBillingAddress_Persists(t *testing.T) {
	repo, pool := newTenantRepoTestPool(t)
	tenant := createTestTenant(t, repo, pool, "update-website-timezone")

	tx, err := pool.BeginTenantTx(context.Background(), "", tenant.ID)
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	ctx := WithTenantTx(context.Background(), tx)

	website := "https://acme.health"
	timezone := "UTC (GMT+0)"
	billingAddress := []byte(`{"zip":"01310-100","city":"São Paulo"}`)
	updated, err := repo.Update(ctx, tenant.ID, TenantUpdate{
		Website: &website, Timezone: &timezone, BillingAddress: &billingAddress,
	})
	if err != nil {
		t.Fatalf("Update() returned unexpected error: %v", err)
	}
	if updated.Website == nil || *updated.Website != website {
		t.Errorf("Website = %v, want %q", updated.Website, website)
	}
	if updated.Timezone == nil || *updated.Timezone != timezone {
		t.Errorf("Timezone = %v, want %q", updated.Timezone, timezone)
	}
	if !reflect.DeepEqual(decodedJSON(t, updated.BillingAddress), decodedJSON(t, billingAddress)) {
		t.Errorf("BillingAddress = %s, want %s", updated.BillingAddress, billingAddress)
	}
}

// TestTenantRepository_Update_NilBillingAddress_LeavesExistingUnchanged
// asserts TenantUpdate's "nil = unchanged" semantics extend to
// BillingAddress: a subsequent Update that omits it must not clobber a
// previously persisted value.
func TestTenantRepository_Update_NilBillingAddress_LeavesExistingUnchanged(t *testing.T) {
	repo, pool := newTenantRepoTestPool(t)
	tenant := createTestTenant(t, repo, pool, "update-nil-billing-address")

	tx, err := pool.BeginTenantTx(context.Background(), "", tenant.ID)
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	ctx := WithTenantTx(context.Background(), tx)

	billingAddress := []byte(`{"zip":"01310-100"}`)
	if _, err := repo.Update(ctx, tenant.ID, TenantUpdate{BillingAddress: &billingAddress}); err != nil {
		t.Fatalf("first Update() returned unexpected error: %v", err)
	}

	newName := "Acme Renamed"
	updated, err := repo.Update(ctx, tenant.ID, TenantUpdate{Name: &newName})
	if err != nil {
		t.Fatalf("second Update() returned unexpected error: %v", err)
	}
	if !reflect.DeepEqual(decodedJSON(t, updated.BillingAddress), decodedJSON(t, billingAddress)) {
		t.Errorf("BillingAddress after unrelated update = %s, want unchanged %s", updated.BillingAddress, billingAddress)
	}
}

// TestTenantRepository_SoftDelete_SetsStatusAndDeletedAt asserts
// settings-page CFGPG-09: SoftDelete marks the tenant deleted with a
// timestamp, without removing the row.
func TestTenantRepository_SoftDelete_SetsStatusAndDeletedAt(t *testing.T) {
	repo, pool := newTenantRepoTestPool(t)
	tenant := createTestTenant(t, repo, pool, "soft-delete-sets-status")

	if err := repo.SoftDelete(context.Background(), tenant.ID); err != nil {
		t.Fatalf("SoftDelete() returned unexpected error: %v", err)
	}

	var status string
	var deletedAt *string
	if err := pool.QueryRow(context.Background(),
		"SELECT status, deleted_at::text FROM tenants WHERE id = $1", tenant.ID,
	).Scan(&status, &deletedAt); err != nil {
		t.Fatalf("querying tenant returned unexpected error: %v", err)
	}
	if status != "deleted" {
		t.Errorf("status = %q, want %q", status, "deleted")
	}
	if deletedAt == nil {
		t.Error("deleted_at = nil, want a timestamp")
	}
}

// TestTenantRepository_SoftDelete_AlreadyDeleted_ErrNotFound asserts
// SoftDelete is not silently idempotent: calling it twice on the same
// tenant returns ErrNotFound the second time (edge case in spec.md).
func TestTenantRepository_SoftDelete_AlreadyDeleted_ErrNotFound(t *testing.T) {
	repo, pool := newTenantRepoTestPool(t)
	tenant := createTestTenant(t, repo, pool, "soft-delete-twice")

	if err := repo.SoftDelete(context.Background(), tenant.ID); err != nil {
		t.Fatalf("first SoftDelete() returned unexpected error: %v", err)
	}

	err := repo.SoftDelete(context.Background(), tenant.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("second SoftDelete() error = %v, want ErrNotFound", err)
	}
}
