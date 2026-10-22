package datadog

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
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
        "sum": 34142.0
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

// TestFetchSLOStatus_FloatDenominatorSum_DecodesWithoutError is a
// regression guard for a bug that reached production: Datadog serializes
// series.denominator.sum as a JSON float (e.g. 45.0) even though it's
// always a whole request count, but SLOStatus.RequestCount decoded
// straight into int64 - every real Datadog response failed with
// "cannot unmarshal number 45.0 into ... of type int64". Every existing
// fixture in this file used a bare integer literal like "sum":5, which
// decodes into int64 fine and never exercised this - only an explicit
// float literal (with a decimal point) reproduces what Datadog's API
// actually returns.
func TestFetchSLOStatus_FloatDenominatorSum_DecodesWithoutError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"overall":{"sli_value":100,"state":"ok"},"series":{"denominator":{"sum":45.0}}}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	status, err := client.FetchSLOStatus(t.Context(), "any-slo-id", time.Unix(0, 0), time.Unix(1, 0))
	if err != nil {
		t.Fatalf("FetchSLOStatus() returned unexpected error: %v", err)
	}
	if status.RequestCount != 45 {
		t.Errorf("RequestCount = %d, want 45", status.RequestCount)
	}
}

// TestFetchSLOStatus_FloatDenominatorSum_RoundsPrecisionNoise asserts a
// float with tiny precision noise around a whole number (plausible from
// real floating-point arithmetic on Datadog's side) still rounds to the
// right integer instead of truncating down by 1.
func TestFetchSLOStatus_FloatDenominatorSum_RoundsPrecisionNoise(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"overall":{"sli_value":100,"state":"ok"},"series":{"denominator":{"sum":44.99999999999999}}}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	status, err := client.FetchSLOStatus(t.Context(), "any-slo-id", time.Unix(0, 0), time.Unix(1, 0))
	if err != nil {
		t.Fatalf("FetchSLOStatus() returned unexpected error: %v", err)
	}
	if status.RequestCount != 45 {
		t.Errorf("RequestCount = %d, want 45 (rounded, not truncated)", status.RequestCount)
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

// errorTrackingResponseBody mirrors the verified
// POST /api/v2/error-tracking/issues/search response shape (design.md's
// Components section): data[0]'s relationships.issue.data.id looks up the
// matching included[] entry for error_type/error_message.
const errorTrackingResponseBody = `{
  "data": [
    {
      "id": "occ-1",
      "attributes": {"total_count": 82},
      "relationships": {"issue": {"data": {"id": "issue-1", "type": "issue"}}}
    }
  ],
  "included": [
    {
      "id": "issue-1",
      "type": "issue",
      "attributes": {
        "error_type": "MongooseError",
        "error_message": "Operation psychology_chat_sessions.aggregate() buffering timed out after 10000ms",
        "service": "psychology-chat-svc"
      }
    }
  ]
}`

func TestSearchErrorTrackingIssues_ValidResponse_ReturnsTopCauseHint(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("DD-API-KEY"); got != "test-api-key" {
			t.Errorf("DD-API-KEY header = %q, want %q", got, "test-api-key")
		}
		if got := r.Header.Get("DD-APPLICATION-KEY"); got != "test-app-key" {
			t.Errorf("DD-APPLICATION-KEY header = %q, want %q", got, "test-app-key")
		}
		if got := r.Method; got != http.MethodPost {
			t.Errorf("method = %q, want POST", got)
		}
		if got := r.URL.Path; got != "/api/v2/error-tracking/issues/search" {
			t.Errorf("path = %q, want the error tracking search path", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(errorTrackingResponseBody))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	from := time.Unix(1000, 0)
	to := time.Unix(2000, 0)
	hint, found, err := client.SearchErrorTrackingIssues(t.Context(), "psychology-chat-svc", "production", from, to)
	if err != nil {
		t.Fatalf("SearchErrorTrackingIssues() returned unexpected error: %v", err)
	}
	if !found {
		t.Fatalf("found = false, want true")
	}
	if hint.ErrorType != "MongooseError" {
		t.Errorf("ErrorType = %q, want %q", hint.ErrorType, "MongooseError")
	}
	wantMessage := "Operation psychology_chat_sessions.aggregate() buffering timed out after 10000ms"
	if hint.ErrorMessage != wantMessage {
		t.Errorf("ErrorMessage = %q, want %q", hint.ErrorMessage, wantMessage)
	}

	data, _ := gotBody["data"].(map[string]any)
	attrs, _ := data["attributes"].(map[string]any)
	if got := attrs["query"]; got != "service:psychology-chat-svc AND env:production" {
		t.Errorf("query = %v, want %q", got, "service:psychology-chat-svc AND env:production")
	}
	if got := attrs["track"]; got != "trace" {
		t.Errorf("track = %v, want %q", got, "trace")
	}
	if got := attrs["order_by"]; got != "TOTAL_COUNT" {
		t.Errorf("order_by = %v, want %q", got, "TOTAL_COUNT")
	}
	if got := attrs["from"]; got != float64(from.UnixMilli()) {
		t.Errorf("from = %v, want %v", got, from.UnixMilli())
	}
	if got := attrs["to"]; got != float64(to.UnixMilli()) {
		t.Errorf("to = %v, want %v", got, to.UnixMilli())
	}
}

func TestSearchErrorTrackingIssues_EmptyData_ReturnsNotFoundNotError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"included":[]}`))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	hint, found, err := client.SearchErrorTrackingIssues(t.Context(), "svc", "production", time.Unix(0, 0), time.Unix(1, 0))
	if err != nil {
		t.Fatalf("SearchErrorTrackingIssues() returned unexpected error: %v", err)
	}
	if found {
		t.Errorf("found = true, want false")
	}
	if hint != (CauseHint{}) {
		t.Errorf("hint = %+v, want zero value", hint)
	}
}

func TestSearchErrorTrackingIssues_Unauthorized_ReturnsErrUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":["Unauthorized"]}`))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, _, err := client.SearchErrorTrackingIssues(t.Context(), "svc", "production", time.Unix(0, 0), time.Unix(1, 0))
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("SearchErrorTrackingIssues() error = %v, want ErrUnauthorized", err)
	}
}

