// Package datadog implements vane's SLOProvider for Datadog, fetching an
// SLO's current status via the Datadog API.
package datadog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	defaultBaseURL          = "https://api.datadoghq.com"
	sloSearchPath           = "/api/v1/slo/search"
	sloHistoryPath          = "/api/v1/slo/%s/history"
	errorTrackingSearchPath = "/api/v2/error-tracking/issues/search"
	defaultTimeout          = 10 * time.Second
	// maxCauseMessageLength bounds CauseHint.ErrorMessage before it ever
	// leaves this package - long enough for the real validation-error
	// example seen live this session (~350 chars) without truncating it,
	// short enough to bound the LLM prompt's token cost predictably
	// regardless of what a future error message looks like (design.md's
	// Tech Decisions).
	maxCauseMessageLength = 500
	// historyBucket is the bucket width Datadog returns for a 1h /history
	// window (confirmed live: 60s buckets). FetchSLOWindows asks for one
	// extra bucket before the long window so the partial first bucket can
	// be discarded, and falls back to a second call for the short window
	// whenever the returned interval is coarser than this.
	historyBucket = 60 * time.Second
)

// SLOStatus is vane's normalized view of a Datadog SLO's status over the
// window requested from FetchSLOStatus.
//
// SPEC_DEVIATION: design.md assumed GET /api/v1/slo/{slo_id} ("get an SLO's
// details") would return current status and error budget directly. Verified
// during T17 (per the [Incerto] flag in design.md) that it does not: the
// official generated client (github.com/DataDog/datadog-api-client-go,
// api/datadogV1/model_slo_response_data.go) shows SLOResponseData carries
// only the SLO's static definition (thresholds, query, tags, ...), no
// status/error-budget field.
type SLOStatus struct {
	// State is one of "ok", "warning", "breached", "no_data" (Datadog's
	// SLOState enum), as computed by Datadog for the requested window.
	State string
	// ErrorBudgetRemaining is SLI - Target, an approximation (not Datadog's
	// own exact error-budget figure, which the history endpoint doesn't
	// return) - positive when healthy, negative when breached, matching
	// Datadog's own sign convention - see AD-019 addendum 2.
	ErrorBudgetRemaining float64
	// SLI is the service level indicator for the requested window, 0-100.
	SLI float64
	// Target is the SLO's configured threshold.
	Target float64
	// Timeframe is the SLO's configured timeframe the target applies to
	// (e.g. "30d") - not the requested window.
	Timeframe string
	// RequestCount is the total request volume in the requested window,
	// used by the poller's low-volume carry-forward guard.
	RequestCount int64
}

// WindowCounts is the request/error volume of a metric-based SLO over one
// time window. Errors is always within [0, Requests].
type WindowCounts struct {
	Requests int64
	Errors   int64
}

// SLOWindows is vane's view of a Datadog SLO for burn-rate classification:
// request/error counts over a short and a long window ending at the same
// instant, plus the SLO's configured target and Datadog's own state.
// Target/Timeframe/State mean exactly what they mean on SLOStatus.
type SLOWindows struct {
	// Target is the SLO's configured threshold (0-100), 0 when the SLO
	// has no thresholds.
	Target float64
	// Timeframe is the SLO's configured timeframe the target applies to
	// (e.g. "30d") - not the requested windows.
	Timeframe string
	// State is Datadog's own "ok"/"warning"/"breached"/"no_data" for the
	// requested range - the fallback signal when HasSeries is false.
	State string
	// SLI is Datadog's own overall SLI (0-100) for the requested range
	// (the long window plus one leading bucket). It is context for the
	// analyzer when the counts can't supply one (monitor-based SLO, or no
	// traffic in the short window), never a classification input.
	SLI float64
	// Short and Long are the counts over [end-short, end) and
	// [end-long, end).
	Short WindowCounts
	Long  WindowCounts
	// HasSeries is false when Datadog returned no request counts at all
	// (monitor-based SLO): Short/Long are then zero and meaningless, and
	// callers must classify from State instead.
	HasSeries bool
}

