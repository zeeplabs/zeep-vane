package db

import "testing"

// TestNotificationDefaultEnabled_DomainExpiring_DefaultsTrue covers T2's
// done-when for DHM-02: a user with no stored row is opted into the
// domain-expiring alert by default (safety-relevant, like the incident
// types - not default-off like the weekly digest).
func TestNotificationDefaultEnabled_DomainExpiring_DefaultsTrue(t *testing.T) {
	if got := NotificationDefaultEnabled(NotificationTypeDomainExpiring); !got {
		t.Errorf("NotificationDefaultEnabled(%q) = %v, want true", NotificationTypeDomainExpiring, got)
	}
}

// TestNotificationDefaultEnabled_DomainNSDrift_DefaultsTrue covers T2's
// done-when for DHM-06.
func TestNotificationDefaultEnabled_DomainNSDrift_DefaultsTrue(t *testing.T) {
	if got := NotificationDefaultEnabled(NotificationTypeDomainNSDrift); !got {
		t.Errorf("NotificationDefaultEnabled(%q) = %v, want true", NotificationTypeDomainNSDrift, got)
	}
}
