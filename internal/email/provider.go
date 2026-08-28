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

// Sender is what a caller outside this package (the future admin-invite
// resend/cancel feature) depends on to send the admin-invite email,
// regardless of which provider is active.
type Sender interface {
	SendAdminInvite(ctx context.Context, to string, data AdminInviteEmailData) error
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
