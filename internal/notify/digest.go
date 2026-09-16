package notify

import (
	"time"

	"github.com/zeeplabs/zeep-vane/internal/email"
)

// digestDateLayout is the period format the weekly digest template renders.
const digestDateLayout = "2006-01-02"

// BuildWeeklyDigestData assembles the weekly-digest email data from
// already-computed aggregates. Uptime is the mean of the tenant's per-service
// uptime percentages - each computed by internal/history's UptimePercent, the
// same metric the public status page reads, so no new metric is invented.
// Services with no data are omitted by the caller. With no values, uptime is
// 100: an empty window carries no evidence of downtime.
func BuildWeeklyDigestData(tenantName string, serviceUptimes []float64, incidentsOpened, incidentsResolved int, periodStart, periodEnd time.Time) email.WeeklyDigestEmailData {
	uptime := 100.0
	if len(serviceUptimes) > 0 {
		var sum float64
		for _, u := range serviceUptimes {
			sum += u
		}
		uptime = sum / float64(len(serviceUptimes))
	}

	return email.WeeklyDigestEmailData{
		TenantName:        tenantName,
		UptimePercent:     uptime,
		IncidentsOpened:   incidentsOpened,
		IncidentsResolved: incidentsResolved,
		PeriodStart:       periodStart.Format(digestDateLayout),
		PeriodEnd:         periodEnd.Format(digestDateLayout),
	}
}
