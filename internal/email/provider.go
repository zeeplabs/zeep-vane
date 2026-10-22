// Package email defines the provider-agnostic contract vane's email
// connectors (SendGrid, Resend, ...) implement, and hosts the business
// logic that connects/activates/lists providers and sends vane's
// transactional email through whichever one is active.
package email

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Message is a single email to send, fully rendered and provider-agnostic -
// no connector-specific fields belong here.
type Message struct {
	To        string
	FromEmail string
	FromName  string
	Subject   string
	HTMLBody  string
	TextBody  string
}

// Provider is the contract any email connector (SendGrid, Resend, ...)
// implements. Service never imports a connector package directly - it
// depends only on this interface, obtained through a ProviderFactory.
type Provider interface {
	// Send delivers msg through the provider's send API.
	Send(ctx context.Context, msg Message) error
	// ValidateCredentials confirms the provider's API key is valid, without
	// sending any email.
	ValidateCredentials(ctx context.Context) error
}

// ProviderFactory builds a Provider for the given provider name ("sendgrid"
// or "resend") authenticated with apiKey. Wired in internal/cli/routes.go,
// the same function-typed dependency injection pattern
// internal/api/integrations_handler.go already uses for Datadog - it keeps
// this package decoupled from which concrete connector packages exist.
type ProviderFactory func(provider, apiKey string) (Provider, error)

// Sender is what a caller outside this package depends on to send vane's
// transactional emails, regardless of which provider is active.
type Sender interface {
	SendAdminInvite(ctx context.Context, to string, data AdminInviteEmailData) error
	SendPasswordReset(ctx context.Context, to string, data PasswordResetEmailData) error
	SendSignupVerification(ctx context.Context, to string, data SignupVerificationEmailData) error
	SendIncidentOpened(ctx context.Context, to string, data IncidentOpenedEmailData) error
	SendIncidentResolved(ctx context.Context, to string, data IncidentResolvedEmailData) error
	SendWeeklyDigest(ctx context.Context, to string, data WeeklyDigestEmailData) error
	SendDomainExpiring(ctx context.Context, to string, data DomainExpiringEmailData) error
	SendDomainNSDrift(ctx context.Context, to string, data DomainNSDriftEmailData) error
}

// AdminInviteEmailData is the data the admin-invite template renders.
// Handoff variable mapping: org_name=CompanyName, invite_url=AcceptURL,
// role_label=RoleLabel(), inviter_name/inviter_email=InviterName/
// InviterEmail, expires_in=ExpiresIn.
type AdminInviteEmailData struct {
	// Layout is stamped by the Sender right before rendering; callers
	// leave it zero.
	Layout
	// CompanyName is the inviting company's display name
	// (company_settings.name).
	CompanyName string
	// Role is the invited admin's role (owner/operator/viewer).
	Role string
	// AcceptURL is the invite acceptance link, built by the caller.
	AcceptURL string
	// InviterName and InviterEmail identify the owner who sent the invite.
	// Either may be empty; the template falls back gracefully.
	InviterName  string
	InviterEmail string
	// ExpiresIn is the invite token's TTL as reader-facing copy (e.g.
	// "1 hour"), built from the handler's real TTL constant.
	ExpiresIn string
}

// RoleLabel maps Role to the label the Users screen shows.
func (d AdminInviteEmailData) RoleLabel() string { return labelOr(roleLabels, d.Role) }

// InviterDisplay is the inviter as named in the subject/body: the name
// when known, else the email, else a neutral fallback.
func (d AdminInviteEmailData) InviterDisplay() string {
	switch {
	case d.InviterName != "":
		return d.InviterName
	case d.InviterEmail != "":
		return d.InviterEmail
	default:
		return "An admin"
	}
}

// Subject is the admin-invite subject line (handoff: "{{inviter_name}}
// invited you to join Vane").
func (d AdminInviteEmailData) Subject() string {
	return fmt.Sprintf("%s invited you to join Vane", d.InviterDisplay())
}

// PasswordResetEmailData is the data the password-reset template renders.
// Handoff mapping: user_email=UserEmail, reset_url=ResetURL,
// expires_in=ExpiresIn.
type PasswordResetEmailData struct {
	// Layout is stamped by the Sender right before rendering.
	Layout
	// CompanyName is the instance's display name (company_settings.name).
	CompanyName string
	// ResetURL is the password-reset link, built by the caller.
	ResetURL string
	// UserEmail is the account address the reset was requested for (the
	// recipient itself - never reveals anything the recipient doesn't
	// already know).
	UserEmail string
	// ExpiresIn is the reset token's TTL as reader-facing copy.
	ExpiresIn string
}

