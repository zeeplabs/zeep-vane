package poller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/connectors/datadog"
)

// fakeProvider returns errs[i] (or the last entry, once exhausted) on the
// i-th call, and status on a nil error.
type fakeProvider struct {
	calls  int
	errs   []error
	status datadog.SLOStatus
}

func (f *fakeProvider) FetchSLOStatus(ctx context.Context, sloID string) (datadog.SLOStatus, error) {
	idx := f.calls
	if idx >= len(f.errs) {
		idx = len(f.errs) - 1
	}
	f.calls++

	if f.errs[idx] != nil {
		return datadog.SLOStatus{}, f.errs[idx]
	}
	return f.status, nil
}

func TestFetchWithRetry_SuccessFirstAttempt_NoRetry(t *testing.T) {
	backoffBase = time.Millisecond
	provider := &fakeProvider{errs: []error{nil}, status: datadog.SLOStatus{State: "ok"}}

	status, err := FetchWithRetry(context.Background(), provider, "slo-1", 3)
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

func TestFetchWithRetry_SuccessThirdAttempt_RetriesTransientErrors(t *testing.T) {
	backoffBase = time.Millisecond
	provider := &fakeProvider{
		errs:   []error{datadog.ErrTimeout, datadog.ErrServer, nil},
		status: datadog.SLOStatus{State: "ok"},
	}

	status, err := FetchWithRetry(context.Background(), provider, "slo-1", 3)
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

	_, err := FetchWithRetry(context.Background(), provider, "slo-1", 3)
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

	_, err := FetchWithRetry(context.Background(), provider, "slo-1", 3)
	if !errors.Is(err, datadog.ErrUnauthorized) {
		t.Errorf("FetchWithRetry() error = %v, want ErrUnauthorized", err)
	}
	if provider.calls != 1 {
		t.Errorf("calls = %d, want 1 (401 must not be retried)", provider.calls)
	}
}
