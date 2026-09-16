package db

import (
	"context"
	"fmt"
)

// systemSessionFlagSQL sets the session flag the tenants table's
// system_iteration_read policy (0027, AD-024) checks. Written as a fixed
// literal with no parameters on purpose: nothing about this statement can
// be influenced by a caller, so no request data can ever end up deciding
// whether a session counts as the system. This is the ONLY place in the
// codebase that sets app.is_system - internal/db/system_flag_scope_test.go
// fails if that stops being true.
const systemSessionFlagSQL = `SELECT set_config('app.is_system', 'true', true)`

// SystemTenantLister enumerates every active tenant for trusted internal
// server code that has no tenant of its own - specifically the poller,
// which must iterate all tenants (T15, TENANT-04) and therefore hits the
// bootstrap paradox AD-024 describes: reading tenants requires
// app.tenant_id, which is exactly what the enumeration would determine.
//
// It exists as its own type rather than a method on Pool or
// TenantRepository so the privileged flag has one narrow, greppable owner:
// the only thing it can ever do is SELECT the tenant list. It is never
// constructed from an HTTP handler, and nothing it does depends on request
// input.
type SystemTenantLister struct {
	pool    *Pool
	tenants *TenantRepository
}

// NewSystemTenantLister builds a SystemTenantLister over pool.
func NewSystemTenantLister(pool *Pool) *SystemTenantLister {
	return &SystemTenantLister{pool: pool, tenants: NewTenantRepository(pool)}
}

// List returns every active tenant, oldest first, readable under a real
// non-superuser role because it runs inside a transaction whose
// app.is_system flag is set (AD-024).
//
// The flag's scope is deliberately as small as it can be: it is SET LOCAL
// on a transaction this method opens itself, that transaction runs the
// enumeration query and nothing else, and it is always rolled back (the
// read needs no commit). Callers get a plain slice back - no context, no
// transaction, nothing carrying the flag - so the per-tenant work that
// follows cannot accidentally inherit it and must go through the ordinary
// app.tenant_id-scoped path (Pool.BeginTenantTx) like every other caller.
func (l *SystemTenantLister) List(ctx context.Context) ([]Tenant, error) {
	tx, err := l.pool.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("db: failed to begin system transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, systemSessionFlagSQL); err != nil {
		return nil, fmt.Errorf("db: failed to set app.is_system: %w", err)
	}

	tenants, err := l.tenants.List(WithTenantTx(ctx, tx))
	if err != nil {
		return nil, err
	}

	return tenants, nil
}