// Subject is the password-reset subject line.
func (d PasswordResetEmailData) Subject() string { return "Reset your Vane password" }

// SignupVerificationEmailData is the data the signup email-verification
// template renders (T9/T10, multi-tenancy-core). Link-only variant of the
// handoff design: this codebase's verification is a single-use link token
// (email_verification_tokens), with no numeric code, so the code block and
// its "ou" divider are intentionally not rendered.
type SignupVerificationEmailData struct {
	// Layout is stamped by the Sender right before rendering.
	Layout
	// TenantName is the newly created tenant's display name.
	TenantName string
	// VerifyURL is the email-verification link, built by the caller.
	VerifyURL string
	// ExpiresIn is the verification token's TTL as reader-facing copy.
	ExpiresIn string
}

// Subject is the signup-verification subject line.
func (d SignupVerificationEmailData) Subject() string { return "Confirm your Vane email" }

// IncidentOpenedEmailData is the data the incident-opened template renders
// (notification-preferences NOTIFPREF-04).
type IncidentOpenedEmailData struct {
	// Layout is stamped by the Sender right before rendering.
	Layout
	// TenantName is the tenant the incident belongs to, for context.
	TenantName string
	// ServiceName is the monitored service(s) the incident affects.
	ServiceName string
	// IncidentTitle is the incident's title.
	IncidentTitle string
	// Severity is the incident's severity: minor/moderate/critical
	// (INCSEV-01).
	Severity string
	// Status is the incident's status (investigating/identified/
	// monitoring/resolved).
	Status string
	// OpenedAt is when the incident was created; zero omits it.
	OpenedAt time.Time
	// Location is the tenant timezone dates render in; nil falls back to
	// DefaultLocation.
	Location *time.Location
	// FirstUpdateAt/FirstUpdateMessage fill the quote box. Empty message
	// hides the box.
	FirstUpdateAt      time.Time
	FirstUpdateMessage string
	// Duration is optional: the handoff omits the row when the email is
	// sent at open time, which is always the case today.
	Duration string
	// DashboardURL links to the incident in the admin dashboard.
	DashboardURL string
}

// SeverityLabel maps Severity to its display label.
func (d IncidentOpenedEmailData) SeverityLabel() string { return labelOr(severityLabels, d.Severity) }

// SeverityBadge is the uppercase badge text ("CRITICAL").
func (d IncidentOpenedEmailData) SeverityBadge() string { return strings.ToUpper(d.SeverityLabel()) }

// StatusLabel maps Status to its display label.
func (d IncidentOpenedEmailData) StatusLabel() string { return labelOr(incidentStatusLabels, d.Status) }

// AccentColor is the top bar / badge / quote-border color: red for
// critical, warning amber for moderate and minor (this repo's severity enum
// has no "major"; the handoff's "major/minor = amber" maps to
// moderate/minor here).
func (d IncidentOpenedEmailData) AccentColor() string {
	if d.Severity == "critical" {
		return colorCritical
	}
	return colorWarning
}

// TintColor is the badge / quote-box background paired with AccentColor.
func (d IncidentOpenedEmailData) TintColor() string {
	if d.Severity == "critical" {
		return colorCriticalBg
	}
	return colorWarningBg
}

// OpenedAtText renders OpenedAt as "Sep 10 at 4:02 PM" in the tenant zone.
func (d IncidentOpenedEmailData) OpenedAtText() string {
	return formatDateTimeAt(d.OpenedAt, d.Location)
}

// OpenedAtClock renders OpenedAt as "16:02" (preheader).
func (d IncidentOpenedEmailData) OpenedAtClock() string { return formatClock(d.OpenedAt, d.Location) }

// FirstUpdateTimeText renders FirstUpdateAt as "16:02".
func (d IncidentOpenedEmailData) FirstUpdateTimeText() string {
	return formatClock(d.FirstUpdateAt, d.Location)
}

// Subject is the incident-opened subject line (handoff: "[Critical] New
// incident: {{incident_title}}").
func (d IncidentOpenedEmailData) Subject() string {
	return fmt.Sprintf("[%s] New incident: %s", d.SeverityLabel(), d.IncidentTitle)
}

