package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/mail"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// companySettingsStore is the subset of *db.CompanySettingsRepository the
// company settings handler depends on.
type companySettingsStore interface {
	Get(ctx context.Context) (*db.CompanySettings, error)
	Update(ctx context.Context, name, contactEmail string) (*db.CompanySettings, error)
	UpdateLogoURL(ctx context.Context, logoURL string) (*db.CompanySettings, error)
}

// CompanySettingsHandler serves the company settings admin routes: GET/PATCH
// /api/company-settings and POST /api/company-settings/logo.
type CompanySettingsHandler struct {
	settings companySettingsStore
	logger   *zap.Logger
}

// NewCompanySettingsHandler builds a CompanySettingsHandler backed by
// settings.
func NewCompanySettingsHandler(settings companySettingsStore, logger *zap.Logger) *CompanySettingsHandler {
	return &CompanySettingsHandler{settings: settings, logger: logger}
}

type companySettingsResponse struct {
	Name         string  `json:"name"`
	ContactEmail string  `json:"contact_email"`
	LogoURL      *string `json:"logo_url"`
}

type updateCompanySettingsRequest struct {
	Name         string `json:"name"`
	ContactEmail string `json:"contact_email"`
}

const invalidCompanySettingsRequestBody = `{"error":"name is required and contact_email must be a valid e-mail address"}`

func toCompanySettingsResponse(settings *db.CompanySettings) companySettingsResponse {
	return companySettingsResponse{Name: settings.Name, ContactEmail: settings.ContactEmail, LogoURL: settings.LogoURL}
}

// Get handles GET /api/company-settings, returning the singleton company
// settings row - including on a fresh install, where it is the seeded row
// rather than a 404 (SET-03).
func (h *CompanySettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	settings, err := h.settings.Get(r.Context())
	if err != nil {
		h.logger.Error("company-settings: failed to get settings", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toCompanySettingsResponse(settings))
}

// Update handles PATCH /api/company-settings. It requires a non-empty name
// (SET-04) and a syntactically valid contact_email (SET-05); either
// failure responds 422 without touching the persisted row.
func (h *CompanySettingsHandler) Update(w http.ResponseWriter, r *http.Request) {
	var req updateCompanySettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCompanySettingsValidationError(w)
		return
	}

	if req.Name == "" {
		writeCompanySettingsValidationError(w)
		return
	}
	if _, err := mail.ParseAddress(req.ContactEmail); err != nil {
		writeCompanySettingsValidationError(w)
		return
	}

	settings, err := h.settings.Update(r.Context(), req.Name, req.ContactEmail)
	if err != nil {
		h.logger.Error("company-settings: failed to update settings", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toCompanySettingsResponse(settings))
}

func writeCompanySettingsValidationError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_, _ = w.Write([]byte(invalidCompanySettingsRequestBody))
}
