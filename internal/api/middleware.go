package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/zeeplabs/zeep-vane/internal/auth"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

// contextKey namespaces values this package stores in request context.
type contextKey string

// adminIDContextKey is the request-context key under which RequireAuth
// stores the authenticated admin's ID.
const adminIDContextKey contextKey = "adminID"

// adminContextKey is the request-context key under which RequireAuth stores
// the full *db.Admin it loaded, so downstream middleware (RequireRole) and
// handlers can read Role without a second database round trip.
const adminContextKey contextKey = "admin"

// AdminIDFromContext returns the authenticated admin ID stored by
// RequireAuth, and whether one was present.
func AdminIDFromContext(ctx context.Context) (string, bool) {
	adminID, ok := ctx.Value(adminIDContextKey).(string)
	return adminID, ok
}

// AdminFromContext returns the authenticated *db.Admin stored by
// RequireAuth, and whether one was present.
func AdminFromContext(ctx context.Context) (*db.Admin, bool) {
	admin, ok := ctx.Value(adminContextKey).(*db.Admin)
	return admin, ok
}

// adminLoader is the subset of *db.AdminRepository RequireAuth depends on.
type adminLoader interface {
	GetByID(ctx context.Context, id string) (*db.Admin, error)
}

// RequireAuth builds middleware that rejects requests without a valid
// session token in the Authorization header ("Bearer <token>"). On success
// it loads the token's admin from admins (the current Role and
// SessionsRevokedAt, not just what the JWT claims), rejects with 401 if the
// token was issued before the admin's sessions were revoked, and stores the
// loaded *db.Admin (and its ID) in the request context for handlers and
// RequireRole downstream.
func RequireAuth(secret string, admins adminLoader) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := bearerToken(r.Header.Get("Authorization"))
			if token == "" {
				token = sessionCookieToken(r)
			}
			if token == "" {
				writeUnauthorized(w)
				return
			}

			claims, err := auth.VerifySessionClaims(token, secret)
			if err != nil {
				writeUnauthorized(w)
				return
			}

			admin, err := admins.GetByID(r.Context(), claims.AdminID)
			if err != nil {
				writeUnauthorized(w)
				return
			}

			if admin.SessionsRevokedAt != nil && claims.IssuedAt.Before(*admin.SessionsRevokedAt) {
				writeUnauthorized(w)
				return
			}

			ctx := context.WithValue(r.Context(), adminIDContextKey, admin.ID)
			ctx = context.WithValue(ctx, adminContextKey, admin)
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

// sessionCookieToken reads the vane_session cookie set by AuthHandler.Login
// (I2), returning "" if absent.
func sessionCookieToken(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
}

func writeForbidden(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"error":"forbidden"}`))
}

// RequireRole builds middleware that rejects a request with 403 unless the
// *db.Admin stored in context by RequireAuth has one of roles. It must run
// after RequireAuth in the middleware chain - if no Admin is present in
// context (RequireAuth didn't run, or rejected the request first), it
// rejects with 403 rather than assuming the request is authorized.
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		allowed[role] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			admin, ok := AdminFromContext(r.Context())
			if !ok {
				writeForbidden(w)
				return
			}

			if _, ok := allowed[admin.Role]; !ok {
				writeForbidden(w)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
