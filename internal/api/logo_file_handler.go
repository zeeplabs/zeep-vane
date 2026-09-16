package api

import (
	"context"
	"net/http"
)

// logoGetter is the subset of *db.TenantRepository the logo file handler
// depends on. This route is public - it has no session and therefore no
// active tenant of its own - so it reads through ActiveLogo, whose
// predicate honours app.tenant_id when one is set and otherwise falls back
// to the installation's single tenant (see
// TenantRepository.activeTenantPredicate).
//
// Which of those two applies depends on the listener, because this handler
// is mounted on both:
//
//   - On the public HTTPS listener (cmd/vane serve, newHTTPSServer) it sits
//     behind router.HostRouter, which resolves the visitor's Host header to
//     a published status page and sets app.tenant_id to that page's tenant
//     for the request's transaction. ActiveLogo then returns that tenant's
//     logo - correct under RLS and correct across tenants.
//   - On the admin HTTP listener (internal/cli/routes.go) it is mounted
//     unauthenticated, outside api.TenantContext, so the login screen's
//     <img> can load before a session exists. That request carries no
//     tenant signal at all: the admin domain is shared, not per-tenant, so
//     there is nothing to resolve from and the single-tenant fallback is
//     what answers. That is correct for self-hosted (one tenant by
//     definition) and is a known open question for a shared-admin-domain
//     SaaS deployment, where resolving the tenant needs a signal this route
//     does not have yet (an admin subdomain or slug). Deliberately not
//     guessed here.
type logoGetter interface {
	ActiveLogo(ctx context.Context) (contentType string, data []byte, found bool, err error)
}

// logoServeCSP is stricter than the general SecurityHeaders default:
// sandbox disables script execution, plugins, and form submission
// unconditionally, regardless of the served file's Content-Type. An
// uploaded .svg can contain <script> (SVG is XML, not a raster format) -
// the upload path's own content-type sniffing (logoContentTypeFor) only
// confirms a file *looks like* SVG, it doesn't sanitize what's inside one,
// so this response's own headers are the last line of defense against a
// malicious admin-uploaded logo executing script when rendered (M13). This
// is deliberately set here rather than relying on the general middleware,
// which allows same-origin script for the SPA itself - this route serves
// untrusted uploaded content, not vane's own code.
const logoServeCSP = "default-src 'none'; sandbox"

// NewLogoFileHandler builds the handler that serves the one stored logo
// back over HTTP with no authentication required (SET-12 - the public
// status page must render it unauthenticated). The logo lives in Postgres
// (tenants.logo_data), not on this replica's local disk - every replica
// reading the same database serves the same logo regardless of which one
// handled the upload (see TenantRepository.UpdateLogo's doc comment). A request when no logo has ever been uploaded gets a
// plain 404, never a directory listing or an empty 200.
func NewLogoFileHandler(logos logoGetter) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType, data, found, err := logos.ActiveLogo(r.Context())
		if err != nil || !found {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", contentType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", logoServeCSP)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	})
}
