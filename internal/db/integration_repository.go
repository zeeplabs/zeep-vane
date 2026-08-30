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

// MarkDatadogChecked records a successful poll cycle: the Datadog
// integration is (re)marked active, any previously recorded failure reason
// is cleared, and last_checked_at advances to now. Called once per poll
// cycle when at least one service was fetched successfully (H5/H6) - not
// per-service - so a single misconfigured SLO among several reachable
// services never leaves the integration stuck reporting invalid, and a
// poller that has recovered after a prior failure clears that failure
// instead of staying invalid forever.
func (r *IntegrationRepository) MarkDatadogChecked(ctx context.Context) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE integrations SET status = 'active', last_error = NULL, last_checked_at = now() WHERE provider = 'datadog'`,
	)
	if err != nil {
		return fmt.Errorf("db: failed to mark datadog integration checked: %w", err)
	}

	return nil
}

// ListPaginated returns one page of connected integrations
// (admin-dashboard ADM-13/ADM-14 - the poller status view reads this
// directly, with no new fetch logic), ordered by provider for a stable
// response (PAG-08). total is computed via COUNT(*) OVER() in the same
// query, with a zero-row fallback COUNT(*), same pattern as the other
// ListPaginated methods. Single caller confirmed (poller_status.go) -
// internal/poller/poller.go never calls IntegrationRepository.List, only
// MarkDatadogChecked/MarkDatadogInvalid, so replacing List outright (no
// ServiceRepository-style "keep the old method" precedent needed here) is
// safe.
func (r *IntegrationRepository) ListPaginated(ctx context.Context, page, pageSize int) ([]Integration, int, error) {
	offset := (page - 1) * pageSize

	rows, err := r.pool.Query(ctx,
		`SELECT id, provider, encrypted_api_key, encrypted_app_key, status, last_checked_at, last_error, COUNT(*) OVER() AS total
		 FROM integrations
		 ORDER BY provider
		 LIMIT $1 OFFSET $2`,
		pageSize, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("db: failed to list integrations: %w", err)
	}
	defer rows.Close()

	integrations := []Integration{}
	total := 0
	for rows.Next() {
		var integration Integration
		if err := rows.Scan(
			&integration.ID, &integration.Provider, &integration.EncryptedAPIKey, &integration.EncryptedAppKey,
			&integration.Status, &integration.LastCheckedAt, &integration.LastError, &total,
		); err != nil {
			return nil, 0, fmt.Errorf("db: failed to scan integration row: %w", err)
		}
		integrations = append(integrations, integration)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("db: failed reading integration rows: %w", err)
	}

	if len(integrations) == 0 {
		total, err = r.countIntegrations(ctx)
		if err != nil {
			return nil, 0, err
		}
	}

	return integrations, total, nil
}

// countIntegrations is the zero-row fallback for ListPaginated's total
// (PAG-08).
func (r *IntegrationRepository) countIntegrations(ctx context.Context) (int, error) {
	var total int
	row := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM integrations")
	if err := row.Scan(&total); err != nil {
		return 0, fmt.Errorf("db: failed to count integrations: %w", err)
	}
	return total, nil
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
