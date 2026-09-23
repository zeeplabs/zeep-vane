package poller

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/connectors/datadog"
	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/llm"
	"github.com/zeeplabs/zeep-vane/internal/notify"
)

// fakeIncidentStore is a no-network, no-DB fake of incidentStore that
// records every call so tests can assert exactly which methods fired.
// Mutex-protected and channel-notified because dispatchOutageEnrichment/
// dispatchClosingCommentEnrichment call SetDescription/
// SetPendingCloseComment from a goroutine (T14) - tests must synchronize
// on that write rather than racing it or sleep-polling for it.
type fakeIncidentStore struct {
	mu            sync.Mutex
	openIncidents map[string]string // serviceID -> incidentID, present means "open"

	createCalls      []*db.Incident
	createServiceIDs [][]string
	createErr        error
	hasOpenErr       error

	// notAllOperational, keyed by incident ID, opts an incident out of the
	// default "all linked services operational" fake response - tests that
	// need AllLinkedServicesOperational to return false set the entry
	// before invoking HandleTransition. Absent/false means "all
	// operational" (the default every existing recovery test relies on).
	notAllOperational       map[string]bool
	allLinkedOperationalErr error

	setDescriptionCalls []string // incidentID
	setDescriptionTexts []string // description, parallel to setDescriptionCalls
	setDescriptionErr   error
	setDescriptionDone  chan struct{} // signaled once per SetDescription call, if non-nil

	setPendingCommentCalls []string // incidentID
	setPendingCommentErr   error
	setPendingCommentDone  chan struct{} // signaled once per SetPendingCloseComment call, if non-nil
}

func (f *fakeIncidentStore) Create(ctx context.Context, incident *db.Incident, serviceIDs []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	f.createCalls = append(f.createCalls, incident)
	f.createServiceIDs = append(f.createServiceIDs, serviceIDs)
	incident.ID = "new-incident-id"
	return nil
}

func (f *fakeIncidentStore) HasOpenIncidentForService(ctx context.Context, serviceID string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.hasOpenErr != nil {
		return "", false, f.hasOpenErr
	}
	id, ok := f.openIncidents[serviceID]
	return id, ok, nil
}

func (f *fakeIncidentStore) AllLinkedServicesOperational(ctx context.Context, incidentID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.allLinkedOperationalErr != nil {
		return false, f.allLinkedOperationalErr
	}
	return !f.notAllOperational[incidentID], nil
}

func (f *fakeIncidentStore) SetDescription(ctx context.Context, incidentID, description string) error {
	f.mu.Lock()
	err := f.setDescriptionErr
	if err == nil {
		f.setDescriptionCalls = append(f.setDescriptionCalls, incidentID)
		f.setDescriptionTexts = append(f.setDescriptionTexts, description)
	}
	done := f.setDescriptionDone
	f.mu.Unlock()
	if done != nil {
		done <- struct{}{}
	}
	return err
}

func (f *fakeIncidentStore) SetPendingCloseComment(ctx context.Context, incidentID, comment string) error {
	f.mu.Lock()
	err := f.setPendingCommentErr
	if err == nil {
		f.setPendingCommentCalls = append(f.setPendingCommentCalls, incidentID)
	}
	done := f.setPendingCommentDone
	f.mu.Unlock()
	if done != nil {
		done <- struct{}{}
	}
	return err
}

func (f *fakeIncidentStore) snapshot() (createCalls int, setDescriptionCalls, setPendingCommentCalls []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.createCalls), append([]string(nil), f.setDescriptionCalls...), append([]string(nil), f.setPendingCommentCalls...)
}

// snapshotDescriptionTexts returns the description text passed to each
// SetDescription call, parallel to setDescriptionCalls.
func (f *fakeIncidentStore) snapshotDescriptionTexts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.setDescriptionTexts...)
}

// fakeStatusAnalysisWriter is a no-DB fake of statusAnalysisWriter.
// Mutex-protected and channel-notified for the same reason as
// fakeIncidentStore - dispatchDegradedEnrichment calls
// UpdateStatusAnalysis from a goroutine.
type fakeStatusAnalysisWriter struct {
	mu    sync.Mutex
	calls []*string // nil entry means UpdateStatusAnalysis(ctx, id, nil)
	err   error
	done  chan struct{} // signaled once per call, if non-nil
}

func (f *fakeStatusAnalysisWriter) UpdateStatusAnalysis(ctx context.Context, serviceID string, analysis *string) error {
	f.mu.Lock()
	err := f.err
	if err == nil {
		f.calls = append(f.calls, analysis)
	}
	done := f.done
	f.mu.Unlock()
	if done != nil {
		done <- struct{}{}
	}
	return err
}

func (f *fakeStatusAnalysisWriter) snapshot() []*string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*string(nil), f.calls...)
}

// fakeLLMGenerator is a no-network fake of llmGenerator with a
// per-method controllable result/error/delay, so T14's tests can exercise
// success, failure, and "still running past the caller's return" without
// any real network call.
type fakeLLMGenerator struct {
	degradedResult, outageResult, closingResult string
	degradedErr, outageErr, closingErr          error
	degradedDelay, outageDelay, closingDelay    time.Duration
	// block, when non-nil, makes every Generate* method wait on ctx.Done()
	// instead of returning - used by the timing test to simulate a
	// provider call that never completes on its own.
	block bool

	// inputsMu guards lastDegradedInput/lastOutageInput/lastClosingInput,
	// captured on every call so a test can assert exactly what
	// llm.AnalysisInput (including CauseType/CauseMessage, T11) each
	// dispatch goroutine built before calling in.
	inputsMu                                             sync.Mutex
	lastDegradedInput, lastOutageInput, lastClosingInput llm.AnalysisInput
}

func (f *fakeLLMGenerator) GenerateDegradedAnalysis(ctx context.Context, in llm.AnalysisInput) (string, error) {
	f.inputsMu.Lock()
	f.lastDegradedInput = in
	f.inputsMu.Unlock()
	return f.run(ctx, f.degradedDelay, f.degradedResult, f.degradedErr)
}

