package cli

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/api"
	"github.com/zeeplabs/zeep-vane/internal/audit"
	"github.com/zeeplabs/zeep-vane/internal/config"
	"github.com/zeeplabs/zeep-vane/internal/connectors/datadog"
	"github.com/zeeplabs/zeep-vane/internal/connectors/resend"
	"github.com/zeeplabs/zeep-vane/internal/connectors/sendgrid"
	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/email"
	"github.com/zeeplabs/zeep-vane/internal/ratelimit"
	"github.com/zeeplabs/zeep-vane/internal/router"
	"github.com/zeeplabs/zeep-vane/web"
)

// credentialRouteRateLimit/Burst/IdleTTL bound login, password-reset, and
// account-creation attempts per client IP (H10) - none of these routes had
// any limit before. 10/min with a burst of 10 tolerates a legitimate user
// mistyping a password a few times in a row without a lockout, while still
// bounding an automated guesser to a rate that makes brute-forcing a real
// password or token impractical.
const credentialRouteRateLimit = 10
const credentialRouteBurst = 10
const credentialRouteIdleTTL = 10 * time.Minute

// buildAdminRouter mounts the admin-facing API on top of router.New's base
// (/healthz): authentication, admin-dashboard admin management, and the
// mvp-core admin resources (domains, services, integrations, incidents,
// status pages), with role-based authorization wired in (admin-dashboard
// T12) - owner and operator may execute mvp-core's write routes, viewer is
// limited to its read routes, and admin management is restricted to owner.
// This assembly lives in internal/cli rather than internal/router because
// internal/api already imports internal/router (for HostRouter's status
// page context helper), and internal/router importing internal/api back
// would cycle.
func buildAdminRouter(pool *db.Pool, cfg config.Config, logger *zap.Logger, pollerManager *PollerManager) http.Handler {
	r := router.New(pool)

	admins := db.NewAdminRepository(pool)
	invites := db.NewAdminInviteRepository(pool)
	auditLog := audit.NewLog(pool)

	companySettingsRepo := db.NewCompanySettingsRepository(pool)

	// emailService is built with a ProviderFactory closure rather than a
	// direct import of internal/connectors/sendgrid|resend from
	// internal/email itself - the same function-typed dependency injection
	// pattern used above for the Datadog integration
	// (validateDatadogCredentials/searchDatadogSLOs), keeping internal/email
	// decoupled from which concrete connector packages exist (design.md).
	emailService, err := email.NewService(db.NewEmailProviderRepository(pool), emailProviderFactory, cfg.MasterKey, logger)
	if err != nil {
		// NewService only errors if the embedded admin-invite templates
		// fail to parse (fail-fast-at-boot, design.md) - that source is
		// compiled into the binary via go:embed, so this is a build-time
		// defect, not a runtime condition. Fatal rather than propagating
		// the error, since buildAdminRouter's signature is not error-return
		// (matches this package's existing boot-assembly style).
		logger.Fatal("cli: failed to build email service", zap.Error(err))
	}
	emailProvidersHandler := api.NewEmailProvidersHandler(emailService, logger)

	authHandler := api.NewAuthHandler(admins, logger, cfg.SessionSecret, cfg.SecureCookies)
	bootstrapHandler := api.NewBootstrapHandler(pool, admins, logger, cfg.SessionSecret, cfg.SecureCookies)
	passwordResetHandler := api.NewPasswordResetHandler(admins, db.NewPasswordResetRepository(pool), logger, cfg.DevTokenLogging)
	adminsHandler := api.NewAdminsHandler(pool, admins, invites, emailService, companySettingsRepo, auditLog, logger, cfg.DevTokenLogging, cfg.HTTPSEnabled, cfg.SessionSecret, cfg.SecureCookies)
	domainsHandler := api.NewDomainsHandler(db.NewDomainRepository(pool), logger)
	servicesHandler := api.NewServicesHandler(db.NewServiceRepository(pool), logger)
	integrationsHandler := api.NewIntegrationsHandler(db.NewIntegrationRepository(pool), validateDatadogCredentials, searchDatadogSLOs, pollerManager, cfg.MasterKey, logger)
	incidentsHandler := api.NewIncidentsHandler(db.NewIncidentRepository(pool), logger)
	statusPagesHandler := api.NewStatusPagesHandler(db.NewStatusPageRepository(pool), logger)
	pollerStatusHandler := api.NewPollerStatusHandler(db.NewIntegrationRepository(pool), logger)
	publicStatusHandler := api.NewPublicStatusHandler(db.NewServiceRepository(pool), db.NewStatusIntervalRepository(pool), db.NewIncidentRepository(pool), companySettingsRepo, logger)
	publicStatusPreviewHandler := api.NewPublicStatusPreviewHandler(db.NewStatusPageRepository(pool), publicStatusHandler, logger)
	companySettingsHandler := api.NewCompanySettingsHandler(companySettingsRepo, logger)
	logoFileHandler := api.NewLogoFileHandler(companySettingsRepo)
	instanceConfigHandler := api.NewInstanceConfigHandler(cfg.PublicDNSTarget, companySettingsRepo, logger)

	requireAuth := api.RequireAuth(cfg.SessionSecret, admins)
	writeRoles := api.RequireRole(db.RoleOwner, db.RoleOperator)
	anyRole := api.RequireRole(db.RoleOwner, db.RoleOperator, db.RoleViewer)
	ownerOnly := api.RequireRole(db.RoleOwner)

	// One shared limiter (not one per route) - login, password-reset, and
	// bootstrap/invite-accept are all the same threat (credential/token
	// guessing), so a single IP's budget is shared across all of them.
	// Otherwise an attacker could just spread guesses across routes to get
	// a multiple of the intended rate (H10).
	credentialLimiter := ratelimit.NewIPLimiter(pool, credentialRouteRateLimit, credentialRouteBurst, credentialRouteIdleTTL)

	// Public - no authentication.
	r.With(credentialLimiter.Middleware).Post("/api/auth/login", authHandler.Login)
	r.With(credentialLimiter.Middleware).Post("/api/auth/password-reset/request", passwordResetHandler.Request)
	r.With(credentialLimiter.Middleware).Post("/api/auth/password-reset/confirm", passwordResetHandler.Confirm)
	r.With(credentialLimiter.Middleware).Post("/api/admins/invite/{token}/accept", adminsHandler.AcceptInvite)

	// First-run bootstrap (SHD-14/SHD-15) - public and unauthenticated by
	// necessity: no authenticated caller can exist before the very first
	// admin does, same structural reason AcceptInvite above is public.
	r.Get("/api/bootstrap/status", bootstrapHandler.Status)
	r.With(credentialLimiter.Middleware).Post("/api/bootstrap", bootstrapHandler.Create)

	// Public branding (login screen + sidebar, both render without an
	// owner-role check) - deliberately not behind ownerOnly like
	// /api/company-settings; see Branding's own doc comment.
	r.Get("/api/instance/branding", instanceConfigHandler.Branding)

	// Public logo file serving (SET-12) - no authentication, so a public
	// status page's <img> can render it. Mounted here rather than in the
	// protected group below, mirroring the other unauthenticated routes
	// above.
	r.Get("/uploads/{filename}", logoFileHandler.ServeHTTP)

	r.Group(func(protected chi.Router) {
		protected.Use(requireAuth)

		protected.With(anyRole).Get("/api/auth/me", authHandler.Me)
		protected.With(anyRole).Post("/api/auth/logout", authHandler.Logout)

		// Admin management (admin-dashboard ADM-09) - owner only.
		protected.With(ownerOnly).Post("/api/admins", adminsHandler.Invite)
		protected.With(ownerOnly).Get("/api/admins", adminsHandler.List)
		protected.With(ownerOnly).Patch("/api/admins/{id}/role", adminsHandler.UpdateRole)
		protected.With(ownerOnly).Delete("/api/admins/{id}", adminsHandler.Delete)
		protected.With(ownerOnly).Post("/api/admins/invites/{id}/resend", adminsHandler.ResendInvite)
		protected.With(ownerOnly).Delete("/api/admins/invites/{id}", adminsHandler.CancelInvite)

		// Company settings (SET-02) - owner only, per SettingsPage.tsx's
		// own "Visível apenas para Owners" copy (spec.md Assumptions).
		protected.With(ownerOnly).Get("/api/company-settings", companySettingsHandler.Get)
		protected.With(ownerOnly).Patch("/api/company-settings", companySettingsHandler.Update)
		protected.With(ownerOnly).Post("/api/company-settings/logo", companySettingsHandler.UploadLogo)

		// mvp-core write routes - owner and operator (ADM-10).
		protected.With(writeRoles).Post("/api/domains", domainsHandler.Create)
		protected.With(writeRoles).Post("/api/services", servicesHandler.Create)
		protected.With(writeRoles).Post("/api/integrations/datadog", integrationsHandler.ConnectDatadog)
		protected.With(writeRoles).Post("/api/integrations/email/{provider}", emailProvidersHandler.Connect)
		protected.With(writeRoles).Post("/api/integrations/email/{provider}/activate", emailProvidersHandler.Activate)
		protected.With(writeRoles).Post("/api/incidents", incidentsHandler.Create)
		protected.With(writeRoles).Post("/api/incidents/{id}/updates", incidentsHandler.AddUpdate)
		protected.With(writeRoles).Patch("/api/incidents/{id}", incidentsHandler.Transition)
		protected.With(writeRoles).Post("/api/status-pages", statusPagesHandler.Create)
		protected.With(writeRoles).Patch("/api/status-pages/{id}/domain", statusPagesHandler.AttachDomain)
		protected.With(writeRoles).Patch("/api/status-pages/{id}/services", statusPagesHandler.SetServices)
		protected.With(writeRoles).Get("/api/instance/dns-target", instanceConfigHandler.DNSTarget)

		// mvp-core read routes and poller status (admin-dashboard ADM-13) -
		// owner, operator, and viewer (ADM-10, ADM-11).
		protected.With(anyRole).Get("/api/domains", domainsHandler.List)
		protected.With(anyRole).Get("/api/services", servicesHandler.List)
		protected.With(anyRole).Get("/api/status-pages", statusPagesHandler.List)
		protected.With(anyRole).Get("/api/incidents", incidentsHandler.List)
		protected.With(anyRole).Get("/api/incidents/{id}/updates", incidentsHandler.ListUpdates)
		protected.With(anyRole).Get("/api/status-pages/{id}/public-preview", publicStatusPreviewHandler.Get)
		protected.With(anyRole).Get("/api/integrations/datadog/status", integrationsHandler.Status)
		protected.With(anyRole).Get("/api/integrations/email", emailProvidersHandler.List)

		// SLO search decrypts the stored Datadog key pair server-side and
		// calls out to Datadog on the admin's behalf - part of the
		// connect/configure flow (I14), so it stays restricted to
		// owner/operator like the rest of that flow, unlike the read-only
		// status/list routes above which viewer can also reach.
		protected.With(writeRoles).Get("/api/integrations/datadog/slos", integrationsHandler.SearchSLOs)
		protected.With(anyRole).Get("/api/poller/status", pollerStatusHandler.List)
	})

	// Serves the embedded SPA (with client-route fallback) for any path
	// that matched no registered route above. chi dispatches NotFound
	// only when nothing more specific matched, so every /api/* route
	// registered above still resolves to its own handler first - this
	// never intercepts them (SHD-04, SHD-05).
	r.NotFound(web.StaticHandler().ServeHTTP)

	// Wraps the whole mux rather than r.Use(...): router.New already
	// registers /healthz before returning, and chi panics if Use is called
	// after any route is registered on the same mux. hsts=false - this is
	// the plain HTTP admin listener, not the TLS-terminating one (M14).
	return api.NewCORSMiddleware(cfg.CORSAllowedOrigin)(api.SecurityHeaders(false)(r))
}

// validateDatadogCredentials adapts datadog.Client.ValidateCredentials to
// the shape IntegrationsHandler depends on.
func validateDatadogCredentials(ctx context.Context, apiKey, appKey string) error {
	return datadog.NewClient(apiKey, appKey).ValidateCredentials(ctx)
}

// searchDatadogSLOs adapts datadog.Client.SearchSLOs to the shape
// IntegrationsHandler depends on (I14).
func searchDatadogSLOs(ctx context.Context, apiKey, appKey, query string) ([]datadog.SLOSummary, error) {
	return datadog.NewClient(apiKey, appKey).SearchSLOs(ctx, query)
}

// emailProviderFactory builds the concrete connector for provider,
// authenticated with apiKey. It is email.Service's only dependency on a
// concrete connector package - internal/email itself never imports
// internal/connectors/sendgrid or internal/connectors/resend directly
// (design.md's "breaking the potential import cycle" decision).
func emailProviderFactory(provider, apiKey string) (email.Provider, error) {
	switch provider {
	case "sendgrid":
		return sendgrid.NewClient(apiKey), nil
	case "resend":
		return resend.NewClient(apiKey), nil
	default:
		return nil, fmt.Errorf("cli: unknown email provider %q", provider)
	}
}
