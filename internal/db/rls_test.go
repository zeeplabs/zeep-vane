//go:build integration

package db

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
)

// rlsTestRole is a non-superuser Postgres role this suite runs its
// assertions as. This matters because the disposable Postgres container
// AGENTS.md §3 prescribes (postgres:16-alpine with POSTGRES_USER=vane)
// makes that bootstrap user a superuser, and a superuser bypasses RLS
// unconditionally - ALTER TABLE ... FORCE ROW LEVEL SECURITY (0024) only
// binds non-superuser roles, including the table owner. Without this,
// every assertion below would pass for the wrong reason (superuser bypass)
// rather than because the policy actually filters. A real deployment's
// application role is expected to be a plain, non-superuser role for the
// same reason RLS exists at all; this role stands in for that here.
const rlsTestRole = "vane_rls_test"

// newRLSTestPool boots a migrated pool and ensures rlsTestRole exists with
// DML access to the tables this suite exercises.
func newRLSTestPool(t *testing.T) *Pool {
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

	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`DO $$ BEGIN
			IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '`+rlsTestRole+`') THEN
				CREATE ROLE `+rlsTestRole+` NOLOGIN;
			END IF;
		END $$;`,
	); err != nil {
		t.Fatalf("creating %s role returned unexpected error: %v", rlsTestRole, err)
	}
	if _, err := pool.Exec(ctx,
		"GRANT SELECT, INSERT, UPDATE, DELETE ON tenants, tenant_memberships, tenant_invites, services TO "+rlsTestRole,
	); err != nil {
		t.Fatalf("granting DML to %s returned unexpected error: %v", rlsTestRole, err)
	}

	return pool
}

// beginRLSTx opens a transaction, downgrades it from the (superuser)
// connection role to rlsTestRole via SET ROLE - a superuser may SET ROLE to
// any role without needing prior membership - then sets whichever of
// app.user_id/app.tenant_id are non-empty, exactly like Pool.BeginTenantTx
// but bound to a role RLS actually restricts. Returns the tx (caller must
// Commit or Rollback) and a context wired for pool.* calls to run on it.
func beginRLSTx(t *testing.T, pool *Pool, userID, tenantID string) (pgx.Tx, context.Context) {
	t.Helper()
	ctx := context.Background()

	tx, err := pool.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() returned unexpected error: %v", err)
	}
	if _, err := tx.Exec(ctx, "SET ROLE "+rlsTestRole); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("SET ROLE returned unexpected error: %v", err)
	}
	if userID != "" {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.user_id', $1, true)", userID); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("set_config(app.user_id) returned unexpected error: %v", err)
		}
	}
	if tenantID != "" {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("set_config(app.tenant_id) returned unexpected error: %v", err)
		}
	}

	return tx, WithTenantTx(ctx, tx)
}

// seedTenantWithService creates and commits one tenant plus one service row
// owned by it, generating the tenant's id up front (SELECT gen_random_uuid())
// and setting app.tenant_id to that same value before either INSERT - both
// tenants and services are FORCE ROW LEVEL SECURITY, so the insert's WITH
// CHECK requires the session setting to already match the row being written
// (see 0024's policy comments). Returns the committed tenant id.
func seedTenantWithService(t *testing.T, pool *Pool, namePrefix string) (tenantID string) {
	t.Helper()
	ctx := context.Background()

	if err := pool.QueryRow(ctx, "SELECT gen_random_uuid()").Scan(&tenantID); err != nil {
		t.Fatalf("generating tenant id returned unexpected error: %v", err)
	}

	tx, txCtx := beginRLSTx(t, pool, "", tenantID)

	if _, err := pool.Exec(txCtx,
		"INSERT INTO tenants (id, name) VALUES ($1, $2)", tenantID, namePrefix+"-tenant",
	); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("seed tenant insert returned unexpected error: %v", err)
	}

	if _, err := pool.Exec(txCtx,
		"INSERT INTO services (name, slo_id) VALUES ($1, $2)", namePrefix+"-service", namePrefix+"-slo",
	); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("seed service insert returned unexpected error: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit returned unexpected error: %v", err)
	}

	t.Cleanup(func() {
		cleanupTx, cleanupCtx := beginRLSTx(t, pool, "", tenantID)
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM services WHERE tenant_id = $1", tenantID)
		_, _ = pool.Exec(cleanupCtx, "DELETE FROM tenants WHERE id = $1", tenantID)
		_ = cleanupTx.Commit(context.Background())
	})

	return tenantID
}