func TestSearchErrorTrackingIssues_Timeout_ReturnsErrTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(errorTrackingResponseBody))
	}))
	defer server.Close()

	client := &Client{
		apiKey:     "test-api-key",
		appKey:     "test-app-key",
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 10 * time.Millisecond},
	}

	_, _, err := client.SearchErrorTrackingIssues(t.Context(), "svc", "production", time.Unix(0, 0), time.Unix(1, 0))
	if !errors.Is(err, ErrTimeout) {
		t.Errorf("SearchErrorTrackingIssues() error = %v, want ErrTimeout", err)
	}
}

func TestSearchErrorTrackingIssues_ServerError_ReturnsErrServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errors":["Internal Server Error"]}`))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, _, err := client.SearchErrorTrackingIssues(t.Context(), "svc", "production", time.Unix(0, 0), time.Unix(1, 0))
	if !errors.Is(err, ErrServer) {
		t.Errorf("SearchErrorTrackingIssues() error = %v, want ErrServer", err)
	}
}

func TestSearchSLOs_SingleServiceTag_PopulatesSLOTypeAndServiceTag(t *testing.T) {
	body := `{"data":{"attributes":{"slos":[{"data":{"id":"abc","attributes":{
		"name":"Checkout latência p95","slo_type":"metric","service_tags":["service:checkout-svc"]
	}}}]}}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
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
	if summaries[0].SLOType != "metric" {
		t.Errorf("SLOType = %q, want %q", summaries[0].SLOType, "metric")
	}
	if summaries[0].ServiceTag != "service:checkout-svc" {
		t.Errorf("ServiceTag = %q, want %q", summaries[0].ServiceTag, "service:checkout-svc")
	}
}

func TestSearchSLOs_ZeroServiceTags_ServiceTagEmpty(t *testing.T) {
	body := `{"data":{"attributes":{"slos":[{"data":{"id":"abc","attributes":{
		"name":"Compra de Consulta","slo_type":"metric","service_tags":[]
	}}}]}}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	summaries, err := client.SearchSLOs(t.Context(), "compra")
	if err != nil {
		t.Fatalf("SearchSLOs() returned unexpected error: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("len(summaries) = %d, want 1", len(summaries))
	}
	if summaries[0].ServiceTag != "" {
		t.Errorf("ServiceTag = %q, want empty (0 service_tags)", summaries[0].ServiceTag)
	}
}

func TestSearchSLOs_MultipleServiceTags_ServiceTagEmpty(t *testing.T) {
	body := `{"data":{"attributes":{"slos":[{"data":{"id":"abc","attributes":{
		"name":"Compra de Consulta","slo_type":"metric","service_tags":["service:checkout-svc","service:payments-svc"]
	}}}]}}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	summaries, err := client.SearchSLOs(t.Context(), "compra")
	if err != nil {
		t.Fatalf("SearchSLOs() returned unexpected error: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("len(summaries) = %d, want 1", len(summaries))
	}
	if summaries[0].ServiceTag != "" {
		t.Errorf("ServiceTag = %q, want empty (2 service_tags, ambiguous)", summaries[0].ServiceTag)
	}
}

func TestSearchErrorTrackingIssues_LongErrorMessage_TruncatedTo500Chars(t *testing.T) {
	longMessage := strings.Repeat("a", 600)
	body := `{"data":[{"id":"occ-1","attributes":{"total_count":1},"relationships":{"issue":{"data":{"id":"issue-1","type":"issue"}}}}],` +
		`"included":[{"id":"issue-1","type":"issue","attributes":{"error_type":"E","error_message":"` + longMessage + `"}}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	hint, found, err := client.SearchErrorTrackingIssues(t.Context(), "svc", "production", time.Unix(0, 0), time.Unix(1, 0))
	if err != nil {
		t.Fatalf("SearchErrorTrackingIssues() returned unexpected error: %v", err)
	}
	if !found {
		t.Fatalf("found = false, want true")
	}
	if len(hint.ErrorMessage) != 500 {
		t.Errorf("len(ErrorMessage) = %d, want 500", len(hint.ErrorMessage))
	}
}

// windowsTestEnd is the end instant every FetchSLOWindows test asks for:
// minute-aligned, so 60s buckets line up with end-long/end-short exactly.
var windowsTestEnd = time.Unix(1_760_000_040, 0)

// historyBucketsBody renders a real-shaped /history response (float counts,
// ms timestamps, 60s interval unless overridden) whose buckets start at
// firstStart and advance by interval seconds. numerators/denominators are
// per-bucket good/total counts.
func historyBucketsBody(t *testing.T, firstStart time.Time, interval int, numerators, denominators []float64) string {
	t.Helper()
	times := make([]float64, len(denominators))
	var numSum, denSum float64
	for i := range denominators {
		times[i] = float64(firstStart.Add(time.Duration(i*interval) * time.Second).UnixMilli())
		numSum += numerators[i]
		denSum += denominators[i]
	}
	body := map[string]any{
		"data": map[string]any{
			"overall": map[string]any{"sli_value": 99.1, "state": "ok"},
			"series": map[string]any{
				"interval":    float64(interval),
				"times":       times,
				"numerator":   map[string]any{"sum": numSum, "values": numerators},
				"denominator": map[string]any{"sum": denSum, "values": denominators},
			},
			"thresholds": map[string]any{
				"30d": map[string]any{"target": 99.5, "timeframe": "30d"},
			},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	// json.Marshal writes whole floats as integers (45, not 45.0), unlike
	// Datadog. Rewrite the interval as a float literal; fixtures that need
	// float counts use fractional values, which Marshal keeps as floats.
	return strings.Replace(string(raw), fmt.Sprintf(`"interval":%d`, interval), fmt.Sprintf(`"interval":%d.0`, interval), 1)
}

func TestFetchSLOWindows_60sSeriesWithPartialFirstBucket_SumsShortAndLong(t *testing.T) {
	const buckets = 61 // 1 extra leading bucket + 60 one-minute buckets
	numerators := make([]float64, buckets)
	denominators := make([]float64, buckets)
	// Bucket 0 starts before end-1h: Datadog's partial first bucket, here
	// zero-valued like the live replay - but give it traffic anyway to
	// prove it's dropped by timestamp, not by being zero.
	numerators[0], denominators[0] = 7.0, 9.0
	for i := 1; i < buckets; i++ {
		numerators[i], denominators[i] = 10.0, 10.0
	}
	// Two errors 30 min ago (long window only) and 1 + 2 errors in the
	// last 5 buckets (short window).
	numerators[30] = 8.0
	numerators[57] = 9.0
	numerators[60] = 7.999999999999999 // float noise, rounds to 8
	end := windowsTestEnd
	firstStart := end.Add(-time.Hour - time.Minute)

	var gotFromTs, gotToTs string
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		gotFromTs = r.URL.Query().Get("from_ts")
		gotToTs = r.URL.Query().Get("to_ts")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(historyBucketsBody(t, firstStart, 60, numerators, denominators)))
	}))
	defer server.Close()

	got, err := newTestClient(t, server).FetchSLOWindows(t.Context(), "slo-1", end, 5*time.Minute, time.Hour)
	if err != nil {
		t.Fatalf("FetchSLOWindows() returned unexpected error: %v", err)
	}

	if calls != 1 {
		t.Errorf("history calls = %d, want 1 (no fallback for a 60s series)", calls)
	}
	if want := fmt.Sprint(end.Add(-time.Hour - time.Minute).Unix()); gotFromTs != want {
		t.Errorf("from_ts = %q, want %q (end - long - one bucket)", gotFromTs, want)
	}
	if want := fmt.Sprint(end.Unix()); gotToTs != want {
		t.Errorf("to_ts = %q, want %q", gotToTs, want)
	}
	if !got.HasSeries {
		t.Error("HasSeries = false, want true")
	}
	if want := (WindowCounts{Requests: 600, Errors: 5}); got.Long != want {
		t.Errorf("Long = %+v, want %+v", got.Long, want)
	}
	if want := (WindowCounts{Requests: 50, Errors: 3}); got.Short != want {
		t.Errorf("Short = %+v, want %+v", got.Short, want)
	}
	if got.Target != 99.5 || got.Timeframe != "30d" || got.State != "ok" {
		t.Errorf("Target/Timeframe/State = %v/%q/%q, want 99.5/30d/ok", got.Target, got.Timeframe, got.State)
	}
}

func TestFetchSLOWindows_ErrorsExceedRequests_Clamped(t *testing.T) {
	end := windowsTestEnd
	firstStart := end.Add(-3 * time.Minute)
	// Bucket 0: numerator > denominator (negative errors) -> clamped to 0.
	// Bucket 1: negative numerator (errors > requests) -> clamped to 5.
	// Bucket 2: normal, 1 error.
	numerators := []float64{12.0, -3.0, 3.5}
	denominators := []float64{10.0, 5.0, 4.5}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(historyBucketsBody(t, firstStart, 60, numerators, denominators)))
	}))
	defer server.Close()

	got, err := newTestClient(t, server).FetchSLOWindows(t.Context(), "slo-1", end, 5*time.Minute, time.Hour)
	if err != nil {
		t.Fatalf("FetchSLOWindows() returned unexpected error: %v", err)
	}

	// 10 + 5 + round(4.5)=5 requests; 0 + 5 + (5 - round(3.5)=4)=1 errors.
	want := WindowCounts{Requests: 20, Errors: 6}
	if got.Long != want {
		t.Errorf("Long = %+v, want %+v", got.Long, want)
	}
	if got.Short != want {
		t.Errorf("Short = %+v, want %+v", got.Short, want)
	}
}

