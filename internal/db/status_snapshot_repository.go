package db

import (
	"context"
	"fmt"
	"time"
)

// StatusSnapshot is a point-in-time record of a service's SLO status, as
// fetched from Datadog by the poller (Phase 4).
type StatusSnapshot struct {
	ID                   string
	ServiceID            string
	Status               string
	ErrorBudgetRemaining float64
	FetchedAt            time.Time
}

// StatusSnapshotRepository accesses the status_snapshots table.
type StatusSnapshotRepository struct {
	pool *Pool
}

// NewStatusSnapshotRepository builds a StatusSnapshotRepository backed by
// pool.
func NewStatusSnapshotRepository(pool *Pool) *StatusSnapshotRepository {
	return &StatusSnapshotRepository{pool: pool}
}

// Create inserts snapshot, filling in its generated ID and FetchedAt.
func (r *StatusSnapshotRepository) Create(ctx context.Context, snapshot *StatusSnapshot) error {
	row := r.pool.QueryRow(ctx,
		"INSERT INTO status_snapshots (service_id, status, error_budget_remaining) VALUES ($1, $2, $3) RETURNING id, fetched_at",
		snapshot.ServiceID, snapshot.Status, snapshot.ErrorBudgetRemaining,
	)

	if err := row.Scan(&snapshot.ID, &snapshot.FetchedAt); err != nil {
		return fmt.Errorf("db: failed to create status snapshot: %w", err)
	}

	return nil
}

// LatestFetchedAtByService returns each service's most recent
// StatusSnapshot.FetchedAt, keyed by service_id - the real "last successful
// update" timestamp the public status page must show (SP-08, SP-09). It
// reads only what the poller has already persisted: a service the poller
// hasn't reached yet (or whose Datadog connection is currently failing) just
// keeps its last recorded value here, never a fabricated "now".
func (r *StatusSnapshotRepository) LatestFetchedAtByService(ctx context.Context) (map[string]time.Time, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT service_id, MAX(fetched_at) FROM status_snapshots GROUP BY service_id")
	if err != nil {
		return nil, fmt.Errorf("db: failed to query latest status snapshots: %w", err)
	}
	defer rows.Close()

	latest := map[string]time.Time{}
	for rows.Next() {
		var serviceID string
		var fetchedAt time.Time
		if err := rows.Scan(&serviceID, &fetchedAt); err != nil {
			return nil, fmt.Errorf("db: failed to scan latest status snapshot: %w", err)
		}
		latest[serviceID] = fetchedAt
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: failed to iterate latest status snapshots: %w", err)
	}

	return latest, nil
}
