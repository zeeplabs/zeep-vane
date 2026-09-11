//go:build integration

package db

import (
	"context"
	"errors"
	"testing"
)

func newTenantMembershipRepoTestPool(t *testing.T) (*TenantMembershipRepository, *TenantRepository, *UserRepository, *Pool) {
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
	return NewTenantMembershipRepository(pool), NewTenantRepository(pool), NewUserRepository(pool), pool
}

// createMembershipTestAdmin inserts a plain user row (identity only -
// tenant_memberships.user_id FKs to users(id)), registering its cleanup.
func createMembershipTestAdmin(t *testing.T, admins *UserRepository, pool *Pool, email string) *User {
	t.Helper()
	admin := &User{Email: email, PasswordHash: "hash"}
	if err := admins.Create(context.Background(), admin); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", admin.ID) })
	return admin
}

func createMembershipTestTenant(t *testing.T, tenants *TenantRepository, pool *Pool, name string) *Tenant {
	t.Helper()
	tenant := &Tenant{Name: name, ContactEmail: "owner@" + name + ".example"}
	if err := tenants.Create(context.Background(), tenant); err != nil {
		t.Fatalf("tenants.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM tenants WHERE id = $1", tenant.ID) })
	return tenant
}

func TestTenantMembershipRepository_Create_PersistsRoleAndCreatedAt(t *testing.T) {
	memberships, tenants, admins, pool := newTenantMembershipRepoTestPool(t)
	admin := createMembershipTestAdmin(t, admins, pool, "membership-create@example.com")
	tenant := createMembershipTestTenant(t, tenants, pool, "membership-create")

	tx, err := pool.BeginTenantTx(context.Background(), "", tenant.ID)
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	ctx := WithTenantTx(context.Background(), tx)

	m := &TenantMembership{UserID: admin.ID, TenantID: tenant.ID, Role: RoleOwner}
	if err := memberships.Create(ctx, m); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
	if m.CreatedAt.IsZero() {
		t.Error("m.CreatedAt is zero, want a timestamp")
	}
}

func TestTenantMembershipRepository_Create_DuplicatePair_ErrDuplicateMembership(t *testing.T) {
	memberships, tenants, admins, pool := newTenantMembershipRepoTestPool(t)
	admin := createMembershipTestAdmin(t, admins, pool, "membership-dup@example.com")
	tenant := createMembershipTestTenant(t, tenants, pool, "membership-dup")

	tx, err := pool.BeginTenantTx(context.Background(), "", tenant.ID)
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	ctx := WithTenantTx(context.Background(), tx)

	if err := memberships.Create(ctx, &TenantMembership{UserID: admin.ID, TenantID: tenant.ID, Role: RoleOwner}); err != nil {
		t.Fatalf("first Create() returned unexpected error: %v", err)
	}
	err = memberships.Create(ctx, &TenantMembership{UserID: admin.ID, TenantID: tenant.ID, Role: RoleOwner})
	if !errors.Is(err, ErrDuplicateMembership) {
		t.Errorf("second Create() error = %v, want ErrDuplicateMembership", err)
	}
}

func TestTenantMembershipRepository_ListForUser_ReturnsAllTenantsForUser(t *testing.T) {
	memberships, tenants, admins, pool := newTenantMembershipRepoTestPool(t)
	admin := createMembershipTestAdmin(t, admins, pool, "membership-listuser@example.com")
	tenantA := createMembershipTestTenant(t, tenants, pool, "membership-listuser-a")
	tenantB := createMembershipTestTenant(t, tenants, pool, "membership-listuser-b")

	for _, tenant := range []*Tenant{tenantA, tenantB} {
		tx, err := pool.BeginTenantTx(context.Background(), "", tenant.ID)
		if err != nil {
			t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
		}
		if err := memberships.Create(WithTenantTx(context.Background(), tx), &TenantMembership{UserID: admin.ID, TenantID: tenant.ID, Role: RoleOwner}); err != nil {
			_ = tx.Rollback(context.Background())
			t.Fatalf("Create() returned unexpected error: %v", err)
		}
		if err := tx.Commit(context.Background()); err != nil {
			t.Fatalf("commit returned unexpected error: %v", err)
		}
	}

	// ListForUser must work with only app.user_id set - no active tenant
	// chosen yet (the login case this exists for).
	tx, err := pool.BeginTenantTx(context.Background(), admin.ID, "")
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	ctx := WithTenantTx(context.Background(), tx)

	got, err := memberships.ListForUser(ctx, admin.ID)
	if err != nil {
		t.Fatalf("ListForUser() returned unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(ListForUser()) = %d, want 2", len(got))
	}
	seen := map[string]bool{got[0].TenantID: true, got[1].TenantID: true}
	if !seen[tenantA.ID] || !seen[tenantB.ID] {
		t.Errorf("ListForUser() tenant ids = %v, want both %q and %q", seen, tenantA.ID, tenantB.ID)
	}
}