func TestFetchSLOWindows_CoarseInterval_FallsBackToSecondCallForShort(t *testing.T) {
	end := windowsTestEnd
	firstStart := end.Add(-time.Hour - time.Minute)
	// 13 five-minute buckets: Long must come from the sums, not buckets.
	numerators := make([]float64, 13)
	denominators := make([]float64, 13)
	for i := range denominators {
		numerators[i], denominators[i] = 98.0, 100.0
	}
	shortBody := `{
  "data": {
    "overall": {"sli_value": 90.0, "state": "breached"},
    "series": {
      "interval": 300.0,
      "numerator": {"sum": 45.0},
      "denominator": {"sum": 50.0}
    },
    "thresholds": {"30d": {"target": 99.5, "timeframe": "30d"}}
  }
}`

	var froms, tos []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		froms = append(froms, r.URL.Query().Get("from_ts"))
		tos = append(tos, r.URL.Query().Get("to_ts"))
		if len(froms) == 1 {
			_, _ = w.Write([]byte(historyBucketsBody(t, firstStart, 300, numerators, denominators)))
			return
		}
		_, _ = w.Write([]byte(shortBody))
	}))
	defer server.Close()

	got, err := newTestClient(t, server).FetchSLOWindows(t.Context(), "slo-1", end, 5*time.Minute, time.Hour)
	if err != nil {
		t.Fatalf("FetchSLOWindows() returned unexpected error: %v", err)
	}

	if len(froms) != 2 {
		t.Fatalf("history calls = %d, want 2 (coarse interval triggers fallback)", len(froms))
	}
	if want := fmt.Sprint(end.Add(-5 * time.Minute).Unix()); froms[1] != want {
		t.Errorf("second call from_ts = %q, want %q (end - short)", froms[1], want)
	}
	if want := fmt.Sprint(end.Unix()); tos[1] != want {
		t.Errorf("second call to_ts = %q, want %q (end)", tos[1], want)
	}
	if !got.HasSeries {
		t.Error("HasSeries = false, want true")
	}
	if want := (WindowCounts{Requests: 1300, Errors: 26}); got.Long != want {
		t.Errorf("Long = %+v, want %+v (first call's sums)", got.Long, want)
	}
	if want := (WindowCounts{Requests: 50, Errors: 5}); got.Short != want {
		t.Errorf("Short = %+v, want %+v (second call's sums)", got.Short, want)
	}
	// State/target come from the first (long) call.
	if got.State != "ok" || got.Target != 99.5 {
		t.Errorf("State/Target = %q/%v, want ok/99.5", got.State, got.Target)
	}
}

