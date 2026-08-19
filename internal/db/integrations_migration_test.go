//go:build integration

package db

import (
	"context"
	"testing"
)

func TestIntegrationsMigration_AppliesClean_AndEnforcesUniqueProvider(t *testing.T) {
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

	const provider = "datadog"
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM integrations WHERE provider = $1", provider)
	})

	_, err = pool.Exec(ctx,
		"INSERT INTO integrations (provider, encrypted_api_key, encrypted_app_key) VALUES ($1, $2, $3)",
		provider, []byte("cipher-1"), []byte("cipher-1"))
	if err != nil {
		t.Fatalf("first insert returned unexpected error: %v", err)
	}

	_, err = pool.Exec(ctx,
		"INSERT INTO integrations (provider, encrypted_api_key, encrypted_app_key) VALUES ($1, $2, $3)",
		provider, []byte("cipher-2"), []byte("cipher-2"))
	if err == nil {
		t.Fatal("second insert with duplicate provider returned nil error, want unique constraint violation")
	}
}
