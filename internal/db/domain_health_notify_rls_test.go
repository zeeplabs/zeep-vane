//go:build integration

package db

import (
	"context"
	"fmt"
	"testing"
)

// TestListMembersWithEmail_RLSFailsClosedWithoutTenantScope pins the exact
// mechanism behind the domain-health-monitoring notification bug: calling
// ListMembersWithEmail (the recipient source for every notify.Service email,
// including domain expiring/NS drift alerts) on a connection that has no
// app.tenant_id set silently resolves to zero rows - not an error - because
// tenant_memberships' RLS policy is `user_id = ... OR tenant_id = ...` and
// both sides are NULL when neither session variable is set (NULL OR NULL is
// NULL, never true; see migration 0024's tenant_isolation policy comment).
// DomainHealthScheduler used to call the notifier with a bare ctx carrying
// no tenant transaction at all, so every domain expiring/NS drift alert
// vanished without any error or log line. This test proves both halves: the
// fail-closed behavior when unscoped, and that scoping via app.tenant_id (as
// BeginTenantTx/WithTenantTx do) is what makes the real row visible again -
// so a regression that drops the tenant-scoped call around a notifier
// invocation shows up as this test failing, not as a silent production gap.
func TestListMembersWithEmail_RLSFailsClosedWithoutTenantScope(t *testing.T) {
	pool := newRLSTestPool(t)
	ctx := context.Background()

	// ListMembersWithEmail joins users, which newRLSTestPool's grant list
	// does not cover (it grants tenants/tenant_memberships/tenant_invites/
	// services/integrations) - extend it for this test only.
	if _, err := pool.Exec(ctx, "GRANT SELECT ON users TO "+rlsTestRole); err != nil {
		t.Fatalf("granting SELECT on users to %s returned unexpected error: %v", rlsTestRole, err)
	}

	tenants := NewTenantRepository(pool)
	admins := NewUserRepository(pool)
	memberships := NewTenantMembershipRepository(pool)

	suffix := tUniqueSuffix()
	tenant := createMembershipTestTenant(t, tenants, pool, fmt.Sprintf("notify-rls-%d", suffix))
	owner := createMembershipTestAdmin(t, admins, pool, fmt.Sprintf("notify-rls-owner-%d@example.com", suffix))

	if err := memberships.Create(ctx, &TenantMembership{UserID: owner.ID, TenantID: tenant.ID, Role: RoleOwner}); err != nil {
		t.Fatalf("Create(owner) returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		cleanupTx, cleanupCtx := beginRLSTx(t, pool, "", tenant.ID)
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM tenant_memberships WHERE tenant_id = $1", tenant.ID)
		_ = cleanupTx.Commit(context.Background())
	})

	// Unscoped: same role, but neither app.user_id nor app.tenant_id set -
	// this is what the pre-fix scheduler effectively did (a plain ctx over a
	// pool connection with no tenant session variables at all).
	unscopedTx, err := pool.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() returned unexpected error: %v", err)
	}
	defer func() { _ = unscopedTx.Rollback(ctx) }()
	if _, err := unscopedTx.Exec(ctx, "SET ROLE "+rlsTestRole); err != nil {
		t.Fatalf("SET ROLE returned unexpected error: %v", err)
	}
	unscopedCtx := WithTenantTx(ctx, unscopedTx)

	members, err := memberships.ListMembersWithEmail(unscopedCtx, tenant.ID)
	if err != nil {
		t.Fatalf("ListMembersWithEmail() unscoped returned unexpected error: %v", err)
	}
	if len(members) != 0 {
		t.Fatalf("ListMembersWithEmail() unscoped = %v, want empty (RLS must fail closed with no session variables)", members)
	}

	// Tenant-scoped: app.tenant_id set, exactly what BeginTenantTx/
	// WithTenantTx now wrap the notifier call in - the real row must come
	// back.
	scopedTx, scopedCtx := beginRLSTx(t, pool, "", tenant.ID)
	defer func() { _ = scopedTx.Rollback(context.Background()) }()

	members, err = memberships.ListMembersWithEmail(scopedCtx, tenant.ID)
	if err != nil {
		t.Fatalf("ListMembersWithEmail() tenant-scoped returned unexpected error: %v", err)
	}
	if len(members) != 1 || members[0].Email != owner.Email {
		t.Fatalf("ListMembersWithEmail() tenant-scoped = %v, want exactly the owner (%s)", members, owner.Email)
	}
}
