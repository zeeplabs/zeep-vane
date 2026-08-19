package router

import (
	"context"
	"net"
	"net/http"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// statusPageHostLookup is the subset of *db.StatusPageRepository HostRouter
// depends on. Unlike tls.HostPolicy (which only needs State to gate ACME),
// HostRouter needs the full row so it can thread StatusPage.ID down to the
// public handler for SP-15 scoping.
type statusPageHostLookup interface {
	GetByHostname(ctx context.Context, hostname string) (*db.StatusPage, error)
}

// statusPageIDContextKey is the context key HostRouter uses to pass the
// resolved StatusPage.ID down to the public handler.
type statusPageIDContextKey struct{}

// WithStatusPageID returns a copy of ctx carrying statusPageID, retrievable
// via StatusPageIDFromContext.
func WithStatusPageID(ctx context.Context, statusPageID string) context.Context {
	return context.WithValue(ctx, statusPageIDContextKey{}, statusPageID)
}

// StatusPageIDFromContext returns the StatusPage.ID HostRouter resolved for
// the current request, and whether one was present.
func StatusPageIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(statusPageIDContextKey{}).(string)
	return id, ok
}

// HostRouter dispatches a request by its Host header: a hostname belonging
// to a published status page (StatusPage.State == "published") is routed to
// publicHandler, with that StatusPage's ID attached to the request context
// (SP-15 - lets publicHandler scope its services/incidents queries to this
// status page instead of returning every row in the installation); any
// other hostname - unregistered, or registered but not yet published - gets
// a 404. Admin API/SPA dispatch by Host is a design.md placeholder, not
// implemented here: the admin API is served on its own listener (see
// cmd/vane serve, router.New), which HostRouter does not touch.
func HostRouter(statusPages statusPageHostLookup, publicHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hostname := stripPort(r.Host)

		statusPage, err := statusPages.GetByHostname(r.Context(), hostname)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if statusPage.State != "published" {
			http.NotFound(w, r)
			return
		}

		ctx := WithStatusPageID(r.Context(), statusPage.ID)
		publicHandler.ServeHTTP(w, r.WithContext(ctx))
	})
}

// stripPort returns host without its ":port" suffix, if any.
func stripPort(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}
