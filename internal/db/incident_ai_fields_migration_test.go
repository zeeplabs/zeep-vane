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

// TestIncidentAIFieldsMigration_AppliesClean_DefaultsCorrect asserts AI-09
// and AI-19: an incident created without description/pending_close_comment
// has both columns NULL, and auto_created defaults to false for a
// manually-created incident.
func TestIncidentAIFieldsMigration_AppliesClean_DefaultsCorrect(t *testing.T) {
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

	var incidentID string
	row := pool.QueryRow(ctx,
		"INSERT INTO incidents (title) VALUES ($1) RETURNING id",
		"ai-fields-test-incident")
	if err := row.Scan(&incidentID); err != nil {
		t.Fatalf("insert incident returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM incidents WHERE id = $1", incidentID) })

	var description, pendingCloseComment *string
	var autoCreated bool
	row = pool.QueryRow(ctx,
		"SELECT description, pending_close_comment, auto_created FROM incidents WHERE id = $1", incidentID)
	if err := row.Scan(&description, &pendingCloseComment, &autoCreated); err != nil {
		t.Fatalf("select returned unexpected error: %v", err)
	}

	if description != nil {
		t.Errorf("description = %q, want nil", *description)
	}
	if pendingCloseComment != nil {
		t.Errorf("pending_close_comment = %q, want nil", *pendingCloseComment)
	}
	if autoCreated != false {
		t.Errorf("auto_created = %v, want false", autoCreated)
	}
}

// TestIncidentAIFieldsMigration_AcceptsExplicitValues confirms the new
// columns actually store the values written to them (not just defaults).
func TestIncidentAIFieldsMigration_AcceptsExplicitValues(t *testing.T) {
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

	var incidentID string
	row := pool.QueryRow(ctx,
		"INSERT INTO incidents (title, description, pending_close_comment, auto_created) VALUES ($1, $2, $3, $4) RETURNING id",
		"ai-fields-test-incident-explicit", "auto-generated description", "proposed closing comment", true)
	if err := row.Scan(&incidentID); err != nil {
		t.Fatalf("insert incident returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM incidents WHERE id = $1", incidentID) })

	var description, pendingCloseComment string
	var autoCreated bool
	row = pool.QueryRow(ctx,
		"SELECT description, pending_close_comment, auto_created FROM incidents WHERE id = $1", incidentID)
	if err := row.Scan(&description, &pendingCloseComment, &autoCreated); err != nil {
		t.Fatalf("select returned unexpected error: %v", err)
	}

	if description != "auto-generated description" {
		t.Errorf("description = %q, want %q", description, "auto-generated description")
	}
	if pendingCloseComment != "proposed closing comment" {
		t.Errorf("pending_close_comment = %q, want %q", pendingCloseComment, "proposed closing comment")
	}
	if !autoCreated {
		t.Errorf("auto_created = %v, want true", autoCreated)
	}
}

// TestIncidentAIFieldsMigration_DownReversesCleanly asserts 0022's
// down.sql drops all three columns without error and the schema can be
// migrated back up afterwards.
func TestIncidentAIFieldsMigration_DownReversesCleanly(t *testing.T) {
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
