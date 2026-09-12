package poller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/connectors/datadog"
	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/llm"
	"github.com/zeeplabs/zeep-vane/internal/notify"
)

// incidentStore is the subset of *db.IncidentRepository SLOAnalyzer depends
// on: creating an auto-detected outage incident (AI-12), checking whether a
// service already has one open, and writing an LLM-generated description/
// closing-comment proposal once the async enrichment goroutine completes.
type incidentStore interface {
	Create(ctx context.Context, incident *db.Incident, serviceIDs []string) error
	HasOpenIncidentForService(ctx context.Context, serviceID string) (incidentID string, found bool, err error)
	AllLinkedServicesOperational(ctx context.Context, incidentID string) (bool, error)
	SetDescription(ctx context.Context, incidentID, description string) error
	SetPendingCloseComment(ctx context.Context, incidentID, comment string) error
}

// statusAnalysisWriter is the subset of *db.ServiceRepository SLOAnalyzer
// depends on to clear/set a service's status_analysis column (AI-14,
// AI-15, AI-17).
type statusAnalysisWriter interface {
	UpdateStatusAnalysis(ctx context.Context, serviceID string, analysis *string) error
}

// llmGenerator is the subset of *llm.Service SLOAnalyzer depends on to
// generate SLO analysis text. Narrowed to the three Generate* methods, same
// convention as every other narrow interface in this package.
type llmGenerator interface {
	GenerateDegradedAnalysis(ctx context.Context, in llm.AnalysisInput) (string, error)
	GenerateOutageDescription(ctx context.Context, in llm.AnalysisInput) (string, error)
	GenerateClosingComment(ctx context.Context, in llm.AnalysisInput) (string, error)
}

// AnalysisTimeout is the recommended bound for every async LLM enrichment
// call HandleTransition dispatches - the value callers wiring SLOAnalyzer
// into production (via NewSLOAnalyzer's timeout parameter) should pass.
// internal/connectors/openai.Client's own HTTP timeout is already 20s
// (resend's 10s doubled for LLM completion latency); this wraps that call
// plus the repository write that follows it, so it needs headroom above
// 20s rather than matching it exactly - otherwise the outer context could
// cut the request off at the same instant the connector's own timeout
// would have classified it as ErrTimeout anyway, turning a clean
// classified error into a race. 30s is deliberately generous: unlike a
// user-facing request, this goroutine's caller (pollService) has already
// returned by the time it fires (see AD-014's password-reset precedent for
// the same reasoning), so a slower-than-usual completion costs nothing but
// delaying one incident's enrichment - never the poll cycle itself
// (verified by T15's timing-bounded test, which proves a *hung* call,
// unbounded by any timeout, still can't delay pollOnce's other services).
const AnalysisTimeout = 30 * time.Second

// maxConcurrentEnrichments bounds how many async LLM enrichment goroutines
// (degraded tooltip, outage description, closing comment - across every
// service) may be in flight at once. Without this, a poll cycle in which
// many services transition simultaneously (or a single flapping service
// keeps re-triggering) would fan out an unbounded number of concurrent,
// paid API calls with no ceiling and no operator-visible alarm.
const maxConcurrentEnrichments = 4

// enrichmentCooldown is the minimum time between two enrichment dispatches
// for the same dedupe key (see dispatch* callers below) - protects against
// a service flapping in and out of the same state repeatedly generating a
// fresh (paid) LLM call every single cycle.
const enrichmentCooldown = 2 * time.Minute

// genericOutageDescription is the fallback incident description written
// synchronously the moment an outage is detected (AI-12), before any LLM
// call has had a chance to run - the async enrichment goroutine overwrites
// it via SetDescription on success, but visitors must never see an empty
// description while that call is in flight or if it fails.
func genericOutageDescription(serviceName string) string {
	return fmt.Sprintf("An outage was automatically detected for %s.", serviceName)
}

