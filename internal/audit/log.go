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
// admins table - removing an Admin must never remove the audit history
// that references it.
func (l *Log) Record(ctx context.Context, actorID, targetID, action string) error {
	if _, err := l.pool.Exec(ctx,
		"INSERT INTO admin_audit_log (actor_id, target_id, action) VALUES ($1, $2, $3)",
		actorID, targetID, action,
	); err != nil {
		return fmt.Errorf("audit: failed to record entry: %w", err)
	}

	return nil
}
