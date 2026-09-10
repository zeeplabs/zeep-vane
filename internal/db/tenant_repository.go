package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Tenant is a single tenant (self-hosted's one auto-provisioned tenant, or
// one SaaS customer account) - multi-tenancy-core, AD-022.
type Tenant struct {
	ID              string
	Name            string
	Slug            *string
	Plan            string
	Status          string
	ContactEmail    string
	LogoData        []byte
	LogoContentType *string
	LegalName       *string
	TaxID           *string
	TaxIDType       *string // "cpf" | "cnpj", nil if unset
	BillingAddress  []byte  // raw JSON, nil if unset
	Locale          string
	PrimaryColor    *string
	SecondaryColor  *string
	CreatedAt       time.Time
}

// logoServedPath is the fixed URL a tenant's stored logo is served at.
// The pre-multi-tenancy design encoded the file extension into the path
// itself ("/uploads/logo.png"); the logo lives in the row as bytes plus a
// content type, so the path never changes - LogoServedURL reports whether
// it currently resolves to anything by checking LogoContentType, not by
// encoding format into the path.
const logoServedPath = "/uploads/logo"

// LogoServedURL returns the URL this tenant's logo is served at, or nil if
// no logo has ever been uploaded.
func (t *Tenant) LogoServedURL() *string {
	if t.LogoContentType == nil {
		return nil
	}
	url := logoServedPath
	return &url
}

// ErrInvalidTaxID is returned when TaxID's digit count doesn't match what
// TaxIDType requires (11 for cpf, 14 for cnpj) - checked in the
// application layer, not the database, per design.md's assumption that
// full CPF/CNPJ check-digit validation is a future billing concern, not
// this foundation's.
var ErrInvalidTaxID = errors.New("db: tax_id digit count does not match tax_id_type")

// TenantRepository accesses the tenants table.
type TenantRepository struct {
	pool *Pool
}

// NewTenantRepository builds a TenantRepository backed by pool.
func NewTenantRepository(pool *Pool) *TenantRepository {
	return &TenantRepository{pool: pool}
}

// Create inserts tenant, filling in its generated ID (if not already set)
// and CreatedAt. tenants is FORCE ROW LEVEL SECURITY (0024) with a policy
// keyed off its own id, so the insert's WITH CHECK requires app.tenant_id
// to already equal the row's id - a chicken-and-egg problem for the very
// first insert of a brand new tenant, since nothing can know that id in
// advance except by generating it first.
//
// If ctx already carries a tenant transaction (db.WithTenantTx - e.g. the
// bootstrap/signup flow orchestrating a tenant + its owner membership in
// one transaction), Create assumes the caller already generated tenant.ID
// and set app.tenant_id to match it, and simply inserts on that
// transaction. Otherwise Create is self-contained: it generates an id,
// opens its own transaction, sets app.tenant_id to it, inserts, and
// commits.
func (r *TenantRepository) Create(ctx context.Context, tenant *Tenant) error {
	if _, ok := TenantTxFromContext(ctx); ok {
		if tenant.ID == "" {
			return errors.New("db: tenant.ID must be set before Create when reusing an existing tenant transaction")
		}
		return r.insert(ctx, tenant)
	}

	var id string
	if err := r.pool.QueryRow(ctx, "SELECT gen_random_uuid()").Scan(&id); err != nil {
		return fmt.Errorf("db: failed to generate tenant id: %w", err)
	}
	tenant.ID = id

	tx, err := r.pool.BeginTenantTx(ctx, "", id)
	if err != nil {
		return fmt.Errorf("db: failed to begin tenant transaction: %w", err)
	}

	if err := r.insert(WithTenantTx(ctx, tx), tenant); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: failed to commit tenant creation: %w", err)
	}

	return nil
}

func (r *TenantRepository) insert(ctx context.Context, tenant *Tenant) error {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO tenants (id, name, contact_email)
		 VALUES ($1, $2, $3)
		 RETURNING plan, status, locale, created_at`,
		tenant.ID, tenant.Name, tenant.ContactEmail,
	)
	if err := row.Scan(&tenant.Plan, &tenant.Status, &tenant.Locale, &tenant.CreatedAt); err != nil {
		return fmt.Errorf("db: failed to create tenant: %w", err)
	}
	return nil
}

// Get returns the tenant with the given id, scoped by RLS - the caller's
// active tenant (app.tenant_id) must already equal id, or this returns
// ErrNotFound exactly as if the row didn't exist (fail-closed, TENANT-03).
func (r *TenantRepository) Get(ctx context.Context, tenantID string) (*Tenant, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, name, slug, plan, status, contact_email, logo_content_type,
		        legal_name, tax_id, tax_id_type, billing_address, locale,
		        primary_color, secondary_color, created_at
		 FROM tenants WHERE id = $1`,
		tenantID,
	)

	var tenant Tenant
	if err := row.Scan(
		&tenant.ID, &tenant.Name, &tenant.Slug, &tenant.Plan, &tenant.Status, &tenant.ContactEmail,
		&tenant.LogoContentType, &tenant.LegalName, &tenant.TaxID, &tenant.TaxIDType, &tenant.BillingAddress,
		&tenant.Locale, &tenant.PrimaryColor, &tenant.SecondaryColor, &tenant.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to get tenant: %w", err)
	}

	return &tenant, nil
}

