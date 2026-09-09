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
// closing-comment proposal once T14's async enrichment completes.
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

// genericOutageDescription is the fallback incident description written
// synchronously the moment an outage is detected (AI-12), before any LLM
// call has had a chance to run - T14's async enrichment overwrites it via
// SetDescription on success, but visitors must never see an empty
// description while that call is in flight or if it fails.
func genericOutageDescription(serviceName string) string {
	return fmt.Sprintf("An outage was automatically detected for %s.", serviceName)
}

// SLOAnalyzer bridges poller state transitions to internal/llm: it is
// called synchronously from Poller.pollService at the exact point a
// transition is already known, and handles both the synchronous DB writes
// that must never wait on an LLM call (incident creation with a generic
// fallback description, status_analysis clearing) and - once T14 adds it -
// the detached, timeout-bounded LLM enrichment that refines that text
// afterwards.
type SLOAnalyzer struct {
	incidents incidentStore
	services  statusAnalysisWriter
	llmSvc    llmGenerator
	timeout   time.Duration
	logger    *zap.Logger
}

// NewSLOAnalyzer builds an SLOAnalyzer. timeout bounds every async LLM
// enrichment call T14 dispatches (analysisTimeout).
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
// Fully synchronous today (T13): outage-incident creation, status_analysis
// clearing. T14 adds a detached, timeout-bounded goroutine after this
// method's synchronous work to refine the generic fallback text with an
// LLM-generated one.
func (a *SLOAnalyzer) HandleTransition(ctx context.Context, svc db.Service, previousStatus, newStatus string, sloStatus datadog.SLOStatus) {
	if previousStatus == newStatus {
		return
	}

	// Entering or leaving "degraded" both invalidate any previously shown
	// tooltip text immediately (AI-15/AI-17) - cleared synchronously so a
	// visitor never sees stale analysis for the new state while an async
	// enrichment call (only dispatched on entering, see T14) is still in
	// flight.
	if previousStatus == "degraded" || newStatus == "degraded" {
		if err := a.services.UpdateStatusAnalysis(ctx, svc.ID, nil); err != nil {
			a.logger.Error("slo-analyzer: failed to clear status analysis",
				zap.String("service_id", svc.ID), zap.Error(err))
		}
	}

	if newStatus == "outage" {
		a.handleOutageTransition(ctx, svc)
	}

	if newStatus == "operational" {
		a.handleRecoveryTransition(ctx, svc)
	}
}

// handleOutageTransition creates an auto-detected incident for svc with a
// generic fallback description (AI-12), unless one is already open for
// this service - a single outage must not spawn a duplicate incident every
// cycle it stays breached.
func (a *SLOAnalyzer) handleOutageTransition(ctx context.Context, svc db.Service) {
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

	// T14 dispatches the async GenerateOutageDescription enrichment here,
	// overwriting the generic description above via SetDescription on
	// success.
}

// handleRecoveryTransition identifies whether svc's recovery to
// "operational" needs a closing-comment proposal (AI-19): true exactly
// when an incident is still open for this service. There is nothing to
// write synchronously yet - an LLM call is required to draft the proposed
// text, so T13 only identifies the case; T14 adds the async dispatch that
// actually drafts and stores it via SetPendingCloseComment.
func (a *SLOAnalyzer) handleRecoveryTransition(ctx context.Context, svc db.Service) {
	_, found, err := a.incidents.HasOpenIncidentForService(ctx, svc.ID)
	if err != nil {
		a.logger.Error("slo-analyzer: failed to check open incident for recovered service",
			zap.String("service_id", svc.ID), zap.Error(err))
		return
	}
	if !found {
		return
	}

	// T14 dispatches the async GenerateClosingComment enrichment here,
	// storing its result via SetPendingCloseComment(incidentID, ...).
}
