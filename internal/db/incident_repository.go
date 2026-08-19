package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
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

// IncidentUpdate is a single timeline entry attached to an Incident.
type IncidentUpdate struct {
	ID         string
	IncidentID string
	Body       string
	CreatedAt  time.Time
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

// AddUpdate appends an update to incidentID's timeline (SP-17). It returns
// ErrNotFound if incidentID doesn't exist.
func (r *IncidentRepository) AddUpdate(ctx context.Context, incidentID, body string) (*IncidentUpdate, error) {
	if err := r.mustExist(ctx, incidentID); err != nil {
		return nil, err
	}

	update := &IncidentUpdate{IncidentID: incidentID, Body: body}
	row := r.pool.QueryRow(ctx,
		"INSERT INTO incident_updates (incident_id, body) VALUES ($1, $2) RETURNING id, created_at",
		incidentID, body,
	)
	if err := row.Scan(&update.ID, &update.CreatedAt); err != nil {
		return nil, fmt.Errorf("db: failed to add incident update: %w", err)
	}

	return update, nil
}

// ListUpdates returns incidentID's timeline, most recent update first
// (spec.md: "ordenado do mais recente para o mais antigo").
func (r *IncidentRepository) ListUpdates(ctx context.Context, incidentID string) ([]IncidentUpdate, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT id, incident_id, body, created_at FROM incident_updates WHERE incident_id = $1 ORDER BY created_at DESC",
		incidentID,
	)
	if err != nil {
		return nil, fmt.Errorf("db: failed to list incident updates: %w", err)
	}
	defer rows.Close()

	var updates []IncidentUpdate
	for rows.Next() {
		var update IncidentUpdate
		if err := rows.Scan(&update.ID, &update.IncidentID, &update.Body, &update.CreatedAt); err != nil {
			return nil, fmt.Errorf("db: failed to scan incident update: %w", err)
		}
		updates = append(updates, update)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: failed to iterate incident updates: %w", err)
	}

	return updates, nil
}

// Transition sets incidentID's status (SP-19), setting ResolvedAt when
// entering "resolved" and clearing it otherwise - so reopening an incident
// (e.g. "resolved" back to "investigating", SP-20) doesn't leave a stale
// resolution timestamp around. It records the transition on the incident's
// timeline in the same transaction and returns ErrNotFound if incidentID
// doesn't exist.
func (r *IncidentRepository) Transition(ctx context.Context, incidentID, status string) (*Incident, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("db: failed to begin incident transition transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	incident := &Incident{ID: incidentID}
	row := tx.QueryRow(ctx,
		`UPDATE incidents
		 SET status = $2,
		     resolved_at = CASE WHEN $2 = 'resolved' THEN now() ELSE NULL END
		 WHERE id = $1
		 RETURNING title, status, created_at, resolved_at`,
		incidentID, status,
	)
	if err := row.Scan(&incident.Title, &incident.Status, &incident.CreatedAt, &incident.ResolvedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to transition incident: %w", err)
	}

	if _, err := tx.Exec(ctx,
		"INSERT INTO incident_updates (incident_id, body) VALUES ($1, $2)",
		incidentID, "Status changed to "+status,
	); err != nil {
		return nil, fmt.Errorf("db: failed to record incident transition on timeline: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("db: failed to commit incident transition transaction: %w", err)
	}

	return incident, nil
}

// mustExist confirms incidentID exists, returning ErrNotFound if it doesn't.
func (r *IncidentRepository) mustExist(ctx context.Context, incidentID string) error {
	var exists bool
	row := r.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM incidents WHERE id = $1)", incidentID)
	if err := row.Scan(&exists); err != nil {
		return fmt.Errorf("db: failed to check incident existence: %w", err)
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}
