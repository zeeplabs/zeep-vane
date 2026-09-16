//go:build integration

package audit

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/dbtest"
)

func testDatabaseURL(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	return dsn
}

func newLogForTest(t *testing.T) (*Log, *db.Pool) {
	t.Helper()
	pool := newTenantScopedTestPool(t, "../db/migrations")
	return NewLog(pool), pool
}

func createTestAdminForAudit(t *testing.T, pool *db.Pool) *db.User {
	t.Helper()
	ctx := context.Background()

	// Creating identity rows here races other packages' bulk clears of the
	// shared `users` table -
	// this transiently creates a real owner-role row in the shared
	// `admins` table. `go test ./...` runs internal/audit, internal/db,
	// internal/api, and internal/cli as separate concurrent processes
	// against the same TEST_DATABASE_URL, so an unlocked create here can
	// corrupt another package's owner-count-sensitive test mid-window.
	// See dbtest.LockUsersTable's doc comment.
	dbtest.LockUsersTable(t, ctx, testDatabaseURL(t))

	admins := db.NewUserRepository(pool)
	email := fmt.Sprintf("audit-log-test-%d@example.com", time.Now().UnixNano())

	admin := &db.User{Email: email, PasswordHash: "hash"}
	if err := admins.Create(ctx, admin); err != nil {
		t.Fatalf("admins.Create() returned unexpected error: %v", err)
	}
	return admin
}

func TestLog_Record_InsertsRowWithTimestamp(t *testing.T) {
	log, pool := newLogForTest(t)
	ctx := context.Background()
	actor := createTestAdminForAudit(t, pool)
	target := createTestAdminForAudit(t, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM admin_audit_log WHERE actor_id = $1", actor.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM users WHERE id IN ($1, $2)", actor.ID, target.ID)
	})

	before := time.Now().Add(-1 * time.Second)
	if err := log.Record(ctx, actor.ID, target.ID, "invited"); err != nil {
		t.Fatalf("Record() returned unexpected error: %v", err)
	}

	var gotActorID, gotTargetID, gotAction string
	var gotCreatedAt time.Time
	row := pool.QueryRow(ctx,
		"SELECT actor_id, target_id, action, created_at FROM admin_audit_log WHERE actor_id = $1", actor.ID)
	if err := row.Scan(&gotActorID, &gotTargetID, &gotAction, &gotCreatedAt); err != nil {
		t.Fatalf("querying inserted row returned unexpected error: %v", err)
	}

	if gotActorID != actor.ID {
		t.Errorf("actor_id = %q, want %q", gotActorID, actor.ID)
	}
	if gotTargetID != target.ID {
		t.Errorf("target_id = %q, want %q", gotTargetID, target.ID)
	}
	if gotAction != "invited" {
		t.Errorf("action = %q, want %q", gotAction, "invited")
	}
	if gotCreatedAt.Before(before) {
		t.Errorf("created_at = %v, want a timestamp at or after %v", gotCreatedAt, before)
	}
}

func TestLog_Record_SurvivesReferencedAdminRemoval(t *testing.T) {
	log, pool := newLogForTest(t)
	ctx := context.Background()
	actor := createTestAdminForAudit(t, pool)
	target := createTestAdminForAudit(t, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM admin_audit_log WHERE actor_id = $1", actor.ID)
	})

	if err := log.Record(ctx, actor.ID, target.ID, "removed"); err != nil {
		t.Fatalf("Record() returned unexpected error: %v", err)
	}

	// Remove both referenced admins - the audit row must not be cascaded
	// away, since it is the historical record of the removal itself.
	if _, err := pool.Exec(ctx, "DELETE FROM users WHERE id IN ($1, $2)", actor.ID, target.ID); err != nil {
		t.Fatalf("deleting referenced admins returned unexpected error: %v", err)
	}

	var count int
	row := pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM admin_audit_log WHERE actor_id = $1 AND target_id = $2 AND action = $3",
		actor.ID, target.ID, "removed")
	if err := row.Scan(&count); err != nil {
		t.Fatalf("querying surviving row returned unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("admin_audit_log rows for removed admin = %d, want 1 (row must survive admin deletion)", count)
	}
}

// newTenantScopedTestPool returns a migrated pool whose every connection
// has app.tenant_id preset (at session level, via the connection's
// `options` parameter) to a throwaway fixture tenant. Every tenant-scoped
// table's tenant_id defaults from current_setting('app.tenant_id', true)
// and is NOT NULL since 0024, so a fixture INSERT made outside a tenant
// context is rejected.
func newTenantScopedTestPool(t *testing.T, migrationsDir string) *db.Pool {
	t.Helper()
	dsn := testDatabaseURL(t)
	if err := db.MigrateUp(dsn, migrationsDir); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx := context.Background()
	bootstrapPool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	var tenantID string
	if err := bootstrapPool.QueryRow(ctx, "INSERT INTO tenants (name) VALUES ($1) RETURNING id", "fixture-tenant").Scan(&tenantID); err != nil {
		bootstrapPool.Close()
		t.Fatalf("seeding fixture tenant returned unexpected error: %v", err)
	}
	bootstrapPool.Close()

	pool, err := db.NewPool(ctx, dbtest.TenantScopedDSN(dsn, tenantID))
	if err != nil {
		t.Fatalf("NewPool() (tenant-scoped) returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM tenants WHERE id = $1", tenantID)
	})

	return pool
}
