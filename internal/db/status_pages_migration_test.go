//go:build integration

package db

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestStatusPagesMigration_AppliesClean_AndEnforcesForeignKeys(t *testing.T) {
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

	// status_pages.domain_id must reject a domain_id that doesn't exist.
	_, err = pool.Exec(ctx,
		"INSERT INTO status_pages (name, subdomain, domain_id) VALUES ($1, $2, gen_random_uuid())",
		"fk-test-page", "status")
	if err == nil {
		t.Fatal("insert with nonexistent domain_id returned nil error, want foreign key violation")
	}

	hostname := fmt.Sprintf("status-pages-migration-test-%d.example.com", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM domains WHERE hostname = $1", hostname) })
	var domainID string
	row := pool.QueryRow(ctx, "INSERT INTO domains (hostname) VALUES ($1) RETURNING id", hostname)
	if err := row.Scan(&domainID); err != nil {
		t.Fatalf("failed to insert domain fixture: %v", err)
	}

	var statusPageID, state string
	row = pool.QueryRow(ctx,
		"INSERT INTO status_pages (name, subdomain, domain_id) VALUES ($1, $2, $3) RETURNING id, state",
		"fk-test-page", "status", domainID)
	if err := row.Scan(&statusPageID, &state); err != nil {
		t.Fatalf("insert with valid domain_id returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM status_pages WHERE id = $1", statusPageID) })

	if state != "draft" {
		t.Errorf("state = %q, want %q", state, "draft")
	}

	// status_page_services must reject a service_id that doesn't exist.
	_, err = pool.Exec(ctx,
		"INSERT INTO status_page_services (status_page_id, service_id) VALUES ($1, gen_random_uuid())",
		statusPageID)
	if err == nil {
		t.Fatal("insert with nonexistent service_id returned nil error, want foreign key violation")
	}

	serviceName := fmt.Sprintf("status-pages-migration-test-service-%d", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM services WHERE name = $1", serviceName) })
	var serviceID string
	row = pool.QueryRow(ctx,
		"INSERT INTO services (name, slo_id) VALUES ($1, $2) RETURNING id",
		serviceName, "slo-fk-test")
	if err := row.Scan(&serviceID); err != nil {
		t.Fatalf("failed to insert service fixture: %v", err)
	}

	_, err = pool.Exec(ctx,
		"INSERT INTO status_page_services (status_page_id, service_id) VALUES ($1, $2)",
		statusPageID, serviceID)
	if err != nil {
		t.Fatalf("insert with valid status_page_id and service_id returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM status_page_services WHERE status_page_id = $1", statusPageID)
	})
}
