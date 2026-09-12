package db

import (
	"context"
	"fmt"
)

const (
	NotificationTypeIncidentOpened   = "incident_opened"
	NotificationTypeIncidentResolved = "incident_resolved"
	NotificationTypeWeeklyDigest     = "weekly_digest"
)

// notificationDefaultEnabled is the documented default for a user with no
// stored row: the two incident emails on, the weekly digest off. It matches
// the redesigned Meu Perfil mock's own seeded state, and it means users
// created before this feature get sensible defaults without a backfill
// writing a row per existing user.
var notificationDefaultEnabled = map[string]bool{
	NotificationTypeIncidentOpened:   true,
	NotificationTypeIncidentResolved: true,
	NotificationTypeWeeklyDigest:     false,
}

// NotificationDefaultEnabled reports the documented default for
// notificationType when a user has no stored row: the two incident types
// default on, the weekly digest defaults off.
func NotificationDefaultEnabled(notificationType string) bool {
	return notificationDefaultEnabled[notificationType]
}

// NotificationPreferenceRepository owns the `notification_preferences` table
// (migration 0032): one narrow row per (user, notification type). Like
// `sessions`, this is personal data, not tenant-scoped - it has no tenant_id
// and no RLS policy; the caller scopes every write to the authenticated
// user's own id.
type NotificationPreferenceRepository struct {
	pool *Pool
}

// NewNotificationPreferenceRepository builds a repository backed by pool.
func NewNotificationPreferenceRepository(pool *Pool) *NotificationPreferenceRepository {
	return &NotificationPreferenceRepository{pool: pool}
}

// Get returns the stored rows for userID as a type->enabled map. A type with
// no row is absent from the map; applying defaults is the caller's concern.
// The table's CHECK constraint only admits the three known types, so no
// unknown key can come back.
func (r *NotificationPreferenceRepository) Get(ctx context.Context, userID string) (map[string]bool, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT notification_type, enabled FROM notification_preferences WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("db: failed to list notification preferences: %w", err)
	}
	defer rows.Close()

	values := make(map[string]bool)
	for rows.Next() {
		var notificationType string
		var enabled bool
		if err := rows.Scan(&notificationType, &enabled); err != nil {
			return nil, fmt.Errorf("db: failed to scan notification preference row: %w", err)
		}
		values[notificationType] = enabled
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: error iterating notification preference rows: %w", err)
	}
	return values, nil
}

// Upsert writes exactly the provided keys for userID, creating or updating
// each row and leaving any omitted type untouched. Two or three rows at most
// per call, so one statement per key is simpler than a batch and needs no
// transaction plumbing.
func (r *NotificationPreferenceRepository) Upsert(ctx context.Context, userID string, values map[string]bool) error {
	for notificationType, enabled := range values {
		if _, err := r.pool.Exec(ctx,
			`INSERT INTO notification_preferences (user_id, notification_type, enabled)
			 VALUES ($1, $2, $3)
			 ON CONFLICT (user_id, notification_type) DO UPDATE SET enabled = EXCLUDED.enabled`,
			userID, notificationType, enabled,
		); err != nil {
			return fmt.Errorf("db: failed to upsert notification preference %q: %w", notificationType, err)
		}
	}
	return nil
}

// ResolveEnabledForUsers returns which of userIDs have notificationType
// resolved to true, treating a missing row as the documented default (the two
// incident types true, the weekly digest false). It batches the lookup so the
// notification fan-out never issues one query per recipient.
func (r *NotificationPreferenceRepository) ResolveEnabledForUsers(ctx context.Context, userIDs []string, notificationType string) (map[string]bool, error) {
	resolved := make(map[string]bool, len(userIDs))
	if len(userIDs) == 0 {
		return resolved, nil
	}

	stored := make(map[string]bool, len(userIDs))
	rows, err := r.pool.Query(ctx,
		`SELECT user_id, enabled FROM notification_preferences
		 WHERE notification_type = $1 AND user_id = ANY($2)`,
		notificationType, userIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("db: failed to resolve notification preferences: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var userID string
		var enabled bool
		if err := rows.Scan(&userID, &enabled); err != nil {
			return nil, fmt.Errorf("db: failed to scan resolved notification preference: %w", err)
		}
		stored[userID] = enabled
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: error iterating resolved notification preferences: %w", err)
	}

	def := notificationDefaultEnabled[notificationType]
	for _, userID := range userIDs {
		if enabled, ok := stored[userID]; ok {
			resolved[userID] = enabled
			continue
		}
		resolved[userID] = def
	}
	return resolved, nil
}
