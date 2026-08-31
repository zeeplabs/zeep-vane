package api

import (
	"context"
	"crypto/x509"
	"net"
	"time"

	"crypto/tls"
)

// domainVerificationResult is what a domainVerifier reports back for one
// hostname check (VerifyDomain, mirrors the "recheck DNS/SSL" flow
// platforms like Vercel/Render offer for custom domains).
type domainVerificationResult struct {
	// ResolvedIPs is every address hostname currently resolves to (via
	// whatever chain of CNAMEs the resolver follows) - purely informational
	// for the UI, not itself a pass/fail signal.
	ResolvedIPs []string
	// DNSResolved is true if hostname resolves to anything at all.
	DNSResolved bool
	// DNSMatchesTarget is nil when no PUBLIC_DNS_TARGET is configured
	// (nothing to compare against), otherwise true if hostname's resolved
	// IPs overlap with the target's resolved IPs. IP-set overlap, not a
	// literal CNAME-string comparison: net.Resolver.LookupCNAME follows
	// the *entire* CNAME chain and returns the final canonical name, not
	// the single record an operator configured - it also returns the
	// queried name itself (no error) when there is no CNAME at all, only
	// an A record. Both make literal string comparison against the
	// configured target wrong in exactly the common case this feature
	// exists for (a Kubernetes/ELB load balancer hostname, always fronted
	// by at least one CNAME hop).
	DNSMatchesTarget *bool
	// TLSReachable is true if a real TLS handshake against hostname:443
	// completed at all - this alone does NOT mean a legitimate certificate
	// was served (a self-signed cert, a wrong cert, or an unrelated TLS
	// endpoint squatting on the hostname all complete a handshake too).
	TLSReachable bool
	// TLSCertValid is true only if TLSReachable and the served
	// certificate chain verifies against the system root pool for
	// hostname - the actual signal that a real, trusted certificate (e.g.
	// Let's Encrypt) is being served, not just "something answered TLS".
	TLSCertValid bool
	// TLSError is the handshake/connection error, or the certificate
	// validation error when the handshake succeeded but the cert didn't
	// verify, or nil when TLSCertValid is true.
	TLSError *string
}

// domainVerifier is the interface api.StatusPagesHandler.VerifyDomain
// depends on, so tests can inject a fake instead of hitting real DNS/the
// network (this handler's own tests only exercise routing/error-mapping,
// not real network behavior - that would make the suite flaky and slow).
type domainVerifier interface {
	// expectedTarget is config.Config.PublicDNSTarget, or "" if the
	// operator never configured PUBLIC_DNS_TARGET - Verify only sets
	// DNSMatchesTarget when it's non-empty.
	Verify(ctx context.Context, hostname, expectedTarget string) domainVerificationResult
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

func (v *netDomainVerifier) Verify(ctx context.Context, hostname, expectedTarget string) domainVerificationResult {
	result := v.checkDNS(ctx, hostname, expectedTarget)
	result.TLSReachable, result.TLSCertValid, result.TLSError = v.dialTLS(ctx, hostname)
	return result
}

// checkDNS resolves hostname's current IPs and, if expectedTarget is
// configured, resolves it too and checks for any overlap - robust to
// CNAME chains of any length and to A-record-only setups alike, unlike
// comparing DNS record strings directly (see DNSMatchesTarget's doc).
func (v *netDomainVerifier) checkDNS(ctx context.Context, hostname, expectedTarget string) domainVerificationResult {
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	hostIPs, err := v.resolveIPs(lookupCtx, hostname)
	if err != nil {
		return domainVerificationResult{DNSResolved: false}
	}
	result := domainVerificationResult{ResolvedIPs: hostIPs, DNSResolved: true}

	if expectedTarget == "" {
		return result
	}
	targetIPs, err := v.resolveIPs(lookupCtx, expectedTarget)
	if err != nil {
		// The operator's own configured target doesn't resolve - can't
		// say whether hostname matches it or not.
		return result
	}
	matches := ipSetsOverlap(hostIPs, targetIPs)
	result.DNSMatchesTarget = &matches
	return result
}

func (v *netDomainVerifier) resolveIPs(ctx context.Context, name string) ([]string, error) {
	addrs, err := v.resolver.LookupHost(ctx, name)
	if err != nil {
		return nil, err
	}
	return addrs, nil
}

func ipSetsOverlap(a, b []string) bool {
	set := make(map[string]struct{}, len(a))
	for _, ip := range a {
		set[ip] = struct{}{}
	}
	for _, ip := range b {
		if _, ok := set[ip]; ok {
			return true
		}
	}
	return false
}

// dialTLS attempts a real TLS handshake against hostname:443 - the public
// HTTPS listener (newHTTPSServer), reached over the real internet exactly
// like a visitor's browser would, not an in-cluster shortcut. CertMagic's
// on-demand TLS triggers ACME issuance synchronously during this same
// handshake if none exists yet and HostPolicy allows it, so this call is
// also what actually kicks off issuance for a page that's never had a real
// visitor.
//
// InsecureSkipVerify on the dial itself is deliberate: verifying during
// the handshake would make the dial fail outright for a self-signed/
// staging certificate, with no way to inspect what was actually served.
// Instead the handshake is always allowed to complete, and the served
// certificate is verified explicitly afterward (verifyServedCert) - this
// is what actually distinguishes "a real, trusted certificate is being
// served" from "something answered TLS", which a bare handshake success
// cannot.
func (v *netDomainVerifier) dialTLS(ctx context.Context, hostname string) (reachable, certValid bool, errMsg *string) {
	dialCtx, cancel := context.WithTimeout(ctx, v.dialTimeout)
	defer cancel()

	dialer := &tls.Dialer{Config: &tls.Config{ServerName: hostname, InsecureSkipVerify: true}} //nolint:gosec // see comment above
	conn, err := dialer.DialContext(dialCtx, "tcp", net.JoinHostPort(hostname, "443"))
	if err != nil {
		msg := err.Error()
		return false, false, &msg
	}
	defer func() { _ = conn.Close() }()

	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		msg := "internal error: dialed connection is not *tls.Conn"
		return true, false, &msg
	}

	if err := verifyServedCert(tlsConn.ConnectionState(), hostname); err != nil {
		msg := err.Error()
		return true, false, &msg
	}
	return true, true, nil
}

// verifyServedCert checks the certificate chain CertMagic (or whatever is
// actually listening on hostname:443) served during state's handshake
// against the system root CA pool - the same trust check a real browser
// performs, which InsecureSkipVerify on the dial itself deliberately
// skipped so the handshake could complete regardless of cert validity.
func verifyServedCert(state tls.ConnectionState, hostname string) error {
	if len(state.PeerCertificates) == 0 {
		return errNoCertificatePresented
	}

	leaf := state.PeerCertificates[0]
	intermediates := x509.NewCertPool()
	for _, cert := range state.PeerCertificates[1:] {
		intermediates.AddCert(cert)
	}

	_, err := leaf.Verify(x509.VerifyOptions{DNSName: hostname, Intermediates: intermediates})
	return err
}

var errNoCertificatePresented = &tlsVerificationError{"tls handshake completed but no certificate was presented"}

type tlsVerificationError struct{ msg string }

func (e *tlsVerificationError) Error() string { return e.msg }
