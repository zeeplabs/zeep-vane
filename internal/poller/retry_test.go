package poller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/connectors/datadog"
)

// fakeProvider returns errs[i] (or the last entry, once exhausted) on the
// i-th call, and status on a nil error. It also records the from/to window
// it received on each call, so tests can assert FetchWithRetry passes the
// window through unchanged.
type fakeProvider struct {
	calls  int
	errs   []error
	status datadog.SLOStatus
	// statuses, when non-empty, overrides status with one entry per logical
	// call (indexed the same way as errs, clamped to the last entry) - used
	// by tests that need FetchSLOStatus to return a different status on
	// successive calls (e.g. breach-hysteresis streak tests).
	statuses []datadog.SLOStatus
	gotFrom  []time.Time
	gotTo    []time.Time
}

func (f *fakeProvider) FetchSLOStatus(ctx context.Context, sloID string, from, to time.Time) (datadog.SLOStatus, error) {
	idx := f.calls
	if idx >= len(f.errs) {
		idx = len(f.errs) - 1
	}
	f.calls++
	f.gotFrom = append(f.gotFrom, from)
	f.gotTo = append(f.gotTo, to)

	if f.errs[idx] != nil {
		return datadog.SLOStatus{}, f.errs[idx]
	}
	if len(f.statuses) > 0 {
		sIdx := idx
		if sIdx >= len(f.statuses) {
			sIdx = len(f.statuses) - 1
		}
		return f.statuses[sIdx], nil
	}
	return f.status, nil
}

func TestFetchWithRetry_SuccessFirstAttempt_NoRetry(t *testing.T) {
	backoffBase = time.Millisecond
	provider := &fakeProvider{errs: []error{nil}, status: datadog.SLOStatus{State: "ok"}}
	from, to := time.Unix(100, 0), time.Unix(200, 0)

	status, err := FetchWithRetry(context.Background(), provider, "slo-1", from, to, 3)
	if err != nil {
		t.Fatalf("FetchWithRetry() returned unexpected error: %v", err)
	}
	if status.State != "ok" {
		t.Errorf("State = %q, want %q", status.State, "ok")
	}
	if provider.calls != 1 {
		t.Errorf("calls = %d, want 1 (no retry needed)", provider.calls)
	}
}

func TestFetchWithRetry_PassesWindowThroughUnchanged(t *testing.T) {
	backoffBase = time.Millisecond
	provider := &fakeProvider{
		errs:   []error{datadog.ErrTimeout, datadog.ErrServer, nil},
		status: datadog.SLOStatus{State: "ok"},
	}
	from, to := time.Unix(100, 0), time.Unix(200, 0)

	if _, err := FetchWithRetry(context.Background(), provider, "slo-1", from, to, 3); err != nil {
		t.Fatalf("FetchWithRetry() returned unexpected error: %v", err)
	}
	if len(provider.gotFrom) != 3 {
		t.Fatalf("provider received %d calls, want 3", len(provider.gotFrom))
	}
	for i := range provider.gotFrom {
		if !provider.gotFrom[i].Equal(from) {
			t.Errorf("call %d: from = %v, want %v (not recomputed per attempt)", i, provider.gotFrom[i], from)
		}
		if !provider.gotTo[i].Equal(to) {
			t.Errorf("call %d: to = %v, want %v (not recomputed per attempt)", i, provider.gotTo[i], to)
		}
	}
}

func TestFetchWithRetry_SuccessThirdAttempt_RetriesTransientErrors(t *testing.T) {
	backoffBase = time.Millisecond
	provider := &fakeProvider{
		errs:   []error{datadog.ErrTimeout, datadog.ErrServer, nil},
		status: datadog.SLOStatus{State: "ok"},
	}
	from, to := time.Unix(100, 0), time.Unix(200, 0)

	status, err := FetchWithRetry(context.Background(), provider, "slo-1", from, to, 3)
	if err != nil {
		t.Fatalf("FetchWithRetry() returned unexpected error: %v", err)
	}
	if status.State != "ok" {
		t.Errorf("State = %q, want %q", status.State, "ok")
	}
	if provider.calls != 3 {
		t.Errorf("calls = %d, want 3 (2 transient failures then success)", provider.calls)
	}
}

func TestFetchWithRetry_ExhaustsAttempts_ReturnsLastTransientError(t *testing.T) {
	backoffBase = time.Millisecond
	provider := &fakeProvider{errs: []error{datadog.ErrTimeout, datadog.ErrTimeout, datadog.ErrTimeout}}
	from, to := time.Unix(100, 0), time.Unix(200, 0)

	_, err := FetchWithRetry(context.Background(), provider, "slo-1", from, to, 3)
	if !errors.Is(err, datadog.ErrTimeout) {
		t.Errorf("FetchWithRetry() error = %v, want ErrTimeout", err)
	}
	if provider.calls != 3 {
		t.Errorf("calls = %d, want 3 (all attempts exhausted, no more)", provider.calls)
	}
}

func TestFetchWithRetry_Unauthorized_FailsImmediatelyWithoutRetry(t *testing.T) {
	backoffBase = time.Millisecond
	provider := &fakeProvider{
		errs:   []error{datadog.ErrUnauthorized, nil, nil},
		status: datadog.SLOStatus{State: "ok"},
	}
	from, to := time.Unix(100, 0), time.Unix(200, 0)

	_, err := FetchWithRetry(context.Background(), provider, "slo-1", from, to, 3)
	if !errors.Is(err, datadog.ErrUnauthorized) {
		t.Errorf("FetchWithRetry() error = %v, want ErrUnauthorized", err)
	}
	if provider.calls != 1 {
		t.Errorf("calls = %d, want 1 (401 must not be retried)", provider.calls)
	}
}
