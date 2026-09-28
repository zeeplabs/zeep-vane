package email

import (
	"strings"
	"testing"
)

// TestRenderDomainExpiring_IncludesHostnameDaysAndThreshold covers T5's
// done-when: the domain-expiring template renders the hostname and its
// type-specific content (days remaining and threshold) with no blank/leftover
// template placeholders.
func TestRenderDomainExpiring_IncludesHostnameDaysAndThreshold(t *testing.T) {
	tmpls, err := parseTemplates()
	if err != nil {
		t.Fatalf("parseTemplates() returned unexpected error: %v", err)
	}

	htmlBody, textBody, err := tmpls.renderDomainExpiring(DomainExpiringEmailData{
		TenantName:    "Acme",
		Hostname:      "starbem.app",
		DaysRemaining: 6,
		ThresholdDays: 7,
		DashboardURL:  "https://vane.example.com/domains",
	})
	if err != nil {
		t.Fatalf("renderDomainExpiring() returned unexpected error: %v", err)
	}

	for _, body := range []string{htmlBody, textBody} {
		if !strings.Contains(body, "starbem.app") {
			t.Errorf("body missing hostname: %q", body)
		}
		if !strings.Contains(body, "6") {
			t.Errorf("body missing days remaining: %q", body)
		}
		if !strings.Contains(body, "7") {
			t.Errorf("body missing threshold: %q", body)
		}
		if strings.Contains(body, "<no value>") {
			t.Errorf("body contains a blank template value: %q", body)
		}
	}
}

// TestRenderDomainExpiring_AlreadyExpired_ReadsAsExpiredNotNegativeDays covers
// a bug where a crossing firing with a zero or negative DaysRemaining (the
// domain already expired, or expires today, by the time the alert sends)
// rendered as "expires in -3 days" - broken-looking copy instead of urgent
// copy.
func TestRenderDomainExpiring_AlreadyExpired_ReadsAsExpiredNotNegativeDays(t *testing.T) {
	tmpls, err := parseTemplates()
	if err != nil {
		t.Fatalf("parseTemplates() returned unexpected error: %v", err)
	}

	htmlBody, textBody, err := tmpls.renderDomainExpiring(DomainExpiringEmailData{
		Hostname:      "starbem.app",
		DaysRemaining: -3,
		ThresholdDays: 7,
		DashboardURL:  "https://vane.example.com/domains",
	})
	if err != nil {
		t.Fatalf("renderDomainExpiring() returned unexpected error: %v", err)
	}

	for _, body := range []string{htmlBody, textBody} {
		if strings.Contains(body, "-3") {
			t.Errorf("body still renders a negative day count: %q", body)
		}
		if !strings.Contains(body, "expired 3 days ago") {
			t.Errorf("body = %q, want it to read as already expired", body)
		}
	}
}

// TestRenderDomainNSDrift_IncludesHostnameAndExpectedVsCurrentNS covers T5's
// done-when: the NS-drift template renders the hostname and both the expected
// and current nameserver sets.
func TestRenderDomainNSDrift_IncludesHostnameAndExpectedVsCurrentNS(t *testing.T) {
	tmpls, err := parseTemplates()
	if err != nil {
		t.Fatalf("parseTemplates() returned unexpected error: %v", err)
	}

	htmlBody, textBody, err := tmpls.renderDomainNSDrift(DomainNSDriftEmailData{
		TenantName:   "Acme",
		Hostname:     "starbem.app",
		ExpectedNS:   []string{"ns1.old.net", "ns2.old.net"},
		CurrentNS:    []string{"ns1.new.net"},
		DashboardURL: "https://vane.example.com/domains",
	})
	if err != nil {
		t.Fatalf("renderDomainNSDrift() returned unexpected error: %v", err)
	}

	for _, body := range []string{htmlBody, textBody} {
		if !strings.Contains(body, "starbem.app") {
			t.Errorf("body missing hostname: %q", body)
		}
		if !strings.Contains(body, "ns1.old.net") || !strings.Contains(body, "ns2.old.net") {
			t.Errorf("body missing expected NS set: %q", body)
		}
		if !strings.Contains(body, "ns1.new.net") {
			t.Errorf("body missing current NS set: %q", body)
		}
		if strings.Contains(body, "<no value>") {
			t.Errorf("body contains a blank template value: %q", body)
		}
	}
}
