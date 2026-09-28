// Package rdap is a minimal client for fetching domain registration data
// (expiration date, registrar) from an RDAP server (RFC 9083). It queries
// the IANA-bootstrapped https://rdap.org/{domain} endpoint by default, which
// redirects to the correct registry RDAP server for the TLD - the redirect
// is followed by net/http's default behavior. Only the subset of the RDAP
// JSON response this feature needs (the expiration event and the registrar
// entity) is parsed; there is deliberately no third-party RDAP dependency
// (domain-health-monitoring design.md).
package rdap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBaseURL is the IANA-bootstrapped RDAP endpoint - the production
// value the scheduler passes to NewClient.
const DefaultBaseURL = "https://rdap.org"

// defaultTimeout bounds one lookup. RDAP registries commonly rate-limit or
// time out slowly; the scheduler treats any error as "retry next cycle", so
// a bounded timeout is all that is needed.
const defaultTimeout = 10 * time.Second

// maxResponseBytes caps how much of a response body is read, so a hostile or
// broken server cannot make a lookup allocate unbounded memory.
const maxResponseBytes = 1 << 20 // 1 MiB

// LookupError describes an RDAP lookup failure. Message is a stable,
// internals-free classification - it names no hostname, status body, or
// transport detail - because it rides unsanitized into
// domains.rdap_last_error and from there straight to the admin API/UI
// (AGENTS.md: never leak raw internal errors to a client). Unwrap exposes
// the real underlying error so the caller can still log full detail
// server-side.
type LookupError struct {
	Message string
	Cause   error
}

func (e *LookupError) Error() string { return e.Message }
func (e *LookupError) Unwrap() error { return e.Cause }

// Client fetches RDAP data. baseURL is an injected field (not a package
// constant) so tests point it at a local httptest.Server instead of the real
// network.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient builds a Client against baseURL using a default HTTP client.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: defaultTimeout},
	}
}

// WithHTTPClient returns c with its HTTP client replaced. Tests inject a
// client with a short timeout to exercise the network-timeout path.
func (c *Client) WithHTTPClient(httpClient *http.Client) *Client {
	c.http = httpClient
	return c
}

// Lookup queries RDAP for hostname and returns its expiration date and
// registrar. expiresAt is nil when the response carries no expiration event
// (some registries omit it) - that is not an error. registrar is nil when no
// registrar entity is present. Any HTTP, decoding, or transport failure is
// returned as an error; the caller (domain-health-monitoring DHM-03) records
// it and retries on the next cycle rather than alerting.
func (c *Client) Lookup(ctx context.Context, hostname string) (*time.Time, *string, error) {
	endpoint := fmt.Sprintf("%s/domain/%s", c.baseURL, url.PathEscape(hostname))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, nil, &LookupError{Message: "failed to build RDAP request", Cause: fmt.Errorf("rdap: failed to build request for %q: %w", hostname, err)}
	}
	req.Header.Set("Accept", "application/rdap+json")

	resp, err := c.http.Do(req)
	if err != nil {
		msg := "RDAP request failed"
		if errors.Is(err, context.DeadlineExceeded) {
			msg = "RDAP lookup timed out"
		}
		return nil, nil, &LookupError{Message: msg, Cause: fmt.Errorf("rdap: request for %q failed: %w", hostname, err)}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		cause := fmt.Errorf("rdap: unexpected status %d for %q", resp.StatusCode, hostname)
		if resp.StatusCode == http.StatusNotFound {
			return nil, nil, &LookupError{Message: "domain not found in RDAP registry", Cause: cause}
		}
		return nil, nil, &LookupError{Message: fmt.Sprintf("RDAP request failed with status %d", resp.StatusCode), Cause: cause}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, nil, &LookupError{Message: "failed to read RDAP response", Cause: fmt.Errorf("rdap: failed to read response for %q: %w", hostname, err)}
	}

	var parsed domainResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, nil, &LookupError{Message: "malformed RDAP response", Cause: fmt.Errorf("rdap: malformed RDAP response for %q: %w", hostname, err)}
	}

	expiresAt, err := parseExpiration(parsed.Events)
	if err != nil {
		return nil, nil, &LookupError{Message: "malformed RDAP response", Cause: fmt.Errorf("rdap: %w", err)}
	}
	return expiresAt, parseRegistrar(parsed.Entities), nil
}

// domainResponse is the subset of an RDAP domain object (RFC 9083 §5.3)
// this client reads.
type domainResponse struct {
	Events   []domainEvent  `json:"events"`
	Entities []domainEntity `json:"entities"`
}

// domainEvent is one entry of the RFC 9083 §4.5 events array.
type domainEvent struct {
	Action string `json:"eventAction"`
	Date   string `json:"eventDate"`
}

// domainEntity is one entry of the RFC 9083 §5.1 entities array.
type domainEntity struct {
	Roles      []string          `json:"roles"`
	VCardArray []json.RawMessage `json:"vcardArray"`
}

// parseExpiration returns the eventDate of the "expiration" event, or nil
// when no such event is present. A present-but-unparseable date is a
// malformed response and surfaces as an error.
func parseExpiration(events []domainEvent) (*time.Time, error) {
	for _, e := range events {
		if e.Action != "expiration" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, e.Date)
		if err != nil {
			return nil, fmt.Errorf("unparseable expiration date %q: %w", e.Date, err)
		}
		return &parsed, nil
	}
	return nil, nil
}

// parseRegistrar returns the name (vCard "fn") of the first entity whose
// roles include "registrar", or nil when there is none.
func parseRegistrar(entities []domainEntity) *string {
	for _, e := range entities {
		if !hasRole(e.Roles, "registrar") {
			continue
		}
		if name := vcardName(e.VCardArray); name != nil {
			return name
		}
	}
	return nil
}

func hasRole(roles []string, want string) bool {
	for _, role := range roles {
		if role == want {
			return true
		}
	}
	return false
}

// vcardName extracts the "fn" (formatted name) value from an RFC 9083
// vcardArray: ["vcard", [ [name, params, type, value], ... ]]. Returns nil
// for any shape that doesn't contain an "fn" text value.
func vcardName(vcardArray []json.RawMessage) *string {
	if len(vcardArray) < 2 {
		return nil
	}
	var props [][]json.RawMessage
	if err := json.Unmarshal(vcardArray[1], &props); err != nil {
		return nil
	}
	for _, prop := range props {
		if len(prop) < 4 {
			continue
		}
		var name string
		if err := json.Unmarshal(prop[0], &name); err != nil || name != "fn" {
			continue
		}
		var value string
		if err := json.Unmarshal(prop[3], &value); err != nil {
			continue
		}
		return &value
	}
	return nil
}