// SLOProvider is the contract any APM connector (Datadog today, others
// later) implements to expose SLO status for a given time window.
type SLOProvider interface {
	FetchSLOStatus(ctx context.Context, sloID string, from, to time.Time) (SLOStatus, error)
	FetchSLOWindows(ctx context.Context, sloID string, end time.Time, short, long time.Duration) (SLOWindows, error)
}

// SLOSummary is a minimal SLO identity, used to let an admin pick an SLO by
// name when linking it to a service (I14) without fetching its full status.
type SLOSummary struct {
	ID   string
	Name string
	// SLOType is "metric" or "monitor", decoded from the same /slo/search
	// response already fetched - no new Datadog API call.
	SLOType string
	// ServiceTag is the SLO's single service_tags entry, "" when absent or
	// when service_tags has more than one entry (flow-type/multi-service
	// SLOs - this session's decision: no attempt to parse the query
	// string to recover a service list).
	ServiceTag string
}

// CauseHint is the narrow root-cause signal SearchErrorTrackingIssues
// returns - deliberately not the full Datadog issue (no stack trace, no
// file_path), per the root-cause-enrichment spec's data-minimization
// decision. ErrorMessage is truncated to maxCauseMessageLength before it
// ever leaves this package.
type CauseHint struct {
	ErrorType    string
	ErrorMessage string
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
	// logger is optional (nil means no logging, see log) - only
	// FetchSLOWindows' rate-limit visibility uses it today.
	logger *zap.Logger
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

// WithLogger sets the logger used for rate-limit visibility and returns c,
// so it chains off NewClient. A nil logger disables logging.
func (c *Client) WithLogger(logger *zap.Logger) *Client {
	c.logger = logger
	return c
}

// log returns c's logger, or a no-op one when none was set (every Client
// built as a struct literal, e.g. in tests, has a nil logger).
func (c *Client) log() *zap.Logger {
	if c.logger == nil {
		return zap.NewNop()
	}
	return c.logger
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
						// Name: [Likely], not live-verified like ID above
						// (see SLOStatus doc) - inferred from the official
						// client's SLOResponseData shape
						// (github.com/DataDog/datadog-api-client-go,
						// model_slo_response_data.go: flat Name field).
						// Re-verify against a real account before relying on
						// this in production.
						Name string `json:"name"`
						// SLOType/ServiceTags: fields this response already
						// carries alongside Name/ID above, per design.md's
						// root-cause-enrichment feature (RCA-01) - "metric"
						// or "monitor", and the SLO's service:-scoped tags.
						SLOType     string   `json:"slo_type"`
						ServiceTags []string `json:"service_tags"`
					} `json:"attributes"`
				} `json:"data"`
			} `json:"slos"`
		} `json:"attributes"`
	} `json:"data"`
}

// sloHistoryResponse mirrors the subset of GET /api/v1/slo/{id}/history
// vane needs. Shape confirmed live (2026-09-08) against a real SLO via
// Datadog MCP SDK discovery + execute_code - the official Go/TS client's
// declared types cover sli_value/thresholds; state is an undeclared extra
// field Datadog still returns inline, consistent with how sloSearchResponse
// already decodes /slo/search.
type sloHistoryResponse struct {
	Data struct {
		Overall struct {
			SLIValue float64 `json:"sli_value"`
			State    string  `json:"state"`
		} `json:"overall"`
		Series struct {
			// Interval is the bucket width in seconds (60 for a 1h window
			// in the live check). Decoded as float64 for the same reason
			// as Sum below - never trust Datadog to serialize a whole
			// number as a JSON integer.
			Interval float64 `json:"interval"`
			// Times holds each bucket's start timestamp, milliseconds since
			// the epoch (see bucketStart for the seconds fallback).
			Times []float64 `json:"times"`
			// Numerator is the per-bucket "good" event count of a
			// metric-based SLO; Denominator is the per-bucket total.
			Numerator   sloHistoryMetric `json:"numerator"`
			Denominator sloHistoryMetric `json:"denominator"`
		} `json:"series"`
		Thresholds map[string]struct {
			Target    float64 `json:"target"`
			Timeframe string  `json:"timeframe"`
		} `json:"thresholds"`
	} `json:"data"`
}

