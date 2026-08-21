package cli

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/api"
	"github.com/zeeplabs/zeep-vane/internal/audit"
	"github.com/zeeplabs/zeep-vane/internal/config"
	"github.com/zeeplabs/zeep-vane/internal/connectors/datadog"
	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/router"
)

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
func buildAdminRouter(pool *db.Pool, cfg config.Config, logger *zap.Logger) http.Handler {
	r := router.New(pool)

	admins := db.NewAdminRepository(pool)
	invites := db.NewAdminInviteRepository(pool)
	auditLog := audit.NewLog(pool)

	authHandler := api.NewAuthHandler(admins, logger, cfg.SessionSecret)
	passwordResetHandler := api.NewPasswordResetHandler(admins, db.NewPasswordResetRepository(pool), logger)
	adminsHandler := api.NewAdminsHandler(pool, admins, invites, auditLog, logger)
	domainsHandler := api.NewDomainsHandler(db.NewDomainRepository(pool), logger)
	servicesHandler := api.NewServicesHandler(db.NewServiceRepository(pool), logger)
	integrationsHandler := api.NewIntegrationsHandler(db.NewIntegrationRepository(pool), validateDatadogCredentials, cfg.MasterKey, logger)
	incidentsHandler := api.NewIncidentsHandler(db.NewIncidentRepository(pool), logger)
	statusPagesHandler := api.NewStatusPagesHandler(db.NewStatusPageRepository(pool), logger)
	pollerStatusHandler := api.NewPollerStatusHandler(db.NewIntegrationRepository(pool), logger)

	requireAuth := api.RequireAuth(cfg.SessionSecret, admins)
	writeRoles := api.RequireRole(db.RoleOwner, db.RoleOperator)
	anyRole := api.RequireRole(db.RoleOwner, db.RoleOperator, db.RoleViewer)
	ownerOnly := api.RequireRole(db.RoleOwner)

	// Public - no authentication.
	r.Post("/api/auth/login", authHandler.Login)
	r.Post("/api/auth/password-reset/request", passwordResetHandler.Request)
	r.Post("/api/auth/password-reset/confirm", passwordResetHandler.Confirm)
	r.Post("/api/admins/invite/{token}/accept", adminsHandler.AcceptInvite)

	r.Group(func(protected chi.Router) {
		protected.Use(requireAuth)

		protected.With(anyRole).Get("/api/auth/me", authHandler.Me)
		protected.With(anyRole).Post("/api/auth/logout", authHandler.Logout)

		// Admin management (admin-dashboard ADM-09) - owner only.
		protected.With(ownerOnly).Post("/api/admins", adminsHandler.Invite)
		protected.With(ownerOnly).Get("/api/admins", adminsHandler.List)
		protected.With(ownerOnly).Patch("/api/admins/{id}/role", adminsHandler.UpdateRole)
		protected.With(ownerOnly).Delete("/api/admins/{id}", adminsHandler.Delete)

		// mvp-core write routes - owner and operator (ADM-10).
		protected.With(writeRoles).Post("/api/domains", domainsHandler.Create)
		protected.With(writeRoles).Post("/api/services", servicesHandler.Create)
		protected.With(writeRoles).Post("/api/integrations/datadog", integrationsHandler.ConnectDatadog)
		protected.With(writeRoles).Post("/api/incidents", incidentsHandler.Create)
		protected.With(writeRoles).Post("/api/incidents/{id}/updates", incidentsHandler.AddUpdate)
		protected.With(writeRoles).Patch("/api/incidents/{id}", incidentsHandler.Transition)
		protected.With(writeRoles).Post("/api/status-pages", statusPagesHandler.Create)

		// mvp-core read routes and poller status (admin-dashboard ADM-13) -
		// owner, operator, and viewer (ADM-10, ADM-11).
		protected.With(anyRole).Get("/api/domains", domainsHandler.List)
		protected.With(anyRole).Get("/api/services", servicesHandler.List)
		protected.With(anyRole).Get("/api/status-pages", statusPagesHandler.List)
		protected.With(anyRole).Get("/api/integrations/datadog/status", integrationsHandler.Status)
		protected.With(anyRole).Get("/api/poller/status", pollerStatusHandler.List)
	})

	// Wraps the whole mux rather than r.Use(...): router.New already
	// registers /healthz before returning, and chi panics if Use is called
	// after any route is registered on the same mux.
	return api.NewCORSMiddleware(cfg.CORSAllowedOrigin)(r)
}

// validateDatadogCredentials adapts datadog.Client.ValidateCredentials to
// the shape IntegrationsHandler depends on.
func validateDatadogCredentials(ctx context.Context, apiKey, appKey string) error {
	return datadog.NewClient(apiKey, appKey).ValidateCredentials(ctx)
}
