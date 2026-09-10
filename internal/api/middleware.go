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

// userIDContextKey is the request-context key under which RequireAuth
// stores the authenticated user's ID.
const userIDContextKey contextKey = "userID"

// userContextKey is the request-context key under which RequireAuth stores
// the full *db.User it loaded, so downstream middleware and handlers can
// read the identity without a second database round trip.
const userContextKey contextKey = "user"

// roleContextKey is the request-context key under which TenantContext
// stores the caller's role in the session's active tenant. Since
// multi-tenancy-core (AD-022) a role is held per tenant on
// tenant_memberships, not on the account, so it can only be resolved once
// the active tenant is known - which is why RequireAuth no longer carries
// it and RequireRole reads it from here.
const roleContextKey contextKey = "role"

// WithRole returns a copy of ctx carrying role, readable via
// RoleFromContext. TenantContext sets it; RequireRole enforces on it.
func WithRole(ctx context.Context, role string) context.Context {
	return context.WithValue(ctx, roleContextKey, role)
}

// RoleFromContext returns the role TenantContext resolved for the active
// tenant, and whether one was present. A caller with no active tenant
// selected has no role, and every role-gated route rejects them.
func RoleFromContext(ctx context.Context) (string, bool) {
	role, ok := ctx.Value(roleContextKey).(string)
	return role, ok
}

// activeTenantIDContextKey is the request-context key under which
// RequireAuth stores the session token's active tenant claim (""  if the
// session has none selected yet - multi-tenancy-core, AD-022). The
// tenant-context middleware reads this to set app.tenant_id for RLS.
const activeTenantIDContextKey contextKey = "activeTenantID"

// ActiveTenantIDFromContext returns the active tenant ID stored by
// RequireAuth (possibly "" - no tenant selected yet), and whether a session
// was present at all.
func ActiveTenantIDFromContext(ctx context.Context) (string, bool) {
	tenantID, ok := ctx.Value(activeTenantIDContextKey).(string)
	return tenantID, ok
}

// UserIDFromContext returns the authenticated user ID stored by
// RequireAuth, and whether one was present.
func UserIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(userIDContextKey).(string)
	return userID, ok
}

// UserFromContext returns the authenticated *db.User stored by
// RequireAuth, and whether one was present.
func UserFromContext(ctx context.Context) (*db.User, bool) {
	user, ok := ctx.Value(userContextKey).(*db.User)
	return user, ok
}

// userLoader is the subset of *db.UserRepository RequireAuth depends on.
type userLoader interface {
	GetByID(ctx context.Context, id string) (*db.User, error)
}

// RequireAuth builds middleware that rejects requests without a valid
// session token in the Authorization header ("Bearer <token>"). On success
// it loads the token's user from users (the current SessionsRevokedAt, not
// just what the JWT claims), rejects with 401 if the token was issued
// before that user's sessions were revoked, and stores the loaded *db.User
// (and its ID) in the request context. The caller's role is not resolved
// here - it belongs to a tenant, so TenantContext resolves it once the
// active tenant is known.
func RequireAuth(secret string, users userLoader) func(http.Handler) http.Handler {
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

			user, err := users.GetByID(r.Context(), claims.AdminID)
			if err != nil {
				writeUnauthorized(w)
				return
			}

			// claims.IssuedAt is a JWT NumericDate - second precision by
			// spec (RFC 7519), always floor()'d from the real issue instant.
			// SessionsRevokedAt is a Postgres timestamptz with microsecond
			// precision (L22). This asymmetry is fail-closed, never
			// fail-open: floor(issued) <= issued always, so this check can
			// only ever treat a token as revoked earlier than the real
			// comparison would (worst case, a token minted in the exact
			// same wall-clock second as a revocation event, and genuinely
			// after it, gets rejected once - it succeeds on retry a moment
			// later). It can never accept a token that was truly issued
			// before revocation, because floor(issued) can never round
			// forward past the real issue instant.
			if user.SessionsRevokedAt != nil && claims.IssuedAt.Before(*user.SessionsRevokedAt) {
				writeUnauthorized(w)
				return
			}

			ctx := context.WithValue(r.Context(), userIDContextKey, user.ID)
			ctx = context.WithValue(ctx, userContextKey, user)
			ctx = context.WithValue(ctx, activeTenantIDContextKey, claims.TenantID)
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
// role TenantContext resolved for the session's active tenant is one of
// roles. It must run after RequireAuth and TenantContext in the middleware
// chain - if no role is present in context (either middleware didn't run
// or rejected the request first, or the session has no active tenant and
// therefore no role anywhere), it rejects with 403 rather than assuming
// the request is authorized.
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		allowed[role] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, ok := RoleFromContext(r.Context())
			if !ok {
				writeForbidden(w)
				return
			}

			if _, ok := allowed[role]; !ok {
				writeForbidden(w)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