// buildAnalysisInput builds an llm.AnalysisInput directly from svc and
// sloStatus - the same datadog.SLOStatus pollService already fetched this
// cycle, so no second Datadog call is ever made to gather analysis
// context.
func buildAnalysisInput(svc db.Service, sloStatus datadog.SLOStatus) llm.AnalysisInput {
	return llm.AnalysisInput{
		ServiceName:          svc.Name,
		SLOState:             sloStatus.State,
		SLI:                  sloStatus.SLI,
		Target:               sloStatus.Target,
		Timeframe:            sloStatus.Timeframe,
		ErrorBudgetRemaining: sloStatus.ErrorBudgetRemaining,
	}
}

// SLOAnalyzer bridges poller state transitions to internal/llm: it is
// called synchronously from Poller.pollService at the exact point a
// transition is already known, and handles both the synchronous DB writes
// that must never wait on an LLM call (incident creation with a generic
// fallback description, status_analysis clearing) and the detached,
// timeout-bounded LLM enrichment that refines that text afterwards.
type SLOAnalyzer struct {
	incidents incidentStore
	services  statusAnalysisWriter
	llmSvc    llmGenerator
	timeout   time.Duration
	logger    *zap.Logger

	// notifier sends the incident-opened email for an auto-created outage
	// incident (notification-preferences NOTIFPREF-04). Optional: nil (the
	// default, and every test that does not exercise notifications) disables
	// those emails. Set via SetNotifier from boot wiring.
	notifier incidentNotifier

	sem chan struct{} // bounds concurrent enrichment goroutines (maxConcurrentEnrichments)

	mu           sync.Mutex
	lastDispatch map[string]time.Time // dedupe key -> last dispatch time (enrichmentCooldown)
}

// incidentNotifier is the subset of *notify.Service SLOAnalyzer depends on.
type incidentNotifier interface {
	NotifyIncidentOpened(ctx context.Context, tenantID string, summary notify.IncidentSummary) error
}

// SetNotifier installs the notifier used for auto-created incident-opened
// emails. Optional - a nil notifier (the default) disables those emails.
func (a *SLOAnalyzer) SetNotifier(n incidentNotifier) {
	a.notifier = n
}

// NewSLOAnalyzer builds an SLOAnalyzer. timeout bounds every async LLM
// enrichment call HandleTransition dispatches (see AnalysisTimeout for the
// recommended value and its rationale).
func NewSLOAnalyzer(incidents incidentStore, services statusAnalysisWriter, llmSvc llmGenerator, timeout time.Duration, logger *zap.Logger) *SLOAnalyzer {
	return &SLOAnalyzer{
		incidents:    incidents,
		services:     services,
		llmSvc:       llmSvc,
		timeout:      timeout,
		logger:       logger,
		sem:          make(chan struct{}, maxConcurrentEnrichments),
		lastDispatch: map[string]time.Time{},
	}
}

// tryAcquire reports whether an enrichment dispatch for key is allowed
// right now: false if key was dispatched within enrichmentCooldown, or if
// maxConcurrentEnrichments goroutines are already in flight across every
// service. Both checks are non-blocking - the poll cycle calling into
// HandleTransition must never wait on either gate; a dispatch that's
// refused simply leaves the existing fallback text in place, exactly like
// any other enrichment failure.
func (a *SLOAnalyzer) tryAcquire(key string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	if last, ok := a.lastDispatch[key]; ok && time.Since(last) < enrichmentCooldown {
		return false
	}
	select {
	case a.sem <- struct{}{}:
	default:
		return false
	}
	a.lastDispatch[key] = time.Now()
	return true
}

// release frees one slot acquired by tryAcquire - deferred by every
// dispatched enrichment goroutine.
func (a *SLOAnalyzer) release() {
	<-a.sem
}

