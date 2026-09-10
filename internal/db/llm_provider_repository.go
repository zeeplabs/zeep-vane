package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/zeeplabs/zeep-vane/internal/llm"
)

// LLMProvider is a connected LLM provider (OpenAI, ...). Its API key is
// always stored encrypted - EncryptedAPIKey is ciphertext, never plaintext.
type LLMProvider struct {
	ID              string
	Provider        string
	EncryptedAPIKey []byte
	Model           string
	Status          string
	LastCheckedAt   *time.Time
	LastError       *string
}

// LLMProviderRepository accesses the llm_providers table and the
// llm_settings row of the active tenant.
type LLMProviderRepository struct {
	pool *Pool
}

// NewLLMProviderRepository builds an LLMProviderRepository backed by pool.
func NewLLMProviderRepository(pool *Pool) *LLMProviderRepository {
	return &LLMProviderRepository{pool: pool}
}

// UpsertProvider stores provider's encrypted key and model as connected,
// creating the row on first connect or overwriting it on reconnect -
// (tenant_id, provider) is unique, so there is always at most one row per
// provider per tenant (AI-01, AI-03). Any previously recorded last_error is cleared,
// since a successful (re)connect supersedes it.
func (r *LLMProviderRepository) UpsertProvider(ctx context.Context, provider string, encryptedAPIKey []byte, model string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO llm_providers (provider, encrypted_api_key, model, status)
		 VALUES ($1, $2, $3, 'connected')
		 ON CONFLICT (tenant_id, provider) DO UPDATE SET
		   encrypted_api_key = EXCLUDED.encrypted_api_key,
		   model = EXCLUDED.model,
		   status = 'connected',
		   last_checked_at = now(),
		   last_error = NULL`,
		provider, encryptedAPIKey, model,
	)
	if err != nil {
		return fmt.Errorf("db: failed to upsert llm provider: %w", err)
	}

	return nil
}

// Get returns provider's current row, or ErrNotFound if it has never been
// connected.
func (r *LLMProviderRepository) Get(ctx context.Context, provider string) (*LLMProvider, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, provider, encrypted_api_key, model, status, last_checked_at, last_error
		 FROM llm_providers WHERE provider = $1`,
		provider,
	)

	var lp LLMProvider
	if err := row.Scan(
		&lp.ID, &lp.Provider, &lp.EncryptedAPIKey, &lp.Model,
		&lp.Status, &lp.LastCheckedAt, &lp.LastError,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to get llm provider: %w", err)
	}

	return &lp, nil
}

// ListPaginated returns one page of connected LLM providers, ordered by
// provider for a stable response (PAG-08). Returns an empty slice (not an
// error) when none exist. total is computed via COUNT(*) OVER() in the
// same query, with a zero-row fallback COUNT(*), same pattern as
// EmailProviderRepository.ListPaginated.
func (r *LLMProviderRepository) ListPaginated(ctx context.Context, page, pageSize int) ([]LLMProvider, int, error) {
	offset := (page - 1) * pageSize

	rows, err := r.pool.Query(ctx,
		`SELECT id, provider, encrypted_api_key, model, status, last_checked_at, last_error, COUNT(*) OVER() AS total
		 FROM llm_providers
		 ORDER BY provider
		 LIMIT $1 OFFSET $2`,
		pageSize, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("db: failed to list llm providers: %w", err)
	}
	defer rows.Close()

	providers := []LLMProvider{}
	total := 0
	for rows.Next() {
		var lp LLMProvider
		if err := rows.Scan(
			&lp.ID, &lp.Provider, &lp.EncryptedAPIKey, &lp.Model,
			&lp.Status, &lp.LastCheckedAt, &lp.LastError, &total,
		); err != nil {
			return nil, 0, fmt.Errorf("db: failed to scan llm provider row: %w", err)
		}
		providers = append(providers, lp)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("db: failed reading llm provider rows: %w", err)
	}

	if len(providers) == 0 {
		total, err = r.countLLMProviders(ctx)
		if err != nil {
			return nil, 0, err
		}
	}

	return providers, total, nil
}

