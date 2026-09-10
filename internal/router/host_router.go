package router

import (
	"context"
	"net"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// statusPageHostLookup is the subset of *db.StatusPageRepository HostRouter
// depends on. Unlike tls.HostPolicy (which only needs State to gate ACME),
// HostRouter needs the full row so it can thread StatusPage.ID down to the
// public handler for SP-15 scoping, and StatusPage.TenantID into the
// request's RLS session settings.
type statusPageHostLookup interface {
	GetByHostname(ctx context.Context, hostname string) (*db.StatusPage, error)
}

// tenantTxBeginner is the subset of *db.Pool HostRouter depends on to open
// the request's tenant-scoped transaction. Same mechanism the authenticated
// side uses (api.TenantContext) - "SET LOCAL app.tenant_id" via
// Pool.BeginTenantTx - so both paths satisfy the 0024 RLS policies the same
// way, rather than the public path growing a second, divergent one.
type tenantTxBeginner interface {
	BeginTenantTx(ctx context.Context, userID, tenantID string) (pgx.Tx, error)
}

// statusPageIDContextKey is the context key HostRouter uses to pass the
// resolved StatusPage.ID down to the public handler.
type statusPageIDContextKey struct{}

// tenantIDContextKey is the context key HostRouter uses to pass the
// resolved StatusPage's TenantID down to the public handler.
type tenantIDContextKey struct{}

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

// WithTenantID returns a copy of ctx carrying tenantID, retrievable via
// TenantIDFromContext.
func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantIDContextKey{}, tenantID)
}

// TenantIDFromContext returns the tenant id HostRouter resolved for the
// current (unauthenticated) request from its Host header, and whether one
// was present.
func TenantIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(tenantIDContextKey{}).(string)
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
//
// It also opens the request's tenant-scoped transaction (TENANT-01/02/03
// for the unauthenticated path). Every table the public handler reads is
// FORCE ROW LEVEL SECURITY with a fail-closed tenant_id policy since 0024,
// and an anonymous visitor carries no session to read an active tenant
// from - the resolved StatusPage's own tenant_id is the only tenant signal
// such a request has. Without setting it, a public request either resolves
// whatever tenant TenantRepository's single-tenant fallback happens to
// return (correct only because self-hosted has exactly one) or reads
// nothing at all under a non-superuser role. So: resolve by hostname, then
// SET LOCAL app.tenant_id to that page's tenant for the rest of the
// request, exactly like api.TenantContext does for authenticated traffic.
//
// The transaction is always rolled back, never committed: every route
// behind HostRouter is read-only (public status JSON, the logo file, the
// static SPA), so there is nothing to persist and no reason to give an
// unauthenticated request commit semantics.
func HostRouter(statusPages statusPageHostLookup, pool tenantTxBeginner, publicHandler http.Handler) http.Handler {
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

		// userID is "" - there is no authenticated user on this path, and
		// no policy the public handler's queries hit reads app.user_id.
		tx, err := pool.BeginTenantTx(r.Context(), "", statusPage.TenantID)
		if err != nil {
			// The database is unreachable or refusing transactions. There
			// is no cached response to fall back to at this layer, so
			// answer with a plain 503 rather than leaking the driver error
			// to a visitor.
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		defer func() { _ = tx.Rollback(r.Context()) }()

		ctx := WithStatusPageID(r.Context(), statusPage.ID)
		ctx = WithTenantID(ctx, statusPage.TenantID)
		ctx = db.WithTenantTx(ctx, tx)
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
