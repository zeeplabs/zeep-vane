//go:build integration

package db

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// auditLogRLSTestRole is a non-superuser Postgres role this suite runs
// ListRecent's assertions as. The disposable Postgres container
// AGENTS.md §3 prescribes (postgres:16-alpine with POSTGRES_USER=vane)
// makes that bootstrap user a superuser, and a superuser always bypasses
// row security regardless of FORCE ROW LEVEL SECURITY (see rls_test.go's
// rlsTestRole for the same rationale, duplicated here rather than reused
// so this file stays scoped to its own task per the co-location rule).
// Without this, every "tenant isolation" assertion below would pass for
// the wrong reason (superuser bypass) instead of because RLS actually
// filters, and every other assertion (capping, ordering, actor-deleted,
// null-label) would risk cross-contamination from other integration
// tests' rows in the same shared admin_audit_log table.
const auditLogRLSTestRole = "vane_audit_log_rls_test"

// newAuditLogRLSTestPool boots a migrated pool and ensures
// auditLogRLSTestRole exists with read access to the two tables
// ListRecent queries.
func newAuditLogRLSTestPool(t *testing.T) *Pool {
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
			IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '`+auditLogRLSTestRole+`') THEN
				CREATE ROLE `+auditLogRLSTestRole+` NOLOGIN;
			END IF;
		END $$;`,
	); err != nil {
		t.Fatalf("creating %s role returned unexpected error: %v", auditLogRLSTestRole, err)
	}
	if _, err := pool.Exec(ctx,
		"GRANT SELECT ON admin_audit_log, users TO "+auditLogRLSTestRole,
	); err != nil {
		t.Fatalf("granting SELECT to %s returned unexpected error: %v", auditLogRLSTestRole, err)
	}

	return pool
}

// beginAuditLogRLSTx opens a transaction, downgrades it from the
// (superuser) connection role to auditLogRLSTestRole via SET ROLE, then
// sets app.tenant_id - exactly like rls_test.go's beginRLSTx, bound to a
// role RLS actually restricts. Returns the tx (caller must Commit or
// Rollback) and a context wired for pool.* calls to run on it.
func beginAuditLogRLSTx(t *testing.T, pool *Pool, tenantID string) (pgx.Tx, context.Context) {
	t.Helper()
	ctx := context.Background()

	tx, err := pool.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() returned unexpected error: %v", err)
	}
	if _, err := tx.Exec(ctx, "SET ROLE "+auditLogRLSTestRole); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("SET ROLE returned unexpected error: %v", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("set_config(app.tenant_id) returned unexpected error: %v", err)
	}

	return tx, WithTenantTx(ctx, tx)
}

// seedAuditLogTenant creates and commits one tenant row (via the
// superuser pool, bypassing RLS - seeding is not what these tests are
// verifying), generating its id up front. Returns the committed tenant id.
func seedAuditLogTenant(t *testing.T, pool *Pool, namePrefix string) (tenantID string) {
	t.Helper()
	ctx := context.Background()

	if err := pool.QueryRow(ctx, "SELECT gen_random_uuid()").Scan(&tenantID); err != nil {
		t.Fatalf("generating tenant id returned unexpected error: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO tenants (id, name) VALUES ($1, $2)", tenantID, namePrefix+"-tenant"); err != nil {
		t.Fatalf("seed tenant insert returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM admin_audit_log WHERE tenant_id = $1", tenantID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM tenants WHERE id = $1", tenantID)
	})

	return tenantID
}

// seedAuditLogEntry inserts one admin_audit_log row directly (via the
// superuser pool, explicit tenant_id - bypassing the DEFAULT/RLS write
// path this test file isn't exercising), for tenantID, with actorID,
// targetLabel and createdAt as given. actorID need not reference an
// existing users row (admin_audit_log has no FK to users, by design -
// audit history must survive the account it references being deleted).
func seedAuditLogEntry(t *testing.T, pool *Pool, tenantID, actorID, action string, targetLabel *string, createdAt time.Time) {
	t.Helper()
	ctx := context.Background()

	var targetID string
	if err := pool.QueryRow(ctx, "SELECT gen_random_uuid()").Scan(&targetID); err != nil {
		t.Fatalf("generating target id returned unexpected error: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO admin_audit_log (tenant_id, actor_id, target_id, target_label, action, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		tenantID, actorID, targetID, targetLabel, action, createdAt,
	); err != nil {
		t.Fatalf("seed admin_audit_log insert returned unexpected error: %v", err)
	}
}

// seedAuditLogUser inserts a real users row (for actor-name/actor-deleted
// coverage) and returns its id. Cleanup is the caller's responsibility -
// TestAuditLogRepository_ListRecent_ActorHardDeleted deliberately deletes
// it itself, mid-test, as the case under test.
func seedAuditLogUser(t *testing.T, pool *Pool, email, name string) string {
	t.Helper()
	ctx := context.Background()

	var userID string
	if err := pool.QueryRow(ctx,
		"INSERT INTO users (email, password_hash, name) VALUES ($1, 'hash', $2) RETURNING id",
		email, name,
	).Scan(&userID); err != nil {
		t.Fatalf("seed user insert returned unexpected error: %v", err)
	}

	return userID
}

func auditLogTUniqueSuffix() int64 {
	auditLogTUniqueCounter++
	return auditLogTUniqueCounter
}

var auditLogTUniqueCounter int64

// TestAuditLogRepository_ListRecent_TenantIsolation covers ACTIVITY-06/08
// (spec.md's Independent Test for the read endpoint): rows seeded across
// two tenants, queried from tenant A's session, must return only A's row
// - RLS enforces this with no application-level filter in ListRecent's
// own query (design.md).
func TestAuditLogRepository_ListRecent_TenantIsolation(t *testing.T) {
	pool := newAuditLogRLSTestPool(t)
	repo := NewAuditLogRepository(pool)

	suffix := auditLogTUniqueSuffix()
	tenantA := seedAuditLogTenant(t, pool, fmt.Sprintf("audit-iso-a-%d", suffix))
	tenantB := seedAuditLogTenant(t, pool, fmt.Sprintf("audit-iso-b-%d", suffix))

	labelA := fmt.Sprintf("Tenant A Target %d", suffix)
	labelB := fmt.Sprintf("Tenant B Target %d", suffix)
	seedAuditLogEntry(t, pool, tenantA, "00000000-0000-0000-0000-000000000001", "invited", &labelA, time.Now())
	seedAuditLogEntry(t, pool, tenantB, "00000000-0000-0000-0000-000000000002", "invited", &labelB, time.Now())

	tx, ctx := beginAuditLogRLSTx(t, pool, tenantA)
	defer func() { _ = tx.Rollback(context.Background()) }()

	entries, err := repo.ListRecent(ctx, 20)
	if err != nil {
		t.Fatalf("ListRecent() returned unexpected error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("ListRecent() returned %d entries, want 1 (tenant A's own row only)", len(entries))
	}
	if entries[0].TargetLabel == nil || *entries[0].TargetLabel != labelA {
		t.Errorf("entries[0].TargetLabel = %v, want %q", entries[0].TargetLabel, labelA)
	}
	for _, entry := range entries {
		if entry.TargetLabel != nil && *entry.TargetLabel == labelB {
			t.Fatalf("tenant A's session saw tenant B's audit entry %q - cross-tenant leak", labelB)
		}
	}
}

// TestAuditLogRepository_ListRecent_CapsAtLimitOrderedByCreatedAtDesc
// covers spec.md AC6/edge case: more than 20 rows exist, ListRecent(ctx,
// 20) returns exactly 20, the 20 most recent, in created_at DESC order.
func TestAuditLogRepository_ListRecent_CapsAtLimitOrderedByCreatedAtDesc(t *testing.T) {
	pool := newAuditLogRLSTestPool(t)
	repo := NewAuditLogRepository(pool)

	suffix := auditLogTUniqueSuffix()
	tenantID := seedAuditLogTenant(t, pool, fmt.Sprintf("audit-cap-%d", suffix))

	const totalRows = 25
	base := time.Now().Add(-time.Hour)
	for i := 0; i < totalRows; i++ {
		label := fmt.Sprintf("Target %d", i)
		seedAuditLogEntry(t, pool, tenantID, "00000000-0000-0000-0000-000000000003", "invited", &label, base.Add(time.Duration(i)*time.Second))
	}

	tx, ctx := beginAuditLogRLSTx(t, pool, tenantID)
	defer func() { _ = tx.Rollback(context.Background()) }()

	entries, err := repo.ListRecent(ctx, 20)
	if err != nil {
		t.Fatalf("ListRecent() returned unexpected error: %v", err)
	}

	if len(entries) != 20 {
		t.Fatalf("ListRecent() returned %d entries, want 20 (hard-capped)", len(entries))
	}
	// The 20 most recent of 25 rows seeded i=0..24 (i=24 newest) are
	// i=5..24, so entries[0] (most recent) must be "Target 24" and
	// entries[19] (least recent of the 20) must be "Target 5".
	if got := *entries[0].TargetLabel; got != "Target 24" {
		t.Errorf("entries[0].TargetLabel = %q, want %q (most recent first)", got, "Target 24")
	}
	if got := *entries[19].TargetLabel; got != "Target 5" {
		t.Errorf("entries[19].TargetLabel = %q, want %q (20th most recent)", got, "Target 5")
	}
	for i := 0; i+1 < len(entries); i++ {
		if entries[i].CreatedAt.Before(entries[i+1].CreatedAt) {
			t.Fatalf("entries[%d].CreatedAt = %v is before entries[%d].CreatedAt = %v, want DESC order",
				i, entries[i].CreatedAt, i+1, entries[i+1].CreatedAt)
		}
	}
}

// TestAuditLogRepository_ListRecent_ActorHardDeleted covers spec.md's
// actor-deleted edge case: once the actor's users row is hard-deleted,
// ListRecent must still return the entry, with ActorDeleted true and
// ActorName empty - never a raw UUID, never an error.
func TestAuditLogRepository_ListRecent_ActorHardDeleted(t *testing.T) {
	pool := newAuditLogRLSTestPool(t)
	repo := NewAuditLogRepository(pool)

	suffix := auditLogTUniqueSuffix()
	tenantID := seedAuditLogTenant(t, pool, fmt.Sprintf("audit-actor-deleted-%d", suffix))
	email := fmt.Sprintf("audit-actor-deleted-%d@example.com", suffix)
	actorID := seedAuditLogUser(t, pool, email, "Deleted Actor")

	label := "Some Target"
	seedAuditLogEntry(t, pool, tenantID, actorID, "removed", &label, time.Now())

	if _, err := pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", actorID); err != nil {
		t.Fatalf("deleting actor's users row returned unexpected error: %v", err)
	}

	tx, ctx := beginAuditLogRLSTx(t, pool, tenantID)
	defer func() { _ = tx.Rollback(context.Background()) }()

	entries, err := repo.ListRecent(ctx, 20)
	if err != nil {
		t.Fatalf("ListRecent() returned unexpected error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("ListRecent() returned %d entries, want 1", len(entries))
	}
	if !entries[0].ActorDeleted {
		t.Error("entries[0].ActorDeleted = false, want true (actor's users row was hard-deleted)")
	}
	if entries[0].ActorName != "" {
		t.Errorf("entries[0].ActorName = %q, want empty string", entries[0].ActorName)
	}
}

// TestAuditLogRepository_ListRecent_NullTargetLabel covers spec.md's
// historical-row edge case: a row predating the target_label column (or
// otherwise written with NULL) must come back as a Go nil, not an error
// and not an empty-but-non-nil string mistaken for one.
func TestAuditLogRepository_ListRecent_NullTargetLabel(t *testing.T) {
	pool := newAuditLogRLSTestPool(t)
	repo := NewAuditLogRepository(pool)

	suffix := auditLogTUniqueSuffix()
	tenantID := seedAuditLogTenant(t, pool, fmt.Sprintf("audit-null-label-%d", suffix))
	seedAuditLogEntry(t, pool, tenantID, "00000000-0000-0000-0000-000000000004", "role_changed", nil, time.Now())

	tx, ctx := beginAuditLogRLSTx(t, pool, tenantID)
	defer func() { _ = tx.Rollback(context.Background()) }()

	entries, err := repo.ListRecent(ctx, 20)
	if err != nil {
		t.Fatalf("ListRecent() returned unexpected error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("ListRecent() returned %d entries, want 1", len(entries))
	}
	if entries[0].TargetLabel != nil {
		t.Errorf("entries[0].TargetLabel = %q, want nil", *entries[0].TargetLabel)
	}
	if entries[0].Action != "role_changed" {
		t.Errorf("entries[0].Action = %q, want %q", entries[0].Action, "role_changed")
	}
}
