package checks

import (
	"context"
	"fmt"
	"net"
	"net/url"
)

// defaultPingPort is the port ValidateTargetSafety/RunCheck use to resolve
// and dial a Ping target that carries no explicit port (manual-polling-
// monitoring design.md Tech Decisions - matches the mock's own placeholder,
// which never shows a port, and is the most broadly-open TCP port for a
// generic reachability probe).
const defaultPingPort = "80"

// ValidateTargetSafety resolves target's host (per pollType) and rejects it
// if any resolved IP falls in a blocked range (MP-03). It does not require
// the target be reachable: a target whose DNS simply doesn't resolve yet is
// allowed through (design.md Tech Decision - a polling-manual service can
// be registered before its target is deployed), only a target that
// resolves into vane's own private network is rejected. Assumes target has
// already passed ValidateTargetFormat.
func ValidateTargetSafety(ctx context.Context, pollType, target string) error {
	host, err := targetHost(pollType, target)
	if err != nil {
		return err
	}

	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		// Does not resolve (yet) - allowed, per design.md's Tech Decision.
		return nil
	}

	for _, addr := range addrs {
		if isBlockedIP(addr.IP) {
			return fmt.Errorf("%w: %s", errBlockedTarget, host)
		}
	}

	return nil
}

// targetHost extracts the bare host to resolve/dial for pollType/target -
// the URL's hostname for HTTP(S), and the SplitHostPort host for TCP/Ping
// (Ping falling back to the whole target as the host when it carries no
// port, per its own format rule). Assumes target already passed
// ValidateTargetFormat for pollType.
func targetHost(pollType, target string) (string, error) {
	switch pollType {
	case PollTypeHTTP:
		u, err := url.Parse(target)
		if err != nil {
			return "", fmt.Errorf("checks: invalid HTTP(S) target %q: %w", target, err)
		}
		return u.Hostname(), nil
	case PollTypeTCP:
		host, _, err := net.SplitHostPort(target)
		if err != nil {
			return "", fmt.Errorf("checks: invalid TCP target %q: %w", target, err)
		}
		return host, nil
	case PollTypePing:
		if host, _, err := net.SplitHostPort(target); err == nil {
			return host, nil
		}
		return target, nil
	default:
		return "", fmt.Errorf("checks: unrecognized poll type %q", pollType)
	}
}

// targetHostPort returns the host:port RunCheck's TCP/Ping dial should use -
// targetHost's host plus, for Ping, defaultPingPort when target carried no
// port of its own.
func targetHostPort(pollType, target string) (string, error) {
	switch pollType {
	case PollTypeTCP:
		return target, nil
	case PollTypePing:
		if _, _, err := net.SplitHostPort(target); err == nil {
			return target, nil
		}
		return net.JoinHostPort(target, defaultPingPort), nil
	default:
		return "", fmt.Errorf("checks: unrecognized poll type %q for host:port dial", pollType)
	}
}
