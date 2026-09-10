// Package audit records sensitive admin-management actions (invite, role
// change, removal) to an append-only log.
package audit

import (
	"context"
	"fmt"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// Log writes entries to the admin_audit_log table.
type Log struct {
	pool *db.Pool
}

// NewLog builds a Log backed by pool.
func NewLog(pool *db.Pool) *Log {
	return &Log{pool: pool}
}

// Record inserts an append-only audit entry: actorID performed action
// against targetID. There is no cascade delete tying rows here to the
// users table - removing a user must never remove the audit history that
// references it. tenant_id is left to the column's default (the session's
// app.tenant_id), so an entry always lands on the tenant whose request
// produced it, and an insert outside a tenant context is rejected by NOT
// NULL rather than landing on the wrong tenant.
func (l *Log) Record(ctx context.Context, actorID, targetID, action string) error {
	if _, err := l.pool.Exec(ctx,
		"INSERT INTO admin_audit_log (actor_id, target_id, action) VALUES ($1, $2, $3)",
		actorID, targetID, action,
	); err != nil {
		return fmt.Errorf("audit: failed to record entry: %w", err)
	}

	return nil
}
