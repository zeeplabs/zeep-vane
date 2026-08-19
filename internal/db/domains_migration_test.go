//go:build integration

package db

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestDomainsMigration_AppliesClean_AndEnforcesUniqueHostname(t *testing.T) {
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

	hostname := fmt.Sprintf("domains-migration-test-%d.example.com", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM domains WHERE hostname = $1", hostname)
	})

	_, err = pool.Exec(ctx, "INSERT INTO domains (hostname) VALUES ($1)", hostname)
	if err != nil {
		t.Fatalf("first insert returned unexpected error: %v", err)
	}

	_, err = pool.Exec(ctx, "INSERT INTO domains (hostname) VALUES ($1)", hostname)
	if err == nil {
		t.Fatal("second insert with duplicate hostname returned nil error, want unique constraint violation")
	}
}
