package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// EmailProvider is a connected email provider (SendGrid or Resend). Its API
// key is always stored encrypted - EncryptedAPIKey is ciphertext, never
// plaintext.
type EmailProvider struct {
	ID              string
	Provider        string
	EncryptedAPIKey []byte
	FromEmail       string
	FromName        string
	Status          string
	LastCheckedAt   *time.Time
	LastError       *string
}

// EmailProviderRepository accesses the email_providers table and the
// email_settings singleton row.
type EmailProviderRepository struct {
	pool *Pool
}

// NewEmailProviderRepository builds an EmailProviderRepository backed by
// pool.
func NewEmailProviderRepository(pool *Pool) *EmailProviderRepository {
	return &EmailProviderRepository{pool: pool}
}

// UpsertProvider stores provider's encrypted key and sender fields as
// connected, creating the row on first connect or overwriting it on
// reconnect - the `provider` column is unique, so there is always at most
// one row per provider (EMAIL-01, EMAIL-03). Any previously recorded
// last_error is cleared, since a successful (re)connect supersedes it.
func (r *EmailProviderRepository) UpsertProvider(ctx context.Context, provider string, encryptedAPIKey []byte, fromEmail, fromName string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO email_providers (provider, encrypted_api_key, from_email, from_name, status)
		 VALUES ($1, $2, $3, $4, 'connected')
		 ON CONFLICT (provider) DO UPDATE SET
		   encrypted_api_key = EXCLUDED.encrypted_api_key,
		   from_email = EXCLUDED.from_email,
		   from_name = EXCLUDED.from_name,
		   status = 'connected',
		   last_checked_at = now(),
		   last_error = NULL`,
		provider, encryptedAPIKey, fromEmail, fromName,
	)
	if err != nil {
		return fmt.Errorf("db: failed to upsert email provider: %w", err)
	}

	return nil
}

// Get returns provider's current row, or ErrNotFound if it has never been
// connected.
func (r *EmailProviderRepository) Get(ctx context.Context, provider string) (*EmailProvider, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, provider, encrypted_api_key, from_email, from_name, status, last_checked_at, last_error
		 FROM email_providers WHERE provider = $1`,
		provider,
	)

	var ep EmailProvider
	if err := row.Scan(
		&ep.ID, &ep.Provider, &ep.EncryptedAPIKey, &ep.FromEmail, &ep.FromName,
		&ep.Status, &ep.LastCheckedAt, &ep.LastError,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to get email provider: %w", err)
	}

	return &ep, nil
}

// List returns every connected email provider, ordered by provider for a
// stable response. Returns an empty slice (not an error) when none exist
// (EMAIL-06).
func (r *EmailProviderRepository) List(ctx context.Context) ([]EmailProvider, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, provider, encrypted_api_key, from_email, from_name, status, last_checked_at, last_error
		 FROM email_providers ORDER BY provider`)
	if err != nil {
		return nil, fmt.Errorf("db: failed to list email providers: %w", err)
	}
	defer rows.Close()

	providers := []EmailProvider{}
	for rows.Next() {
		var ep EmailProvider
		if err := rows.Scan(
			&ep.ID, &ep.Provider, &ep.EncryptedAPIKey, &ep.FromEmail, &ep.FromName,
			&ep.Status, &ep.LastCheckedAt, &ep.LastError,
		); err != nil {
			return nil, fmt.Errorf("db: failed to scan email provider row: %w", err)
		}
		providers = append(providers, ep)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: failed reading email provider rows: %w", err)
	}

	return providers, nil
}

// GetActiveProvider returns the currently active provider's name, or ""
// (not an error) when email_settings.active_provider is NULL - the
// singleton row always exists (seeded by migration 0016).
func (r *EmailProviderRepository) GetActiveProvider(ctx context.Context) (string, error) {
	var activeProvider *string
	row := r.pool.QueryRow(ctx, "SELECT active_provider FROM email_settings WHERE id = 1")
	if err := row.Scan(&activeProvider); err != nil {
		return "", fmt.Errorf("db: failed to get active email provider: %w", err)
	}
	if activeProvider == nil {
		return "", nil
	}
	return *activeProvider, nil
}

// SetActiveProvider updates the singleton email_settings row's
// active_provider to provider (EMAIL-04).
func (r *EmailProviderRepository) SetActiveProvider(ctx context.Context, provider string) error {
	_, err := r.pool.Exec(ctx, "UPDATE email_settings SET active_provider = $1 WHERE id = 1", provider)
	if err != nil {
		return fmt.Errorf("db: failed to set active email provider: %w", err)
	}

	return nil
}
