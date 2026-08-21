package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/connectors/datadog"
	"github.com/zeeplabs/zeep-vane/internal/crypto"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

// datadogIntegrationUpserter is the subset of *db.IntegrationRepository the
// integrations handler depends on.
type datadogIntegrationUpserter interface {
	UpsertDatadog(ctx context.Context, encryptedAPIKey, encryptedAppKey []byte) error
	GetDatadog(ctx context.Context) (*db.Integration, error)
}

// validateDatadogCredentials checks that an API key + App key pair is valid
// and has SLO read permission, without needing a specific SLO ID (SP-01.2).
type validateDatadogCredentials func(ctx context.Context, apiKey, appKey string) error

// searchDatadogSLOs searches Datadog for SLOs matching query, using the
// stored integration's decrypted key pair (I14).
type searchDatadogSLOs func(ctx context.Context, apiKey, appKey, query string) ([]datadog.SLOSummary, error)

// IntegrationsHandler serves the integrations admin routes.
type IntegrationsHandler struct {
	integrations datadogIntegrationUpserter
	validate     validateDatadogCredentials
	search       searchDatadogSLOs
	masterKey    string
	logger       *zap.Logger
}

// NewIntegrationsHandler builds an IntegrationsHandler. validate is called
// with the submitted keys to confirm they are usable before anything is
// encrypted or persisted; masterKey encrypts the keys at rest (T16). search
// is called with the stored, decrypted key pair to serve SLO lookups (I14).
func NewIntegrationsHandler(integrations datadogIntegrationUpserter, validate validateDatadogCredentials, search searchDatadogSLOs, masterKey string, logger *zap.Logger) *IntegrationsHandler {
	return &IntegrationsHandler{integrations: integrations, validate: validate, search: search, masterKey: masterKey, logger: logger}
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

type datadogStatusResponse struct {
	Status        string  `json:"status"`
	LastCheckedAt *string `json:"last_checked_at"`
	LastError     *string `json:"last_error"`
}

const datadogIntegrationNotFoundBody = `{"error":"datadog integration not connected yet"}`

// Status handles GET /api/integrations/datadog/status, exposing the
// Datadog integration's current status and last recorded error to the
// authenticated admin (SP-09) - in particular, the failure the poller
// recorded after exhausting its retries.
func (h *IntegrationsHandler) Status(w http.ResponseWriter, r *http.Request) {
	integration, err := h.integrations.GetDatadog(r.Context())
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(datadogIntegrationNotFoundBody))
			return
		}
		h.logger.Error("integrations: failed to get datadog integration status", zap.Error(err))
		writeInternalError(w)
		return
	}

	resp := datadogStatusResponse{Status: integration.Status, LastError: integration.LastError}
	if integration.LastCheckedAt != nil {
		formatted := integration.LastCheckedAt.Format(time.RFC3339)
		resp.LastCheckedAt = &formatted
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

type sloSummaryResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SearchSLOs handles GET /api/integrations/datadog/slos?query=, letting an
// admin search Datadog SLOs by name to link one to a service (AF-42, I14).
// No integration connected yet is not an error from this endpoint's point
// of view (SPEC_DEVIATION: the done-when only specifies 200/401, not this
// case) - it simply has nothing to search yet, so it returns an empty list
// rather than failing the admin's search UI.
func (h *IntegrationsHandler) SearchSLOs(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")

	integration, err := h.integrations.GetDatadog(r.Context())
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeSLOSummaries(w, nil)
			return
		}
		h.logger.Error("integrations: failed to get datadog integration for slo search", zap.Error(err))
		writeInternalError(w)
		return
	}

	apiKey, err := crypto.Decrypt(h.masterKey, integration.EncryptedAPIKey)
	if err != nil {
		h.logger.Error("integrations: failed to decrypt datadog api key", zap.Error(err))
		writeInternalError(w)
		return
	}
	appKey, err := crypto.Decrypt(h.masterKey, integration.EncryptedAppKey)
	if err != nil {
		h.logger.Error("integrations: failed to decrypt datadog app key", zap.Error(err))
		writeInternalError(w)
		return
	}

	slos, err := h.search(r.Context(), string(apiKey), string(appKey), query)
	if err != nil {
		h.logger.Error("integrations: failed to search datadog slos", zap.Error(err))
		writeInternalError(w)
		return
	}

	resp := make([]sloSummaryResponse, len(slos))
	for i, slo := range slos {
		resp[i] = sloSummaryResponse{ID: slo.ID, Name: slo.Name}
	}
	writeSLOSummaries(w, resp)
}

func writeSLOSummaries(w http.ResponseWriter, resp []sloSummaryResponse) {
	if resp == nil {
		resp = []sloSummaryResponse{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func writeInvalidDatadogCredentials(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_, _ = w.Write([]byte(invalidDatadogCredentialsBody))
}
