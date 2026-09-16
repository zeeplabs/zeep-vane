package checks

import (
	"context"
	"fmt"
	"net"
	"syscall"
)

// blockedRanges are the CIDR ranges no polling-manual check target may ever
// connect to (manual-polling-monitoring spec.md Assumptions/MP-03):
// loopback, the three private ranges, and the link-local/cloud-metadata
// range (169.254.0.0/16, which covers the common cloud metadata endpoint
// 169.254.169.254).
var blockedRanges = mustParseCIDRs(
	"127.0.0.0/8",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"169.254.0.0/16",
)

func mustParseCIDRs(cidrs ...string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			// A build-time defect (a hardcoded literal failing to parse),
			// not a runtime/user-input condition - panicking here is the
			// same "fail fast at construction" posture ServicesHandler
			// already uses for its own fixed timezone load.
			panic(fmt.Sprintf("checks: invalid hardcoded CIDR %q: %v", cidr, err))
		}
		nets = append(nets, ipNet)
	}
	return nets
}

// isBlockedIP reports whether ip falls in any of blockedRanges.
func isBlockedIP(ip net.IP) bool {
	for _, blocked := range blockedRanges {
		if blocked.Contains(ip) {
			return true
		}
	}
	return false
}

// errBlockedTarget is returned (wrapped) by the safe dialer's Control hook
// when the resolved address falls in a blocked range, and by
// ValidateTargetSafety for the same reason at creation time.
var errBlockedTarget = fmt.Errorf("checks: target resolves to a blocked network range")

// safeDialer is the single net.Dialer every polling-manual check dial goes
// through - HTTP(S) via its Transport.DialContext, TCP/Ping directly via
// DialContext (manual-polling-monitoring design.md Approach Exploration).
// Its Control hook fires after DNS resolution but before the connect
// syscall, on every dial (i.e. every poll cycle, not only at
// ValidateTargetSafety's creation-time check), which is what closes the
// DNS-rebinding/TOCTOU gap: a target that resolved safely at creation time
// but now resolves into a blocked range is rejected right here, at the
// point of actually connecting.
var safeDialer = &net.Dialer{
	Control: func(_, address string, c syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("checks: invalid dial address %q: %w", address, err)
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return fmt.Errorf("checks: dial address %q did not resolve to a literal IP", address)
		}
		if isBlockedIP(ip) {
			return errBlockedTarget
		}
		return nil
	},
}

// dialContext is safeDialer.DialContext, used directly for TCP/Ping checks
// and as the HTTP client's Transport.DialContext.
func dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return safeDialer.DialContext(ctx, network, address)
}