func (f *fakeLLMGenerator) GenerateOutageDescription(ctx context.Context, in llm.AnalysisInput) (string, error) {
	f.inputsMu.Lock()
	f.lastOutageInput = in
	f.inputsMu.Unlock()
	return f.run(ctx, f.outageDelay, f.outageResult, f.outageErr)
}

func (f *fakeLLMGenerator) GenerateClosingComment(ctx context.Context, in llm.AnalysisInput) (string, error) {
	f.inputsMu.Lock()
	f.lastClosingInput = in
	f.inputsMu.Unlock()
	return f.run(ctx, f.closingDelay, f.closingResult, f.closingErr)
}

// snapshotDegradedInput returns the last llm.AnalysisInput
// GenerateDegradedAnalysis received.
func (f *fakeLLMGenerator) snapshotDegradedInput() llm.AnalysisInput {
	f.inputsMu.Lock()
	defer f.inputsMu.Unlock()
	return f.lastDegradedInput
}

// snapshotOutageInput returns the last llm.AnalysisInput
// GenerateOutageDescription received.
func (f *fakeLLMGenerator) snapshotOutageInput() llm.AnalysisInput {
	f.inputsMu.Lock()
	defer f.inputsMu.Unlock()
	return f.lastOutageInput
}

func (f *fakeLLMGenerator) run(ctx context.Context, delay time.Duration, result string, err error) (string, error) {
	if f.block {
		<-ctx.Done()
		return "", ctx.Err()
	}
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return result, err
}

func newTestAnalyzer(incidents *fakeIncidentStore, services *fakeStatusAnalysisWriter, llmSvc *fakeLLMGenerator, timeout time.Duration) *SLOAnalyzer {
	return NewSLOAnalyzer(incidents, services, &fakeIntervalAnalysisWriter{}, llmSvc, timeout, zap.NewNop())
}

// fakeIntervalAnalysisWriter is a no-DB fake of intervalAnalysisWriter,
// recording every SetIntervalAnalysis call so a test can assert exactly
// which interval ID received which text (degraded-interval-analysis
// DEGINT-04/05).
type fakeIntervalAnalysisWriter struct {
	mu    sync.Mutex
	calls []struct{ intervalID, analysis string }
	done  chan struct{} // signaled once per call, if non-nil
}

func (f *fakeIntervalAnalysisWriter) SetIntervalAnalysis(ctx context.Context, intervalID, analysis string) error {
	f.mu.Lock()
	f.calls = append(f.calls, struct{ intervalID, analysis string }{intervalID, analysis})
	done := f.done
	f.mu.Unlock()
	if done != nil {
		done <- struct{}{}
	}
	return nil
}

func (f *fakeIntervalAnalysisWriter) snapshot() []struct{ intervalID, analysis string } {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]struct{ intervalID, analysis string }(nil), f.calls...)
}

// waitOrTimeout waits up to 2s for ch to receive, failing the test on
// timeout - the standard "wait for the async goroutine to finish" pattern
// used throughout this file instead of a fixed sleep.
func waitOrTimeout(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for async goroutine to finish")
	}
}

func TestSLOAnalyzer_HandleTransition_NoOp_SameStatus_CallsNothing(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{}, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "operational", datadog.SLOStatus{}, "test-interval")

	createCalls, _, _ := incidents.snapshot()
	if createCalls != 0 {
		t.Errorf("Create called %d times, want 0", createCalls)
	}
	if len(services.snapshot()) != 0 {
		t.Errorf("UpdateStatusAnalysis called %d times, want 0", len(services.snapshot()))
	}
}

// TestSLOAnalyzer_HandleTransition_NoOp_SameStatus_Degraded_CallsNothing and
// its outage counterpart below are the defense-in-depth regression tests for
// AI-13/AI-18: unlike the operational->operational no-op case above (inert
// only because no incident exists to duplicate), these fixtures are set up
// so that, if the previousStatus == newStatus guard were ever removed,
// HandleTransition would fall through into dispatchDegradedEnrichment /
// handleOutageTransition and register a call the assertions below would
// catch (verified by temporarily removing the guard: both tests fail
// without it and pass with it restored).
func TestSLOAnalyzer_HandleTransition_NoOp_SameStatus_Degraded_CallsNothing(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{}, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "degraded", "degraded", datadog.SLOStatus{}, "test-interval")

	if len(services.snapshot()) != 0 {
		t.Errorf("UpdateStatusAnalysis called %d times, want 0 (no-op guard must prevent re-clearing/re-dispatching for an unchanged degraded status)", len(services.snapshot()))
	}
	createCalls, _, _ := incidents.snapshot()
	if createCalls != 0 {
		t.Errorf("Create called %d times, want 0", createCalls)
	}
}

// TestSLOAnalyzer_HandleTransition_NoOp_SameStatus_Outage_CallsNothing seeds
// no open incident (openIncidents empty) - without the no-op guard,
// outage->outage would fall through to handleOutageTransition, find no
// existing incident, and call Create, which the assertion below would catch.
func TestSLOAnalyzer_HandleTransition_NoOp_SameStatus_Outage_CallsNothing(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{}, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "outage", "outage", datadog.SLOStatus{}, "test-interval")

	createCalls, setDescriptionCalls, _ := incidents.snapshot()
	if createCalls != 0 {
		t.Errorf("Create called %d times, want 0 (no-op guard must prevent creating a duplicate incident for an unchanged outage status)", createCalls)
	}
	if len(setDescriptionCalls) != 0 {
		t.Errorf("SetDescription called %d times, want 0", len(setDescriptionCalls))
	}
}

