//go:build integration

package db

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newScratchDatabase creates a brand-new, empty database on the same
// Postgres instance TEST_DATABASE_URL points at, returning a DSN for it.
// This proves MigrateUpEmbedded applies cleanly from zero, independent of
// whatever migrations the shared TEST_DATABASE_URL database already has
// applied by other tests in this package.
func newScratchDatabase(t *testing.T) string {
	t.Helper()
	baseDSN := testDatabaseURL(t)

	u, err := url.Parse(baseDSN)
	if err != nil {
		t.Fatalf("failed to parse TEST_DATABASE_URL: %v", err)
	}

	adminCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	adminPool, err := pgxpool.New(adminCtx, baseDSN)
	if err != nil {
		t.Fatalf("failed to connect for scratch database setup: %v", err)
	}
	defer adminPool.Close()

	dbName := fmt.Sprintf("vane_migrate_embed_test_%d", time.Now().UnixNano())
	if _, err := adminPool.Exec(adminCtx, fmt.Sprintf(`CREATE DATABASE %q`, dbName)); err != nil {
		t.Fatalf("failed to create scratch database %q: %v", dbName, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cleanupPool, err := pgxpool.New(cleanupCtx, baseDSN)
		if err != nil {
			return
		}
		defer cleanupPool.Close()
		_, _ = cleanupPool.Exec(cleanupCtx, fmt.Sprintf(`DROP DATABASE IF EXISTS %q WITH (FORCE)`, dbName))
	})

	u.Path = "/" + dbName
	return u.String()
}

func TestMigrateUpEmbedded_FreshDatabase_AppliesAllMigrations(t *testing.T) {
	dsn := newScratchDatabase(t)

	if err := MigrateUpEmbedded(dsn); err != nil {
		t.Fatalf("MigrateUpEmbedded() returned unexpected error: %v", err)
	}

	ctx := context.Background()
	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	defer pool.Close()

	var adminsTableExists bool
	row := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'admins')")
	if err := row.Scan(&adminsTableExists); err != nil {
		t.Fatalf("querying information_schema returned unexpected error: %v", err)
	}
	if !adminsTableExists {
		t.Error("admins table does not exist after MigrateUpEmbedded(), want it created by the embedded migrations")
	}

	var appliedCount int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&appliedCount); err != nil {
		t.Fatalf("querying schema_migrations returned unexpected error: %v", err)
	}
	if appliedCount == 0 {
		t.Error("schema_migrations is empty after MigrateUpEmbedded(), want at least one row recorded")
	}
}

func TestMigrateUpEmbedded_RunTwice_Idempotent(t *testing.T) {
	dsn := newScratchDatabase(t)

	if err := MigrateUpEmbedded(dsn); err != nil {
		t.Fatalf("first MigrateUpEmbedded() returned unexpected error: %v", err)
	}
	if err := MigrateUpEmbedded(dsn); err != nil {
		t.Fatalf("second MigrateUpEmbedded() returned unexpected error: %v", err)
	}
}

// TestMigrateUpEmbedded_NoMigrationsDirectoryOnDisk_StillApplies proves
// MigrateUpEmbedded has no runtime dependency on a migrations/ directory
// being present on disk (SHD-06): it runs from a working directory that
// has no such directory at all, while the disk-based MigrateUp genuinely
// fails under the same condition - the exact contrast this feature exists
// to fix for the container's FROM scratch runtime image.
func TestMigrateUpEmbedded_NoMigrationsDirectoryOnDisk_StillApplies(t *testing.T) {
	dsn := newScratchDatabase(t)

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() returned unexpected error: %v", err)
	}
	emptyDir := t.TempDir()
	if err := os.Chdir(emptyDir); err != nil {
		t.Fatalf("os.Chdir() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalWD); err != nil {
			t.Fatalf("failed to restore working directory: %v", err)
		}
	})

	// Contrast: the disk-based MigrateUp cannot find a relative
	// "migrations" directory here - it does not exist under emptyDir.
	if err := MigrateUp(dsn, "migrations"); err == nil {
		t.Fatal("MigrateUp() with no migrations/ on disk returned nil error, want a file-not-found error")
	}

	// The embedded path has no such dependency - it must still succeed.
	if err := MigrateUpEmbedded(dsn); err != nil {
		t.Fatalf("MigrateUpEmbedded() with no migrations/ on disk returned unexpected error: %v", err)
	}
}
