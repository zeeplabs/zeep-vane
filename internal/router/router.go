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

// New builds the chi router with the routes vane serves.
func New(pool *db.Pool) http.Handler {
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
