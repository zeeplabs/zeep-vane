package llm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/crypto"
)

// Typed errors Service returns. Handlers map these to HTTP status codes;
// Service itself is transport-agnostic.
var (
	// ErrInvalidInput means a required connect field was missing (AI-02).
	ErrInvalidInput = errors.New("llm: invalid input")
	// ErrValidationFailed means the provider rejected the submitted API key
	// (or the connector itself errored), so nothing was persisted
	// (AI-02).
	ErrValidationFailed = errors.New("llm: provider credential validation failed")
	// ErrUnknownModel means SetModel (or Connect, if an explicit model is
	// eventually validated there too) was given a model outside the
	// provider's known-model allowlist (AI-04).
	ErrUnknownModel = errors.New("llm: unknown model for provider")
	// ErrProviderNotConnected means Activate or SetModel was called for a
	// provider with no connected row.
	ErrProviderNotConnected = errors.New("llm: provider not connected")
	// ErrNoActiveProvider means a Generate* method was called with no
	// active provider set (mirrors email.ErrNoActiveProvider).
	ErrNoActiveProvider = errors.New("llm: no active llm provider")
	// ErrProviderRecordNotFound is what LLMProviderStore.Get returns when
	// no row exists for the requested provider - this package's own
	// not-found sentinel (mirrors db.ErrNotFound) so Service does not need
	// to import internal/db to distinguish "not connected" from an actual
	// repository failure.
	ErrProviderRecordNotFound = errors.New("llm: provider record not found")
)

// modelAllowlist is the fixed set of models Service.Connect/SetModel will
// accept per provider. A model not in this list is rejected regardless of
// whether the provider itself would accept it - keeps the admin-facing
// model picker and the validation in lockstep with a small, deliberately
// curated set rather than accepting anything a caller sends.
var modelAllowlist = map[string][]string{
	"openai": {"gpt-4o-mini", "gpt-4o", "gpt-4.1-mini", "gpt-4.1"},
}

// defaultModel is used by Connect when the caller supplies an empty model
// (AI-03).
const defaultModel = "gpt-4o-mini"

// ProviderRecord is one connected LLM provider's stored row, as returned
// by LLMProviderStore. Owned by this package (not internal/db) so
// Service compiles independent of the repository's concrete
// implementation - the repository is expected to return values shaped
// like this one.
type ProviderRecord struct {
	Provider        string
	EncryptedAPIKey []byte
	Model           string
	Status          string
	LastCheckedAt   *time.Time
	LastError       *string
}

// LLMProviderStore is the subset of the LLM provider repository Service
// depends on - the same narrowing convention email.EmailProviderStore
// uses.
type LLMProviderStore interface {
	UpsertProvider(ctx context.Context, provider string, encryptedAPIKey []byte, model string) error
	Get(ctx context.Context, provider string) (*ProviderRecord, error)
	ListPaginated(ctx context.Context, page, pageSize int) ([]ProviderRecord, int, error)
	GetActiveProvider(ctx context.Context) (string, error)
	SetActiveProvider(ctx context.Context, provider string) error
	UpdateModel(ctx context.Context, provider, model string) error
}

// ProviderStatus is one connected provider's observable state - never
// includes the encrypted (or decrypted) API key.
type ProviderStatus struct {
	Provider      string
	Model         string
	Status        string
	LastCheckedAt *time.Time
	LastError     *string
}

// ListResult is List's return shape: one page of connected providers plus
// which one, if any, is active, and Total/Page/PageSize so the caller can
// build a pagination envelope (PAG-08) without a second round-trip.
type ListResult struct {
	ActiveProvider string
	Providers      []ProviderStatus
	Total          int
	Page           int
	PageSize       int
}

// Service implements connect/activate/list/generate for LLM providers.
type Service struct {
	repo      LLMProviderStore
	factory   ProviderFactory
	masterKey string
	logger    *zap.Logger
}

// NewService builds a Service.
func NewService(repo LLMProviderStore, factory ProviderFactory, masterKey string, logger *zap.Logger) *Service {
	return &Service{repo: repo, factory: factory, masterKey: masterKey, logger: logger}
}

// Connect validates apiKey against provider's API before persisting
// anything (AI-01). On invalid input it returns ErrInvalidInput without
// calling the factory or any network endpoint (AI-02); on a validation
// failure it returns ErrValidationFailed and persists nothing, leaving any
// previously stored row for provider untouched (AI-02). An empty model
// defaults to defaultModel (AI-03). On success it encrypts apiKey and
// upserts the row (AI-01).
func (s *Service) Connect(ctx context.Context, provider, apiKey, model string) error {
	if apiKey == "" {
		return ErrInvalidInput
	}
	if model == "" {
		model = defaultModel
	}

	p, err := s.factory(provider, apiKey, model)
	if err != nil {
		return ErrValidationFailed
	}

	if err := p.ValidateCredentials(ctx); err != nil {
		return ErrValidationFailed
	}

	encryptedAPIKey, err := crypto.Encrypt(s.masterKey, []byte(apiKey))
	if err != nil {
		return fmt.Errorf("llm: failed to encrypt api key: %w", err)
	}

	if err := s.repo.UpsertProvider(ctx, provider, encryptedAPIKey, model); err != nil {
		return fmt.Errorf("llm: failed to persist provider: %w", err)
	}

	return nil
}

