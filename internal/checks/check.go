package checks

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// httpClient is the shared client every HTTP(S) check uses - its
// Transport.DialContext is the SSRF-safe dialer (dial.go), so an HTTP(S)
// check gets the same blocked-range protection, re-checked on every dial,
// as TCP/Ping (manual-polling-monitoring design.md Approach Exploration).
// Redirects are not followed: http.Client's default CheckRedirect would
// otherwise dial a second, possibly different, admin-uncontrolled host per
// hop - out of scope for a health check whose only contract is "this exact
// target answered 2xx/3xx".
var httpClient = &http.Client{
	Transport: &http.Transport{
		DialContext: dialContext,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// RunCheck performs one health check of pollType against target, through
// the shared SSRF-safe dialer, bounded by timeout (MP-07/MP-08/MP-09). A
// nil error is a succeeded check; any error (a non-2xx/3xx HTTP response, a
// connection error, a timeout, or the dialer's own Control hook rejecting a
// now-blocked address - MP-15) is a failed check. Assumes target already
// passed ValidateTargetFormat for pollType.
func RunCheck(ctx context.Context, pollType, target string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	switch pollType {
	case PollTypeHTTP:
		return runHTTPCheck(ctx, target)
	case PollTypeTCP, PollTypePing:
		return runDialCheck(ctx, pollType, target)
	default:
		return fmt.Errorf("checks: unrecognized poll type %q", pollType)
	}
}

// runHTTPCheck issues a GET against target and treats a 2xx or 3xx status
// as success (MP-07); any other status, a connection error, or a timeout is
// a failure.
func runHTTPCheck(ctx context.Context, target string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("checks: failed to build request for %q: %w", target, err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("checks: HTTP check failed for %q: %w", target, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return fmt.Errorf("checks: HTTP check for %q returned status %d", target, resp.StatusCode)
	}

	return nil
}

// runDialCheck opens a TCP connection to target (TCP: host:port as given;
// Ping: host, defaulting to port 80 when target carries none - MP-08) and
// treats a successful connect as success, closing it immediately since a
// polling-manual reachability check has nothing further to say to the
// target.
func runDialCheck(ctx context.Context, pollType, target string) error {
	hostPort, err := targetHostPort(pollType, target)
	if err != nil {
		return err
	}

	conn, err := dialContext(ctx, "tcp", hostPort)
	if err != nil {
		return fmt.Errorf("checks: %s check failed for %q: %w", pollType, target, err)
	}
	_ = conn.Close()

	return nil
}
