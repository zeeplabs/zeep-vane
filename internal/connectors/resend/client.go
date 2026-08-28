// Package resend implements vane's email.Provider for Resend, sending mail
// and validating credentials via Resend's API.
package resend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/email"
)

const (
	defaultBaseURL = "https://api.resend.com"
	sendPath       = "/emails"
	apiKeysPath    = "/api-keys"
	defaultTimeout = 10 * time.Second
)

// Client sends email through Resend's API using an API key.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewClient builds a Client authenticated with apiKey, talking to the real
// Resend API.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		baseURL:    defaultBaseURL,
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
}

// sendRequestBody mirrors Resend's documented POST /emails request shape
// (resend.com/docs/api-reference/emails/send-email): `from` is a single
// "Name <email>" string, not split name/email fields.
type sendRequestBody struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Subject string `json:"subject"`
	HTML    string `json:"html,omitempty"`
	Text    string `json:"text,omitempty"`
}

// Send delivers msg through Resend's POST /emails endpoint. Resend returns
// 200 with `{"id": "..."}` on success.
func (c *Client) Send(ctx context.Context, msg email.Message) error {
	from := msg.FromEmail
	if msg.FromName != "" {
		from = fmt.Sprintf("%s <%s>", msg.FromName, msg.FromEmail)
	}

	body := sendRequestBody{
		From:    from,
		To:      msg.To,
		Subject: msg.Subject,
		HTML:    msg.HTMLBody,
		Text:    msg.TextBody,
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("resend: failed to encode send request: %w", err)
	}

	resp, err := c.post(ctx, c.baseURL+sendPath, encoded)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

// ValidateCredentials checks that the client's API key is valid, without
// sending any email. It performs a minimal list-api-keys lookup and only
// inspects the outcome; the response body's content is irrelevant here.
//
// Resend classifies an invalid key as 403 ("The API key used was invalid"
// - resend.com/docs/api-reference/introduction, confirmed via Resend's
// docs during T5), not 401 as originally assumed in design.md; 401 is
// reserved for a missing Authorization header or a send-only key hitting a
// non-send endpoint (resend.com/docs/api-reference/errors). c.do
// classifies both 401 and 403 as email.ErrUnauthorized, so this method
// correctly rejects an invalid key either way.
func (c *Client) ValidateCredentials(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+apiKeysPath, nil)
	if err != nil {
		return fmt.Errorf("resend: failed to build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

// post issues an authenticated POST with a JSON body against endpoint and
// classifies the outcome into vane's shared email typed errors. On success
// it returns the response with its body still open for the caller.
func (c *Client) post(ctx context.Context, endpoint string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("resend: failed to build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	return c.do(req)
}

// do executes req and classifies the outcome into vane's shared email
// typed errors.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if isTimeout(err) {
			return nil, email.ErrTimeout
		}
		return nil, fmt.Errorf("resend: request failed: %w", err)
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		resp.Body.Close()
		return nil, email.ErrUnauthorized
	case resp.StatusCode >= http.StatusInternalServerError:
		resp.Body.Close()
		return nil, email.ErrServer
	case resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices:
		return resp, nil
	default:
		resp.Body.Close()
		return nil, fmt.Errorf("resend: unexpected status %d", resp.StatusCode)
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