// sloHistoryMetric is one side (numerator or denominator) of a metric-based
// SLO's history series.
type sloHistoryMetric struct {
	// Sum is a whole request count, but Datadog serializes it as a JSON
	// float (e.g. 45.0, not 45) - decoding straight into int64 fails with
	// "cannot unmarshal number 45.0 into ... of type int64" on every real
	// response. Confirmed live in production (2026-09-08): every poll
	// against a real Datadog account failed this way, none of it caught by
	// any test, because every test's fixture response JSON was hand-written
	// with integer literals instead of the float shape Datadog's API
	// actually returns. A pointer so FetchSLOWindows can tell "no counts
	// at all" (monitor-based SLO) apart from "zero traffic".
	Sum *float64 `json:"sum"`
	// Values is the per-bucket count, aligned index-for-index with
	// Series.Times - floats for the same reason as Sum. A JSON null entry
	// decodes as 0.
	Values []float64 `json:"values"`
}

// total returns the metric's Sum when Datadog sent one, else the sum of its
// per-bucket Values, and whether either was present at all.
func (m sloHistoryMetric) total() (float64, bool) {
	if m.Sum != nil {
		return *m.Sum, true
	}
	if len(m.Values) == 0 {
		return 0, false
	}
	var total float64
	for _, v := range m.Values {
		total += v
	}
	return total, true
}

// pickThreshold returns the target/timeframe FetchSLOStatus and
// FetchSLOWindows both report. parsed.Data.Thresholds is keyed by timeframe
// (e.g. "7d", "30d") - iterating a Go map directly picked a random key on
// every call for any SLO configured with more than one threshold, making
// Target/Timeframe (and therefore ErrorBudgetRemaining) non-deterministic
// per poll. Sorting keys first makes the pick stable; which specific
// timeframe wins is otherwise arbitrary (single-threshold SLOs, the common
// case observed in this org, are unaffected either way). Returns zero
// values when the SLO has no thresholds.
func (r sloHistoryResponse) pickThreshold() (target float64, timeframe string) {
	if len(r.Data.Thresholds) == 0 {
		return 0, ""
	}
	timeframes := make([]string, 0, len(r.Data.Thresholds))
	for tf := range r.Data.Thresholds {
		timeframes = append(timeframes, tf)
	}
	sort.Strings(timeframes)
	chosen := r.Data.Thresholds[timeframes[0]]
	return chosen.Target, chosen.Timeframe
}

// FetchSLOStatus fetches sloID's status for the [from, to) window, via
// Datadog's SLO history endpoint (state computed against the SLO's own
// configured target, no threshold comparison reimplemented here - see
// AD-019). Returns ErrUnauthorized on 401/403 (never retried by callers),
// ErrNotFound on 404 (no SLO with this ID), ErrTimeout on a request
// timeout, and ErrServer on 5xx (both retried by callers, see the poller
// retry wrapper).
func (c *Client) FetchSLOStatus(ctx context.Context, sloID string, from, to time.Time) (SLOStatus, error) {
	path := fmt.Sprintf(sloHistoryPath, url.PathEscape(sloID))
	endpoint := fmt.Sprintf("%s%s?from_ts=%d&to_ts=%d", c.baseURL, path, from.Unix(), to.Unix())

	resp, err := c.get(ctx, endpoint)
	if err != nil {
		return SLOStatus{}, err
	}
	defer resp.Body.Close()

	var parsed sloHistoryResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return SLOStatus{}, fmt.Errorf("datadog: failed to decode response: %w", err)
	}

	var requests float64
	if sum := parsed.Data.Series.Denominator.Sum; sum != nil {
		requests = *sum
	}
	status := SLOStatus{
		State: parsed.Data.Overall.State,
		SLI:   parsed.Data.Overall.SLIValue,
		// math.Round, not a bare int64() truncation: Denominator.Sum is a
		// whole count Datadog happens to serialize as a float, so rounding
		// guards against any float-precision noise (e.g. 44.99999999999999)
		// still landing on the right integer instead of silently
		// undercounting by 1.
		RequestCount: int64(math.Round(requests)),
	}
	status.Target, status.Timeframe = parsed.pickThreshold()
	// Datadog's own error_budget_remaining is positive when healthy (0-100
	// scale); Target - SLI (this approximation, chosen because the history
	// endpoint doesn't return the exact figure without an explicit target
	// param - AD-019) is negative when healthy instead. SLI - Target at
	// least matches Datadog's sign convention for the inert DB-only column
	// this feeds (status_intervals.error_budget_remaining - never read by
	// any handler or frontend, see AD-019's Trade-off).
	status.ErrorBudgetRemaining = status.SLI - status.Target

	return status, nil
}

