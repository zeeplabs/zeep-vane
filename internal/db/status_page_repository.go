package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrDomainAlreadyAttached is returned by AttachDomain when the target
// status page already has a non-null domain_id.
var ErrDomainAlreadyAttached = errors.New("db: status page already has a domain attached")

// ErrInvalidDomain is returned by AttachDomain when the given domain_id
// does not reference an existing Domain.
var ErrInvalidDomain = errors.New("db: domain_id does not reference an existing domain")

// ErrDuplicateDomainSubdomain is returned by AttachDomain when the given
// (domain_id, subdomain) pair is already used by another status page.
var ErrDuplicateDomainSubdomain = errors.New("db: this domain/subdomain pair is already in use")

// ErrNoDomainAttached is returned by PublicHostnameByID when the target
// status page has no domain_id/subdomain set yet.
var ErrNoDomainAttached = errors.New("db: status page has no domain attached")

// pendingTLSState is the StatusPage.State value AttachDomain moves a page
// to (was left at "draft" - a bug: tls.HostPolicy rejects ACME issuance
// for any hostname whose state is still "draft", but only MarkPublished/
// MarkTLSFailed - both reachable only via a HostPolicy-approved ACME
// attempt - ever moved it off "draft". AttachDomain never changed state,
// so every status page was permanently stuck: HostPolicy always found
// "draft" and always refused, so ACME was never attempted, so the state
// could never change. See migration 0020 for the one-time data fix on
// existing rows already stuck this way.
const pendingTLSState = "pending_tls"

// StatusPage is a public status page published at Subdomain under Domain,
// showing the linked services' current status. Subdomain and DomainID are
// both nullable: a status page can be created with no domain attached
// (SPD-01) and gets both set together, exactly once, by AttachDomain.
type StatusPage struct {
	ID           string
	Name         string
	Subdomain    *string
	DomainID     *string
	State        string
	TLSLastError *string
	CreatedAt    time.Time
	ServiceIDs   []string
}

// StatusPageRepository accesses the status_pages and status_page_services
// tables.
type StatusPageRepository struct {
	pool *Pool
}

// NewStatusPageRepository builds a StatusPageRepository backed by pool.
func NewStatusPageRepository(pool *Pool) *StatusPageRepository {
	return &StatusPageRepository{pool: pool}
}

