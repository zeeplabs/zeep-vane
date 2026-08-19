package api

import (
	"context"
	"encoding/json"
	"net/http"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/crypto"
)

// datadogIntegrationUpserter is the subset of *db.IntegrationRepository the
// integrations handler depends on.
type datadogIntegrationUpserter interface {
	UpsertDatadog(ctx context.Context, encryptedAPIKey, encryptedAppKey []byte) error
}

// validateDatadogCredentials checks that an API key + App key pair is valid
// and has SLO read permission, without needing a specific SLO ID (SP-01.2).
type validateDatadogCredentials func(ctx context.Context, apiKey, appKey string) error

// IntegrationsHandler serves the integrations admin routes.
type IntegrationsHandler struct {
	integrations datadogIntegrationUpserter
	validate     validateDatadogCredentials
	masterKey    string
	logger       *zap.Logger
}

// NewIntegrationsHandler builds an IntegrationsHandler. validate is called
// with the submitted keys to confirm they are usable before anything is
// encrypted or persisted; masterKey encrypts the keys at rest (T16).
func NewIntegrationsHandler(integrations datadogIntegrationUpserter, validate validateDatadogCredentials, masterKey string, logger *zap.Logger) *IntegrationsHandler {
	return &IntegrationsHandler{integrations: integrations, validate: validate, masterKey: masterKey, logger: logger}
}

type connectDatadogRequest struct {
	APIKey string `json:"api_key"`
	AppKey string `json:"app_key"`
}

const invalidDatadogCredentialsBody = `{"error":"invalid datadog api key or app key, or missing slo read permission"}`

// ConnectDatadog handles POST /api/integrations/datadog. It validates the
// submitted key pair against Datadog before persisting anything (SP-01.1);
// on an invalid key or missing SLO permission it rejects with 422 and saves
// nothing (SP-01.2). The keys are encrypted before being stored and are
// never echoed back or logged in plaintext (SP-01.4).
func (h *IntegrationsHandler) ConnectDatadog(w http.ResponseWriter, r *http.Request) {
	var req connectDatadogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.APIKey == "" || req.AppKey == "" {
		writeInvalidDatadogCredentials(w)
		return
	}

	if err := h.validate(r.Context(), req.APIKey, req.AppKey); err != nil {
		// Any validation failure - invalid key, missing permission, or a
		// connectivity problem with Datadog - means the credentials are not
		// confirmed usable, so nothing is persisted (SP-01.2).
		writeInvalidDatadogCredentials(w)
		return
	}

	encryptedAPIKey, err := crypto.Encrypt(h.masterKey, []byte(req.APIKey))
	if err != nil {
		h.logger.Error("integrations: failed to encrypt datadog api key", zap.Error(err))
		writeInternalError(w)
		return
	}

	encryptedAppKey, err := crypto.Encrypt(h.masterKey, []byte(req.AppKey))
	if err != nil {
		h.logger.Error("integrations: failed to encrypt datadog app key", zap.Error(err))
		writeInternalError(w)
		return
	}

	if err := h.integrations.UpsertDatadog(r.Context(), encryptedAPIKey, encryptedAppKey); err != nil {
		h.logger.Error("integrations: failed to persist datadog integration", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte(`{"status":"connected"}`))
}

func writeInvalidDatadogCredentials(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_, _ = w.Write([]byte(invalidDatadogCredentialsBody))
}