// FetchSLOWindows fetches sloID's request/error counts over the short and
// long windows ending at end, for burn-rate classification
// (poller-burn-rate-classification, BURN-17), plus the SLO's target and
// Datadog's own state.
//
// One /history call covers [end-long-historyBucket, end]: the extra
// leading bucket is requested so the partial first bucket Datadog returns
// (seen zero-valued in the live replay) can be discarded. Per bucket,
// errors = denominator - numerator clamped to [0, denominator]; Long sums
// the buckets starting at or after end-long, Short those starting at or
// after end-short.
//
// When Datadog returns buckets coarser than historyBucket, or only
// aggregate sums with no per-bucket values, the short window can't be cut
// out of the first response, so a second /history call over
// [end-short, end] supplies Short from its sums; Long then comes from the
// first call's sums. When Datadog returns no counts at all (monitor-based
// SLO), HasSeries is false and no second call is made - Target/State are
// still returned so the caller can classify from State.
//
// Errors follow FetchSLOStatus: ErrUnauthorized, ErrNotFound, ErrTimeout,
// ErrServer, or a wrapped decode error, from either call.
func (c *Client) FetchSLOWindows(ctx context.Context, sloID string, end time.Time, short, long time.Duration) (SLOWindows, error) {
	longStart := end.Add(-long)
	shortStart := end.Add(-short)

	parsed, err := c.fetchHistory(ctx, sloID, longStart.Add(-historyBucket), end)
	if err != nil {
		return SLOWindows{}, err
	}

	windows := SLOWindows{State: parsed.Data.Overall.State, SLI: parsed.Data.Overall.SLIValue}
	windows.Target, windows.Timeframe = parsed.pickThreshold()

	series := parsed.Data.Series
	if buckets := len(series.Times); buckets > 0 && len(series.Denominator.Values) > 0 && seriesInterval(series.Interval, series.Times) <= historyBucket {
		windows.HasSeries = true
		// Defensive: Datadog aligns values with times index-for-index, but
		// never index past the shortest of the three.
		buckets = min(buckets, len(series.Denominator.Values))
		for i := 0; i < buckets; i++ {
			start := bucketStart(series.Times[i])
			if start.Before(longStart) {
				continue
			}
			var good float64
			if i < len(series.Numerator.Values) {
				good = series.Numerator.Values[i]
			}
			bucket := countsFrom(series.Denominator.Values[i], good)
			windows.Long.add(bucket)
			if !start.Before(shortStart) {
				windows.Short.add(bucket)
			}
		}
		return windows, nil
	}

	total, ok := series.Denominator.total()
	if !ok {
		// Monitor-based SLO: no request counts at all.
		return windows, nil
	}
	good, _ := series.Numerator.total()
	windows.HasSeries = true
	windows.Long = countsFrom(total, good)

	shortParsed, err := c.fetchHistory(ctx, sloID, shortStart, end)
	if err != nil {
		return SLOWindows{}, err
	}
	shortTotal, _ := shortParsed.Data.Series.Denominator.total()
	shortGood, _ := shortParsed.Data.Series.Numerator.total()
	windows.Short = countsFrom(shortTotal, shortGood)

	return windows, nil
}

// fetchHistory performs one /history call for sloID over [from, to],
// logging Datadog's rate-limit headers, and decodes the response.
func (c *Client) fetchHistory(ctx context.Context, sloID string, from, to time.Time) (sloHistoryResponse, error) {
	path := fmt.Sprintf(sloHistoryPath, url.PathEscape(sloID))
	endpoint := fmt.Sprintf("%s%s?from_ts=%d&to_ts=%d", c.baseURL, path, from.Unix(), to.Unix())

	resp, err := c.get(ctx, endpoint)
	if err != nil {
		return sloHistoryResponse{}, err
	}
	defer resp.Body.Close()

	c.logRateLimit(resp.Header, sloID)

	var parsed sloHistoryResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return sloHistoryResponse{}, fmt.Errorf("datadog: failed to decode response: %w", err)
	}
	return parsed, nil
}

