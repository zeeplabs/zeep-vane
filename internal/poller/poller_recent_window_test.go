package poller

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/connectors/datadog"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

// fakeIntervalWriter records every OpenOrExtend call it receives.
type fakeIntervalWriter struct {
	calls []struct {
		serviceID, status string
		errorBudget       float64
		at                time.Time
	}
}

func (f *fakeIntervalWriter) OpenOrExtend(ctx context.Context, serviceID, status string, errorBudgetRemaining float64, at time.Time) error {
	f.calls = append(f.calls, struct {
		serviceID, status string
		errorBudget       float64
		at                time.Time
	}{serviceID, status, errorBudgetRemaining, at})
	return nil
}

// fakeStatusUpdater records every UpdateStatus call it receives.
type fakeStatusUpdater struct {
	calls []struct {
		serviceID, status string
	}
}

func (f *fakeStatusUpdater) UpdateStatus(ctx context.Context, serviceID, status string) error {
	f.calls = append(f.calls, struct{ serviceID, status string }{serviceID, status})
	return nil
}

func newTestPoller(provider datadog.SLOProvider, interval time.Duration, intervals *fakeIntervalWriter, statuses *fakeStatusUpdater) *Poller {
	return &Poller{
		statuses:        statuses,
		statusIntervals: intervals,
		provider:        provider,
		interval:        interval,
		logger:          zap.NewNop(),
	}
}

func TestPollService_WindowPassedToProvider_MatchesIntervalBounds(t *testing.T) {
	provider := &fakeProvider{
		errs:   []error{nil},
		status: datadog.SLOStatus{State: "ok", RequestCount: 10},
	}
	intervals := &fakeIntervalWriter{}
	statuses := &fakeStatusUpdater{}
	p := newTestPoller(provider, time.Hour, intervals, statuses)

	before := time.Now()
	if err := p.pollService(t.Context(), db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: "not_configured"}); err != nil {
		t.Fatalf("pollService() returned unexpected error: %v", err)
	}
	after := time.Now()

	if len(provider.gotFrom) != 1 {
		t.Fatalf("provider received %d calls, want 1", len(provider.gotFrom))
	}
	gotFrom, gotTo := provider.gotFrom[0], provider.gotTo[0]
	if gotTo.Before(before) || gotTo.After(after) {
		t.Errorf("to = %v, want within [%v, %v]", gotTo, before, after)
	}
	wantFrom := gotTo.Add(-time.Hour)
	if !gotFrom.Equal(wantFrom) {
		t.Errorf("from = %v, want %v (to - interval)", gotFrom, wantFrom)
	}
}

func TestPollService_SufficientVolume_RecomputesFromRecoveredState(t *testing.T) {
	provider := &fakeProvider{
		errs:   []error{nil},
		status: datadog.SLOStatus{State: "ok", RequestCount: 10},
	}
	intervals := &fakeIntervalWriter{}
	statuses := &fakeStatusUpdater{}
	p := newTestPoller(provider, time.Hour, intervals, statuses)

	if err := p.pollService(t.Context(), db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: "outage"}); err != nil {
		t.Fatalf("pollService() returned unexpected error: %v", err)
	}

	if len(statuses.calls) != 1 || statuses.calls[0].status != "operational" {
		t.Errorf("UpdateStatus calls = %+v, want a single call with status %q", statuses.calls, "operational")
	}
	if len(intervals.calls) != 1 || intervals.calls[0].status != "operational" {
		t.Errorf("OpenOrExtend calls = %+v, want a single call with status %q", intervals.calls, "operational")
	}
}

func TestPollService_LowVolumeNoisyBreach_CarriesForwardPreviousStatus(t *testing.T) {
	provider := &fakeProvider{
		errs:   []error{nil},
		status: datadog.SLOStatus{State: "breached", RequestCount: 3},
	}
	intervals := &fakeIntervalWriter{}
	statuses := &fakeStatusUpdater{}
	p := newTestPoller(provider, time.Hour, intervals, statuses)

	if err := p.pollService(t.Context(), db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: "operational"}); err != nil {
		t.Fatalf("pollService() returned unexpected error: %v", err)
	}

	if len(statuses.calls) != 1 || statuses.calls[0].status != "operational" {
		t.Errorf("UpdateStatus calls = %+v, want carry-forward of %q", statuses.calls, "operational")
	}
	if len(intervals.calls) != 1 || intervals.calls[0].status != "operational" {
		t.Errorf("OpenOrExtend calls = %+v, want carry-forward of %q", intervals.calls, "operational")
	}
}

func TestPollService_LowVolumeCarryForward_StillInvokesWriters(t *testing.T) {
	provider := &fakeProvider{
		errs:   []error{nil},
		status: datadog.SLOStatus{State: "ok", RequestCount: 0},
	}
	intervals := &fakeIntervalWriter{}
	statuses := &fakeStatusUpdater{}
	p := newTestPoller(provider, time.Hour, intervals, statuses)

	if err := p.pollService(t.Context(), db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: "degraded"}); err != nil {
		t.Fatalf("pollService() returned unexpected error: %v", err)
	}

	if len(intervals.calls) != 1 {
		t.Fatalf("OpenOrExtend calls = %d, want 1 (carry-forward still writes the interval)", len(intervals.calls))
	}
	if len(statuses.calls) != 1 {
		t.Fatalf("UpdateStatus calls = %d, want 1 (carry-forward still updates cached status)", len(statuses.calls))
	}
}

func TestPollService_FirstPollLowVolume_StaysNotConfigured(t *testing.T) {
	provider := &fakeProvider{
		errs:   []error{nil},
		status: datadog.SLOStatus{State: "ok", RequestCount: 1},
	}
	intervals := &fakeIntervalWriter{}
	statuses := &fakeStatusUpdater{}
	p := newTestPoller(provider, time.Hour, intervals, statuses)

	if err := p.pollService(t.Context(), db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: "not_configured"}); err != nil {
		t.Fatalf("pollService() returned unexpected error: %v", err)
	}

	if len(statuses.calls) != 1 || statuses.calls[0].status != "not_configured" {
		t.Errorf("UpdateStatus calls = %+v, want carry-forward of %q, no special-cased default", statuses.calls, "not_configured")
	}
}
