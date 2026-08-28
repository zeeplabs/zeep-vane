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

// ListPaginated returns one page of registered services, ordered by name,
// with each service's current status (PAG-08). total is computed via
// COUNT(*) OVER() in the same query, with a zero-row fallback COUNT(*),
// same pattern as IncidentRepository/DomainRepository's ListPaginated.
func (r *ServiceRepository) ListPaginated(ctx context.Context, page, pageSize int) ([]Service, int, error) {
	offset := (page - 1) * pageSize

	rows, err := r.pool.Query(ctx,
		`SELECT id, name, slo_id, current_status, last_status_change_at, COUNT(*) OVER() AS total
		 FROM services
		 ORDER BY name
		 LIMIT $1 OFFSET $2`,
		pageSize, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("db: failed to list services: %w", err)
	}
	defer rows.Close()

	services := []Service{}
	total := 0
	for rows.Next() {
		var service Service
		if err := rows.Scan(&service.ID, &service.Name, &service.SLOID, &service.CurrentStatus, &service.LastStatusChangeAt, &total); err != nil {
			return nil, 0, fmt.Errorf("db: failed to scan service: %w", err)
		}
		services = append(services, service)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("db: failed to iterate services: %w", err)
	}

	if len(services) == 0 {
		total, err = r.countServices(ctx)
		if err != nil {
			return nil, 0, err
		}
	}

	return services, total, nil
}

// countServices is the zero-row fallback for ListPaginated's total (PAG-08).
func (r *ServiceRepository) countServices(ctx context.Context) (int, error) {
	var total int
	row := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM services")
	if err := row.Scan(&total); err != nil {
		return 0, fmt.Errorf("db: failed to count services: %w", err)
	}
	return total, nil
}

// List returns every registered service, ordered by name, with its current
// status. Kept unpaginated (not removed, unlike DomainRepository.List) -
// internal/poller/poller.go polls every service every cycle and must never
// see only one page (SPEC_DEVIATION already documented by design.md itself:
// this is the ServiceRepository/poller.go precedent, not a new deviation).
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