// logRateLimit logs Datadog's X-RateLimit-* headers at debug, and warns
// when fewer than 10% of the window's requests remain - the /history rate
// limit is undocumented, so this is visibility only, never throttling.
// Headers absent or unparsable: nothing is logged.
func (c *Client) logRateLimit(header http.Header, sloID string) {
	remaining, err := strconv.ParseInt(header.Get("X-RateLimit-Remaining"), 10, 64)
	if err != nil {
		return
	}
	fields := []zap.Field{
		zap.String("slo_id", sloID),
		zap.Int64("rate_limit_remaining", remaining),
	}
	limit, limitErr := strconv.ParseInt(header.Get("X-RateLimit-Limit"), 10, 64)
	if limitErr == nil {
		fields = append(fields, zap.Int64("rate_limit_limit", limit))
	}
	if period := header.Get("X-RateLimit-Period"); period != "" {
		fields = append(fields, zap.String("rate_limit_period", period))
	}
	if reset := header.Get("X-RateLimit-Reset"); reset != "" {
		fields = append(fields, zap.String("rate_limit_reset", reset))
	}

	if limitErr == nil && limit > 0 && remaining*10 < limit {
		c.log().Warn("datadog: slo history rate limit nearly exhausted", fields...)
		return
	}
	c.log().Debug("datadog: slo history rate limit", fields...)
}

// add accumulates other into w.
func (w *WindowCounts) add(other WindowCounts) {
	w.Requests += other.Requests
	w.Errors += other.Errors
}

// countsFrom turns a float total/good pair from Datadog into whole counts:
// both rounded (Datadog serializes whole counts as floats, see
// sloHistoryMetric.Sum), errors = total - good clamped to [0, total] so a
// numerator briefly ahead of its denominator (or a negative/garbage value)
// never yields negative errors or more errors than requests (BURN-17).
func countsFrom(total, good float64) WindowCounts {
	requests := max(int64(math.Round(total)), 0)
	errs := requests - int64(math.Round(good))
	errs = min(max(errs, 0), requests)
	return WindowCounts{Requests: requests, Errors: errs}
}

// seriesInterval returns the series' bucket width: Datadog's interval
// field (seconds) when present, else the gap between the first two
// timestamps, else 0 (a single bucket - treated as fine-grained).
func seriesInterval(interval float64, times []float64) time.Duration {
	if interval > 0 {
		return time.Duration(interval * float64(time.Second))
	}
	if len(times) >= 2 {
		return bucketStart(times[1]).Sub(bucketStart(times[0]))
	}
	return 0
}

// bucketStart converts a series timestamp to a time. Datadog's history
// series uses milliseconds since the epoch; a value too small to be
// milliseconds for any date after 1973 is treated as seconds instead, so a
// unit change on Datadog's side can't silently drop every bucket.
func bucketStart(ts float64) time.Time {
	if ts < 1e11 {
		return time.Unix(int64(math.Round(ts)), 0)
	}
	return time.UnixMilli(int64(math.Round(ts)))
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
		summary := SLOSummary{
			ID:      slo.Data.ID,
			Name:    slo.Data.Attributes.Name,
			SLOType: slo.Data.Attributes.SLOType,
		}
		// ServiceTag is only set when service_tags has exactly one entry -
		// flow-type/multi-service SLOs (0 or 2+ tags) get "", per this
		// session's decision not to attempt parsing the query string to
		// recover a service list (design.md's Data Models).
		if len(slo.Data.Attributes.ServiceTags) == 1 {
			summary.ServiceTag = slo.Data.Attributes.ServiceTags[0]
		}
		summaries = append(summaries, summary)
	}

	return summaries, nil
}

// errorTrackingSearchRequest mirrors the verified
// POST /api/v2/error-tracking/issues/search request body (design.md's
// Components section, confirmed live against Datadog's public docs).
type errorTrackingSearchRequest struct {
	Data errorTrackingSearchRequestData `json:"data"`
}

type errorTrackingSearchRequestData struct {
	Type       string                               `json:"type"`
	Attributes errorTrackingSearchRequestAttributes `json:"attributes"`
}

