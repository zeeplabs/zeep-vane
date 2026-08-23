//go:build integration

package db

import (
	"context"
	"testing"
	"time"
)

// TestCompanySettingsMigration_AppliesClean_SeedsSingletonRow asserts
// SET-03/SET-06: a fresh install (no prior PATCH ever issued) has exactly
// one company_settings row, seeded with name="", contact_email="", and a
// NULL logo_url - never a "row missing" state a caller would need to
// special-case.
func TestCompanySettingsMigration_AppliesClean_SeedsSingletonRow(t *testing.T) {
	dsn := testDatabaseURL(t)

	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM company_settings").Scan(&count); err != nil {
		t.Fatalf("count query returned unexpected error: %v", err)
	}
	if count != 1 {
		t.Fatalf("company_settings row count = %d, want exactly 1", count)
	}

	var name, contactEmail string
	var logoURL *string
	row := pool.QueryRow(ctx, "SELECT name, contact_email, logo_url FROM company_settings WHERE id = 1")
	if err := row.Scan(&name, &contactEmail, &logoURL); err != nil {
		t.Fatalf("seed row query returned unexpected error: %v", err)
	}
	if name != "" {
		t.Errorf("seeded name = %q, want \"\"", name)
	}
	if contactEmail != "" {
		t.Errorf("seeded contact_email = %q, want \"\"", contactEmail)
	}
	if logoURL != nil {
		t.Errorf("seeded logo_url = %q, want nil", *logoURL)
	}
}

// TestCompanySettingsMigration_SecondRow_ConstraintViolation asserts the
// design's DB-level singleton guarantee: CHECK (id = 1) rejects any row
// whose id isn't 1, at the database level rather than as an application
// convention (SET-06).
func TestCompanySettingsMigration_SecondRow_ConstraintViolation(t *testing.T) {
	dsn := testDatabaseURL(t)

	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	_, err = pool.Exec(ctx, "INSERT INTO company_settings (id) VALUES (2)")
	if err == nil {
		t.Fatal("insert with id != 1 returned nil error, want CHECK constraint violation")
	}
}
