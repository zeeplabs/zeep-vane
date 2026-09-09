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