type errorTrackingSearchRequestAttributes struct {
	Query   string `json:"query"`
	From    int64  `json:"from"`
	To      int64  `json:"to"`
	Track   string `json:"track"`
	OrderBy string `json:"order_by"`
}

// errorTrackingSearchResponse mirrors the verified response shape: data[]
// is ordered by the requested order_by (TOTAL_COUNT descending here);
// data[0]'s relationships.issue.data.id looks up the matching entry in
// included[] for the actual error_type/error_message.
type errorTrackingSearchResponse struct {
	Data []struct {
		Relationships struct {
			Issue struct {
				Data struct {
					ID string `json:"id"`
				} `json:"data"`
			} `json:"issue"`
		} `json:"relationships"`
	} `json:"data"`
	Included []struct {
		ID         string `json:"id"`
		Attributes struct {
			ErrorType    string `json:"error_type"`
			ErrorMessage string `json:"error_message"`
		} `json:"attributes"`
	} `json:"included"`
}

// SearchErrorTrackingIssues returns the highest-total_count Error Tracking
// issue's type + message for service/env within [from, to), or
// (CauseHint{}, false, nil) when nothing matches - mirroring SearchSLOs'
// "empty result is normal, not a failure" convention. Only "trace" track
// issues are queried (every real service in the reference org is a backend
// microservice - design.md's Tech Decisions).
func (c *Client) SearchErrorTrackingIssues(ctx context.Context, service, env string, from, to time.Time) (CauseHint, bool, error) {
	endpoint := fmt.Sprintf("%s%s", c.baseURL, errorTrackingSearchPath)
	reqBody := errorTrackingSearchRequest{
		Data: errorTrackingSearchRequestData{
			Type: "search_request",
			Attributes: errorTrackingSearchRequestAttributes{
				Query:   fmt.Sprintf("service:%s AND env:%s", service, env),
				From:    from.UnixMilli(),
				To:      to.UnixMilli(),
				Track:   "trace",
				OrderBy: "TOTAL_COUNT",
			},
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return CauseHint{}, false, fmt.Errorf("datadog: failed to encode request: %w", err)
	}

	resp, err := c.post(ctx, endpoint, body)
	if err != nil {
		return CauseHint{}, false, err
	}
	defer resp.Body.Close()

	var parsed errorTrackingSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return CauseHint{}, false, fmt.Errorf("datadog: failed to decode response: %w", err)
	}

	if len(parsed.Data) == 0 {
		return CauseHint{}, false, nil
	}

	topIssueID := parsed.Data[0].Relationships.Issue.Data.ID
	for _, included := range parsed.Included {
		if included.ID != topIssueID {
			continue
		}
		hint := CauseHint{
			ErrorType:    included.Attributes.ErrorType,
			ErrorMessage: included.Attributes.ErrorMessage,
		}
		if len(hint.ErrorMessage) > maxCauseMessageLength {
			hint.ErrorMessage = hint.ErrorMessage[:maxCauseMessageLength]
		}
		return hint, true, nil
	}

	// top issue's id has no matching included[] entry - treat as "not
	// found" rather than erroring, since the query itself succeeded.
	return CauseHint{}, false, nil
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
	case resp.StatusCode == http.StatusNotFound:
		resp.Body.Close()
		return nil, ErrNotFound
	case resp.StatusCode >= http.StatusInternalServerError:
		resp.Body.Close()
		return nil, ErrServer
	case resp.StatusCode != http.StatusOK:
		resp.Body.Close()
		return nil, fmt.Errorf("datadog: unexpected status %d", resp.StatusCode)
	}

	return resp, nil
}

// post issues an authenticated POST with a JSON body against endpoint and
// classifies the outcome into vane's typed connector errors, mirroring
// get's header/timeout/error-classification behavior. On success (200) it
// returns the response with its body still open for the caller to decode.
func (c *Client) post(ctx context.Context, endpoint string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("datadog: failed to build request: %w", err)
	}
	req.Header.Set("DD-API-KEY", c.apiKey)
	req.Header.Set("DD-APPLICATION-KEY", c.appKey)
	req.Header.Set("Content-Type", "application/json")

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
	case resp.StatusCode == http.StatusNotFound:
		resp.Body.Close()
		return nil, ErrNotFound
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
