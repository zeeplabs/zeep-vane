package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Integration is a connected external provider (Datadog in the MVP). Its
// API/App keys are always stored encrypted - EncryptedAPIKey/EncryptedAppKey
// are ciphertext, never plaintext.
type Integration struct {
	ID              string
	Provider        string
	EncryptedAPIKey []byte
	EncryptedAppKey []byte
	Status          string
	LastCheckedAt   *time.Time
	LastError       *string
}

// IntegrationRepository accesses the integrations table.
type IntegrationRepository struct {
	pool *Pool
}

// NewIntegrationRepository builds an IntegrationRepository backed by pool.
func NewIntegrationRepository(pool *Pool) *IntegrationRepository {
	return &IntegrationRepository{pool: pool}
}

// UpsertDatadog stores the Datadog integration's encrypted keys as active,
// creating the row on first connect or overwriting it on reconnect - the
// `provider` column is unique, so there is always at most one.
func (r *IntegrationRepository) UpsertDatadog(ctx context.Context, encryptedAPIKey, encryptedAppKey []byte) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO integrations (provider, encrypted_api_key, encrypted_app_key, status)
		 VALUES ('datadog', $1, $2, 'active')
		 ON CONFLICT (provider) DO UPDATE SET
		   encrypted_api_key = EXCLUDED.encrypted_api_key,
		   encrypted_app_key = EXCLUDED.encrypted_app_key,
		   status = 'active',
		   last_checked_at = NULL,
		   last_error = NULL`,
		encryptedAPIKey, encryptedAppKey,
	)
	if err != nil {
		return fmt.Errorf("db: failed to upsert datadog integration: %w", err)
	}

	return nil
}

// MarkDatadogInvalid marks the Datadog integration as invalid and records
// lastError as the reason - called once the poller exhausts its retries
// fetching SLO status (SP-09), so the admin can see why.
func (r *IntegrationRepository) MarkDatadogInvalid(ctx context.Context, lastError string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE integrations SET status = 'invalid', last_error = $1, last_checked_at = now() WHERE provider = 'datadog'`,
		lastError,
	)
	if err != nil {
		return fmt.Errorf("db: failed to mark datadog integration invalid: %w", err)
	}

	return nil
}

// GetDatadog returns the Datadog integration's current status, last error,
// and encrypted credentials, or ErrNotFound if no Datadog integration has
// been connected yet.
func (r *IntegrationRepository) GetDatadog(ctx context.Context) (*Integration, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, provider, encrypted_api_key, encrypted_app_key, status, last_checked_at, last_error
		 FROM integrations WHERE provider = 'datadog'`)

	var integration Integration
	if err := row.Scan(
		&integration.ID, &integration.Provider, &integration.EncryptedAPIKey, &integration.EncryptedAppKey,
		&integration.Status, &integration.LastCheckedAt, &integration.LastError,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to get datadog integration: %w", err)
	}

	return &integration, nil
}