func TestSLOAnalyzer_HandleTransition_OutageNoExistingIncident_CreatesAutoIncident(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}, setDescriptionDone: make(chan struct{}, 1)}
	services := &fakeStatusAnalysisWriter{}
	llmSvc := &fakeLLMGenerator{outageResult: "A real outage description."}
	a := newTestAnalyzer(incidents, services, llmSvc, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "outage", datadog.SLOStatus{}, "test-interval")

	createCalls, _, _ := incidents.snapshot()
	if createCalls != 1 {
		t.Fatalf("Create called %d times, want 1", createCalls)
	}
	created := incidents.createCalls[0]
	if !created.AutoCreated {
		t.Error("AutoCreated = false, want true")
	}
	if created.Description == nil || *created.Description == "" {
		t.Error("Description is nil/empty, want a generic fallback description")
	}
	if len(incidents.createServiceIDs[0]) != 1 || incidents.createServiceIDs[0][0] != "svc-1" {
		t.Errorf("serviceIDs = %v, want [svc-1]", incidents.createServiceIDs[0])
	}

	waitOrTimeout(t, incidents.setDescriptionDone)
	_, setDescriptionCalls, _ := incidents.snapshot()
	if len(setDescriptionCalls) != 1 || setDescriptionCalls[0] != "new-incident-id" {
		t.Errorf("SetDescription calls = %v, want [new-incident-id] (async enrichment must overwrite the generic description)", setDescriptionCalls)
	}
}

func TestSLOAnalyzer_HandleTransition_OutageAlreadyOpenIncident_CreatesNothing(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{"svc-1": "existing-incident"}}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{}, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "outage", datadog.SLOStatus{}, "test-interval")

	createCalls, _, _ := incidents.snapshot()
	if createCalls != 0 {
		t.Errorf("Create called %d times, want 0 (an incident is already open)", createCalls)
	}
}

func TestSLOAnalyzer_HandleTransition_IntoDegraded_ClearsStatusAnalysisSynchronously(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{}
	llmSvc := &fakeLLMGenerator{degradedDelay: 50 * time.Millisecond, degradedResult: "tooltip text"}
	a := newTestAnalyzer(incidents, services, llmSvc, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "degraded", datadog.SLOStatus{}, "test-interval")

	// The synchronous clear must be visible immediately, before the async
	// enrichment (delayed 50ms above) has had a chance to run.
	calls := services.snapshot()
	if len(calls) != 1 {
		t.Fatalf("UpdateStatusAnalysis called %d times synchronously, want 1", len(calls))
	}
	if calls[0] != nil {
		t.Errorf("UpdateStatusAnalysis called with %q, want nil (synchronous clear)", *calls[0])
	}
}

func TestSLOAnalyzer_HandleTransition_AwayFromDegraded_ClearsStatusAnalysis(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{}, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "degraded", "operational", datadog.SLOStatus{}, "test-interval")

	calls := services.snapshot()
	if len(calls) != 1 {
		t.Fatalf("UpdateStatusAnalysis called %d times, want 1", len(calls))
	}
	if calls[0] != nil {
		t.Errorf("UpdateStatusAnalysis called with %q, want nil", *calls[0])
	}
}

func TestSLOAnalyzer_HandleTransition_DegradedToOutage_ClearsStatusAnalysisAndCreatesIncidentOnce(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{}, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "degraded", "outage", datadog.SLOStatus{}, "test-interval")

	if len(services.snapshot()) != 1 {
		t.Errorf("UpdateStatusAnalysis called %d times, want 1 (leaving degraded)", len(services.snapshot()))
	}
	createCalls, _, _ := incidents.snapshot()
	if createCalls != 1 {
		t.Errorf("Create called %d times, want 1 (entering outage)", createCalls)
	}
}

func TestSLOAnalyzer_HandleTransition_OperationalWithOpenIncident_DispatchesClosingCommentAsync(t *testing.T) {
	incidents := &fakeIncidentStore{
		openIncidents:         map[string]string{"svc-1": "existing-incident"},
		setPendingCommentDone: make(chan struct{}, 1),
	}
	services := &fakeStatusAnalysisWriter{}
	llmSvc := &fakeLLMGenerator{closingResult: "Service has recovered."}
	a := newTestAnalyzer(incidents, services, llmSvc, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "outage", "operational", datadog.SLOStatus{}, "test-interval")

	createCalls, _, _ := incidents.snapshot()
	if createCalls != 0 {
		t.Errorf("Create called %d times, want 0 (operational transition never creates an incident)", createCalls)
	}

	waitOrTimeout(t, incidents.setPendingCommentDone)
	_, _, pendingCalls := incidents.snapshot()
	if len(pendingCalls) != 1 || pendingCalls[0] != "existing-incident" {
		t.Errorf("SetPendingCloseComment calls = %v, want [existing-incident]", pendingCalls)
	}
}

func TestSLOAnalyzer_HandleTransition_OperationalWithNoOpenIncident_DoesNothing(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{}, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "degraded", "operational", datadog.SLOStatus{}, "test-interval")

	createCalls, _, pendingCalls := incidents.snapshot()
	if createCalls != 0 {
		t.Errorf("Create called %d times, want 0", createCalls)
	}
	if len(pendingCalls) != 0 {
		t.Errorf("SetPendingCloseComment called %d times, want 0", len(pendingCalls))
	}
}

// TestSLOAnalyzer_HandleTransition_RecoveryWithOtherLinkedServiceStillDown_SkipsClosingProposal
// covers the post-review fix: an incident linked to more than one service
// (incident_services is N:N) must not get a closing-comment proposal just
// because one of its services recovered - every linked service must be
// operational first.
func TestSLOAnalyzer_HandleTransition_RecoveryWithOtherLinkedServiceStillDown_SkipsClosingProposal(t *testing.T) {
	incidents := &fakeIncidentStore{
		openIncidents:     map[string]string{"svc-1": "shared-incident"},
		notAllOperational: map[string]bool{"shared-incident": true},
	}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{}, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "outage", "operational", datadog.SLOStatus{}, "test-interval")

	_, _, pendingCalls := incidents.snapshot()
	if len(pendingCalls) != 0 {
		t.Errorf("SetPendingCloseComment called %d times, want 0 (another linked service is still down)", len(pendingCalls))
	}
}

func TestSLOAnalyzer_HandleTransition_HasOpenIncidentError_LogsAndDoesNotPanic(t *testing.T) {
	incidents := &fakeIncidentStore{hasOpenErr: errors.New("db unreachable")}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{}, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "outage", datadog.SLOStatus{}, "test-interval")

	createCalls, _, _ := incidents.snapshot()
	if createCalls != 0 {
		t.Errorf("Create called %d times, want 0 (HasOpenIncidentForService failed)", createCalls)
	}
}

