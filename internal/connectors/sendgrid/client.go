// Package sendgrid implements vane's email.Provider for SendGrid, sending
// mail and validating credentials via SendGrid's v3 API.
package sendgrid

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
	defaultBaseURL = "https://api.sendgrid.com"
	sendPath       = "/v3/mail/send"
	scopesPath     = "/v3/scopes"
	defaultTimeout = 10 * time.Second
)

// Client sends email through SendGrid's v3 API using an API key.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewClient builds a Client authenticated with apiKey, talking to the real
// SendGrid API.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		baseURL:    defaultBaseURL,
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
}

// sendRequestBody mirrors SendGrid's documented v3 mail/send request shape
// (docs.sendgrid.com/api-reference/mail-send/mail-send).
type sendRequestBody struct {
	Personalizations []personalization `json:"personalizations"`
	From             emailAddress      `json:"from"`
	Subject          string            `json:"subject"`
	Content          []content         `json:"content"`
}

type personalization struct {
	To []emailAddress `json:"to"`
}

type emailAddress struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

type content struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// Send delivers msg through SendGrid's v3 mail/send endpoint. SendGrid
// returns 202 with an empty body on success.
func (c *Client) Send(ctx context.Context, msg email.Message) error {
	body := sendRequestBody{
		Personalizations: []personalization{{To: []emailAddress{{Email: msg.To}}}},
		From:             emailAddress{Email: msg.FromEmail, Name: msg.FromName},
		Subject:          msg.Subject,
		Content: []content{
			{Type: "text/plain", Value: msg.TextBody},
			{Type: "text/html", Value: msg.HTMLBody},
		},
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("sendgrid: failed to encode send request: %w", err)
	}

	resp, err := c.post(ctx, c.baseURL+sendPath, encoded)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

// ValidateCredentials checks that the client's API key is valid, without
// sending any email. It performs a minimal scopes lookup and only inspects
// the outcome; the response body's content is irrelevant here.
func (c *Client) ValidateCredentials(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+scopesPath, nil)
	if err != nil {
		return fmt.Errorf("sendgrid: failed to build request: %w", err)
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
		return nil, fmt.Errorf("sendgrid: failed to build request: %w", err)
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
		return nil, fmt.Errorf("sendgrid: request failed: %w", err)
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
		return nil, fmt.Errorf("sendgrid: unexpected status %d", resp.StatusCode)
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