func TestFetchSLOWindows_SumsWithoutBuckets_FallsBackToSecondCall(t *testing.T) {
	end := windowsTestEnd
	longBody := `{"data":{"overall":{"sli_value":99.0,"state":"ok"},"series":{"numerator":{"sum":990.0},"denominator":{"sum":1000.0}},"thresholds":{"30d":{"target":99.5,"timeframe":"30d"}}}}`
	shortBody := `{"data":{"overall":{"sli_value":95.0,"state":"ok"},"series":{"numerator":{"sum":76.0},"denominator":{"sum":80.0}}}}`

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte(longBody))
			return
		}
		_, _ = w.Write([]byte(shortBody))
	}))
	defer server.Close()

	got, err := newTestClient(t, server).FetchSLOWindows(t.Context(), "slo-1", end, 5*time.Minute, time.Hour)
	if err != nil {
		t.Fatalf("FetchSLOWindows() returned unexpected error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("history calls = %d, want 2", calls)
	}
	if !got.HasSeries {
		t.Error("HasSeries = false, want true")
	}
	if want := (WindowCounts{Requests: 1000, Errors: 10}); got.Long != want {
		t.Errorf("Long = %+v, want %+v", got.Long, want)
	}
	if want := (WindowCounts{Requests: 80, Errors: 4}); got.Short != want {
		t.Errorf("Short = %+v, want %+v", got.Short, want)
	}
}

