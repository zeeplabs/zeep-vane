package poller

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/connectors/datadog"
	"github.com/zeeplabs/zeep-vane/internal/db"
)

// fakeIntervalWriter records every OpenOrExtend/SetIntervalAnalysis call it
// receives, doubling as both statusIntervalWriter and intervalAnalysisWriter
// (same *db.StatusIntervalRepository backs both roles in production). A
// non-nil err fails OpenOrExtend without recording the call.
type fakeIntervalWriter struct {
	calls []struct {
		serviceID, status string
		errorBudget       float64
		at                time.Time
	}
	err           error
	nextID        int
	analysisCalls []struct{ intervalID, analysis string }
}

func (f *fakeIntervalWriter) OpenOrExtend(ctx context.Context, serviceID, status string, errorBudgetRemaining float64, at time.Time) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.calls = append(f.calls, struct {
		serviceID, status string
		errorBudget       float64
		at                time.Time
	}{serviceID, status, errorBudgetRemaining, at})
	f.nextID++
	return fmt.Sprintf("fake-interval-%d", f.nextID), nil
}

func (f *fakeIntervalWriter) SetIntervalAnalysis(ctx context.Context, intervalID, analysis string) error {
	f.analysisCalls = append(f.analysisCalls, struct{ intervalID, analysis string }{intervalID, analysis})
	return nil
}

// statusWrite is one status write fakeStatusUpdater received: reading is
// nil for UpdateStatus and the persisted reading for
// UpdateStatusAndReading.
type statusWrite struct {
	serviceID, status string
	reading           *db.ServiceReading
}

// fakeStatusUpdater records every UpdateStatus/UpdateStatusAndReading call
// it receives, in order, in calls. onWrite, when set, runs at the start of
// every write (before err is checked) so a test can observe what else had
// already happened at that point; a non-nil err fails the write without
// recording it.
type fakeStatusUpdater struct {
	calls   []statusWrite
	err     error
	onWrite func()
}

func (f *fakeStatusUpdater) UpdateStatus(ctx context.Context, serviceID, status string) error {
	return f.record(statusWrite{serviceID: serviceID, status: status})
}

func (f *fakeStatusUpdater) UpdateStatusAndReading(ctx context.Context, serviceID, status string, rd db.ServiceReading) error {
	return f.record(statusWrite{serviceID: serviceID, status: status, reading: &rd})
}

func (f *fakeStatusUpdater) record(w statusWrite) error {
	if f.onWrite != nil {
		f.onWrite()
	}
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, w)
	return nil
}

// newTestPoller builds a Poller around fakes, returning the incident store
// its analyzer writes to so tests can observe transition dispatch (an
// operational -> outage transition creates an incident synchronously).
func newTestPoller(provider datadog.SLOProvider, intervals *fakeIntervalWriter, statuses *fakeStatusUpdater) (*Poller, *fakeIncidentStore) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	analyzer := NewSLOAnalyzer(incidents, &fakeStatusAnalysisWriter{}, intervals, &fakeLLMGenerator{}, time.Second, zap.NewNop())
	return &Poller{
		statuses:        statuses,
		statusIntervals: intervals,
		provider:        provider,
		interval:        time.Hour,
		analyzer:        analyzer,
		logger:          zap.NewNop(),
	}, incidents
}

// burningWindows returns a 99.5%-target metric-SLO reading whose short and
// long windows carry the given counts.
func burningWindows(short, long datadog.WindowCounts) datadog.SLOWindows {
	w := healthyWindows()
	w.Short, w.Long = short, long
	return w
}

func TestPollService_FetchesShortAndLongWindowsEndingLagBeforeNow(t *testing.T) {
	provider := &fakeProvider{errs: []error{nil}, windows: healthyWindows()}
	p, _ := newTestPoller(provider, &fakeIntervalWriter{}, &fakeStatusUpdater{})

	before := time.Now()
	if err := p.pollService(t.Context(), db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: "not_configured"}); err != nil {
		t.Fatalf("pollService() returned unexpected error: %v", err)
	}
	after := time.Now()

	if provider.calls != 1 {
		t.Fatalf("provider received %d calls, want 1", provider.calls)
	}
	gotEnd := provider.gotEnd[0]
	if gotEnd.Before(before.Add(-recentWindowLag)) || gotEnd.After(after.Add(-recentWindowLag)) {
		t.Errorf("end = %v, want within [%v, %v] (now - recentWindowLag)", gotEnd, before.Add(-recentWindowLag), after.Add(-recentWindowLag))
	}
	if provider.gotShort[0] != shortWindow || provider.gotLong[0] != longWindow {
		t.Errorf("short/long = %v/%v, want %v/%v", provider.gotShort[0], provider.gotLong[0], shortWindow, longWindow)
	}
}

