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
	dsn := testDatabaseURL(t)

	if err := db.MigrateUp(dsn, "../db/migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	return NewLog(pool), pool
}

func createTestAdminForAudit(t *testing.T, pool *db.Pool) *db.Admin {
	t.Helper()
	ctx := context.Background()

	// db.AdminRepository.Create always inserts with the `admins.role`
	// column's database default, which is `owner` (migration 0009) -
	// this transiently creates a real owner-role row in the shared
	// `admins` table. `go test ./...` runs internal/audit, internal/db,
	// internal/api, and internal/cli as separate concurrent processes
	// against the same TEST_DATABASE_URL, so an unlocked create here can
	// corrupt another package's owner-count-sensitive test mid-window.
	// See dbtest.LockAdminsTable's doc comment.
	dbtest.LockAdminsTable(t, ctx, testDatabaseURL(t))

	admins := db.NewAdminRepository(pool)
	email := fmt.Sprintf("audit-log-test-%d@example.com", time.Now().UnixNano())

	admin := &db.Admin{Email: email, PasswordHash: "hash"}
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
		_, _ = pool.Exec(ctx, "DELETE FROM admins WHERE id IN ($1, $2)", actor.ID, target.ID)
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
	if _, err := pool.Exec(ctx, "DELETE FROM admins WHERE id IN ($1, $2)", actor.ID, target.ID); err != nil {
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
