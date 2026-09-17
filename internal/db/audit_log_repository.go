package db

import (
	"context"
	"fmt"
	"time"
)

// AuditLogEntry is one admin_audit_log row joined to the actor's current
// user record (recent-team-activity feature). TargetLabel is nil for
// historical rows written before the target_label column existed
// (migration 0037) - the caller renders those gracefully, never as a
// literal "null". ActorDeleted is true when the actor's user row no
// longer exists (hard-deleted account); ActorName is then empty and the
// caller substitutes a locale-appropriate placeholder.
type AuditLogEntry struct {
	Action       string
	TargetLabel  *string
	ActorName    string
	ActorDeleted bool
	CreatedAt    time.Time
}

// AuditLogRepository reads admin_audit_log for the recent-team-activity
// feature's read path.
type AuditLogRepository struct {
	pool *Pool
}

// NewAuditLogRepository builds an AuditLogRepository backed by pool.
func NewAuditLogRepository(pool *Pool) *AuditLogRepository {
	return &AuditLogRepository{pool: pool}
}

// ListRecent returns the caller's tenant's most recent audit entries,
// most recent first, capped at limit. Tenant isolation comes entirely
// from admin_audit_log's existing RLS policy (0024_multi_tenancy_core) -
// this query deliberately carries no "WHERE tenant_id = ..." of its own
// (design.md: AuditLogRepository). users carries no tenant_id (global
// identity, 0024's own doc comment) so the LEFT JOIN needs no additional
// scoping: actor_id was itself only ever written by an actor whose action
// was already tenant-scoped at write time.
func (r *AuditLogRepository) ListRecent(ctx context.Context, limit int) ([]AuditLogEntry, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT a.action, a.target_label, a.created_at, COALESCE(u.name, '') AS actor_name, (u.id IS NULL) AS actor_deleted
		 FROM admin_audit_log a
		 LEFT JOIN users u ON u.id = a.actor_id
		 ORDER BY a.created_at DESC
		 LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("db: failed to list recent audit log entries: %w", err)
	}
	defer rows.Close()

	var entries []AuditLogEntry
	for rows.Next() {
		var entry AuditLogEntry
		if err := rows.Scan(&entry.Action, &entry.TargetLabel, &entry.CreatedAt, &entry.ActorName, &entry.ActorDeleted); err != nil {
			return nil, fmt.Errorf("db: failed to scan audit log entry: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: failed to list recent audit log entries: %w", err)
	}

	return entries, nil
}