func TestFetchSLOWindows_MissingSeries_HasSeriesFalseWithStateAndTarget(t *testing.T) {
	// Monitor-based SLO: overall + thresholds, no series at all.
	body := `{
  "data": {
    "overall": {"sli_value": 98.2, "state": "breached"},
    "thresholds": {"7d": {"target": 99.0, "timeframe": "7d"}}
  }
}`
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	got, err := newTestClient(t, server).FetchSLOWindows(t.Context(), "slo-1", windowsTestEnd, 5*time.Minute, time.Hour)
	if err != nil {
		t.Fatalf("FetchSLOWindows() returned unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("history calls = %d, want 1 (no fallback without counts)", calls)
	}
	if got.HasSeries {
		t.Error("HasSeries = true, want false")
	}
	if got.State != "breached" || got.Target != 99.0 || got.Timeframe != "7d" {
		t.Errorf("State/Target/Timeframe = %q/%v/%q, want breached/99/7d", got.State, got.Target, got.Timeframe)
	}
	if got.Short != (WindowCounts{}) || got.Long != (WindowCounts{}) {
		t.Errorf("Short/Long = %+v/%+v, want zero", got.Short, got.Long)
	}
}

func TestFetchSLOWindows_ServerError_ReturnsErrServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	_, err := newTestClient(t, server).FetchSLOWindows(t.Context(), "slo-1", windowsTestEnd, 5*time.Minute, time.Hour)
	if !errors.Is(err, ErrServer) {
		t.Errorf("error = %v, want ErrServer", err)
	}
}

