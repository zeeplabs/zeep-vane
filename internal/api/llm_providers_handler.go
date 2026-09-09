package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/llm"
)

// llmProvidersPageSize is the fixed page size for /api/integrations/llm
// (same PAG-08 precedent as emailProvidersPageSize - spec.md Assumptions).
const llmProvidersPageSize = 20

// llmProviderService is the subset of *llm.Service LLMProvidersHandler
// depends on - the same narrowing convention emailProviderService uses.
type llmProviderService interface {
	Connect(ctx context.Context, provider, apiKey, model string) error
	SetModel(ctx context.Context, provider, model string) error
	Activate(ctx context.Context, provider string) error
	List(ctx context.Context, page, pageSize int) (llm.ListResult, error)
}

// LLMProvidersHandler serves the /api/integrations/llm/* admin routes.
type LLMProvidersHandler struct {
	svc    llmProviderService
	logger *zap.Logger
}

// NewLLMProvidersHandler builds an LLMProvidersHandler.
func NewLLMProvidersHandler(svc llmProviderService, logger *zap.Logger) *LLMProvidersHandler {
	return &LLMProvidersHandler{svc: svc, logger: logger}
}

// isKnownLLMProvider is the LLM equivalent of isKnownEmailProvider - only
// "openai" is accepted today (AI-01 AC4-equivalent, matches
// modelAllowlist's single provider key in internal/llm/service.go).
func isKnownLLMProvider(provider string) bool {
	return provider == "openai"
}

type connectLLMProviderRequest struct {
	APIKey string `json:"api_key"`
	Model  string `json:"model"`
}

type setLLMProviderModelRequest struct {
	Model string `json:"model"`
}

const (
	unknownLLMProviderBody      = `{"error":"unknown llm provider"}`
	invalidLLMProviderBody      = `{"error":"invalid llm provider api key"}`
	unknownLLMModelBody         = `{"error":"unknown model for llm provider"}`
	llmProviderNotConnectedBody = `{"error":"llm provider not connected"}`
)

// Connect handles POST /api/integrations/llm/{provider}. It validates the
// submitted key against the provider before anything is persisted (AI-01);
// on an unknown provider it responds 404, and on invalid input or a failed
// credential check it responds 422 without persisting anything (AI-02). The
// response never includes api_key in any form.
func (h *LLMProvidersHandler) Connect(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	if !isKnownLLMProvider(provider) {
		writeUnknownLLMProvider(w)
		return
	}

	var req connectLLMProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeInvalidLLMProvider(w)
		return
	}

	if err := h.svc.Connect(r.Context(), provider, req.APIKey, req.Model); err != nil {
		if errors.Is(err, llm.ErrInvalidInput) || errors.Is(err, llm.ErrValidationFailed) {
			writeInvalidLLMProvider(w)
			return
		}
		h.logger.Error("llm providers: failed to connect provider", zap.String("provider", provider), zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte(`{"status":"connected"}`))
}

// SetModel handles POST /api/integrations/llm/{provider}/model. It changes
// the active model for an already-connected provider without re-supplying
// or re-validating the API key (AI-04). An unknown provider responds 404, a
// model outside the provider's known-model allowlist or a provider with no
// connected row responds 422.
func (h *LLMProvidersHandler) SetModel(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	if !isKnownLLMProvider(provider) {
		writeUnknownLLMProvider(w)
		return
	}

	var req setLLMProviderModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Model == "" {
		writeUnknownLLMModel(w)
		return
	}

	if err := h.svc.SetModel(r.Context(), provider, req.Model); err != nil {
		if errors.Is(err, llm.ErrProviderNotConnected) {
			writeLLMProviderNotConnected(w)
			return
		}
		if errors.Is(err, llm.ErrUnknownModel) {
			writeUnknownLLMModel(w)
			return
		}
		h.logger.Error("llm providers: failed to set model", zap.String("provider", provider), zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"updated"}`))
}

// Activate handles POST /api/integrations/llm/{provider}/activate. It
// succeeds only when provider has a connected row (AI-06); an unknown
// provider responds 404, and a provider with no connected row responds 422
// without changing the active provider.
func (h *LLMProvidersHandler) Activate(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	if !isKnownLLMProvider(provider) {
		writeUnknownLLMProvider(w)
		return
	}

	if err := h.svc.Activate(r.Context(), provider); err != nil {
		if errors.Is(err, llm.ErrProviderNotConnected) {
			writeLLMProviderNotConnected(w)
			return
		}
		h.logger.Error("llm providers: failed to activate provider", zap.String("provider", provider), zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"active"}`))
}

type llmProviderResponse struct {
	Provider      string  `json:"provider"`
	Model         string  `json:"model"`
	Status        string  `json:"status"`
	LastCheckedAt *string `json:"last_checked_at"`
	LastError     *string `json:"last_error"`
}

type listLLMProvidersResponse struct {
	ActiveProvider *string               `json:"active_provider"`
	Providers      []llmProviderResponse `json:"providers"`
	Total          int                   `json:"total"`
	Page           int                   `json:"page"`
	PageSize       int                   `json:"page_size"`
}

// List handles GET /api/integrations/llm. It returns one page of connected
// providers (PAG-08, page_size 20) plus which one, if any, is active - an
// empty list and a null active_provider when nothing has ever been
// connected, never a 404. The response never includes api_key in any form.
func (h *LLMProvidersHandler) List(w http.ResponseWriter, r *http.Request) {
	page := parsePage(r)
	result, err := h.svc.List(r.Context(), page, llmProvidersPageSize)
	if err != nil {
		h.logger.Error("llm providers: failed to list providers", zap.Error(err))
		writeInternalError(w)
		return
	}

	resp := listLLMProvidersResponse{
		Providers: make([]llmProviderResponse, 0, len(result.Providers)),
		Total:     result.Total,
		Page:      result.Page,
		PageSize:  result.PageSize,
	}
	if result.ActiveProvider != "" {
		active := result.ActiveProvider
		resp.ActiveProvider = &active
	}
	for _, p := range result.Providers {
		item := llmProviderResponse{
			Provider:  p.Provider,
			Model:     p.Model,
			Status:    p.Status,
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

func writeUnknownLLMProvider(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(unknownLLMProviderBody))
}

func writeInvalidLLMProvider(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_, _ = w.Write([]byte(invalidLLMProviderBody))
}

func writeUnknownLLMModel(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_, _ = w.Write([]byte(unknownLLMModelBody))
}

func writeLLMProviderNotConnected(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_, _ = w.Write([]byte(llmProviderNotConnectedBody))
}
