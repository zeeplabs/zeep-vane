package checks

import (
	"context"
	"testing"
)

// unresolvableHost is a hostname guaranteed never to resolve via a real DNS
// lookup - reserved by RFC 6761 §6.4 for exactly this purpose (testing).
const unresolvableHost = "definitely-does-not-resolve.invalid"

func TestValidateTargetSafety_RejectsLiteralLoopback_AllThreeTypes(t *testing.T) {
	cases := []struct {
		pollType string
		target   string
	}{
		{PollTypeHTTP, "http://127.0.0.1/health"},
		{PollTypeTCP, "127.0.0.1:8080"},
		{PollTypePing, "127.0.0.1"},
	}
	for _, tc := range cases {
		if err := ValidateTargetSafety(context.Background(), tc.pollType, tc.target); err == nil {
			t.Errorf("ValidateTargetSafety(%s, %q) = nil error, want a blocked-range rejection", tc.pollType, tc.target)
		}
	}
}

func TestValidateTargetSafety_RejectsLiteralPrivateRanges_AllThreeTypes(t *testing.T) {
	cases := []struct {
		pollType string
		target   string
	}{
		// 10.0.0.0/8
		{PollTypeHTTP, "http://10.1.2.3/health"},
		{PollTypeTCP, "10.1.2.3:8080"},
		{PollTypePing, "10.1.2.3"},
		// 172.16.0.0/12
		{PollTypeHTTP, "http://172.20.0.5/health"},
		{PollTypeTCP, "172.20.0.5:8080"},
		{PollTypePing, "172.20.0.5"},
		// 192.168.0.0/16
		{PollTypeHTTP, "http://192.168.1.1/health"},
		{PollTypeTCP, "192.168.1.1:8080"},
		{PollTypePing, "192.168.1.1"},
		// 169.254.0.0/16 (also covers the cloud metadata endpoint 169.254.169.254)
		{PollTypeHTTP, "http://169.254.169.254/latest/meta-data/"},
		{PollTypeTCP, "169.254.169.254:80"},
		{PollTypePing, "169.254.169.254"},
	}
	for _, tc := range cases {
		if err := ValidateTargetSafety(context.Background(), tc.pollType, tc.target); err == nil {
			t.Errorf("ValidateTargetSafety(%s, %q) = nil error, want a blocked-range rejection", tc.pollType, tc.target)
		}
	}
}

// TestValidateTargetSafety_AllowsUnresolvedDNS asserts T4's "Done when": a
// target whose DNS simply does not resolve is allowed through (design.md
// Tech Decision - a polling-manual service can be registered before its
// target is deployed).
func TestValidateTargetSafety_AllowsUnresolvedDNS(t *testing.T) {
	cases := []struct {
		pollType string
		target   string
	}{
		{PollTypeHTTP, "https://" + unresolvableHost + "/health"},
		{PollTypeTCP, unresolvableHost + ":8080"},
		{PollTypePing, unresolvableHost},
	}
	for _, tc := range cases {
		if err := ValidateTargetSafety(context.Background(), tc.pollType, tc.target); err != nil {
			t.Errorf("ValidateTargetSafety(%s, %q) = %v, want nil (unresolved DNS is allowed)", tc.pollType, tc.target, err)
		}
	}
}