// SetModel changes the active model for an already-connected provider,
// without re-supplying or re-validating the API key (AI-04). It returns
// ErrProviderNotConnected if provider has no connected row, ErrUnknownModel
// if model is outside provider's allowlist - in either case the stored row
// is left untouched.
func (s *Service) SetModel(ctx context.Context, provider, model string) error {
	ep, err := s.repo.Get(ctx, provider)
	if err != nil {
		if errors.Is(err, ErrProviderRecordNotFound) {
			return ErrProviderNotConnected
		}
		return fmt.Errorf("llm: failed to get provider for model update: %w", err)
	}
	if ep.Status != "connected" {
		return ErrProviderNotConnected
	}

	allowed := modelAllowlist[provider]
	valid := false
	for _, m := range allowed {
		if m == model {
			valid = true
			break
		}
	}
	if !valid {
		return ErrUnknownModel
	}

	if err := s.repo.UpdateModel(ctx, provider, model); err != nil {
		return fmt.Errorf("llm: failed to update model: %w", err)
	}

	return nil
}

// Activate sets provider as the active LLM provider. It succeeds only when
// provider has a connected row; otherwise it returns ErrProviderNotConnected
// and leaves active_provider unchanged.
func (s *Service) Activate(ctx context.Context, provider string) error {
	ep, err := s.repo.Get(ctx, provider)
	if err != nil {
		if errors.Is(err, ErrProviderRecordNotFound) {
			return ErrProviderNotConnected
		}
		return fmt.Errorf("llm: failed to get provider for activation: %w", err)
	}
	if ep.Status != "connected" {
		return ErrProviderNotConnected
	}

	if err := s.repo.SetActiveProvider(ctx, provider); err != nil {
		return fmt.Errorf("llm: failed to set active provider: %w", err)
	}

	return nil
}

// List returns one page of connected providers plus the current active
// provider (PAG-08). It never includes any provider's encrypted API key.
func (s *Service) List(ctx context.Context, page, pageSize int) (ListResult, error) {
	providers, total, err := s.repo.ListPaginated(ctx, page, pageSize)
	if err != nil {
		return ListResult{}, fmt.Errorf("llm: failed to list providers: %w", err)
	}

	active, err := s.repo.GetActiveProvider(ctx)
	if err != nil {
		return ListResult{}, fmt.Errorf("llm: failed to get active provider: %w", err)
	}

	statuses := make([]ProviderStatus, 0, len(providers))
	for _, p := range providers {
		statuses = append(statuses, ProviderStatus{
			Provider:      p.Provider,
			Model:         p.Model,
			Status:        p.Status,
			LastCheckedAt: p.LastCheckedAt,
			LastError:     p.LastError,
		})
	}

	return ListResult{ActiveProvider: active, Providers: statuses, Total: total, Page: page, PageSize: pageSize}, nil
}

// GenerateDegradedAnalysis builds the degraded-tooltip prompt from in and
// returns the active provider's completion for it, untouched. Returns
// ErrNoActiveProvider immediately if no provider is active, without
// attempting any network call (mirrors email.Service.SendAdminInvite's
// active-provider-resolution short-circuit).
func (s *Service) GenerateDegradedAnalysis(ctx context.Context, in AnalysisInput) (string, error) {
	return s.generate(ctx, in, buildDegradedTooltipPrompt)
}

// GenerateOutageDescription builds the outage-description prompt from in
// and returns the active provider's completion for it, untouched. Same
// ErrNoActiveProvider short-circuit as GenerateDegradedAnalysis.
func (s *Service) GenerateOutageDescription(ctx context.Context, in AnalysisInput) (string, error) {
	return s.generate(ctx, in, buildOutageDescriptionPrompt)
}

// GenerateClosingComment builds the closing-comment prompt from in and
// returns the active provider's completion for it, untouched. Same
// ErrNoActiveProvider short-circuit as GenerateDegradedAnalysis.
func (s *Service) GenerateClosingComment(ctx context.Context, in AnalysisInput) (string, error) {
	return s.generate(ctx, in, buildClosingCommentPrompt)
}

// generate resolves the active provider, builds its client via the
// factory, and returns build's prompt run through Provider.Complete
// unmodified - empty/malformed-response handling is the caller's
// (SLOAnalyzer's) responsibility, not duplicated here.
func (s *Service) generate(ctx context.Context, in AnalysisInput, build func(AnalysisInput) (string, string)) (string, error) {
	active, err := s.repo.GetActiveProvider(ctx)
	if err != nil {
		return "", fmt.Errorf("llm: failed to get active provider: %w", err)
	}
	if active == "" {
		return "", ErrNoActiveProvider
	}

	ep, err := s.repo.Get(ctx, active)
	if err != nil {
		if errors.Is(err, ErrProviderRecordNotFound) {
			// The active_provider FK guarantees this row exists in
			// practice; treat it the same as "no active provider" rather
			// than a distinct error, since from the caller's point of
			// view generating is equally impossible either way.
			return "", ErrNoActiveProvider
		}
		return "", fmt.Errorf("llm: failed to get active provider row: %w", err)
	}

	apiKey, err := crypto.Decrypt(s.masterKey, ep.EncryptedAPIKey)
	if err != nil {
		return "", fmt.Errorf("llm: failed to decrypt active provider api key: %w", err)
	}

	provider, err := s.factory(active, string(apiKey), ep.Model)
	if err != nil {
		return "", fmt.Errorf("llm: failed to build active provider client: %w", err)
	}

	systemPrompt, userPrompt := build(in)

	return provider.Complete(ctx, systemPrompt, userPrompt)
}
