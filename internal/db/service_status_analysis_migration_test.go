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

// TestServiceStatusAnalysisMigration_AppliesClean_DefaultsNull asserts
// AI-15: an existing service row has status_analysis = NULL until
// something explicitly sets it.
func TestServiceStatusAnalysisMigration_AppliesClean_DefaultsNull(t *testing.T) {
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

	serviceName := fmt.Sprintf("status-analysis-migration-test-%d", time.Now().UnixNano())
	var serviceID string
	tenantID := seedPlainTenant(t, pool)
	withTenantTx(t, pool, tenantID, func(txCtx context.Context) {
		row := pool.QueryRow(txCtx,
			"INSERT INTO services (name, slo_id) VALUES ($1, $2) RETURNING id",
			serviceName, "slo-status-analysis-test")
		if err := row.Scan(&serviceID); err != nil {
			t.Fatalf("insert service returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", serviceID) })

	var statusAnalysis *string
	row := pool.QueryRow(ctx, "SELECT status_analysis FROM services WHERE id = $1", serviceID)
	if err := row.Scan(&statusAnalysis); err != nil {
		t.Fatalf("select returned unexpected error: %v", err)
	}
	if statusAnalysis != nil {
		t.Errorf("status_analysis = %q, want nil", *statusAnalysis)
	}

	if _, err := pool.Exec(ctx, "UPDATE services SET status_analysis = $1 WHERE id = $2", "SLI dropped below target", serviceID); err != nil {
		t.Fatalf("update status_analysis returned unexpected error: %v", err)
	}

	row = pool.QueryRow(ctx, "SELECT status_analysis FROM services WHERE id = $1", serviceID)
	if err := row.Scan(&statusAnalysis); err != nil {
		t.Fatalf("select after update returned unexpected error: %v", err)
	}
	if statusAnalysis == nil || *statusAnalysis != "SLI dropped below target" {
		t.Errorf("status_analysis = %v, want %q", statusAnalysis, "SLI dropped below target")
	}
}

// TestServiceStatusAnalysisMigration_DownReversesCleanly asserts 0023's
// down.sql drops the column without error and the schema can be migrated
// back up afterwards.
func TestServiceStatusAnalysisMigration_DownReversesCleanly(t *testing.T) {
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

	// Apply up to exactly 0023 (its own migration), then step that one
	// down - rather than migrating the whole schema up and stepping down
	// whatever is latest, which is no longer 0023 now that later
	// migrations exist.
	const serviceStatusAnalysisVersion = 23
	if err := m.Migrate(serviceStatusAnalysisVersion); err != nil {
		t.Fatalf("m.Migrate(%d) returned unexpected error: %v", serviceStatusAnalysisVersion, err)
	}
	if err := m.Steps(-1); err != nil {
		t.Fatalf("m.Steps(-1) returned unexpected error: %v", err)
	}
	if err := m.Migrate(serviceStatusAnalysisVersion); err != nil {
		t.Fatalf("re-applying %d after down returned unexpected error: %v", serviceStatusAnalysisVersion, err)
	}
}
