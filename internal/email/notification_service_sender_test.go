package email

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap"
)

type fakeNotificationServiceClient struct {
	lastReq NotificationServiceRequest
	err     error
	calls   int
}

func (f *fakeNotificationServiceClient) Send(ctx context.Context, req NotificationServiceRequest) error {
	f.calls++
	f.lastReq = req
	return f.err
}

func newTestSender(t *testing.T, client NotificationServiceClient) *NotificationServiceSender {
	t.Helper()
	s, err := NewNotificationServiceSender(client, "vane-saas", zap.NewNop())
	if err != nil {
		t.Fatalf("NewNotificationServiceSender() returned unexpected error: %v", err)
	}
	return s
}

func TestSendAdminInvite_MapsCategoryPriorityType(t *testing.T) {
	fake := &fakeNotificationServiceClient{}
	s := newTestSender(t, fake)

	err := s.SendAdminInvite(t.Context(), "to@example.com", AdminInviteEmailData{CompanyName: "Acme", Role: "owner", AcceptURL: "https://example.com/accept"})
	if err != nil {
		t.Fatalf("SendAdminInvite() returned unexpected error: %v", err)
	}
	if fake.lastReq.Category != "transactional" || fake.lastReq.Priority != "normal" || fake.lastReq.Type != "ADMIN_INVITE" {
		t.Errorf("category/priority/type = %q/%q/%q, want transactional/normal/ADMIN_INVITE", fake.lastReq.Category, fake.lastReq.Priority, fake.lastReq.Type)
	}
	if fake.lastReq.TenantKey != "vane-saas" {
		t.Errorf("TenantKey = %q, want %q", fake.lastReq.TenantKey, "vane-saas")
	}
}

func TestSendPasswordReset_MapsCategoryPriorityType(t *testing.T) {
	fake := &fakeNotificationServiceClient{}
	s := newTestSender(t, fake)

	err := s.SendPasswordReset(t.Context(), "to@example.com", PasswordResetEmailData{CompanyName: "Acme", ResetURL: "https://example.com/reset"})
	if err != nil {
		t.Fatalf("SendPasswordReset() returned unexpected error: %v", err)
	}
	if fake.lastReq.Category != "transactional" || fake.lastReq.Priority != "critical" || fake.lastReq.Type != "PASSWORD_RESET" {
		t.Errorf("category/priority/type = %q/%q/%q, want transactional/critical/PASSWORD_RESET", fake.lastReq.Category, fake.lastReq.Priority, fake.lastReq.Type)
	}
}

func TestSendSignupVerification_MapsCategoryPriorityType(t *testing.T) {
	fake := &fakeNotificationServiceClient{}
	s := newTestSender(t, fake)

	err := s.SendSignupVerification(t.Context(), "to@example.com", SignupVerificationEmailData{TenantName: "Acme", VerifyURL: "https://example.com/verify"})
	if err != nil {
		t.Fatalf("SendSignupVerification() returned unexpected error: %v", err)
	}
	if fake.lastReq.Category != "transactional" || fake.lastReq.Priority != "critical" || fake.lastReq.Type != "SIGNUP_VERIFICATION" {
		t.Errorf("category/priority/type = %q/%q/%q, want transactional/critical/SIGNUP_VERIFICATION", fake.lastReq.Category, fake.lastReq.Priority, fake.lastReq.Type)
	}
}

func TestSendIncidentOpened_MapsCategoryPriorityType(t *testing.T) {
	fake := &fakeNotificationServiceClient{}
	s := newTestSender(t, fake)

	err := s.SendIncidentOpened(t.Context(), "to@example.com", IncidentOpenedEmailData{TenantName: "Acme", ServiceName: "API", IncidentTitle: "Down", Severity: "critical", DashboardURL: "https://example.com/i/1"})
	if err != nil {
		t.Fatalf("SendIncidentOpened() returned unexpected error: %v", err)
	}
	if fake.lastReq.Category != "transactional" || fake.lastReq.Priority != "normal" || fake.lastReq.Type != "INCIDENT_OPENED" {
		t.Errorf("category/priority/type = %q/%q/%q, want transactional/normal/INCIDENT_OPENED", fake.lastReq.Category, fake.lastReq.Priority, fake.lastReq.Type)
	}
}

