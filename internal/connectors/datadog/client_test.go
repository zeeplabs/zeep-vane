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

// validHistoryResponseBody mirrors the shape confirmed live against a real
// Datadog SLO in this session (see sloHistoryResponse doc in client.go).
const validHistoryResponseBody = `{
  "data": {
    "overall": {
      "sli_value": 99.90405942762656,
      "state": "ok"
    },
    "series": {
      "denominator": {
        "sum": 34142
      }
    },
    "thresholds": {
      "30d": {"target": 99.5, "timeframe": "30d"}
    }
  }
}`

func TestFetchSLOStatus_ValidResponse_ReturnsNormalizedStatus(t *testing.T) {
	var gotFromTs, gotToTs string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("DD-API-KEY"); got != "test-api-key" {
			t.Errorf("DD-API-KEY header = %q, want %q", got, "test-api-key")
		}
		if got := r.Header.Get("DD-APPLICATION-KEY"); got != "test-app-key" {
			t.Errorf("DD-APPLICATION-KEY header = %q, want %q", got, "test-app-key")
		}
		if got := r.URL.Path; got != "/api/v1/slo/34709d4e377558da8630d86b309b732b/history" {
			t.Errorf("path = %q, want the SLO history path", got)
		}
		gotFromTs = r.URL.Query().Get("from_ts")
		gotToTs = r.URL.Query().Get("to_ts")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(validHistoryResponseBody))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	from := time.Unix(1000, 0)
	to := time.Unix(2000, 0)
	status, err := client.FetchSLOStatus(t.Context(), "34709d4e377558da8630d86b309b732b", from, to)
	if err != nil {
		t.Fatalf("FetchSLOStatus() returned unexpected error: %v", err)
	}

	if gotFromTs != "1000" {
		t.Errorf("from_ts = %q, want %q", gotFromTs, "1000")
	}
	if gotToTs != "2000" {
		t.Errorf("to_ts = %q, want %q", gotToTs, "2000")
	}
	if status.State != "ok" {
		t.Errorf("State = %q, want %q", status.State, "ok")
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
	if status.RequestCount != 34142 {
		t.Errorf("RequestCount = %v, want %v", status.RequestCount, 34142)
	}
	wantBudget := status.SLI - status.Target
	if status.ErrorBudgetRemaining != wantBudget {
		t.Errorf("ErrorBudgetRemaining = %v, want %v", status.ErrorBudgetRemaining, wantBudget)
	}
}

func TestFetchSLOStatus_MissingThresholds_ReturnsZeroTargetNoCrash(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"overall":{"sli_value":100,"state":"ok"},"series":{"denominator":{"sum":5}}}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	status, err := client.FetchSLOStatus(t.Context(), "any-slo-id", time.Unix(0, 0), time.Unix(1, 0))
	if err != nil {
		t.Fatalf("FetchSLOStatus() returned unexpected error: %v", err)
	}
	if status.Target != 0 {
		t.Errorf("Target = %v, want 0", status.Target)
	}
	if status.Timeframe != "" {
		t.Errorf("Timeframe = %q, want empty", status.Timeframe)
	}
}

func TestFetchSLOStatus_MultipleThresholds_PicksSameOneEveryTime(t *testing.T) {
	body := `{"data":{"overall":{"sli_value":99,"state":"ok"},"series":{"denominator":{"sum":100}},
	  "thresholds":{"90d":{"target":99.9,"timeframe":"90d"},"30d":{"target":99.5,"timeframe":"30d"},"7d":{"target":99,"timeframe":"7d"}}}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	client := newTestClient(t, server)

	var gotTimeframes []string
	for i := 0; i < 20; i++ {
		status, err := client.FetchSLOStatus(t.Context(), "any-slo-id", time.Unix(0, 0), time.Unix(1, 0))
		if err != nil {
			t.Fatalf("FetchSLOStatus() returned unexpected error: %v", err)
		}
		gotTimeframes = append(gotTimeframes, status.Timeframe)
	}

	first := gotTimeframes[0]
	for i, tf := range gotTimeframes {
		if tf != first {
			t.Fatalf("call %d picked timeframe %q, want the same %q every call (non-deterministic map iteration)", i, tf, first)
		}
	}
	// "30d" sorts before "7d" and "90d" lexicographically - documents which
	// one wins, not that it's the "right" one (arbitrary but stable, see
	// client.go's comment above the sort).
	if first != "30d" {
		t.Errorf("picked timeframe = %q, want %q (lexicographically first key)", first, "30d")
	}
}

func TestFetchSLOStatus_MalformedState_PassedThroughAsIs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"overall":{"sli_value":0,"state":"totally-unknown"},"series":{"denominator":{"sum":0}}}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	status, err := client.FetchSLOStatus(t.Context(), "any-slo-id", time.Unix(0, 0), time.Unix(1, 0))
	if err != nil {
		t.Fatalf("FetchSLOStatus() returned unexpected error: %v", err)
	}
	if status.State != "totally-unknown" {
		t.Errorf("State = %q, want passthrough of %q", status.State, "totally-unknown")
	}
	if status.RequestCount != 0 {
		t.Errorf("RequestCount = %v, want 0", status.RequestCount)
	}
}

func TestFetchSLOStatus_Unauthorized_ReturnsErrUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":["Unauthorized"]}`))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.FetchSLOStatus(t.Context(), "any-slo-id", time.Unix(0, 0), time.Unix(1, 0))
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("FetchSLOStatus() error = %v, want ErrUnauthorized", err)
	}
}

