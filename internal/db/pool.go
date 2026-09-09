// Package db provides the Postgres connection pool used by vane.
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool wraps pgxpool.Pool to allow future extension without breaking the
// external contract.
type Pool struct {
	*pgxpool.Pool
}

// tenantTxContextKey is the context key WithTenantTx stores a request- or
// operation-scoped transaction under.
type tenantTxContextKey struct{}

// WithTenantTx returns a context carrying tx, so that every subsequent call
// to (*Pool).QueryRow/Query/Exec made with that context runs on tx instead
// of checking out a fresh pooled connection. This is what makes RLS's
// session settings (app.tenant_id, app.user_id - see BeginTenantTx) visible
// to every query for the transaction's lifetime: Postgres scopes
// set_config(..., true) ("SET LOCAL") to the current transaction only, so
// every query that must see it has to run on that same transaction, not
// merely the same *Pool. Existing repositories need no code change to
// benefit from this - they already call ctx-taking methods on *Pool, and
// those methods now check ctx for a tenant transaction first.
func WithTenantTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, tenantTxContextKey{}, tx)
}

// TenantTxFromContext returns the transaction stored by WithTenantTx, and
// whether one was present.
func TenantTxFromContext(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(tenantTxContextKey{}).(pgx.Tx)
	return tx, ok
}

// BeginTenantTx opens a transaction and sets the RLS session settings every
// tenant-scoped policy in the 0024 migration checks: app.user_id (readable
// independent of an active tenant - tenant_memberships needs this before a
// tenant is even chosen, e.g. at login) and app.tenant_id (the active
// tenant once one is known). Either id may be "" to leave that setting
// unset, which every policy treats as fail-closed (zero rows), never an
// error or a cross-tenant leak. Callers must WithTenantTx the returned tx
// into the context passed to any repository call, and always Commit or
// Rollback it.
func (p *Pool) BeginTenantTx(ctx context.Context, userID, tenantID string) (pgx.Tx, error) {
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("db: failed to begin tenant transaction: %w", err)
	}
	if userID != "" {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.user_id', $1, true)", userID); err != nil {
			_ = tx.Rollback(ctx)
			return nil, fmt.Errorf("db: failed to set app.user_id: %w", err)
		}
	}
	if tenantID != "" {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
			_ = tx.Rollback(ctx)
			return nil, fmt.Errorf("db: failed to set app.tenant_id: %w", err)
		}
	}
	return tx, nil
}

// QueryRow overrides pgxpool.Pool's QueryRow: if ctx carries a transaction
// via WithTenantTx, it runs there so RLS session settings apply; otherwise
// it falls back to a pooled connection exactly as before.
func (p *Pool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if tx, ok := TenantTxFromContext(ctx); ok {
		return tx.QueryRow(ctx, sql, args...)
	}
	return p.Pool.QueryRow(ctx, sql, args...)
}

// Query overrides pgxpool.Pool's Query - see QueryRow.
func (p *Pool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if tx, ok := TenantTxFromContext(ctx); ok {
		return tx.Query(ctx, sql, args...)
	}
	return p.Pool.Query(ctx, sql, args...)
}

// Exec overrides pgxpool.Pool's Exec - see QueryRow.
func (p *Pool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if tx, ok := TenantTxFromContext(ctx); ok {
		return tx.Exec(ctx, sql, args...)
	}
	return p.Pool.Exec(ctx, sql, args...)
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
