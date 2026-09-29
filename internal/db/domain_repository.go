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

	// Health-check state (domain-health-monitoring DHM-01/05/06/07),
	// written by SetHealthCheckResult and read by the admin API/scheduler.
	// ExpiresAt/Registrar come from the latest successful RDAP lookup;
	// ExpectedNS is the baseline learned on the domain's first successful NS
	// check; CurrentNS is the latest resolved set; NSDriftDetected is true
	// when CurrentNS differs from ExpectedNS; LastRDAPCheckAt stamps the most
	// recent health-check cycle attempt (success or failure);
	// LastRDAPSuccessAt stamps only the most recent cycle that got real RDAP
	// data - the scheduler compares expiration bands against this, not
	// LastRDAPCheckAt, so a failed lookup can never mask a threshold crossing
	// on the next successful one. RDAPLastError records the last RDAP failure
	// (nil on success or when none has run yet).
	ExpiresAt         *time.Time
	Registrar         *string
	ExpectedNS        []string
	CurrentNS         []string
	NSDriftDetected   bool
	LastRDAPCheckAt   *time.Time
	LastRDAPSuccessAt *time.Time
	RDAPLastError     *string
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
		`SELECT id, hostname, created_at, domain_type, status, ssl_status, verified_at, last_error,
		        expires_at, registrar, expected_ns, current_ns, ns_drift_detected, last_rdap_check_at, last_rdap_success_at, rdap_last_error,
		        COUNT(*) OVER() AS total
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
			&domain.Status, &domain.SSLStatus, &domain.VerifiedAt, &domain.LastError,
			&domain.ExpiresAt, &domain.Registrar, &domain.ExpectedNS, &domain.CurrentNS,
			&domain.NSDriftDetected, &domain.LastRDAPCheckAt, &domain.LastRDAPSuccessAt, &domain.RDAPLastError, &total); err != nil {
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
		`SELECT id, hostname, created_at, domain_type, status, ssl_status, verified_at, last_error,
		        expires_at, registrar, expected_ns, current_ns, ns_drift_detected, last_rdap_check_at, last_rdap_success_at, rdap_last_error
		 FROM domains WHERE id = $1`,
		id,
	)

	var domain Domain
	if err := row.Scan(&domain.ID, &domain.Hostname, &domain.CreatedAt, &domain.DomainType,
		&domain.Status, &domain.SSLStatus, &domain.VerifiedAt, &domain.LastError,
		&domain.ExpiresAt, &domain.Registrar, &domain.ExpectedNS, &domain.CurrentNS,
		&domain.NSDriftDetected, &domain.LastRDAPCheckAt, &domain.LastRDAPSuccessAt, &domain.RDAPLastError); err != nil {
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

// SetHealthCheckResult persists one domain health-check cycle's outcome for
// the domain identified by id (domain-health-monitoring DHM-01/05/06/07):
// RDAP expiration/registrar/error and the resolved NS set. ExpectedNS (the
// baseline) is learned from currentNS on the domain's first successful NS
// check and is never overwritten afterwards; a nil currentNS (NS lookup
// failure) leaves both CurrentNS and ExpectedNS untouched, so a transient
// DNS failure can never erase the learned baseline. LastRDAPCheckAt is
// stamped to the database clock. It returns ErrNotFound if id doesn't exist.
//
// Like IncidentRepository.Create/StatusPageRepository.Create, this reuses a
// tenant transaction already present on ctx (the scheduler path) and opens
// one only as a fallback - never a bare pool.Begin that would discard the
// caller's RLS session settings.
func (r *DomainRepository) SetHealthCheckResult(ctx context.Context, id string, expiresAt *time.Time, registrar *string, currentNS []string, driftDetected bool, rdapErr *string) error {
	if _, ok := TenantTxFromContext(ctx); ok {
		return r.setHealthCheckResult(ctx, id, expiresAt, registrar, currentNS, driftDetected, rdapErr)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: failed to begin domain health check transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := r.setHealthCheckResult(WithTenantTx(ctx, tx), id, expiresAt, registrar, currentNS, driftDetected, rdapErr); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: failed to commit domain health check transaction: %w", err)
	}
	return nil
}

// setHealthCheckResult performs SetHealthCheckResult's write on whatever
// transaction ctx carries, mirroring IncidentRepository.insert.
//
// ns_drift_detected is only overwritten when currentNS is non-nil (a real NS
// lookup ran this cycle) - like current_ns/expected_ns, a transient DNS
// failure (currentNS nil) must leave the previously detected drift state
// untouched rather than silently clearing it. last_rdap_success_at is
// stamped to now() only when rdapErr is nil (a real successful RDAP lookup),
// distinct from last_rdap_check_at which stamps every attempt - the
// scheduler's threshold-crossing comparison needs the former, not the
// latter, or a failed lookup on the crossing day would hide the crossing.
func (r *DomainRepository) setHealthCheckResult(ctx context.Context, id string, expiresAt *time.Time, registrar *string, currentNS []string, driftDetected bool, rdapErr *string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE domains
		 SET expires_at = $2,
		     registrar = $3,
		     current_ns = COALESCE($4, current_ns),
		     expected_ns = COALESCE(expected_ns, $4),
		     ns_drift_detected = CASE WHEN $4 IS NULL THEN ns_drift_detected ELSE $5 END,
		     rdap_last_error = $6,
		     last_rdap_check_at = now(),
		     last_rdap_success_at = CASE WHEN $6::text IS NULL THEN now() ELSE last_rdap_success_at END
		 WHERE id = $1`,
		id, expiresAt, registrar, currentNS, driftDetected, rdapErr,
	)
	if err != nil {
		return fmt.Errorf("db: failed to set domain health check result: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
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

// CountVerified returns how many domains are currently verified for the
// active tenant (OVW-06) - status "verified", the value
// SetVerificationResult writes after a successful real DNS/TLS check. Like
// every other tenant-scoped read, it relies on app.tenant_id being set so
// RLS scopes the count to one tenant.
func (r *DomainRepository) CountVerified(ctx context.Context) (int, error) {
	var total int
	row := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM domains WHERE status = 'verified'")
	if err := row.Scan(&total); err != nil {
		return 0, fmt.Errorf("db: failed to count verified domains: %w", err)
	}
	return total, nil
}

// CountAll returns the total number of domains for the active tenant,
// verified or not - the denominator for the Overview page's "verified
// domains" card (e.g. "3/4").
func (r *DomainRepository) CountAll(ctx context.Context) (int, error) {
	return r.countDomains(ctx)
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
