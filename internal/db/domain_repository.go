package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrDuplicateHostname is returned when registering a domain whose
// hostname is already registered.
var ErrDuplicateHostname = errors.New("db: hostname already registered")

// Domain is a registered root domain a status page's subdomain can be
// published under.
type Domain struct {
	ID        string
	Hostname  string
	CreatedAt time.Time
}

// DomainRepository accesses the domains table.
type DomainRepository struct {
	pool *Pool
}

// NewDomainRepository builds a DomainRepository backed by pool.
func NewDomainRepository(pool *Pool) *DomainRepository {
	return &DomainRepository{pool: pool}
}

// Create inserts domain, filling in its generated ID and CreatedAt. It
// returns ErrDuplicateHostname if the hostname is already registered
// (spec.md edge case: rejecting a duplicate root domain).
func (r *DomainRepository) Create(ctx context.Context, domain *Domain) error {
	row := r.pool.QueryRow(ctx,
		"INSERT INTO domains (hostname) VALUES ($1) RETURNING id, created_at",
		domain.Hostname,
	)

	if err := row.Scan(&domain.ID, &domain.CreatedAt); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return ErrDuplicateHostname
		}
		return fmt.Errorf("db: failed to create domain: %w", err)
	}

	return nil
}