// TestPollService_BurnRatePath covers BURN-01's wiring: a metric SLO with a
// usable target is classified by classifyBurn and persisted through
// UpdateStatusAndReading with that cycle's reading.
func TestPollService_BurnRatePath(t *testing.T) {
	tests := []struct {
		name        string
		previous    string
		short, long datadog.WindowCounts
		wantStatus  string
		wantShort   float64
		wantLong    float64
	}{
		{
			name:       "both windows over 14.4x is outage",
			previous:   "operational",
			short:      datadog.WindowCounts{Requests: 100, Errors: 20},
			long:       datadog.WindowCounts{Requests: 1000, Errors: 100},
			wantStatus: "outage",
			wantShort:  40,
			wantLong:   20,
		},
		{
			name:       "both windows over 7.2x is degraded",
			previous:   "operational",
			short:      datadog.WindowCounts{Requests: 100, Errors: 5},
			long:       datadog.WindowCounts{Requests: 1000, Errors: 50},
			wantStatus: "degraded",
			wantShort:  10,
			wantLong:   10,
		},
		{
			name:       "clean short window recovers an outage",
			previous:   "outage",
			short:      datadog.WindowCounts{Requests: 500},
			long:       datadog.WindowCounts{Requests: 6000, Errors: 600},
			wantStatus: "operational",
			wantShort:  0,
			wantLong:   20,
		},
		{
			name:       "two errors are noise below the minimum",
			previous:   "operational",
			short:      datadog.WindowCounts{Requests: 20, Errors: 2},
			long:       datadog.WindowCounts{Requests: 20, Errors: 2},
			wantStatus: "operational",
			wantShort:  20,
			wantLong:   20,
		},
		{
			name:       "empty long window takes the short burn",
			previous:   "not_configured",
			short:      datadog.WindowCounts{Requests: 100, Errors: 20},
			long:       datadog.WindowCounts{},
			wantStatus: "outage",
			wantShort:  40,
			wantLong:   40,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &fakeProvider{errs: []error{nil}, windows: burningWindows(tt.short, tt.long)}
			intervals := &fakeIntervalWriter{}
			statuses := &fakeStatusUpdater{}
			p, _ := newTestPoller(provider, intervals, statuses)

			before := time.Now()
			if err := p.pollService(t.Context(), db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: tt.previous}); err != nil {
				t.Fatalf("pollService() returned unexpected error: %v", err)
			}
			after := time.Now()

			if len(intervals.calls) != 1 || intervals.calls[0].status != tt.wantStatus {
				t.Errorf("OpenOrExtend calls = %+v, want one with status %q", intervals.calls, tt.wantStatus)
			}
			if len(statuses.calls) != 1 {
				t.Fatalf("status writes = %+v, want exactly 1", statuses.calls)
			}
			got := statuses.calls[0]
			if got.status != tt.wantStatus {
				t.Errorf("status = %q, want %q", got.status, tt.wantStatus)
			}
			if got.reading == nil {
				t.Fatal("status written via UpdateStatus, want UpdateStatusAndReading with a reading")
			}
			rd := *got.reading
			if math.Abs(rd.BurnShort-tt.wantShort) > 1e-9 || math.Abs(rd.BurnLong-tt.wantLong) > 1e-9 {
				t.Errorf("burn short/long = %v/%v, want %v/%v", rd.BurnShort, rd.BurnLong, tt.wantShort, tt.wantLong)
			}
			if rd.ShortErrors != tt.short.Errors || rd.ShortRequests != tt.short.Requests {
				t.Errorf("short errors/requests = %d/%d, want %d/%d", rd.ShortErrors, rd.ShortRequests, tt.short.Errors, tt.short.Requests)
			}
			if rd.ReadAt.Before(before) || rd.ReadAt.After(after) {
				t.Errorf("ReadAt = %v, want within [%v, %v]", rd.ReadAt, before, after)
			}
		})
	}
}

