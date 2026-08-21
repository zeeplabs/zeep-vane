package api

import (
	"net/http"

	"github.com/go-chi/cors"
)

// corsAllowedMethods and corsAllowedHeaders cover what apiClient.ts's fetch
// calls need: JSON bodies over GET/POST/PATCH/DELETE, credentialed
// requests.
var (
	corsAllowedMethods = []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete, http.MethodOptions}
	corsAllowedHeaders = []string{"Content-Type", "Authorization"}
)

// NewCORSMiddleware builds CORS middleware restricted to a single allowed
// origin (the Vite dev server, or whatever origin serves the SPA),
// permitting credentialed requests (cookies) from it. AllowedOrigins is
// deliberately a single concrete origin, never "*" - go-chi/cors rejects
// combining a wildcard origin with AllowCredentials, but this is
// exercised explicitly in cors_test.go rather than left to that implicit
// library behavior (AF-N/A, see I5 in tasks.md).
func NewCORSMiddleware(allowedOrigin string) func(http.Handler) http.Handler {
	return cors.Handler(corsOptions(allowedOrigin))
}

// corsOptions builds the cors.Options NewCORSMiddleware wraps, split out so
// the wildcard-vs-credentials invariant can be asserted directly on the
// struct in cors_test.go, not just inferred from library behavior.
func corsOptions(allowedOrigin string) cors.Options {
	return cors.Options{
		AllowedOrigins:   []string{allowedOrigin},
		AllowedMethods:   corsAllowedMethods,
		AllowedHeaders:   corsAllowedHeaders,
		AllowCredentials: true,
	}
}
