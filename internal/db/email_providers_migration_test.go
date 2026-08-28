//go:build integration

package db

import (
	"context"
	"database/sql"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestEmailProvidersMigration_AppliesClean_SeedsSingletonRow asserts
// EMAIL-04: a fresh install has exactly one email_settings row, seeded
// with a NULL active_provider - never a "row missing" state a caller
// would need to special-case.
func TestEmailProvidersMigration_AppliesClean_SeedsSingletonRow(t *testing.T) {
	dsn := testDatabaseURL(t)

	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx := context.Background()
	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM email_settings").Scan(&count); err != nil {
		t.Fatalf("count query returned unexpected error: %v", err)
	}
	if count != 1 {
		t.Fatalf("email_settings row count = %d, want exactly 1", count)
	}

	var activeProvider *string
	row := pool.QueryRow(ctx, "SELECT active_provider FROM email_settings WHERE id = 1")
	if err := row.Scan(&activeProvider); err != nil {
		t.Fatalf("seed row query returned unexpected error: %v", err)
	}
	if activeProvider != nil {
		t.Errorf("seeded active_provider = %q, want nil", *activeProvider)
	}
}

// TestEmailProvidersMigration_SecondSettingsRow_ConstraintViolation asserts
// the design's DB-level singleton guarantee: CHECK (id = 1) rejects any
// email_settings row whose id isn't 1.
func TestEmailProvidersMigration_SecondSettingsRow_ConstraintViolation(t *testing.T) {
	dsn := testDatabaseURL(t)

	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx := context.Background()
	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	_, err = pool.Exec(ctx, "INSERT INTO email_settings (id) VALUES (2)")
	if err == nil {
		t.Fatal("insert with id != 1 returned nil error, want CHECK constraint violation")
	}
}

// TestEmailProvidersMigration_ProviderCheck_RejectsUnknownProvider asserts
// EMAIL-04's edge case that the schema itself, not just application code,
// refuses an unsupported provider name in email_providers.
func TestEmailProvidersMigration_ProviderCheck_RejectsUnknownProvider(t *testing.T) {
	dsn := testDatabaseURL(t)

	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx := context.Background()
	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	_, err = pool.Exec(ctx,
		"INSERT INTO email_providers (provider, encrypted_api_key, from_email, from_name) VALUES ('mailgun', $1, $2, $3)",
		[]byte("cipher"), "a@example.com", "A")
	if err == nil {
		t.Fatal("insert with unsupported provider returned nil error, want CHECK constraint violation")
	}
}

// TestEmailProvidersMigration_StatusCheck_RejectsUnknownStatus asserts the
// schema-level guard on email_providers.status matching the same
// fail-closed instinct as the provider CHECK above.
func TestEmailProvidersMigration_StatusCheck_RejectsUnknownStatus(t *testing.T) {
	dsn := testDatabaseURL(t)

	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx := context.Background()
	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM email_providers WHERE provider = 'sendgrid'")
	})

	_, err = pool.Exec(ctx,
		"INSERT INTO email_providers (provider, encrypted_api_key, from_email, from_name, status) VALUES ('sendgrid', $1, $2, $3, 'bogus')",
		[]byte("cipher"), "a@example.com", "A")
	if err == nil {
		t.Fatal("insert with unsupported status returned nil error, want CHECK constraint violation")
	}
}

// TestEmailProvidersMigration_DownReversesCleanly asserts 0016's down.sql
// drops both tables without error and the schema can be migrated back up
// afterwards - mirrors TestStatusIntervalsMigration_DownReversesCleanly's
// pattern for the same reason: stepping down exactly one migration (this
// one, assuming it's the latest applied) rather than migrate.Down() (which
// would tear down every migration in the project).
func TestEmailProvidersMigration_DownReversesCleanly(t *testing.T) {
	dsn := testDatabaseURL(t)

	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	driver, err := pgxmigrate.WithInstance(sqlDB, &pgxmigrate.Config{})
	if err != nil {
		t.Fatalf("pgxmigrate.WithInstance() returned unexpected error: %v", err)
	}

	m, err := migrate.NewWithDatabaseInstance("file://migrations", "pgx5", driver)
	if err != nil {
		t.Fatalf("migrate.NewWithDatabaseInstance() returned unexpected error: %v", err)
	}

	if err := m.Steps(-1); err != nil {
		t.Fatalf("m.Steps(-1) returned unexpected error: %v", err)
	}

	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("second MigrateUp() returned unexpected error: %v", err)
	}
}
