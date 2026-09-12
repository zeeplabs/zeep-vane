//go:build integration

package api

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/auth"
	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/dbtest"
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

// currentAPITestTenant is the fixture tenant of the pool most recently
// built by newAPITenantScopedPool. This package's integration tests run
// sequentially (none calls t.Parallel), and each starts by building its
// router - and therefore its pool - so this is always the tenant of the
// test currently executing. It exists so seedSessionForRole can issue a
// session bound to the same tenant the test's own fixtures were written
// under, without every one of this package's ~120 call sites having to
// thread a tenant id through.
var currentAPITestTenant string

// apiTestTenantID returns the fixture tenant of the pool this test built.
func apiTestTenantID(t *testing.T) string {
	t.Helper()
	if currentAPITestTenant == "" {
		t.Fatal("apiTestTenantID called before newAPITenantScopedPool - no fixture tenant exists yet")
	}
	return currentAPITestTenant
}

// newAPITenantScopedPool returns a migrated pool whose every connection
// has app.tenant_id preset (at session level, via the connection's
// `options` parameter) to a throwaway fixture tenant, plus that tenant's
// id.
//
// Every tenant-scoped table's tenant_id defaults from
// current_setting('app.tenant_id', true) and is NOT NULL since 0024, so a
// fixture INSERT made outside a tenant context is rejected; and the
// unauthenticated handlers this package exercises (public status page,
// /uploads/logo, /api/instance/branding) resolve their tenant the same
// way. Pinning it per connection makes both deterministic without every
// test opening its own transaction.
func newAPITenantScopedPool(t *testing.T) (*db.Pool, string) {
	t.Helper()
	dsn := testDatabaseURL(t)
	if err := db.MigrateUp(dsn, "../db/migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx := context.Background()
	bootstrapPool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}

	// Insert the fixture user + session row backing auth.IssueTestSessionID
	// (a fixed UUID sentinel that integration tests pass to
	// IssueSessionWithTenant in place of a real sessions-table row). Shared
	// with internal/cli's fixture via dbtest so each package's suite passes
	// in isolation instead of depending on the other package's fixture
	// having run first in a shared TEST_DATABASE_URL.
	dbtest.SeedIssueTestSession(t, dsn)

	var tenantID string
	if err := bootstrapPool.QueryRow(ctx, "INSERT INTO tenants (name) VALUES ($1) RETURNING id",
		fmt.Sprintf("api-test-tenant-%d", time.Now().UnixNano())).Scan(&tenantID); err != nil {
		bootstrapPool.Close()
		t.Fatalf("seeding fixture tenant returned unexpected error: %v", err)
	}
	bootstrapPool.Close()

	pool, err := db.NewPool(ctx, dbtest.TenantScopedDSN(dsn, tenantID))
	if err != nil {
		t.Fatalf("NewPool() (tenant-scoped) returned unexpected error: %v", err)
	}
	currentAPITestTenant = tenantID
	t.Cleanup(pool.Close)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM tenants WHERE id = $1", tenantID)
	})

	return pool, tenantID
}

// seedSessionForRole creates a user, a throwaway tenant, and a membership
// giving that user role in it, then issues a session token with that
// tenant active. Since multi-tenancy-core a role is a tenant_memberships
// row rather than a users column, so a token is only as privileged as the
// membership behind it - which is exactly what TenantContext resolves and
// RequireRole enforces.
//
// It takes LockUsersTable itself rather than relying on each call site to
// remember to: `go test ./...` runs internal/db, internal/api, and
// internal/cli as separate concurrent processes against the same
// TEST_DATABASE_URL, so a test creating identity rows here can otherwise
// race another package's bulk clear of the users table.
func seedSessionForRole(t *testing.T, users *db.UserRepository, role string) string {
	t.Helper()
	if currentAPITestTenant == "" {
		t.Fatal("seedSessionForRole called before newAPITenantScopedPool - no fixture tenant to bind the session to")
	}
	return seedSessionForTenant(t, users, currentAPITestTenant, role)
}

// seedSessionForTenant creates a user plus a tenant_membership giving them
// role in tenantID, and issues a session token with that tenant active.
// Since multi-tenancy-core a role is a tenant_memberships row rather than
// a users column, so a token is only as privileged as the membership
// behind it - which is what TenantContext resolves and RequireRole
// enforces.
//
// It takes LockUsersTable itself rather than relying on each call site to
// remember to: `go test ./...` runs internal/db, internal/api, and
// internal/cli as separate concurrent processes against the same
// TEST_DATABASE_URL, so a test creating identity rows here can otherwise
// race another package's bulk clear of the users table.
func seedSessionForTenant(t *testing.T, users *db.UserRepository, tenantID, role string) string {
	t.Helper()
	ctx := context.Background()
	dbtest.LockUsersTable(t, ctx, testDatabaseURL(t))

	user := &db.User{Email: uniqueTestEmail(t), PasswordHash: "hash"}
	if err := users.Create(ctx, user); err != nil {
		t.Fatalf("users.Create() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = users.Delete(context.Background(), user.ID) })

	seedMembership(t, user.ID, tenantID, role)

	token, err := auth.IssueSessionWithTenant(user.ID, tenantID, auth.IssueTestSessionID, middlewareTestSecret)
	if err != nil {
		t.Fatalf("auth.IssueSessionWithTenant() returned unexpected error: %v", err)
	}
	return token
}

// seedMembership gives userID the given role in tenantID, using its own
// short-lived tenant transaction so tenant_memberships' RLS WITH CHECK is
// satisfied.
func seedMembership(t *testing.T, userID, tenantID, role string) {
	t.Helper()
	ctx := context.Background()
	pool, err := db.NewPool(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	defer pool.Close()

	withTenantTx(t, pool, tenantID, func(txCtx context.Context) {
		if _, err := pool.Exec(txCtx,
			"INSERT INTO tenant_memberships (user_id, tenant_id, role) VALUES ($1, $2, $3)", userID, tenantID, role,
		); err != nil {
			t.Fatalf("seeding tenant membership returned unexpected error: %v", err)
		}
	})
}

// ptr returns a pointer to v - db.TenantUpdate's fields are pointers
// (nil = leave unchanged), so tests setting one need an addressable value.
func ptr[T any](v T) *T { return &v }

// newPlainTestPool returns a migrated pool with no app.tenant_id preset -
// for the handful of tests whose subject is what happens when no tenant is
// active at all.
func newPlainTestPool(t *testing.T) *db.Pool {
	t.Helper()
	dsn := testDatabaseURL(t)
	if err := db.MigrateUp(dsn, "../db/migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}
	pool, err := db.NewPool(context.Background(), dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
