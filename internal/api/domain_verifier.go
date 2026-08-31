package api

import (
	"context"
	"crypto/tls"
	"net"
	"strings"
	"time"
)

// domainVerificationResult is what a domainVerifier reports back for one
// hostname check (VerifyDomain, mirrors the "recheck DNS/SSL" flow
// platforms like Vercel/Render offer for custom domains).
type domainVerificationResult struct {
	// ResolvedCNAME is the hostname's current CNAME target, or nil if it
	// has none (e.g. an A record instead, or nothing resolves at all).
	// The caller compares this against GET /api/instance/dns-target's
	// value itself - this package has no config dependency.
	ResolvedCNAME *string
	// DNSResolved is true if hostname resolves to anything at all (CNAME
	// or A), regardless of whether it matches the expected target.
	DNSResolved bool
	// TLSReachable is true if a real TLS handshake against hostname:443
	// succeeded - the only way to know for sure a certificate has
	// actually been issued and served, not just that the DB row says
	// "published".
	TLSReachable bool
	// TLSDialError is the handshake/connection error, or nil on success.
	TLSDialError *string
}

// domainVerifier is the interface api.StatusPagesHandler.VerifyDomain
// depends on, so tests can inject a fake instead of hitting real DNS/the
// network (this handler's own tests only exercise routing/error-mapping,
// not real network behavior - that would make the suite flaky and slow).
type domainVerifier interface {
	Verify(ctx context.Context, hostname string) domainVerificationResult
}

// netDomainVerifier is the real domainVerifier: it performs an actual DNS
// lookup and TLS handshake against hostname, exactly like a real visitor's
// browser would. This is deliberate - it's the only way to confirm DNS
// has propagated and a certificate was actually issued and is being
// served, as opposed to inferring it indirectly from the StatusPage row
// (which can lag behind reality, or never update at all if on-demand TLS
// is never triggered by real traffic).
type netDomainVerifier struct {
	resolver *net.Resolver
	// dialTimeout bounds the whole TLS dial, including any on-demand ACME
	// issuance CertMagic performs synchronously during the handshake -
	// Let's Encrypt's HTTP-01 challenge normally completes in a few
	// seconds, but this leaves real headroom.
	dialTimeout time.Duration
}

// newNetDomainVerifier builds the production domainVerifier.
func newNetDomainVerifier() *netDomainVerifier {
	return &netDomainVerifier{resolver: net.DefaultResolver, dialTimeout: 25 * time.Second}
}

func (v *netDomainVerifier) Verify(ctx context.Context, hostname string) domainVerificationResult {
	result := v.checkDNS(ctx, hostname)
	result.TLSReachable, result.TLSDialError = v.dialTLS(ctx, hostname)
	return result
}

// checkDNS looks up hostname's CNAME first (the record type every DNS
// setup instruction in this codebase asks operators to configure - see
// AttachDomainDrawer.tsx), falling back to a plain host lookup so an
// operator who pointed an A record directly at the load balancer's IP
// (uncommon, but not invalid) still shows as "resolves".
func (v *netDomainVerifier) checkDNS(ctx context.Context, hostname string) domainVerificationResult {
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if cname, err := v.resolver.LookupCNAME(lookupCtx, hostname); err == nil {
		trimmed := strings.TrimSuffix(cname, ".")
		return domainVerificationResult{ResolvedCNAME: &trimmed, DNSResolved: true}
	}

	if _, err := v.resolver.LookupHost(lookupCtx, hostname); err == nil {
		return domainVerificationResult{DNSResolved: true}
	}

	return domainVerificationResult{DNSResolved: false}
}

// dialTLS attempts a real TLS handshake against hostname:443 - the public
// HTTPS listener (newHTTPSServer), reached over the real internet exactly
// like a visitor's browser would, not an in-cluster shortcut. A successful
// handshake means a certificate really was served; CertMagic's on-demand
// TLS triggers ACME issuance synchronously during this same handshake if
// none exists yet and HostPolicy allows it, so this call is also what
// actually kicks off issuance for a page that's never had a real visitor.
// InsecureSkipVerify is deliberate: this only checks that a handshake
// completes at all (real cert issued and served), not that hostname
// happens to trust this process's CA pool - a self-signed/staging
// certificate would otherwise report as "unreachable" even though
// issuance genuinely succeeded.
func (v *netDomainVerifier) dialTLS(ctx context.Context, hostname string) (bool, *string) {
	dialCtx, cancel := context.WithTimeout(ctx, v.dialTimeout)
	defer cancel()

	dialer := &tls.Dialer{Config: &tls.Config{ServerName: hostname, InsecureSkipVerify: true}} //nolint:gosec // see comment above
	conn, err := dialer.DialContext(dialCtx, "tcp", net.JoinHostPort(hostname, "443"))
	if err != nil {
		msg := err.Error()
		return false, &msg
	}
	_ = conn.Close()
	return true, nil
}
