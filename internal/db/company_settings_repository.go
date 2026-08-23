package db

import (
	"context"
	"fmt"
)

// CompanySettings is the single, always-present company_settings row
// (design.md: true singleton, no company_id - AD-002 single-tenant).
type CompanySettings struct {
	Name         string
	ContactEmail string
	LogoURL      *string
}

// CompanySettingsRepository accesses the singleton company_settings row.
type CompanySettingsRepository struct {
	pool *Pool
}

// NewCompanySettingsRepository builds a CompanySettingsRepository backed by
// pool.
func NewCompanySettingsRepository(pool *Pool) *CompanySettingsRepository {
	return &CompanySettingsRepository{pool: pool}
}

// Get returns the singleton company_settings row. There is no "not found"
// branch: the 0012_company_settings migration seeds exactly one row, so a
// missing row would be a migration bug, not a normal runtime state
// (SET-03, SET-06).
func (r *CompanySettingsRepository) Get(ctx context.Context) (*CompanySettings, error) {
	row := r.pool.QueryRow(ctx, "SELECT name, contact_email, logo_url FROM company_settings WHERE id = 1")

	var settings CompanySettings
	if err := row.Scan(&settings.Name, &settings.ContactEmail, &settings.LogoURL); err != nil {
		return nil, fmt.Errorf("db: failed to get company settings: %w", err)
	}

	return &settings, nil
}

// Update persists name and contactEmail on the singleton row and returns
// the updated settings (SET-01).
func (r *CompanySettingsRepository) Update(ctx context.Context, name, contactEmail string) (*CompanySettings, error) {
	row := r.pool.QueryRow(ctx,
		"UPDATE company_settings SET name = $1, contact_email = $2 WHERE id = 1 RETURNING name, contact_email, logo_url",
		name, contactEmail,
	)

	var settings CompanySettings
	if err := row.Scan(&settings.Name, &settings.ContactEmail, &settings.LogoURL); err != nil {
		return nil, fmt.Errorf("db: failed to update company settings: %w", err)
	}

	return &settings, nil
}

// UpdateLogoURL persists logoURL on the singleton row independently of
// Update, and returns the updated settings. The handler calls this only
// after the logo file write to UPLOADS_DIR has already succeeded, so a
// failed write never leaves logo_url pointing at a file that was never
// written (SET-13).
func (r *CompanySettingsRepository) UpdateLogoURL(ctx context.Context, logoURL string) (*CompanySettings, error) {
	row := r.pool.QueryRow(ctx,
		"UPDATE company_settings SET logo_url = $1 WHERE id = 1 RETURNING name, contact_email, logo_url",
		logoURL,
	)

	var settings CompanySettings
	if err := row.Scan(&settings.Name, &settings.ContactEmail, &settings.LogoURL); err != nil {
		return nil, fmt.Errorf("db: failed to update company settings logo url: %w", err)
	}

	return &settings, nil
}
