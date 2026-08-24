// Package history builds hourly status-bucket summaries for the public
// status page from raw status_intervals rows. It is deliberately
// dependency-free (no DB, no HTTP) so the bucketing rule is unit-testable
// on its own.
package history

import (
	"time"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// NoData is the status of an hourly bucket with no interval overlapping it.
const NoData = "no_data"

// statusPriority ranks statuses from worst to best for bucket resolution:
// an hour that touched a worse status anywhere in its span reports that
// worse status, even if a better one was observed later in the same hour.
var statusPriority = map[string]int{
	"outage":      3,
	"degraded":    2,
	"operational": 1,
}

// HourlyBucket is one hour's resolved status in a service's uptime history.
type HourlyBucket struct {
	Start  time.Time
	Status string
}

// BuildHourly returns exactly windowHours hourly buckets covering
// [now-windowHours+1h, now], one per local hour in loc, oldest first, with
// the current (possibly partial) local hour as the last bucket.
//
// Each bucket's status is the highest-priority status (outage > degraded >
// operational) among every interval in intervals that overlaps that
// bucket's [start, start+1h) span - an interval spanning multiple buckets
// contributes its status to every bucket it overlaps, not only the one
// containing its StartsAt. An open interval (EndsAt nil) is treated as
// still overlapping up through now. A bucket with no overlapping interval
// is NoData. intervals need not be pre-sorted or pre-filtered to the
// window - only intervals overlapping a bucket affect it, everything else
// is ignored.
func BuildHourly(intervals []db.StatusInterval, now time.Time, loc *time.Location, windowHours int) []HourlyBucket {
	nowLocal := now.In(loc)
	currentStart := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), nowLocal.Hour(), 0, 0, 0, loc)
	leftmostStart := currentStart.Add(-time.Duration(windowHours-1) * time.Hour)

	buckets := make([]HourlyBucket, windowHours)
	for i := range buckets {
		buckets[i] = HourlyBucket{
			Start:  leftmostStart.Add(time.Duration(i) * time.Hour),
			Status: NoData,
		}
	}

	for _, interval := range intervals {
		startLocal := interval.StartsAt.In(loc)
		endLocal := now
		if interval.EndsAt != nil {
			endLocal = *interval.EndsAt
		}
		endLocal = endLocal.In(loc)

		if !endLocal.After(startLocal) {
			continue
		}

		firstIndex := int(startLocal.Sub(leftmostStart) / time.Hour)
		if firstIndex < 0 {
			firstIndex = 0
		}

		endOffset := endLocal.Sub(leftmostStart)
		lastIndex := int(endOffset / time.Hour)
		if endOffset%time.Hour == 0 {
			// endLocal lands exactly on a bucket boundary: the bucket that
			// starts there is not overlapped (the interval already ended).
			lastIndex--
		}
		if lastIndex >= windowHours {
			lastIndex = windowHours - 1
		}

		for i := firstIndex; i <= lastIndex; i++ {
			if i < 0 || i >= windowHours {
				continue
			}
			if statusPriority[interval.Status] > statusPriority[buckets[i].Status] {
				buckets[i].Status = interval.Status
			}
		}
	}

	return buckets
}
