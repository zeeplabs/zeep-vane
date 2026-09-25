package cli

import (
	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/config"
	"github.com/zeeplabs/zeep-vane/internal/connectors/notificationservice"
	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/email"
)

// notificationServiceTenantKey is the single TenantKey every request to
// zeep-notification-service carries: tenants of that service isolate
// products/clients of Zeep itself, not each individual company hosted by
// Vane's SaaS deployment (AD-034, design.md's Tech Decisions).
const notificationServiceTenantKey = "vane-saas"

// newEmailSender is the single point of decision between vane's two
// email.Sender implementations (AD-034): self-hosted (per-tenant
// email_providers, unchanged) or saas (zeep-notification-service, a single
// platform-wide credential). Replaces the email.NewService(...) call that
// used to be duplicated between buildAdminRouter and newNotifyService.
func newEmailSender(cfg config.Config, pool *db.Pool, logger *zap.Logger) (email.Sender, error) {
	if cfg.DeploymentMode == config.DeploymentModeSaaS {
		client := notificationservice.NewClient(cfg.NotificationServiceBaseURL, cfg.NotificationServiceAPIKey)
		return email.NewNotificationServiceSender(client, notificationServiceTenantKey, logger)
	}
	return email.NewService(db.NewEmailProviderRepository(pool), emailProviderFactory, cfg.MasterKey, logger)
}