// TestTenantMembershipRepository_ListForUser_ReturnsTenantNamePlan covers
// SHELL-20/21: each returned TenantMembership carries the owning tenant's
// name/plan, joined in from the tenants table rather than left zero-valued.
func TestTenantMembershipRepository_ListForUser_ReturnsTenantNamePlan(t *testing.T) {
	memberships, tenants, admins, pool := newTenantMembershipRepoTestPool(t)
	admin := createMembershipTestAdmin(t, admins, pool, "membership-nameplan@example.com")
	tenant := createMembershipTestTenant(t, tenants, pool, "membership-nameplan")
	if _, err := pool.Exec(context.Background(), "UPDATE tenants SET plan = $1 WHERE id = $2", "scale", tenant.ID); err != nil {
		t.Fatalf("seeding tenant plan returned unexpected error: %v", err)
	}

	tx, err := pool.BeginTenantTx(context.Background(), "", tenant.ID)
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	ctx := WithTenantTx(context.Background(), tx)

	if err := memberships.Create(ctx, &TenantMembership{UserID: admin.ID, TenantID: tenant.ID, Role: RoleOwner}); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit returned unexpected error: %v", err)
	}

	listTx, err := pool.BeginTenantTx(context.Background(), admin.ID, "")
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	defer func() { _ = listTx.Rollback(context.Background()) }()

	got, err := memberships.ListForUser(WithTenantTx(context.Background(), listTx), admin.ID)
	if err != nil {
		t.Fatalf("ListForUser() returned unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(ListForUser()) = %d, want 1", len(got))
	}
	if got[0].Name != "membership-nameplan" {
		t.Errorf("ListForUser()[0].Name = %q, want %q", got[0].Name, "membership-nameplan")
	}
	if got[0].Plan != "scale" {
		t.Errorf("ListForUser()[0].Plan = %q, want %q", got[0].Plan, "scale")
	}
}

// TestTenantMembershipRepository_ListForUser_EmptyPlanPassthrough covers
// SHELL-21's edge case: a tenant with plan = ” (explicitly cleared, distinct
// from the schema's 'free' default) comes back with Plan: "" unchanged - no
// default invented in the repository.
func TestTenantMembershipRepository_ListForUser_EmptyPlanPassthrough(t *testing.T) {
	memberships, tenants, admins, pool := newTenantMembershipRepoTestPool(t)
	admin := createMembershipTestAdmin(t, admins, pool, "membership-emptyplan@example.com")
	tenant := createMembershipTestTenant(t, tenants, pool, "membership-emptyplan")
	if _, err := pool.Exec(context.Background(), "UPDATE tenants SET plan = '' WHERE id = $1", tenant.ID); err != nil {
		t.Fatalf("seeding empty tenant plan returned unexpected error: %v", err)
	}

	tx, err := pool.BeginTenantTx(context.Background(), "", tenant.ID)
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	ctx := WithTenantTx(context.Background(), tx)

	if err := memberships.Create(ctx, &TenantMembership{UserID: admin.ID, TenantID: tenant.ID, Role: RoleOwner}); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit returned unexpected error: %v", err)
	}

	listTx, err := pool.BeginTenantTx(context.Background(), admin.ID, "")
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	defer func() { _ = listTx.Rollback(context.Background()) }()

	got, err := memberships.ListForUser(WithTenantTx(context.Background(), listTx), admin.ID)
	if err != nil {
		t.Fatalf("ListForUser() returned unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(ListForUser()) = %d, want 1", len(got))
	}
	if got[0].Plan != "" {
		t.Errorf("ListForUser()[0].Plan = %q, want empty string unchanged (no backend default invented)", got[0].Plan)
	}
}

