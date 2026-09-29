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

// DomainExpiringSummary is the data a domain-expiring email renders
// (domain-health-monitoring DHM-02).
type DomainExpiringSummary struct {
	DomainID      string
	Hostname      string
	DaysRemaining int
	ThresholdDays int
	TenantName    string
}

// DomainNSDriftSummary is the data a domain NS-drift email renders
// (domain-health-monitoring DHM-06).
type DomainNSDriftSummary struct {
	DomainID   string
	Hostname   string
	ExpectedNS []string
	CurrentNS  []string
	TenantName string
}

// notificationPayload carries exactly one summary, the one matching the
// dispatched notificationType. Only that field is populated by the caller.
type notificationPayload struct {
	incident       IncidentSummary
	domainExpiring DomainExpiringSummary
	domainNSDrift  DomainNSDriftSummary
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

// emailSender is the subset of *email.Service notifications need.
type emailSender interface {
	SendIncidentOpened(ctx context.Context, to string, data email.IncidentOpenedEmailData) error
	SendIncidentResolved(ctx context.Context, to string, data email.IncidentResolvedEmailData) error
	SendWeeklyDigest(ctx context.Context, to string, data email.WeeklyDigestEmailData) error
	SendDomainExpiring(ctx context.Context, to string, data email.DomainExpiringEmailData) error
	SendDomainNSDrift(ctx context.Context, to string, data email.DomainNSDriftEmailData) error
}

// Service resolves recipients and sends notification emails.
type Service struct {
	members      memberLister
	preferences  preferenceResolver
	sender       emailSender
	adminBaseURL string
	logger       *zap.Logger
}

// NewService builds a Service. adminBaseURL is the dashboard base used to build
// the incident link; an empty value simply omits the link.
func NewService(members memberLister, preferences preferenceResolver, sender emailSender, adminBaseURL string, logger *zap.Logger) *Service {
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

// NotifyDomainExpiring emails every owner/operator member of tenantID whose
// domain_expiring preference resolves true (domain-health-monitoring DHM-02).
func (s *Service) NotifyDomainExpiring(ctx context.Context, tenantID string, summary DomainExpiringSummary) error {
	return s.notify(ctx, tenantID, db.NotificationTypeDomainExpiring,
		notificationPayload{domainExpiring: summary}, s.domainDashboardURL(summary.DomainID))
}

// NotifyDomainNSDrift emails every owner/operator member of tenantID whose
// domain_ns_drift preference resolves true (domain-health-monitoring DHM-06).
func (s *Service) NotifyDomainNSDrift(ctx context.Context, tenantID string, summary DomainNSDriftSummary) error {
	return s.notify(ctx, tenantID, db.NotificationTypeDomainNSDrift,
		notificationPayload{domainNSDrift: summary}, s.domainDashboardURL(summary.DomainID))
}

func (s *Service) notifyIncident(ctx context.Context, tenantID, notificationType string, summary IncidentSummary) error {
	return s.notify(ctx, tenantID, notificationType,
		notificationPayload{incident: summary}, s.dashboardURL(summary.IncidentID))
}

// notify resolves the tenant's enabled owner/operator recipients for
// notificationType and sends each one the matching payload. One recipient's
// send failure never blocks the others or the caller: log and continue.
func (s *Service) notify(ctx context.Context, tenantID, notificationType string, payload notificationPayload, dashboardURL string) error {
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

	for _, m := range recipients {
		if !enabled[m.UserID] {
			continue
		}
		if err := s.send(ctx, m.Email, notificationType, payload, dashboardURL); err != nil {
			s.logger.Error("notify: failed to send notification",
				zap.String("notification_type", notificationType),
				zap.String("user_id", m.UserID),
				zap.Error(err),
			)
		}
	}
	return nil
}

// WeeklyDigestRecipients returns the tenant's owner/operator members whose
// weekly_digest preference resolves true - the recipient list the digest
// scheduler sends one email to per member.
func (s *Service) WeeklyDigestRecipients(ctx context.Context, tenantID string) ([]db.TenantMember, error) {
	members, err := s.members.ListMembersWithEmail(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("notify: failed to list tenant members: %w", err)
	}

	candidates := make([]db.TenantMember, 0, len(members))
	userIDs := make([]string, 0, len(members))
	for _, m := range members {
		if m.Role != "owner" && m.Role != "operator" {
			continue
		}
		candidates = append(candidates, m)
		userIDs = append(userIDs, m.UserID)
	}
	if len(candidates) == 0 {
		return candidates, nil
	}

	enabled, err := s.preferences.ResolveEnabledForUsers(ctx, userIDs, db.NotificationTypeWeeklyDigest)
	if err != nil {
		return nil, fmt.Errorf("notify: failed to resolve notification preferences: %w", err)
	}

	recipients := make([]db.TenantMember, 0, len(candidates))
	for _, m := range candidates {
		if enabled[m.UserID] {
			recipients = append(recipients, m)
		}
	}
	return recipients, nil
}

// SendWeeklyDigest sends the assembled digest to one recipient.
func (s *Service) SendWeeklyDigest(ctx context.Context, to string, data email.WeeklyDigestEmailData) error {
	return s.sender.SendWeeklyDigest(ctx, to, data)
}

func (s *Service) send(ctx context.Context, to, notificationType string, payload notificationPayload, dashboardURL string) error {
	switch notificationType {
	case db.NotificationTypeIncidentOpened:
		return s.sender.SendIncidentOpened(ctx, to, email.IncidentOpenedEmailData{
			TenantName:    payload.incident.TenantName,
			ServiceName:   payload.incident.ServiceName,
			IncidentTitle: payload.incident.Title,
			Severity:      payload.incident.Severity,
			DashboardURL:  dashboardURL,
		})
	case db.NotificationTypeIncidentResolved:
		return s.sender.SendIncidentResolved(ctx, to, email.IncidentResolvedEmailData{
			TenantName:    payload.incident.TenantName,
			ServiceName:   payload.incident.ServiceName,
			IncidentTitle: payload.incident.Title,
			Severity:      payload.incident.Severity,
			DashboardURL:  dashboardURL,
		})
	case db.NotificationTypeDomainExpiring:
		return s.sender.SendDomainExpiring(ctx, to, email.DomainExpiringEmailData{
			TenantName:    payload.domainExpiring.TenantName,
			Hostname:      payload.domainExpiring.Hostname,
			DaysRemaining: payload.domainExpiring.DaysRemaining,
			ThresholdDays: payload.domainExpiring.ThresholdDays,
			DashboardURL:  dashboardURL,
		})
	case db.NotificationTypeDomainNSDrift:
		return s.sender.SendDomainNSDrift(ctx, to, email.DomainNSDriftEmailData{
			TenantName:   payload.domainNSDrift.TenantName,
			Hostname:     payload.domainNSDrift.Hostname,
			ExpectedNS:   payload.domainNSDrift.ExpectedNS,
			CurrentNS:    payload.domainNSDrift.CurrentNS,
			DashboardURL: dashboardURL,
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

// domainDashboardURL builds the domains-screen link for a domain
// notification. The admin app has a single /domains route (the detail is a
// drawer over the list), so there is no per-domain deep link.
func (s *Service) domainDashboardURL(domainID string) string {
	if s.adminBaseURL == "" || domainID == "" {
		return ""
	}
	return fmt.Sprintf("%s/domains", strings.TrimRight(s.adminBaseURL, "/"))
}
