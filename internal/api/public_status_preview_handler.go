package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// statusPageStateGetter is the subset of *db.StatusPageRepository the
// preview handler depends on to gate unpublished pages.
type statusPageStateGetter interface {
	GetByID(ctx context.Context, id string) (*db.StatusPage, error)
}

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
	statusPages statusPageStateGetter
	inner       *PublicStatusHandler
	logger      *zap.Logger
}

// NewPublicStatusPreviewHandler builds a PublicStatusPreviewHandler that
// composes its response the same way inner (the production handler) does,
// just resolved by ID rather than Host header.
func NewPublicStatusPreviewHandler(statusPages statusPageStateGetter, inner *PublicStatusHandler, logger *zap.Logger) *PublicStatusPreviewHandler {
	return &PublicStatusPreviewHandler{statusPages: statusPages, inner: inner, logger: logger}
}

// Get handles GET /api/status-pages/{id}/public-preview, returning the same
// {services,incidents} shape PublicStatusHandler.Get produces for a
// hostname, resolved instead by the status page's ID. Behind requireAuth +
// anyRole - not the production public endpoint.
//
// AD-008 (deliberate divergence from production, supersedes part of
// AD-007/I12's rationale): this endpoint composes for a status page in ANY
// state, including domain-less (domain_id: null) pages that never had a
// hostname to mirror in the first place. router.HostRouter's own gate
// (internal/router/host_router.go) still requires "published" for the
// real, fully public path - only this authenticated/admin-only preview
// drops the gate, so an admin can check a page's content/layout before its
// domain's DNS/TLS exists at all (the bug AD-008 fixes).
func (h *PublicStatusPreviewHandler) Get(w http.ResponseWriter, r *http.Request) {
	statusPageID := chi.URLParam(r, "id")

	if _, err := h.statusPages.GetByID(r.Context(), statusPageID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		h.logger.Error("public-status-preview: failed to look up status page", zap.Error(err))
		writeInternalError(w)
		return
	}

	resp, err := h.inner.composeResponse(r.Context(), statusPageID, parsePage(r))
	if err != nil {
		h.logger.Error("public-status-preview: failed to compose response", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
