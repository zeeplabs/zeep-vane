package poller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/connectors/datadog"
)

// fakeProvider returns errs[i] (or the last entry, once exhausted) on the
// i-th FetchSLOWindows call, and windows on a nil error. It also records
// the end/short/long it received on each call, so tests can assert
// FetchWindowsWithRetry and pollService pass them through as expected.
// FetchSLOStatus (the state-based fallback's short-window call) has its own
// call counter, statusErrs sequence (empty means always succeed) and
// recorded from/to.
type fakeProvider struct {
	calls   int
	errs    []error
	windows datadog.SLOWindows
	// windowsSeq, when non-empty, overrides windows with one entry per
	// logical call (indexed the same way as errs, clamped to the last
	// entry) - used by tests that need successive cycles to see different
	// windows.
	windowsSeq []datadog.SLOWindows
	gotEnd     []time.Time
	gotShort   []time.Duration
	gotLong    []time.Duration

	status      datadog.SLOStatus
	statusErrs  []error
	statusCalls int
	gotFrom     []time.Time
	gotTo       []time.Time
}

func (f *fakeProvider) FetchSLOWindows(ctx context.Context, sloID string, end time.Time, short, long time.Duration) (datadog.SLOWindows, error) {
	idx := f.calls
	if idx >= len(f.errs) {
		idx = len(f.errs) - 1
	}
	f.calls++
	f.gotEnd = append(f.gotEnd, end)
	f.gotShort = append(f.gotShort, short)
	f.gotLong = append(f.gotLong, long)

	if f.errs[idx] != nil {
		return datadog.SLOWindows{}, f.errs[idx]
	}
	if len(f.windowsSeq) > 0 {
		wIdx := min(idx, len(f.windowsSeq)-1)
		return f.windowsSeq[wIdx], nil
	}
	return f.windows, nil
}

func (f *fakeProvider) FetchSLOStatus(ctx context.Context, sloID string, from, to time.Time) (datadog.SLOStatus, error) {
	idx := f.statusCalls
	f.statusCalls++
	f.gotFrom = append(f.gotFrom, from)
	f.gotTo = append(f.gotTo, to)

	if len(f.statusErrs) > 0 {
		if err := f.statusErrs[min(idx, len(f.statusErrs)-1)]; err != nil {
			return datadog.SLOStatus{}, err
		}
	}
	return f.status, nil
}

// SearchErrorTrackingIssues is a no-op stub so fakeProvider satisfies
// poller.DatadogClient in full (not just datadog.SLOProvider) - needed by
// any test resolving fakeProvider through a DatadogClientResolver
// (integrations-tenant-scope).
func (f *fakeProvider) SearchErrorTrackingIssues(ctx context.Context, service, env string, from, to time.Time) (datadog.CauseHint, bool, error) {
	return datadog.CauseHint{}, false, nil
}

// healthyWindows is a metric-based SLO reading with steady traffic and no
// errors at a 99.5% target - classified "operational" by classifyBurn.
func healthyWindows() datadog.SLOWindows {
	return datadog.SLOWindows{
		Target:    99.5,
		Timeframe: "30d",
		State:     "ok",
		SLI:       100,
		Short:     datadog.WindowCounts{Requests: 500},
		Long:      datadog.WindowCounts{Requests: 6000},
		HasSeries: true,
	}
}

func TestFetchWindowsWithRetry_SuccessFirstAttempt_NoRetry(t *testing.T) {
	backoffBase = time.Millisecond
	provider := &fakeProvider{errs: []error{nil}, windows: healthyWindows()}

	windows, err := FetchWindowsWithRetry(context.Background(), provider, "slo-1", time.Unix(200, 0), shortWindow, longWindow, 3)
	if err != nil {
		t.Fatalf("FetchWindowsWithRetry() returned unexpected error: %v", err)
	}
	if windows != healthyWindows() {
		t.Errorf("windows = %+v, want %+v", windows, healthyWindows())
	}
	if provider.calls != 1 {
		t.Errorf("calls = %d, want 1 (no retry needed)", provider.calls)
	}
}

func TestFetchWindowsWithRetry_PassesWindowsThroughUnchanged(t *testing.T) {
	backoffBase = time.Millisecond
	provider := &fakeProvider{
		errs:    []error{datadog.ErrTimeout, datadog.ErrServer, nil},
		windows: healthyWindows(),
	}
	end := time.Unix(200, 0)

	if _, err := FetchWindowsWithRetry(context.Background(), provider, "slo-1", end, shortWindow, longWindow, 3); err != nil {
		t.Fatalf("FetchWindowsWithRetry() returned unexpected error: %v", err)
	}
	if len(provider.gotEnd) != 3 {
		t.Fatalf("provider received %d calls, want 3", len(provider.gotEnd))
	}
	for i := range provider.gotEnd {
		if !provider.gotEnd[i].Equal(end) {
			t.Errorf("call %d: end = %v, want %v (not recomputed per attempt)", i, provider.gotEnd[i], end)
		}
		if provider.gotShort[i] != shortWindow || provider.gotLong[i] != longWindow {
			t.Errorf("call %d: short/long = %v/%v, want %v/%v", i, provider.gotShort[i], provider.gotLong[i], shortWindow, longWindow)
		}
	}
}

