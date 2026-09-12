// Package notify resolves who should be notified about an event and sends the
// email, keeping that policy out of the HTTP handlers. It is the single place
// that maps "tenant + event" to a recipient list: the tenant's owner/operator
// members whose preference for that event is enabled.
package notify

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/email"
)

// IncidentSummary is the incident data an incident-opened/resolved email
// renders. Callers supply what they have; the templates omit empty optional
// fields.
type IncidentSummary struct {
	IncidentID  string
	Title       string
	Severity    string
	ServiceName string
	TenantName  string
}

// memberLister is the subset of *db.TenantMembershipRepository Service depends
// on.
type memberLister interface {
	ListMembersWithEmail(ctx context.Context, tenantID string) ([]db.TenantMember, error)
}

// preferenceResolver is the subset of *db.NotificationPreferenceRepository
// Service depends on.
type preferenceResolver interface {
	ResolveEnabledForUsers(ctx context.Context, userIDs []string, notificationType string) (map[string]bool, error)
}

// incidentEmailSender is the subset of *email.Service incident notifications
// need.
type incidentEmailSender interface {
	SendIncidentOpened(ctx context.Context, to string, data email.IncidentOpenedEmailData) error
	SendIncidentResolved(ctx context.Context, to string, data email.IncidentResolvedEmailData) error
}

// Service resolves recipients and sends incident notification emails.
type Service struct {
	members      memberLister
	preferences  preferenceResolver
	sender       incidentEmailSender
	adminBaseURL string
	logger       *zap.Logger
}

// NewService builds a Service. adminBaseURL is the dashboard base used to build
// the incident link; an empty value simply omits the link.
func NewService(members memberLister, preferences preferenceResolver, sender incidentEmailSender, adminBaseURL string, logger *zap.Logger) *Service {
	return &Service{members: members, preferences: preferences, sender: sender, adminBaseURL: adminBaseURL, logger: logger}
}

// NotifyIncidentOpened emails every owner/operator member of tenantID whose
// incident_opened preference resolves true.
func (s *Service) NotifyIncidentOpened(ctx context.Context, tenantID string, summary IncidentSummary) error {
	return s.notifyIncident(ctx, tenantID, db.NotificationTypeIncidentOpened, summary)
}

// NotifyIncidentResolved emails every owner/operator member of tenantID whose
// incident_resolved preference resolves true.
func (s *Service) NotifyIncidentResolved(ctx context.Context, tenantID string, summary IncidentSummary) error {
	return s.notifyIncident(ctx, tenantID, db.NotificationTypeIncidentResolved, summary)
}

func (s *Service) notifyIncident(ctx context.Context, tenantID, notificationType string, summary IncidentSummary) error {
	members, err := s.members.ListMembersWithEmail(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("notify: failed to list tenant members: %w", err)
	}

	recipients := make([]db.TenantMember, 0, len(members))
	userIDs := make([]string, 0, len(members))
	for _, m := range members {
		if m.Role != "owner" && m.Role != "operator" {
			continue
		}
		recipients = append(recipients, m)
		userIDs = append(userIDs, m.UserID)
	}
	if len(recipients) == 0 {
		return nil
	}

	enabled, err := s.preferences.ResolveEnabledForUsers(ctx, userIDs, notificationType)
	if err != nil {
		return fmt.Errorf("notify: failed to resolve notification preferences: %w", err)
	}

	dashboardURL := s.dashboardURL(summary.IncidentID)
	for _, m := range recipients {
		if !enabled[m.UserID] {
			continue
		}
		if err := s.send(ctx, m.Email, notificationType, summary, dashboardURL); err != nil {
			// One recipient's send failure never blocks the others or the
			// caller: log and continue (spec's non-fatal requirement).
			s.logger.Error("notify: failed to send incident notification",
				zap.String("notification_type", notificationType),
				zap.String("user_id", m.UserID),
				zap.Error(err),
			)
		}
	}
	return nil
}

func (s *Service) send(ctx context.Context, to, notificationType string, summary IncidentSummary, dashboardURL string) error {
	switch notificationType {
	case db.NotificationTypeIncidentOpened:
		return s.sender.SendIncidentOpened(ctx, to, email.IncidentOpenedEmailData{
			TenantName:    summary.TenantName,
			ServiceName:   summary.ServiceName,
			IncidentTitle: summary.Title,
			Severity:      summary.Severity,
			DashboardURL:  dashboardURL,
		})
	case db.NotificationTypeIncidentResolved:
		return s.sender.SendIncidentResolved(ctx, to, email.IncidentResolvedEmailData{
			TenantName:    summary.TenantName,
			ServiceName:   summary.ServiceName,
			IncidentTitle: summary.Title,
			Severity:      summary.Severity,
			DashboardURL:  dashboardURL,
		})
	default:
		return fmt.Errorf("notify: unknown notification type %q", notificationType)
	}
}

func (s *Service) dashboardURL(incidentID string) string {
	if s.adminBaseURL == "" || incidentID == "" {
		return ""
	}
	return fmt.Sprintf("%s/incidents/%s", strings.TrimRight(s.adminBaseURL, "/"), incidentID)
}