// HandleTransition reacts to svc's status changing from previousStatus to
// newStatus this poll cycle, using sloStatus (the same datadog.SLOStatus
// pollService already fetched this cycle - no second Datadog call). It is
// a no-op when previousStatus == newStatus. It never returns an error to
// the caller - a failure here must never fail pollService's per-service
// loop (AI-08): every failure is logged and swallowed.
//
// Synchronous work (outage-incident creation, status_analysis clearing)
// happens before this method returns. Any LLM enrichment it needs is
// dispatched as a detached goroutine, bounded by a.timeout via
// context.WithTimeout(context.WithoutCancel(ctx), a.timeout) - the same
// shape AD-014 established for the password-reset email send - so this
// method always returns well before the goroutine's result is known.
func (a *SLOAnalyzer) HandleTransition(ctx context.Context, svc db.Service, previousStatus, newStatus string, sloStatus datadog.SLOStatus) {
	if previousStatus == newStatus {
		return
	}

	enteringDegraded := newStatus == "degraded"
	leavingDegraded := previousStatus == "degraded" && newStatus != "degraded"

	// Entering or leaving "degraded" both invalidate any previously shown
	// tooltip text immediately (AI-15/AI-17) - cleared synchronously so a
	// visitor never sees stale analysis for the new state while an async
	// enrichment call (only dispatched on entering, below) is still in
	// flight.
	if enteringDegraded || leavingDegraded {
		if err := a.services.UpdateStatusAnalysis(ctx, svc.ID, nil); err != nil {
			a.logger.Error("slo-analyzer: failed to clear status analysis",
				zap.String("service_id", svc.ID), zap.Error(err))
		}
	}
	if enteringDegraded {
		a.dispatchDegradedEnrichment(ctx, svc, sloStatus)
	}

	if newStatus == "outage" {
		a.handleOutageTransition(ctx, svc, sloStatus)
	}

	if newStatus == "operational" {
		a.handleRecoveryTransition(ctx, svc, sloStatus)
	}
}

// handleOutageTransition creates an auto-detected incident for svc with a
// generic fallback description (AI-12), unless one is already open for
// this service - a single outage must not spawn a duplicate incident every
// cycle it stays breached. On successful creation it dispatches the async
// GenerateOutageDescription enrichment to refine that description.
func (a *SLOAnalyzer) handleOutageTransition(ctx context.Context, svc db.Service, sloStatus datadog.SLOStatus) {
	_, found, err := a.incidents.HasOpenIncidentForService(ctx, svc.ID)
	if err != nil {
		a.logger.Error("slo-analyzer: failed to check open incident for outage service",
			zap.String("service_id", svc.ID), zap.Error(err))
		return
	}
	if found {
		return
	}

	description := genericOutageDescription(svc.Name)
	incident := &db.Incident{
		Title:       fmt.Sprintf("Outage detected: %s", svc.Name),
		Description: &description,
		AutoCreated: true,
	}
	if err := a.incidents.Create(ctx, incident, []string{svc.ID}); err != nil {
		a.logger.Error("slo-analyzer: failed to create auto-detected outage incident",
			zap.String("service_id", svc.ID), zap.Error(err))
		return
	}

	// Auto-created incidents bypass IncidentsHandler.Create, so their
	// incident-opened notification has to fire here (spec edge case: an
	// auto-created incident still notifies). Best-effort, like the handler
	// hooks: a lookup or send failure is logged and never delays or fails the
	// poll cycle. tenantID comes from the poll cycle's per-tenant context.
	if tenantID := tenantIDFromContext(ctx); a.notifier != nil && tenantID != "" {
		if err := a.notifier.NotifyIncidentOpened(ctx, tenantID, notify.IncidentSummary{
			IncidentID:  incident.ID,
			Title:       incident.Title,
			Severity:    incident.Severity,
			ServiceName: svc.Name,
		}); err != nil {
			a.logger.Error("slo-analyzer: failed to notify incident opened",
				zap.String("incident_id", incident.ID), zap.Error(err))
		}
	}

	a.dispatchOutageEnrichment(ctx, svc, sloStatus, incident.ID)
}