// TestPollService_StateFallback covers BURN-09 and NB-7: no usable target,
// or no request counts at all, classifies from Datadog's state over the
// short window (a second, short-range call - never the windows call's ~1h
// state) and writes no reading. Each case gives the windows call a State
// that would classify differently, proving which one is used, and counts
// that would burn, proving they are ignored.
func TestPollService_StateFallback(t *testing.T) {
	burning := datadog.WindowCounts{Requests: 100, Errors: 50}
	tests := []struct {
		name       string
		windows    datadog.SLOWindows
		shortState string
		wantStatus string
	}{
		{
			name:       "target <= 0 with ok short-window state ignores burning counts",
			windows:    datadog.SLOWindows{State: "breached", Target: 0, Short: burning, Long: burning, HasSeries: true},
			shortState: "ok",
			wantStatus: "operational",
		},
		{
			name:       "target <= 0 with breached short-window state is outage",
			windows:    datadog.SLOWindows{State: "ok", Target: 0, Short: burning, Long: burning, HasSeries: true},
			shortState: "breached",
			wantStatus: "outage",
		},
		{
			name:       "monitor-based SLO uses the short-window warning state",
			windows:    datadog.SLOWindows{State: "ok", Target: 99.5, HasSeries: false},
			shortState: "warning",
			wantStatus: "degraded",
		},
		{
			name:       "monitor-based SLO recovers on the short-window ok state",
			windows:    datadog.SLOWindows{State: "breached", Target: 99.5, HasSeries: false},
			shortState: "ok",
			wantStatus: "operational",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &fakeProvider{
				errs:    []error{nil},
				windows: tt.windows,
				status:  datadog.SLOStatus{State: tt.shortState, SLI: 99.9, Target: 99.5, ErrorBudgetRemaining: 0.4},
			}
			intervals := &fakeIntervalWriter{}
			statuses := &fakeStatusUpdater{}
			p, _ := newTestPoller(provider, intervals, statuses)

			if err := p.pollService(t.Context(), db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: "outage", LastStatusChangeAt: time.Now()}); err != nil {
				t.Fatalf("pollService() returned unexpected error: %v", err)
			}

			if len(statuses.calls) != 1 || statuses.calls[0].status != tt.wantStatus {
				t.Fatalf("status writes = %+v, want one with status %q", statuses.calls, tt.wantStatus)
			}
			if statuses.calls[0].reading != nil {
				t.Errorf("reading = %+v, want nil (state fallback writes via UpdateStatus, no reading)", *statuses.calls[0].reading)
			}
			if len(intervals.calls) != 1 || intervals.calls[0].status != tt.wantStatus {
				t.Errorf("OpenOrExtend calls = %+v, want one with status %q", intervals.calls, tt.wantStatus)
			}
			if intervals.calls[0].errorBudget != 0.4 {
				t.Errorf("error budget = %v, want 0.4 (from the short-window status)", intervals.calls[0].errorBudget)
			}
		})
	}
}

func TestPollService_StateFallback_FetchesShortWindowEndingWithTheWindows(t *testing.T) {
	provider := &fakeProvider{
		errs:    []error{nil},
		windows: datadog.SLOWindows{State: "ok", Target: 99.5, HasSeries: false},
		status:  datadog.SLOStatus{State: "ok"},
	}
	p, _ := newTestPoller(provider, &fakeIntervalWriter{}, &fakeStatusUpdater{})

	if err := p.pollService(t.Context(), db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: "operational"}); err != nil {
		t.Fatalf("pollService() returned unexpected error: %v", err)
	}

	if provider.statusCalls != 1 {
		t.Fatalf("FetchSLOStatus calls = %d, want 1", provider.statusCalls)
	}
	end := provider.gotEnd[0]
	if !provider.gotTo[0].Equal(end) {
		t.Errorf("to = %v, want %v (the windows call's end, now - recentWindowLag)", provider.gotTo[0], end)
	}
	if want := end.Add(-shortWindow); !provider.gotFrom[0].Equal(want) {
		t.Errorf("from = %v, want %v (to - shortWindow)", provider.gotFrom[0], want)
	}
}

func TestPollService_BurnPath_MakesNoStatusCall(t *testing.T) {
	provider := &fakeProvider{errs: []error{nil}, windows: healthyWindows()}
	p, _ := newTestPoller(provider, &fakeIntervalWriter{}, &fakeStatusUpdater{})

	if err := p.pollService(t.Context(), db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: "operational"}); err != nil {
		t.Fatalf("pollService() returned unexpected error: %v", err)
	}

	if provider.calls != 1 || provider.statusCalls != 0 {
		t.Errorf("windows/status calls = %d/%d, want 1/0 (burn path is one call)", provider.calls, provider.statusCalls)
	}
}

