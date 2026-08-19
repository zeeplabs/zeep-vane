package router

import (
	"context"
	"net"
	"net/http"
)

// statusPageHostLookup is the subset of *db.StatusPageRepository HostRouter
// depends on.
type statusPageHostLookup interface {
	StateByHostname(ctx context.Context, hostname string) (string, error)
}

// HostRouter dispatches a request by its Host header: a hostname belonging
// to a published status page (StatusPage.State == "published") is routed to
// publicHandler; any other hostname - unregistered, or registered but not
// yet published - gets a 404. Admin API/SPA dispatch by Host is a design.md
// placeholder, not implemented here: the admin API is served on its own
// listener (see cmd/vane serve, router.New), which HostRouter does not
// touch.
func HostRouter(statusPages statusPageHostLookup, publicHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hostname := stripPort(r.Host)

		state, err := statusPages.StateByHostname(r.Context(), hostname)
		if err != nil || state != "published" {
			http.NotFound(w, r)
			return
		}

		publicHandler.ServeHTTP(w, r)
	})
}

// stripPort returns host without its ":port" suffix, if any.
func stripPort(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}