// handleRecoveryTransition dispatches a closing-comment proposal for svc's
// recovery to "operational" (AI-19) when an incident is still open for
// this service and every other service linked to that incident (an
// incident can cover more than one service via incident_services' N:N
// shape) has also recovered - one recovered service must never propose
// closing an incident whose other linked services are still degraded/
// outage. There is nothing to write synchronously here, since the
// proposed text only exists once the LLM call in the dispatched goroutine
// completes.
func (a *SLOAnalyzer) handleRecoveryTransition(ctx context.Context, svc db.Service, sloStatus datadog.SLOStatus) {
	incidentID, found, err := a.incidents.HasOpenIncidentForService(ctx, svc.ID)
	if err != nil {
		a.logger.Error("slo-analyzer: failed to check open incident for recovered service",
			zap.String("service_id", svc.ID), zap.Error(err))
		return
	}
	if !found {
		return
	}

	allOperational, err := a.incidents.AllLinkedServicesOperational(ctx, incidentID)
	if err != nil {
		a.logger.Error("slo-analyzer: failed to check linked services status for incident",
			zap.String("incident_id", incidentID), zap.Error(err))
		return
	}
	if !allOperational {
		return
	}

	a.dispatchClosingCommentEnrichment(ctx, svc, sloStatus, incidentID)
}

// dispatchDegradedEnrichment kicks off the detached, timeout-bounded
// goroutine that generates the degraded-state tooltip text and stores it
// via UpdateStatusAnalysis (AI-14). On any failure or timeout it logs and
// leaves status_analysis NULL - the fallback state HandleTransition's
// synchronous clear already left it in (AI-16), never an error string.
// Gated by tryAcquire (maxConcurrentEnrichments/enrichmentCooldown) - a
// refused dispatch simply leaves that NULL fallback in place.
func (a *SLOAnalyzer) dispatchDegradedEnrichment(ctx context.Context, svc db.Service, sloStatus datadog.SLOStatus) {
	key := "degraded:" + svc.ID
	if !a.tryAcquire(key) {
		a.logger.Warn("slo-analyzer: skipping degraded enrichment (cooldown or concurrency limit)",
			zap.String("service_id", svc.ID))
		return
	}

	in := buildAnalysisInput(svc, sloStatus)

	go func() {
		defer a.release()
		defer a.recoverEnrichmentPanic("degraded", svc.ID)

		dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.timeout)
		defer cancel()

		analysis, err := a.llmSvc.GenerateDegradedAnalysis(dctx, in)
		if err != nil {
			a.logger.Error("slo-analyzer: failed to generate degraded analysis",
				zap.String("service_id", svc.ID), zap.Error(err))
			return
		}
		if strings.TrimSpace(analysis) == "" {
			a.logger.Error("slo-analyzer: degraded analysis result was empty, treating as failure",
				zap.String("service_id", svc.ID))
			return
		}

		if err := a.services.UpdateStatusAnalysis(dctx, svc.ID, &analysis); err != nil {
			a.logger.Error("slo-analyzer: failed to persist generated degraded analysis",
				zap.String("service_id", svc.ID), zap.Error(err))
		}
	}()
}

