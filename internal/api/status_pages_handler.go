package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// statusPageCreatorLister is the subset of *db.StatusPageRepository the
// status pages handler depends on.
type statusPageCreatorLister interface {
	Create(ctx context.Context, statusPage *db.StatusPage, serviceIDs []string) error
	List(ctx context.Context) ([]db.StatusPage, error)
}

// StatusPagesHandler serves the status page admin routes.
type StatusPagesHandler struct {
	statusPages statusPageCreatorLister
	logger      *zap.Logger
}

// NewStatusPagesHandler builds a StatusPagesHandler backed by statusPages.
func NewStatusPagesHandler(statusPages statusPageCreatorLister, logger *zap.Logger) *StatusPagesHandler {
	return &StatusPagesHandler{statusPages: statusPages, logger: logger}
}

type createStatusPageRequest struct {
	Name       string   `json:"name"`
	Subdomain  string   `json:"subdomain"`
	DomainID   string   `json:"domain_id"`
	ServiceIDs []string `json:"service_ids"`
}

type statusPageResponse struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Subdomain    string    `json:"subdomain"`
	DomainID     string    `json:"domain_id"`
	State        string    `json:"state"`
	TLSLastError *string   `json:"tls_last_error"`
	CreatedAt    time.Time `json:"created_at"`
}

const invalidStatusPageRequestBody = `{"error":"name, subdomain, and domain_id are required"}`

// Create handles POST /api/status-pages, creating a status page bound to a
// domain and a set of services, starting in the "draft" state (SP-15). The
// system places no technical limit on the number of status pages or root
// domains a single install can have - this handler never rejects a second
// status page or a second domain, it only validates the request shape.
func (h *StatusPagesHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createStatusPageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.Subdomain == "" || req.DomainID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(invalidStatusPageRequestBody))
		return
	}

	statusPage := &db.StatusPage{Name: req.Name, Subdomain: req.Subdomain, DomainID: req.DomainID}
	if err := h.statusPages.Create(r.Context(), statusPage, req.ServiceIDs); err != nil {
		h.logger.Error("status-pages: failed to create status page", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(statusPageResponse{
		ID:           statusPage.ID,
		Name:         statusPage.Name,
		Subdomain:    statusPage.Subdomain,
		DomainID:     statusPage.DomainID,
		State:        statusPage.State,
		TLSLastError: statusPage.TLSLastError,
		CreatedAt:    statusPage.CreatedAt,
	})
}

// List handles GET /api/status-pages, returning every registered status
// page.
func (h *StatusPagesHandler) List(w http.ResponseWriter, r *http.Request) {
	statusPages, err := h.statusPages.List(r.Context())
	if err != nil {
		h.logger.Error("status-pages: failed to list status pages", zap.Error(err))
		writeInternalError(w)
		return
	}

	resp := make([]statusPageResponse, len(statusPages))
	for i, sp := range statusPages {
		resp[i] = statusPageResponse{
			ID:           sp.ID,
			Name:         sp.Name,
			Subdomain:    sp.Subdomain,
			DomainID:     sp.DomainID,
			State:        sp.State,
			TLSLastError: sp.TLSLastError,
			CreatedAt:    sp.CreatedAt,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