// countLLMProviders is the zero-row fallback for ListPaginated's total
// (PAG-08).
func (r *LLMProviderRepository) countLLMProviders(ctx context.Context) (int, error) {
	var total int
	row := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM llm_providers")
	if err := row.Scan(&total); err != nil {
		return 0, fmt.Errorf("db: failed to count llm providers: %w", err)
	}
	return total, nil
}

// GetActiveProvider returns the currently active provider's name, or ""
// (not an error) when the active tenant has no llm_settings row yet, or
// has one whose active_provider is NULL. The row is created lazily by
// SetActiveProvider: since multi-tenancy-core there is one per tenant,
// not one seeded singleton per installation.
func (r *LLMProviderRepository) GetActiveProvider(ctx context.Context) (string, error) {
	var activeProvider *string
	row := r.pool.QueryRow(ctx, "SELECT active_provider FROM llm_settings")
	if err := row.Scan(&activeProvider); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("db: failed to get active llm provider: %w", err)
	}
	if activeProvider == nil {
		return "", nil
	}
	return *activeProvider, nil
}

// SetActiveProvider sets the active tenant's llm_settings row's
// active_provider to provider (AI-06).
func (r *LLMProviderRepository) SetActiveProvider(ctx context.Context, provider string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO llm_settings (active_provider) VALUES ($1)
		 ON CONFLICT (tenant_id) DO UPDATE SET active_provider = EXCLUDED.active_provider`,
		provider)
	if err != nil {
		return fmt.Errorf("db: failed to set active llm provider: %w", err)
	}

	return nil
}

// UpdateModel updates only provider's model column, leaving status and
// every other column untouched (AI-04) - email providers have no model
// concept, so this method has no EmailProviderRepository equivalent.
func (r *LLMProviderRepository) UpdateModel(ctx context.Context, provider, model string) error {
	_, err := r.pool.Exec(ctx, "UPDATE llm_providers SET model = $2 WHERE provider = $1", provider, model)
	if err != nil {
		return fmt.Errorf("db: failed to update llm provider model: %w", err)
	}

	return nil
}

// MarkInvalid records that provider's stored credentials failed
// validation, setting status to 'invalid' and recording lastError.
func (r *LLMProviderRepository) MarkInvalid(ctx context.Context, provider, lastError string) error {
	_, err := r.pool.Exec(ctx,
		"UPDATE llm_providers SET status = 'invalid', last_checked_at = now(), last_error = $2 WHERE provider = $1",
		provider, lastError,
	)
	if err != nil {
		return fmt.Errorf("db: failed to mark llm provider invalid: %w", err)
	}

	return nil
}

// MarkChecked records that provider's stored credentials were confirmed
// valid by a successful Generate* call, setting status to 'connected',
// clearing any previous last_error, and stamping last_checked_at. Callers
// must only invoke this after an actual successful completion - see
// MarkTransientFailure for a failed-but-not-unauthorized call, which must
// not silently clear a previously recorded 'invalid' status.
func (r *LLMProviderRepository) MarkChecked(ctx context.Context, provider string) error {
	_, err := r.pool.Exec(ctx,
		"UPDATE llm_providers SET status = 'connected', last_checked_at = now(), last_error = NULL WHERE provider = $1",
		provider,
	)
	if err != nil {
		return fmt.Errorf("db: failed to mark llm provider checked: %w", err)
	}

	return nil
}

// MarkTransientFailure records that provider's stored credentials were
// used in a Generate* call that failed for a reason other than
// authorization (timeout, 5xx, network error, empty response) - it stamps
// last_checked_at/last_error but deliberately leaves status untouched.
// Unlike MarkChecked, a transient failure is not evidence the credentials
// are valid: calling MarkChecked here would silently clear a previously
// recorded 'invalid' status (set by MarkInvalid after a revoked key was
// detected) the moment any unrelated 5xx or timeout occurred afterward,
// making 'invalid' effectively unobservable.
func (r *LLMProviderRepository) MarkTransientFailure(ctx context.Context, provider, lastError string) error {
	_, err := r.pool.Exec(ctx,
		"UPDATE llm_providers SET last_checked_at = now(), last_error = $2 WHERE provider = $1",
		provider, lastError,
	)
	if err != nil {
		return fmt.Errorf("db: failed to mark llm provider transient failure: %w", err)
	}

	return nil
}

// llmProviderStoreAdapter adapts *LLMProviderRepository to
// llm.LLMProviderStore, translating db.LLMProvider/db.ErrNotFound to
// llm.ProviderRecord/llm.ErrProviderRecordNotFound at the internal/db ->
// internal/llm boundary. internal/llm deliberately does not import
// internal/db (T5's package-owned ProviderRecord/ErrProviderRecordNotFound
// exist precisely so llm.Service compiles independent of the repository's
// concrete implementation), so this translation lives here instead - the
// same pattern internal/email/service.go's EmailProviderStore interface
// would need if internal/email ever stopped depending on db types
// directly.
type llmProviderStoreAdapter struct {
	repo *LLMProviderRepository
}

// NewLLMProviderStore builds an llm.LLMProviderStore backed by repo.
func NewLLMProviderStore(repo *LLMProviderRepository) llm.LLMProviderStore {
	return &llmProviderStoreAdapter{repo: repo}
}

func (a *llmProviderStoreAdapter) UpsertProvider(ctx context.Context, provider string, encryptedAPIKey []byte, model string) error {
	return a.repo.UpsertProvider(ctx, provider, encryptedAPIKey, model)
}

func (a *llmProviderStoreAdapter) Get(ctx context.Context, provider string) (*llm.ProviderRecord, error) {
	lp, err := a.repo.Get(ctx, provider)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, llm.ErrProviderRecordNotFound
		}
		return nil, err
	}

	return &llm.ProviderRecord{
		Provider:        lp.Provider,
		EncryptedAPIKey: lp.EncryptedAPIKey,
		Model:           lp.Model,
		Status:          lp.Status,
		LastCheckedAt:   lp.LastCheckedAt,
		LastError:       lp.LastError,
	}, nil
}

func (a *llmProviderStoreAdapter) ListPaginated(ctx context.Context, page, pageSize int) ([]llm.ProviderRecord, int, error) {
	providers, total, err := a.repo.ListPaginated(ctx, page, pageSize)
	if err != nil {
		return nil, 0, err
	}

	records := make([]llm.ProviderRecord, 0, len(providers))
	for _, p := range providers {
		records = append(records, llm.ProviderRecord{
			Provider:        p.Provider,
			EncryptedAPIKey: p.EncryptedAPIKey,
			Model:           p.Model,
			Status:          p.Status,
			LastCheckedAt:   p.LastCheckedAt,
			LastError:       p.LastError,
		})
	}

	return records, total, nil
}

func (a *llmProviderStoreAdapter) GetActiveProvider(ctx context.Context) (string, error) {
	return a.repo.GetActiveProvider(ctx)
}

func (a *llmProviderStoreAdapter) SetActiveProvider(ctx context.Context, provider string) error {
	return a.repo.SetActiveProvider(ctx, provider)
}

func (a *llmProviderStoreAdapter) UpdateModel(ctx context.Context, provider, model string) error {
	return a.repo.UpdateModel(ctx, provider, model)
}

func (a *llmProviderStoreAdapter) MarkInvalid(ctx context.Context, provider, lastError string) error {
	return a.repo.MarkInvalid(ctx, provider, lastError)
}

func (a *llmProviderStoreAdapter) MarkChecked(ctx context.Context, provider string) error {
	return a.repo.MarkChecked(ctx, provider)
}

func (a *llmProviderStoreAdapter) MarkTransientFailure(ctx context.Context, provider, lastError string) error {
	return a.repo.MarkTransientFailure(ctx, provider, lastError)
}