// TenantUpdate carries the optional fields Update may change - a nil
// pointer means "leave this field unchanged", not "clear it".
type TenantUpdate struct {
	Name         *string
	ContactEmail *string
	LegalName    *string
	TaxID        *string
	TaxIDType    *string
}

// cpfDigitCount and cnpjDigitCount are the exact digit counts
// design.md's Assumptions require - basic length validation, not full
// check-digit validation (that's a future billing-feature concern).
const (
	cpfDigitCount  = 11
	cnpjDigitCount = 14
)

// validateTaxID checks taxID's digit count against taxIDType, returning
// ErrInvalidTaxID if they don't match. Either being nil is valid (both
// fields are optional) - only a mismatched *pair* is rejected.
func validateTaxID(taxID, taxIDType *string) error {
	if taxID == nil || taxIDType == nil {
		return nil
	}

	digits := 0
	for _, r := range *taxID {
		if r >= '0' && r <= '9' {
			digits++
		}
	}

	switch *taxIDType {
	case "cpf":
		if digits != cpfDigitCount {
			return ErrInvalidTaxID
		}
	case "cnpj":
		if digits != cnpjDigitCount {
			return ErrInvalidTaxID
		}
	default:
		return ErrInvalidTaxID
	}

	return nil
}

// Update persists the non-nil fields in u onto the tenant with the given
// id, leaving unset fields untouched, and returns the updated tenant.
// Rejects with ErrInvalidTaxID (no persistence at all) if TaxID/TaxIDType
// together don't pass validateTaxID - the tenant's *existing* tax_id_type
// is not consulted, only the pair being written in this same call, since a
// caller changing just one of the two is expected to send both together.
func (r *TenantRepository) Update(ctx context.Context, tenantID string, u TenantUpdate) (*Tenant, error) {
	if err := validateTaxID(u.TaxID, u.TaxIDType); err != nil {
		return nil, err
	}

	row := r.pool.QueryRow(ctx,
		`UPDATE tenants SET
		    name = COALESCE($2, name),
		    contact_email = COALESCE($3, contact_email),
		    legal_name = COALESCE($4, legal_name),
		    tax_id = COALESCE($5, tax_id),
		    tax_id_type = COALESCE($6, tax_id_type)
		 WHERE id = $1
		 RETURNING id, name, slug, plan, status, contact_email, logo_content_type,
		           legal_name, tax_id, tax_id_type, billing_address, locale,
		           primary_color, secondary_color, created_at`,
		tenantID, u.Name, u.ContactEmail, u.LegalName, u.TaxID, u.TaxIDType,
	)

	var tenant Tenant
	if err := row.Scan(
		&tenant.ID, &tenant.Name, &tenant.Slug, &tenant.Plan, &tenant.Status, &tenant.ContactEmail,
		&tenant.LogoContentType, &tenant.LegalName, &tenant.TaxID, &tenant.TaxIDType, &tenant.BillingAddress,
		&tenant.Locale, &tenant.PrimaryColor, &tenant.SecondaryColor, &tenant.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to update tenant: %w", err)
	}

	return &tenant, nil
}

