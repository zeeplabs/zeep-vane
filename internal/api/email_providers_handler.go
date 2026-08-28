package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/email"
)

// emailProviderService is the subset of *email.Service EmailProvidersHandler
// depends on - the same narrowing convention datadogIntegrationUpserter
// uses for the Datadog integration handler.
type emailProviderService interface {
	Connect(ctx context.Context, provider, apiKey, fromEmail, fromName string) error
	Activate(ctx context.Context, provider string) error
	List(ctx context.Context) (email.ListResult, error)
}

// EmailProvidersHandler serves the /api/integrations/email/* admin routes.
type EmailProvidersHandler struct {
	svc    emailProviderService
	logger *zap.Logger
}

// NewEmailProvidersHandler builds an EmailProvidersHandler.
func NewEmailProvidersHandler(svc emailProviderService, logger *zap.Logger) *EmailProvidersHandler {
	return &EmailProvidersHandler{svc: svc, logger: logger}
}

// knownEmailProviders are the only provider names this feature accepts
// (EMAIL-01 AC4, spec Edge Cases). Any other {provider} path segment is
// rejected before it ever reaches the service layer.
func isKnownEmailProvider(provider string) bool {
	return provider == "sendgrid" || provider == "resend"
}

type connectEmailProviderRequest struct {
	APIKey    string `json:"api_key"`
	FromEmail string `json:"from_email"`
	FromName  string `json:"from_name"`
}

const (
	unknownEmailProviderBody      = `{"error":"unknown email provider"}`
	invalidEmailProviderBody      = `{"error":"invalid email provider api key, from_email, or from_name"}`
	emailProviderNotConnectedBody = `{"error":"email provider not connected"}`
)

// Connect handles POST /api/integrations/email/{provider}. It validates the
// submitted key against the provider before anything is persisted
// (EMAIL-01); on an unknown provider it responds 404 (EMAIL-01 AC4), and on
// invalid input or a failed credential check it responds 422 without
// persisting anything (EMAIL-01 AC2). The response never includes api_key
// in any form (EMAIL-01 AC5).
func (h *EmailProvidersHandler) Connect(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	if !isKnownEmailProvider(provider) {
		writeUnknownEmailProvider(w)
		return
	}

	var req connectEmailProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeInvalidEmailProvider(w)
		return
	}

	if err := h.svc.Connect(r.Context(), provider, req.APIKey, req.FromEmail, req.FromName); err != nil {
		if errors.Is(err, email.ErrInvalidInput) || errors.Is(err, email.ErrValidationFailed) {
			writeInvalidEmailProvider(w)
			return
		}
		h.logger.Error("email providers: failed to connect provider", zap.String("provider", provider), zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte(`{"status":"connected"}`))
}

// Activate handles POST /api/integrations/email/{provider}/activate. It
// succeeds only when provider has a connected row (EMAIL-04); an unknown
// provider responds 404, and a provider with no connected row responds 422
// without changing active_provider (EMAIL-05).
func (h *EmailProvidersHandler) Activate(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	if !isKnownEmailProvider(provider) {
		writeUnknownEmailProvider(w)
		return
	}

	if err := h.svc.Activate(r.Context(), provider); err != nil {
		if errors.Is(err, email.ErrProviderNotConnected) {
			writeEmailProviderNotConnected(w)
			return
		}
		h.logger.Error("email providers: failed to activate provider", zap.String("provider", provider), zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"active"}`))
}

type emailProviderResponse struct {
	Provider      string  `json:"provider"`
	Status        string  `json:"status"`
	FromEmail     string  `json:"from_email"`
	FromName      string  `json:"from_name"`
	LastCheckedAt *string `json:"last_checked_at"`
	LastError     *string `json:"last_error"`
}

type listEmailProvidersResponse struct {
	ActiveProvider *string                 `json:"active_provider"`
	Providers      []emailProviderResponse `json:"providers"`
}

// List handles GET /api/integrations/email. It returns every connected
// provider plus which one, if any, is active (EMAIL-06 AC1) - an empty
// list and a null active_provider when nothing has ever been connected
// (EMAIL-06 AC2, EMAIL-04 AC4), never a 404. The response never includes
// api_key in any form.
func (h *EmailProvidersHandler) List(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.List(r.Context())
	if err != nil {
		h.logger.Error("email providers: failed to list providers", zap.Error(err))
		writeInternalError(w)
		return
	}

	resp := listEmailProvidersResponse{Providers: make([]emailProviderResponse, 0, len(result.Providers))}
	if result.ActiveProvider != "" {
		active := result.ActiveProvider
		resp.ActiveProvider = &active
	}
	for _, p := range result.Providers {
		item := emailProviderResponse{
			Provider:  p.Provider,
			Status:    p.Status,
			FromEmail: p.FromEmail,
			FromName:  p.FromName,
			LastError: p.LastError,
		}
		if p.LastCheckedAt != nil {
			formatted := p.LastCheckedAt.Format(time.RFC3339)
			item.LastCheckedAt = &formatted
		}
		resp.Providers = append(resp.Providers, item)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func writeUnknownEmailProvider(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(unknownEmailProviderBody))
}

func writeInvalidEmailProvider(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_, _ = w.Write([]byte(invalidEmailProviderBody))
}

func writeEmailProviderNotConnected(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_, _ = w.Write([]byte(emailProviderNotConnectedBody))
}
