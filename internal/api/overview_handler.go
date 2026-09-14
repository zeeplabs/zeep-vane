package api

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/history"
)

const (
	// overviewUptimeWindowDays is the summary card's uptime window (OVW-03:
	// "Uptime médio (30d)").
	overviewUptimeWindowDays = 30
	// overviewUptimeSeriesDays is the number of daily buckets in the
	// 14-day chart (OVW-07), oldest first.
	overviewUptimeSeriesDays = 14
	// overviewRecentIncidents is the maximum number of rows in the recent
	// incidents list (OVW-08).
	overviewRecentIncidents = 3
)

// overviewServiceLister is the subset of *db.ServiceRepository the overview
// handler depends on. Unlike the public status handler's serviceLister, the
// overview is tenant-wide, not scoped to one status page.
type overviewServiceLister interface {
	List(ctx context.Context) ([]db.Service, error)
}

// overviewIncidentReader is the subset of *db.IncidentRepository the
// overview handler depends on: the open count (OVW-04) and the
// most-recent-first list already implemented by ListPaginated (OVW-08).
type overviewIncidentReader interface {
	CountOpen(ctx context.Context) (int, error)
	ListPaginated(ctx context.Context, page, pageSize int) ([]db.Incident, int, error)
}

// overviewDomainCounter is the subset of *db.DomainRepository the overview
// handler depends on (OVW-06).
type overviewDomainCounter interface {
	CountVerified(ctx context.Context) (int, error)
}

// OverviewResponse is the GET /api/overview JSON contract (design.md). It is
// a flat summary DTO, not a Page[T] - the endpoint returns a fixed-shaped
// aggregate, not a paginated list. UptimeAvg30d is nil when no service has
// any data in the window (OVW-03, rendered as "—").
type OverviewResponse struct {
	UptimeAvg30d      *float64                                       `json:"uptime_avg_30d"`
	OpenIncidents     int                                            `json:"open_incidents"`
	UnhealthyServices int                                            `json:"unhealthy_services"`
	VerifiedDomains   int                                            `json:"verified_domains"`
	UptimeSeries      [overviewUptimeSeriesDays]OverviewUptimeBucket `json:"uptime_series"`
	RecentIncidents   []OverviewIncident                             `json:"recent_incidents"`
}

// OverviewUptimeBucket is one day of the 14-day chart. Date is the bucket's
// local (America/Sao_Paulo) calendar day, YYYY-MM-DD; UptimePercent is nil
// for a day with no service data (OVW-07).
type OverviewUptimeBucket struct {
	Date          string   `json:"date"`
	UptimePercent *float64 `json:"uptime_percent"`
}

// OverviewIncident is one row of the recent-incidents list (OVW-08).
type OverviewIncident struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// OverviewHandler serves GET /api/overview - a single read-only aggregation
// of the tenant's current monitoring state (OVW-01/02), composed entirely
// from repositories that already own the underlying data. No new tables, no
// writes, no Datadog calls: the uptime math reuses internal/history, the
// same code the public status page runs.
type OverviewHandler struct {
	services   overviewServiceLister
	intervals  statusIntervalReader
	incidents  overviewIncidentReader
	domains    overviewDomainCounter
	logger     *zap.Logger
	historyLoc *time.Location
}

// NewOverviewHandler builds an OverviewHandler backed by the four
// repositories. It loads America/Sao_Paulo once here (same tzdata
// assumption as NewPublicStatusHandler); a load failure is a build defect,
// so it panics at construction rather than turning every request into a 500.
func NewOverviewHandler(services overviewServiceLister, intervals statusIntervalReader, incidents overviewIncidentReader, domains overviewDomainCounter, logger *zap.Logger) *OverviewHandler {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		panic(fmt.Sprintf("overview: failed to load America/Sao_Paulo location: %v", err))
	}
	return &OverviewHandler{services: services, intervals: intervals, incidents: incidents, domains: domains, logger: logger, historyLoc: loc}
}