func TestSLOAnalyzer_HandleTransition_CreateError_LogsAndDoesNotPanic(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}, createErr: errors.New("insert failed")}
	services := &fakeStatusAnalysisWriter{}
	a := newTestAnalyzer(incidents, services, &fakeLLMGenerator{}, time.Second)

	// Must not panic.
	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "outage", datadog.SLOStatus{}, "test-interval")
}

// --- T14: async enrichment dispatch ---

func TestSLOAnalyzer_DegradedEnrichment_Success_UpdatesStatusAnalysis(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{done: make(chan struct{}, 2)}
	llmSvc := &fakeLLMGenerator{degradedResult: "SLO is approaching its error budget limit."}
	a := newTestAnalyzer(incidents, services, llmSvc, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "degraded", datadog.SLOStatus{}, "test-interval")

	// Two calls: the synchronous clear, then the async write.
	waitOrTimeout(t, services.done)
	waitOrTimeout(t, services.done)

	calls := services.snapshot()
	if len(calls) != 2 {
		t.Fatalf("UpdateStatusAnalysis called %d times, want 2 (sync clear + async write)", len(calls))
	}
	if calls[1] == nil || *calls[1] != "SLO is approaching its error budget limit." {
		t.Errorf("second UpdateStatusAnalysis call = %v, want the generated analysis text", calls[1])
	}
}

// TestSLOAnalyzer_DegradedEnrichment_WritesToIntervalCapturedAtDispatch is
// the degraded-interval-analysis DEGINT-05 discriminating test: the
// enrichment goroutine for interval "interval-A" is delayed long enough
// that a second transition - closing interval-A and opening interval-B -
// happens before it finishes. The generated text must still land on
// interval-A (the one that triggered generation), never interval-B (the
// one open when the goroutine actually completes).
func TestSLOAnalyzer_DegradedEnrichment_WritesToIntervalCapturedAtDispatch(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{done: make(chan struct{}, 4)}
	intervals := &fakeIntervalAnalysisWriter{done: make(chan struct{}, 1)}
	llmSvc := &fakeLLMGenerator{degradedResult: "Latência elevada.", degradedDelay: 100 * time.Millisecond}
	a := NewSLOAnalyzer(incidents, services, intervals, llmSvc, time.Second, zap.NewNop())

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "degraded", datadog.SLOStatus{}, "interval-A")

	// Before the delayed goroutine above finishes, simulate the service
	// recovering (closes interval-A, opens interval-B) - HandleTransition
	// itself takes no interval action on this branch besides the
	// synchronous status_analysis clear, mirroring what pollService would
	// have already done at the DB layer.
	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "degraded", "operational", datadog.SLOStatus{}, "interval-B")

	waitOrTimeout(t, intervals.done)

	calls := intervals.snapshot()
	if len(calls) != 1 {
		t.Fatalf("SetIntervalAnalysis called %d times, want 1", len(calls))
	}
	if calls[0].intervalID != "interval-A" {
		t.Errorf("SetIntervalAnalysis intervalID = %q, want %q (the interval that triggered generation, not whichever is open when it finishes)", calls[0].intervalID, "interval-A")
	}
	if calls[0].analysis != "Latência elevada." {
		t.Errorf("SetIntervalAnalysis analysis = %q, want %q", calls[0].analysis, "Latência elevada.")
	}
}

func TestSLOAnalyzer_DegradedEnrichment_Failure_LeavesStatusAnalysisNull(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{done: make(chan struct{}, 2)}
	llmSvc := &fakeLLMGenerator{degradedErr: errors.New("provider unreachable")}
	a := newTestAnalyzer(incidents, services, llmSvc, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "degraded", datadog.SLOStatus{}, "test-interval")

	// Only the synchronous clear fires - GenerateDegradedAnalysis fails, so
	// the goroutine returns before calling UpdateStatusAnalysis a second
	// time.
	waitOrTimeout(t, services.done)
	time.Sleep(20 * time.Millisecond) // let the goroutine's early return (if any second call were coming) settle

	calls := services.snapshot()
	if len(calls) != 1 {
		t.Fatalf("UpdateStatusAnalysis called %d times, want 1 (only the synchronous clear; the failed enrichment writes nothing)", len(calls))
	}
}

// TestSLOAnalyzer_DegradedEnrichment_EmptyResult_LeavesStatusAnalysisNull
// covers spec.md's edge case: "IF the LLM returns an empty or clearly
// malformed response THEN the system SHALL treat it the same as a failed
// call... never publish empty or garbled text."
// internal/connectors/openai.Client.Complete can legally return ("", nil).
func TestSLOAnalyzer_DegradedEnrichment_EmptyResult_LeavesStatusAnalysisNull(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{done: make(chan struct{}, 2)}
	llmSvc := &fakeLLMGenerator{degradedResult: "   "}
	a := newTestAnalyzer(incidents, services, llmSvc, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "degraded", datadog.SLOStatus{}, "test-interval")

	// Only the synchronous clear fires - the goroutine must discard the
	// whitespace-only result instead of writing it.
	waitOrTimeout(t, services.done)
	time.Sleep(20 * time.Millisecond) // let a would-be second call (if the empty guard were missing) settle

	calls := services.snapshot()
	if len(calls) != 1 {
		t.Fatalf("UpdateStatusAnalysis called %d times, want 1 (only the synchronous clear; an empty analysis result must not be written)", len(calls))
	}
}

func TestSLOAnalyzer_OutageEnrichment_EmptyResult_LeavesGenericDescriptionInPlace(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{}
	llmSvc := &fakeLLMGenerator{outageResult: ""}
	a := newTestAnalyzer(incidents, services, llmSvc, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "outage", datadog.SLOStatus{}, "test-interval")

	time.Sleep(50 * time.Millisecond) // let the goroutine finish

	_, setDescriptionCalls, _ := incidents.snapshot()
	if len(setDescriptionCalls) != 0 {
		t.Errorf("SetDescription called %d times, want 0 (empty result must not overwrite the generic description)", len(setDescriptionCalls))
	}
}