// UpdateLogo persists the logo's bytes and content type on the tenant with
// the given id, independently of Update, and returns the updated tenant -
// same shape as the removed CompanySettingsRepository.UpdateLogo (T16
// merges the caller of this into TenantRepository; not wired to any
// handler yet in this batch).
func (r *TenantRepository) UpdateLogo(ctx context.Context, tenantID, contentType string, data []byte) (*Tenant, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE tenants SET logo_data = $2, logo_content_type = $3
		 WHERE id = $1
		 RETURNING id, name, slug, plan, status, contact_email, logo_content_type,
		           legal_name, tax_id, tax_id_type, billing_address, locale,
		           primary_color, secondary_color, created_at`,
		tenantID, data, contentType,
	)

	var tenant Tenant
	if err := row.Scan(
		&tenant.ID, &tenant.Name, &tenant.Slug, &tenant.Plan, &tenant.Status, &tenant.ContactEmail,
		&tenant.LogoContentType, &tenant.LegalName, &tenant.TaxID, &tenant.TaxIDType, &tenant.BillingAddress,
		&tenant.Locale, &tenant.PrimaryColor, &tenant.SecondaryColor, &tenant.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to update tenant logo: %w", err)
	}

	return &tenant, nil
}

// legacyDataTenantID is the placeholder tenant the 0024 migration assigns
// to any pre-existing domain row it finds (see that migration's backfill
// note). It is never a real tenant, so the no-active-tenant fallback below
// must never resolve to it.
const legacyDataTenantID = "00000000-0000-0000-0000-000000000000"

// activeTenantPredicate matches the session's active tenant
// (app.tenant_id), falling back to the installation's own single tenant
// when no tenant is active.
//
// The fallback exists for the routes that legitimately have no session:
// the public status page, the public logo file, and the login screen's
// branding, all of which read what used to be the company_settings
// singleton. Resolving a tenant for unauthenticated traffic is not solved
// by this feature (no task covers it; T15 covers only the poller), so
// these paths keep their pre-multi-tenancy semantics - "the one tenant
// this installation has" - which is exactly correct for self-hosted and
// is the same shape the dropped company_settings row had. It does not
// weaken RLS: under a non-superuser role the policy still fails closed and
// this returns nothing.
const activeTenantPredicate = `
	WHERE id = COALESCE(NULLIF(current_setting('app.tenant_id', true), '')::uuid, id)
	  AND id <> '` + legacyDataTenantID + `'
	ORDER BY created_at ASC
	LIMIT 1`

// Active returns the session's active tenant, or the installation's single
// tenant when no tenant is active - see activeTenantPredicate. It returns
// ErrNotFound when neither resolves.
func (r *TenantRepository) Active(ctx context.Context) (*Tenant, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, name, slug, plan, status, contact_email, logo_content_type,
		        legal_name, tax_id, tax_id_type, billing_address, locale,
		        primary_color, secondary_color, created_at
		 FROM tenants`+activeTenantPredicate)

	var tenant Tenant
	if err := row.Scan(
		&tenant.ID, &tenant.Name, &tenant.Slug, &tenant.Plan, &tenant.Status, &tenant.ContactEmail,
		&tenant.LogoContentType, &tenant.LegalName, &tenant.TaxID, &tenant.TaxIDType, &tenant.BillingAddress,
		&tenant.Locale, &tenant.PrimaryColor, &tenant.SecondaryColor, &tenant.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to get active tenant: %w", err)
	}

	return &tenant, nil
}

// List returns every active tenant, oldest first - the poller's own
// enumeration for tenant-by-tenant iteration (T15, TENANT-04), never a
// query gated behind a particular session's own app.tenant_id (there is no
// single active tenant when the caller's job is to iterate all of them).
//
// Known limitation, not silently papered over: tenants' RLS policy (0024)
// is keyed on tenants.id = app.tenant_id, so under a real non-superuser
// application role (not this project's dev/self-hosted docker-compose
// default, which connects as a Postgres superuser and so bypasses RLS
// entirely - see internal/db/rls_test.go's note) this query returns zero
// rows. This is the same bootstrap gap AD-023 closed for the anonymous
// public-status-page read path (0025_public_status_page_read); closing it
// here too - a permissive read policy for tenant enumeration when
// app.tenant_id is unset - is a real RLS-policy decision (AGENTS.md §7)
// left for a dedicated follow-up rather than folded into this task.
func (r *TenantRepository) List(ctx context.Context) ([]Tenant, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, name, slug, plan, status, contact_email, logo_content_type,
		        legal_name, tax_id, tax_id_type, billing_address, locale,
		        primary_color, secondary_color, created_at
		 FROM tenants WHERE status = 'active' ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("db: failed to list tenants: %w", err)
	}
	defer rows.Close()

	var tenants []Tenant
	for rows.Next() {
		var tenant Tenant
		if err := rows.Scan(
			&tenant.ID, &tenant.Name, &tenant.Slug, &tenant.Plan, &tenant.Status, &tenant.ContactEmail,
			&tenant.LogoContentType, &tenant.LegalName, &tenant.TaxID, &tenant.TaxIDType, &tenant.BillingAddress,
			&tenant.Locale, &tenant.PrimaryColor, &tenant.SecondaryColor, &tenant.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("db: failed to scan tenant: %w", err)
		}
		tenants = append(tenants, tenant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: failed to list tenants: %w", err)
	}

	return tenants, nil
}

// ActiveLogo returns the active tenant's stored logo bytes and content
// type. found is false when no logo has ever been uploaded (logo_data is
// NULL) or no tenant resolves at all - the caller must respond 404 rather
// than serve an empty body.
func (r *TenantRepository) ActiveLogo(ctx context.Context) (contentType string, data []byte, found bool, err error) {
	row := r.pool.QueryRow(ctx, `SELECT logo_content_type, logo_data FROM tenants`+activeTenantPredicate)

	var ct *string
	var d []byte
	if err := row.Scan(&ct, &d); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil, false, nil
		}
		return "", nil, false, fmt.Errorf("db: failed to get tenant logo: %w", err)
	}
	if d == nil {
		return "", nil, false, nil
	}
	if ct != nil {
		contentType = *ct
	}
	return contentType, d, true, nil
}
