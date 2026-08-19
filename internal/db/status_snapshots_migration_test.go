//go:build integration

package db

import (
	"context"
	"testing"
)

func TestStatusSnapshotsMigration_AppliesClean_AndHasServiceFetchedAtIndex(t *testing.T) {
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

	const serviceName = "status-snapshots-migration-test-service"
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM services WHERE name = $1", serviceName)
	})

	var serviceID string
	row := pool.QueryRow(ctx,
		"INSERT INTO services (name, slo_id) VALUES ($1, $2) RETURNING id", serviceName, "slo-456")
	if err := row.Scan(&serviceID); err != nil {
		t.Fatalf("insert service returned unexpected error: %v", err)
	}

	_, err = pool.Exec(ctx,
		"INSERT INTO status_snapshots (service_id, status, error_budget_remaining) VALUES ($1, $2, $3)",
		serviceID, "operational", 95.5,
	)
	if err != nil {
		t.Fatalf("insert status snapshot returned unexpected error: %v", err)
	}

	var count int
	row = pool.QueryRow(ctx,
		`SELECT count(*) FROM pg_indexes
		 WHERE tablename = 'status_snapshots'
		 AND indexdef LIKE '%(service_id, fetched_at)%'`)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("Scan() index check returned unexpected error: %v", err)
	}

	if count != 1 {
		t.Errorf("index on (service_id, fetched_at) count = %d, want 1", count)
	}
}