func TestSLOAnalyzer_ClosingCommentEnrichment_EmptyResult_LeavesIncidentOpenWithNoProposal(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{"svc-1": "existing-incident"}}
	services := &fakeStatusAnalysisWriter{}
	llmSvc := &fakeLLMGenerator{closingResult: "  \n "}
	a := newTestAnalyzer(incidents, services, llmSvc, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "outage", "operational", datadog.SLOStatus{}, "test-interval")

	time.Sleep(50 * time.Millisecond) // let the goroutine finish

	_, _, pendingCalls := incidents.snapshot()
	if len(pendingCalls) != 0 {
		t.Errorf("SetPendingCloseComment called %d times, want 0 (empty result must leave no proposal)", len(pendingCalls))
	}
}

func TestSLOAnalyzer_OutageEnrichment_Success_OverwritesGenericDescription(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}, setDescriptionDone: make(chan struct{}, 1)}
	services := &fakeStatusAnalysisWriter{}
	llmSvc := &fakeLLMGenerator{outageResult: "The payments service is returning 5xx errors for most requests."}
	a := newTestAnalyzer(incidents, services, llmSvc, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "outage", datadog.SLOStatus{}, "test-interval")

	waitOrTimeout(t, incidents.setDescriptionDone)
	_, setDescriptionCalls, _ := incidents.snapshot()
	if len(setDescriptionCalls) != 1 {
		t.Fatalf("SetDescription called %d times, want 1", len(setDescriptionCalls))
	}
	texts := incidents.snapshotDescriptionTexts()
	if len(texts) != 1 || texts[0] != "The payments service is returning 5xx errors for most requests." {
		t.Errorf("SetDescription text = %v, want [%q] (the generated description, not just a call)", texts, "The payments service is returning 5xx errors for most requests.")
	}
}

func TestSLOAnalyzer_OutageEnrichment_Failure_LeavesGenericDescriptionInPlace(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{}
	llmSvc := &fakeLLMGenerator{outageErr: errors.New("provider unreachable")}
	a := newTestAnalyzer(incidents, services, llmSvc, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "outage", datadog.SLOStatus{}, "test-interval")

	time.Sleep(50 * time.Millisecond) // let the failed goroutine finish

	_, setDescriptionCalls, _ := incidents.snapshot()
	if len(setDescriptionCalls) != 0 {
		t.Errorf("SetDescription called %d times, want 0 (failed enrichment must not overwrite the generic description)", len(setDescriptionCalls))
	}
	created := incidents.createCalls[0]
	if created.Description == nil || *created.Description == "" {
		t.Error("original generic Description was cleared, want it left in place")
	}
}

func TestSLOAnalyzer_ClosingCommentEnrichment_Failure_LeavesIncidentOpenWithNoProposal(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{"svc-1": "existing-incident"}}
	services := &fakeStatusAnalysisWriter{}
	llmSvc := &fakeLLMGenerator{closingErr: errors.New("provider unreachable")}
	a := newTestAnalyzer(incidents, services, llmSvc, time.Second)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "outage", "operational", datadog.SLOStatus{}, "test-interval")

	time.Sleep(50 * time.Millisecond) // let the failed goroutine finish

	_, _, pendingCalls := incidents.snapshot()
	if len(pendingCalls) != 0 {
		t.Errorf("SetPendingCloseComment called %d times, want 0 (failed enrichment must leave no proposal)", len(pendingCalls))
	}
}

// TestSLOAnalyzer_HandleTransition_ReturnsWellBeforeBlockedLLMCallUnblocks is
// the T14-mandated proof that a hung provider call cannot delay
// HandleTransition's own return - the actual "must not delay pollOnce's
// remaining services" guarantee is proven end-to-end by T15's poller-level
// timing test; this is the analyzer-level half of that guarantee.
func TestSLOAnalyzer_HandleTransition_ReturnsWellBeforeBlockedLLMCallUnblocks(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{done: make(chan struct{}, 1)}
	llmSvc := &fakeLLMGenerator{block: true}
	// A long a.timeout - the goroutine is bounded by it eventually, but
	// this test only cares that HandleTransition itself returns almost
	// immediately, long before that bound is reached.
	a := newTestAnalyzer(incidents, services, llmSvc, 5*time.Second)

	start := time.Now()
	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "degraded", datadog.SLOStatus{}, "test-interval")
	elapsed := time.Since(start)

	if elapsed > 200*time.Millisecond {
		t.Errorf("HandleTransition() took %v, want well under the 5s timeout (blocked LLM call must not delay the caller)", elapsed)
	}
}

// recordingIncidentNotifier is an incidentNotifier double recording the
// auto-created incident-opened notifications the analyzer fires.
type recordingIncidentNotifier struct {
	tenants   []string
	summaries []notify.IncidentSummary
	err       error
}

func (n *recordingIncidentNotifier) NotifyIncidentOpened(_ context.Context, tenantID string, summary notify.IncidentSummary) error {
	n.tenants = append(n.tenants, tenantID)
	n.summaries = append(n.summaries, summary)
	return n.err
}

// TestSLOAnalyzer_AutoCreatedOutage_NotifiesIncidentOpened covers the
// notification-preferences edge case: an incident auto-created by the analyzer
// (not via the HTTP handler) still fires the incident-opened notification,
// scoped to the poll cycle's tenant.
func TestSLOAnalyzer_AutoCreatedOutage_NotifiesIncidentOpened(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	notifier := &recordingIncidentNotifier{}
	a := newTestAnalyzer(incidents, &fakeStatusAnalysisWriter{}, &fakeLLMGenerator{}, time.Second)
	a.SetNotifier(notifier)

	ctx := withTenantID(context.Background(), "tenant-1")
	a.HandleTransition(ctx, db.Service{ID: "svc-1", Name: "API"}, "operational", "outage", datadog.SLOStatus{}, "test-interval")

	if len(notifier.tenants) != 1 {
		t.Fatalf("notifications fired = %d, want exactly 1", len(notifier.tenants))
	}
	if notifier.tenants[0] != "tenant-1" {
		t.Errorf("notification tenant = %q, want %q", notifier.tenants[0], "tenant-1")
	}
	summary := notifier.summaries[0]
	if summary.IncidentID != "new-incident-id" {
		t.Errorf("summary.IncidentID = %q, want %q", summary.IncidentID, "new-incident-id")
	}
	if summary.ServiceName != "API" {
		t.Errorf("summary.ServiceName = %q, want %q", summary.ServiceName, "API")
	}
}

