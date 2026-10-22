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
	layout := emailLayoutFromConfig(cfg)
	if cfg.DeploymentMode == config.DeploymentModeSaaS {
		client := notificationservice.NewClient(cfg.NotificationServiceBaseURL, cfg.NotificationServiceAPIKey)
		sender, err := email.NewNotificationServiceSender(client, notificationServiceTenantKey, logger)
		if err != nil {
			return nil, err
		}
		sender.SetLayout(layout)
		return sender, nil
	}
	svc, err := email.NewService(db.NewEmailProviderRepository(pool), emailProviderFactory, cfg.MasterKey, logger)
	if err != nil {
		return nil, err
	}
	svc.SetLayout(layout)
	return svc, nil
}

// notificationPrefsPath is the admin SPA route hosting the Notifications
// preferences section (Meu Perfil).
const notificationPrefsPath = "/profile"

// emailLayoutFromConfig builds the deployment-wide email chrome every
// redesigned transactional email shares. The preferences link (also the
// digest's frequency/unsubscribe target) is only built when
// VANE_ADMIN_BASE_URL is set - never from a request Host header, same rule
// as every other emailed link (see cfg.AdminBaseURL).
func emailLayoutFromConfig(cfg config.Config) email.Layout {
	layout := email.Layout{
		AssetsBaseURL: cfg.EmailAssetsBaseURL,
	}
	if cfg.AdminBaseURL != "" {
		layout.NotificationPrefsURL = cfg.AdminBaseURL + notificationPrefsPath
	}
	return layout
}
