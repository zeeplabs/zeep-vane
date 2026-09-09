package api

import (
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

// TenantContext builds middleware that opens a transaction for the
// request's lifetime and sets the RLS session settings every tenant-scoped
// policy checks (0024): app.user_id, always, from the *db.Admin RequireAuth
// already loaded (tenant_memberships needs this independent of an active
// tenant - e.g. listing a user's own memberships before one is chosen), and
// app.tenant_id, from the session's active-tenant claim
// (ActiveTenantIDFromContext) - "" when the session has none selected yet,
// which every tenant-scoped policy treats as fail-closed (TENANT-03), never
// an error and never another tenant's rows.
//
// It must run after RequireAuth (it reads the admin and tenant claim
// RequireAuth stores in context) and ahead of every tenant-scoped route
// group. The transaction commits once the handler returns with a status
// under 500, or rolls back on a 5xx or a panic - a request that failed
// server-side should not persist partial writes made before the failure.
func TenantContext(pool *db.Pool, logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			admin, ok := AdminFromContext(r.Context())
			if !ok {
				// RequireAuth didn't run (or rejected the request first) -
				// mirrors RequireRole's own "no admin in context" handling.
				writeUnauthorized(w)
				return
			}
			tenantID, _ := ActiveTenantIDFromContext(r.Context())

			tx, err := pool.BeginTenantTx(r.Context(), admin.ID, tenantID)
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
