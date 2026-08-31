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
	// A genuinely fresh, private database - not the shared TEST_DATABASE_URL
	// one every other integration test writes to - because this test
	// asserts the singleton row's seeded defaults, which only hold on a
	// database no other test has ever PATCHed. LockCompanySettings would
	// only serialize concurrent writes to the shared row, not guarantee
	// this test observes it before some other test's write landed; a
	// scratch database sidesteps the ordering problem entirely instead of
	// just narrowing its window (see newScratchDatabase in
	// migrations_embed_test.go, the established pattern for this exact
	// need in this package).
	dsn := newScratchDatabase(t)

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
	var logoData []byte
	var logoContentType *string
	row := pool.QueryRow(ctx, "SELECT name, contact_email, logo_data, logo_content_type FROM company_settings WHERE id = 1")
	if err := row.Scan(&name, &contactEmail, &logoData, &logoContentType); err != nil {
		t.Fatalf("seed row query returned unexpected error: %v", err)
	}
	if name != "" {
		t.Errorf("seeded name = %q, want \"\"", name)
	}
	if contactEmail != "" {
		t.Errorf("seeded contact_email = %q, want \"\"", contactEmail)
	}
	if logoData != nil {
		t.Errorf("seeded logo_data = %v, want nil", logoData)
	}
	if logoContentType != nil {
		t.Errorf("seeded logo_content_type = %q, want nil", *logoContentType)
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
