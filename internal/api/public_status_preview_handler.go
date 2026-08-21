package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// PublicStatusPreviewHandler serves the dev/preview status page endpoint
// used by web/src/features/public-status. It exists ONLY because the admin
// SPA has no host-routing infrastructure in dev/preview (no way to serve a
// request as if it arrived on a status page's real published hostname) -
// SPEC_DEVIATION (AD-007, I12): production traffic never goes through this
// handler. It coexists deliberately with PublicStatusHandler.Get (mounted
// via router.HostRouter in cmd/vane, resolved by Host header - the real,
// fully public production path). This handler resolves by StatusPage ID
// instead, and sits behind RequireAuth so it is never actually public: it
// lets a logged-in admin preview a status page's public shape from the SPA
// before its hostname's TLS/DNS is ready to serve it for real.
type PublicStatusPreviewHandler struct {
	inner  *PublicStatusHandler
	logger *zap.Logger
}

// NewPublicStatusPreviewHandler builds a PublicStatusPreviewHandler that
// composes its response the same way inner (the production handler) does,
// just resolved by ID rather than Host header.
func NewPublicStatusPreviewHandler(inner *PublicStatusHandler, logger *zap.Logger) *PublicStatusPreviewHandler {
	return &PublicStatusPreviewHandler{inner: inner, logger: logger}
}

// Get handles GET /api/status-pages/{id}/public-preview, returning the same
// {services,incidents} shape PublicStatusHandler.Get produces for a
// hostname, resolved instead by the status page's ID. Behind requireAuth +
// anyRole - not the production public endpoint.
func (h *PublicStatusPreviewHandler) Get(w http.ResponseWriter, r *http.Request) {
	statusPageID := chi.URLParam(r, "id")

	resp, err := h.inner.composeResponse(r.Context(), statusPageID)
	if err != nil {
		h.logger.Error("public-status-preview: failed to compose response", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