func TestFetchSLOStatus_NotFound_ReturnsErrNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":["Not Found"]}`))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.FetchSLOStatus(t.Context(), "any-slo-id", time.Unix(0, 0), time.Unix(1, 0))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("FetchSLOStatus() error = %v, want ErrNotFound", err)
	}
}

func TestFetchSLOStatus_Timeout_ReturnsErrTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(validHistoryResponseBody))
	}))
	defer server.Close()

	client := &Client{
		apiKey:     "test-api-key",
		appKey:     "test-app-key",
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 10 * time.Millisecond},
	}

	_, err := client.FetchSLOStatus(t.Context(), "any-slo-id", time.Unix(0, 0), time.Unix(1, 0))
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
	_, err := client.FetchSLOStatus(t.Context(), "any-slo-id", time.Unix(0, 0), time.Unix(1, 0))
	if !errors.Is(err, ErrServer) {
		t.Errorf("FetchSLOStatus() error = %v, want ErrServer", err)
	}
}

// searchResponseBody mirrors validSearchResponseBody but with a name
// attribute added, as SearchSLOs needs (see the [Provável] note on
// sloSearchResponse in client.go).
const searchResponseBody = `{
  "data": {
    "attributes": {
      "slos": [
        {
          "data": {
            "id": "34709d4e377558da8630d86b309b732b",
            "attributes": {
              "name": "Checkout latência p95",
              "status": {
                "error_budget_remaining": 80.812,
                "sli": 99.90405942762656,
                "state": "ok"
              },
              "thresholds": [
                {"target": 99.5, "timeframe": "30d"}
              ]
            }
          }
        }
      ]
    }
  }
}`

func TestSearchSLOs_ValidResponse_ReturnsSummaries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Wrapped in *…* wildcards: Datadog's slo/search matches whole
		// tokens, not substrings, so an unwrapped query would miss a
		// name fragment that isn't a full token (see SearchSLOs' doc).
		if got := r.URL.Query().Get("query"); got != "*checkout*" {
			t.Errorf("query param = %q, want %q", got, "*checkout*")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(searchResponseBody))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	summaries, err := client.SearchSLOs(t.Context(), "checkout")
	if err != nil {
		t.Fatalf("SearchSLOs() returned unexpected error: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("len(summaries) = %d, want 1", len(summaries))
	}
	if summaries[0].ID != "34709d4e377558da8630d86b309b732b" {
		t.Errorf("summaries[0].ID = %q, want %q", summaries[0].ID, "34709d4e377558da8630d86b309b732b")
	}
	if summaries[0].Name != "Checkout latência p95" {
		t.Errorf("summaries[0].Name = %q, want %q", summaries[0].Name, "Checkout latência p95")
	}
}

// TestSearchSLOs_FacetFilterQuery_NotWildcarded guards against a
// regression where every query got wrapped in *…* wildcards
// unconditionally: "*id:<sloID>*" is not a valid Datadog filter and
// matches nothing (confirmed live), breaking fetchSLOName's SLO-name
// lookup by id (web/src/features/integrations/hooks.ts), which is the
// only caller that sends a facet-filter query rather than free text.
func TestSearchSLOs_FacetFilterQuery_NotWildcarded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("query"); got != "id:abc123" {
			t.Errorf("query param = %q, want %q", got, "id:abc123")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(searchResponseBody))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	if _, err := client.SearchSLOs(t.Context(), "id:abc123"); err != nil {
		t.Fatalf("SearchSLOs() returned unexpected error: %v", err)
	}
}

func TestSearchSLOs_NoMatches_ReturnsEmptySliceNotError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"attributes":{"slos":[]}}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	summaries, err := client.SearchSLOs(t.Context(), "nonexistent")
	if err != nil {
		t.Fatalf("SearchSLOs() returned unexpected error: %v", err)
	}
	if len(summaries) != 0 {
		t.Errorf("len(summaries) = %d, want 0", len(summaries))
	}
}

func TestSearchSLOs_Unauthorized_ReturnsErrUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":["Unauthorized"]}`))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.SearchSLOs(t.Context(), "checkout")
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("SearchSLOs() error = %v, want ErrUnauthorized", err)
	}
}

func TestSearchSLOs_ServerError_ReturnsErrServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errors":["Internal Server Error"]}`))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.SearchSLOs(t.Context(), "checkout")
	if !errors.Is(err, ErrServer) {
		t.Errorf("SearchSLOs() error = %v, want ErrServer", err)
	}
}
