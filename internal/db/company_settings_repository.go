package db

import (
	"context"
	"fmt"
)

// CompanySettings is the single, always-present company_settings row
// (design.md: true singleton, no company_id - AD-002 single-tenant).
type CompanySettings struct {
	Name            string
	ContactEmail    string
	LogoContentType *string
}

// logoServedPath is the fixed URL the stored logo is always served at.
// The old design encoded the file extension into the path itself
// ("/uploads/logo.png"); the logo now lives in the row as bytes plus a
// content type, so the path never changes - LogoServedURL reports
// whether it currently resolves to anything by checking LogoContentType,
// not by encoding format into the path.
const logoServedPath = "/uploads/logo"

// LogoServedURL returns the URL the stored logo is served at, or nil if no
// logo has ever been uploaded.
func (s *CompanySettings) LogoServedURL() *string {
	if s.LogoContentType == nil {
		return nil
	}
	url := logoServedPath
	return &url
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
// (SET-03, SET-06). It never fetches the logo's bytes - only whether one
// is stored (LogoContentType) - since most callers only need to know
// whether to render an <img> tag; GetLogo is the one path that needs the
// bytes themselves.
func (r *CompanySettingsRepository) Get(ctx context.Context) (*CompanySettings, error) {
	row := r.pool.QueryRow(ctx, "SELECT name, contact_email, logo_content_type FROM company_settings WHERE id = 1")

	var settings CompanySettings
	if err := row.Scan(&settings.Name, &settings.ContactEmail, &settings.LogoContentType); err != nil {
		return nil, fmt.Errorf("db: failed to get company settings: %w", err)
	}

	return &settings, nil
}

// Update persists name and contactEmail on the singleton row and returns
// the updated settings (SET-01).
func (r *CompanySettingsRepository) Update(ctx context.Context, name, contactEmail string) (*CompanySettings, error) {
	row := r.pool.QueryRow(ctx,
		"UPDATE company_settings SET name = $1, contact_email = $2 WHERE id = 1 RETURNING name, contact_email, logo_content_type",
		name, contactEmail,
	)

	var settings CompanySettings
	if err := row.Scan(&settings.Name, &settings.ContactEmail, &settings.LogoContentType); err != nil {
		return nil, fmt.Errorf("db: failed to update company settings: %w", err)
	}

	return &settings, nil
}

// UpdateLogo persists the logo's bytes and content type on the singleton
// row independently of Update, and returns the updated settings. The
// handler calls this only after sniffing/validating the upload, so a
// rejected upload never touches the previously stored logo (SET-13).
//
// Storing the bytes in Postgres itself - rather than a file under
// UPLOADS_DIR on whichever replica's local disk handled this request - is
// what makes the logo correct under multi-replica deployment without
// requiring operators to provision a shared RWX volume: every replica
// reads the same row, regardless of which one received the upload.
func (r *CompanySettingsRepository) UpdateLogo(ctx context.Context, contentType string, data []byte) (*CompanySettings, error) {
	row := r.pool.QueryRow(ctx,
		"UPDATE company_settings SET logo_data = $1, logo_content_type = $2 WHERE id = 1 RETURNING name, contact_email, logo_content_type",
		data, contentType,
	)

	var settings CompanySettings
	if err := row.Scan(&settings.Name, &settings.ContactEmail, &settings.LogoContentType); err != nil {
		return nil, fmt.Errorf("db: failed to update company settings logo: %w", err)
	}

	return &settings, nil
}

// GetLogo returns the stored logo's bytes and content type. found is false
// when no logo has ever been uploaded (logo_data is NULL) - the caller
// must respond 404 rather than serve an empty body.
func (r *CompanySettingsRepository) GetLogo(ctx context.Context) (contentType string, data []byte, found bool, err error) {
	row := r.pool.QueryRow(ctx, "SELECT logo_content_type, logo_data FROM company_settings WHERE id = 1")

	var ct *string
	var d []byte
	if err := row.Scan(&ct, &d); err != nil {
		return "", nil, false, fmt.Errorf("db: failed to get company settings logo: %w", err)
	}
	if d == nil {
		return "", nil, false, nil
	}
	if ct != nil {
		contentType = *ct
	}
	return contentType, d, true, nil
}
