//go:build integration

package db

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestIncidentsMigration_AppliesClean_AndEnforcesForeignKeys(t *testing.T) {
	ctx := context.Background()
	pool, _ := newTenantScopedPool(t)

	var incidentID, status string
	row := pool.QueryRow(ctx,
		"INSERT INTO incidents (title) VALUES ($1) RETURNING id, status",
		"fk-test-incident")
	if err := row.Scan(&incidentID, &status); err != nil {
		t.Fatalf("insert incident returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM incidents WHERE id = $1", incidentID) })

	if status != "investigating" {
		t.Errorf("status = %q, want %q", status, "investigating")
	}

	// incident_services.service_id must reject a service_id that doesn't
	// exist.
	_, err := pool.Exec(ctx,
		"INSERT INTO incident_services (incident_id, service_id) VALUES ($1, gen_random_uuid())",
		incidentID)
	if err == nil {
		t.Fatal("insert with nonexistent service_id returned nil error, want foreign key violation")
	}

	serviceName := fmt.Sprintf("incidents-migration-test-service-%d", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE name = $1", serviceName) })
	var serviceID string
	tenantID := seedPlainTenant(t, pool)
	withTenantTx(t, pool, tenantID, func(txCtx context.Context) {
		row = pool.QueryRow(txCtx,
			"INSERT INTO services (name, slo_id) VALUES ($1, $2) RETURNING id",
			serviceName, "slo-fk-test")
		if err := row.Scan(&serviceID); err != nil {
			t.Fatalf("failed to insert service fixture: %v", err)
		}
	})

	_, err = pool.Exec(ctx,
		"INSERT INTO incident_services (incident_id, service_id) VALUES ($1, $2)",
		incidentID, serviceID)
	if err != nil {
		t.Fatalf("insert with valid incident_id and service_id returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM incident_services WHERE incident_id = $1", incidentID)
	})

	// incident_updates.incident_id must reject an incident_id that doesn't
	// exist.
	_, err = pool.Exec(ctx,
		"INSERT INTO incident_updates (incident_id, body) VALUES (gen_random_uuid(), $1)",
		"orphan update")
	if err == nil {
		t.Fatal("insert with nonexistent incident_id returned nil error, want foreign key violation")
	}

	_, err = pool.Exec(ctx,
		"INSERT INTO incident_updates (incident_id, body) VALUES ($1, $2)",
		incidentID, "investigating the issue")
	if err != nil {
		t.Fatalf("insert with valid incident_id returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM incident_updates WHERE incident_id = $1", incidentID)
	})
}
