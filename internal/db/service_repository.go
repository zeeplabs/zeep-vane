package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Service is a monitored service, monitored either via a Datadog SLO
// (MonitorMode "slo", linked by SLOID) or via direct HTTP/TCP/Ping polling
// (MonitorMode "polling", configured by PollType/PollTarget/
// PollIntervalSeconds - manual-polling-monitoring design.md). CurrentStatus
// defaults to "not_configured" until a poller (the Datadog Poller or, for a
// polling-mode service, poller.ManualScheduler) has fetched a status for it
// at least once.
type Service struct {
	ID   string
	Name string
	// SLOID is '' for a polling-mode service (manual-polling-monitoring
	// MP-01/MP-05).
	//
	// SPEC_DEVIATION: design.md's data model calls for this field to become
	// *string (nil for monitor_mode="polling"). Kept as string instead: the
	// column itself is nullable (0034_service_polling_mode), but every read
	// path scans it via COALESCE(slo_id, '') so the Go type never has to
	// change. Changing it to *string would ripple into every existing
	// caller across internal/api, internal/poller, and internal/cli that
	// already treats SLOID as a plain string (15+ files) - well outside
	// this task's scope, and '' already unambiguously means "no SLO" here,
	// matching the SLOName field's own precedent below.
	SLOID string
	// SLOName is the linked SLO's human name, recorded at link time
	// (monitored-services-page design.md) so no read path needs a live
	// Datadog call to display it. '' for a row created before this field
	// existed and never re-saved, or for a polling-mode service (no SLO).
	SLOName string
	// MonitorMode is "slo" (default, unchanged existing behavior) or
	// "polling" (manual-polling-monitoring MP-01/MP-02).
	MonitorMode string
	// PollType/PollTarget/PollIntervalSeconds are set only for
	// MonitorMode="polling" (nil otherwise) - manual-polling-monitoring
	// MP-01/MP-02/MP-06.
	PollType            *string
	PollTarget          *string
	PollIntervalSeconds *int
	CurrentStatus       string
	LastStatusChangeAt  time.Time
	// StatusAnalysis is the LLM-generated degraded-tooltip text (AI-14),
	// non-nil only while the service is "degraded" - UpdateStatusAnalysis
	// clears it to NULL synchronously on entering/leaving that state
	// (AI-15/AI-17). Populated by ListForStatusPage, the only read path
	// that needs it (public status page tooltip); List/ListPaginated leave
	// it at its zero value since neither caller (poller, admin services
	// list) reads it.
	StatusAnalysis *string
}

// ErrServiceInUse is returned by SoftDelete when the service is still
// referenced by a status_page_services row (service-delete spec.md
// SVCDEL-03) - same "block, never silently unlink" convention
// DomainRepository.Delete's ErrDomainInUse uses, except here there is no
// natural FK-violation to catch (a soft delete never touches
// status_page_services), so the check is explicit.
var ErrServiceInUse = errors.New("db: service is still attached to a status page")

// ServiceRepository accesses the services table.
type ServiceRepository struct {
	pool *Pool
}

// NewServiceRepository builds a ServiceRepository backed by pool.
func NewServiceRepository(pool *Pool) *ServiceRepository {
	return &ServiceRepository{pool: pool}
}

// Create inserts service, filling in its generated ID, CurrentStatus, and
// LastStatusChangeAt. The INSERT's column list branches on
// service.MonitorMode (manual-polling-monitoring MP-01/MP-02): "polling"
// persists PollType/PollTarget/PollIntervalSeconds and leaves slo_id/
// slo_name NULL/empty; anything else (including "", every existing caller's
// zero value) persists slo_id/slo_name unchanged from today and normalizes
// service.MonitorMode to "slo" on success.
func (r *ServiceRepository) Create(ctx context.Context, service *Service) error {
	var row pgx.Row
	if service.MonitorMode == "polling" {
		row = r.pool.QueryRow(ctx,
			`INSERT INTO services (name, monitor_mode, poll_type, poll_target, poll_interval_seconds)
			 VALUES ($1, 'polling', $2, $3, $4) RETURNING id, current_status, last_status_change_at`,
			service.Name, service.PollType, service.PollTarget, service.PollIntervalSeconds,
		)
	} else {
		row = r.pool.QueryRow(ctx,
			"INSERT INTO services (name, slo_id, slo_name) VALUES ($1, $2, $3) RETURNING id, current_status, last_status_change_at",
			service.Name, service.SLOID, service.SLOName,
		)
	}

	if err := row.Scan(&service.ID, &service.CurrentStatus, &service.LastStatusChangeAt); err != nil {
		return fmt.Errorf("db: failed to create service: %w", err)
	}

	if service.MonitorMode != "polling" {
		service.MonitorMode = "slo"
	}

	return nil
}

