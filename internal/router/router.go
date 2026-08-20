// Package router builds vane's HTTP router.
package router

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

// pinger is the subset of *db.Pool that healthzHandler depends on, so tests
// can exercise both a reachable and an unreachable Postgres without needing
// two full pools' worth of setup beyond what db.Pool already provides.
type pinger interface {
	Ping(ctx context.Context) error
}

const healthzPingTimeout = 3 * time.Second

// New builds the base chi router with /healthz. It returns chi.Router
// (not just http.Handler) so a caller that also depends on internal/api -
// which internal/router cannot import back without a cycle, since
// internal/api already depends on internal/router for HostRouter's status
// page context helper - can mount further routes onto the same router
// (see internal/cli's serve command, which wires the admin-dashboard and
// mvp-core admin routes on top of this base).
func New(pool *db.Pool) chi.Router {
	r := chi.NewRouter()
	r.Get("/healthz", healthzHandler(pool))
	return r
}

func healthzHandler(pool pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), healthzPingTimeout)
		defer cancel()

		w.Header().Set("Content-Type", "application/json")

		if err := pool.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"down"}`))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}
}
