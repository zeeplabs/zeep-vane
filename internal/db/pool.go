// Package db provides the Postgres connection pool used by vane.
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool wraps pgxpool.Pool to allow future extension without breaking the
// external contract.
type Pool struct {
	*pgxpool.Pool
}

// NewPool parses dsn and creates a connection pool. It returns a clear error
// if dsn is malformed. It does not eagerly connect; use Ping to verify
// reachability.
func NewPool(ctx context.Context, dsn string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("db: invalid DSN: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: failed to create pool: %w", err)
	}

	return &Pool{Pool: pool}, nil
}

// Ping checks whether the database is reachable, returning a clear error if
// not.
func (p *Pool) Ping(ctx context.Context) error {
	if err := p.Pool.Ping(ctx); err != nil {
		return fmt.Errorf("db: ping failed: %w", err)
	}
	return nil
}
