package db

import (
	"context"
	"fmt"
	"time"
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
