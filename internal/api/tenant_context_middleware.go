package api

import (
	"context"
	"errors"
	"net/http"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// statusCapturingWriter wraps http.ResponseWriter to record the status code
// the handler wrote, so TenantContext can decide whether to commit or roll
// back the request's transaction after the handler returns. Defaults to
// 200 if the handler never calls WriteHeader explicitly (matches
// net/http's own default).
type statusCapturingWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusCapturingWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// roleResolver is the subset of *db.TenantMembershipRepository
// TenantContext depends on to resolve the caller's role in the active
// tenant.
type roleResolver interface {
	GetRole(ctx context.Context, userID, tenantID string) (string, error)
}

// TenantContext builds middleware that opens a transaction for the
// request's lifetime and sets the RLS session settings every tenant-scoped
// policy checks (0024): app.user_id, always, from the *db.User RequireAuth
// already loaded (tenant_memberships needs this independent of an active
// tenant - e.g. listing a user's own memberships before one is chosen), and
// app.tenant_id, from the session's active-tenant claim
// (ActiveTenantIDFromContext) - "" when the session has none selected yet,
// which every tenant-scoped policy treats as fail-closed (TENANT-03), never
// an error and never another tenant's rows.
//
// It also resolves the caller's effective role for that tenant and stores
// it in the request context for RequireRole. Since multi-tenancy-core a
// role lives on tenant_memberships, so it exists only relative to an
// active tenant: a session with none selected yet carries no role, and
// every role-gated route rejects it (routes that only need authentication,
// like /api/auth/me and switch-tenant, are mounted without RequireRole for
// exactly that reason).
//
// It must run after RequireAuth (it reads the user and tenant claim
// RequireAuth stores in context) and ahead of every tenant-scoped route
// group. The transaction commits once the handler returns with a status
// under 500, or rolls back on a 5xx or a panic - a request that failed
// server-side should not persist partial writes made before the failure.
func TenantContext(pool *db.Pool, memberships roleResolver, logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := UserFromContext(r.Context())
			if !ok {
				// RequireAuth didn't run (or rejected the request first) -
				// mirrors RequireRole's own "no role in context" handling.
				writeUnauthorized(w)
				return
			}
			tenantID, _ := ActiveTenantIDFromContext(r.Context())

			tx, err := pool.BeginTenantTx(r.Context(), user.ID, tenantID)
			if err != nil {
				logger.Error("tenant_context: failed to begin tenant transaction", zap.Error(err))
				writeInternalError(w)
				return
			}

			committed := false
			defer func() {
				if !committed {
					_ = tx.Rollback(r.Context())
				}
			}()

			ctx := db.WithTenantTx(r.Context(), tx)

			// A missing membership is not an error: it is the fail-closed
			// case (no active tenant selected, or the caller was removed
			// from it). It yields the empty role, which RequireRole
			// rejects.
			role := ""
			if tenantID != "" {
				resolved, err := memberships.GetRole(ctx, user.ID, tenantID)
				switch {
				case err == nil:
					role = resolved
				case errors.Is(err, db.ErrNotFound):
					role = ""
				default:
					logger.Error("tenant_context: failed to resolve membership role", zap.Error(err))
					writeInternalError(w)
					return
				}
			}
			ctx = WithRole(ctx, role)

			rec := &statusCapturingWriter{ResponseWriter: w, status: http.StatusOK}

			func() {
				defer func() {
					// A panicking handler must never commit - roll back
					// (the deferred rollback above still fires since
					// committed stays false) and let the panic continue
					// unwinding to whatever top-level recoverer this
					// server already has.
					if p := recover(); p != nil {
						panic(p)
					}
				}()
				next.ServeHTTP(rec, r.WithContext(ctx))
			}()

			if rec.status >= http.StatusInternalServerError {
				return
			}
			if err := tx.Commit(r.Context()); err != nil {
				logger.Error("tenant_context: failed to commit tenant transaction", zap.Error(err))
				return
			}
			committed = true
		})
	}
}
