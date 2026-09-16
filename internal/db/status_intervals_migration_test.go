//go:build integration

package db

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestStatusIntervalsMigration_AppliesClean_AndHasExpectedIndexes(t *testing.T) {
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

	const serviceName = "status-intervals-migration-test-service"
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM services WHERE name = $1", serviceName)
	})

	var serviceID string
	tenantID := seedPlainTenant(t, pool)
	withTenantTx(t, pool, tenantID, func(txCtx context.Context) {
		row := pool.QueryRow(txCtx,
			"INSERT INTO services (name, slo_id) VALUES ($1, $2) RETURNING id", serviceName, "slo-789")
		if err := row.Scan(&serviceID); err != nil {
			t.Fatalf("insert service returned unexpected error: %v", err)
		}
	})

	now := time.Now().UTC()
	_, err = pool.Exec(ctx,
		"INSERT INTO status_intervals (service_id, status, error_budget_remaining, starts_at, last_seen_at) VALUES ($1, $2, $3, $4, $4)",
		serviceID, "operational", 95.5, now,
	)
	if err != nil {
		t.Fatalf("insert status interval returned unexpected error: %v", err)
	}

	var oneOpenIdx int
	row := pool.QueryRow(ctx,
		`SELECT count(*) FROM pg_indexes
		 WHERE tablename = 'status_intervals'
		 AND indexdef LIKE '%(service_id)%' AND indexdef LIKE '%ends_at IS NULL%'`)
	if err := row.Scan(&oneOpenIdx); err != nil {
		t.Fatalf("Scan() one-open-per-service index check returned unexpected error: %v", err)
	}
	if oneOpenIdx != 1 {
		t.Errorf("unique partial index on (service_id) WHERE ends_at IS NULL count = %d, want 1", oneOpenIdx)
	}

	var startsAtIdx int
	row = pool.QueryRow(ctx,
		`SELECT count(*) FROM pg_indexes
		 WHERE tablename = 'status_intervals'
		 AND indexdef LIKE '%(service_id, starts_at)%'`)
	if err := row.Scan(&startsAtIdx); err != nil {
		t.Fatalf("Scan() service_id/starts_at index check returned unexpected error: %v", err)
	}
	if startsAtIdx != 1 {
		t.Errorf("index on (service_id, starts_at) count = %d, want 1", startsAtIdx)
	}

	var endsAtIdx int
	row = pool.QueryRow(ctx,
		`SELECT count(*) FROM pg_indexes
		 WHERE tablename = 'status_intervals'
		 AND indexdef LIKE '%(ends_at)%' AND indexdef LIKE '%ends_at IS NOT NULL%'`)
	if err := row.Scan(&endsAtIdx); err != nil {
		t.Fatalf("Scan() ends_at index check returned unexpected error: %v", err)
	}
	if endsAtIdx != 1 {
		t.Errorf("partial index on (ends_at) WHERE ends_at IS NOT NULL count = %d, want 1", endsAtIdx)
	}

	var snapshotsTableCount int
	row = pool.QueryRow(ctx,
		"SELECT count(*) FROM pg_tables WHERE tablename = 'status_snapshots'")
	if err := row.Scan(&snapshotsTableCount); err != nil {
		t.Fatalf("Scan() status_snapshots absence check returned unexpected error: %v", err)
	}
	if snapshotsTableCount != 0 {
		t.Errorf("status_snapshots table count = %d, want 0 (should be dropped)", snapshotsTableCount)
	}
}

func TestStatusIntervalsMigration_DownReversesCleanly(t *testing.T) {
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

	// Apply up to exactly 0014 (its own migration), then step that one
	// down - rather than migrating the whole schema up and stepping down
	// whatever is latest, which is no longer 0014 now that later
	// migrations exist.
	const statusIntervalsVersion = 14
	if err := m.Migrate(statusIntervalsVersion); err != nil {
		t.Fatalf("m.Migrate(%d) returned unexpected error: %v", statusIntervalsVersion, err)
	}
	if err := m.Steps(-1); err != nil {
		t.Fatalf("m.Steps(-1) returned unexpected error: %v", err)
	}
	if err := m.Migrate(statusIntervalsVersion); err != nil {
		t.Fatalf("re-applying %d after down returned unexpected error: %v", statusIntervalsVersion, err)
	}
}
