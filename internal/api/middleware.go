package api

import (
	"context"
	"net/http"
	"strings"

	"go.uber.org/zap"

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

// sessionIDContextKey is the request-context key under which RequireAuth
// stores the session row id (the JWT's `sid` claim, resolved to the
// sessions-table row that backs this token). Downstream middleware and
// handlers - notably AuthHandler.Logout and AuthHandler.SwitchTenant -
// read this via SessionIDFromContext to revoke or reissue the row.
const sessionIDContextKey contextKey = "sessionID"

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

// SessionIDFromContext returns the session row id stored by RequireAuth,
// and whether one was present. Empty string with ok=false means
// RequireAuth was not in the middleware chain (or rejected the request
// before reaching this point).
func SessionIDFromContext(ctx context.Context) (string, bool) {
	sid, ok := ctx.Value(sessionIDContextKey).(string)
	return sid, ok
}

// userLoader is the subset of *db.UserRepository RequireAuth depends on.
type userLoader interface {
	GetByID(ctx context.Context, id string) (*db.User, error)
}

// sessionLoader is the subset of *db.SessionRepository RequireAuth depends
// on. The session row it returns (or its absence - ErrNotFound) is what
// backs every per-device revocation decision in this middleware.
type sessionLoader interface {
	GetByID(ctx context.Context, id string) (*db.Session, error)
	TouchLastSeen(ctx context.Context, id string) error
}

// RequireAuth builds middleware that rejects requests without a valid
// session token in the Authorization header ("Bearer <token>") or the
// `vane_session` cookie (admin-frontend I2/I3). On success:
//
//  1. JWT is parsed and verified (claims.SessionID, claims.TenantID, etc.)
//  2. users.GetByID(claims.AdminID) - rejects with 401 if the user is
//     gone, or if their SessionsRevokedAt global timestamp is later than
//     the token's IssuedAt (the existing admin-dashboard global-revoke
//     flow).
//  3. sessions.GetByID(claims.SessionID) - rejects with 401 if the row
//     is missing (token pre-dates user-sessions, or sid was tampered
//     with) or if the row's revoked_at is set (per-device revoke by
//     Logout, the "Encerrar" button on the redesigned Meu Perfil
//     screen, or RevokeAllForUser from admin events UpdateRole/Delete
//     per decision #1 of context.md).
//  4. sessions.TouchLastSeen(claims.SessionID) - fire-and-forget update
//     of last_seen_at, throttled to at most once per 5 minutes by the
//     repository. Failures are logged but never block the request (the
//     UI's "last active" signal is best-effort, not correctness-critical).
//
// On success the loaded *db.User, user.ID, active tenant, and session id
// are stored in the request context for downstream middleware/handlers.
// The caller's role is not resolved here - it belongs to a tenant, so
// TenantContext resolves it once the active tenant is known.
func RequireAuth(secret string, users userLoader, sessions sessionLoader, logger *zap.Logger) func(http.Handler) http.Handler {
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
			// precision. This asymmetry is fail-closed, never fail-open:
			// floor(issued) <= issued always, so this check can only ever
			// treat a token as revoked earlier than the real comparison
			// would (worst case, a token minted in the exact same wall-
			// clock second as a revocation event, and genuinely after it,
			// gets rejected once - it succeeds on retry a moment later).
			// It can never accept a token that was truly issued before
			// revocation, because floor(issued) can never round forward
			// past the real issue instant.
			if user.SessionsRevokedAt != nil && claims.IssuedAt.Before(*user.SessionsRevokedAt) {
				writeUnauthorized(w)
				return
			}

			// Per-device revocation check (user-sessions spec SESS-03).
			// claims.SessionID is guaranteed non-empty by
			// auth.VerifySessionClaims, so this lookup is by primary key
			// - cheap, but it IS a write path through TouchLastSeen below
			// (kept throttled so the write rate is bounded).
			sess, err := sessions.GetByID(r.Context(), claims.SessionID)
			if err != nil {
				// ErrNotFound here means either: (a) the token pre-dates
				// the user-sessions deploy and never had a real row, or
				// (b) someone tampered with the sid to a UUID nobody
				// owns. Both indistinguishable from RequireAuth's POV,
				// both 401.
				writeUnauthorized(w)
				return
			}
			if sess.RevokedAt.Valid {
				writeUnauthorized(w)
				return
			}

			// Fire-and-forget: TouchLastSeen updates last_seen_at
			// (throttled inside the repo to >=5min between writes).
			// Failures logged but never block the request - the UI's
			// "last active" signal is best-effort, not correctness-
			// critical (matching rate-limiter fail-open posture).
			if err := sessions.TouchLastSeen(r.Context(), sess.ID); err != nil {
				if logger != nil {
					logger.Warn("middleware: TouchLastSeen failed", zap.Error(err), zap.String("session_id", sess.ID))
				}
			}

			ctx := context.WithValue(r.Context(), userIDContextKey, user.ID)
			ctx = context.WithValue(ctx, userContextKey, user)
			ctx = context.WithValue(ctx, activeTenantIDContextKey, claims.TenantID)
			ctx = context.WithValue(ctx, sessionIDContextKey, sess.ID)
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