// Get returns the service identified by id, including SLOName,
// CurrentStatus, and StatusAnalysis - the full row the monitored-services
// detail drawer needs (SVC-14). Returns found=false (no error) if no
// service matches id, the same not-found convention
// IncidentRepository.HasOpenIncidentForService uses.
func (r *ServiceRepository) Get(ctx context.Context, id string) (*Service, bool, error) {
	var service Service
	row := r.pool.QueryRow(ctx,
		"SELECT id, name, COALESCE(slo_id, ''), slo_name, current_status, last_status_change_at, status_analysis FROM services WHERE id = $1 AND deleted_at IS NULL",
		id,
	)
	if err := row.Scan(&service.ID, &service.Name, &service.SLOID, &service.SLOName, &service.CurrentStatus, &service.LastStatusChangeAt, &service.StatusAnalysis); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("db: failed to get service: %w", err)
	}

	return &service, true, nil
}

// ListPaginated returns one page of registered services, ordered by name,
// with each service's current status (PAG-08). total is computed via
// COUNT(*) OVER() in the same query, with a zero-row fallback COUNT(*),
// same pattern as IncidentRepository/DomainRepository's ListPaginated.
func (r *ServiceRepository) ListPaginated(ctx context.Context, page, pageSize int) ([]Service, int, error) {
	offset := (page - 1) * pageSize

	rows, err := r.pool.Query(ctx,
		`SELECT id, name, COALESCE(slo_id, ''), slo_name, current_status, last_status_change_at, COUNT(*) OVER() AS total
		 FROM services
		 WHERE deleted_at IS NULL
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
		if err := rows.Scan(&service.ID, &service.Name, &service.SLOID, &service.SLOName, &service.CurrentStatus, &service.LastStatusChangeAt, &total); err != nil {
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
	row := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM services WHERE deleted_at IS NULL")
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
		"SELECT id, name, COALESCE(slo_id, ''), monitor_mode, current_status, last_status_change_at FROM services WHERE deleted_at IS NULL ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("db: failed to list services: %w", err)
	}
	defer rows.Close()

	var services []Service
	for rows.Next() {
		var service Service
		if err := rows.Scan(&service.ID, &service.Name, &service.SLOID, &service.MonitorMode, &service.CurrentStatus, &service.LastStatusChangeAt); err != nil {
			return nil, fmt.Errorf("db: failed to scan service: %w", err)
		}
		services = append(services, service)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: failed to iterate services: %w", err)
	}

	return services, nil
}

// ListPollingManual returns every polling-manual service (monitor_mode =
// 'polling'), ordered by name - the discovery query
// poller.ManualScheduler's reconciliation loop uses (manual-polling-
// monitoring MP-06). Always a non-nil slice, empty (not nil) when no
// polling-manual service exists, matching ListPaginated's zero-row
// convention.
func (r *ServiceRepository) ListPollingManual(ctx context.Context) ([]Service, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, name, poll_type, poll_target, poll_interval_seconds, current_status, last_status_change_at
		 FROM services
		 WHERE monitor_mode = 'polling' AND deleted_at IS NULL
		 ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("db: failed to list polling-manual services: %w", err)
	}
	defer rows.Close()

	services := []Service{}
	for rows.Next() {
		var service Service
		if err := rows.Scan(&service.ID, &service.Name, &service.PollType, &service.PollTarget, &service.PollIntervalSeconds, &service.CurrentStatus, &service.LastStatusChangeAt); err != nil {
			return nil, fmt.Errorf("db: failed to scan polling-manual service: %w", err)
		}
		service.MonitorMode = "polling"
		services = append(services, service)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: failed to iterate polling-manual services: %w", err)
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
		`SELECT s.id, s.name, COALESCE(s.slo_id, ''), s.current_status, s.last_status_change_at, s.status_analysis
		 FROM services s
		 JOIN status_page_services sps ON sps.service_id = s.id
		 WHERE sps.status_page_id = $1 AND s.deleted_at IS NULL
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
		if err := rows.Scan(&service.ID, &service.Name, &service.SLOID, &service.CurrentStatus, &service.LastStatusChangeAt, &service.StatusAnalysis); err != nil {
			return nil, fmt.Errorf("db: failed to scan service: %w", err)
		}
		services = append(services, service)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: failed to iterate services: %w", err)
	}

	return services, nil
}

