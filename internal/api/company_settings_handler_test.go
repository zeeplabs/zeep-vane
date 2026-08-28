//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/dbtest"
)

// pngSignatureBytes is a minimal, valid PNG file header - enough for
// http.DetectContentType to sniff "image/png" (it only inspects the
// leading bytes, not the full chunk structure).
var pngSignatureBytes = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D}

const validSVGBody = `<?xml version="1.0" encoding="UTF-8"?><svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"></svg>`

// multipartLogoBody serializes a POST /api/company-settings/logo body whose
// "logo" form field contains content, named filename, returning the raw
// bytes and the matching Content-Type header value. Exposed separately from
// buildMultipartLogoRequest so a test can measure the exact multipart
// framing overhead for a given filename (see
// TestUploadLogo_JustUnderSizeLimit_200UpdatesLogoURL).
func multipartLogoBody(t *testing.T, filename string, content []byte) (body []byte, contentType string) {
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
	return buf.Bytes(), writer.FormDataContentType()
}

// buildMultipartLogoRequest builds a POST /api/company-settings/logo
// request whose "logo" form field contains content, named filename.
func buildMultipartLogoRequest(t *testing.T, filename string, content []byte, token string) *http.Request {
	t.Helper()
	body, contentType := multipartLogoBody(t, filename, content)

	req := httptest.NewRequest(http.MethodPost, "/api/company-settings/logo", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

// newCompanySettingsRouter builds a router mounting only RequireAuth (no
// RequireRole) in front of CompanySettingsHandler, mirroring
// newDomainsRouter: RBAC for these routes is asserted at the routes.go
// wiring level (T8), not by the handler itself.
func newCompanySettingsRouter(t *testing.T) (http.Handler, *db.Pool, *db.AdminRepository) {
	t.Helper()
	pool, admins := newCompanySettingsTestPool(t)
	repo := db.NewCompanySettingsRepository(pool)
	return buildCompanySettingsRouter(admins, repo), pool, admins
}

// failingLogoStore wraps a real *db.CompanySettingsRepository, forcing
// UpdateLogo to fail while Get/Update still hit the real database - used
// by TestUploadLogo_PersistFailure_500NoLogoChange to force a persistence
// failure deterministically (SET-13), the same "force a dependency to
// fail" pattern integrations_handler_test.go's spyPollerRestarter uses,
// rather than relying on filesystem permissions now that the logo has no
// on-disk representation to break.
type failingLogoStore struct {
	*db.CompanySettingsRepository
}

func (s *failingLogoStore) UpdateLogo(ctx context.Context, contentType string, data []byte) (*db.CompanySettings, error) {
	return nil, errors.New("forced UpdateLogo failure for test")
}

func newCompanySettingsTestPool(t *testing.T) (*db.Pool, *db.AdminRepository) {
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
	// this package - reset it to a known state before and after each
	// test. That reset races internal/db's and internal/cli's own
	// company_settings tests across the separate concurrent processes
	// `go test ./...` runs them as, so take the shared advisory lock for
	// the duration of this test - see LockCompanySettings' doc comment.
	// Deliberately context.Background(), not the bounded `ctx` above,
	// which is canceled by the deferred cancel() as soon as this
	// function returns.
	dbtest.LockCompanySettings(t, context.Background(), dsn)
	reset := func() {
		_, _ = pool.Exec(context.Background(), "UPDATE company_settings SET name = '', contact_email = '', logo_data = NULL, logo_content_type = NULL WHERE id = 1")
	}
	reset()
	t.Cleanup(reset)

	admins := db.NewAdminRepository(pool)
	return pool, admins
}

func buildCompanySettingsRouter(admins *db.AdminRepository, store companySettingsStore) http.Handler {
	handler := NewCompanySettingsHandler(store, zap.NewNop())

	r := chi.NewRouter()
	r.Group(func(protected chi.Router) {
		protected.Use(RequireAuth(middlewareTestSecret, admins))
		protected.Get("/api/company-settings", handler.Get)
		protected.Patch("/api/company-settings", handler.Update)
		protected.Post("/api/company-settings/logo", handler.UploadLogo)
	})
	return r
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
// upload under 10 MB is persisted and responds 200 with the fixed
// "/uploads/logo" URL.
func TestUploadLogo_ValidPNG_200UpdatesLogoURL(t *testing.T) {
	r, pool, admins := newCompanySettingsRouter(t)
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
	if resp.LogoURL == nil || *resp.LogoURL != "/uploads/logo" {
		t.Fatalf("LogoURL = %v, want %q", resp.LogoURL, "/uploads/logo")
	}

	contentType, data, found, err := db.NewCompanySettingsRepository(pool).GetLogo(context.Background())
	if err != nil {
		t.Fatalf("GetLogo() returned unexpected error: %v", err)
	}
	if !found {
		t.Fatal("GetLogo() found = false, want true after a successful upload")
	}
	if contentType != "image/png" {
		t.Errorf("GetLogo() contentType = %q, want %q", contentType, "image/png")
	}
	if string(data) != string(pngSignatureBytes) {
		t.Errorf("GetLogo() data does not match the uploaded bytes")
	}

	getRec := getCompanySettings(t, r, token)
	var getResp companySettingsResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if getResp.LogoURL == nil || *getResp.LogoURL != "/uploads/logo" {
		t.Errorf("persisted LogoURL = %v, want %q", getResp.LogoURL, "/uploads/logo")
	}
}

// TestUploadLogo_ValidSVG_200UpdatesLogoURL asserts SET-07 for the other
// allowed MIME type: a valid SVG upload succeeds the same way a PNG does.
func TestUploadLogo_ValidSVG_200UpdatesLogoURL(t *testing.T) {
	r, pool, admins := newCompanySettingsRouter(t)
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
	if resp.LogoURL == nil || *resp.LogoURL != "/uploads/logo" {
		t.Fatalf("LogoURL = %v, want %q", resp.LogoURL, "/uploads/logo")
	}

	contentType, _, found, err := db.NewCompanySettingsRepository(pool).GetLogo(context.Background())
	if err != nil {
		t.Fatalf("GetLogo() returned unexpected error: %v", err)
	}
	if !found {
		t.Fatal("GetLogo() found = false, want true after a successful upload")
	}
	if contentType != "image/svg+xml" {
		t.Errorf("GetLogo() contentType = %q, want %q", contentType, "image/svg+xml")
	}
}

// specMaxLogoBytes pins spec.md's fixed 10 MB bound for SET-08 - the sizing
// tests below MUST derive their payloads from this literal, never from
// maxLogoBytes (the constant under test). A test sized off maxLogoBytes
// would stay green if that constant were ever widened or narrowed, which is
// exactly what it must catch.
const specMaxLogoBytes = 10 * 1024 * 1024

// TestUploadLogo_OverSizeLimit_422NoLogoURLChange asserts SET-08: a file
// over the spec's fixed 10 MB bound is rejected with 422 and the stored
// logo is left unchanged.
func TestUploadLogo_OverSizeLimit_422NoLogoURLChange(t *testing.T) {
	r, pool, admins := newCompanySettingsRouter(t)
	token := issueTestSessionToken(t, admins)

	oversized := make([]byte, specMaxLogoBytes+1024)
	copy(oversized, pngSignatureBytes)

	req := buildMultipartLogoRequest(t, "logo.png", oversized, token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}

	_, _, found, err := db.NewCompanySettingsRepository(pool).GetLogo(context.Background())
	if err != nil {
		t.Fatalf("GetLogo() returned unexpected error: %v", err)
	}
	if found {
		t.Error("GetLogo() found = true after a rejected oversized upload, want false (unchanged)")
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

// TestUploadLogo_JustUnderSizeLimit_200UpdatesLogoURL asserts the accepted
// side of SET-08's boundary: a request body one byte under the spec's fixed
// 10 MB bound must succeed, not merely "somewhere under maxLogoBytes". The
// file content is sized so that, once wrapped in multipart framing, the
// total request body lands at exactly specMaxLogoBytes-1.
func TestUploadLogo_JustUnderSizeLimit_200UpdatesLogoURL(t *testing.T) {
	r, _, admins := newCompanySettingsRouter(t)
	token := issueTestSessionToken(t, admins)

	emptyBody, _ := multipartLogoBody(t, "logo.png", nil)
	overhead := len(emptyBody)

	content := make([]byte, specMaxLogoBytes-1-overhead)
	copy(content, pngSignatureBytes)

	req := buildMultipartLogoRequest(t, "logo.png", content, token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp companySettingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if resp.LogoURL == nil || *resp.LogoURL != "/uploads/logo" {
		t.Fatalf("LogoURL = %v, want %q", resp.LogoURL, "/uploads/logo")
	}
}

// TestUploadLogo_PersistFailure_500NoLogoChange asserts SET-13: when
// persisting the logo fails (e.g. a database error), the handler responds
// 500 and the previously stored logo is left unchanged - a subsequent GET
// still returns the logo_url from the last successful upload. The failure
// is forced via failingLogoStore rather than a real disk/DB fault, since
// the logo has no on-disk representation left to break.
func TestUploadLogo_PersistFailure_500NoLogoChange(t *testing.T) {
	pool, admins := newCompanySettingsTestPool(t)
	realRepo := db.NewCompanySettingsRepository(pool)
	r := buildCompanySettingsRouter(admins, realRepo)
	token := issueTestSessionToken(t, admins)

	// Seed a known-good logo first so "unchanged" is a non-nil value - the
	// stronger form of the AC than proving it merely stayed null.
	seedReq := buildMultipartLogoRequest(t, "logo.png", pngSignatureBytes, token)
	seedRec := httptest.NewRecorder()
	r.ServeHTTP(seedRec, seedReq)
	if seedRec.Code != http.StatusOK {
		t.Fatalf("seed upload status = %d, want %d, body = %s", seedRec.Code, http.StatusOK, seedRec.Body.String())
	}

	failingRouter := buildCompanySettingsRouter(admins, &failingLogoStore{CompanySettingsRepository: realRepo})

	req := buildMultipartLogoRequest(t, "logo.svg", []byte(validSVGBody), token)
	rec := httptest.NewRecorder()
	failingRouter.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}

	getRec := getCompanySettings(t, r, token)
	var getResp companySettingsResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if getResp.LogoURL == nil || *getResp.LogoURL != "/uploads/logo" {
		t.Errorf("LogoURL after failed persist = %v, want unchanged %q", getResp.LogoURL, "/uploads/logo")
	}

	contentType, _, found, err := realRepo.GetLogo(context.Background())
	if err != nil {
		t.Fatalf("GetLogo() returned unexpected error: %v", err)
	}
	if !found || contentType != "image/png" {
		t.Errorf("GetLogo() contentType = %q, found = %v, want unchanged %q/true (the seeded PNG)", contentType, found, "image/png")
	}
}

// TestUploadLogo_MultipartFieldNameContract_LogoAccepted pins the
// cross-layer multipart field name contract: the frontend
// (web/src/features/settings/hooks.ts) hardcodes the literal "logo" as its
// FormData key, independently of the backend's logoFormFieldName constant.
// Every other test in this file goes through buildMultipartLogoRequest,
// which builds its part via logoFormFieldName - so it would stay green even
// if that constant were renamed, since both sides of the comparison move
// together. This test hardcodes the literal directly, so a rename of
// logoFormFieldName away from "logo" (breaking the real wire contract with
// the frontend) fails here.
func TestUploadLogo_MultipartFieldNameContract_LogoAccepted(t *testing.T) {
	r, _, admins := newCompanySettingsRouter(t)
	token := issueTestSessionToken(t, admins)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("logo", "logo.png")
	if err != nil {
		t.Fatalf("CreateFormFile() returned unexpected error: %v", err)
	}
	if _, err := part.Write(pngSignatureBytes); err != nil {
		t.Fatalf("part.Write() returned unexpected error: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/company-settings/logo", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

// TestUploadLogo_WrongMIMEType_422NoLogoURLChange asserts SET-09: a
// non-PNG/SVG payload is rejected with 422 and the previously stored logo
// (none, here) is left untouched.
func TestUploadLogo_WrongMIMEType_422NoLogoURLChange(t *testing.T) {
	r, _, admins := newCompanySettingsRouter(t)
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

// TestUploadLogo_SecondValidUpload_OverwritesFirst asserts SET-10: a second
// successful upload replaces the first - the served URL is still the same
// fixed "/uploads/logo" path, but its content type/bytes now reflect the
// second upload, never a mix of the two.
func TestUploadLogo_SecondValidUpload_OverwritesFirst(t *testing.T) {
	r, pool, admins := newCompanySettingsRouter(t)
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

	contentType, data, found, err := db.NewCompanySettingsRepository(pool).GetLogo(context.Background())
	if err != nil {
		t.Fatalf("GetLogo() returned unexpected error: %v", err)
	}
	if !found {
		t.Fatal("GetLogo() found = false, want true")
	}
	if contentType != "image/svg+xml" {
		t.Errorf("GetLogo() contentType = %q, want %q (the second upload)", contentType, "image/svg+xml")
	}
	if string(data) != validSVGBody {
		t.Errorf("GetLogo() data does not match the second upload's bytes")
	}

	getRec := getCompanySettings(t, r, token)
	var getResp companySettingsResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("json.Unmarshal() returned unexpected error: %v", err)
	}
	if getResp.LogoURL == nil || *getResp.LogoURL != "/uploads/logo" {
		t.Errorf("persisted LogoURL = %v, want %q", getResp.LogoURL, "/uploads/logo")
	}
}
