//go:build integration

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/dbtest"
)

// newLogoFileHandlerTestPool returns a fresh *db.Pool against
// TEST_DATABASE_URL, migrated and with the shared company_settings
// singleton row locked/reset for the duration of the calling test - the
// logo now lives in that row, not on a per-test temp dir, so every test
// here races internal/db's and internal/cli's own company_settings tests
// the same way company_settings_handler_test.go's does.
func newLogoFileHandlerTestPool(t *testing.T) *db.Pool {
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

	dbtest.LockCompanySettings(t, context.Background(), dsn)
	reset := func() {
		_, _ = pool.Exec(context.Background(), "UPDATE company_settings SET logo_data = NULL, logo_content_type = NULL WHERE id = 1")
	}
	reset()
	t.Cleanup(reset)

	return pool
}

func getLogoFile(t *testing.T, r http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/uploads/logo", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestLogoFileHandler_LogoStored_200ServesBytes asserts the happy path: a
// logo persisted via CompanySettingsRepository.UpdateLogo is served back
// with its stored content type and bytes.
func TestLogoFileHandler_LogoStored_200ServesBytes(t *testing.T) {
	pool := newLogoFileHandlerTestPool(t)
	repo := db.NewCompanySettingsRepository(pool)
	if _, err := repo.UpdateLogo(context.Background(), "image/png", []byte("fake-png-bytes")); err != nil {
		t.Fatalf("UpdateLogo() returned unexpected error: %v", err)
	}

	rec := getLogoFile(t, NewLogoFileHandler(repo))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "fake-png-bytes" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "fake-png-bytes")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want %q", ct, "image/png")
	}
}

// TestLogoFileHandler_NeverUploaded_404 asserts the edge case: a fresh
// install (no logo ever uploaded) 404s rather than serving an empty body.
func TestLogoFileHandler_NeverUploaded_404(t *testing.T) {
	pool := newLogoFileHandlerTestPool(t)
	repo := db.NewCompanySettingsRepository(pool)

	rec := getLogoFile(t, NewLogoFileHandler(repo))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// TestLogoFileHandler_NoAuthenticationRequired_200 asserts SET-12: the
// logo is served with no session/Authorization header at all - this test
// never sets one, and mounts the handler with no RequireAuth/RequireRole
// in front of it, unlike every other admin route in this package.
func TestLogoFileHandler_NoAuthenticationRequired_200(t *testing.T) {
	pool := newLogoFileHandlerTestPool(t)
	repo := db.NewCompanySettingsRepository(pool)
	if _, err := repo.UpdateLogo(context.Background(), "image/svg+xml", []byte("<svg></svg>")); err != nil {
		t.Fatalf("UpdateLogo() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/uploads/logo", nil)
	// Deliberately no Authorization header and no session cookie.
	rec := httptest.NewRecorder()
	NewLogoFileHandler(repo).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

// TestLogoFileHandler_SVGFile_SandboxedCSPAndNosniff is the M13 regression
// guard: an uploaded .svg (SVG is XML - it can contain <script>) must never
// be served in a way a browser would execute script from, even if the
// upload path's own content sniffing was fooled into accepting a malicious
// file. The response's own CSP sandbox directive is the last line of
// defense, independent of Content-Type.
func TestLogoFileHandler_SVGFile_SandboxedCSPAndNosniff(t *testing.T) {
	pool := newLogoFileHandlerTestPool(t)
	repo := db.NewCompanySettingsRepository(pool)
	if _, err := repo.UpdateLogo(context.Background(), "image/svg+xml", []byte("<svg><script>alert(1)</script></svg>")); err != nil {
		t.Fatalf("UpdateLogo() returned unexpected error: %v", err)
	}

	rec := getLogoFile(t, NewLogoFileHandler(repo))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "sandbox") {
		t.Errorf("Content-Security-Policy = %q, want it to contain %q", csp, "sandbox")
	}
	if !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("Content-Security-Policy = %q, want it to contain %q", csp, "default-src 'none'")
	}
}

// TestLogoFileHandler_MultiReplica_SecondReplicaServesFirstReplicasUpload
// is the actual multi-replica regression guard this whole redesign exists
// for: two independent *db.Pool/handler pairs - standing in for two
// separate `vane serve` processes/containers with no shared filesystem
// between them - both read the one Postgres database. A logo persisted
// through "replica" A must be servable through "replica" B without either
// process needing to see the other's local disk, since there no longer is
// one: the old design (a file under UPLOADS_DIR) would 404 on B here,
// because B's local disk never received A's upload.
func TestLogoFileHandler_MultiReplica_SecondReplicaServesFirstReplicasUpload(t *testing.T) {
	poolA := newLogoFileHandlerTestPool(t)
	dsn := testDatabaseURL(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	poolB, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() for replica B returned unexpected error: %v", err)
	}
	t.Cleanup(poolB.Close)

	replicaA := db.NewCompanySettingsRepository(poolA)
	replicaB := db.NewCompanySettingsRepository(poolB)

	if _, err := replicaA.UpdateLogo(context.Background(), "image/png", []byte("uploaded-via-replica-a")); err != nil {
		t.Fatalf("replica A UpdateLogo() returned unexpected error: %v", err)
	}

	rec := getLogoFile(t, NewLogoFileHandler(replicaB))

	if rec.Code != http.StatusOK {
		t.Fatalf("replica B status = %d, want %d - a logo uploaded through replica A must be servable through replica B", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "uploaded-via-replica-a" {
		t.Errorf("replica B body = %q, want %q", rec.Body.String(), "uploaded-via-replica-a")
	}
}