// TestTenantMembershipRepository_ListForUser_OrderingUnchangedWithJoin
// re-asserts the pre-existing created_at ASC ordering guarantee still holds
// once the query gained its JOIN tenants - the join must not perturb row
// order.
func TestTenantMembershipRepository_ListForUser_OrderingUnchangedWithJoin(t *testing.T) {
	memberships, tenants, admins, pool := newTenantMembershipRepoTestPool(t)
	admin := createMembershipTestAdmin(t, admins, pool, "membership-order@example.com")
	tenantA := createMembershipTestTenant(t, tenants, pool, "membership-order-a")
	tenantB := createMembershipTestTenant(t, tenants, pool, "membership-order-b")

	for _, tenant := range []*Tenant{tenantA, tenantB} {
		tx, err := pool.BeginTenantTx(context.Background(), "", tenant.ID)
		if err != nil {
			t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
		}
		if err := memberships.Create(WithTenantTx(context.Background(), tx), &TenantMembership{UserID: admin.ID, TenantID: tenant.ID, Role: RoleOwner}); err != nil {
			_ = tx.Rollback(context.Background())
			t.Fatalf("Create() returned unexpected error: %v", err)
		}
		if err := tx.Commit(context.Background()); err != nil {
			t.Fatalf("commit returned unexpected error: %v", err)
		}
	}

	tx, err := pool.BeginTenantTx(context.Background(), admin.ID, "")
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	ctx := WithTenantTx(context.Background(), tx)

	got, err := memberships.ListForUser(ctx, admin.ID)
	if err != nil {
		t.Fatalf("ListForUser() returned unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(ListForUser()) = %d, want 2", len(got))
	}
	if got[0].TenantID != tenantA.ID || got[1].TenantID != tenantB.ID {
		t.Errorf("ListForUser() order = [%q, %q], want [%q, %q] (created_at ASC, tenantA created first)", got[0].TenantID, got[1].TenantID, tenantA.ID, tenantB.ID)
	}
}

func TestTenantMembershipRepository_ListForTenant_ReturnsAllMembers(t *testing.T) {
	memberships, tenants, admins, pool := newTenantMembershipRepoTestPool(t)
	owner := createMembershipTestAdmin(t, admins, pool, "membership-listtenant-owner@example.com")
	viewer := createMembershipTestAdmin(t, admins, pool, "membership-listtenant-viewer@example.com")
	tenant := createMembershipTestTenant(t, tenants, pool, "membership-listtenant")

	tx, err := pool.BeginTenantTx(context.Background(), "", tenant.ID)
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	ctx := WithTenantTx(context.Background(), tx)

	if err := memberships.Create(ctx, &TenantMembership{UserID: owner.ID, TenantID: tenant.ID, Role: RoleOwner}); err != nil {
		t.Fatalf("Create(owner) returned unexpected error: %v", err)
	}
	if err := memberships.Create(ctx, &TenantMembership{UserID: viewer.ID, TenantID: tenant.ID, Role: RoleViewer}); err != nil {
		t.Fatalf("Create(viewer) returned unexpected error: %v", err)
	}

	got, err := memberships.ListForTenant(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("ListForTenant() returned unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(ListForTenant()) = %d, want 2", len(got))
	}
}

func TestTenantMembershipRepository_UpdateRole_PersistsNewRole(t *testing.T) {
	memberships, tenants, admins, pool := newTenantMembershipRepoTestPool(t)
	admin := createMembershipTestAdmin(t, admins, pool, "membership-updaterole@example.com")
	tenant := createMembershipTestTenant(t, tenants, pool, "membership-updaterole")

	tx, err := pool.BeginTenantTx(context.Background(), "", tenant.ID)
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	ctx := WithTenantTx(context.Background(), tx)

	if err := memberships.Create(ctx, &TenantMembership{UserID: admin.ID, TenantID: tenant.ID, Role: RoleViewer}); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}

	if err := memberships.UpdateRole(ctx, admin.ID, tenant.ID, RoleOperator); err != nil {
		t.Fatalf("UpdateRole() returned unexpected error: %v", err)
	}

	got, err := memberships.ListForTenant(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("ListForTenant() returned unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Role != RoleOperator {
		t.Fatalf("ListForTenant() = %+v, want exactly one membership with role %q", got, RoleOperator)
	}
}

