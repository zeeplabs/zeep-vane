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

// TestServicePollingModeMigration_AppliesClean_ExistingRowsDefaultToSLO
// asserts MP-01/MP-02: a service row created before this migration ran (or
// via a plain slo_id insert afterward) defaults monitor_mode to 'slo' with
// slo_id unchanged and every poll_* column NULL.
func TestServicePollingModeMigration_AppliesClean_ExistingRowsDefaultToSLO(t *testing.T) {
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

	serviceName := fmt.Sprintf("polling-mode-migration-test-%d", time.Now().UnixNano())
	var serviceID string
	tenantID := seedPlainTenant(t, pool)
	withTenantTx(t, pool, tenantID, func(txCtx context.Context) {
		row := pool.QueryRow(txCtx,
			"INSERT INTO services (name, slo_id) VALUES ($1, $2) RETURNING id",
			serviceName, "slo-polling-mode-test")
		if err := row.Scan(&serviceID); err != nil {
			t.Fatalf("insert service returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id = $1", serviceID) })

	var monitorMode, sloID string
	var pollType, pollTarget *string
	var pollIntervalSeconds *int
	row := pool.QueryRow(ctx,
		"SELECT monitor_mode, slo_id, poll_type, poll_target, poll_interval_seconds FROM services WHERE id = $1",
		serviceID)
	if err := row.Scan(&monitorMode, &sloID, &pollType, &pollTarget, &pollIntervalSeconds); err != nil {
		t.Fatalf("select returned unexpected error: %v", err)
	}

	if monitorMode != "slo" {
		t.Errorf("monitor_mode = %q, want %q", monitorMode, "slo")
	}
	if sloID != "slo-polling-mode-test" {
		t.Errorf("slo_id = %q, want %q", sloID, "slo-polling-mode-test")
	}
	if pollType != nil || pollTarget != nil || pollIntervalSeconds != nil {
		t.Errorf("poll_type/poll_target/poll_interval_seconds = %v/%v/%v, want all nil", pollType, pollTarget, pollIntervalSeconds)
	}
}

// TestServicePollingModeMigration_ValidCombinations_BothAccepted asserts
// MP-01/MP-02/MP-05: an slo-mode row (slo_id set, all poll_* NULL) and a
// polling-mode row (slo_id NULL, all three poll_* set) both insert cleanly.
func TestServicePollingModeMigration_ValidCombinations_BothAccepted(t *testing.T) {
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

	tenantID := seedPlainTenant(t, pool)
	prefix := fmt.Sprintf("polling-mode-valid-%d", time.Now().UnixNano())

	var sloRowID, pollingRowID string
	withTenantTx(t, pool, tenantID, func(txCtx context.Context) {
		row := pool.QueryRow(txCtx,
			"INSERT INTO services (name, monitor_mode, slo_id) VALUES ($1, 'slo', $2) RETURNING id",
			prefix+"-slo", "slo-1")
		if err := row.Scan(&sloRowID); err != nil {
			t.Fatalf("insert slo-mode row returned unexpected error: %v", err)
		}

		row = pool.QueryRow(txCtx,
			`INSERT INTO services (name, monitor_mode, poll_type, poll_target, poll_interval_seconds)
			 VALUES ($1, 'polling', 'http', 'https://example.test/health', 30) RETURNING id`,
			prefix+"-polling")
		if err := row.Scan(&pollingRowID); err != nil {
			t.Fatalf("insert polling-mode row returned unexpected error: %v", err)
		}
	})
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM services WHERE id IN ($1, $2)", sloRowID, pollingRowID)
	})
}

// TestServicePollingModeMigration_InvalidCombinations_Rejected asserts
// MP-01/MP-02/MP-05: the fields-combination CHECK constraint rejects an
// slo-mode row carrying a poll_* field, a polling-mode row carrying slo_id,
// and a polling-mode row missing any one of the three required poll_*
// fields.
func TestServicePollingModeMigration_InvalidCombinations_Rejected(t *testing.T) {
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

	tenantID := seedPlainTenant(t, pool)
	prefix := fmt.Sprintf("polling-mode-invalid-%d", time.Now().UnixNano())

	cases := []struct {
		name  string
		query string
		args  []any
	}{
		{
			name:  "slo mode with poll_type set",
			query: "INSERT INTO services (name, monitor_mode, slo_id, poll_type) VALUES ($1, 'slo', $2, 'http')",
			args:  []any{prefix + "-slo-with-poll-type", "slo-2"},
		},
		{
			name:  "polling mode with slo_id set",
			query: "INSERT INTO services (name, monitor_mode, slo_id, poll_type, poll_target, poll_interval_seconds) VALUES ($1, 'polling', $2, 'http', 'https://example.test', 30)",
			args:  []any{prefix + "-polling-with-slo-id", "slo-3"},
		},
		{
			name:  "polling mode missing poll_interval_seconds",
			query: "INSERT INTO services (name, monitor_mode, poll_type, poll_target) VALUES ($1, 'polling', 'http', 'https://example.test')",
			args:  []any{prefix + "-polling-missing-interval"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Each case's INSERT is expected to fail the CHECK constraint,
			// which aborts the transaction (Postgres semantics) - unlike
			// withTenantTx (which always commits after fn returns and
			// expects that to succeed), this manages its own tx so the
			// expected failure can be asserted and then rolled back
			// cleanly.
			tx, err := pool.BeginTenantTx(context.Background(), "", tenantID)
			if err != nil {
				t.Fatalf("BeginTenantTx() returned unexpected error: %v", err)
			}
			txCtx := WithTenantTx(context.Background(), tx)

			_, execErr := pool.Exec(txCtx, tc.query, tc.args...)
			if execErr == nil {
				t.Fatalf("Exec(%s) succeeded, want a CHECK constraint violation error", tc.name)
			}

			_ = tx.Rollback(context.Background())
		})
	}
}

// TestServicePollingModeMigration_DownReversesCleanly asserts 0034's
// down.sql drops the new constraints/columns and restores slo_id NOT NULL
// without error, and the migration can be re-applied afterward.
func TestServicePollingModeMigration_DownReversesCleanly(t *testing.T) {
	// Runs against its own scratch database, same reasoning as
	// TestServiceSLONameMigration_DownReversesCleanly: stepping a migration
	// down mutates the schema and must not race other packages' tests
	// sharing TEST_DATABASE_URL.
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

	const servicePollingModeVersion = 34
	if err := m.Migrate(servicePollingModeVersion); err != nil {
		t.Fatalf("m.Migrate(%d) returned unexpected error: %v", servicePollingModeVersion, err)
	}
	if err := m.Steps(-1); err != nil {
		t.Fatalf("m.Steps(-1) returned unexpected error: %v", err)
	}
	if err := m.Migrate(servicePollingModeVersion); err != nil {
		t.Fatalf("re-applying %d after down returned unexpected error: %v", servicePollingModeVersion, err)
	}
}
