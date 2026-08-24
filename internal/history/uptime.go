package history

import (
	"math"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// UptimePercent computes the uptime percentage over [windowStart, now] from
// intervals - the same overlapping-interval set BuildHourly consumes.
//
// The denominator starts at the later of windowStart or the earliest
// StartsAt among intervals (a service newer than the window uses
// now-earliestStartsAt as its denominator instead of the full window,
// SHU-12/13). Downtime is the summed overlap of every outage-status
// interval with [denominatorStart, now]; every other status counts as
// uptime. The result is clamped to [0, 100] before being floored to one
// decimal place (never rounded up).
//
// ok is false - "undefined, render a dash" - when intervals is empty or
// the clipped denominator is zero or negative.
func UptimePercent(intervals []db.StatusInterval, windowStart, now time.Time) (percent float64, ok bool) {
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

	total := now.Sub(denominatorStart)
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
		end := now
		if interval.EndsAt != nil && interval.EndsAt.Before(now) {
			end = *interval.EndsAt
		}
		if end.After(now) {
			end = now
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