// TestTenantMembershipRepository_UpdateRole_LastOwnerDemotion_ErrLastOwner
// covers the lockout half of ADM-06 for a role change: demoting the only
// owner a tenant has is refused, and the membership keeps its role.
func TestTenantMembershipRepository_UpdateRole_LastOwnerDemotion_ErrLastOwner(t *testing.T) {
	memberships, tenants, admins, pool := newTenantMembershipRepoTestPool(t)
	admin := createMembershipTestAdmin(t, admins, pool, "membership-updaterole-lastowner@example.com")
	tenant := createMembershipTestTenant(t, tenants, pool, "membership-updaterole-lastowner")

	tx, err := pool.BeginTenantTx(context.Background(), "", tenant.ID)
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	ctx := WithTenantTx(context.Background(), tx)

	if err := memberships.Create(ctx, &TenantMembership{UserID: admin.ID, TenantID: tenant.ID, Role: RoleOwner}); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}

	if err := memberships.UpdateRole(ctx, admin.ID, tenant.ID, RoleViewer); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("UpdateRole() error = %v, want ErrLastOwner", err)
	}

	got, err := memberships.ListForTenant(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("ListForTenant() returned unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Role != RoleOwner {
		t.Fatalf("ListForTenant() after refused demotion = %+v, want the membership still %q", got, RoleOwner)
	}
}

func TestTenantMembershipRepository_UpdateRole_UnknownPair_ErrNotFound(t *testing.T) {
	memberships, tenants, admins, pool := newTenantMembershipRepoTestPool(t)
	admin := createMembershipTestAdmin(t, admins, pool, "membership-updaterole-unknown@example.com")
	tenant := createMembershipTestTenant(t, tenants, pool, "membership-updaterole-unknown")

	tx, err := pool.BeginTenantTx(context.Background(), "", tenant.ID)
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	ctx := WithTenantTx(context.Background(), tx)

	err = memberships.UpdateRole(ctx, admin.ID, tenant.ID, RoleOperator)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateRole() error = %v, want ErrNotFound", err)
	}
}

func TestTenantMembershipRepository_Delete_NonOwner_Succeeds(t *testing.T) {
	memberships, tenants, admins, pool := newTenantMembershipRepoTestPool(t)
	owner := createMembershipTestAdmin(t, admins, pool, "membership-delete-owner@example.com")
	viewer := createMembershipTestAdmin(t, admins, pool, "membership-delete-viewer@example.com")
	tenant := createMembershipTestTenant(t, tenants, pool, "membership-delete")

	tx, err := pool.BeginTenantTx(context.Background(), "", tenant.ID)
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	ctx := WithTenantTx(context.Background(), tx)

	if err := memberships.Create(ctx, &TenantMembership{UserID: owner.ID, TenantID: tenant.ID, Role: RoleOwner}); err != nil {
		t.Fatalf("Create(owner) returned unexpected error: %v", err)
	}
	if err := memberships.Create(ctx, &TenantMembership{UserID: viewer.ID, TenantID: tenant.ID, Role: RoleViewer}); err != nil {
		t.Fatalf("Create(viewer) returned unexpected error: %v", err)
	}

	if err := memberships.Delete(ctx, viewer.ID, tenant.ID); err != nil {
		t.Fatalf("Delete() returned unexpected error: %v", err)
	}

	got, err := memberships.ListForTenant(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("ListForTenant() returned unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].UserID != owner.ID {
		t.Fatalf("ListForTenant() after delete = %+v, want only the owner", got)
	}
}

func TestTenantMembershipRepository_Delete_LastOwner_ErrLastOwnerNoRowRemoved(t *testing.T) {
	memberships, tenants, admins, pool := newTenantMembershipRepoTestPool(t)
	owner := createMembershipTestAdmin(t, admins, pool, "membership-delete-lastowner@example.com")
	tenant := createMembershipTestTenant(t, tenants, pool, "membership-delete-lastowner")

	tx, err := pool.BeginTenantTx(context.Background(), "", tenant.ID)
	if err != nil {
		t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	ctx := WithTenantTx(context.Background(), tx)

	if err := memberships.Create(ctx, &TenantMembership{UserID: owner.ID, TenantID: tenant.ID, Role: RoleOwner}); err != nil {
		t.Fatalf("Create() returned unexpected error: %v", err)
	}

	err = memberships.Delete(ctx, owner.ID, tenant.ID)
	if !errors.Is(err, ErrLastOwner) {
		t.Fatalf("Delete() error = %v, want ErrLastOwner", err)
	}

	got, err := memberships.ListForTenant(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("ListForTenant() returned unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("len(ListForTenant()) after blocked delete = %d, want 1 (no row removed)", len(got))
	}
}
