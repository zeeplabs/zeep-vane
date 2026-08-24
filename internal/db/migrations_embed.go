package db

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// MigrationsFS embeds every migration file in internal/db/migrations into
// the binary at build time, so a minimal runtime image (FROM scratch, no
// migrations/ directory on disk) can still apply them. This is additive:
// MigrateUp (file-based, disk-path) is kept unchanged for the CLI's
// `vane migrate up` and its 37 existing test call sites.
//
//go:embed migrations
var MigrationsFS embed.FS

// MigrateUpEmbedded applies all pending migrations from the embedded
// MigrationsFS against dsn, with no dependency on a migrations directory
// existing on disk. It shares MigrateUp's idempotency contract: running it
// again when nothing is pending is a no-op, never an error.
func MigrateUpEmbedded(dsn string) error {
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("db: failed to open sql connection: %w", err)
	}
	defer sqlDB.Close()

	driver, err := pgxmigrate.WithInstance(sqlDB, &pgxmigrate.Config{})
	if err != nil {
		return fmt.Errorf("db: failed to create migrate driver: %w", err)
	}

	sourceDriver, err := iofs.New(MigrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("db: failed to create embedded migration source: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", sourceDriver, "pgx5", driver)
	if err != nil {
		return fmt.Errorf("db: failed to initialize migrator: %w", err)
	}

	// ErrNoChange means every migration is already applied. os.ErrNotExist
	// mirrors MigrateUp's handling for an empty source. Both are no-ops,
	// not failures.
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("db: embedded migration up failed: %w", err)
	}

	return nil
}