// TestSLOAnalyzer_AutoCreatedOutage_NoTenantContext_NoNotify covers the guard:
// without a tenant in context (the non-tenant poll path), no notification is
// attempted.
func TestSLOAnalyzer_AutoCreatedOutage_NoTenantContext_NoNotify(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	notifier := &recordingIncidentNotifier{}
	a := newTestAnalyzer(incidents, &fakeStatusAnalysisWriter{}, &fakeLLMGenerator{}, time.Second)
	a.SetNotifier(notifier)

	a.HandleTransition(context.Background(), db.Service{ID: "svc-1", Name: "API"}, "operational", "outage", datadog.SLOStatus{}, "test-interval")

	if len(notifier.tenants) != 0 {
		t.Errorf("notifications fired = %d, want 0 without a tenant context", len(notifier.tenants))
	}
}

// TestSLOAnalyzer_OutageAlreadyOpenIncident_NoNotify proves the notification
// is tied to incident creation: when an incident is already open (no new
// incident, no duplicate), no notification fires.
func TestSLOAnalyzer_OutageAlreadyOpenIncident_NoNotify(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{"svc-1": "existing-incident"}}
	notifier := &recordingIncidentNotifier{}
	a := newTestAnalyzer(incidents, &fakeStatusAnalysisWriter{}, &fakeLLMGenerator{}, time.Second)
	a.SetNotifier(notifier)

	ctx := withTenantID(context.Background(), "tenant-1")
	a.HandleTransition(ctx, db.Service{ID: "svc-1", Name: "API"}, "operational", "outage", datadog.SLOStatus{}, "test-interval")

	if len(notifier.tenants) != 0 {
		t.Errorf("notifications fired = %d, want 0 when no new incident was created", len(notifier.tenants))
	}
}

// TestSLOAnalyzer_AutoCreatedOutage_NotifierError_StillCreatesIncident proves a
// notification failure is non-fatal: the incident is still created and the
// poll cycle is not failed.
func TestSLOAnalyzer_AutoCreatedOutage_NotifierError_StillCreatesIncident(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	notifier := &recordingIncidentNotifier{err: errors.New("notify boom")}
	a := newTestAnalyzer(incidents, &fakeStatusAnalysisWriter{}, &fakeLLMGenerator{}, time.Second)
	a.SetNotifier(notifier)

	ctx := withTenantID(context.Background(), "tenant-1")
	a.HandleTransition(ctx, db.Service{ID: "svc-1", Name: "API"}, "operational", "outage", datadog.SLOStatus{}, "test-interval")

	createCalls, _, _ := incidents.snapshot()
	if createCalls != 1 {
		t.Errorf("Create called %d times, want 1 despite the notification error", createCalls)
	}
}

// fakeEnrichmentSettingsReader is a no-DB fake of enrichmentSettingsReader.
type fakeEnrichmentSettingsReader struct {
	enabled bool
	err     error
}

func (f *fakeEnrichmentSettingsReader) RootCauseEnrichmentEnabled(ctx context.Context) (bool, error) {
	return f.enabled, f.err
}

// fakeErrorCauseProvider is a no-network fake of errorCauseProvider,
// recording every call's arguments so a test can assert exactly what
// service/env/window resolveCauseHint queried with.
type fakeErrorCauseProvider struct {
	hint  datadog.CauseHint
	found bool
	err   error

	calls                int
	lastService, lastEnv string
	lastFrom, lastTo     time.Time
}

func (f *fakeErrorCauseProvider) SearchErrorTrackingIssues(ctx context.Context, service, env string, from, to time.Time) (datadog.CauseHint, bool, error) {
	f.calls++
	f.lastService, f.lastEnv, f.lastFrom, f.lastTo = service, env, from, to
	return f.hint, f.found, f.err
}

// metricSLOService is a db.Service shaped like a linked metric-type SLO
// with a single resolved service tag - the only shape resolveCauseHint
// ever queries Error Tracking for (RCA-01).
func metricSLOService() db.Service {
	return db.Service{ID: "svc-1", Name: "API", SLOType: "metric", DatadogServiceTag: "checkout-api"}
}

// TestSLOAnalyzer_ResolveCauseHint_Unset_ReturnsEmpty covers RCA-04: an
// analyzer that never called SetErrorCauseEnrichment (every pre-feature
// caller/test) must behave exactly as before - resolveCauseHint short-
// circuits to empty without touching either dependency.
func TestSLOAnalyzer_ResolveCauseHint_Unset_ReturnsEmpty(t *testing.T) {
	a := newTestAnalyzer(&fakeIncidentStore{}, &fakeStatusAnalysisWriter{}, &fakeLLMGenerator{}, time.Second)

	causeType, causeMessage := a.resolveCauseHint(context.Background(), metricSLOService())

	if causeType != "" || causeMessage != "" {
		t.Errorf("resolveCauseHint = (%q, %q), want (\"\", \"\") when SetErrorCauseEnrichment was never called", causeType, causeMessage)
	}
}

