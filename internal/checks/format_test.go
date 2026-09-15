package checks

import "testing"

// TestValidateTargetFormat_HTTP_RejectsBareHost asserts MP-04/spec.md Edge
// Cases: an HTTP(S) target with no scheme is rejected as a format error,
// never silently accepted with a prepended scheme.
func TestValidateTargetFormat_HTTP_RejectsBareHost(t *testing.T) {
	if err := ValidateTargetFormat(PollTypeHTTP, "api.acme.health"); err == nil {
		t.Errorf("ValidateTargetFormat(http, bare host) = nil error, want an error")
	}
}

// TestValidateTargetFormat_HTTP_AcceptsFullURL asserts MP-04: a full URL
// with http:// or https:// is accepted.
func TestValidateTargetFormat_HTTP_AcceptsFullURL(t *testing.T) {
	cases := []string{
		"https://api.acme.health/health",
		"http://api.acme.health/health",
	}
	for _, target := range cases {
		if err := ValidateTargetFormat(PollTypeHTTP, target); err != nil {
			t.Errorf("ValidateTargetFormat(http, %q) = %v, want nil", target, err)
		}
	}
}

// TestValidateTargetFormat_HTTP_RejectsNonHTTPScheme asserts
// validateHTTPFormat rejects a target using a scheme other than http/https
// (e.g. ftp://) rather than silently accepting it as a "full URL".
func TestValidateTargetFormat_HTTP_RejectsNonHTTPScheme(t *testing.T) {
	if err := ValidateTargetFormat(PollTypeHTTP, "ftp://api.acme.health/health"); err == nil {
		t.Errorf("ValidateTargetFormat(http, ftp://...) = nil error, want an error")
	}
}

// TestValidateTargetFormat_TCP_RejectsBareHost asserts MP-04: a TCP target
// with no port is rejected.
func TestValidateTargetFormat_TCP_RejectsBareHost(t *testing.T) {
	if err := ValidateTargetFormat(PollTypeTCP, "db.acme.health"); err == nil {
		t.Errorf("ValidateTargetFormat(tcp, bare host) = nil error, want an error")
	}
}

// TestValidateTargetFormat_TCP_AcceptsHostPort asserts MP-04: a TCP target
// with a port is accepted.
func TestValidateTargetFormat_TCP_AcceptsHostPort(t *testing.T) {
	if err := ValidateTargetFormat(PollTypeTCP, "db.acme.health:5432"); err != nil {
		t.Errorf("ValidateTargetFormat(tcp, host:port) = %v, want nil", err)
	}
}

// TestValidateTargetFormat_Ping_AcceptsBareHost asserts MP-04: a Ping
// target with no port is accepted (default port applied downstream, not by
// this function).
func TestValidateTargetFormat_Ping_AcceptsBareHost(t *testing.T) {
	if err := ValidateTargetFormat(PollTypePing, "cache.acme.health"); err != nil {
		t.Errorf("ValidateTargetFormat(ping, bare host) = %v, want nil", err)
	}
}

// TestValidateTargetFormat_Ping_AcceptsHostPort asserts MP-04: a Ping
// target with an explicit port is also accepted.
func TestValidateTargetFormat_Ping_AcceptsHostPort(t *testing.T) {
	if err := ValidateTargetFormat(PollTypePing, "cache.acme.health:6379"); err != nil {
		t.Errorf("ValidateTargetFormat(ping, host:port) = %v, want nil", err)
	}
}

// TestValidateTargetFormat_Ping_RejectsEmpty asserts validatePingFormat
// rejects an empty target rather than accepting it as "a bare host".
func TestValidateTargetFormat_Ping_RejectsEmpty(t *testing.T) {
	if err := ValidateTargetFormat(PollTypePing, ""); err == nil {
		t.Errorf("ValidateTargetFormat(ping, \"\") = nil error, want an error")
	}
}

// TestValidateTargetFormat_UnrecognizedPollType_ReturnsErrorNotPanic
// asserts T3's "Done when": an unrecognized pollType returns an error
// instead of panicking - defensive, since ServicesHandler's own validation
// is expected to catch this first.
func TestValidateTargetFormat_UnrecognizedPollType_ReturnsErrorNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ValidateTargetFormat panicked: %v", r)
		}
	}()

	if err := ValidateTargetFormat("carrier-pigeon", "anything"); err == nil {
		t.Errorf("ValidateTargetFormat(unrecognized, ...) = nil error, want an error")
	}
}
