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

// ErrDuplicateHostname is returned when registering a domain whose
// hostname is already registered.
var ErrDuplicateHostname = errors.New("db: hostname already registered")

// ErrDomainInUse is returned by Delete when the domain is still referenced
// by a status page's domain_id (no ON DELETE CASCADE on that FK - deleting
// a domain out from under a published status page would silently break its
// public URL, so the operator must detach the domain from the status page
// first).
var ErrDomainInUse = errors.New("db: domain is still attached to a status page")

// Domain is a registered root domain a status page's subdomain can be
// published under.
type Domain struct {
	ID        string
	Hostname  string
	CreatedAt time.Time
	// DomainType is currently always "custom" (domain-verification-state
	// DOMVER-02) - the "subdomain" (Vane-hosted, zero-CNAME) type is out of
	// scope until a shared base-domain/wildcard-TLS story exists.
	DomainType string
	// Status is "pending"/"verified"/"error" (DOMVER-01), set by Create's
	// DB default and mutated only by SetVerificationResult.
	Status string
	// SSLStatus is "pending"/"active"/"error" (DOMVER-01).
	SSLStatus string
	// VerifiedAt is nil until the domain's first successful/failed
	// verification attempt (DOMVER-02).
	VerifiedAt *time.Time
	// LastError describes the most recent verification failure, or nil if
	// the last attempt (if any) succeeded, or none has run yet.
	LastError *string
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
		`INSERT INTO domains (hostname) VALUES ($1)
		 RETURNING id, created_at, domain_type, status, ssl_status, verified_at, last_error`,
		domain.Hostname,
	)

	if err := row.Scan(&domain.ID, &domain.CreatedAt, &domain.DomainType, &domain.Status,
		&domain.SSLStatus, &domain.VerifiedAt, &domain.LastError); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return ErrDuplicateHostname
		}
		return fmt.Errorf("db: failed to create domain: %w", err)
	}

	return nil
}

// ListPaginated returns one page of registered domains, ordered by
// hostname (PAG-08). total is the total number of domains in the table,
// computed via COUNT(*) OVER() in the same query; when the requested page
// is beyond the last page (or the table is empty) the primary query
// returns zero rows and can't carry a window-function total, so a fallback
// plain COUNT(*) runs only in that case (same pattern as
// IncidentRepository.ListPaginated).
func (r *DomainRepository) ListPaginated(ctx context.Context, page, pageSize int) ([]Domain, int, error) {
	offset := (page - 1) * pageSize

	rows, err := r.pool.Query(ctx,
		`SELECT id, hostname, created_at, domain_type, status, ssl_status, verified_at, last_error, COUNT(*) OVER() AS total
		 FROM domains
		 ORDER BY hostname
		 LIMIT $1 OFFSET $2`,
		pageSize, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("db: failed to list domains: %w", err)
	}
	defer rows.Close()

	domains := []Domain{}
	total := 0
	for rows.Next() {
		var domain Domain
		if err := rows.Scan(&domain.ID, &domain.Hostname, &domain.CreatedAt, &domain.DomainType,
			&domain.Status, &domain.SSLStatus, &domain.VerifiedAt, &domain.LastError, &total); err != nil {
			return nil, 0, fmt.Errorf("db: failed to scan domain: %w", err)
		}
		domains = append(domains, domain)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("db: failed to iterate domains: %w", err)
	}

	if len(domains) == 0 {
		total, err = r.countDomains(ctx)
		if err != nil {
			return nil, 0, err
		}
	}

	return domains, total, nil
}

// GetByID returns the domain identified by id, or ErrNotFound if none
// matches.
func (r *DomainRepository) GetByID(ctx context.Context, id string) (*Domain, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, hostname, created_at, domain_type, status, ssl_status, verified_at, last_error
		 FROM domains WHERE id = $1`,
		id,
	)

	var domain Domain
	if err := row.Scan(&domain.ID, &domain.Hostname, &domain.CreatedAt, &domain.DomainType,
		&domain.Status, &domain.SSLStatus, &domain.VerifiedAt, &domain.LastError); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to get domain: %w", err)
	}

	return &domain, nil
}

// SetVerificationResult persists the outcome of a real DNS/TLS check
// (domain-verification-state DOMVER-04/07/08): status, sslStatus, and
// lastError (nil on full success) are set, and verifiedAt is stamped to the
// check's timestamp. Returns ErrNotFound if id doesn't exist.
func (r *DomainRepository) SetVerificationResult(ctx context.Context, id, status, sslStatus string, lastError *string, verifiedAt time.Time) (*Domain, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE domains
		 SET status = $2, ssl_status = $3, last_error = $4, verified_at = $5
		 WHERE id = $1
		 RETURNING id, hostname, created_at, domain_type, status, ssl_status, verified_at, last_error`,
		id, status, sslStatus, lastError, verifiedAt,
	)

	var domain Domain
	if err := row.Scan(&domain.ID, &domain.Hostname, &domain.CreatedAt, &domain.DomainType,
		&domain.Status, &domain.SSLStatus, &domain.VerifiedAt, &domain.LastError); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to set domain verification result: %w", err)
	}

	return &domain, nil
}

// Delete removes the domain identified by id. It returns ErrNotFound if no
// domain matches id, or ErrDomainInUse if a status page still references it
// via domain_id (the FK has no ON DELETE CASCADE by design).
func (r *DomainRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, "DELETE FROM domains WHERE id = $1", id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.ForeignKeyViolation {
			return ErrDomainInUse
		}
		return fmt.Errorf("db: failed to delete domain: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// countDomains is the zero-row fallback for ListPaginated's total (PAG-08).
func (r *DomainRepository) countDomains(ctx context.Context) (int, error) {
	var total int
	row := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM domains")
	if err := row.Scan(&total); err != nil {
		return 0, fmt.Errorf("db: failed to count domains: %w", err)
	}
	return total, nil
}
