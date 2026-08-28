package sendgrid

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
		if r.URL.Path != scopesPath {
			t.Errorf("request path = %q, want %q", r.URL.Path, scopesPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-api-key" {
			t.Errorf("Authorization header = %q, want %q", got, "Bearer test-api-key")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"scopes":["mail.send"]}`))
	}))
	defer server.Close()

	client := newTestClient(server)
	if err := client.ValidateCredentials(t.Context()); err != nil {
		t.Errorf("ValidateCredentials() returned unexpected error: %v", err)
	}
}

func TestValidateCredentials_InvalidKey_ReturnsErrUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":[{"message":"unauthorized"}]}`))
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
		w.WriteHeader(http.StatusAccepted)
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
	if len(gotBody.Personalizations) != 1 || len(gotBody.Personalizations[0].To) != 1 || gotBody.Personalizations[0].To[0].Email != msg.To {
		t.Errorf("Personalizations = %+v, want To=%q", gotBody.Personalizations, msg.To)
	}
	if gotBody.From.Email != msg.FromEmail || gotBody.From.Name != msg.FromName {
		t.Errorf("From = %+v, want Email=%q Name=%q", gotBody.From, msg.FromEmail, msg.FromName)
	}
	if gotBody.Subject != msg.Subject {
		t.Errorf("Subject = %q, want %q", gotBody.Subject, msg.Subject)
	}
	if len(gotBody.Content) != 2 || gotBody.Content[0].Value != msg.TextBody || gotBody.Content[1].Value != msg.HTMLBody {
		t.Errorf("Content = %+v, want text=%q html=%q", gotBody.Content, msg.TextBody, msg.HTMLBody)
	}
}

func TestSend_Unauthorized_ReturnsErrUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
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
		w.WriteHeader(http.StatusAccepted)
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
