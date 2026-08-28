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
	ServiceIDs []string // populated by List/Create; nil for callers that don't need it (Transition, ListPublic*)
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

// ListPaginated returns one page of incidents, most recently created first,
// each with its linked service_ids (I16 - the admin incidents list badges
// each incident with the services it affects). total is the total number of
// incidents in the table, computed via COUNT(*) OVER() in the same query;
// when the requested page is beyond the last page (or the table is empty)
// the primary query returns zero rows and can't carry a window-function
// total, so a fallback plain COUNT(*) runs only in that case (PAG-04, PAG-06).
func (r *IncidentRepository) ListPaginated(ctx context.Context, page, pageSize int) ([]Incident, int, error) {
	offset := (page - 1) * pageSize

	rows, err := r.pool.Query(ctx,
		`SELECT id, title, status, created_at, resolved_at, COUNT(*) OVER() AS total
		 FROM incidents
		 ORDER BY created_at DESC
		 LIMIT $1 OFFSET $2`,
		pageSize, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("db: failed to list incidents: %w", err)
	}
	defer rows.Close()

	incidents := []Incident{}
	total := 0
	for rows.Next() {
		var incident Incident
		if err := rows.Scan(&incident.ID, &incident.Title, &incident.Status, &incident.CreatedAt, &incident.ResolvedAt, &total); err != nil {
			return nil, 0, fmt.Errorf("db: failed to scan incident: %w", err)
		}
		incidents = append(incidents, incident)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("db: failed to iterate incidents: %w", err)
	}

	if len(incidents) == 0 {
		total, err = r.countIncidents(ctx)
		if err != nil {
			return nil, 0, err
		}
	}

	for i := range incidents {
		serviceIDs, err := r.listServiceIDs(ctx, incidents[i].ID)
		if err != nil {
			return nil, 0, err
		}
		incidents[i].ServiceIDs = serviceIDs
	}

	return incidents, total, nil
}

// countIncidents is the zero-row fallback for ListPaginated's total, used
// when the primary query's COUNT(*) OVER() has no row to carry it (PAG-04).
func (r *IncidentRepository) countIncidents(ctx context.Context) (int, error) {
	var total int
	row := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM incidents")
	if err := row.Scan(&total); err != nil {
		return 0, fmt.Errorf("db: failed to count incidents: %w", err)
	}
	return total, nil
}

