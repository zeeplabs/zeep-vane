//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// pngSignatureBytes is a minimal, valid PNG file header - enough for
// http.DetectContentType to sniff "image/png" (it only inspects the
// leading bytes, not the full chunk structure).
var pngSignatureBytes = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D}

const validSVGBody = `<?xml version="1.0" encoding="UTF-8"?><svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"></svg>`

// buildMultipartLogoRequest builds a POST /api/company-settings/logo
// request whose "logo" form field contains content, named filename.
func buildMultipartLogoRequest(t *testing.T, filename string, content []byte, token string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile(logoFormFieldName, filename)
	if err != nil {
		t.Fatalf("CreateFormFile() returned unexpected error: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("part.Write() returned unexpected error: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/company-settings/logo", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

// newCompanySettingsRouter builds a router mounting only RequireAuth (no
// RequireRole) in front of CompanySettingsHandler, mirroring
// newDomainsRouter: RBAC for these routes is asserted at the routes.go
// wiring level (T8), not by the handler itself. Uploaded logos are written
// under a fresh t.TempDir(), isolated per test.
func newCompanySettingsRouter(t *testing.T) (http.Handler, *db.Pool, *db.AdminRepository) {
	t.Helper()
	r, pool, admins, _ := newCompanySettingsRouterWithUploadsDir(t)
	return r, pool, admins
}

func newCompanySettingsRouterWithUploadsDir(t *testing.T) (http.Handler, *db.Pool, *db.AdminRepository, string) {
	t.Helper()
	dsn := testDatabaseURL(t)

	if err := db.MigrateUp(dsn, "../db/migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	// The company_settings row is a singleton shared across every test in
	// this package - reset it to a known state before and after each test.
	reset := func() {
		_, _ = pool.Exec(context.Background(), "UPDATE company_settings SET name = '', contact_email = '', logo_url = NULL WHERE id = 1")
	}
	reset()
	t.Cleanup(reset)

	uploadsDir := t.TempDir()
	repo := db.NewCompanySettingsRepository(pool)
	admins := db.NewAdminRepository(pool)
	handler := NewCompanySettingsHandler(repo, uploadsDir, zap.NewNop())

	r := chi.NewRouter()
	r.Group(func(protected chi.Router) {
		protected.Use(RequireAuth(middlewareTestSecret, admins))
		protected.Get("/api/company-settings", handler.Get)
		protected.Patch("/api/company-settings", handler.Update)
		protected.Post("/api/company-settings/logo", handler.UploadLogo)
	})

	return r, pool, admins, uploadsDir
}

func getCompanySettings(t *testing.T, r http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/company-settings", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func patchCompanySettings(t *testing.T, r http.Handler, token string, body updateCompanySettingsRequest) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/api/company-settings", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestCompanySettingsGet_FreshInstall_200SeededRow asserts SET-03: GET
// returns the seeded row (empty name/contact_email, null logo_url) on a
// fresh install, never a 404.
func TestCompanySettingsGet_FreshInstall_200SeededRow(t *testing.T) {
	r, _, admins := newCompanySettingsRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := getCompanySettings(t, r, token)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp companySettingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if resp.Name != "" {
		t.Errorf("Name = %q, want \"\"", resp.Name)
	}
	if resp.ContactEmail != "" {
		t.Errorf("ContactEmail = %q, want \"\"", resp.ContactEmail)
	}
	if resp.LogoURL != nil {
		t.Errorf("LogoURL = %v, want nil", *resp.LogoURL)
	}
}

// TestCompanySettingsUpdate_ValidBody_200Persists asserts SET-01: a valid
// PATCH persists name/contact_email and responds 200 with the updated row.
func TestCompanySettingsUpdate_ValidBody_200Persists(t *testing.T) {
	r, _, admins := newCompanySettingsRouter(t)
	token := issueTestSessionToken(t, admins)

	rec := patchCompanySettings(t, r, token, updateCompanySettingsRequest{Name: "Acme Inc.", ContactEmail: "owner@acme.example.com"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp companySettingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if resp.Name != "Acme Inc." {
		t.Errorf("Name = %q, want %q", resp.Name, "Acme Inc.")
	}
	if resp.ContactEmail != "owner@acme.example.com" {
		t.Errorf("ContactEmail = %q, want %q", resp.ContactEmail, "owner@acme.example.com")
	}

	getRec := getCompanySettings(t, r, token)
	var getResp companySettingsResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if getResp.Name != "Acme Inc." {
		t.Errorf("persisted Name = %q, want %q", getResp.Name, "Acme Inc.")
	}
}

// TestCompanySettingsUpdate_EmptyName_422NoPersistence asserts SET-04: an
// empty name is rejected with 422 and the persisted row is left untouched.
func TestCompanySettingsUpdate_EmptyName_422NoPersistence(t *testing.T) {
	r, _, admins := newCompanySettingsRouter(t)
	token := issueTestSessionToken(t, admins)

	// Seed a known-good value first so we can prove it survives the
	// rejected PATCH.
	if rec := patchCompanySettings(t, r, token, updateCompanySettingsRequest{Name: "Acme Inc.", ContactEmail: "owner@acme.example.com"}); rec.Code != http.StatusOK {
		t.Fatalf("setup PATCH status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	rec := patchCompanySettings(t, r, token, updateCompanySettingsRequest{Name: "", ContactEmail: "owner@acme.example.com"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}

	getRec := getCompanySettings(t, r, token)
	var getResp companySettingsResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if getResp.Name != "Acme Inc." {
		t.Errorf("Name after rejected PATCH = %q, want unchanged %q", getResp.Name, "Acme Inc.")
	}
}

// TestCompanySettingsUpdate_MalformedContactEmail_422NoPersistence asserts
// SET-05: a malformed contact_email is rejected with 422 and the persisted
// row is left untouched.
func TestCompanySettingsUpdate_MalformedContactEmail_422NoPersistence(t *testing.T) {
	r, _, admins := newCompanySettingsRouter(t)
	token := issueTestSessionToken(t, admins)

	if rec := patchCompanySettings(t, r, token, updateCompanySettingsRequest{Name: "Acme Inc.", ContactEmail: "owner@acme.example.com"}); rec.Code != http.StatusOK {
		t.Fatalf("setup PATCH status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	rec := patchCompanySettings(t, r, token, updateCompanySettingsRequest{Name: "Acme Inc.", ContactEmail: "not-an-email"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}

	getRec := getCompanySettings(t, r, token)
	var getResp companySettingsResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if getResp.ContactEmail != "owner@acme.example.com" {
		t.Errorf("ContactEmail after rejected PATCH = %q, want unchanged %q", getResp.ContactEmail, "owner@acme.example.com")
	}
}

// TestUploadLogo_ValidPNG_200UpdatesLogoURL asserts SET-07: a valid PNG
// upload under 10 MB stores the file, updates logo_url, and responds 200.
func TestUploadLogo_ValidPNG_200UpdatesLogoURL(t *testing.T) {
	r, _, admins, uploadsDir := newCompanySettingsRouterWithUploadsDir(t)
	token := issueTestSessionToken(t, admins)

	req := buildMultipartLogoRequest(t, "logo.png", pngSignatureBytes, token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp companySettingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if resp.LogoURL == nil || *resp.LogoURL != "/uploads/logo.png" {
		t.Fatalf("LogoURL = %v, want %q", resp.LogoURL, "/uploads/logo.png")
	}

	if _, err := os.Stat(filepath.Join(uploadsDir, "logo.png")); err != nil {
		t.Errorf("expected logo.png to exist in uploads dir, Stat() returned error: %v", err)
	}

	getRec := getCompanySettings(t, r, token)
	var getResp companySettingsResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if getResp.LogoURL == nil || *getResp.LogoURL != "/uploads/logo.png" {
		t.Errorf("persisted LogoURL = %v, want %q", getResp.LogoURL, "/uploads/logo.png")
	}
}

// TestUploadLogo_ValidSVG_200UpdatesLogoURL asserts SET-07 for the other
// allowed MIME type: a valid SVG upload succeeds the same way a PNG does.
func TestUploadLogo_ValidSVG_200UpdatesLogoURL(t *testing.T) {
	r, _, admins, uploadsDir := newCompanySettingsRouterWithUploadsDir(t)
	token := issueTestSessionToken(t, admins)

	req := buildMultipartLogoRequest(t, "logo.svg", []byte(validSVGBody), token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp companySettingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if resp.LogoURL == nil || *resp.LogoURL != "/uploads/logo.svg" {
		t.Fatalf("LogoURL = %v, want %q", resp.LogoURL, "/uploads/logo.svg")
	}
	if _, err := os.Stat(filepath.Join(uploadsDir, "logo.svg")); err != nil {
		t.Errorf("expected logo.svg to exist in uploads dir, Stat() returned error: %v", err)
	}
}

// TestUploadLogo_OverSizeLimit_422NoLogoURLChange asserts SET-08: a file
// over 10 MB is rejected with 422, no file is written, and logo_url is
// left unchanged.
func TestUploadLogo_OverSizeLimit_422NoLogoURLChange(t *testing.T) {
	r, _, admins, uploadsDir := newCompanySettingsRouterWithUploadsDir(t)
	token := issueTestSessionToken(t, admins)

	oversized := make([]byte, maxLogoBytes+1024)
	copy(oversized, pngSignatureBytes)

	req := buildMultipartLogoRequest(t, "logo.png", oversized, token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}

	entries, err := os.ReadDir(uploadsDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("os.ReadDir() returned unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("uploads dir contains %d entries after a rejected oversized upload, want 0", len(entries))
	}

	getRec := getCompanySettings(t, r, token)
	var getResp companySettingsResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if getResp.LogoURL != nil {
		t.Errorf("LogoURL after rejected oversized upload = %v, want nil (unchanged)", *getResp.LogoURL)
	}
}

// TestUploadLogo_WrongMIMEType_422NoLogoURLChange asserts SET-09: a
// non-PNG/SVG payload is rejected with 422 and the previously stored logo
// (none, here) is left untouched.
func TestUploadLogo_WrongMIMEType_422NoLogoURLChange(t *testing.T) {
	r, _, admins, _ := newCompanySettingsRouterWithUploadsDir(t)
	token := issueTestSessionToken(t, admins)

	req := buildMultipartLogoRequest(t, "logo.txt", []byte("just some plain text, not an image"), token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}

	getRec := getCompanySettings(t, r, token)
	var getResp companySettingsResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if getResp.LogoURL != nil {
		t.Errorf("LogoURL after rejected wrong-mime upload = %v, want nil (unchanged)", *getResp.LogoURL)
	}
}

// TestUploadLogo_SecondValidUpload_OverwritesFirst asserts SET-10: a
// second successful upload leaves exactly one logo file on disk - the
// first one is removed/overwritten, never left orphaned.
func TestUploadLogo_SecondValidUpload_OverwritesFirst(t *testing.T) {
	r, _, admins, uploadsDir := newCompanySettingsRouterWithUploadsDir(t)
	token := issueTestSessionToken(t, admins)

	firstReq := buildMultipartLogoRequest(t, "logo.png", pngSignatureBytes, token)
	firstRec := httptest.NewRecorder()
	r.ServeHTTP(firstRec, firstReq)
	if firstRec.Code != http.StatusOK {
		t.Fatalf("first upload status = %d, want %d, body = %s", firstRec.Code, http.StatusOK, firstRec.Body.String())
	}

	secondReq := buildMultipartLogoRequest(t, "logo.svg", []byte(validSVGBody), token)
	secondRec := httptest.NewRecorder()
	r.ServeHTTP(secondRec, secondReq)
	if secondRec.Code != http.StatusOK {
		t.Fatalf("second upload status = %d, want %d, body = %s", secondRec.Code, http.StatusOK, secondRec.Body.String())
	}

	entries, err := os.ReadDir(uploadsDir)
	if err != nil {
		t.Fatalf("os.ReadDir() returned unexpected error: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("uploads dir entries = %v, want exactly 1", names)
	}
	if entries[0].Name() != "logo.svg" {
		t.Errorf("remaining file = %q, want %q", entries[0].Name(), "logo.svg")
	}

	getRec := getCompanySettings(t, r, token)
	var getResp companySettingsResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if getResp.LogoURL == nil || *getResp.LogoURL != "/uploads/logo.svg" {
		t.Errorf("persisted LogoURL = %v, want %q", getResp.LogoURL, "/uploads/logo.svg")
	}
}
