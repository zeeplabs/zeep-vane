package openai

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/llm"
)

func newTestClient(server *httptest.Server) *Client {
	return &Client{
		apiKey:     "test-api-key",
		model:      "gpt-4o-mini",
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 2 * time.Second},
	}
}

func TestValidateCredentials_ValidKey_ReturnsNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != modelsPath {
			t.Errorf("request path = %q, want %q", r.URL.Path, modelsPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-api-key" {
			t.Errorf("Authorization header = %q, want %q", got, "Bearer test-api-key")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer server.Close()

	client := newTestClient(server)
	if err := client.ValidateCredentials(t.Context()); err != nil {
		t.Errorf("ValidateCredentials() returned unexpected error: %v", err)
	}
}

func TestValidateCredentials_InvalidKey_401_ReturnsErrUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided","type":"invalid_request_error"}}`))
	}))
	defer server.Close()

	client := newTestClient(server)
	err := client.ValidateCredentials(t.Context())
	if !errors.Is(err, llm.ErrUnauthorized) {
		t.Errorf("ValidateCredentials() error = %v, want llm.ErrUnauthorized", err)
	}
}

func TestValidateCredentials_Forbidden_403_ReturnsErrUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	client := newTestClient(server)
	err := client.ValidateCredentials(t.Context())
	if !errors.Is(err, llm.ErrUnauthorized) {
		t.Errorf("ValidateCredentials() error = %v, want llm.ErrUnauthorized", err)
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
		model:      "gpt-4o-mini",
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 10 * time.Millisecond},
	}

	err := client.ValidateCredentials(t.Context())
	if !errors.Is(err, llm.ErrTimeout) {
		t.Errorf("ValidateCredentials() error = %v, want llm.ErrTimeout", err)
	}
}

func TestValidateCredentials_ServerError_ReturnsErrServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := newTestClient(server)
	err := client.ValidateCredentials(t.Context())
	if !errors.Is(err, llm.ErrServer) {
		t.Errorf("ValidateCredentials() error = %v, want llm.ErrServer", err)
	}
}

func TestComplete_ValidPrompt_PostsCorrectRequestAndParsesResponse(t *testing.T) {
	var gotBody chatCompletionRequest
	var gotAuth, gotContentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != chatCompletionsPath {
			t.Errorf("request path = %q, want %q", r.URL.Path, chatCompletionsPath)
		}
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","choices":[{"index":0,"message":{"role":"assistant","content":"All good here."},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	client := newTestClient(server)
	out, err := client.Complete(t.Context(), "system prompt text", "user prompt text")
	if err != nil {
		t.Fatalf("Complete() returned unexpected error: %v", err)
	}

	if gotAuth != "Bearer test-api-key" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer test-api-key")
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type header = %q, want %q", gotContentType, "application/json")
	}
	if gotBody.Model != "gpt-4o-mini" {
		t.Errorf("Model = %q, want %q", gotBody.Model, "gpt-4o-mini")
	}
	if gotBody.MaxTokens != maxCompletionTokens {
		t.Errorf("MaxTokens = %d, want %d", gotBody.MaxTokens, maxCompletionTokens)
	}
	if len(gotBody.Messages) != 2 || gotBody.Messages[0].Role != "system" || gotBody.Messages[0].Content != "system prompt text" {
		t.Errorf("Messages[0] = %+v, want role=system content=%q", gotBody.Messages[0], "system prompt text")
	}
	if len(gotBody.Messages) != 2 || gotBody.Messages[1].Role != "user" || gotBody.Messages[1].Content != "user prompt text" {
		t.Errorf("Messages[1] = %+v, want role=user content=%q", gotBody.Messages[1], "user prompt text")
	}
	if out != "All good here." {
		t.Errorf("Complete() = %q, want %q", out, "All good here.")
	}
}

func TestComplete_Unauthorized_401_ReturnsErrUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := newTestClient(server)
	_, err := client.Complete(t.Context(), "system", "user")
	if !errors.Is(err, llm.ErrUnauthorized) {
		t.Errorf("Complete() error = %v, want llm.ErrUnauthorized", err)
	}
}

func TestComplete_ServerError_ReturnsErrServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := newTestClient(server)
	_, err := client.Complete(t.Context(), "system", "user")
	if !errors.Is(err, llm.ErrServer) {
		t.Errorf("Complete() error = %v, want llm.ErrServer", err)
	}
}

func TestComplete_Timeout_ReturnsErrTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &Client{
		apiKey:     "test-api-key",
		model:      "gpt-4o-mini",
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 10 * time.Millisecond},
	}

	_, err := client.Complete(t.Context(), "system", "user")
	if !errors.Is(err, llm.ErrTimeout) {
		t.Errorf("Complete() error = %v, want llm.ErrTimeout", err)
	}
}

func TestComplete_MalformedJSONResponse_ReturnsWrappedError_NotPanic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{not valid json`))
	}))
	defer server.Close()

	client := newTestClient(server)
	_, err := client.Complete(t.Context(), "system", "user")
	if err == nil {
		t.Fatal("Complete() returned nil error for malformed JSON response, want a wrapped error")
	}
}

func TestComplete_NoChoicesInResponse_ReturnsError_NotPanic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","choices":[]}`))
	}))
	defer server.Close()

	client := newTestClient(server)
	_, err := client.Complete(t.Context(), "system", "user")
	if err == nil {
		t.Fatal("Complete() returned nil error for a response with no choices, want an error")
	}
}
