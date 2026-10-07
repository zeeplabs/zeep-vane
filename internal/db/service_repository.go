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
	// SLOType is the linked SLO's type ("metric" or "monitor"), and
	// DatadogServiceTag is its single service:-scoped tag - both resolved
	// once at SLO-link time (slo-root-cause-enrichment RCA-01), same
	// ""-means-absent convention as SLOName above. DatadogServiceTag is ""
	// when the SLO has 0 or 2+ service_tags entries (flow-type SLO) or for
	// a row saved before this feature existed.
	SLOType           string
	DatadogServiceTag string
	// Environment is a visual-only admin tag (service-environment-tag
	// SET-01): "" (default, same ""-means-absent convention as SLOName/
	// SLOType/DatadogServiceTag above) means no tag, otherwise one of
	// "development"/"staging"/"production" - enforced by the
	// 0047_service_environment CHECK constraint, not by this type. Purely
	// cosmetic (an admin-only badge): no business logic reads it.
	Environment string
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
	// GroupID, GroupName, ParentServiceID, and Position describe this
	// service's placement on one specific status page (status_page_services'
	// group_id/parent_service_id/position columns and
	// status_page_service_groups' name, migration 0046 - service-groups-
	// and-dependencies SGD-02/SGD-03/SGD-06). They are populated only by
	// ListForStatusPage; every other read path (Get, List, ListPaginated,
	// ListPollingManual, Create) has no status-page context to place a
	// service within and leaves them at their zero value (nil/nil/nil/0).
	// Added directly to the shared struct rather than a separate wrapper
	// type: no other caller serializes Service to JSON or otherwise reads
	// every field blindly (internal/api builds its own response DTOs), so
	// the always-empty fields in non-status-page contexts are inert, and
	// ListForStatusPage's return type ([]Service) must stay exactly what
	// api.ServiceLister's interface already declares.
	GroupID         *string
	GroupName       *string
	ParentServiceID *string
	Position        int
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
// zero value) persists slo_id/slo_name/slo_type/datadog_service_tag
// unchanged from today and normalizes service.MonitorMode to "slo" on
// success. SLOType/DatadogServiceTag are stored NULL (via NULLIF) when "",
// matching the nullable-column shape design.md defines for them.
func (r *ServiceRepository) Create(ctx context.Context, service *Service) error {
	var row pgx.Row
	if service.MonitorMode == "polling" {
		row = r.pool.QueryRow(ctx,
			`INSERT INTO services (name, monitor_mode, poll_type, poll_target, poll_interval_seconds, environment)
			 VALUES ($1, 'polling', $2, $3, $4, $5) RETURNING id, current_status, last_status_change_at`,
			service.Name, service.PollType, service.PollTarget, service.PollIntervalSeconds, service.Environment,
		)
	} else {
		row = r.pool.QueryRow(ctx,
			`INSERT INTO services (name, slo_id, slo_name, slo_type, datadog_service_tag, environment)
			 VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6)
			 RETURNING id, current_status, last_status_change_at`,
			service.Name, service.SLOID, service.SLOName, service.SLOType, service.DatadogServiceTag, service.Environment,
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
		`SELECT id, name, COALESCE(slo_id, ''), slo_name, COALESCE(slo_type, ''), COALESCE(datadog_service_tag, ''),
		        environment, current_status, last_status_change_at, status_analysis
		 FROM services WHERE id = $1 AND deleted_at IS NULL`,
		id,
	)
	if err := row.Scan(&service.ID, &service.Name, &service.SLOID, &service.SLOName, &service.SLOType, &service.DatadogServiceTag, &service.Environment, &service.CurrentStatus, &service.LastStatusChangeAt, &service.StatusAnalysis); err != nil {
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
		`SELECT id, name, COALESCE(slo_id, ''), slo_name, COALESCE(slo_type, ''), COALESCE(datadog_service_tag, ''),
		        environment, current_status, last_status_change_at, COUNT(*) OVER() AS total
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
		if err := rows.Scan(&service.ID, &service.Name, &service.SLOID, &service.SLOName, &service.SLOType, &service.DatadogServiceTag, &service.Environment, &service.CurrentStatus, &service.LastStatusChangeAt, &total); err != nil {
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
		`SELECT id, name, COALESCE(slo_id, ''), monitor_mode, COALESCE(slo_type, ''), COALESCE(datadog_service_tag, ''),
		        current_status, last_status_change_at
		 FROM services WHERE deleted_at IS NULL ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("db: failed to list services: %w", err)
	}
	defer rows.Close()

	var services []Service
	for rows.Next() {
		var service Service
		if err := rows.Scan(&service.ID, &service.Name, &service.SLOID, &service.MonitorMode, &service.SLOType, &service.DatadogServiceTag, &service.CurrentStatus, &service.LastStatusChangeAt); err != nil {
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
// status_page_services junction table, with its current status and its
// placement on this page (group_id/parent_service_id/position, plus the
// group's name via a LEFT JOIN - service-groups-and-dependencies SGD-02/
// SGD-03/SGD-06). This is the scoped counterpart to List (SP-15): a status
// page's public page must show only its own linked services, never every
// service in the installation.
//
// Ordering is ORDER BY g.position NULLS LAST, g.id, sps.position: grouped
// services sort by their group's own position first (so every service in
// an earlier group precedes every service in a later one), with the
// group's own id as a tie-break so two groups sharing a position (a
// concurrent CreateGroup race, or a manual UpdateGroup - see ListGroups'
// doc comment) don't interleave their services against each other, then by
// each service's own position within that group/ungrouped bucket;
// ungrouped services (g.position NULL, since the LEFT JOIN has no matching
// group row) sort after every grouped service, not interleaved by name as
// before this task. NULLS LAST is standard SQL and valid as written for
// pgx/Postgres (no query-builder translation needed - this file always
// writes raw SQL).
func (r *ServiceRepository) ListForStatusPage(ctx context.Context, statusPageID string) ([]Service, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT s.id, s.name, COALESCE(s.slo_id, ''), s.current_status, s.last_status_change_at, s.status_analysis,
		        sps.group_id, g.name, sps.parent_service_id, sps.position
		 FROM services s
		 JOIN status_page_services sps ON sps.service_id = s.id
		 LEFT JOIN status_page_service_groups g ON g.id = sps.group_id
		 WHERE sps.status_page_id = $1 AND s.deleted_at IS NULL
		 ORDER BY g.position NULLS LAST, g.id, sps.position`,
		statusPageID,
	)
	if err != nil {
		return nil, fmt.Errorf("db: failed to list services for status page: %w", err)
	}
	defer rows.Close()

	var services []Service
	for rows.Next() {
		var service Service
		if err := rows.Scan(&service.ID, &service.Name, &service.SLOID, &service.CurrentStatus, &service.LastStatusChangeAt, &service.StatusAnalysis,
			&service.GroupID, &service.GroupName, &service.ParentServiceID, &service.Position); err != nil {
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

// UpdateWithMetadata is Update, plus - inside the same transaction - the
// SLO metadata and/or environment tag updates ServicesHandler.Update
// (service-edit SVCEDIT-01..05) previously ran as 2-3 separate
// pool-level calls (Update, then UpdateSLOMetadata, then UpdateEnvironment).
// That left a window where a failure between calls (e.g. the process
// crashing, or a transient DB error on the second/third statement) could
// rename a service without applying its SLO metadata or environment tag,
// or apply one but not the other - a partial update with no way to tell
// from outside that it happened. All writes now commit or roll back
// together.
//
// sloType/datadogServiceTag are both-or-neither, same convention
// UpdateSLOMetadata uses: the handler only passes them non-nil when the
// request explicitly included both fields, and either nil leaves SLO
// metadata untouched (not cleared). environment nil leaves the
// environment tag untouched; non-nil (including a pointer to "") sets or
// clears it, same convention UpdateEnvironment uses. Returns ErrNotFound
// if id doesn't exist or is already soft-deleted.
func (r *ServiceRepository) UpdateWithMetadata(ctx context.Context, id, name string, sloType, datadogServiceTag, environment *string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: failed to begin update transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, "UPDATE services SET name = $2 WHERE id = $1 AND deleted_at IS NULL", id, name)
	if err != nil {
		return fmt.Errorf("db: failed to update service: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	if sloType != nil && datadogServiceTag != nil {
		sloTag, err := tx.Exec(ctx,
			"UPDATE services SET slo_type = NULLIF($2, ''), datadog_service_tag = NULLIF($3, '') WHERE id = $1 AND deleted_at IS NULL",
			id, *sloType, *datadogServiceTag,
		)
		if err != nil {
			return fmt.Errorf("db: failed to update service SLO metadata: %w", err)
		}
		if sloTag.RowsAffected() == 0 {
			return ErrNotFound
		}
	}

	if environment != nil {
		envTag, err := tx.Exec(ctx,
			"UPDATE services SET environment = $2 WHERE id = $1 AND deleted_at IS NULL",
			id, *environment,
		)
		if err != nil {
			return fmt.Errorf("db: failed to update service environment: %w", err)
		}
		if envTag.RowsAffected() == 0 {
			return ErrNotFound
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: failed to commit update transaction: %w", err)
	}

	return nil
}

// UpdateSLOMetadata sets serviceID's slo_type/datadog_service_tag columns -
// metadata about an already-linked SLO (design.md's Data Models: no CHECK
// constraint tying these to monitor_mode, unlike slo_id/monitor_mode/poll_*
// which Update deliberately leaves out of scope). Passing "" for either
// clears it to NULL, mirroring SLOName's ""-means-absent convention.
// Returns ErrNotFound if no service matches serviceID, the same convention
// Update uses.
func (r *ServiceRepository) UpdateSLOMetadata(ctx context.Context, serviceID, sloType, datadogServiceTag string) error {
	tag, err := r.pool.Exec(ctx,
		"UPDATE services SET slo_type = NULLIF($2, ''), datadog_service_tag = NULLIF($3, '') WHERE id = $1 AND deleted_at IS NULL",
		serviceID, sloType, datadogServiceTag,
	)
	if err != nil {
		return fmt.Errorf("db: failed to update service SLO metadata: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateEnvironment sets serviceID's environment column (service-environment-
// tag SET-03) - visual-only, no other side effect. Passing "" clears the tag
// back to "no tag", the same ""-means-absent convention UpdateSLOMetadata
// uses for slo_type/datadog_service_tag; unlike that method, no NULLIF is
// needed since environment is NOT NULL DEFAULT ” (0047_service_environment),
// not a nullable column. The handler is responsible for rejecting a value
// outside the enum before calling this (422, AGENTS.md §4) - the
// 0047_service_environment CHECK constraint is the last-resort backstop, not
// the primary validation path. Returns ErrNotFound if no service matches
// serviceID, the same convention Update/UpdateSLOMetadata use.
func (r *ServiceRepository) UpdateEnvironment(ctx context.Context, serviceID, environment string) error {
	tag, err := r.pool.Exec(ctx,
		"UPDATE services SET environment = $2 WHERE id = $1 AND deleted_at IS NULL",
		serviceID, environment,
	)
	if err != nil {
		return fmt.Errorf("db: failed to update service environment: %w", err)
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
