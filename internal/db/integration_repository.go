package db

import (
	"context"
	"fmt"
	"time"
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
