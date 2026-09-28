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
