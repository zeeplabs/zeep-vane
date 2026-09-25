package email

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"go.uber.org/zap"
)

// NotificationServiceSender implements Sender for the saas deployment mode
// (AD-034): it renders the same local templates *Service uses, but never
// consults email_providers/GetActiveProvider - delivery always goes
// through zeep-notification-service, authenticated with a single platform
// credential, never a per-tenant one.
type NotificationServiceSender struct {
	client    NotificationServiceClient
	tenantKey string
	logger    *zap.Logger
	templates *templates
}

// NewNotificationServiceSender builds a NotificationServiceSender. It
// returns an error only if the embedded templates fail to parse - the same
// fail-fast-at-boot check NewService performs.
func NewNotificationServiceSender(client NotificationServiceClient, tenantKey string, logger *zap.Logger) (*NotificationServiceSender, error) {
	tmpls, err := parseTemplates()
	if err != nil {
		return nil, err
	}
	return &NotificationServiceSender{client: client, tenantKey: tenantKey, logger: logger, templates: tmpls}, nil
}

var _ Sender = (*NotificationServiceSender)(nil)

// idempotencyKey derives a deterministic key from the content that would be
// sent: a genuine duplicate call (same recipient+subject+html) dedupes at
// zeep-notification-service; a legitimate resend (e.g. a new verification
// token embedded in the URL) naturally produces a different body and
// therefore a different key, with no extra dedup logic needed on vane's
// side.
func idempotencyKey(recipient, subject, htmlBody string) string {
	sum := sha256.Sum256([]byte(recipient + "|" + subject + "|" + htmlBody))
	return hex.EncodeToString(sum[:])
}

func (s *NotificationServiceSender) send(ctx context.Context, to, subject, category, priority, notificationType, htmlBody, textBody string) error {
	req := NotificationServiceRequest{
		TenantKey:      s.tenantKey,
		Category:       category,
		Priority:       priority,
		Type:           notificationType,
		RecipientEmail: to,
		Subject:        subject,
		HTMLBody:       htmlBody,
		TextBody:       textBody,
		IdempotencyKey: idempotencyKey(to, subject, htmlBody),
	}
	return s.client.Send(ctx, req)
}

func (s *NotificationServiceSender) SendAdminInvite(ctx context.Context, to string, data AdminInviteEmailData) error {
	htmlBody, textBody, err := s.templates.renderAdminInvite(data)
	if err != nil {
		return err
	}
	subject := fmt.Sprintf("You've been invited to join %s", data.CompanyName)
	return s.send(ctx, to, subject, "transactional", "normal", "ADMIN_INVITE", htmlBody, textBody)
}

func (s *NotificationServiceSender) SendPasswordReset(ctx context.Context, to string, data PasswordResetEmailData) error {
	htmlBody, textBody, err := s.templates.renderPasswordReset(data)
	if err != nil {
		return err
	}
	subject := fmt.Sprintf("Reset your %s password", data.CompanyName)
	return s.send(ctx, to, subject, "transactional", "critical", "PASSWORD_RESET", htmlBody, textBody)
}

func (s *NotificationServiceSender) SendSignupVerification(ctx context.Context, to string, data SignupVerificationEmailData) error {
	htmlBody, textBody, err := s.templates.renderSignupVerification(data)
	if err != nil {
		return err
	}
	subject := "Verify your email"
	if data.TenantName != "" {
		subject = fmt.Sprintf("Verify your email for %s", data.TenantName)
	}
	return s.send(ctx, to, subject, "transactional", "critical", "SIGNUP_VERIFICATION", htmlBody, textBody)
}

func (s *NotificationServiceSender) SendIncidentOpened(ctx context.Context, to string, data IncidentOpenedEmailData) error {
	htmlBody, textBody, err := s.templates.renderIncidentOpened(data)
	if err != nil {
		return err
	}
	subject := fmt.Sprintf("Incident opened: %s", data.IncidentTitle)
	return s.send(ctx, to, subject, "transactional", "normal", "INCIDENT_OPENED", htmlBody, textBody)
}

func (s *NotificationServiceSender) SendIncidentResolved(ctx context.Context, to string, data IncidentResolvedEmailData) error {
	htmlBody, textBody, err := s.templates.renderIncidentResolved(data)
	if err != nil {
		return err
	}
	subject := fmt.Sprintf("Incident resolved: %s", data.IncidentTitle)
	return s.send(ctx, to, subject, "transactional", "normal", "INCIDENT_RESOLVED", htmlBody, textBody)
}

func (s *NotificationServiceSender) SendWeeklyDigest(ctx context.Context, to string, data WeeklyDigestEmailData) error {
	htmlBody, textBody, err := s.templates.renderWeeklyDigest(data)
	if err != nil {
		return err
	}
	subject := "Weekly digest"
	if data.TenantName != "" {
		subject = fmt.Sprintf("Weekly digest for %s", data.TenantName)
	}
	return s.send(ctx, to, subject, "marketing", "low", "WEEKLY_DIGEST", htmlBody, textBody)
}
