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
	ListPaginated(ctx context.Context, page, pageSize int) ([]db.EmailProvider, int, error)
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

// List returns one page of connected providers plus the current active
// provider (EMAIL-06, PAG-08). It never includes any provider's encrypted
// API key.
func (s *Service) List(ctx context.Context, page, pageSize int) (ListResult, error) {
	providers, total, err := s.repo.ListPaginated(ctx, page, pageSize)
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

	return ListResult{ActiveProvider: active, Providers: statuses, Total: total, Page: page, PageSize: pageSize}, nil
}

// SendAdminInvite renders the admin-invite template and sends it through
// whichever provider is currently active (EMAIL-07). If no provider is
// active it returns ErrNoActiveProvider without building a Provider or
// calling any send API (EMAIL-08). A send failure is returned to the
// caller unmodified - no retry, no queue (EMAIL-08).
func (s *Service) SendAdminInvite(ctx context.Context, to string, data AdminInviteEmailData) error {
	active, err := s.repo.GetActiveProvider(ctx)
	if err != nil {
		return fmt.Errorf("email: failed to get active provider: %w", err)
	}
	if active == "" {
		return ErrNoActiveProvider
	}

	ep, err := s.repo.Get(ctx, active)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			// The active_provider FK guarantees this row exists in
			// practice; treat it the same as "no active provider" rather
			// than a distinct error, since from the caller's point of
			// view sending is equally impossible either way.
			return ErrNoActiveProvider
		}
		return fmt.Errorf("email: failed to get active provider row: %w", err)
	}

	apiKey, err := crypto.Decrypt(s.masterKey, ep.EncryptedAPIKey)
	if err != nil {
		return fmt.Errorf("email: failed to decrypt active provider api key: %w", err)
	}

	provider, err := s.factory(active, string(apiKey))
	if err != nil {
		return fmt.Errorf("email: failed to build active provider client: %w", err)
	}

	htmlBody, textBody, err := s.templates.renderAdminInvite(data)
	if err != nil {
		return err
	}

	msg := Message{
		To:        to,
		FromEmail: ep.FromEmail,
		FromName:  ep.FromName,
		Subject:   fmt.Sprintf("You've been invited to join %s", data.CompanyName),
		HTMLBody:  htmlBody,
		TextBody:  textBody,
	}

	return provider.Send(ctx, msg)
}

// SendPasswordReset renders the password-reset template and sends it
// through whichever provider is currently active. Same active-provider and
// send-failure handling as SendAdminInvite (no retry, no queue) - the
// caller (PasswordResetHandler) treats a failure here as non-fatal to the
// request, since the response must stay identical whether or not delivery
// succeeded (account-enumeration protection).
func (s *Service) SendPasswordReset(ctx context.Context, to string, data PasswordResetEmailData) error {
	active, err := s.repo.GetActiveProvider(ctx)
	if err != nil {
		return fmt.Errorf("email: failed to get active provider: %w", err)
	}
	if active == "" {
		return ErrNoActiveProvider
	}

	ep, err := s.repo.Get(ctx, active)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return ErrNoActiveProvider
		}
		return fmt.Errorf("email: failed to get active provider row: %w", err)
	}

	apiKey, err := crypto.Decrypt(s.masterKey, ep.EncryptedAPIKey)
	if err != nil {
		return fmt.Errorf("email: failed to decrypt active provider api key: %w", err)
	}

	provider, err := s.factory(active, string(apiKey))
	if err != nil {
		return fmt.Errorf("email: failed to build active provider client: %w", err)
	}

	htmlBody, textBody, err := s.templates.renderPasswordReset(data)
	if err != nil {
		return err
	}

	msg := Message{
		To:        to,
		FromEmail: ep.FromEmail,
		FromName:  ep.FromName,
		Subject:   fmt.Sprintf("Reset your %s password", data.CompanyName),
		HTMLBody:  htmlBody,
		TextBody:  textBody,
	}

	return provider.Send(ctx, msg)
}
