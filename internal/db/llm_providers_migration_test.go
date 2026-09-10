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

// TestLLMProvidersMigration_SettingsRow_IsOnePerTenant asserts AI-05 as
// multi-tenancy-core reshaped it: llm_settings is no longer an id = 1
// singleton seeded per installation, it is keyed by tenant_id and created
// on first activation, with a NULL active_provider until one is chosen.
func TestLLMProvidersMigration_SettingsRow_IsOnePerTenant(t *testing.T) {
	ctx := context.Background()
	pool, tenantID := newTenantScopedPool(t)

	if _, err := pool.Exec(ctx, "INSERT INTO llm_settings DEFAULT VALUES"); err != nil {
		t.Fatalf("insert of the tenant's settings row returned unexpected error: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM llm_settings WHERE tenant_id = $1", tenantID).Scan(&count); err != nil {
		t.Fatalf("count query returned unexpected error: %v", err)
	}
	if count != 1 {
		t.Fatalf("llm_settings row count for this tenant = %d, want exactly 1", count)
	}

	var activeProvider *string
	row := pool.QueryRow(ctx, "SELECT active_provider FROM llm_settings WHERE tenant_id = $1", tenantID)
	if err := row.Scan(&activeProvider); err != nil {
		t.Fatalf("settings row query returned unexpected error: %v", err)
	}
	if activeProvider != nil {
		t.Errorf("new row's active_provider = %q, want nil", *activeProvider)
	}

	if _, err := pool.Exec(ctx, "INSERT INTO llm_settings DEFAULT VALUES"); err == nil {
		t.Fatal("second insert for the same tenant returned nil error, want primary key violation")
	}
}

// TestLLMProvidersMigration_ProviderRoundTrips_WithModel confirms
// llm_providers has the expected columns, including model, by inserting
// and reading a full row.
func TestLLMProvidersMigration_ProviderRoundTrips_WithModel(t *testing.T) {
	ctx := context.Background()
	pool, tenantID := newTenantScopedPool(t)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM llm_providers WHERE tenant_id = $1", tenantID)
	})

	_, err := pool.Exec(ctx,
		"INSERT INTO llm_providers (provider, encrypted_api_key, model) VALUES ('openai', $1, $2)",
		[]byte("cipher"), "gpt-4o-mini")
	if err != nil {
		t.Fatalf("insert with valid provider/model returned unexpected error: %v", err)
	}

	var provider, model, status string
	row := pool.QueryRow(ctx, "SELECT provider, model, status FROM llm_providers WHERE provider = 'openai'")
	if err := row.Scan(&provider, &model, &status); err != nil {
		t.Fatalf("select returned unexpected error: %v", err)
	}
	if provider != "openai" || model != "gpt-4o-mini" || status != "connected" {
		t.Errorf("provider/model/status = %q/%q/%q, want %q/%q/%q", provider, model, status, "openai", "gpt-4o-mini", "connected")
	}
}

// TestLLMProvidersMigration_SecondSettingsRow_ConstraintViolation asserts
// the design's DB-level singleton guarantee: CHECK (id = 1) rejects any
// llm_settings row whose id isn't 1.
func TestLLMProvidersMigration_SecondSettingsRow_ConstraintViolation(t *testing.T) {
	ctx := context.Background()
	pool, _ := newTenantScopedPool(t)

	_, err := pool.Exec(ctx, "INSERT INTO llm_settings (id) VALUES (2)")
	if err == nil {
		t.Fatal("insert with id != 1 returned nil error, want CHECK constraint violation")
	}
}

// TestLLMProvidersMigration_ProviderCheck_RejectsUnknownProvider asserts
// the schema itself, not just application code, refuses an unsupported
// provider name in llm_providers.
func TestLLMProvidersMigration_ProviderCheck_RejectsUnknownProvider(t *testing.T) {
	ctx := context.Background()
	pool, _ := newTenantScopedPool(t)

	_, err := pool.Exec(ctx,
		"INSERT INTO llm_providers (provider, encrypted_api_key, model) VALUES ('anthropic', $1, $2)",
		[]byte("cipher"), "some-model")
	if err == nil {
		t.Fatal("insert with unsupported provider returned nil error, want CHECK constraint violation")
	}
}

// TestLLMProvidersMigration_StatusCheck_RejectsUnknownStatus asserts the
// schema-level guard on llm_providers.status matching the same fail-closed
// instinct as the provider CHECK above.
func TestLLMProvidersMigration_StatusCheck_RejectsUnknownStatus(t *testing.T) {
	ctx := context.Background()
	pool, _ := newTenantScopedPool(t)

	_, err := pool.Exec(ctx,
		"INSERT INTO llm_providers (provider, encrypted_api_key, model, status) VALUES ('openai', $1, $2, 'bogus')",
		[]byte("cipher"), "gpt-4o-mini")
	if err == nil {
		t.Fatal("insert with unsupported status returned nil error, want CHECK constraint violation")
	}
}

// TestLLMProvidersMigration_DownReversesCleanly asserts 0021's down.sql
// drops both tables without error and the schema can be migrated back up
// afterwards.
func TestLLMProvidersMigration_DownReversesCleanly(t *testing.T) {
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