// IncidentResolvedEmailData is the data the incident-resolved template renders
// (notification-preferences NOTIFPREF-07).
type IncidentResolvedEmailData struct {
	// Layout is stamped by the Sender right before rendering.
	Layout
	// TenantName is the tenant the incident belongs to, for context.
	TenantName string
	// ServiceName is the monitored service(s) the incident affected.
	ServiceName string
	// IncidentTitle is the incident's title.
	IncidentTitle string
	// Severity is the incident's severity: minor/moderate/critical.
	Severity string
	// OpenedAt and ResolvedAt bound the incident; either zero omits the
	// matching row (and TotalDuration needs both).
	OpenedAt   time.Time
	ResolvedAt time.Time
	// Location is the tenant timezone dates render in.
	Location *time.Location
	// ResolutionSummary fills the quote box (the closing note); empty hides
	// it.
	ResolutionSummary string
	// DashboardURL links to the incident in the admin dashboard.
	DashboardURL string
}

// ResolvedAtText renders ResolvedAt as "Sep 8 at 10:20 AM".
func (d IncidentResolvedEmailData) ResolvedAtText() string {
	return formatDateTimeAt(d.ResolvedAt, d.Location)
}

// OpenedAtRow renders OpenedAt as "Sep 8, 9:15 AM" (detail table).
func (d IncidentResolvedEmailData) OpenedAtRow() string {
	return formatDateTimeComma(d.OpenedAt, d.Location)
}

// ResolvedAtRow renders ResolvedAt as "Sep 8, 10:20 AM" (detail table).
func (d IncidentResolvedEmailData) ResolvedAtRow() string {
	return formatDateTimeComma(d.ResolvedAt, d.Location)
}

// TotalDuration is ResolvedAt-OpenedAt as "1h 05min", or "" when either
// bound is unknown.
func (d IncidentResolvedEmailData) TotalDuration() string {
	if d.OpenedAt.IsZero() || d.ResolvedAt.IsZero() {
		return ""
	}
	return formatDuration(d.ResolvedAt.Sub(d.OpenedAt))
}

// Subject is the incident-resolved subject line.
func (d IncidentResolvedEmailData) Subject() string {
	return fmt.Sprintf("Resolved: %s", d.IncidentTitle)
}

// DomainExpiringEmailData is the data the domain-expiring template renders
// (domain-health-monitoring DHM-02).
type DomainExpiringEmailData struct {
	// TenantName is the tenant the domain belongs to, for context.
	TenantName string
	// Hostname is the registered domain approaching expiration.
	Hostname string
	// DaysRemaining is how many days remain until expiration at alert time.
	DaysRemaining int
	// ThresholdDays is the crossed alert threshold (30, 15 or 7).
	ThresholdDays int
	// DashboardURL links to the domains screen in the admin dashboard.
	DashboardURL string
}

// DaysRemainingText renders DaysRemaining as reader-friendly copy: a
// crossing can fire with a zero or negative day count (the domain already
// expired, or expires today, by the time the alert is dispatched), and
// "expires in -3 days" reads as broken rather than urgent.
func (d DomainExpiringEmailData) DaysRemainingText() string {
	switch {
	case d.DaysRemaining < 0:
		return fmt.Sprintf("expired %d days ago", -d.DaysRemaining)
	case d.DaysRemaining == 0:
		return "expires today"
	default:
		return fmt.Sprintf("expires in %d days", d.DaysRemaining)
	}
}

// DomainNSDriftEmailData is the data the domain NS-drift template renders
// (domain-health-monitoring DHM-06).
type DomainNSDriftEmailData struct {
	// TenantName is the tenant the domain belongs to, for context.
	TenantName string
	// Hostname is the registered domain whose nameservers changed.
	Hostname string
	// ExpectedNS is the learned baseline nameserver set.
	ExpectedNS []string
	// CurrentNS is the freshly resolved nameserver set that differs from it.
	CurrentNS []string
	// DashboardURL links to the domains screen in the admin dashboard.
	DashboardURL string
}

