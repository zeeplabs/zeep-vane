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

// TestEmailProvidersMigration_SettingsRow_IsOnePerTenant asserts EMAIL-04
// as multi-tenancy-core reshaped it: email_settings is no longer an
// id = 1 singleton seeded per installation, it is keyed by tenant_id and
// created on first activation. A tenant's row starts with a NULL
// active_provider - never a "row missing where one was expected" state a
// caller would need to special-case beyond "not activated yet".
func TestEmailProvidersMigration_SettingsRow_IsOnePerTenant(t *testing.T) {
	ctx := context.Background()
	pool, tenantID := newTenantScopedPool(t)

	if _, err := pool.Exec(ctx, "INSERT INTO email_settings DEFAULT VALUES"); err != nil {
		t.Fatalf("insert of the tenant's settings row returned unexpected error: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM email_settings WHERE tenant_id = $1", tenantID).Scan(&count); err != nil {
		t.Fatalf("count query returned unexpected error: %v", err)
	}
	if count != 1 {
		t.Fatalf("email_settings row count for this tenant = %d, want exactly 1", count)
	}

	var activeProvider *string
	row := pool.QueryRow(ctx, "SELECT active_provider FROM email_settings WHERE tenant_id = $1", tenantID)
	if err := row.Scan(&activeProvider); err != nil {
		t.Fatalf("settings row query returned unexpected error: %v", err)
	}
	if activeProvider != nil {
		t.Errorf("new row's active_provider = %q, want nil", *activeProvider)
	}
}

// TestEmailProvidersMigration_SecondSettingsRowForSameTenant_ConstraintViolation
// asserts the DB-level "at most one settings row per tenant" guarantee -
// the tenant_id primary key that replaced the old CHECK (id = 1).
func TestEmailProvidersMigration_SecondSettingsRowForSameTenant_ConstraintViolation(t *testing.T) {
	ctx := context.Background()
	pool, _ := newTenantScopedPool(t)

	if _, err := pool.Exec(ctx, "INSERT INTO email_settings DEFAULT VALUES"); err != nil {
		t.Fatalf("first insert returned unexpected error: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO email_settings DEFAULT VALUES"); err == nil {
		t.Fatal("second insert for the same tenant returned nil error, want primary key violation")
	}
}

// TestEmailProvidersMigration_ProviderCheck_RejectsUnknownProvider asserts
// EMAIL-04's edge case that the schema itself, not just application code,
// refuses an unsupported provider name in email_providers.
func TestEmailProvidersMigration_ProviderCheck_RejectsUnknownProvider(t *testing.T) {
	ctx := context.Background()
	pool, _ := newTenantScopedPool(t)

	_, err := pool.Exec(ctx,
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
	ctx := context.Background()
	pool, tenantID := newTenantScopedPool(t)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM email_providers WHERE tenant_id = $1", tenantID)
	})

	_, err := pool.Exec(ctx,
		"INSERT INTO email_providers (provider, encrypted_api_key, from_email, from_name, status) VALUES ('sendgrid', $1, $2, $3, 'bogus')",
		[]byte("cipher"), "a@example.com", "A")
	if err == nil {
		t.Fatal("insert with unsupported status returned nil error, want CHECK constraint violation")
	}
}

// TestEmailProvidersMigration_DownReversesCleanly asserts 0016's down.sql
// drops both tables without error and the schema can be migrated back up
// afterwards. It runs against its own scratch database and migrates up to
// exactly 0016 before stepping it down - never on the shared
// TEST_DATABASE_URL, where stepping a migration down would tear schema out
// from under every other package whose tests run in parallel.
func TestEmailProvidersMigration_DownReversesCleanly(t *testing.T) {
	dsn := newScratchDatabase(t)

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

	const emailProvidersVersion = 16
	if err := m.Migrate(emailProvidersVersion); err != nil {
		t.Fatalf("m.Migrate(%d) returned unexpected error: %v", emailProvidersVersion, err)
	}
	if err := m.Steps(-1); err != nil {
		t.Fatalf("m.Steps(-1) returned unexpected error: %v", err)
	}
	if err := m.Migrate(emailProvidersVersion); err != nil {
		t.Fatalf("re-applying %d after down returned unexpected error: %v", emailProvidersVersion, err)
	}
}
