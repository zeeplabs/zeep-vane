package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/mail"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/uploads"
)

// maxLogoBytes bounds an uploaded logo to 10 MB (SET-08) - an owner-only
// endpoint, but still a bound against disk/memory abuse.
const maxLogoBytes = 10 << 20

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
	settings   companySettingsStore
	uploadsDir string
	logger     *zap.Logger
}

// NewCompanySettingsHandler builds a CompanySettingsHandler backed by
// settings, writing uploaded logos under uploadsDir.
func NewCompanySettingsHandler(settings companySettingsStore, uploadsDir string, logger *zap.Logger) *CompanySettingsHandler {
	return &CompanySettingsHandler{settings: settings, uploadsDir: uploadsDir, logger: logger}
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

const (
	invalidLogoUploadBody = `{"error":"logo must be a PNG or SVG image no larger than 10 MB"}`
	logoFormFieldName     = "logo"
)

// UploadLogo handles POST /api/company-settings/logo, a multipart upload
// of the company logo. It bounds the request body to maxLogoBytes (SET-08)
// before parsing, sniffs the uploaded bytes to confirm they are a PNG or
// SVG image (SET-09) rather than trusting the client-sent Content-Type
// header alone, writes the file via uploads.Save (SET-10, SET-11), and
// only then updates the persisted logo_url (SET-13: a failed write never
// updates the DB).
func (h *CompanySettingsHandler) UploadLogo(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxLogoBytes)

	if err := r.ParseMultipartForm(maxLogoBytes); err != nil {
		// Covers both an oversized body (SET-08: http.MaxBytesReader trips
		// mid-parse) and a malformed multipart request.
		writeLogoUploadError(w)
		return
	}
	if r.MultipartForm != nil {
		defer func() { _ = r.MultipartForm.RemoveAll() }()
	}

	file, _, err := r.FormFile(logoFormFieldName)
	if err != nil {
		writeLogoUploadError(w)
		return
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(file)
	if err != nil {
		writeLogoUploadError(w)
		return
	}

	ext, ok := logoExtensionFor(data)
	if !ok {
		// SET-09: neither image/png nor image/svg+xml - previous logo (if
		// any) is left untouched, since we return before uploads.Save.
		writeLogoUploadError(w)
		return
	}

	servedPath, err := uploads.Save(h.uploadsDir, ext, bytes.NewReader(data))
	if err != nil {
		h.logger.Error("company-settings: failed to save logo file", zap.Error(err))
		writeInternalError(w)
		return
	}

	settings, err := h.settings.UpdateLogoURL(r.Context(), servedPath)
	if err != nil {
		h.logger.Error("company-settings: failed to update logo url", zap.Error(err))
		writeInternalError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toCompanySettingsResponse(settings))
}

func writeLogoUploadError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_, _ = w.Write([]byte(invalidLogoUploadBody))
}

// logoExtensionFor sniffs data (the uploaded file's actual bytes, never
// the client-declared Content-Type alone) and returns the file extension
// to store it under, and whether it was recognized as an allowed image
// type (SET-09: image/png or image/svg+xml).
//
// SPEC_DEVIATION: design.md describes sniffing via a plain
// http.DetectContentType call. net/http's sniff table has no signature for
// SVG (it has none for any XML-based vector format), so a literal
// DetectContentType-only check can never accept a real SVG upload -
// breaking spec.md's P1 AC1 ("owner uploads ... image/svg+xml ... SHALL
// respond 200"). isLikelySVG below adds a minimal, still byte-content-based
// check (never the client's Content-Type header) so a real SVG file is
// recognized, while still rejecting arbitrary non-image files.
// Reason: net/http has no SVG sniffing signature; recognizing SVGs is
// required by the spec's stated MIME allowlist.
func logoExtensionFor(data []byte) (ext string, ok bool) {
	if http.DetectContentType(data) == "image/png" {
		return ".png", true
	}
	if isLikelySVG(data) {
		return ".svg", true
	}
	return "", false
}

// isLikelySVG reports whether data's content, after skipping an optional
// byte-order mark, leading whitespace, an XML declaration, and any
// DOCTYPE, begins with an "<svg" tag.
func isLikelySVG(data []byte) bool {
	trimmed := bytes.TrimLeft(data, "\xEF\xBB\xBF \t\r\n")

	if bytes.HasPrefix(trimmed, []byte("<?xml")) {
		if idx := bytes.Index(trimmed, []byte("?>")); idx != -1 {
			trimmed = bytes.TrimLeft(trimmed[idx+2:], " \t\r\n")
		}
	}
	for bytes.HasPrefix(trimmed, []byte("<!")) {
		idx := bytes.IndexByte(trimmed, '>')
		if idx == -1 {
			break
		}
		trimmed = bytes.TrimLeft(trimmed[idx+1:], " \t\r\n")
	}

	return bytes.HasPrefix(bytes.ToLower(trimmed), []byte("<svg"))
}
