package db

import (
	"context"
	"fmt"
	"time"
)

// Service is a monitored service, linked to a Datadog SLO by SLOID.
// CurrentStatus defaults to "not_configured" until the poller (Phase 4) has
// fetched a status for it at least once.
type Service struct {
	ID                 string
	Name               string
	SLOID              string
	CurrentStatus      string
	LastStatusChangeAt time.Time
}

// ServiceRepository accesses the services table.
type ServiceRepository struct {
	pool *Pool
}

// NewServiceRepository builds a ServiceRepository backed by pool.
func NewServiceRepository(pool *Pool) *ServiceRepository {
	return &ServiceRepository{pool: pool}
}

// Create inserts service, filling in its generated ID, CurrentStatus, and
// LastStatusChangeAt.
func (r *ServiceRepository) Create(ctx context.Context, service *Service) error {
	row := r.pool.QueryRow(ctx,
		"INSERT INTO services (name, slo_id) VALUES ($1, $2) RETURNING id, current_status, last_status_change_at",
		service.Name, service.SLOID,
	)

	if err := row.Scan(&service.ID, &service.CurrentStatus, &service.LastStatusChangeAt); err != nil {
		return fmt.Errorf("db: failed to create service: %w", err)
	}

	return nil
}

// List returns every registered service, ordered by name, with its current
// status.
func (r *ServiceRepository) List(ctx context.Context) ([]Service, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT id, name, slo_id, current_status, last_status_change_at FROM services ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("db: failed to list services: %w", err)
	}
	defer rows.Close()

	var services []Service
	for rows.Next() {
		var service Service
		if err := rows.Scan(&service.ID, &service.Name, &service.SLOID, &service.CurrentStatus, &service.LastStatusChangeAt); err != nil {
			return nil, fmt.Errorf("db: failed to scan service: %w", err)
		}
		services = append(services, service)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: failed to iterate services: %w", err)
	}

	return services, nil
}

// ListForStatusPage returns every service linked to statusPageID via the
// status_page_services junction table, ordered by name, with its current
// status. This is the scoped counterpart to List (SP-15): a status page's
// public page must show only its own linked services, never every service
// in the installation.
func (r *ServiceRepository) ListForStatusPage(ctx context.Context, statusPageID string) ([]Service, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT s.id, s.name, s.slo_id, s.current_status, s.last_status_change_at
		 FROM services s
		 JOIN status_page_services sps ON sps.service_id = s.id
		 WHERE sps.status_page_id = $1
		 ORDER BY s.name`,
		statusPageID,
	)
	if err != nil {
		return nil, fmt.Errorf("db: failed to list services for status page: %w", err)
	}
	defer rows.Close()

	var services []Service
	for rows.Next() {
		var service Service
		if err := rows.Scan(&service.ID, &service.Name, &service.SLOID, &service.CurrentStatus, &service.LastStatusChangeAt); err != nil {
			return nil, fmt.Errorf("db: failed to scan service: %w", err)
		}
		services = append(services, service)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: failed to iterate services: %w", err)
	}

	return services, nil
}

// UpdateStatus sets service serviceID's current_status to status. It only
// touches last_status_change_at when status actually differs from the
// stored value, so a repeated "operational" poll result doesn't fake a
// status change.
func (r *ServiceRepository) UpdateStatus(ctx context.Context, serviceID, status string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE services
		 SET current_status = $2,
		     last_status_change_at = CASE WHEN current_status <> $2 THEN now() ELSE last_status_change_at END
		 WHERE id = $1`,
		serviceID, status,
	)
	if err != nil {
		return fmt.Errorf("db: failed to update service status: %w", err)
	}

	return nil
}
