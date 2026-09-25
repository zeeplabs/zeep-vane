package email

import "context"

// NotificationServiceRequest is a single email to send through
// zeep-notification-service, fully rendered and connector-agnostic - the
// same role Message plays for a per-tenant Provider (AD-034). The concrete
// implementation lives in internal/connectors/notificationservice, never
// the other way around: this package owns the contract, the connector
// package imports it, mirroring how internal/connectors/resend already
// depends on Message/Provider today.
type NotificationServiceRequest struct {
	TenantKey      string
	Category       string // "transactional" | "marketing"
	Priority       string // "critical" | "normal" | "low"
	Type           string // e.g. "SIGNUP_VERIFICATION"
	RecipientEmail string
	Subject        string
	HTMLBody       string
	TextBody       string
	IdempotencyKey string
}

// NotificationServiceClient is what NotificationServiceSender depends on to
// deliver a NotificationServiceRequest. The HTTP implementation lives in
// internal/connectors/notificationservice.Client and must return
// ErrUnauthorized, ErrTimeout, or ErrServer for the matching failure modes -
// the same three typed errors a Provider implementation already returns -
// so callers classify failures the same way regardless of channel.
type NotificationServiceClient interface {
	Send(ctx context.Context, req NotificationServiceRequest) error
}
