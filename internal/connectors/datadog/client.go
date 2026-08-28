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
	"strings"
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

// SLOSummary is a minimal SLO identity, used to let an admin pick an SLO by
// name when linking it to a service (I14) without fetching its full status.
type SLOSummary struct {
	ID   string
	Name string
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
					ID         string `json:"id"`
					Attributes struct {
						// Name: [Provável], not live-verified like Status/
						// Thresholds/ID above (see SLOStatus doc) - inferred
						// from the official client's SLOResponseData shape
						// (github.com/DataDog/datadog-api-client-go,
						// model_slo_response_data.go: flat Name/Thresholds
						// fields), which this search response's "attributes"
						// object mirrors for Status/Thresholds. Re-verify
						// against a real account before relying on this in
						// production.
						Name   string `json:"name"`
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

	resp, err := c.get(ctx, endpoint)
	if err != nil {
		return SLOStatus{}, err
	}
	defer resp.Body.Close()

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

// SearchSLOs searches for SLOs by free-text name (I14), reusing the same
// sloSearchPath as FetchSLOStatus but with a name filter instead of
// id:<sloID>. Returns an empty slice (not ErrNotFound) when nothing
// matches - unlike FetchSLOStatus, an empty result here is a normal,
// expected outcome of a search, not a lookup failure.
//
// A free-text query (no ":") is wrapped in *…* wildcards: Datadog's
// slo/search matches whole tokens (split on non-alphanumerics), not
// arbitrary substrings, so an unwrapped query like "atewa" against an SLO
// named "...-gateway-..." returns zero results even though the admin
// typed a real fragment of the name. Confirmed live against the Datadog
// API: "gateway" (whole token) matches, "atewa" (mid-token) doesn't,
// "*atewa*" does.
//
// A query already using Datadog's field-filter syntax (e.g. "id:<sloID>",
// used by the frontend's fetchSLOName to resolve a linked SLO's display
// name - web/src/features/integrations/hooks.ts) must NOT be wrapped:
// "*id:<sloID>*" is not a valid filter and matches nothing, confirmed
// live. Presence of ":" is what distinguishes the two callers.
func (c *Client) SearchSLOs(ctx context.Context, query string) ([]SLOSummary, error) {
	wildcardQuery := query
	if wildcardQuery != "" && !strings.Contains(wildcardQuery, ":") {
		wildcardQuery = "*" + wildcardQuery + "*"
	}
	endpoint := fmt.Sprintf("%s%s?query=%s", c.baseURL, sloSearchPath, url.QueryEscape(wildcardQuery))

	resp, err := c.get(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var parsed sloSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("datadog: failed to decode response: %w", err)
	}

	slos := parsed.Data.Attributes.SLOs
	summaries := make([]SLOSummary, 0, len(slos))
	for _, slo := range slos {
		summaries = append(summaries, SLOSummary{ID: slo.Data.ID, Name: slo.Data.Attributes.Name})
	}

	return summaries, nil
}

// ValidateCredentials checks that the client's API/App key pair is valid
// and has SLO read permission, without requiring a specific SLO ID -
// SP-01.2 must be checkable at connect time, before any service/SLO has
// been configured yet. It performs a minimal SLO search call and only
// inspects the outcome; the response body's content is irrelevant here.
func (c *Client) ValidateCredentials(ctx context.Context) error {
	endpoint := fmt.Sprintf("%s%s?page_size=1", c.baseURL, sloSearchPath)

	resp, err := c.get(ctx, endpoint)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

// get issues an authenticated GET against endpoint and classifies the
// outcome into vane's typed connector errors. On success (200) it returns
// the response with its body still open for the caller to decode.
func (c *Client) get(ctx context.Context, endpoint string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("datadog: failed to build request: %w", err)
	}
	req.Header.Set("DD-API-KEY", c.apiKey)
	req.Header.Set("DD-APPLICATION-KEY", c.appKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if isTimeout(err) {
			return nil, ErrTimeout
		}
		return nil, fmt.Errorf("datadog: request failed: %w", err)
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		resp.Body.Close()
		return nil, ErrUnauthorized
	case resp.StatusCode >= http.StatusInternalServerError:
		resp.Body.Close()
		return nil, ErrServer
	case resp.StatusCode != http.StatusOK:
		resp.Body.Close()
		return nil, fmt.Errorf("datadog: unexpected status %d", resp.StatusCode)
	}

	return resp, nil
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