// TestPollService_StateFallbackFetchFailure_NoWritesNoDispatch covers
// BURN-11 for the fallback's second call: the same contract as the first.
func TestPollService_StateFallbackFetchFailure_NoWritesNoDispatch(t *testing.T) {
	backoffBase = time.Millisecond
	provider := &fakeProvider{
		errs:       []error{nil},
		windows:    datadog.SLOWindows{State: "ok", Target: 99.5, HasSeries: false},
		statusErrs: []error{datadog.ErrTimeout},
	}
	intervals := &fakeIntervalWriter{}
	statuses := &fakeStatusUpdater{}
	p, incidents := newTestPoller(provider, intervals, statuses)

	err := p.pollService(t.Context(), db.Service{ID: "svc-1", Name: "API", SLOID: "slo-1", CurrentStatus: "operational"})
	if !errors.Is(err, datadog.ErrTimeout) {
		t.Fatalf("pollService() error = %v, want ErrTimeout", err)
	}
	if provider.statusCalls != maxFetchAttempts {
		t.Errorf("FetchSLOStatus calls = %d, want %d (retried)", provider.statusCalls, maxFetchAttempts)
	}
	if len(intervals.calls) != 0 || len(statuses.calls) != 0 {
		t.Errorf("OpenOrExtend calls = %+v, status writes = %+v, want none after a failed fetch", intervals.calls, statuses.calls)
	}
	if created, _, _ := incidents.snapshot(); created != 0 {
		t.Errorf("incidents created = %d, want 0 (analyzer not notified)", created)
	}
}

func TestPollService_LastErrorAt(t *testing.T) {
	previous := time.Now().Add(-20 * time.Minute).Truncate(time.Second)

	t.Run("set to the cycle time when the short window has errors", func(t *testing.T) {
		provider := &fakeProvider{errs: []error{nil}, windows: burningWindows(
			datadog.WindowCounts{Requests: 500, Errors: 1}, datadog.WindowCounts{Requests: 6000, Errors: 1})}
		statuses := &fakeStatusUpdater{}
		p, _ := newTestPoller(provider, &fakeIntervalWriter{}, statuses)

		before := time.Now()
		if err := p.pollService(t.Context(), db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: "operational", LastErrorAt: &previous}); err != nil {
			t.Fatalf("pollService() returned unexpected error: %v", err)
		}
		after := time.Now()

		got := statuses.calls[0].reading.LastErrorAt
		if got == nil || got.Before(before) || got.After(after) {
			t.Errorf("LastErrorAt = %v, want within [%v, %v]", got, before, after)
		}
	})

	t.Run("carried forward when the short window has no errors", func(t *testing.T) {
		provider := &fakeProvider{errs: []error{nil}, windows: healthyWindows()}
		statuses := &fakeStatusUpdater{}
		p, _ := newTestPoller(provider, &fakeIntervalWriter{}, statuses)

		if err := p.pollService(t.Context(), db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: "operational", LastErrorAt: &previous}); err != nil {
			t.Fatalf("pollService() returned unexpected error: %v", err)
		}

		got := statuses.calls[0].reading.LastErrorAt
		if got == nil || !got.Equal(previous) {
			t.Errorf("LastErrorAt = %v, want the previous %v carried forward", got, previous)
		}
	})

	t.Run("stays nil when errors were never seen", func(t *testing.T) {
		provider := &fakeProvider{errs: []error{nil}, windows: healthyWindows()}
		statuses := &fakeStatusUpdater{}
		p, _ := newTestPoller(provider, &fakeIntervalWriter{}, statuses)

		if err := p.pollService(t.Context(), db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: "operational"}); err != nil {
			t.Fatalf("pollService() returned unexpected error: %v", err)
		}

		if got := statuses.calls[0].reading.LastErrorAt; got != nil {
			t.Errorf("LastErrorAt = %v, want nil", *got)
		}
	})
}