// dispatchOutageEnrichment kicks off the detached, timeout-bounded
// goroutine that generates a real outage description and stores it via
// SetDescription (AI-10), replacing the generic fallback text
// handleOutageTransition already persisted synchronously. On any failure
// or timeout it logs and leaves the generic description in place - it is
// never overwritten with an error string (AI-13 fallback behavior). Gated
// by tryAcquire, same as dispatchDegradedEnrichment.
func (a *SLOAnalyzer) dispatchOutageEnrichment(ctx context.Context, svc db.Service, sloStatus datadog.SLOStatus, incidentID string) {
	key := "outage:" + incidentID
	if !a.tryAcquire(key) {
		a.logger.Warn("slo-analyzer: skipping outage description enrichment (cooldown or concurrency limit)",
			zap.String("incident_id", incidentID))
		return
	}

	in := buildAnalysisInput(svc, sloStatus)

	go func() {
		defer a.release()
		defer a.recoverEnrichmentPanic("outage", incidentID)

		dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.timeout)
		defer cancel()

		description, err := a.llmSvc.GenerateOutageDescription(dctx, in)
		if err != nil {
			a.logger.Error("slo-analyzer: failed to generate outage description",
				zap.String("incident_id", incidentID), zap.Error(err))
			return
		}
		if strings.TrimSpace(description) == "" {
			a.logger.Error("slo-analyzer: outage description result was empty, treating as failure",
				zap.String("incident_id", incidentID))
			return
		}

		if err := a.incidents.SetDescription(dctx, incidentID, description); err != nil {
			if errors.Is(err, db.ErrIncidentAlreadyResolved) {
				a.logger.Info("slo-analyzer: incident resolved before outage enrichment completed, discarding description",
					zap.String("incident_id", incidentID))
				return
			}
			a.logger.Error("slo-analyzer: failed to persist generated outage description",
				zap.String("incident_id", incidentID), zap.Error(err))
		}
	}()
}

// dispatchClosingCommentEnrichment kicks off the detached, timeout-bounded
// goroutine that drafts a closing comment and stores it as incidentID's
// pending_close_comment via SetPendingCloseComment (AI-20), awaiting
// owner/operator confirmation. On any failure or timeout it logs and
// leaves pending_close_comment NULL, so the incident stays open with no
// proposal and the admin falls back to the existing manual close flow
// (AI-23). Gated by tryAcquire, same as dispatchDegradedEnrichment.
func (a *SLOAnalyzer) dispatchClosingCommentEnrichment(ctx context.Context, svc db.Service, sloStatus datadog.SLOStatus, incidentID string) {
	key := "close:" + incidentID
	if !a.tryAcquire(key) {
		a.logger.Warn("slo-analyzer: skipping closing comment enrichment (cooldown or concurrency limit)",
			zap.String("incident_id", incidentID))
		return
	}

	in := buildAnalysisInput(svc, sloStatus)

	go func() {
		defer a.release()
		defer a.recoverEnrichmentPanic("closing-comment", incidentID)

		dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.timeout)
		defer cancel()

		comment, err := a.llmSvc.GenerateClosingComment(dctx, in)
		if err != nil {
			a.logger.Error("slo-analyzer: failed to generate closing comment",
				zap.String("incident_id", incidentID), zap.Error(err))
			return
		}
		if strings.TrimSpace(comment) == "" {
			a.logger.Error("slo-analyzer: closing comment result was empty, treating as failure",
				zap.String("incident_id", incidentID))
			return
		}

		if err := a.incidents.SetPendingCloseComment(dctx, incidentID, comment); err != nil {
			if errors.Is(err, db.ErrIncidentAlreadyResolved) {
				a.logger.Info("slo-analyzer: incident resolved before closing-comment enrichment completed, discarding proposal",
					zap.String("incident_id", incidentID))
				return
			}
			a.logger.Error("slo-analyzer: failed to persist generated closing comment",
				zap.String("incident_id", incidentID), zap.Error(err))
		}
	}()
}

// recoverEnrichmentPanic recovers a panic inside any enrichment goroutine
// and logs it instead of letting it crash the process. None of these
// goroutines' work (an LLM completion, a repository write) should ever
// panic, but this package has no other supervision for detached
// goroutines, and a crash here would take down the public status page -
// the one thing this product exists to keep available - over a failure in
// a best-effort background enrichment. kind/key identify which dispatch
// call this recover belongs to, for correlating with the corresponding
// dispatch/generate log lines.
func (a *SLOAnalyzer) recoverEnrichmentPanic(kind, key string) {
	if r := recover(); r != nil {
		a.logger.Error("slo-analyzer: recovered panic in enrichment goroutine",
			zap.String("kind", kind), zap.String("key", key), zap.Any("panic", r))
	}
}
