//go:build integration

package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func newLogoFileHandlerRouter(t *testing.T) (http.Handler, string) {
	t.Helper()
	uploadsDir := t.TempDir()

	r := chi.NewRouter()
	r.Get("/uploads/{filename}", NewLogoFileHandler(uploadsDir).ServeHTTP)

	return r, uploadsDir
}

// TestLogoFileHandler_ExistingFile_200ServesBytes asserts the happy path:
// requesting the exact stored filename serves the file's bytes with 200.
func TestLogoFileHandler_ExistingFile_200ServesBytes(t *testing.T) {
	r, uploadsDir := newLogoFileHandlerRouter(t)
	if err := os.WriteFile(filepath.Join(uploadsDir, "logo.png"), []byte("fake-png-bytes"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/uploads/logo.png", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "fake-png-bytes" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "fake-png-bytes")
	}
}

// TestLogoFileHandler_PathTraversalFilename_404 asserts the edge case: a
// ".." filename segment never resolves outside uploadsDir - the handler
// rejects it directly (isSafeLogoFilename), rather than joining it into a
// path and letting the filesystem walk upward.
func TestLogoFileHandler_PathTraversalFilename_404(t *testing.T) {
	r, uploadsDir := newLogoFileHandlerRouter(t)

	// A real secret file outside uploadsDir, one level up, that a
	// traversal attempt would try to reach.
	secretDir := filepath.Dir(uploadsDir)
	secretPath := filepath.Join(secretDir, "secret.txt")
	if err := os.WriteFile(secretPath, []byte("should never be served"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(secretPath) })

	req := httptest.NewRequest(http.MethodGet, "/uploads/..", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

// TestLogoFileHandler_MissingFile_404 asserts the edge case from spec.md:
// a stored filename missing from disk (e.g. a misconfigured volume) 404s,
// the same way any missing static asset would.
func TestLogoFileHandler_MissingFile_404(t *testing.T) {
	r, _ := newLogoFileHandlerRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/uploads/logo.png", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// TestLogoFileHandler_NoAuthenticationRequired_200 asserts SET-12: the
// file is served with no session/Authorization header at all - this test
// never sets one, and mounts the handler with no RequireAuth/RequireRole
// in front of it, unlike every other admin route in this package.
func TestLogoFileHandler_NoAuthenticationRequired_200(t *testing.T) {
	r, uploadsDir := newLogoFileHandlerRouter(t)
	if err := os.WriteFile(filepath.Join(uploadsDir, "logo.svg"), []byte("<svg></svg>"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/uploads/logo.svg", nil)
	// Deliberately no Authorization header and no session cookie.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

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
	r, uploadsDir := newLogoFileHandlerRouter(t)
	if err := os.WriteFile(filepath.Join(uploadsDir, "logo.svg"), []byte("<svg><script>alert(1)</script></svg>"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() returned unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/uploads/logo.svg", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

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