func TestFetchSLOWindows_FallbackCallServerError_ReturnsErrServer(t *testing.T) {
	longBody := `{"data":{"overall":{"state":"ok"},"series":{"interval":300.0,"numerator":{"sum":10.0},"denominator":{"sum":10.0}}}}`
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte(longBody))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	_, err := newTestClient(t, server).FetchSLOWindows(t.Context(), "slo-1", windowsTestEnd, 5*time.Minute, time.Hour)
	if !errors.Is(err, ErrServer) {
		t.Errorf("error = %v, want ErrServer", err)
	}
}

func TestFetchSLOWindows_MalformedBody_ReturnsDecodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"series":{"times":"not-an-array"}}}`))
	}))
	defer server.Close()

	_, err := newTestClient(t, server).FetchSLOWindows(t.Context(), "slo-1", windowsTestEnd, 5*time.Minute, time.Hour)
	if err == nil || !strings.Contains(err.Error(), "failed to decode response") {
		t.Errorf("error = %v, want a wrapped decode error", err)
	}
}

// rateLimitTestBody is a monitor-based (no series) response, so
// FetchSLOWindows makes exactly one call and logs rate limits exactly once.
const rateLimitTestBody = `{"data":{"overall":{"sli_value":99.9,"state":"ok"}}}`

func TestFetchSLOWindows_RateLimitLow_LogsWarning(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "100")
		w.Header().Set("X-RateLimit-Remaining", "9")
		_, _ = w.Write([]byte(rateLimitTestBody))
	}))
	defer server.Close()

	client := newTestClient(t, server).WithLogger(zap.New(core))
	if _, err := client.FetchSLOWindows(t.Context(), "slo-1", windowsTestEnd, 5*time.Minute, time.Hour); err != nil {
		t.Fatalf("FetchSLOWindows() returned unexpected error: %v", err)
	}

	warnings := logs.FilterLevelExact(zapcore.WarnLevel).All()
	if len(warnings) != 1 {
		t.Fatalf("warn logs = %d, want 1", len(warnings))
	}
	if got := warnings[0].ContextMap()["rate_limit_remaining"]; got != int64(9) {
		t.Errorf("rate_limit_remaining = %v, want 9", got)
	}
	for _, entry := range logs.All() {
		for _, field := range entry.Context {
			if strings.Contains(field.String, "test-api-key") || strings.Contains(field.String, "test-app-key") {
				t.Errorf("log field %q leaks a credential", field.Key)
			}
		}
	}
}

func TestFetchSLOWindows_RateLimitHealthy_LogsDebugOnly(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "100")
		w.Header().Set("X-RateLimit-Remaining", "50")
		_, _ = w.Write([]byte(rateLimitTestBody))
	}))
	defer server.Close()

	client := newTestClient(t, server).WithLogger(zap.New(core))
	if _, err := client.FetchSLOWindows(t.Context(), "slo-1", windowsTestEnd, 5*time.Minute, time.Hour); err != nil {
		t.Fatalf("FetchSLOWindows() returned unexpected error: %v", err)
	}

	if n := logs.FilterLevelExact(zapcore.WarnLevel).Len(); n != 0 {
		t.Errorf("warn logs = %d, want 0", n)
	}
	if n := logs.FilterLevelExact(zapcore.DebugLevel).Len(); n != 1 {
		t.Errorf("debug logs = %d, want 1", n)
	}
}