// Update renames the service identified by id (service-edit spec.md
// SVCEDIT-01/03/04: name only - monitor_mode/slo_id/poll_* are deliberately
// out of scope for this method, since changing them requires resetting
// poller-side in-memory hysteresis state, a separate feature). Returns
// ErrNotFound if no service matches id, the same not-found convention
// DomainRepository.Delete uses.
func (r *ServiceRepository) Update(ctx context.Context, id, name string) error {
	tag, err := r.pool.Exec(ctx, "UPDATE services SET name = $2 WHERE id = $1 AND deleted_at IS NULL", id, name)
	if err != nil {
		return fmt.Errorf("db: failed to update service: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SoftDelete marks the service identified by id as deleted (service-delete
// SVCDEL-01/02/07): sets deleted_at, never removes the row or touches
// status_intervals/incidents (no cascade). Blocked with ErrServiceInUse if
// any status_page_services row still references id (SVCDEL-03) - checked
// inside the same transaction as the update to avoid a race between the
// check and the write. Returns ErrNotFound if id doesn't exist or is
// already soft-deleted (idempotent: a second delete call 404s the same way
// GET does, per spec.md Assumptions).
func (r *ServiceRepository) SoftDelete(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: failed to begin soft-delete transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the services row FOR UPDATE before checking status_page_services
	// (Verifier finding, service-delete validation.md M2): an INSERT into
	// status_page_services implicitly takes a FOR KEY SHARE lock on the
	// referenced services row as part of its FK check, and Postgres's lock
	// matrix has FOR UPDATE conflict with FOR KEY SHARE - so this genuinely
	// serializes against a concurrent attach, the same way AttachDomain's
	// own SELECT ... FOR UPDATE serializes against concurrent attaches on
	// status_pages (status_page_repository.go). Without this lock, a
	// concurrent attach could commit between the EXISTS check below and the
	// UPDATE, leaving a service both deleted and attached - exactly the
	// state spec.md's Assumptions table says cannot occur.
	var lockedID string
	err = tx.QueryRow(ctx, "SELECT id FROM services WHERE id = $1 FOR UPDATE", id).Scan(&lockedID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("db: failed to lock service row: %w", err)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}

	var inUse bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM status_page_services WHERE service_id = $1)", id).Scan(&inUse); err != nil {
		return fmt.Errorf("db: failed to check status_page_services for service: %w", err)
	}
	if inUse {
		return ErrServiceInUse
	}

	tag, err := tx.Exec(ctx, "UPDATE services SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL", id)
	if err != nil {
		return fmt.Errorf("db: failed to soft-delete service: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: failed to commit soft-delete transaction: %w", err)
	}
	return nil
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

// UpdateStatusAnalysis sets serviceID's status_analysis column to analysis -
// a nullable-set update: passing nil clears the column (AI-15/AI-17,
// SLOAnalyzer clears it synchronously on entering/leaving "degraded"),
// passing a non-nil value sets it (AI-14, the async LLM-generated tooltip
// text). Mirrors UpdateStatus's single-column-update shape; unlike
// UpdateStatus, it does not touch last_status_change_at.
func (r *ServiceRepository) UpdateStatusAnalysis(ctx context.Context, serviceID string, analysis *string) error {
	_, err := r.pool.Exec(ctx,
		"UPDATE services SET status_analysis = $2 WHERE id = $1",
		serviceID, analysis,
	)
	if err != nil {
		return fmt.Errorf("db: failed to update service status analysis: %w", err)
	}

	return nil
}
