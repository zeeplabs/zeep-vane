//go:build integration

package db

import (
	"context"
	"testing"
)

func TestServicesMigration_AppliesClean_AndDefaultsToNotConfigured(t *testing.T) {
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

	const name = "services-migration-test-service"
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM services WHERE name = $1", name)
	})

	_, err = pool.Exec(ctx,
		"INSERT INTO services (name, slo_id) VALUES ($1, $2)", name, "slo-123")
	if err != nil {
		t.Fatalf("insert returned unexpected error: %v", err)
	}

	var currentStatus string
	row := pool.QueryRow(ctx, "SELECT current_status FROM services WHERE name = $1", name)
	if err := row.Scan(&currentStatus); err != nil {
		t.Fatalf("Scan() returned unexpected error: %v", err)
	}

	if currentStatus != "not_configured" {
		t.Errorf("current_status = %q, want %q", currentStatus, "not_configured")
	}
}
