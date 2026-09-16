//go:build integration

package db

import (
	"context"
	"fmt"
	"net/url"
	"testing"
)

// systemListerRole is a non-superuser LOGIN role this file connects as.
// Unlike rls_test.go's SET ROLE fixture, SystemTenantLister opens its own
// transaction internally - there is no seam to downgrade the role on -
// so proving the 0027 policy (AD-024) actually does the work requires a
// pool whose connections authenticate as a restricted role from the
// start. A superuser bypasses RLS unconditionally and would make these
// assertions pass for the wrong reason.
const systemListerRole = "vane_system_lister_test"

// systemListerRolePassword is the login password granted to
// systemListerRole in the disposable test container only.
const systemListerRolePassword = "vane_system_lister_test_pw"

// newSystemListerPools returns two pools over the same test database: the
// ordinary superuser pool (for seeding) and a second pool connecting as
// systemListerRole, a plain non-superuser role RLS really constrains.
func newSystemListerPools(t *testing.T) (super *Pool, restricted *Pool) {
	t.Helper()
	dsn := testDatabaseURL(t)
	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx := context.Background()
	super, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(super.Close)

	if _, err := super.Exec(ctx,
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '`+systemListerRole+`') THEN
				CREATE ROLE `+systemListerRole+` LOGIN PASSWORD '`+systemListerRolePassword+`';
			END IF;
		END $$;`,
	); err != nil {
		t.Fatalf("creating %s role returned unexpected error: %v", systemListerRole, err)
	}
	if _, err := super.Exec(ctx, "GRANT SELECT ON tenants TO "+systemListerRole); err != nil {
		t.Fatalf("granting SELECT to %s returned unexpected error: %v", systemListerRole, err)
	}

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parsing TEST_DATABASE_URL returned unexpected error: %v", err)
	}
	parsed.User = url.UserPassword(systemListerRole, systemListerRolePassword)

	restricted, err = NewPool(ctx, parsed.String())
	if err != nil {
		t.Fatalf("NewPool(restricted) returned unexpected error: %v", err)
	}
	t.Cleanup(restricted.Close)
	if err := restricted.Ping(ctx); err != nil {
		t.Fatalf("Ping(restricted) returned unexpected error: %v", err)
	}

	return super, restricted
}

// seedTenantWithStatus inserts one tenant with the given status through
// the superuser pool and registers its cleanup, returning its id.
func seedTenantWithStatus(t *testing.T, super *Pool, namePrefix, status string) string {
	t.Helper()
	ctx := context.Background()

	var id string
	if err := super.QueryRow(ctx,
		"INSERT INTO tenants (name, status) VALUES ($1, $2) RETURNING id", namePrefix, status,
	).Scan(&id); err != nil {
		t.Fatalf("seeding tenant returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = super.Exec(context.Background(), "DELETE FROM tenants WHERE id = $1", id) })

	return id
}

// TestSystemTenantLister_List_NonSuperuserRole_ReturnsEveryActiveTenant is
// the AD-024 proof: under a real non-superuser role, where the 0024
// tenant_isolation policy alone yields nothing, the poller's enumeration
// still sees every active tenant because the 0027 system_iteration_read
// policy matches its app.is_system session flag. Suspended tenants stay
// out (TenantRepository.List's own WHERE status = 'active').
func TestSystemTenantLister_List_NonSuperuserRole_ReturnsEveryActiveTenant(t *testing.T) {
	super, restricted := newSystemListerPools(t)

	prefix := fmt.Sprintf("system-lister-%d", tUniqueSuffix())
	activeA := seedTenantWithStatus(t, super, prefix+"-a", "active")
	activeB := seedTenantWithStatus(t, super, prefix+"-b", "active")
	suspended := seedTenantWithStatus(t, super, prefix+"-suspended", "suspended")

	tenants, err := NewSystemTenantLister(restricted).List(context.Background())
	if err != nil {
		t.Fatalf("SystemTenantLister.List() returned unexpected error: %v", err)
	}

	seen := make(map[string]bool, len(tenants))
	for _, tenant := range tenants {
		seen[tenant.ID] = true
	}

	if !seen[activeA] {
		t.Errorf("active tenant %s visible to SystemTenantLister.List() = false, want true", activeA)
	}
	if !seen[activeB] {
		t.Errorf("active tenant %s visible to SystemTenantLister.List() = false, want true", activeB)
	}
	if seen[suspended] {
		t.Errorf("suspended tenant %s visible to SystemTenantLister.List() = true, want false", suspended)
	}
}

// TestTenantRepository_List_NonSuperuserRoleWithoutSystemFlag_ReturnsZeroRows
// is the discrimination half of the test above: the same query, same role,
// same seeded rows, but on a plain context with no app.is_system set stays
// fail-closed. Without this, the test above would still pass if the 0027
// policy had been written to grant SELECT unconditionally.
func TestTenantRepository_List_NonSuperuserRoleWithoutSystemFlag_ReturnsZeroRows(t *testing.T) {
	super, restricted := newSystemListerPools(t)

	prefix := fmt.Sprintf("system-lister-noflag-%d", tUniqueSuffix())
	seedTenantWithStatus(t, super, prefix+"-a", "active")

	tenants, err := NewTenantRepository(restricted).List(context.Background())
	if err != nil {
		t.Fatalf("TenantRepository.List() returned unexpected error: %v", err)
	}

	if len(tenants) != 0 {
		t.Errorf("tenants visible with no app.is_system and no app.tenant_id = %d, want 0 (fail-closed)", len(tenants))
	}
}

// TestSystemTenantLister_List_DoesNotLeakSystemFlagToLaterQueries proves
// the flag's scope stays inside List's own transaction (AD-024's
// trade-off mitigation): a query issued on the same pool right after List
// returns must be fail-closed again, not still privileged.
func TestSystemTenantLister_List_DoesNotLeakSystemFlagToLaterQueries(t *testing.T) {
	super, restricted := newSystemListerPools(t)

	prefix := fmt.Sprintf("system-lister-scope-%d", tUniqueSuffix())
	seedTenantWithStatus(t, super, prefix+"-a", "active")

	if _, err := NewSystemTenantLister(restricted).List(context.Background()); err != nil {
		t.Fatalf("SystemTenantLister.List() returned unexpected error: %v", err)
	}

	var flag string
	if err := restricted.QueryRow(context.Background(),
		"SELECT COALESCE(current_setting('app.is_system', true), '')",
	).Scan(&flag); err != nil {
		t.Fatalf("reading app.is_system returned unexpected error: %v", err)
	}
	if flag == "true" {
		t.Errorf("app.is_system after List() = %q, want it unset outside List's own transaction", flag)
	}

	tenants, err := NewTenantRepository(restricted).List(context.Background())
	if err != nil {
		t.Fatalf("TenantRepository.List() returned unexpected error: %v", err)
	}
	if len(tenants) != 0 {
		t.Errorf("tenants visible after List() returned = %d, want 0 (flag must not outlive its transaction)", len(tenants))
	}
}