// TestSLOAnalyzer_ResolveCauseHint_ToggleOff_ReturnsEmptyAndSkipsProvider
// covers RCA-04's "zero extra Datadog API calls when disabled" guarantee.
func TestSLOAnalyzer_ResolveCauseHint_ToggleOff_ReturnsEmptyAndSkipsProvider(t *testing.T) {
	a := newTestAnalyzer(&fakeIncidentStore{}, &fakeStatusAnalysisWriter{}, &fakeLLMGenerator{}, time.Second)
	provider := &fakeErrorCauseProvider{found: true, hint: datadog.CauseHint{ErrorType: "X", ErrorMessage: "Y"}}
	a.SetErrorCauseEnrichment(&fakeEnrichmentSettingsReader{enabled: false}, provider)

	causeType, causeMessage := a.resolveCauseHint(context.Background(), metricSLOService())

	if causeType != "" || causeMessage != "" {
		t.Errorf("resolveCauseHint = (%q, %q), want (\"\", \"\") when the toggle is off", causeType, causeMessage)
	}
	if provider.calls != 0 {
		t.Errorf("errorCauseProvider called %d times, want 0 when the toggle is off", provider.calls)
	}
}

// TestSLOAnalyzer_ResolveCauseHint_WrongSLOType_ReturnsEmptyAndSkipsProvider
// covers RCA-01's scoping to metric-type SLOs only.
func TestSLOAnalyzer_ResolveCauseHint_WrongSLOType_ReturnsEmptyAndSkipsProvider(t *testing.T) {
	a := newTestAnalyzer(&fakeIncidentStore{}, &fakeStatusAnalysisWriter{}, &fakeLLMGenerator{}, time.Second)
	provider := &fakeErrorCauseProvider{found: true, hint: datadog.CauseHint{ErrorType: "X", ErrorMessage: "Y"}}
	a.SetErrorCauseEnrichment(&fakeEnrichmentSettingsReader{enabled: true}, provider)

	svc := metricSLOService()
	svc.SLOType = "monitor"
	causeType, causeMessage := a.resolveCauseHint(context.Background(), svc)

	if causeType != "" || causeMessage != "" {
		t.Errorf("resolveCauseHint = (%q, %q), want (\"\", \"\") for a monitor-type SLO", causeType, causeMessage)
	}
	if provider.calls != 0 {
		t.Errorf("errorCauseProvider called %d times, want 0 for a monitor-type SLO", provider.calls)
	}
}

// TestSLOAnalyzer_ResolveCauseHint_NoServiceTag_ReturnsEmptyAndSkipsProvider
// covers RCA-01's scoping to a single resolved service tag - a flow-type
// SLO (0 or 2+ service_tags) has DatadogServiceTag == "".
func TestSLOAnalyzer_ResolveCauseHint_NoServiceTag_ReturnsEmptyAndSkipsProvider(t *testing.T) {
	a := newTestAnalyzer(&fakeIncidentStore{}, &fakeStatusAnalysisWriter{}, &fakeLLMGenerator{}, time.Second)
	provider := &fakeErrorCauseProvider{found: true, hint: datadog.CauseHint{ErrorType: "X", ErrorMessage: "Y"}}
	a.SetErrorCauseEnrichment(&fakeEnrichmentSettingsReader{enabled: true}, provider)

	svc := metricSLOService()
	svc.DatadogServiceTag = ""
	causeType, causeMessage := a.resolveCauseHint(context.Background(), svc)

	if causeType != "" || causeMessage != "" {
		t.Errorf("resolveCauseHint = (%q, %q), want (\"\", \"\") when DatadogServiceTag is empty", causeType, causeMessage)
	}
	if provider.calls != 0 {
		t.Errorf("errorCauseProvider called %d times, want 0 when DatadogServiceTag is empty", provider.calls)
	}
}

// TestSLOAnalyzer_ResolveCauseHint_ProviderError_ReturnsEmpty covers
// RCA-03: a failing Error Tracking query must never surface as an error to
// the caller - just an empty cause, same as any other fallback.
func TestSLOAnalyzer_ResolveCauseHint_ProviderError_ReturnsEmpty(t *testing.T) {
	a := newTestAnalyzer(&fakeIncidentStore{}, &fakeStatusAnalysisWriter{}, &fakeLLMGenerator{}, time.Second)
	provider := &fakeErrorCauseProvider{err: datadog.ErrUnauthorized}
	a.SetErrorCauseEnrichment(&fakeEnrichmentSettingsReader{enabled: true}, provider)

	causeType, causeMessage := a.resolveCauseHint(context.Background(), metricSLOService())

	if causeType != "" || causeMessage != "" {
		t.Errorf("resolveCauseHint = (%q, %q), want (\"\", \"\") when the provider errors", causeType, causeMessage)
	}
}

// TestSLOAnalyzer_ResolveCauseHint_NotFound_ReturnsEmpty covers RCA-03's
// "zero issues in the window is not an error" case.
func TestSLOAnalyzer_ResolveCauseHint_NotFound_ReturnsEmpty(t *testing.T) {
	a := newTestAnalyzer(&fakeIncidentStore{}, &fakeStatusAnalysisWriter{}, &fakeLLMGenerator{}, time.Second)
	provider := &fakeErrorCauseProvider{found: false}
	a.SetErrorCauseEnrichment(&fakeEnrichmentSettingsReader{enabled: true}, provider)

	causeType, causeMessage := a.resolveCauseHint(context.Background(), metricSLOService())

	if causeType != "" || causeMessage != "" {
		t.Errorf("resolveCauseHint = (%q, %q), want (\"\", \"\") when the query finds nothing", causeType, causeMessage)
	}
}

