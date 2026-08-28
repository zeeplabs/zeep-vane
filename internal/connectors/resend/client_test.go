package resend

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/email"
)

func newTestClient(server *httptest.Server) *Client {
	return &Client{
		apiKey:     "test-api-key",
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 2 * time.Second},
	}
}

func TestValidateCredentials_ValidKey_ReturnsNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != apiKeysPath {
			t.Errorf("request path = %q, want %q", r.URL.Path, apiKeysPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-api-key" {
			t.Errorf("Authorization header = %q, want %q", got, "Bearer test-api-key")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"object":"list","has_more":false,"data":[]}`))
	}))
	defer server.Close()

	client := newTestClient(server)
	if err := client.ValidateCredentials(t.Context()); err != nil {
		t.Errorf("ValidateCredentials() returned unexpected error: %v", err)
	}
}

// TestValidateCredentials_InvalidKey_403_ReturnsErrUnauthorized asserts the
// T5 research finding: Resend returns 403 ("The API key used was invalid"),
// not 401, for a genuinely wrong/unrecognized key.
func TestValidateCredentials_InvalidKey_403_ReturnsErrUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"statusCode":403,"message":"The API key used was invalid","name":"validation_error"}`))
	}))
	defer server.Close()

	client := newTestClient(server)
	err := client.ValidateCredentials(t.Context())
	if !errors.Is(err, email.ErrUnauthorized) {
		t.Errorf("ValidateCredentials() error = %v, want email.ErrUnauthorized", err)
	}
}

// TestValidateCredentials_MissingKey_401_ReturnsErrUnauthorized covers
// Resend's other documented invalid-auth status (401 missing_api_key), so
// both codes this client can receive for an unusable key are handled.
func TestValidateCredentials_MissingKey_401_ReturnsErrUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"statusCode":401,"message":"Missing API key in the authorization header","name":"missing_api_key"}`))
	}))
	defer server.Close()

	client := newTestClient(server)
	err := client.ValidateCredentials(t.Context())
	if !errors.Is(err, email.ErrUnauthorized) {
		t.Errorf("ValidateCredentials() error = %v, want email.ErrUnauthorized", err)
	}
}

func TestValidateCredentials_Timeout_ReturnsErrTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &Client{
		apiKey:     "test-api-key",
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 10 * time.Millisecond},
	}

	err := client.ValidateCredentials(t.Context())
	if !errors.Is(err, email.ErrTimeout) {
		t.Errorf("ValidateCredentials() error = %v, want email.ErrTimeout", err)
	}
}

func TestValidateCredentials_ServerError_ReturnsErrServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := newTestClient(server)
	err := client.ValidateCredentials(t.Context())
	if !errors.Is(err, email.ErrServer) {
		t.Errorf("ValidateCredentials() error = %v, want email.ErrServer", err)
	}
}

func TestSend_ValidMessage_PostsCorrectAuthAndBody(t *testing.T) {
	var gotBody sendRequestBody
	var gotAuth, gotContentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != sendPath {
			t.Errorf("request path = %q, want %q", r.URL.Path, sendPath)
		}
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"49a3999c-0ce1-4ea6-ab68-afcd6dc2e794"}`))
	}))
	defer server.Close()

	client := newTestClient(server)
	msg := email.Message{
		To:        "invitee@example.com",
		FromEmail: "owner@example.com",
		FromName:  "Acme Owner",
		Subject:   "You're invited",
		HTMLBody:  "<p>Join Acme</p>",
		TextBody:  "Join Acme",
	}

	if err := client.Send(t.Context(), msg); err != nil {
		t.Fatalf("Send() returned unexpected error: %v", err)
	}

	if gotAuth != "Bearer test-api-key" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer test-api-key")
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type header = %q, want %q", gotContentType, "application/json")
	}
	if gotBody.From != "Acme Owner <owner@example.com>" {
		t.Errorf("From = %q, want %q", gotBody.From, "Acme Owner <owner@example.com>")
	}
	if gotBody.To != msg.To {
		t.Errorf("To = %q, want %q", gotBody.To, msg.To)
	}
	if gotBody.Subject != msg.Subject {
		t.Errorf("Subject = %q, want %q", gotBody.Subject, msg.Subject)
	}
	if gotBody.HTML != msg.HTMLBody {
		t.Errorf("HTML = %q, want %q", gotBody.HTML, msg.HTMLBody)
	}
	if gotBody.Text != msg.TextBody {
		t.Errorf("Text = %q, want %q", gotBody.Text, msg.TextBody)
	}
}

func TestSend_Unauthorized_403_ReturnsErrUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	client := newTestClient(server)
	err := client.Send(t.Context(), email.Message{To: "a@example.com"})
	if !errors.Is(err, email.ErrUnauthorized) {
		t.Errorf("Send() error = %v, want email.ErrUnauthorized", err)
	}
}

func TestSend_ServerError_ReturnsErrServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := newTestClient(server)
	err := client.Send(t.Context(), email.Message{To: "a@example.com"})
	if !errors.Is(err, email.ErrServer) {
		t.Errorf("Send() error = %v, want email.ErrServer", err)
	}
}

func TestSend_Timeout_ReturnsErrTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &Client{
		apiKey:     "test-api-key",
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 10 * time.Millisecond},
	}

	err := client.Send(t.Context(), email.Message{To: "a@example.com"})
	if !errors.Is(err, email.ErrTimeout) {
		t.Errorf("Send() error = %v, want email.ErrTimeout", err)
	}
}
