//go:build integration

package db

import (
	"context"
	"testing"
)

func TestMigrateUp_EmptyMigrationsDir_NoError(t *testing.T) {
	dsn := testDatabaseURL(t)

	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}
}

func TestMigrateUp_RunTwice_Idempotent(t *testing.T) {
	dsn := testDatabaseURL(t)

	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("first MigrateUp() returned unexpected error: %v", err)
	}
	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("second MigrateUp() returned unexpected error: %v", err)
	}

	pool, err := NewPool(context.Background(), dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	defer pool.Close()

	var count int
	err = pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	if err != nil {
		t.Fatalf("querying schema_migrations returned unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("schema_migrations row count = %d after running with 0 migration files, want 0", count)
	}
}
