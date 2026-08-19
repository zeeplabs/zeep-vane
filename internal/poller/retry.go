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

// FetchWithRetry calls provider.FetchSLOStatus, retrying up to maxAttempts
// times total on transient errors (timeout, 5xx) with exponential backoff
// between attempts. ErrUnauthorized is never retried - a bad credential
// cannot succeed by trying again, so it fails on the first attempt (SP-05).
func FetchWithRetry(ctx context.Context, provider datadog.SLOProvider, sloID string, maxAttempts int) (datadog.SLOStatus, error) {
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		status, err := provider.FetchSLOStatus(ctx, sloID)
		if err == nil {
			return status, nil
		}

		lastErr = err
		if !isTransient(err) {
			return datadog.SLOStatus{}, err
		}

		if attempt < maxAttempts {
			delay := backoffBase * time.Duration(1<<(attempt-1))
			select {
			case <-ctx.Done():
				return datadog.SLOStatus{}, ctx.Err()
			case <-time.After(delay):
			}
		}
	}

	return datadog.SLOStatus{}, lastErr
}

// isTransient reports whether err is worth retrying: a timeout or a 5xx
// server error. Auth failures and not-found are not transient.
func isTransient(err error) bool {
	return errors.Is(err, datadog.ErrTimeout) || errors.Is(err, datadog.ErrServer)
}
