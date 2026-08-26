package history

import (
	"math"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// UptimePercent computes the uptime percentage over [windowStart, asOf] from
// intervals - the same overlapping-interval set BuildHourly consumes. asOf
// should be the last time the poller actually confirmed status - not
// necessarily wall-clock now - so a stalled poller's open interval doesn't
// get credited as uptime for however long it's been silently unconfirmed
// (H7). A healthy poller has asOf effectively equal to now and nothing
// changes.
//
// The denominator starts at the later of windowStart or the earliest
// StartsAt among intervals (a service newer than the window uses
// asOf-earliestStartsAt as its denominator instead of the full window,
// SHU-12/13). Downtime is the summed overlap of every outage-status
// interval with [denominatorStart, asOf]; every other status counts as
// uptime. The result is clamped to [0, 100] before being floored to one
// decimal place (never rounded up).
//
// ok is false - "undefined, render a dash" - when intervals is empty or
// the clipped denominator is zero or negative.
func UptimePercent(intervals []db.StatusInterval, windowStart, asOf time.Time) (percent float64, ok bool) {
	if len(intervals) == 0 {
		return 0, false
	}

	earliestStartsAt := intervals[0].StartsAt
	for _, interval := range intervals[1:] {
		if interval.StartsAt.Before(earliestStartsAt) {
			earliestStartsAt = interval.StartsAt
		}
	}

	denominatorStart := windowStart
	if earliestStartsAt.After(denominatorStart) {
		denominatorStart = earliestStartsAt
	}

	total := asOf.Sub(denominatorStart)
	if total <= 0 {
		return 0, false
	}

	var downtime time.Duration
	for _, interval := range intervals {
		if interval.Status != "outage" {
			continue
		}

		start := interval.StartsAt
		if start.Before(denominatorStart) {
			start = denominatorStart
		}
		end := asOf
		if interval.EndsAt != nil && interval.EndsAt.Before(asOf) {
			end = *interval.EndsAt
		}
		if end.After(asOf) {
			end = asOf
		}

		if overlap := end.Sub(start); overlap > 0 {
			downtime += overlap
		}
	}

	pct := (1 - float64(downtime)/float64(total)) * 100
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}

	return math.Floor(pct*10) / 10, true
}
