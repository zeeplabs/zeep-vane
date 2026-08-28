package email

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/crypto"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

// Typed errors Service returns. Handlers map these to HTTP status codes;
// Service itself is transport-agnostic.
var (
	// ErrInvalidInput means a required connect field was missing or
	// from_email was not a syntactically valid address (EMAIL-02).
	ErrInvalidInput = errors.New("email: invalid input")
	// ErrValidationFailed means the provider rejected the submitted API key
	// (or the connector itself errored), so nothing was persisted
	// (EMAIL-02).
	ErrValidationFailed = errors.New("email: provider credential validation failed")
	// ErrProviderNotConnected means Activate was called for a provider
	// with no connected row (EMAIL-05).
	ErrProviderNotConnected = errors.New("email: provider not connected")
	// ErrNoActiveProvider means SendAdminInvite was called with no active
	// provider set (EMAIL-08).
	ErrNoActiveProvider = errors.New("email: no active email provider")
)

// EmailProviderStore is the subset of *db.EmailProviderRepository Service
// depends on - the same narrowing convention
// internal/api/integrations_handler.go's datadogIntegrationUpserter uses.
type EmailProviderStore interface {
	UpsertProvider(ctx context.Context, provider string, encryptedAPIKey []byte, fromEmail, fromName string) error
	Get(ctx context.Context, provider string) (*db.EmailProvider, error)
	List(ctx context.Context) ([]db.EmailProvider, error)
	GetActiveProvider(ctx context.Context) (string, error)
	SetActiveProvider(ctx context.Context, provider string) error
}

// ProviderStatus is one connected provider's observable state - never
// includes the encrypted (or decrypted) API key (EMAIL-06).
type ProviderStatus struct {
	Provider      string
	Status        string
	FromEmail     string
	FromName      string
	LastCheckedAt *time.Time
	LastError     *string
}

// ListResult is List's return shape: every connected provider plus which
// one, if any, is active.
type ListResult struct {
	ActiveProvider string
	Providers      []ProviderStatus
}

// Service implements connect/activate/list/send for email providers.
type Service struct {
	repo      EmailProviderStore
	factory   ProviderFactory
	masterKey string
	logger    *zap.Logger
	templates *templates
}

// NewService builds a Service. It returns an error only if the embedded
// admin-invite templates fail to parse - a fail-fast-at-boot check rather
// than one deferred to the first send.
func NewService(repo EmailProviderStore, factory ProviderFactory, masterKey string, logger *zap.Logger) (*Service, error) {
	tmpls, err := parseTemplates()
	if err != nil {
		return nil, err
	}

	return &Service{repo: repo, factory: factory, masterKey: masterKey, logger: logger, templates: tmpls}, nil
}

// Connect validates apiKey against provider's send API before persisting
// anything (EMAIL-01). On invalid input it returns ErrInvalidInput without
// calling the factory or any network endpoint (EMAIL-02); on a validation
// failure it returns ErrValidationFailed and persists nothing, leaving any
// previously stored row for provider untouched (EMAIL-02, spec edge case).
// On success it encrypts apiKey and upserts the row (EMAIL-01, EMAIL-03).
func (s *Service) Connect(ctx context.Context, provider, apiKey, fromEmail, fromName string) error {
	if apiKey == "" || fromEmail == "" || fromName == "" {
		return ErrInvalidInput
	}
	if _, err := mail.ParseAddress(fromEmail); err != nil {
		return ErrInvalidInput
	}

	p, err := s.factory(provider, apiKey)
	if err != nil {
		return ErrValidationFailed
	}

	if err := p.ValidateCredentials(ctx); err != nil {
		return ErrValidationFailed
	}

	encryptedAPIKey, err := crypto.Encrypt(s.masterKey, []byte(apiKey))
	if err != nil {
		return fmt.Errorf("email: failed to encrypt api key: %w", err)
	}

	if err := s.repo.UpsertProvider(ctx, provider, encryptedAPIKey, fromEmail, fromName); err != nil {
		return fmt.Errorf("email: failed to persist provider: %w", err)
	}

	return nil
}

// Activate sets provider as the active email provider (EMAIL-04). It
// succeeds only when provider has a connected row; otherwise it returns
// ErrProviderNotConnected and leaves active_provider unchanged (EMAIL-05).
func (s *Service) Activate(ctx context.Context, provider string) error {
	ep, err := s.repo.Get(ctx, provider)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return ErrProviderNotConnected
		}
		return fmt.Errorf("email: failed to get provider for activation: %w", err)
	}
	if ep.Status != "connected" {
		return ErrProviderNotConnected
	}

	if err := s.repo.SetActiveProvider(ctx, provider); err != nil {
		return fmt.Errorf("email: failed to set active provider: %w", err)
	}

	return nil
}

// List returns every connected provider plus the current active provider
// (EMAIL-06). It never includes any provider's encrypted API key.
func (s *Service) List(ctx context.Context) (ListResult, error) {
	providers, err := s.repo.List(ctx)
	if err != nil {
		return ListResult{}, fmt.Errorf("email: failed to list providers: %w", err)
	}

	active, err := s.repo.GetActiveProvider(ctx)
	if err != nil {
		return ListResult{}, fmt.Errorf("email: failed to get active provider: %w", err)
	}

	statuses := make([]ProviderStatus, 0, len(providers))
	for _, p := range providers {
		statuses = append(statuses, ProviderStatus{
			Provider:      p.Provider,
			Status:        p.Status,
			FromEmail:     p.FromEmail,
			FromName:      p.FromName,
			LastCheckedAt: p.LastCheckedAt,
			LastError:     p.LastError,
		})
	}

	return ListResult{ActiveProvider: active, Providers: statuses}, nil
}
