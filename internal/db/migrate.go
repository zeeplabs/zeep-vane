package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// MigrateUp applies all pending migrations found in migrationsDir against
// dsn. It is idempotent: running it again when nothing is pending is a
// no-op, never a duplicate application.
func MigrateUp(dsn, migrationsDir string) error {
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("db: failed to open sql connection: %w", err)
	}
	defer sqlDB.Close()

	driver, err := pgxmigrate.WithInstance(sqlDB, &pgxmigrate.Config{})
	if err != nil {
		return fmt.Errorf("db: failed to create migrate driver: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance(fmt.Sprintf("file://%s", migrationsDir), "pgx5", driver)
	if err != nil {
		return fmt.Errorf("db: failed to initialize migrator: %w", err)
	}

	// ErrNoChange means every migration is already applied. os.ErrNotExist
	// is returned by the file source when the migrations directory has no
	// migration files at all. Both are no-ops, not failures.
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("db: migration up failed: %w", err)
	}

	return nil
}