// WeeklyDigestEmailData is the data the weekly digest template renders
// (notification-preferences NOTIFPREF-10). The aggregates are computed by the
// caller (DigestScheduler) from existing data; this struct only carries the
// values.
type WeeklyDigestEmailData struct {
	// Layout is stamped by the Sender right before rendering.
	Layout
	// TenantName is the tenant the digest summarizes.
	TenantName string
	// UptimePercent is the tenant's mean per-service uptime over the
	// period, 0-100.
	UptimePercent float64
	// IncidentsOpened is how many incidents opened during the period.
	IncidentsOpened int
	// IncidentsResolved is how many incidents resolved during the period.
	IncidentsResolved int
	// PeriodStart and PeriodEnd bound the window the digest covers
	// ([PeriodStart, PeriodEnd)).
	PeriodStart time.Time
	PeriodEnd   time.Time
	// Location is the tenant timezone the period label renders in.
	Location *time.Location
	// Services is the per-service uptime list, already capped at
	// DigestServicesCap by the builder; TotalServices is the uncapped
	// count, so the template can say how many were left out.
	Services      []DigestServiceRow
	TotalServices int
	// Incidents lists the incidents that were open at some point in the
	// period (opened, resolved, or still ongoing in it), already capped at
	// DigestIncidentsCap by the builder; TotalIncidents is the uncapped
	// count, so the stat tile stays the true total and the template can say
	// how many were left out.
	Incidents      []DigestIncidentRow
	TotalIncidents int
	// MTTR is the mean time to resolution of incidents resolved in the
	// period; zero when none were.
	MTTR time.Duration
	// DashboardURL links to the admin overview.
	DashboardURL string
	// IncidentsURL links to the admin incidents list, for the "+N more"
	// line under a capped incidents list.
	IncidentsURL string
}

// DigestIncidentsCap is how many incidents the digest lists before
// deferring to the admin incidents page (same bound as the services list).
const DigestIncidentsCap = 10

// DigestServicesCap is how many services the digest lists before deferring
// to the dashboard CTA (handoff: "Cap at ~10 services").
const DigestServicesCap = 10

// digestHealthyThreshold is the uptime at or above which a digest service
// row reads "Operational" in green (handoff: >= 99.5%).
const digestHealthyThreshold = 99.5

// DigestServiceRow is one row of the digest's "Monitored services" list.
type DigestServiceRow struct {
	Name          string
	UptimePercent float64
}

func (r DigestServiceRow) healthy() bool { return r.UptimePercent >= digestHealthyThreshold }

// UptimeText renders the uptime with 2 decimals ("99.94%"), matching the
// handoff's sample formatting.
func (r DigestServiceRow) UptimeText() string { return fmt.Sprintf("%.2f%%", r.UptimePercent) }

// StatusLabel is "Operational" at/above the healthy threshold, otherwise
// "Degraded this week".
func (r DigestServiceRow) StatusLabel() string {
	if r.healthy() {
		return "Operational"
	}
	return "Degraded this week"
}

// StatusColor is the status label's text color.
func (r DigestServiceRow) StatusColor() string {
	if r.healthy() {
		return colorSuccessText
	}
	return colorWarning
}

// BarColor is the filled bar cell's color.
func (r DigestServiceRow) BarColor() string {
	if r.healthy() {
		return colorSuccessDot
	}
	return colorWarning
}

// BarFilled is the filled cell's width out of 100px: round(uptime), or 100
// at/above 99.9 (handoff bar rule).
func (r DigestServiceRow) BarFilled() int {
	if r.UptimePercent >= 99.9 {
		return 100
	}
	filled := int(r.UptimePercent + 0.5)
	if filled < 0 {
		return 0
	}
	if filled > 100 {
		return 100
	}
	return filled
}

// BarEmpty is the remaining cell's width (100 - BarFilled).
func (r DigestServiceRow) BarEmpty() int { return 100 - r.BarFilled() }

// DigestIncidentRow is one row of the digest's "This week's incidents" list.
type DigestIncidentRow struct {
	Title       string
	ServiceName string
	Status      string
	// OngoingAtPeriodEnd marks an incident still open when the period
	// ended, though since resolved (between the period end and the send):
	// its current Status would wrongly read RESOLVED for the week being
	// summarized, so the pill reads ONGOING instead.
	OngoingAtPeriodEnd bool
	// Duration is how long the incident lasted (resolved) or has lasted so
	// far as of the digest's period end (still open).
	Duration time.Duration
}

// pillStatus is the status the pill renders: "ongoing" for an incident that
// was still open at period end but has resolved since, otherwise Status.
func (r DigestIncidentRow) pillStatus() string {
	if r.OngoingAtPeriodEnd {
		return "ongoing"
	}
	return r.Status
}

// DurationText renders Duration ("2h 10min").
func (r DigestIncidentRow) DurationText() string { return formatDuration(r.Duration) }

