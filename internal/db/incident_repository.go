package db

import (
	"context"
	"fmt"
	"time"
)

// Incident is a manually-managed incident post, optionally linked to one or
// more services, communicating human context beyond the automated status
// (spec.md P1: "Gerenciar incidentes manuais").
type Incident struct {
	ID         string
	Title      string
	Status     string // "investigating" | "identified" | "monitoring" | "resolved"
	CreatedAt  time.Time
	ResolvedAt *time.Time
}

// IncidentRepository accesses the incidents, incident_services, and
// incident_updates tables.
type IncidentRepository struct {
	pool *Pool
}

// NewIncidentRepository builds an IncidentRepository backed by pool.
func NewIncidentRepository(pool *Pool) *IncidentRepository {
	return &IncidentRepository{pool: pool}
}

// Create inserts incident and links it to every service in serviceIDs,
// filling in incident's generated ID, Status (the DB default,
// "investigating"), and CreatedAt (SP-16). The insert and the service links
// are wrapped in a single transaction, mirroring StatusPageRepository.Create:
// an incident is never left without its intended service links because a
// later insert failed partway through.
func (r *IncidentRepository) Create(ctx context.Context, incident *Incident, serviceIDs []string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: failed to begin incident create transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row := tx.QueryRow(ctx,
		"INSERT INTO incidents (title) VALUES ($1) RETURNING id, status, created_at",
		incident.Title,
	)
	if err := row.Scan(&incident.ID, &incident.Status, &incident.CreatedAt); err != nil {
		return fmt.Errorf("db: failed to create incident: %w", err)
	}

	for _, serviceID := range serviceIDs {
		if _, err := tx.Exec(ctx,
			"INSERT INTO incident_services (incident_id, service_id) VALUES ($1, $2)",
			incident.ID, serviceID,
		); err != nil {
			return fmt.Errorf("db: failed to link service %s to incident: %w", serviceID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: failed to commit incident create transaction: %w", err)
	}

	return nil
}
