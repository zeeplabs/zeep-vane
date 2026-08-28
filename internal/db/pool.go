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

// maxConns caps how many connections a single *Pool ever opens. Left to
// pgxpool's own default (max(4, runtime.NumCPU())) this was large enough
// that vane's own integration test suite - dozens of packages, each
// opening its own *Pool - could collectively approach or exceed
// Postgres's max_connections (100 by default) under `go test
// -tags=integration ./...`'s default package-level parallelism, causing
// intermittent "FATAL: sorry, too many clients already" (SQLSTATE 53300)
// in whichever query happened to lose the race - reproduced live against
// a deliberately-lowered max_connections. A single vane process (the only
// production caller of NewPool) never needs anywhere close to 4
// concurrent connections at this MVP's scale, so the same low cap is safe
// for both.
const maxConns = 4

// NewPool parses dsn and creates a connection pool. It returns a clear error
// if dsn is malformed. It does not eagerly connect; use Ping to verify
// reachability.
func NewPool(ctx context.Context, dsn string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("db: invalid DSN: %w", err)
	}
	cfg.MaxConns = maxConns

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
