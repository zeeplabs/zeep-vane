package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// StatusPage is a public status page published at Subdomain under Domain,
// showing the linked services' current status.
type StatusPage struct {
	ID           string
	Name         string
	Subdomain    string
	DomainID     string
	State        string
	TLSLastError *string
	CreatedAt    time.Time
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

	return nil
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
