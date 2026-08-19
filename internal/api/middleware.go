package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/zeeplabs/zeep-vane/internal/auth"
)

// contextKey namespaces values this package stores in request context.
type contextKey string

// adminIDContextKey is the request-context key under which RequireAuth
// stores the authenticated admin's ID.
const adminIDContextKey contextKey = "adminID"

// AdminIDFromContext returns the authenticated admin ID stored by
// RequireAuth, and whether one was present.
func AdminIDFromContext(ctx context.Context) (string, bool) {
	adminID, ok := ctx.Value(adminIDContextKey).(string)
	return adminID, ok
}

// RequireAuth builds middleware that rejects requests without a valid
// session token in the Authorization header ("Bearer <token>"), and stores
// the authenticated admin ID in the request context for handlers downstream.
func RequireAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := bearerToken(r.Header.Get("Authorization"))
			if token == "" {
				writeUnauthorized(w)
				return
			}

			adminID, err := auth.VerifySession(token, secret)
			if err != nil {
				writeUnauthorized(w)
				return
			}

			ctx := context.WithValue(r.Context(), adminIDContextKey, adminID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimPrefix(header, prefix)
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
}
