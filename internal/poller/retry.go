// Package poller periodically fetches SLO status for every configured
// service and updates its cached current status (Phase 4).
package poller

import (
	"context"
	"errors"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/connectors/datadog"
)

// backoffBase is the base delay between retry attempts; attempt N waits
// backoffBase * 2^(N-1). It is a package variable, not a constant, so tests
// can shrink it and avoid real sleeps.
var backoffBase = 500 * time.Millisecond

// FetchWindowsWithRetry calls provider.FetchSLOWindows with retryFetch's
// retry semantics (the burn-rate path's one call per service per cycle).
// end/short/long are passed through unchanged on every attempt, never
// recomputed per attempt.
func FetchWindowsWithRetry(ctx context.Context, provider datadog.SLOProvider, sloID string, end time.Time, short, long time.Duration, maxAttempts int) (datadog.SLOWindows, error) {
	return retryFetch(ctx, maxAttempts, func() (datadog.SLOWindows, error) {
		return provider.FetchSLOWindows(ctx, sloID, end, short, long)
	})
}

// FetchStatusWithRetry calls provider.FetchSLOStatus over [from, to] with
// retryFetch's retry semantics - the state-based fallback's short-window
// call. from/to are passed through unchanged on every attempt.
func FetchStatusWithRetry(ctx context.Context, provider datadog.SLOProvider, sloID string, from, to time.Time, maxAttempts int) (datadog.SLOStatus, error) {
	return retryFetch(ctx, maxAttempts, func() (datadog.SLOStatus, error) {
		return provider.FetchSLOStatus(ctx, sloID, from, to)
	})
}

// retryFetch calls fetch, retrying up to maxAttempts times total on
// transient errors (timeout, 5xx) with exponential backoff between
// attempts. ErrUnauthorized is never retried - a bad credential cannot
// succeed by trying again, so it fails on the first attempt (SP-05). A
// canceled ctx aborts the backoff wait immediately with ctx.Err(). On
// failure it returns T's zero value.
func retryFetch[T any](ctx context.Context, maxAttempts int, fetch func() (T, error)) (T, error) {
	var zero T
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, err := fetch()
		if err == nil {
			return result, nil
		}

		lastErr = err
		if !isTransient(err) {
			return zero, err
		}

		if attempt < maxAttempts {
			delay := backoffBase * time.Duration(1<<(attempt-1))
			select {
			case <-ctx.Done():
				return zero, ctx.Err()
			case <-time.After(delay):
			}
		}
	}

	return zero, lastErr
}

// isTransient reports whether err is worth retrying: a timeout or a 5xx
// server error. Auth failures and not-found are not transient.
func isTransient(err error) bool {
	return errors.Is(err, datadog.ErrTimeout) || errors.Is(err, datadog.ErrServer)
}
