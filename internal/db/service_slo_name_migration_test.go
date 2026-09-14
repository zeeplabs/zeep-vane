//go:build integration

package db

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestServiceSLONameMigration_AppliesClean_DefaultsEmptyString asserts
// SVC-01: an existing service row (created before this migration ran)
// defaults slo_name to the empty string rather than erroring or requiring
// a backfill.
func TestServiceSLONameMigration_AppliesClean_DefaultsEmptyString(t *testing.T) {
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

	serviceName := fmt.Sprintf("slo-name-migration-test-%d", time.Now().UnixNano())
	var serviceID string
	tenantID := seedPlainTenant(t, pool)
	withTenantTx(t, pool, tenantID, func(txCtx context.Context) {
		row := pool.QueryRow(txCtx,
			"INSERT INTO services (name, slo_id) VALUES ($1, $2) RETURNING id",
			serviceName, "slo-name-test")
		if err := row.Scan(&serviceID); err != nil {
			t.Fatalf("insert service returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", serviceID) })

	var sloName string
	row := pool.QueryRow(ctx, "SELECT slo_name FROM services WHERE id = $1", serviceID)
	if err := row.Scan(&sloName); err != nil {
		t.Fatalf("select returned unexpected error: %v", err)
	}
	if sloName != "" {
		t.Errorf("slo_name = %q, want %q", sloName, "")
	}

	if _, err := pool.Exec(ctx, "UPDATE services SET slo_name = $1 WHERE id = $2", "Checkout latency SLO", serviceID); err != nil {
		t.Fatalf("update slo_name returned unexpected error: %v", err)
	}

	row = pool.QueryRow(ctx, "SELECT slo_name FROM services WHERE id = $1", serviceID)
	if err := row.Scan(&sloName); err != nil {
		t.Fatalf("select after update returned unexpected error: %v", err)
	}
	if sloName != "Checkout latency SLO" {
		t.Errorf("slo_name = %q, want %q", sloName, "Checkout latency SLO")
	}
}

// TestServiceSLONameMigration_DownReversesCleanly asserts 0033's down.sql
// drops the column without error and the schema can be migrated back up
// afterwards.
func TestServiceSLONameMigration_DownReversesCleanly(t *testing.T) {
	// Runs against its own scratch database: stepping a migration down
	// mutates the schema, and doing that on the shared TEST_DATABASE_URL
	// would tear the latest migration down out from under every other
	// package whose tests run in parallel against the same database.
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

	// Apply up to exactly 0033 (its own migration), then step that one
	// down - rather than migrating the whole schema up and stepping down
	// whatever is latest, which is no longer 0033 once later migrations
	// exist.
	const serviceSLONameVersion = 33
	if err := m.Migrate(serviceSLONameVersion); err != nil {
		t.Fatalf("m.Migrate(%d) returned unexpected error: %v", serviceSLONameVersion, err)
	}
	if err := m.Steps(-1); err != nil {
		t.Fatalf("m.Steps(-1) returned unexpected error: %v", err)
	}
	if err := m.Migrate(serviceSLONameVersion); err != nil {
		t.Fatalf("re-applying %d after down returned unexpected error: %v", serviceSLONameVersion, err)
	}
}
