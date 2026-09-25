package notificationservice

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/email"
)

func newTestClient(server *httptest.Server) *Client {
	return &Client{
		baseURL:    server.URL,
		apiKey:     "test-api-key",
		httpClient: &http.Client{Timeout: 2 * time.Second},
	}
}

func testRequest() email.NotificationServiceRequest {
	return email.NotificationServiceRequest{
		TenantKey:      "vane-saas",
		Category:       "transactional",
		Priority:       "critical",
		Type:           "SIGNUP_VERIFICATION",
		RecipientEmail: "new-owner@example.com",
		Subject:        "Confirm your account",
		HTMLBody:       "<p>Confirm</p>",
		TextBody:       "Confirm",
		IdempotencyKey: "abc123",
	}
}

func TestSend_Accepted_ReturnsNil(t *testing.T) {
	var gotBody sendRequestBody
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != sendPath {
			t.Errorf("request path = %q, want %q", r.URL.Path, sendPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-api-key" {
			t.Errorf("Authorization header = %q, want %q", got, "Bearer test-api-key")
		}
		if got := r.Header.Get("Idempotency-Key"); got != "abc123" {
			t.Errorf("Idempotency-Key header = %q, want %q", got, "abc123")
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := newTestClient(server)
	if err := client.Send(t.Context(), testRequest()); err != nil {
		t.Errorf("Send() returned unexpected error: %v", err)
	}

	if gotBody.Channel != "email" {
		t.Errorf("Channel = %q, want %q", gotBody.Channel, "email")
	}
	if gotBody.TenantID != "vane-saas" {
		t.Errorf("TenantID = %q, want %q", gotBody.TenantID, "vane-saas")
	}
	if gotBody.Recipient.Email != "new-owner@example.com" {
		t.Errorf("Recipient.Email = %q, want %q", gotBody.Recipient.Email, "new-owner@example.com")
	}
	if gotBody.Content.Subject != "Confirm your account" {
		t.Errorf("Content.Subject = %q, want %q", gotBody.Content.Subject, "Confirm your account")
	}
}

func TestSend_401_ReturnsErrUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := newTestClient(server)
	err := client.Send(t.Context(), testRequest())
	if !errors.Is(err, email.ErrUnauthorized) {
		t.Errorf("Send() error = %v, want email.ErrUnauthorized", err)
	}
}

func TestSend_403_ReturnsErrUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	client := newTestClient(server)
	err := client.Send(t.Context(), testRequest())
	if !errors.Is(err, email.ErrUnauthorized) {
		t.Errorf("Send() error = %v, want email.ErrUnauthorized", err)
	}
}

func TestSend_Timeout_ReturnsErrTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := &Client{
		baseURL:    server.URL,
		apiKey:     "test-api-key",
		httpClient: &http.Client{Timeout: 5 * time.Millisecond},
	}
	err := client.Send(t.Context(), testRequest())
	if !errors.Is(err, email.ErrTimeout) {
		t.Errorf("Send() error = %v, want email.ErrTimeout", err)
	}
}

func TestSend_5xx_ReturnsErrServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	client := newTestClient(server)
	err := client.Send(t.Context(), testRequest())
	if !errors.Is(err, email.ErrServer) {
		t.Errorf("Send() error = %v, want email.ErrServer", err)
	}
}

func TestSend_422_ReturnsGenericError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"type":"validation_error","detail":"recipient suppressed"}`))
	}))
	defer server.Close()

	client := newTestClient(server)
	err := client.Send(t.Context(), testRequest())
	if err == nil {
		t.Fatal("Send() returned nil error, want an error for status 422")
	}
	if errors.Is(err, email.ErrUnauthorized) || errors.Is(err, email.ErrTimeout) || errors.Is(err, email.ErrServer) {
		t.Errorf("Send() error = %v, want a generic (non-typed) error for status 422", err)
	}
}

func TestSend_APIKeyNeverLogged(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"type":"validation_error","detail":"bad payload"}`))
	}))
	defer server.Close()

	client := newTestClient(server)
	err := client.Send(t.Context(), testRequest())
	if err == nil {
		t.Fatal("Send() returned nil error, want an error")
	}
	if got := err.Error(); got == "" {
		t.Fatal("Send() error message is empty")
	} else if strings.Contains(got, "test-api-key") {
		t.Errorf("Send() error message leaked the API key: %q", got)
	}
}