// TestPollService_ZeroTrafficGraceFromServiceRow covers BURN-06 through
// pollService: the grace timer is read from the service row
// (last_error_at, falling back to last_status_change_at), never from
// in-memory poller state.
func TestPollService_ZeroTrafficGraceFromServiceRow(t *testing.T) {
	empty := burningWindows(datadog.WindowCounts{}, datadog.WindowCounts{})
	tests := []struct {
		name         string
		lastErrorAgo time.Duration
		useErrorAt   bool
		changeAgo    time.Duration
		wantStatus   string
	}{
		{name: "last error 10 min ago keeps outage", lastErrorAgo: 10 * time.Minute, useErrorAt: true, changeAgo: time.Hour, wantStatus: "outage"},
		{name: "last error 16 min ago recovers", lastErrorAgo: 16 * time.Minute, useErrorAt: true, changeAgo: time.Hour, wantStatus: "operational"},
		{name: "no last error falls back to last change, 10 min ago", changeAgo: 10 * time.Minute, wantStatus: "outage"},
		{name: "no last error falls back to last change, 16 min ago", changeAgo: 16 * time.Minute, wantStatus: "operational"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &fakeProvider{errs: []error{nil}, windows: empty}
			statuses := &fakeStatusUpdater{}
			p, _ := newTestPoller(provider, &fakeIntervalWriter{}, statuses)

			svc := db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: "outage", LastStatusChangeAt: time.Now().Add(-tt.changeAgo)}
			if tt.useErrorAt {
				at := time.Now().Add(-tt.lastErrorAgo)
				svc.LastErrorAt = &at
			}
			if err := p.pollService(t.Context(), svc); err != nil {
				t.Fatalf("pollService() returned unexpected error: %v", err)
			}

			if len(statuses.calls) != 1 || statuses.calls[0].status != tt.wantStatus {
				t.Errorf("status writes = %+v, want one with status %q", statuses.calls, tt.wantStatus)
			}
		})
	}
}

// TestPollService_FetchFailure_NoWritesNoDispatch covers BURN-11: after
// retries are exhausted pollService returns the error and writes nothing.
func TestPollService_FetchFailure_NoWritesNoDispatch(t *testing.T) {
	backoffBase = time.Millisecond
	provider := &fakeProvider{errs: []error{datadog.ErrTimeout, datadog.ErrTimeout, datadog.ErrTimeout}}
	intervals := &fakeIntervalWriter{}
	statuses := &fakeStatusUpdater{}
	p, incidents := newTestPoller(provider, intervals, statuses)

	err := p.pollService(t.Context(), db.Service{ID: "svc-1", SLOID: "slo-1", CurrentStatus: "operational"})
	if !errors.Is(err, datadog.ErrTimeout) {
		t.Fatalf("pollService() error = %v, want ErrTimeout", err)
	}
	if provider.calls != maxFetchAttempts {
		t.Errorf("provider calls = %d, want %d (retried)", provider.calls, maxFetchAttempts)
	}
	if len(intervals.calls) != 0 || len(statuses.calls) != 0 {
		t.Errorf("OpenOrExtend calls = %+v, status writes = %+v, want none after a failed fetch", intervals.calls, statuses.calls)
	}
	if created, _, _ := incidents.snapshot(); created != 0 {
		t.Errorf("incidents created = %d, want 0 (analyzer not notified)", created)
	}
}

// TestPollService_TransitionDispatchedOnceAfterPersistence covers BURN-11's
// analyzer-hook clause: the transition reaches the analyzer exactly once,
// only after the status (and reading) were persisted.
func TestPollService_TransitionDispatchedOnceAfterPersistence(t *testing.T) {
	provider := &fakeProvider{errs: []error{nil}, windows: burningWindows(
		datadog.WindowCounts{Requests: 100, Errors: 20}, datadog.WindowCounts{Requests: 1000, Errors: 100})}
	intervals := &fakeIntervalWriter{}
	statuses := &fakeStatusUpdater{}
	p, incidents := newTestPoller(provider, intervals, statuses)

	createdAtWrite := -1
	statuses.onWrite = func() { createdAtWrite, _, _ = incidents.snapshot() }

	if err := p.pollService(t.Context(), db.Service{ID: "svc-1", Name: "API", SLOID: "slo-1", CurrentStatus: "operational"}); err != nil {
		t.Fatalf("pollService() returned unexpected error: %v", err)
	}

	if createdAtWrite != 0 {
		t.Errorf("incidents created before the status write = %d, want 0 (dispatch must follow persistence)", createdAtWrite)
	}
	if created, _, _ := incidents.snapshot(); created != 1 {
		t.Errorf("incidents created = %d, want exactly 1 (one dispatch for the operational -> outage transition)", created)
	}
}