// Create inserts statusPage and links it to every service in serviceIDs,
// filling in statusPage's generated ID, State ("draft" - the DB default -
// SP-15), and CreatedAt. The insert and the service links are wrapped in a
// single transaction: a status page is never left without its intended
// service links because a later insert failed partway through.
func (r *StatusPageRepository) Create(ctx context.Context, statusPage *StatusPage, serviceIDs []string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: failed to begin status page create transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row := tx.QueryRow(ctx,
		"INSERT INTO status_pages (name, subdomain, domain_id) VALUES ($1, $2, $3) RETURNING id, state, created_at",
		statusPage.Name, statusPage.Subdomain, statusPage.DomainID,
	)
	if err := row.Scan(&statusPage.ID, &statusPage.State, &statusPage.CreatedAt); err != nil {
		return fmt.Errorf("db: failed to create status page: %w", err)
	}

	for _, serviceID := range serviceIDs {
		if _, err := tx.Exec(ctx,
			"INSERT INTO status_page_services (status_page_id, service_id) VALUES ($1, $2)",
			statusPage.ID, serviceID,
		); err != nil {
			return fmt.Errorf("db: failed to link service %s to status page: %w", serviceID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: failed to commit status page create transaction: %w", err)
	}

	statusPage.ServiceIDs = serviceIDs
	return nil
}

// SetServices replaces the full set of services linked to the status page
// identified by id with serviceIDs, inside a single transaction (delete
// all existing links, then insert the given set) - the same replace-all
// semantics as Create's initial linking, so a partial update never leaves
// a mix of old and new links. It returns ErrNotFound if no status page
// matches id.
func (r *StatusPageRepository) SetServices(ctx context.Context, id string, serviceIDs []string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: failed to begin set services transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var exists bool
	row := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM status_pages WHERE id = $1)", id)
	if err := row.Scan(&exists); err != nil {
		return fmt.Errorf("db: failed to check status page existence: %w", err)
	}
	if !exists {
		return ErrNotFound
	}

	if _, err := tx.Exec(ctx, "DELETE FROM status_page_services WHERE status_page_id = $1", id); err != nil {
		return fmt.Errorf("db: failed to clear existing service links: %w", err)
	}
	for _, serviceID := range serviceIDs {
		if _, err := tx.Exec(ctx,
			"INSERT INTO status_page_services (status_page_id, service_id) VALUES ($1, $2)",
			id, serviceID,
		); err != nil {
			return fmt.Errorf("db: failed to link service %s to status page: %w", serviceID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: failed to commit set services transaction: %w", err)
	}

	return nil
}

// serviceIDsByStatusPage batch-loads every status_page_services link for
// pageIDs into a map, so List can attach ServiceIDs to each row with one
// extra query instead of one per page.
func (r *StatusPageRepository) serviceIDsByStatusPage(ctx context.Context, pageIDs []string) (map[string][]string, error) {
	result := make(map[string][]string, len(pageIDs))
	if len(pageIDs) == 0 {
		return result, nil
	}

	rows, err := r.pool.Query(ctx,
		"SELECT status_page_id, service_id FROM status_page_services WHERE status_page_id = ANY($1)", pageIDs)
	if err != nil {
		return nil, fmt.Errorf("db: failed to list service links: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var statusPageID, serviceID string
		if err := rows.Scan(&statusPageID, &serviceID); err != nil {
			return nil, fmt.Errorf("db: failed to scan service link: %w", err)
		}
		result[statusPageID] = append(result[statusPageID], serviceID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: failed to list service links: %w", err)
	}

	return result, nil
}

// AttachDomain sets domain_id/subdomain on the status page identified by
// id, exactly once (SPD-06). It locks the target row with SELECT ... FOR
// UPDATE inside an explicit transaction, rather than a conditional UPDATE
// ... WHERE domain_id IS NULL, because the row lock lets a single query
// resolve which of 4 distinguishable outcomes applies before deciding what
// to do - a conditional UPDATE affecting 0 rows can't tell "page doesn't
// exist" apart from "page already has a domain" (design.md Tech
// Decisions).
//
// It returns ErrNotFound if no status page matches id, ErrDomainAlreadyAttached
// if the page's domain_id is already non-null (SPD-07), ErrInvalidDomain if
// domainID does not reference an existing Domain (SPD-07), or
// ErrDuplicateDomainSubdomain if the (domainID, subdomain) pair is already
// used by another status page (SPD-09, enforced by the partial unique index
// added in migration 0013). On any error the row is left unmodified.
func (r *StatusPageRepository) AttachDomain(ctx context.Context, id, domainID, subdomain string) (*StatusPage, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("db: failed to begin attach domain transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var currentDomainID *string
	row := tx.QueryRow(ctx, "SELECT domain_id FROM status_pages WHERE id = $1 FOR UPDATE", id)
	if err := row.Scan(&currentDomainID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to lock status page for domain attach: %w", err)
	}
	if currentDomainID != nil {
		return nil, ErrDomainAlreadyAttached
	}

	var sp StatusPage
	sp.ID = id
	row = tx.QueryRow(ctx,
		"UPDATE status_pages SET domain_id = $1, subdomain = $2, state = $3 WHERE id = $4 "+
			"RETURNING name, subdomain, domain_id, state, tls_last_error, created_at",
		domainID, subdomain, pendingTLSState, id,
	)
	if err := row.Scan(&sp.Name, &sp.Subdomain, &sp.DomainID, &sp.State, &sp.TLSLastError, &sp.CreatedAt); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case pgerrcode.ForeignKeyViolation:
				return nil, ErrInvalidDomain
			case pgerrcode.UniqueViolation:
				return nil, ErrDuplicateDomainSubdomain
			}
		}
		return nil, fmt.Errorf("db: failed to attach domain to status page: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("db: failed to commit attach domain transaction: %w", err)
	}

	return &sp, nil
}

// ListPaginated returns one page of registered status pages, ordered by
// name, each with its linked service_ids (PAG-08). total is computed via
// COUNT(*) OVER() in the same query, with a zero-row fallback COUNT(*),
// same pattern as Incident/Domain/ServiceRepository's ListPaginated. The
// serviceIDsByStatusPage batch lookup now runs against only this page's
// IDs instead of every status page in the table.
func (r *StatusPageRepository) ListPaginated(ctx context.Context, page, pageSize int) ([]StatusPage, int, error) {
	offset := (page - 1) * pageSize

	rows, err := r.pool.Query(ctx,
		`SELECT id, name, subdomain, domain_id, state, tls_last_error, created_at, COUNT(*) OVER() AS total
		 FROM status_pages
		 ORDER BY name
		 LIMIT $1 OFFSET $2`,
		pageSize, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("db: failed to list status pages: %w", err)
	}
	defer rows.Close()

	statusPages := []StatusPage{}
	total := 0
	for rows.Next() {
		var sp StatusPage
		if err := rows.Scan(&sp.ID, &sp.Name, &sp.Subdomain, &sp.DomainID, &sp.State, &sp.TLSLastError, &sp.CreatedAt, &total); err != nil {
			return nil, 0, fmt.Errorf("db: failed to scan status page: %w", err)
		}
		statusPages = append(statusPages, sp)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("db: failed to iterate status pages: %w", err)
	}

	if len(statusPages) == 0 {
		total, err = r.countStatusPages(ctx)
		if err != nil {
			return nil, 0, err
		}
	}

	pageIDs := make([]string, len(statusPages))
	for i, sp := range statusPages {
		pageIDs[i] = sp.ID
	}
	serviceIDsByPage, err := r.serviceIDsByStatusPage(ctx, pageIDs)
	if err != nil {
		return nil, 0, err
	}
	for i := range statusPages {
		statusPages[i].ServiceIDs = serviceIDsByPage[statusPages[i].ID]
	}

	return statusPages, total, nil
}

// Delete removes the status page identified by id, along with its
// status_page_services links (deleted first, in the same transaction,
// since that join table has no ON DELETE CASCADE). It returns ErrNotFound
// if no status page matches id.
func (r *StatusPageRepository) Delete(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: failed to begin delete status page transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "DELETE FROM status_page_services WHERE status_page_id = $1", id); err != nil {
		return fmt.Errorf("db: failed to clear service links before delete: %w", err)
	}

	tag, err := tx.Exec(ctx, "DELETE FROM status_pages WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("db: failed to delete status page: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: failed to commit delete status page transaction: %w", err)
	}

	return nil
}

// PublicHostnameByID returns the full public hostname (subdomain + root
// domain, same shape hostnameMatch/StateByHostname join on) for the status
// page identified by id - used by the admin-facing domain verification
// endpoint to know what hostname to check DNS/TLS for. It returns
// ErrNotFound if no status page matches id, or ErrNoDomainAttached if the
// page exists but has no domain_id/subdomain set yet.
func (r *StatusPageRepository) PublicHostnameByID(ctx context.Context, id string) (string, error) {
	row := r.pool.QueryRow(ctx,
		"SELECT sp.subdomain || '.' || d.hostname FROM status_pages sp "+
			"LEFT JOIN domains d ON d.id = sp.domain_id WHERE sp.id = $1",
		id,
	)

	var hostname *string
	if err := row.Scan(&hostname); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("db: failed to get public hostname by id: %w", err)
	}
	if hostname == nil {
		return "", ErrNoDomainAttached
	}

	return *hostname, nil
}

// countStatusPages is the zero-row fallback for ListPaginated's total
// (PAG-08).
func (r *StatusPageRepository) countStatusPages(ctx context.Context) (int, error) {
	var total int
	row := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM status_pages")
	if err := row.Scan(&total); err != nil {
		return 0, fmt.Errorf("db: failed to count status pages: %w", err)
	}
	return total, nil
}

// GetByID returns the full StatusPage row with id. It returns ErrNotFound
// if no status page matches.
func (r *StatusPageRepository) GetByID(ctx context.Context, id string) (*StatusPage, error) {
	row := r.pool.QueryRow(ctx,
		"SELECT id, name, subdomain, domain_id, state, tls_last_error, created_at FROM status_pages WHERE id = $1",
		id,
	)

	var sp StatusPage
	if err := row.Scan(&sp.ID, &sp.Name, &sp.Subdomain, &sp.DomainID, &sp.State, &sp.TLSLastError, &sp.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to get status page by id: %w", err)
	}

	serviceIDsByPage, err := r.serviceIDsByStatusPage(ctx, []string{sp.ID})
	if err != nil {
		return nil, err
	}
	sp.ServiceIDs = serviceIDsByPage[sp.ID]

	return &sp, nil
}

// hostnameMatch is the shared WHERE-clause fragment matching a status
// page's public hostname: its subdomain joined to its domain's root
// hostname (e.g. "status" + "empresa.com" = "status.empresa.com").
const hostnameMatch = "d.id = sp.domain_id AND sp.subdomain || '.' || d.hostname = $1"

// StateByHostname returns the State of the status page published at
// hostname, joining status_pages to its parent domain. It returns
// ErrNotFound if no status page matches - CertMagic's HostPolicy (T30)
// treats that the same as any other lookup failure: reject the ACME
// request.
func (r *StatusPageRepository) StateByHostname(ctx context.Context, hostname string) (string, error) {
	row := r.pool.QueryRow(ctx,
		"SELECT sp.state FROM status_pages sp JOIN domains d ON "+hostnameMatch,
		hostname,
	)

	var state string
	if err := row.Scan(&state); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("db: failed to get status page state by hostname: %w", err)
	}

	return state, nil
}

// GetByHostname returns the full StatusPage row published at hostname,
// joining status_pages to its parent domain. It returns ErrNotFound if no
// status page matches. Unlike StateByHostname (used only by tls.HostPolicy,
// which needs just the state to gate ACME issuance), this returns the
// StatusPage's ID too, so callers like router.HostRouter can thread it down
// to the scoped public queries that implement SP-15 (a status page shows
// only its own linked services/incidents).
func (r *StatusPageRepository) GetByHostname(ctx context.Context, hostname string) (*StatusPage, error) {
	row := r.pool.QueryRow(ctx,
		"SELECT sp.id, sp.name, sp.subdomain, sp.domain_id, sp.state, sp.tls_last_error, sp.created_at "+
			"FROM status_pages sp JOIN domains d ON "+hostnameMatch,
		hostname,
	)

	var sp StatusPage
	if err := row.Scan(&sp.ID, &sp.Name, &sp.Subdomain, &sp.DomainID, &sp.State, &sp.TLSLastError, &sp.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to get status page by hostname: %w", err)
	}

	return &sp, nil
}

// MarkPublished sets the status page at hostname to "published" and clears
// any prior tls_last_error (SP-12: a successful certificate issuance
// publishes the page). It returns ErrNotFound if no status page matches.
func (r *StatusPageRepository) MarkPublished(ctx context.Context, hostname string) error {
	tag, err := r.pool.Exec(ctx,
		"UPDATE status_pages sp SET state = 'published', tls_last_error = NULL FROM domains d WHERE "+hostnameMatch,
		hostname,
	)
	if err != nil {
		return fmt.Errorf("db: failed to mark status page published: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// MarkTLSFailed sets the status page at hostname to "tls_failed" and
// records reason (SP-13: a failed certificate issuance keeps the page
// unpublished and surfaces why to the admin). It returns ErrNotFound if no
// status page matches.
func (r *StatusPageRepository) MarkTLSFailed(ctx context.Context, hostname, reason string) error {
	tag, err := r.pool.Exec(ctx,
		"UPDATE status_pages sp SET state = 'tls_failed', tls_last_error = $2 FROM domains d WHERE "+hostnameMatch,
		hostname, reason,
	)
	if err != nil {
		return fmt.Errorf("db: failed to mark status page tls_failed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}
