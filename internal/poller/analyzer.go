package poller

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/connectors/datadog"
	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/llm"
)

// incidentStore is the subset of *db.IncidentRepository SLOAnalyzer depends
// on: creating an auto-detected outage incident (AI-12), checking whether a
// service already has one open, and writing an LLM-generated description/
// closing-comment proposal once the async enrichment goroutine completes.
type incidentStore interface {
	Create(ctx context.Context, incident *db.Incident, serviceIDs []string) error
	HasOpenIncidentForService(ctx context.Context, serviceID string) (incidentID string, found bool, err error)
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

// analysisTimeout is the recommended bound for every async LLM enrichment
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
const analysisTimeout = 30 * time.Second

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
}

// NewSLOAnalyzer builds an SLOAnalyzer. timeout bounds every async LLM
// enrichment call HandleTransition dispatches (see analysisTimeout for the
// recommended value and its rationale).
func NewSLOAnalyzer(incidents incidentStore, services statusAnalysisWriter, llmSvc llmGenerator, timeout time.Duration, logger *zap.Logger) *SLOAnalyzer {
	return &SLOAnalyzer{incidents: incidents, services: services, llmSvc: llmSvc, timeout: timeout, logger: logger}
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

	a.dispatchOutageEnrichment(ctx, svc, sloStatus, incident.ID)
}

// handleRecoveryTransition dispatches a closing-comment proposal for svc's
// recovery to "operational" (AI-19) when an incident is still open for
// this service - there is nothing to write synchronously here, since the
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

	a.dispatchClosingCommentEnrichment(ctx, svc, sloStatus, incidentID)
}

// dispatchDegradedEnrichment kicks off the detached, timeout-bounded
// goroutine that generates the degraded-state tooltip text and stores it
// via UpdateStatusAnalysis (AI-14). On any failure or timeout it logs and
// leaves status_analysis NULL - the fallback state HandleTransition's
// synchronous clear already left it in (AI-16), never an error string.
func (a *SLOAnalyzer) dispatchDegradedEnrichment(ctx context.Context, svc db.Service, sloStatus datadog.SLOStatus) {
	in := buildAnalysisInput(svc, sloStatus)

	go func() {
		dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.timeout)
		defer cancel()

		analysis, err := a.llmSvc.GenerateDegradedAnalysis(dctx, in)
		if err != nil {
			a.logger.Error("slo-analyzer: failed to generate degraded analysis",
				zap.String("service_id", svc.ID), zap.Error(err))
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
// never overwritten with an error string (AI-13 fallback behavior).
func (a *SLOAnalyzer) dispatchOutageEnrichment(ctx context.Context, svc db.Service, sloStatus datadog.SLOStatus, incidentID string) {
	in := buildAnalysisInput(svc, sloStatus)

	go func() {
		dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.timeout)
		defer cancel()

		description, err := a.llmSvc.GenerateOutageDescription(dctx, in)
		if err != nil {
			a.logger.Error("slo-analyzer: failed to generate outage description",
				zap.String("incident_id", incidentID), zap.Error(err))
			return
		}

		if err := a.incidents.SetDescription(dctx, incidentID, description); err != nil {
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
// (AI-23).
func (a *SLOAnalyzer) dispatchClosingCommentEnrichment(ctx context.Context, svc db.Service, sloStatus datadog.SLOStatus, incidentID string) {
	in := buildAnalysisInput(svc, sloStatus)

	go func() {
		dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.timeout)
		defer cancel()

		comment, err := a.llmSvc.GenerateClosingComment(dctx, in)
		if err != nil {
			a.logger.Error("slo-analyzer: failed to generate closing comment",
				zap.String("incident_id", incidentID), zap.Error(err))
			return
		}

		if err := a.incidents.SetPendingCloseComment(dctx, incidentID, comment); err != nil {
			a.logger.Error("slo-analyzer: failed to persist generated closing comment",
				zap.String("incident_id", incidentID), zap.Error(err))
		}
	}()
}