func TestPollService_NoTransition_NoDispatch(t *testing.T) {
	provider := &fakeProvider{errs: []error{nil}, windows: burningWindows(
		datadog.WindowCounts{Requests: 100, Errors: 20}, datadog.WindowCounts{Requests: 1000, Errors: 100})}
	statuses := &fakeStatusUpdater{}
	p, incidents := newTestPoller(provider, &fakeIntervalWriter{}, statuses)

	if err := p.pollService(t.Context(), db.Service{ID: "svc-1", Name: "API", SLOID: "slo-1", CurrentStatus: "outage"}); err != nil {
		t.Fatalf("pollService() returned unexpected error: %v", err)
	}

	if len(statuses.calls) != 1 || statuses.calls[0].status != "outage" {
		t.Errorf("status writes = %+v, want one with status %q", statuses.calls, "outage")
	}
	if created, _, _ := incidents.snapshot(); created != 0 {
		t.Errorf("incidents created = %d, want 0 (no transition, no dispatch)", created)
	}
}

func TestPollService_PersistenceFailure_NoDispatch(t *testing.T) {
	burning := burningWindows(datadog.WindowCounts{Requests: 100, Errors: 20}, datadog.WindowCounts{Requests: 1000, Errors: 100})
	writeErr := errors.New("write failed")

	t.Run("status write fails", func(t *testing.T) {
		provider := &fakeProvider{errs: []error{nil}, windows: burning}
		statuses := &fakeStatusUpdater{err: writeErr}
		p, incidents := newTestPoller(provider, &fakeIntervalWriter{}, statuses)

		err := p.pollService(t.Context(), db.Service{ID: "svc-1", Name: "API", SLOID: "slo-1", CurrentStatus: "operational"})
		if !errors.Is(err, writeErr) {
			t.Fatalf("pollService() error = %v, want %v", err, writeErr)
		}
		if created, _, _ := incidents.snapshot(); created != 0 {
			t.Errorf("incidents created = %d, want 0 (analyzer not notified when persistence fails)", created)
		}
	})

	t.Run("interval write fails", func(t *testing.T) {
		provider := &fakeProvider{errs: []error{nil}, windows: burning}
		statuses := &fakeStatusUpdater{}
		p, incidents := newTestPoller(provider, &fakeIntervalWriter{err: writeErr}, statuses)

		err := p.pollService(t.Context(), db.Service{ID: "svc-1", Name: "API", SLOID: "slo-1", CurrentStatus: "operational"})
		if !errors.Is(err, writeErr) {
			t.Fatalf("pollService() error = %v, want %v", err, writeErr)
		}
		if len(statuses.calls) != 0 {
			t.Errorf("status writes = %+v, want none after the interval write failed", statuses.calls)
		}
		if created, _, _ := incidents.snapshot(); created != 0 {
			t.Errorf("incidents created = %d, want 0", created)
		}
	})
}

func TestSLOStatusFromWindows(t *testing.T) {
	tests := []struct {
		name        string
		windows     datadog.SLOWindows
		wantSLI     float64
		wantBudget  float64
		wantRequest int64
	}{
		{
			name:        "short window with traffic supplies the SLI",
			windows:     datadog.SLOWindows{Target: 99.5, SLI: 99.99, Short: datadog.WindowCounts{Requests: 1000, Errors: 10}, HasSeries: true},
			wantSLI:     99,
			wantBudget:  -0.5,
			wantRequest: 1000,
		},
		{
			name:        "empty short window falls back to Datadog's SLI",
			windows:     datadog.SLOWindows{Target: 99.5, SLI: 99.9, HasSeries: true},
			wantSLI:     99.9,
			wantBudget:  0.4,
			wantRequest: 0,
		},
		{
			name:        "monitor-based SLO uses Datadog's SLI",
			windows:     datadog.SLOWindows{Target: 99, SLI: 100},
			wantSLI:     100,
			wantBudget:  1,
			wantRequest: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sloStatusFromWindows(tt.windows)
			if math.Abs(got.SLI-tt.wantSLI) > 1e-9 || math.Abs(got.ErrorBudgetRemaining-tt.wantBudget) > 1e-9 || got.RequestCount != tt.wantRequest {
				t.Errorf("SLI/budget/requests = %v/%v/%d, want %v/%v/%d", got.SLI, got.ErrorBudgetRemaining, got.RequestCount, tt.wantSLI, tt.wantBudget, tt.wantRequest)
			}
			if got.Target != tt.windows.Target || got.State != tt.windows.State {
				t.Errorf("Target/State = %v/%q, want %v/%q", got.Target, got.State, tt.windows.Target, tt.windows.State)
			}
		})
	}
}
