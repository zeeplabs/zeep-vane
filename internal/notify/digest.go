package notify

import (
	"sort"
	"strings"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/db"
	"github.com/zeeplabs/zeep-vane/internal/email"
)

// DigestServiceUptime is one service's uptime over the digest window, as
// computed by internal/history's UptimePercent (the same metric the public
// status page reads). Services with no data in the window are omitted by the
// caller rather than passed with a made-up value.
type DigestServiceUptime struct {
	Name          string
	UptimePercent float64
}

// WeeklyDigestInput is everything BuildWeeklyDigestData needs, all already
// fetched by the caller (DigestScheduler) inside the tenant's read
// transaction.
type WeeklyDigestInput struct {
	Tenant            db.Tenant
	Services          []DigestServiceUptime
	Incidents         []db.DigestIncident
	IncidentsOpened   int
	IncidentsResolved int
	PeriodStart       time.Time
	PeriodEnd         time.Time
}

// BuildWeeklyDigestData assembles the weekly-digest email data from
// already-computed inputs. Uptime is the mean of the tenant's per-service
// uptime percentages; with no services it is 100, since an empty window
// carries no evidence of downtime. The services list is capped at
// email.DigestServicesCap - when capped, the lowest-uptime services are the
// ones kept (the ones a reader most needs to see), then displayed in name
// order. MTTR is the mean duration of incidents resolved inside the window.
func BuildWeeklyDigestData(in WeeklyDigestInput) email.WeeklyDigestEmailData {
	uptime := 100.0
	if len(in.Services) > 0 {
		var sum float64
		for _, s := range in.Services {
			sum += s.UptimePercent
		}
		uptime = sum / float64(len(in.Services))
	}

	rows := make([]email.DigestServiceRow, 0, len(in.Services))
	for _, s := range in.Services {
		rows = append(rows, email.DigestServiceRow{Name: s.Name, UptimePercent: s.UptimePercent})
	}
	if len(rows) > email.DigestServicesCap {
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].UptimePercent < rows[j].UptimePercent })
		rows = rows[:email.DigestServicesCap]
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })

	incidents := make([]email.DigestIncidentRow, 0, len(in.Incidents))
	var resolvedTotal time.Duration
	var resolvedCount int
	for _, inc := range in.Incidents {
		end := in.PeriodEnd
		if inc.ResolvedAt != nil {
			end = *inc.ResolvedAt
		}
		duration := end.Sub(inc.CreatedAt)
		if duration < 0 {
			duration = 0
		}
		incidents = append(incidents, email.DigestIncidentRow{
			Title:       inc.Title,
			ServiceName: strings.Join(inc.ServiceNames, ", "),
			Status:      inc.Status,
			Duration:    duration,
		})
		if inc.ResolvedAt != nil && !inc.ResolvedAt.Before(in.PeriodStart) && inc.ResolvedAt.Before(in.PeriodEnd) {
			resolvedTotal += duration
			resolvedCount++
		}
	}

	var mttr time.Duration
	if resolvedCount > 0 {
		mttr = resolvedTotal / time.Duration(resolvedCount)
	}

	return email.WeeklyDigestEmailData{
		TenantName:        in.Tenant.Name,
		UptimePercent:     uptime,
		IncidentsOpened:   in.IncidentsOpened,
		IncidentsResolved: in.IncidentsResolved,
		PeriodStart:       in.PeriodStart,
		PeriodEnd:         in.PeriodEnd,
		Location:          email.TenantLocation(in.Tenant.Timezone),
		Services:          rows,
		TotalServices:     len(in.Services),
		Incidents:         incidents,
		MTTR:              mttr,
	}
}
