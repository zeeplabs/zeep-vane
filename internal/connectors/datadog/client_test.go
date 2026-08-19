package datadog

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	return &Client{
		apiKey:     "test-api-key",
		appKey:     "test-app-key",
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 2 * time.Second},
	}
}

// validSearchResponseBody mirrors the shape confirmed live against a real
// Datadog SLO in this session (see SLOStatus doc in client.go).
const validSearchResponseBody = `{
  "data": {
    "attributes": {
      "slos": [
        {
          "data": {
            "attributes": {
              "status": {
                "error_budget_remaining": 80.812,
                "sli": 99.90405942762656,
                "state": "ok"
              },
              "thresholds": [
                {"target": 99.5, "timeframe": "30d"}
              ]
            },
            "id": "34709d4e377558da8630d86b309b732b"
          }
        }
      ]
    }
  }
}`

func TestFetchSLOStatus_ValidResponse_ReturnsNormalizedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("DD-API-KEY"); got != "test-api-key" {
			t.Errorf("DD-API-KEY header = %q, want %q", got, "test-api-key")
		}
		if got := r.Header.Get("DD-APPLICATION-KEY"); got != "test-app-key" {
			t.Errorf("DD-APPLICATION-KEY header = %q, want %q", got, "test-app-key")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(validSearchResponseBody))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	status, err := client.FetchSLOStatus(t.Context(), "34709d4e377558da8630d86b309b732b")
	if err != nil {
		t.Fatalf("FetchSLOStatus() returned unexpected error: %v", err)
	}

	if status.State != "ok" {
		t.Errorf("State = %q, want %q", status.State, "ok")
	}
	if status.ErrorBudgetRemaining != 80.812 {
		t.Errorf("ErrorBudgetRemaining = %v, want %v", status.ErrorBudgetRemaining, 80.812)
	}
	if status.SLI != 99.90405942762656 {
		t.Errorf("SLI = %v, want %v", status.SLI, 99.90405942762656)
	}
	if status.Target != 99.5 {
		t.Errorf("Target = %v, want %v", status.Target, 99.5)
	}
	if status.Timeframe != "30d" {
		t.Errorf("Timeframe = %q, want %q", status.Timeframe, "30d")
	}
}

func TestFetchSLOStatus_Unauthorized_ReturnsErrUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":["Unauthorized"]}`))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.FetchSLOStatus(t.Context(), "any-slo-id")
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("FetchSLOStatus() error = %v, want ErrUnauthorized", err)
	}
}

func TestFetchSLOStatus_Timeout_ReturnsErrTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(validSearchResponseBody))
	}))
	defer server.Close()

	client := &Client{
		apiKey:     "test-api-key",
		appKey:     "test-app-key",
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 10 * time.Millisecond},
	}

	_, err := client.FetchSLOStatus(t.Context(), "any-slo-id")
	if !errors.Is(err, ErrTimeout) {
		t.Errorf("FetchSLOStatus() error = %v, want ErrTimeout", err)
	}
}

func TestFetchSLOStatus_ServerError_ReturnsErrServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errors":["Internal Server Error"]}`))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.FetchSLOStatus(t.Context(), "any-slo-id")
	if !errors.Is(err, ErrServer) {
		t.Errorf("FetchSLOStatus() error = %v, want ErrServer", err)
	}
}
