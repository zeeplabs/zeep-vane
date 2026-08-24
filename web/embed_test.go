package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// discoverRealAssetPath finds a real, hashed asset filename that
// `npm run build` actually produced under web/dist/assets, since the exact
// filename changes across builds (Vite content-hashes it). This requires
// the one-time `cd web && npm install && npm run build` prerequisite this
// task's "What" section documents - same accepted convention zeep-orbit
// already has for its own embedded static/ directory.
func discoverRealAssetPath(t *testing.T) string {
	t.Helper()
	entries, err := os.ReadDir("dist/assets")
	if err != nil || len(entries) == 0 {
		t.Skip("web/dist/assets not built - run `cd web && npm install && npm run build` first")
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			return "/assets/" + entry.Name()
		}
	}
	t.Skip("no built asset files found under web/dist/assets")
	return ""
}

func TestStaticHandler_RealAsset_ServesExactFileWithContentType(t *testing.T) {
	assetPath := discoverRealAssetPath(t)
	wantBody, err := os.ReadFile(filepath.Join("dist", strings.TrimPrefix(assetPath, "/")))
	if err != nil {
		t.Fatalf("reading real asset from disk failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, assetPath, nil)
	rec := httptest.NewRecorder()
	StaticHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200", assetPath, rec.Code)
	}
	if rec.Body.String() != string(wantBody) {
		t.Errorf("GET %s body does not match the real asset's bytes on disk", assetPath)
	}

	ct := rec.Header().Get("Content-Type")
	wantSuffix := "javascript"
	if strings.HasSuffix(assetPath, ".css") {
		wantSuffix = "css"
	}
	if !strings.Contains(strings.ToLower(ct), wantSuffix) {
		t.Errorf("GET %s Content-Type = %q, want it to mention %q", assetPath, ct, wantSuffix)
	}
}

func TestStaticHandler_SPARoute_FallsBackToIndexHTML(t *testing.T) {
	wantBody, err := os.ReadFile("dist/index.html")
	if err != nil {
		t.Skip("web/dist/index.html not built - run `cd web && npm install && npm run build` first")
	}

	for _, route := range []string{"/services", "/bootstrap", "/"} {
		req := httptest.NewRequest(http.MethodGet, route, nil)
		rec := httptest.NewRecorder()
		StaticHandler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", route, rec.Code)
		}
		if rec.Body.String() != string(wantBody) {
			t.Errorf("GET %s body does not match dist/index.html", route)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "html") {
			t.Errorf("GET %s Content-Type = %q, want it to mention html", route, ct)
		}
	}
}

func TestStaticHandler_UnmatchedAPIPath_ReturnsJSON404NotIndexHTML(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/does-not-exist", nil)
	rec := httptest.NewRecorder()
	StaticHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "json") {
		t.Fatalf("Content-Type = %q, want application/json (never text/html)", ct)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not valid JSON (means it fell back to index.html): %v (body=%q)", err, rec.Body.String())
	}
	if body["error"] == "" {
		t.Error(`response body missing a non-empty "error" field`)
	}
}
