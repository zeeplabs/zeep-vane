// Package datadog implements vane's SLOProvider for Datadog, fetching an
// SLO's current status via the Datadog API.
package datadog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

const (
	defaultBaseURL = "https://api.datadoghq.com"
	sloSearchPath  = "/api/v1/slo/search"
	defaultTimeout = 10 * time.Second
)

// SLOStatus is vane's normalized view of a Datadog SLO's current status.
//
// SPEC_DEVIATION: design.md assumed GET /api/v1/slo/{slo_id} ("get an SLO's
// details") would return current status and error budget directly. Verified
// during T17 (per the [Incerto] flag in design.md) that it does not: the
// official generated client (github.com/DataDog/datadog-api-client-go,
// api/datadogV1/model_slo_response_data.go) shows SLOResponseData carries
// only the SLO's static definition (thresholds, query, tags, ...), no
// status/error-budget field. The status fields below were instead confirmed
// live against a real SLO in this session via the Datadog MCP
// (GET /api/v1/slo/search?query=id:<slo_id>), whose response nests exactly
// this shape under data.attributes.slos[].data.attributes.{status,thresholds}.
// Reason: search-by-id is the endpoint that actually carries current
// status; get-by-id does not.
type SLOStatus struct {
	// State is one of "ok", "warning", "breached", "no_data" (Datadog's
	// SLOState enum).
	State string
	// ErrorBudgetRemaining is the percentage (0-100) of error budget left.
	ErrorBudgetRemaining float64
	// SLI is the current service level indicator, 0-100.
	SLI float64
	// Target is the configured threshold for the primary timeframe.
	Target float64
	// Timeframe is the primary timeframe the above values apply to (e.g. "30d").
	Timeframe string
}

// SLOProvider is the contract any APM connector (Datadog today, others
// later) implements to expose current SLO status.
type SLOProvider interface {
	FetchSLOStatus(ctx context.Context, sloID string) (SLOStatus, error)
}

// Typed errors so callers (the poller's retry logic, Phase 4) can tell an
// auth failure - never worth retrying - apart from a transient one.
var (
	// ErrUnauthorized means the API key/App key is invalid or lacks SLO
	// read permission.
	ErrUnauthorized = errors.New("datadog: unauthorized (invalid or unpermitted api/app key)")
	// ErrTimeout means the request did not complete before its deadline.
	ErrTimeout = errors.New("datadog: request timed out")
	// ErrServer means Datadog returned a 5xx.
	ErrServer = errors.New("datadog: server error")
	// ErrNotFound means no SLO with the given ID exists (or is visible to
	// this key).
	ErrNotFound = errors.New("datadog: slo not found")
)

// Client calls the Datadog SLO API using an API key + Application key pair.
type Client struct {
	apiKey     string
	appKey     string
	baseURL    string
	httpClient *http.Client
}

// NewClient builds a Client authenticated with apiKey/appKey, talking to the
// real Datadog API.
func NewClient(apiKey, appKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		appKey:     appKey,
		baseURL:    defaultBaseURL,
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
}

// sloSearchResponse mirrors the subset of the confirmed
// GET /api/v1/slo/search response vane needs (see SLOStatus doc for how
// this shape was verified).
type sloSearchResponse struct {
	Data struct {
		Attributes struct {
			SLOs []struct {
				Data struct {
					Attributes struct {
						Status struct {
							ErrorBudgetRemaining float64 `json:"error_budget_remaining"`
							SLI                  float64 `json:"sli"`
							State                string  `json:"state"`
						} `json:"status"`
						Thresholds []struct {
							Target    float64 `json:"target"`
							Timeframe string  `json:"timeframe"`
						} `json:"thresholds"`
					} `json:"attributes"`
				} `json:"data"`
			} `json:"slos"`
		} `json:"attributes"`
	} `json:"data"`
}

// FetchSLOStatus fetches sloID's current status, searching by exact ID.
// Returns ErrUnauthorized on 401/403 (never retried by callers), ErrTimeout
// on a request timeout, and ErrServer on 5xx (both retried by callers, see
// the Phase 4 poller retry wrapper).
func (c *Client) FetchSLOStatus(ctx context.Context, sloID string) (SLOStatus, error) {
	endpoint := fmt.Sprintf("%s%s?query=%s", c.baseURL, sloSearchPath, url.QueryEscape("id:"+sloID))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return SLOStatus{}, fmt.Errorf("datadog: failed to build request: %w", err)
	}
	req.Header.Set("DD-API-KEY", c.apiKey)
	req.Header.Set("DD-APPLICATION-KEY", c.appKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if isTimeout(err) {
			return SLOStatus{}, ErrTimeout
		}
		return SLOStatus{}, fmt.Errorf("datadog: request failed: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return SLOStatus{}, ErrUnauthorized
	case resp.StatusCode >= http.StatusInternalServerError:
		return SLOStatus{}, ErrServer
	case resp.StatusCode != http.StatusOK:
		return SLOStatus{}, fmt.Errorf("datadog: unexpected status %d", resp.StatusCode)
	}

	var parsed sloSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return SLOStatus{}, fmt.Errorf("datadog: failed to decode response: %w", err)
	}

	slos := parsed.Data.Attributes.SLOs
	if len(slos) == 0 {
		return SLOStatus{}, ErrNotFound
	}

	attrs := slos[0].Data.Attributes
	status := SLOStatus{
		State:                attrs.Status.State,
		ErrorBudgetRemaining: attrs.Status.ErrorBudgetRemaining,
		SLI:                  attrs.Status.SLI,
	}
	if len(attrs.Thresholds) > 0 {
		status.Target = attrs.Thresholds[0].Target
		status.Timeframe = attrs.Thresholds[0].Timeframe
	}

	return status, nil
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
