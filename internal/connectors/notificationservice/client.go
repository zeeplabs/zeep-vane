// Package notificationservice implements vane's
// email.NotificationServiceClient against zeep-notification-service, the
// single platform-wide channel SaaS transactional email goes through
// (AD-034) - never a per-tenant credential like email_providers.
package notificationservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/email"
)

const (
	sendPath       = "/v1/notifications"
	defaultTimeout = 10 * time.Second
)

// Client sends email through zeep-notification-service's content mode -
// html/text already rendered by the caller, never a template registered on
// that service.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewClient builds a Client authenticated with apiKey, talking to baseURL
// (zeep-notification-service's own host, e.g.
// https://notifications.zeeptecnologia.com.br).
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
}

// sendRequestBody mirrors zeep-notification-service's documented
// POST /v1/notifications content-mode request shape.
type sendRequestBody struct {
	Channel   string          `json:"channel"`
	TenantID  string          `json:"tenant_id"`
	Category  string          `json:"category"`
	Priority  string          `json:"priority"`
	Type      string          `json:"type"`
	Recipient sendRecipient   `json:"recipient"`
	Content   sendContentBody `json:"content"`
}

type sendRecipient struct {
	Email string `json:"email"`
}

type sendContentBody struct {
	Subject  string `json:"subject"`
	HTMLBody string `json:"html_body"`
	TextBody string `json:"text_body"`
}

// Send delivers req through POST {baseURL}/v1/notifications in content
// mode. zeep-notification-service returns 202 on accepted.
func (c *Client) Send(ctx context.Context, req email.NotificationServiceRequest) error {
	body := sendRequestBody{
		Channel:  "email",
		TenantID: req.TenantKey,
		Category: req.Category,
		Priority: req.Priority,
		Type:     req.Type,
		Recipient: sendRecipient{
			Email: req.RecipientEmail,
		},
		Content: sendContentBody{
			Subject:  req.Subject,
			HTMLBody: req.HTMLBody,
			TextBody: req.TextBody,
		},
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("notificationservice: failed to encode send request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+sendPath, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("notificationservice: failed to build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Idempotency-Key", req.IdempotencyKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if isTimeout(err) {
			return email.ErrTimeout
		}
		return fmt.Errorf("notificationservice: request failed: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusAccepted:
		return nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return email.ErrUnauthorized
	case resp.StatusCode >= http.StatusInternalServerError:
		return email.ErrServer
	default:
		// 400/409/413/422 - malformed payload, an idempotency key reused
		// with a different body, an oversized body, or a suppressed
		// recipient. None of these are expected in normal operation (every
		// payload here is generated internally, never from direct user
		// input), so the body is embedded only for logging, never
		// surfaced to an end user.
		problemBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("notificationservice: unexpected status %d: %s", resp.StatusCode, problemBody)
	}
}

// isTimeout reports whether err represents a client-side timeout (either
// the context deadline or the http.Client's own Timeout firing).
func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
