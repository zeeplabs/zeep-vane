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