func TestFetchWindowsWithRetry_SuccessThirdAttempt_RetriesTransientErrors(t *testing.T) {
	backoffBase = time.Millisecond
	provider := &fakeProvider{
		errs:    []error{datadog.ErrTimeout, datadog.ErrServer, nil},
		windows: healthyWindows(),
	}

	windows, err := FetchWindowsWithRetry(context.Background(), provider, "slo-1", time.Unix(200, 0), shortWindow, longWindow, 3)
	if err != nil {
		t.Fatalf("FetchWindowsWithRetry() returned unexpected error: %v", err)
	}
	if !windows.HasSeries {
		t.Errorf("windows = %+v, want the successful attempt's windows", windows)
	}
	if provider.calls != 3 {
		t.Errorf("calls = %d, want 3 (2 transient failures then success)", provider.calls)
	}
}

func TestFetchWindowsWithRetry_ExhaustsAttempts_ReturnsLastTransientError(t *testing.T) {
	backoffBase = time.Millisecond
	provider := &fakeProvider{errs: []error{datadog.ErrTimeout, datadog.ErrTimeout, datadog.ErrTimeout}}

	_, err := FetchWindowsWithRetry(context.Background(), provider, "slo-1", time.Unix(200, 0), shortWindow, longWindow, 3)
	if !errors.Is(err, datadog.ErrTimeout) {
		t.Errorf("FetchWindowsWithRetry() error = %v, want ErrTimeout", err)
	}
	if provider.calls != 3 {
		t.Errorf("calls = %d, want 3 (all attempts exhausted, no more)", provider.calls)
	}
}

func TestFetchWindowsWithRetry_Unauthorized_FailsImmediatelyWithoutRetry(t *testing.T) {
	backoffBase = time.Millisecond
	provider := &fakeProvider{
		errs:    []error{datadog.ErrUnauthorized, nil, nil},
		windows: healthyWindows(),
	}

	_, err := FetchWindowsWithRetry(context.Background(), provider, "slo-1", time.Unix(200, 0), shortWindow, longWindow, 3)
	if !errors.Is(err, datadog.ErrUnauthorized) {
		t.Errorf("FetchWindowsWithRetry() error = %v, want ErrUnauthorized", err)
	}
	if provider.calls != 1 {
		t.Errorf("calls = %d, want 1 (401 must not be retried)", provider.calls)
	}
}

func TestFetchWindowsWithRetry_CanceledDuringBackoff_ReturnsContextError(t *testing.T) {
	backoffBase = time.Hour
	t.Cleanup(func() { backoffBase = time.Millisecond })
	provider := &fakeProvider{errs: []error{datadog.ErrTimeout}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := FetchWindowsWithRetry(ctx, provider, "slo-1", time.Unix(200, 0), shortWindow, longWindow, 3)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("FetchWindowsWithRetry() error = %v, want context.Canceled", err)
	}
	if provider.calls != 1 {
		t.Errorf("calls = %d, want 1 (canceled ctx aborts the backoff wait)", provider.calls)
	}
}

func TestFetchStatusWithRetry_RetriesTransientErrorsAndPassesRangeThrough(t *testing.T) {
	backoffBase = time.Millisecond
	provider := &fakeProvider{
		statusErrs: []error{datadog.ErrServer, nil},
		status:     datadog.SLOStatus{State: "ok"},
	}
	from, to := time.Unix(100, 0), time.Unix(400, 0)

	status, err := FetchStatusWithRetry(context.Background(), provider, "slo-1", from, to, 3)
	if err != nil {
		t.Fatalf("FetchStatusWithRetry() returned unexpected error: %v", err)
	}
	if status.State != "ok" {
		t.Errorf("State = %q, want %q", status.State, "ok")
	}
	if provider.statusCalls != 2 {
		t.Errorf("calls = %d, want 2 (one transient failure then success)", provider.statusCalls)
	}
	for i := range provider.gotFrom {
		if !provider.gotFrom[i].Equal(from) || !provider.gotTo[i].Equal(to) {
			t.Errorf("call %d: from/to = %v/%v, want %v/%v (not recomputed per attempt)", i, provider.gotFrom[i], provider.gotTo[i], from, to)
		}
	}
}

func TestFetchStatusWithRetry_Unauthorized_FailsImmediatelyWithoutRetry(t *testing.T) {
	backoffBase = time.Millisecond
	provider := &fakeProvider{statusErrs: []error{datadog.ErrUnauthorized, nil}}

	_, err := FetchStatusWithRetry(context.Background(), provider, "slo-1", time.Unix(100, 0), time.Unix(400, 0), 3)
	if !errors.Is(err, datadog.ErrUnauthorized) {
		t.Errorf("FetchStatusWithRetry() error = %v, want ErrUnauthorized", err)
	}
	if provider.statusCalls != 1 {
		t.Errorf("calls = %d, want 1 (401 must not be retried)", provider.statusCalls)
	}
}
