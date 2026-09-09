// Package llm defines the provider-agnostic contract vane's LLM connectors
// (OpenAI, ...) implement, and hosts the business logic that
// connects/activates/lists providers and generates SLO analysis text
// through whichever one is active.
package llm

import (
	"context"
	"errors"
)

// Provider is the contract any LLM connector (OpenAI, ...) implements.
// Service never imports a connector package directly - it depends only on
// this interface, obtained through a ProviderFactory.
type Provider interface {
	// Complete sends systemPrompt and userPrompt to the provider's chat
	// completion API and returns the model's response text.
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
	// ValidateCredentials confirms the provider's API key is valid, without
	// generating any completion.
	ValidateCredentials(ctx context.Context) error
}

// ProviderFactory builds a Provider for the given provider name ("openai")
// authenticated with apiKey and configured to use model. Wired in
// internal/cli/routes.go, the same function-typed dependency injection
// pattern internal/email.ProviderFactory uses - it keeps this package
// decoupled from which concrete connector packages exist.
type ProviderFactory func(provider, apiKey, model string) (Provider, error)

// Typed errors shared by any future connector, since the HTTP-behavior
// classification they represent (unauthorized, timeout, server error) is
// provider-agnostic. This is this package's own copy - not shared with
// internal/email's equivalent values - since the two packages are separate
// domains (mirrors how internal/connectors/resend and
// internal/connectors/sendgrid already don't share code with each other).
var (
	// ErrUnauthorized means the provider rejected the API key (401/403).
	ErrUnauthorized = errors.New("llm: unauthorized (invalid or unpermitted api key)")
	// ErrTimeout means the request did not complete before its deadline.
	ErrTimeout = errors.New("llm: request timed out")
	// ErrServer means the provider returned a 5xx.
	ErrServer = errors.New("llm: server error")
)
