//go:build integration

package db

import (
	"context"
	"testing"
)

func TestMigrateUp_EmptyMigrationsDir_NoError(t *testing.T) {
	dsn := testDatabaseURL(t)

	// SPEC_DEVIATION: uses an isolated empty temp dir instead of the real
	// "migrations" dir, which is no longer empty once feature migrations
	// (e.g. admins, T8) land there. Reason: decouples this test's "0
	// migrations is a no-op" assertion from the production migrations
	// directory's contents, which is expected to keep growing.
	if err := MigrateUp(dsn, t.TempDir()); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}
}

func TestMigrateUp_RunTwice_Idempotent(t *testing.T) {
	dsn := testDatabaseURL(t)
	dir := t.TempDir()

	pool, err := NewPool(context.Background(), dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	defer pool.Close()

	countSchemaMigrations := func() int {
		var count int
		if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
			t.Fatalf("querying schema_migrations returned unexpected error: %v", err)
		}
		return count
	}

	if err := MigrateUp(dsn, dir); err != nil {
		t.Fatalf("first MigrateUp() returned unexpected error: %v", err)
	}
	// SPEC_DEVIATION: compares the row count before/after the second run
	// instead of asserting a literal 0. Reason: schema_migrations is a
	// single table shared by the whole test database across this
	// package's tests, so its absolute count depends on which other
	// migrations (e.g. real feature migrations from other tests) already
	// ran against it. The T6 "Done when" criterion is idempotency -
	// running twice must not duplicate anything - which this checks
	// directly regardless of what else touched the shared database.
	countAfterFirst := countSchemaMigrations()

	if err := MigrateUp(dsn, dir); err != nil {
		t.Fatalf("second MigrateUp() returned unexpected error: %v", err)
	}
	countAfterSecond := countSchemaMigrations()

	if countAfterSecond != countAfterFirst {
		t.Errorf("schema_migrations row count after second run = %d, want %d (same as after first run, no duplication)", countAfterSecond, countAfterFirst)
	}
}
