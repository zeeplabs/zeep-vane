// Package email defines the provider-agnostic contract vane's email
// connectors (SendGrid, Resend, ...) implement, and hosts the business
// logic that connects/activates/lists providers and sends vane's
// transactional email through whichever one is active.
package email

import (
	"context"
	"errors"
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
}

// AdminInviteEmailData is the data the admin-invite template renders.
type AdminInviteEmailData struct {
	// CompanyName is the inviting company's display name
	// (company_settings.name).
	CompanyName string
	// Role is the invited admin's role (owner/operator/viewer).
	Role string
	// AcceptURL is the invite acceptance link, built by the caller.
	AcceptURL string
}

// PasswordResetEmailData is the data the password-reset template renders.
type PasswordResetEmailData struct {
	// CompanyName is the instance's display name (company_settings.name).
	CompanyName string
	// ResetURL is the password-reset link, built by the caller.
	ResetURL string
}

// SignupVerificationEmailData is the data the signup email-verification
// template renders (T9/T10, multi-tenancy-core).
type SignupVerificationEmailData struct {
	// TenantName is the newly created tenant's display name.
	TenantName string
	// VerifyURL is the email-verification link, built by the caller.
	VerifyURL string
}

// IncidentOpenedEmailData is the data the incident-opened template renders
// (notification-preferences NOTIFPREF-04).
type IncidentOpenedEmailData struct {
	// TenantName is the tenant the incident belongs to, for context.
	TenantName string
	// ServiceName is the monitored service the incident affects.
	ServiceName string
	// IncidentTitle is the incident's title.
	IncidentTitle string
	// Severity is the incident's severity (e.g. critical/major/minor).
	Severity string
	// DashboardURL links to the incident in the admin dashboard.
	DashboardURL string
}

// IncidentResolvedEmailData is the data the incident-resolved template renders
// (notification-preferences NOTIFPREF-07).
type IncidentResolvedEmailData struct {
	// TenantName is the tenant the incident belongs to, for context.
	TenantName string
	// ServiceName is the monitored service the incident affected.
	ServiceName string
	// IncidentTitle is the incident's title.
	IncidentTitle string
	// Severity is the incident's severity (e.g. critical/major/minor).
	Severity string
	// DashboardURL links to the incident in the admin dashboard.
	DashboardURL string
}

// WeeklyDigestEmailData is the data the weekly digest template renders
// (notification-preferences NOTIFPREF-10). The aggregates are computed by the
// caller (DigestScheduler) from existing data; this struct only carries the
// values.
type WeeklyDigestEmailData struct {
	// TenantName is the tenant the digest summarizes.
	TenantName string
	// UptimePercent is the tenant's uptime over the period, 0-100.
	UptimePercent float64
	// IncidentsOpened is how many incidents opened during the period.
	IncidentsOpened int
	// IncidentsResolved is how many incidents resolved during the period.
	IncidentsResolved int
	// PeriodStart and PeriodEnd are preformatted date strings for the period
	// the digest covers.
	PeriodStart string
	PeriodEnd   string
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
