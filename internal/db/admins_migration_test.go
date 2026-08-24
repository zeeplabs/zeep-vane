//go:build integration

package db

import (
	"context"
	"testing"

	"github.com/zeeplabs/zeep-vane/internal/dbtest"
)

func TestAdminsMigration_AppliesClean_AndEnforcesUniqueEmail(t *testing.T) {
	dsn := testDatabaseURL(t)

	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx := context.Background()
	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	// Registered before the delete cleanup below so it runs after it
	// (t.Cleanup runs LIFO): a `defer pool.Close()` here would run at
	// function return, before any t.Cleanup, closing the pool the delete
	// cleanup still needs.
	t.Cleanup(pool.Close)

	// This INSERT relies on `admins.role`'s database default (owner, see
	// migration 0009), so it transiently creates an owner-role row like
	// any other admin-creating test - see LockAdminsTable's doc comment
	// for why this must be held across concurrently-run packages.
	dbtest.LockAdminsTable(t, ctx, dsn)

	// A fixed literal email here (rather than a per-run-unique one) would
	// collide with a leftover row from a previous failed/interrupted run
	// that never got cleaned up, producing a spurious unique-constraint
	// failure or a spurious pass unrelated to what this test checks.
	email := uniqueTestEmail(t)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM admins WHERE email = $1", email)
	})

	_, err = pool.Exec(ctx,
		"INSERT INTO admins (email, password_hash) VALUES ($1, $2)", email, "hash-1")
	if err != nil {
		t.Fatalf("first insert returned unexpected error: %v", err)
	}

	_, err = pool.Exec(ctx,
		"INSERT INTO admins (email, password_hash) VALUES ($1, $2)", email, "hash-2")
	if err == nil {
		t.Fatal("second insert with duplicate email returned nil error, want unique constraint violation")
	}
}
