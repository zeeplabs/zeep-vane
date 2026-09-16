// Package checks validates and executes polling-manual health checks
// (manual-polling-monitoring): HTTP(S), TCP, and Ping (TCP-connect)
// targets. This is the one place in the codebase that dials a fully
// admin-supplied, dynamic network target - every check target must go
// through ValidateTargetFormat, ValidateTargetSafety, and (at check time)
// the shared SSRF-safe dialer in dial.go, no exceptions.
package checks

import (
	"fmt"
	"net"
	"net/url"
)

// Poll type constants (manual-polling-monitoring design.md).
const (
	PollTypeHTTP = "http"
	PollTypeTCP  = "tcp"
	PollTypePing = "ping"
)

// ValidateTargetFormat is pure-parsing format validation, per check type,
// with no network access (MP-04):
//
//   - HTTP(S): target must parse as a URL with both a scheme and a host
//     (url.Parse alone accepts a bare host as a path with no scheme, so a
//     bare host is rejected explicitly rather than silently allowed through).
//   - TCP: target must be host:port, via net.SplitHostPort.
//   - Ping: target must be a bare host, optionally with :port - the default
//     port (80) is applied downstream by RunCheck/ValidateTargetSafety, not
//     here, since this function only validates the input's shape.
//
// An unrecognized pollType returns an error rather than panicking -
// ServicesHandler's own validation is expected to catch this first, but
// this function must be safe to call directly.
func ValidateTargetFormat(pollType, target string) error {
	switch pollType {
	case PollTypeHTTP:
		return validateHTTPFormat(target)
	case PollTypeTCP:
		return validateTCPFormat(target)
	case PollTypePing:
		return validatePingFormat(target)
	default:
		return fmt.Errorf("checks: unrecognized poll type %q", pollType)
	}
}

// validateHTTPFormat requires target to parse as an absolute URL with both
// a scheme and a host (e.g. "https://api.acme.health/health"). A bare host
// like "api.acme.health" parses successfully under url.Parse (as a
// schemeless relative path), so it is rejected explicitly here rather than
// silently accepted (spec.md Edge Cases: no scheme -> format error, never a
// silently-prepended scheme).
func validateHTTPFormat(target string) error {
	u, err := url.Parse(target)
	if err != nil {
		return fmt.Errorf("checks: invalid HTTP(S) target %q: %w", target, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("checks: HTTP(S) target %q must be a full URL with a scheme (http:// or https://) and a host", target)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("checks: HTTP(S) target %q must use the http or https scheme, got %q", target, u.Scheme)
	}
	return nil
}

// validateTCPFormat requires target to be host:port, via net.SplitHostPort -
// a bare host with no port is rejected (spec.md Assumptions: TCP requires
// host:port).
func validateTCPFormat(target string) error {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return fmt.Errorf("checks: TCP target %q must be host:port: %w", target, err)
	}
	if host == "" || port == "" {
		return fmt.Errorf("checks: TCP target %q must include both a host and a port", target)
	}
	return nil
}

// validatePingFormat accepts a bare host, or host:port (spec.md
// Assumptions: Ping is a bare host, optional :port, default port applied
// elsewhere). net.SplitHostPort is tried first to accept the host:port
// form; when it fails (no ':port' suffix), the whole target is treated as
// a bare host, only rejected if empty.
func validatePingFormat(target string) error {
	if target == "" {
		return fmt.Errorf("checks: Ping target must not be empty")
	}
	if host, port, err := net.SplitHostPort(target); err == nil {
		if host == "" || port == "" {
			return fmt.Errorf("checks: Ping target %q must include a non-empty host (and port, if given)", target)
		}
		return nil
	}
	return nil
}
