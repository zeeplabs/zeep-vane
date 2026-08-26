// Package web embeds the built SPA (web/dist) into the Go binary so a
// minimal runtime image (FROM scratch, no files on disk beyond the binary
// itself) can serve the admin UI from the same process as the admin API.
//
// The embedding file must live inside (or at) the directory being
// embedded - go:embed patterns cannot traverse "..". This mirrors why
// zeep-orbit's own embed.go lives next to its static/ directory instead of
// in a more conventional internal/ package.
package web

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

// distFS embeds every file npm run build produces under web/dist. A
// go:embed directory pattern fails to compile if the directory has no
// matching files, so web/dist/.gitkeep is committed to guarantee the
// pattern always has at least one file - even before the frontend has
// been built. The "all:" prefix is required because go:embed otherwise
// silently skips dotfiles, and .gitkeep would not count as a match.
// Run `make web-build` (or `npm run build` in web/) at least once
// before `go build`/`go test` on a fresh clone.
//
//go:embed all:dist
var distFS embed.FS

// StaticHandler serves the embedded SPA with client-side route fallback:
//
//   - A request path that resolves to a real embedded file is served
//     as-is, with its content type derived from its extension.
//   - Any other path is served the embedded index.html (SPA client-route
//     fallback), so a direct browser navigation/refresh on a route like
//     /services or /bootstrap still works.
//   - The one exception: a path starting with /api/ that matches no
//     embedded file gets a plain JSON 404 instead of index.html - an
//     unmatched API route must never look like an HTML page to an API
//     client.
func StaticHandler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// distFS is populated by //go:embed dist above - "dist" always
		// exists as a subtree of the embedded FS regardless of whether
		// the frontend was ever built. A failure here would mean the Go
		// toolchain itself is broken, not a runtime condition to recover
		// from, so this panics loudly at startup instead of serving
		// broken responses silently for the process's whole lifetime.
		panic("web: failed to sub embedded dist filesystem: " + err.Error())
	}

	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath := strings.TrimPrefix(r.URL.Path, "/")

		if requestPath != "" {
			if info, statErr := fs.Stat(sub, requestPath); statErr == nil && !info.IsDir() {
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeNotFoundJSON(w)
			return
		}

		serveIndex(w, sub)
	})
}

func serveIndex(w http.ResponseWriter, sub fs.FS) {
	index, err := sub.Open("index.html")
	if err != nil {
		// Only reachable if the frontend was never built (dist/index.html
		// missing from the embedded FS) - see the package doc's "must
		// build once" convention.
		writeNotFoundJSON(w)
		return
	}
	defer index.Close()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, index)
}

func writeNotFoundJSON(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`{"error":"not found"}`))
}