func TestSendIncidentResolved_MapsCategoryPriorityType(t *testing.T) {
	fake := &fakeNotificationServiceClient{}
	s := newTestSender(t, fake)

	err := s.SendIncidentResolved(t.Context(), "to@example.com", IncidentResolvedEmailData{TenantName: "Acme", ServiceName: "API", IncidentTitle: "Down", Severity: "critical", DashboardURL: "https://example.com/i/1"})
	if err != nil {
		t.Fatalf("SendIncidentResolved() returned unexpected error: %v", err)
	}
	if fake.lastReq.Category != "transactional" || fake.lastReq.Priority != "normal" || fake.lastReq.Type != "INCIDENT_RESOLVED" {
		t.Errorf("category/priority/type = %q/%q/%q, want transactional/normal/INCIDENT_RESOLVED", fake.lastReq.Category, fake.lastReq.Priority, fake.lastReq.Type)
	}
}

func TestSendWeeklyDigest_MapsCategoryPriorityType(t *testing.T) {
	fake := &fakeNotificationServiceClient{}
	s := newTestSender(t, fake)

	err := s.SendWeeklyDigest(t.Context(), "to@example.com", WeeklyDigestEmailData{TenantName: "Acme", UptimePercent: 99.9, IncidentsOpened: 1, IncidentsResolved: 1, PeriodStart: "2026-09-01", PeriodEnd: "2026-09-07"})
	if err != nil {
		t.Fatalf("SendWeeklyDigest() returned unexpected error: %v", err)
	}
	if fake.lastReq.Category != "marketing" || fake.lastReq.Priority != "low" || fake.lastReq.Type != "WEEKLY_DIGEST" {
		t.Errorf("category/priority/type = %q/%q/%q, want marketing/low/WEEKLY_DIGEST", fake.lastReq.Category, fake.lastReq.Priority, fake.lastReq.Type)
	}
}

func TestIdempotencyKey_SameContent_SameKey(t *testing.T) {
	a := idempotencyKey("to@example.com", "subject", "<p>body</p>")
	b := idempotencyKey("to@example.com", "subject", "<p>body</p>")
	if a != b {
		t.Errorf("idempotencyKey() = %q and %q, want equal for identical content", a, b)
	}
}

func TestIdempotencyKey_DifferentContent_DifferentKey(t *testing.T) {
	a := idempotencyKey("to@example.com", "subject", "<p>body v1</p>")
	b := idempotencyKey("to@example.com", "subject", "<p>body v2</p>")
	if a == b {
		t.Errorf("idempotencyKey() = %q for both, want different keys for different content", a)
	}
}

func TestIdempotencyKey_DifferentRecipient_DifferentKey(t *testing.T) {
	a := idempotencyKey("a@example.com", "subject", "<p>body</p>")
	b := idempotencyKey("b@example.com", "subject", "<p>body</p>")
	if a == b {
		t.Errorf("idempotencyKey() = %q for both, want different keys for different recipients", a)
	}
}

func TestSendSignupVerification_SubjectAndBodyReachClient(t *testing.T) {
	fake := &fakeNotificationServiceClient{}
	s := newTestSender(t, fake)

	if err := s.SendSignupVerification(t.Context(), "to@example.com", SignupVerificationEmailData{TenantName: "Acme", VerifyURL: "https://example.com/verify"}); err != nil {
		t.Fatalf("SendSignupVerification() returned unexpected error: %v", err)
	}
	if fake.lastReq.Subject != "Verify your email for Acme" {
		t.Errorf("Subject = %q, want %q", fake.lastReq.Subject, "Verify your email for Acme")
	}
	if fake.lastReq.RecipientEmail != "to@example.com" {
		t.Errorf("RecipientEmail = %q, want %q", fake.lastReq.RecipientEmail, "to@example.com")
	}
	if fake.lastReq.HTMLBody == "" {
		t.Error("HTMLBody is empty, want rendered template content")
	}
	if fake.lastReq.IdempotencyKey == "" {
		t.Error("IdempotencyKey is empty, want a derived key")
	}
}

func TestSend_ClientFailure_PropagatedUnwrapped(t *testing.T) {
	sentinel := errors.New("boom")
	fake := &fakeNotificationServiceClient{err: sentinel}
	s := newTestSender(t, fake)

	err := s.SendAdminInvite(t.Context(), "to@example.com", AdminInviteEmailData{CompanyName: "Acme", Role: "owner", AcceptURL: "https://example.com/accept"})
	if !errors.Is(err, sentinel) {
		t.Errorf("SendAdminInvite() error = %v, want errors.Is to match the underlying client error", err)
	}
}
