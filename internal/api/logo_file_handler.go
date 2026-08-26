package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

// logoFilePathPrefix is the path this handler is mounted under on both
// listeners (design.md) - used as a fallback when no chi URL param is
// available (the public listener mounts this handler on a plain
// http.ServeMux, outside chi).
const logoFilePathPrefix = "/uploads/"

// logoServeCSP is stricter than the general SecurityHeaders default:
// sandbox disables script execution, plugins, and form submission
// unconditionally, regardless of the served file's Content-Type. An
// uploaded .svg can contain <script> (SVG is XML, not a raster format) -
// the upload path's own content-type sniffing (logoExtensionFor) only
// confirms a file *looks like* SVG, it doesn't sanitize what's inside one,
// so this response's own headers are the last line of defense against a
// malicious admin-uploaded logo executing script when rendered (M13). This
// is deliberately set here rather than relying on the general middleware,
// which allows same-origin script for the SPA itself - this route serves
// untrusted uploaded content, not vane's own code.
const logoServeCSP = "default-src 'none'; sandbox"

// NewLogoFileHandler builds the handler that serves the one stored logo
// file back over HTTP with no authentication required (SET-12 - the
// public status page must render it unauthenticated). It rejects any
// filename containing a path separator or a "."/".." segment before
// joining it with uploadsDir, so a request can never resolve outside that
// directory; anything but the exact stored filename - a path-traversal
// attempt, or a filename that simply doesn't exist on disk - responds 404,
// never a directory listing.
func NewLogoFileHandler(uploadsDir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		filename := chi.URLParam(r, "filename")
		if filename == "" {
			filename = strings.TrimPrefix(r.URL.Path, logoFilePathPrefix)
		}

		if !isSafeLogoFilename(filename) {
			http.NotFound(w, r)
			return
		}

		fullPath := filepath.Join(uploadsDir, filename)
		if info, err := os.Stat(fullPath); err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", logoServeCSP)
		http.ServeFile(w, r, fullPath)
	})
}

// isSafeLogoFilename reports whether filename is a single path segment -
// no "/" or "\", and not "." or ".." - so joining it with uploadsDir can
// never escape that directory.
func isSafeLogoFilename(filename string) bool {
	if filename == "" || filename == "." || filename == ".." {
		return false
	}
	return !strings.ContainsAny(filename, "/\\")
}
