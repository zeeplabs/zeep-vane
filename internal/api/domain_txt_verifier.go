package api

import (
	"context"
	"net"
	"time"
)

// apexTXTResult is the outcome of one root-domain TXT ownership check
// (DATV-03/DATV-04). A domains row is always a root domain that never serves
// vane traffic, so proving ownership means checking a dedicated TXT record,
// never resolving IPs or dialing TLS on the apex.
type apexTXTResult struct {
	// TXTFound is true when the lookup returned at least one TXT value for
	// _vane-verify.<hostname> (false on NXDOMAIN, an empty answer, or a
	// resolver error/timeout).
	TXTFound bool
	// TXTMatches is true when any returned value equals the expected token
	// exactly. Meaningful only alongside TXTFound.
	TXTMatches bool
}

// apexTXTVerifier is the seam DomainsHandler depends on, so tests inject a
// fake instead of hitting real DNS. Distinct from domainVerifier, which
// serves the unrelated subdomain CNAME/TLS VerifyDomain flow.
type apexTXTVerifier interface {
	// Verify looks up the TXT record _vane-verify.<hostname> and reports
	// whether it was found and whether any of its values matches
	// expectedToken exactly.
	Verify(ctx context.Context, hostname, expectedToken string) apexTXTResult
}

// txtResolver is the DNS lookup seam netApexTXTVerifier uses; *net.Resolver
// satisfies it. Tests inject a fake.
type txtResolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

// apexTXTRecordPrefix is the dedicated TXT record name prefix. A dedicated
// subdomain-of-the-record name (rather than the bare apex) avoids colliding
// with the domain's real TXT records (SPF, DKIM, other verification tools).
const apexTXTRecordPrefix = "_vane-verify."

// apexTXTLookupTimeout bounds the DNS lookup so a slow/unreachable resolver
// can't hang a verification request.
const apexTXTLookupTimeout = 5 * time.Second

// netApexTXTVerifier is the production apexTXTVerifier.
type netApexTXTVerifier struct {
	resolver txtResolver
}

// newNetApexTXTVerifier builds the production apexTXTVerifier.
func newNetApexTXTVerifier() *netApexTXTVerifier {
	return &netApexTXTVerifier{resolver: net.DefaultResolver}
}

func (v *netApexTXTVerifier) Verify(ctx context.Context, hostname, expectedToken string) apexTXTResult {
	lookupCtx, cancel := context.WithTimeout(ctx, apexTXTLookupTimeout)
	defer cancel()

	values, err := v.resolver.LookupTXT(lookupCtx, apexTXTRecordPrefix+hostname)
	if err != nil || len(values) == 0 {
		return apexTXTResult{TXTFound: false, TXTMatches: false}
	}

	for _, value := range values {
		if value == expectedToken {
			return apexTXTResult{TXTFound: true, TXTMatches: true}
		}
	}
	return apexTXTResult{TXTFound: true, TXTMatches: false}
}
