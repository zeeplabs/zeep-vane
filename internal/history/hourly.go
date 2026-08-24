// Package history builds hourly status-bucket summaries for the public
// status page from raw status_snapshots rows. It is deliberately
// dependency-free (no DB, no HTTP) so the bucketing rule is unit-testable
// on its own.
package history

import (
	"time"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// NoData is the status of an hourly bucket with no snapshot in its window.
const NoData = "no_data"

// HourlyBucket is one hour's resolved status in a service's uptime history.
type HourlyBucket struct {
	Start  time.Time
	Status string
}

// BuildHourly returns exactly windowHours hourly buckets covering
// [now-windowHours+1h, now], one per local hour in loc, oldest first, with
// the current (possibly partial) local hour as the last bucket.
//
// Each bucket's status is the status of the snapshot with the latest
// FetchedAt that falls within that bucket's [start, end) window; a bucket
// with no snapshot in its window is NoData. snapshots need not be
// pre-sorted or pre-filtered to the window - only snapshots that land
// inside a bucket are used, everything else is ignored.
func BuildHourly(snapshots []db.StatusSnapshot, now time.Time, loc *time.Location, windowHours int) []HourlyBucket {
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

	latestSeen := make([]time.Time, windowHours)

	for _, snapshot := range snapshots {
		fetchedAtLocal := snapshot.FetchedAt.In(loc)
		index := int(fetchedAtLocal.Sub(leftmostStart) / time.Hour)
		if index < 0 || index >= windowHours {
			continue
		}
		if buckets[index].Status == NoData || fetchedAtLocal.After(latestSeen[index]) {
			buckets[index].Status = snapshot.Status
			latestSeen[index] = fetchedAtLocal
		}
	}

	return buckets
}