// TestRLS_NoTenantContext_ReturnsZeroRows proves TENANT-03: a query that
// runs in a transaction with app.tenant_id unset never returns another
// tenant's rows, or any row at all - fail-closed, not an error and not
// "everything". This exercises services with a plain, deliberately
// unfiltered "SELECT id FROM services" - exactly the shape of query a
// repository that forgot "WHERE tenant_id = ?" would run.
func TestRLS_NoTenantContext_ReturnsZeroRows(t *testing.T) {
	pool := newRLSTestPool(t)
	prefix := fmt.Sprintf("rls-nocontext-%d", tUniqueSuffix())
	seedTenantWithService(t, pool, prefix)

	// SET ROLE only - no app.tenant_id set for this transaction.
	tx, ctx := beginRLSTx(t, pool, "", "")
	defer func() { _ = tx.Rollback(context.Background()) }()

	rows, err := pool.Query(ctx, "SELECT id FROM services")
	if err != nil {
		t.Fatalf("Query() returned unexpected error: %v", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err() returned unexpected error: %v", err)
	}

	if count != 0 {
		t.Errorf("services rows visible with no app.tenant_id set = %d, want 0 (fail-closed)", count)
	}
}

// TestRLS_TenantA_NeverSeesTenantB_EvenWithUnfilteredQuery proves
// TENANT-01/02: two tenants each with their own service row, queried from
// tenant A's session with a raw "SELECT * FROM services" that deliberately
// omits "WHERE tenant_id = ?" - RLS must still only return A's row, never
// B's, even though the query itself does nothing to ask for that.
func TestRLS_TenantA_NeverSeesTenantB_EvenWithUnfilteredQuery(t *testing.T) {
	pool := newRLSTestPool(t)
	prefixA := fmt.Sprintf("rls-tenant-a-%d", tUniqueSuffix())
	prefixB := fmt.Sprintf("rls-tenant-b-%d", tUniqueSuffix())
	tenantA := seedTenantWithService(t, pool, prefixA)
	_ = seedTenantWithService(t, pool, prefixB)

	tx, ctx := beginRLSTx(t, pool, "", tenantA)
	defer func() { _ = tx.Rollback(context.Background()) }()

	rows, err := pool.Query(ctx, "SELECT name FROM services")
	if err != nil {
		t.Fatalf("Query() returned unexpected error: %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("Scan() returned unexpected error: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err() returned unexpected error: %v", err)
	}

	wantName := prefixA + "-service"
	if len(names) != 1 || names[0] != wantName {
		t.Fatalf("services visible from tenant A's session = %v, want exactly [%q]", names, wantName)
	}
	for _, name := range names {
		if name == prefixB+"-service" {
			t.Fatalf("tenant A's session saw tenant B's service %q - cross-tenant leak", name)
		}
	}
}

// TestRLS_TenantB_NeverSeesTenantA_Symmetric is the mirror of the above -
// isolation must hold in both directions, not just because tenant A
// happened to be seeded first.
func TestRLS_TenantB_NeverSeesTenantA_Symmetric(t *testing.T) {
	pool := newRLSTestPool(t)
	prefixA := fmt.Sprintf("rls-sym-a-%d", tUniqueSuffix())
	prefixB := fmt.Sprintf("rls-sym-b-%d", tUniqueSuffix())
	_ = seedTenantWithService(t, pool, prefixA)
	tenantB := seedTenantWithService(t, pool, prefixB)

	tx, ctx := beginRLSTx(t, pool, "", tenantB)
	defer func() { _ = tx.Rollback(context.Background()) }()

	var count int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM services WHERE name = $1", prefixA+"-service").Scan(&count); err != nil {
		t.Fatalf("QueryRow() returned unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("tenant B's session sees %d rows matching tenant A's service name, want 0", count)
	}

	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM services WHERE name = $1", prefixB+"-service").Scan(&count); err != nil {
		t.Fatalf("QueryRow() returned unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("tenant B's session sees %d rows matching its own service name, want 1", count)
	}
}

// TestRLS_NoTenantContext_InsertRejected proves the fail-closed guarantee
// covers writes too: an INSERT with no app.tenant_id set can never silently
// land on NULL/no tenant - the column's NOT NULL constraint (fed by the
// DEFAULT current_setting expression) rejects it outright, independent of
// role/superuser status.
func TestRLS_NoTenantContext_InsertRejected(t *testing.T) {
	pool := newRLSTestPool(t)

	tx, ctx := beginRLSTx(t, pool, "", "")
	defer func() { _ = tx.Rollback(context.Background()) }()

	_, err := pool.Exec(ctx,
		"INSERT INTO services (name, slo_id) VALUES ($1, $2)", "rls-no-context-insert", "slo-x",
	)
	if err == nil {
		t.Fatal("Exec() with no app.tenant_id set returned nil error, want a NOT NULL violation")
	}
}

var tUniqueCounter int64

// tUniqueSuffix returns a monotonically increasing value so fixtures across
// this file's tests never collide, without depending on wall-clock
// resolution.
func tUniqueSuffix() int64 {
	tUniqueCounter++
	return tUniqueCounter
}
