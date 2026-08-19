// Package tls builds vane's on-demand TLS manager. It wraps CertMagic
// (github.com/caddyserver/certmagic) so publishing a status page at a
// custom subdomain gets a Let's Encrypt certificate automatically, with no
// external reverse proxy and no manual certificate step.
package tls

import (
	"context"
	"errors"
	"fmt"

	"github.com/caddyserver/certmagic"
)

// ErrHostnameNotFound is returned by a StatusPageStore when no status page
// is registered for the requested hostname.
var ErrHostnameNotFound = errors.New("tls: hostname not registered to any status page")

// draftState is the StatusPage.State value a page has before an admin has
// pointed it at a domain worth issuing a real certificate for. HostPolicy
// never allows ACME issuance for a page still in this state.
const draftState = "draft"

// StatusPageStore looks up a status page's current State by its full
// public hostname (subdomain + root domain, e.g. "status.empresa.com").
type StatusPageStore interface {
	StateByHostname(ctx context.Context, hostname string) (string, error)
}

// NewManager builds a certmagic.Config with on-demand TLS gated by
// HostPolicy (SP-11, SP-13): a certificate is only ever requested for a
// hostname that belongs to a registered, non-draft StatusPage. This gate is
// not optional - on-demand TLS without it lets any inbound TLS handshake
// force an ACME issuance attempt for an arbitrary hostname, which risks
// tripping Let's Encrypt's per-domain rate limit and enables abuse by a
// third party who controls no domain the operator registered (design.md
// Risks & Concerns).
//
// storagePath is where CertMagic persists certificates and ACME account
// state; it must point at a volume that survives container restarts, or
// every restart re-issues every certificate from scratch.
func NewManager(store StatusPageStore, storagePath string) *certmagic.Config {
	certmagic.Default.Storage = &certmagic.FileStorage{Path: storagePath}

	cfg := certmagic.NewDefault()
	cfg.OnDemand = &certmagic.OnDemandConfig{
		DecisionFunc: HostPolicy(store),
	}

	return cfg
}

// HostPolicy builds the decision function CertMagic calls before issuing or
// renewing a certificate for hostname. It rejects hostname unless store
// resolves it to a StatusPage whose State is not "draft" - a hostname with
// no matching StatusPage, and a hostname whose StatusPage is still in
// "draft", are both rejected the same way: no ACME call is made.
func HostPolicy(store StatusPageStore) func(ctx context.Context, hostname string) error {
	return func(ctx context.Context, hostname string) error {
		state, err := store.StateByHostname(ctx, hostname)
		if errors.Is(err, ErrHostnameNotFound) {
			return fmt.Errorf("tls: %s is not a registered status page hostname", hostname)
		}
		if err != nil {
			return fmt.Errorf("tls: failed to look up status page for hostname %s: %w", hostname, err)
		}
		if state == draftState {
			return fmt.Errorf("tls: %s belongs to a draft status page, not eligible for tls issuance", hostname)
		}
		return nil
	}
}