// TestSLOAnalyzer_ResolveCauseHint_Success_ReturnsCauseTypeAndMessage
// covers RCA-01/RCA-02: a found issue's ErrorType/ErrorMessage must pass
// through unchanged, and the query must use svc's DatadogServiceTag and the
// fixed "production" env.
func TestSLOAnalyzer_ResolveCauseHint_Success_ReturnsCauseTypeAndMessage(t *testing.T) {
	a := newTestAnalyzer(&fakeIncidentStore{}, &fakeStatusAnalysisWriter{}, &fakeLLMGenerator{}, time.Second)
	provider := &fakeErrorCauseProvider{
		found: true,
		hint:  datadog.CauseHint{ErrorType: "MongooseError", ErrorMessage: "Operation timed out"},
	}
	a.SetErrorCauseEnrichment(&fakeEnrichmentSettingsReader{enabled: true}, provider)

	causeType, causeMessage := a.resolveCauseHint(context.Background(), metricSLOService())

	if causeType != "MongooseError" || causeMessage != "Operation timed out" {
		t.Errorf("resolveCauseHint = (%q, %q), want (%q, %q)", causeType, causeMessage, "MongooseError", "Operation timed out")
	}
	if provider.calls != 1 {
		t.Fatalf("errorCauseProvider called %d times, want 1", provider.calls)
	}
	if provider.lastService != "checkout-api" {
		t.Errorf("queried service = %q, want %q", provider.lastService, "checkout-api")
	}
	if provider.lastEnv != "production" {
		t.Errorf("queried env = %q, want %q", provider.lastEnv, "production")
	}
	if window := provider.lastTo.Sub(provider.lastFrom); window != errorTrackingWindow {
		t.Errorf("queried window = %v, want %v", window, errorTrackingWindow)
	}
}

// TestSLOAnalyzer_DegradedEnrichment_CausePopulated_PassesCauseIntoAnalysisInput
// covers RCA-01/RCA-05: a degraded transition, with enrichment enabled and
// a matching Error Tracking issue found, must reach
// GenerateDegradedAnalysis with CauseType/CauseMessage populated.
func TestSLOAnalyzer_DegradedEnrichment_CausePopulated_PassesCauseIntoAnalysisInput(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{done: make(chan struct{}, 2)}
	llmSvc := &fakeLLMGenerator{degradedResult: "tooltip text"}
	a := newTestAnalyzer(incidents, services, llmSvc, time.Second)
	provider := &fakeErrorCauseProvider{
		found: true,
		hint:  datadog.CauseHint{ErrorType: "MongooseError", ErrorMessage: "Operation timed out"},
	}
	a.SetErrorCauseEnrichment(&fakeEnrichmentSettingsReader{enabled: true}, provider)

	a.HandleTransition(context.Background(), metricSLOService(), "operational", "degraded", datadog.SLOStatus{}, "test-interval")

	waitOrTimeout(t, services.done) // synchronous clear
	waitOrTimeout(t, services.done) // async write, after Generate* ran

	in := llmSvc.snapshotDegradedInput()
	if in.CauseType != "MongooseError" || in.CauseMessage != "Operation timed out" {
		t.Errorf("GenerateDegradedAnalysis received CauseType=%q CauseMessage=%q, want %q/%q", in.CauseType, in.CauseMessage, "MongooseError", "Operation timed out")
	}
}

// TestSLOAnalyzer_OutageEnrichment_CausePopulated_PassesCauseIntoAnalysisInput
// mirrors the degraded case above for the outage dispatch path.
func TestSLOAnalyzer_OutageEnrichment_CausePopulated_PassesCauseIntoAnalysisInput(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}, setDescriptionDone: make(chan struct{}, 1)}
	services := &fakeStatusAnalysisWriter{}
	llmSvc := &fakeLLMGenerator{outageResult: "A real outage description."}
	a := newTestAnalyzer(incidents, services, llmSvc, time.Second)
	provider := &fakeErrorCauseProvider{
		found: true,
		hint:  datadog.CauseHint{ErrorType: "MongooseError", ErrorMessage: "Operation timed out"},
	}
	a.SetErrorCauseEnrichment(&fakeEnrichmentSettingsReader{enabled: true}, provider)

	a.HandleTransition(context.Background(), metricSLOService(), "operational", "outage", datadog.SLOStatus{}, "test-interval")

	waitOrTimeout(t, incidents.setDescriptionDone)

	in := llmSvc.snapshotOutageInput()
	if in.CauseType != "MongooseError" || in.CauseMessage != "Operation timed out" {
		t.Errorf("GenerateOutageDescription received CauseType=%q CauseMessage=%q, want %q/%q", in.CauseType, in.CauseMessage, "MongooseError", "Operation timed out")
	}
}

// TestSLOAnalyzer_DegradedEnrichment_CauseEnrichmentEnabled_StillRespectsCooldownAndConcurrency
// is the T11 regression check: wiring resolveCauseHint into the dispatch
// goroutines must not bypass tryAcquire's existing cooldown gate
// (enrichmentCooldown) - a second dispatch for the same key, immediately
// following the first, must still be refused.
func TestSLOAnalyzer_DegradedEnrichment_CauseEnrichmentEnabled_StillRespectsCooldownAndConcurrency(t *testing.T) {
	incidents := &fakeIncidentStore{openIncidents: map[string]string{}}
	services := &fakeStatusAnalysisWriter{done: make(chan struct{}, 4)}
	llmSvc := &fakeLLMGenerator{degradedResult: "tooltip text"}
	a := newTestAnalyzer(incidents, services, llmSvc, time.Second)
	provider := &fakeErrorCauseProvider{found: true, hint: datadog.CauseHint{ErrorType: "X", ErrorMessage: "Y"}}
	a.SetErrorCauseEnrichment(&fakeEnrichmentSettingsReader{enabled: true}, provider)

	svc := metricSLOService()
	a.HandleTransition(context.Background(), svc, "operational", "degraded", datadog.SLOStatus{}, "interval-A")
	waitOrTimeout(t, services.done) // synchronous clear
	waitOrTimeout(t, services.done) // async write

	// Same dedupe key ("degraded:"+svc.ID) dispatched again immediately -
	// enrichmentCooldown (2 min) must refuse this second one, exactly as it
	// did before root-cause enrichment existed.
	a.HandleTransition(context.Background(), svc, "operational", "degraded", datadog.SLOStatus{}, "interval-B")

	calls := services.snapshot()
	if len(calls) != 3 {
		t.Errorf("UpdateStatusAnalysis called %d times, want 3 (2 from the first dispatch + 1 sync clear from the second; the second's async write must be refused by cooldown)", len(calls))
	}
	if provider.calls != 1 {
		t.Errorf("errorCauseProvider called %d times, want 1 (cooldown must prevent a second Error Tracking query, same as it prevents a second LLM call)", provider.calls)
	}
}
