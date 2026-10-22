// Package notify resolves who should be notified about an event and sends the
// email, keeping that policy out of the HTTP handlers. It is the single place
// that maps "tenant + event" to a recipient list: the tenant's owner/operator
// members whose preference for that event is enabled.
package notify

import (
	"context"
	"fmt"
	"strings"
	"time"

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
	// Status is the incident's status at send time (investigating/...).
	Status string
	// OpenedAt is the incident's created_at; ResolvedAt is set for a
	// resolved incident.
	OpenedAt   time.Time
	ResolvedAt *time.Time
	// Description is the incident's initial description (manual incidents)
	// or the generic outage description (auto-created ones). The
	// incident-opened email shows it as the first timeline entry: at open
	// time no incident_updates row exists yet.
	Description string
	// ResolutionSummary is the closing note for an incident-resolved email.
	// When empty and incident details are wired (WithIncidentDetails), the
	// latest human- or AI-authored timeline entry is used.
	ResolutionSummary string
}

// tenantGetter is the subset of *db.TenantRepository Service uses to
// resolve the tenant's display name and timezone for incident emails.
type tenantGetter interface {
	Get(ctx context.Context, tenantID string) (*db.Tenant, error)
}

// incidentDetailReader is the subset of *db.IncidentRepository Service uses
// to fill incident-email fields a caller didn't supply.
type incidentDetailReader interface {
	ListServiceNames(ctx context.Context, incidentID string) ([]string, error)
	ListUpdatesPaginated(ctx context.Context, incidentID string, page, pageSize int) ([]db.IncidentUpdate, int, error)
}

// resolutionLookback is how many of the most recent timeline entries are
// scanned for the closing note - enough to skip past the system-authored
// "Status changed to ..." entries Transition appends.
const resolutionLookback = 10

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
	// location is the tenant timezone incident dates render in; nil falls
	// back to email.DefaultLocation.
	location *time.Location
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

	// tenants and incidents are optional (WithIncidentDetails): when set,
	// incident emails are enriched with the tenant's name/timezone, the
	// linked service names, and the closing note. Every lookup is
	// best-effort - a failure is logged and the field is simply omitted.
	tenants   tenantGetter
	incidents incidentDetailReader
}

// NewService builds a Service. adminBaseURL is the dashboard base used to build
// the incident link; an empty value simply omits the link.
func NewService(members memberLister, preferences preferenceResolver, sender emailSender, adminBaseURL string, logger *zap.Logger) *Service {
	return &Service{members: members, preferences: preferences, sender: sender, adminBaseURL: adminBaseURL, logger: logger}
}

// WithIncidentDetails wires the read-only lookups incident emails use to
// fill what the caller didn't supply (tenant name and timezone, service
// names, resolution note). Returns s for chaining at construction time.
func (s *Service) WithIncidentDetails(tenants tenantGetter, incidents incidentDetailReader) *Service {
	s.tenants = tenants
	s.incidents = incidents
	return s
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
	payload := s.enrichIncident(ctx, tenantID, notificationType, summary)
	return s.notify(ctx, tenantID, notificationType, payload, s.dashboardURL(summary.IncidentID))
}

// enrichIncident fills the incident-email fields the caller left empty from
// the optional lookups. Runs once per notification, before recipients are
// resolved, so N recipients cost one set of lookups - not N.
func (s *Service) enrichIncident(ctx context.Context, tenantID, notificationType string, summary IncidentSummary) notificationPayload {
	payload := notificationPayload{incident: summary}

	if s.tenants != nil {
		tenant, err := s.tenants.Get(ctx, tenantID)
		if err != nil {
			s.logger.Warn("notify: failed to load tenant for incident email", zap.String("tenant_id", tenantID), zap.Error(err))
		} else {
			if payload.incident.TenantName == "" {
				payload.incident.TenantName = tenant.Name
			}
			payload.location = email.TenantLocation(tenant.Timezone)
		}
	}

	if s.incidents == nil || summary.IncidentID == "" {
		return payload
	}

	if payload.incident.ServiceName == "" {
		names, err := s.incidents.ListServiceNames(ctx, summary.IncidentID)
		if err != nil {
			s.logger.Warn("notify: failed to load incident service names", zap.String("incident_id", summary.IncidentID), zap.Error(err))
		} else {
			payload.incident.ServiceName = strings.Join(names, ", ")
		}
	}

	if notificationType == db.NotificationTypeIncidentResolved && payload.incident.ResolutionSummary == "" {
		updates, _, err := s.incidents.ListUpdatesPaginated(ctx, summary.IncidentID, 1, resolutionLookback)
		if err != nil {
			s.logger.Warn("notify: failed to load incident timeline", zap.String("incident_id", summary.IncidentID), zap.Error(err))
		} else {
			payload.incident.ResolutionSummary = latestAuthoredNote(updates)
		}
	}

	return payload
}

// latestAuthoredNote returns the most recent timeline entry written by a
// human or recorded as the AI closing summary (updates are newest first),
// skipping the system-authored "Status changed to ..." entries Transition
// appends - those carry no resolution context worth emailing.
func latestAuthoredNote(updates []db.IncidentUpdate) string {
	for _, u := range updates {
		if u.AuthorID != nil || u.IsAISummary {
			return u.Body
		}
	}
	return ""
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

// SendWeeklyDigest sends the assembled digest to one recipient, filling the
// dashboard and incidents-list links from adminBaseURL when the caller
// didn't set them.
func (s *Service) SendWeeklyDigest(ctx context.Context, to string, data email.WeeklyDigestEmailData) error {
	if data.DashboardURL == "" && s.adminBaseURL != "" {
		data.DashboardURL = strings.TrimRight(s.adminBaseURL, "/") + "/overview"
	}
	if data.IncidentsURL == "" && s.adminBaseURL != "" {
		data.IncidentsURL = strings.TrimRight(s.adminBaseURL, "/") + "/incidents"
	}
	return s.sender.SendWeeklyDigest(ctx, to, data)
}

func (s *Service) send(ctx context.Context, to, notificationType string, payload notificationPayload, dashboardURL string) error {
	switch notificationType {
	case db.NotificationTypeIncidentOpened:
		inc := payload.incident
		data := email.IncidentOpenedEmailData{
			TenantName:    inc.TenantName,
			ServiceName:   inc.ServiceName,
			IncidentTitle: inc.Title,
			Severity:      inc.Severity,
			Status:        inc.Status,
			OpenedAt:      inc.OpenedAt,
			Location:      payload.location,
			DashboardURL:  dashboardURL,
		}
		if inc.Description != "" {
			data.FirstUpdateAt = inc.OpenedAt
			data.FirstUpdateMessage = inc.Description
		}
		return s.sender.SendIncidentOpened(ctx, to, data)
	case db.NotificationTypeIncidentResolved:
		inc := payload.incident
		data := email.IncidentResolvedEmailData{
			TenantName:        inc.TenantName,
			ServiceName:       inc.ServiceName,
			IncidentTitle:     inc.Title,
			Severity:          inc.Severity,
			OpenedAt:          inc.OpenedAt,
			Location:          payload.location,
			ResolutionSummary: inc.ResolutionSummary,
			DashboardURL:      dashboardURL,
		}
		if inc.ResolvedAt != nil {
			data.ResolvedAt = *inc.ResolvedAt
		}
		return s.sender.SendIncidentResolved(ctx, to, data)
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