// listServiceIDs returns the service IDs linked to incidentID via
// incident_services.
func (r *IncidentRepository) listServiceIDs(ctx context.Context, incidentID string) ([]string, error) {
	rows, err := r.pool.Query(ctx, "SELECT service_id FROM incident_services WHERE incident_id = $1", incidentID)
	if err != nil {
		return nil, fmt.Errorf("db: failed to list incident service links: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("db: failed to scan incident service link: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: failed to iterate incident service links: %w", err)
	}
	return ids, nil
}

// SPEC_DEVIATION: design.md assumed ListUpdates(ctx, id) had a single caller
// (the handler) and would be fully replaced by ListUpdatesPaginated. It also
// has an internal caller: withTimelinesSplit, used by ListPublic/
// ListPublicForStatusPage to build each incident's full public timeline.
// That caller must keep seeing the complete, unpaginated timeline (the
// public page's per-incident update list is out of scope for this feature -
// see spec.md's Out of Scope table), so ListUpdates is kept unchanged and
// ListUpdatesPaginated is added alongside it for the two handler call sites,
// mirroring the ServiceRepository/poller.go precedent already established
// in design.md for exactly this "internal caller must stay unpaginated"
// situation.

// ListUpdates returns incidentID's full timeline, most recent update first
// (spec.md: "ordenado do mais recente para o mais antigo"). Returns
// ErrNotFound if incidentID doesn't exist. Used internally by
// withTimelinesSplit for the public status page's per-incident timeline,
// which is not paginated by this feature.
func (r *IncidentRepository) ListUpdates(ctx context.Context, incidentID string) ([]IncidentUpdate, error) {
	if err := r.mustExist(ctx, incidentID); err != nil {
		return nil, err
	}

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

// ListUpdatesPaginated returns one page of incidentID's timeline, most
// recent update first, scoped by incident_id (PAG-05). Returns ErrNotFound
// if incidentID doesn't exist. total is computed via COUNT(*) OVER() in the
// same query, with a zero-row fallback COUNT(*) matching ListPaginated's
// pattern (PAG-06).
func (r *IncidentRepository) ListUpdatesPaginated(ctx context.Context, incidentID string, page, pageSize int) ([]IncidentUpdate, int, error) {
	if err := r.mustExist(ctx, incidentID); err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize

	rows, err := r.pool.Query(ctx,
		`SELECT id, incident_id, body, created_at, COUNT(*) OVER() AS total
		 FROM incident_updates
		 WHERE incident_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2 OFFSET $3`,
		incidentID, pageSize, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("db: failed to list incident updates: %w", err)
	}
	defer rows.Close()

	updates := []IncidentUpdate{}
	total := 0
	for rows.Next() {
		var update IncidentUpdate
		if err := rows.Scan(&update.ID, &update.IncidentID, &update.Body, &update.CreatedAt, &total); err != nil {
			return nil, 0, fmt.Errorf("db: failed to scan incident update: %w", err)
		}
		updates = append(updates, update)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("db: failed to iterate incident updates: %w", err)
	}

	if len(updates) == 0 {
		total, err = r.countIncidentUpdates(ctx, incidentID)
		if err != nil {
			return nil, 0, err
		}
	}

	return updates, total, nil
}

// countIncidentUpdates is the zero-row fallback for ListUpdatesPaginated's
// total (PAG-06).
func (r *IncidentRepository) countIncidentUpdates(ctx context.Context, incidentID string) (int, error) {
	var total int
	row := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM incident_updates WHERE incident_id = $1", incidentID)
	if err := row.Scan(&total); err != nil {
		return 0, fmt.Errorf("db: failed to count incident updates: %w", err)
	}
	return total, nil
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

// IncidentPublic is an Incident with its timeline, shaped for the public
// status page (SP-18).
type IncidentPublic struct {
	Incident
	Updates []IncidentUpdate
}

// ListPublic returns incidents for the public status page (SP-18): active
// splits out every incident whose status isn't "resolved" (shown in
// destaque at the top), resolved splits out incidents resolved within the
// last retentionDays (spec.md's 90-day retention assumption) - a resolved
// incident older than that is omitted entirely. Both are ordered most
// recently created first, each with its timeline ordered most-recent-update
// first.
func (r *IncidentRepository) ListPublic(ctx context.Context, retentionDays int) (active, resolved []IncidentPublic, err error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, title, status, created_at, resolved_at FROM incidents
		 WHERE status <> 'resolved' OR resolved_at > now() - make_interval(days => $1)
		 ORDER BY created_at DESC`,
		retentionDays,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("db: failed to list public incidents: %w", err)
	}
	defer rows.Close()

	incidents, err := scanIncidentRows(rows)
	if err != nil {
		return nil, nil, err
	}

	return r.withTimelinesSplit(ctx, incidents)
}

// ListPublicForStatusPage is the scoped counterpart to ListPublic (SP-15):
// it returns only incidents linked, via incident_services, to a service
// that itself belongs to statusPageID (via status_page_services) - a status
// page's public page must never surface an incident for a service it
// doesn't publish. Active/resolved splitting and the retention window
// behave exactly as in ListPublic.
func (r *IncidentRepository) ListPublicForStatusPage(ctx context.Context, statusPageID string, retentionDays int) (active, resolved []IncidentPublic, err error) {
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT i.id, i.title, i.status, i.created_at, i.resolved_at
		 FROM incidents i
		 JOIN incident_services isv ON isv.incident_id = i.id
		 JOIN status_page_services sps ON sps.service_id = isv.service_id
		 WHERE sps.status_page_id = $1
		   AND (i.status <> 'resolved' OR i.resolved_at > now() - make_interval(days => $2))
		 ORDER BY i.created_at DESC`,
		statusPageID, retentionDays,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("db: failed to list public incidents for status page: %w", err)
	}
	defer rows.Close()

	incidents, err := scanIncidentRows(rows)
	if err != nil {
		return nil, nil, err
	}

	return r.withTimelinesSplit(ctx, incidents)
}

// scanIncidentRows scans the (id, title, status, created_at, resolved_at)
// shape shared by ListPublic and ListPublicForStatusPage.
func scanIncidentRows(rows pgx.Rows) ([]Incident, error) {
	var incidents []Incident
	for rows.Next() {
		var incident Incident
		if err := rows.Scan(&incident.ID, &incident.Title, &incident.Status, &incident.CreatedAt, &incident.ResolvedAt); err != nil {
			return nil, fmt.Errorf("db: failed to scan public incident: %w", err)
		}
		incidents = append(incidents, incident)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: failed to iterate public incidents: %w", err)
	}
	return incidents, nil
}

// withTimelinesSplit loads each incident's timeline and splits the result
// into active (status <> "resolved", shown in destaque) and resolved
// (shown in history within the retention window) groups, per SP-18.
func (r *IncidentRepository) withTimelinesSplit(ctx context.Context, incidents []Incident) (active, resolved []IncidentPublic, err error) {
	for _, incident := range incidents {
		updates, err := r.ListUpdates(ctx, incident.ID)
		if err != nil {
			return nil, nil, err
		}

		withUpdates := IncidentPublic{Incident: incident, Updates: updates}
		if incident.Status == "resolved" {
			resolved = append(resolved, withUpdates)
		} else {
			active = append(active, withUpdates)
		}
	}

	return active, resolved, nil
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