// Get handles GET /api/overview (role: owner, operator, viewer). ACs:
// OVW-03..OVW-09. Every repository error is logged server-side and returned
// as the fixed generic 500 (AGENTS.md §4 - never leak err.Error()).
func (h *OverviewHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	services, err := h.services.List(ctx)
	if err != nil {
		h.logger.Error("overview: failed to list services", zap.Error(err))
		writeInternalError(w)
		return
	}

	serviceIDs := make([]string, 0, len(services))
	for _, service := range services {
		serviceIDs = append(serviceIDs, service.ID)
	}

	now := time.Now()
	windowStart := now.AddDate(0, 0, -overviewUptimeWindowDays)
	overlapping, err := h.intervals.ListOverlapping(ctx, serviceIDs, windowStart, now)
	if err != nil {
		h.logger.Error("overview: failed to list overlapping status intervals", zap.Error(err))
		writeInternalError(w)
		return
	}
	intervalsByService := map[string][]db.StatusInterval{}
	for _, interval := range overlapping {
		intervalsByService[interval.ServiceID] = append(intervalsByService[interval.ServiceID], interval)
	}

	openIncidents, err := h.incidents.CountOpen(ctx)
	if err != nil {
		h.logger.Error("overview: failed to count open incidents", zap.Error(err))
		writeInternalError(w)
		return
	}

	recent, _, err := h.incidents.ListPaginated(ctx, 1, overviewRecentIncidents)
	if err != nil {
		h.logger.Error("overview: failed to list recent incidents", zap.Error(err))
		writeInternalError(w)
		return
	}

	verifiedDomains, err := h.domains.CountVerified(ctx)
	if err != nil {
		h.logger.Error("overview: failed to count verified domains", zap.Error(err))
		writeInternalError(w)
		return
	}

	// OVW-05 counts every service whose current status is not
	// "operational" (degraded or outage). A not-yet-polled service is
	// "not_configured", which also satisfies "!= operational" - the AC's
	// literal rule.
	unhealthy := 0
	for _, service := range services {
		if service.CurrentStatus != "operational" {
			unhealthy++
		}
	}

	resp := OverviewResponse{
		UptimeAvg30d:      averageUptime(intervalsByService, serviceIDs, windowStart, now),
		OpenIncidents:     openIncidents,
		UnhealthyServices: unhealthy,
		VerifiedDomains:   verifiedDomains,
		UptimeSeries:      h.buildUptimeSeries(intervalsByService, serviceIDs, now),
		RecentIncidents:   toOverviewIncidents(recent),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// averageUptime returns the mean of history.UptimePercent over every
// service with ok=true, floored to one decimal, or nil when no service has
// data (OVW-03, rendered as "—"). Services with no interval in the window
// are excluded, never counted as 0%.
func averageUptime(intervalsByService map[string][]db.StatusInterval, serviceIDs []string, windowStart, asOf time.Time) *float64 {
	var sum float64
	var n int
	for _, id := range serviceIDs {
		if pct, ok := history.UptimePercent(intervalsByService[id], windowStart, asOf); ok {
			sum += pct
			n++
		}
	}
	if n == 0 {
		return nil
	}
	avg := math.Floor((sum/float64(n))*10) / 10
	return &avg
}

// buildUptimeSeries returns exactly overviewUptimeSeriesDays daily buckets,
// oldest first, each day aligned to local midnight (OVW-07). Each bucket is
// computed independently with the same per-service average as the 30d card,
// scoped to that one day; the last (current) day is a partial window ending
// at now.
func (h *OverviewHandler) buildUptimeSeries(intervalsByService map[string][]db.StatusInterval, serviceIDs []string, now time.Time) [overviewUptimeSeriesDays]OverviewUptimeBucket {
	var series [overviewUptimeSeriesDays]OverviewUptimeBucket

	nowLocal := now.In(h.historyLoc)
	todayStart := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, h.historyLoc)
	firstDayStart := todayStart.AddDate(0, 0, -(overviewUptimeSeriesDays - 1))

	for i := 0; i < overviewUptimeSeriesDays; i++ {
		dayStart := firstDayStart.AddDate(0, 0, i)
		dayEnd := dayStart.AddDate(0, 0, 1)
		if i == overviewUptimeSeriesDays-1 {
			dayEnd = now
		}

		// Scope each service's intervals to the ones actually overlapping
		// this day before computing its uptime. Passing the whole 30d set
		// would let UptimePercent's earliest-interval denominator rule
		// credit a day with no data as 100% whenever the service has any
		// interval before it in the window.
		dayIntervals := make(map[string][]db.StatusInterval, len(serviceIDs))
		for _, id := range serviceIDs {
			dayIntervals[id] = intervalsOverlapping(intervalsByService[id], dayStart, dayEnd)
		}

		series[i] = OverviewUptimeBucket{
			Date:          dayStart.Format("2006-01-02"),
			UptimePercent: averageUptime(dayIntervals, serviceIDs, dayStart, dayEnd),
		}
	}

	return series
}

// intervalsOverlapping filters intervals to those overlapping [from, to). An
// open interval (EndsAt nil) overlaps as long as it started before to.
func intervalsOverlapping(intervals []db.StatusInterval, from, to time.Time) []db.StatusInterval {
	var out []db.StatusInterval
	for _, interval := range intervals {
		if !interval.StartsAt.Before(to) {
			continue
		}
		if interval.EndsAt != nil && !interval.EndsAt.After(from) {
			continue
		}
		out = append(out, interval)
	}
	return out
}

// toOverviewIncidents maps the repository's most-recent-first page into the
// response DTO. Always returns a non-nil slice so the JSON is [] rather than
// null for a tenant with no incidents (OVW-09).
func toOverviewIncidents(incidents []db.Incident) []OverviewIncident {
	out := make([]OverviewIncident, len(incidents))
	for i, incident := range incidents {
		out[i] = OverviewIncident{
			ID:        incident.ID,
			Title:     incident.Title,
			Status:    incident.Status,
			CreatedAt: incident.CreatedAt,
		}
	}
	return out
}