// StatusPill is the pill text (handoff: RESOLVED/MONITORING/INVESTIGATING;
// "identified" renders IDENTIFIED, OngoingAtPeriodEnd renders ONGOING).
func (r DigestIncidentRow) StatusPill() string {
	if r.OngoingAtPeriodEnd {
		return "ONGOING"
	}
	return strings.ToUpper(labelOr(incidentStatusLabels, r.Status))
}

// PillColor is the pill text color: green resolved, amber monitoring and
// identified (and ongoing-at-period-end), red investigating.
func (r DigestIncidentRow) PillColor() string {
	switch r.pillStatus() {
	case "resolved":
		return colorSuccessText
	case "monitoring", "identified", "ongoing":
		return colorWarning
	default:
		return colorCritical
	}
}

// PillBg is the pill background paired with PillColor.
func (r DigestIncidentRow) PillBg() string {
	switch r.pillStatus() {
	case "resolved":
		return colorSuccessBg
	case "monitoring", "identified", "ongoing":
		return colorWarningBg
	default:
		return colorCriticalBg
	}
}

// PeriodLabel renders the window as "Sep 8–14, 2026" in the tenant zone.
func (d WeeklyDigestEmailData) PeriodLabel() string { return formatPeriod(d.PeriodEnd, d.Location) }

// AvgUptimeText renders the mean uptime ("99.94%").
func (d WeeklyDigestEmailData) AvgUptimeText() string { return fmt.Sprintf("%.2f%%", d.UptimePercent) }

// IncidentCount is how many incidents were active in the period - the
// uncapped total (TotalIncidents), never just the rows the capped list
// shows. Falls back to len(Incidents) when TotalIncidents is unset.
func (d WeeklyDigestEmailData) IncidentCount() int {
	if d.TotalIncidents > len(d.Incidents) {
		return d.TotalIncidents
	}
	return len(d.Incidents)
}

// HiddenIncidents is how many incidents the cap left out of the list.
func (d WeeklyDigestEmailData) HiddenIncidents() int { return d.IncidentCount() - len(d.Incidents) }

// MTTRText renders MTTR ("34min"), or "—" when nothing resolved in the
// period.
func (d WeeklyDigestEmailData) MTTRText() string {
	if d.MTTR <= 0 {
		return "—"
	}
	return formatDuration(d.MTTR)
}

// IsLastService reports whether i is the last services-list row (no bottom
// border, per the handoff).
func (d WeeklyDigestEmailData) IsLastService(i int) bool { return i == len(d.Services)-1 }

// IsLastIncident reports whether i is the last incidents-list row.
func (d WeeklyDigestEmailData) IsLastIncident(i int) bool { return i == len(d.Incidents)-1 }

// HiddenServices is how many services the cap left out of the list.
func (d WeeklyDigestEmailData) HiddenServices() int {
	if d.TotalServices > len(d.Services) {
		return d.TotalServices - len(d.Services)
	}
	return 0
}

// FrequencySettingsURL and UnsubscribeURL both point at the notification
// preferences screen, where the weekly_digest toggle lives. There is no
// per-recipient one-click unsubscribe token in this codebase yet, so no
// List-Unsubscribe header is sent either - see the email handoff gap notes.
func (d WeeklyDigestEmailData) FrequencySettingsURL() string { return d.NotificationPrefsURL }

// UnsubscribeURL - see FrequencySettingsURL.
func (d WeeklyDigestEmailData) UnsubscribeURL() string { return d.NotificationPrefsURL }

// Subject is the weekly-digest subject line (handoff: "Your weekly summary
// · {{period_label}}").
func (d WeeklyDigestEmailData) Subject() string {
	if label := d.PeriodLabel(); label != "" {
		return "Your weekly summary · " + label
	}
	return "Your weekly summary"
}

// Typed errors shared by both connectors, since the HTTP-behavior
// classification they represent (unauthorized, timeout, server error) is
// provider-agnostic - duplicating the same three values in two connector
// packages would be pure repetition with no benefit.
var (
	// ErrUnauthorized means the provider rejected the API key (401/403).
	ErrUnauthorized = errors.New("email: unauthorized (invalid or unpermitted api key)")
	// ErrTimeout means the request did not complete before its deadline.
	ErrTimeout = errors.New("email: request timed out")
	// ErrServer means the provider returned a 5xx.
	ErrServer = errors.New("email: server error")
)
