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
//
// Incidents are described as of the period end, not as of the send: one
// resolved after PeriodEnd (between the end of the week and the Monday
// send) was still open for the whole week being summarized, so it is shown
// ongoing with its duration up to PeriodEnd - the same reading MTTR already
// takes, which excludes it. The incidents list is capped at
// email.DigestIncidentsCap, keeping first the incidents still open at
// period end, then the longest; the kept rows stay in the input order
// (ListActiveBetween's newest first), and TotalIncidents carries the
// uncapped count.
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
		resolvedInPeriod := inc.ResolvedAt != nil && inc.ResolvedAt.Before(in.PeriodEnd)
		end := in.PeriodEnd
		if resolvedInPeriod {
			end = *inc.ResolvedAt
		}
		duration := end.Sub(inc.CreatedAt)
		if duration < 0 {
			duration = 0
		}
		incidents = append(incidents, email.DigestIncidentRow{
			Title:              inc.Title,
			ServiceName:        strings.Join(inc.ServiceNames, ", "),
			Status:             inc.Status,
			OngoingAtPeriodEnd: inc.ResolvedAt != nil && !resolvedInPeriod,
			Duration:           duration,
		})
		if resolvedInPeriod && !inc.ResolvedAt.Before(in.PeriodStart) {
			resolvedTotal += duration
			resolvedCount++
		}
	}
	shown := capDigestIncidents(incidents, in.Incidents, in.PeriodEnd)

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
		Incidents:         shown,
		TotalIncidents:    len(incidents),
		MTTR:              mttr,
	}
}

// capDigestIncidents returns at most email.DigestIncidentsCap of rows
// (rows[i] built from source[i]), choosing incidents still open at
// periodEnd first, then the longest, ties broken by input order; the chosen
// rows keep their input order.
func capDigestIncidents(rows []email.DigestIncidentRow, source []db.DigestIncident, periodEnd time.Time) []email.DigestIncidentRow {
	if len(rows) <= email.DigestIncidentsCap {
		return rows
	}
	openAtEnd := func(i int) bool {
		return source[i].ResolvedAt == nil || !source[i].ResolvedAt.Before(periodEnd)
	}
	order := make([]int, len(rows))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		ia, ib := order[a], order[b]
		if oa, ob := openAtEnd(ia), openAtEnd(ib); oa != ob {
			return oa
		}
		return rows[ia].Duration > rows[ib].Duration
	})
	keep := order[:email.DigestIncidentsCap]
	sort.Ints(keep)
	capped := make([]email.DigestIncidentRow, 0, len(keep))
	for _, i := range keep {
		capped = append(capped, rows[i])
	}
	return capped
}
