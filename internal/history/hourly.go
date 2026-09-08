// Package history builds status-bucket summaries for the public status page
// from raw status_intervals rows. It is deliberately dependency-free (no
// DB, no HTTP) so the bucketing rule is unit-testable on its own.
package history

import (
	"time"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// NoData is the status of a bucket with no interval overlapping it.
const NoData = "no_data"

// statusPriority ranks statuses from worst to best for bucket resolution:
// a bucket that touched a worse status anywhere in its span reports that
// worse status, even if a better one was observed later in the same bucket.
var statusPriority = map[string]int{
	"outage":      3,
	"degraded":    2,
	"operational": 1,
}

// Bucket is one bucket's resolved status in a service's uptime history.
type Bucket struct {
	Start  time.Time
	Status string
}

// BuildBuckets returns exactly bucketCount buckets, each bucketWidth wide,
// covering [now-bucketCount*bucketWidth+bucketWidth, now], one per local
// bucketWidth-wide span in loc, oldest first, with the current (possibly
// partial) span as the last bucket.
//
// Each bucket's status is the highest-priority status (outage > degraded >
// operational) among every interval in intervals that overlaps that
// bucket's [start, start+bucketWidth) span - an interval spanning multiple
// buckets contributes its status to every bucket it overlaps, not only the
// one containing its StartsAt. An open interval (EndsAt nil) is treated as
// still overlapping up through asOf - not now (H7). now anchors which
// bucketCount buckets are shown (always the real, current window - a dead
// poller must not hide recent buckets from the chart); asOf is the last
// time the poller actually confirmed this status. When the poller is
// healthy, asOf is effectively now and nothing changes. When it has
// stalled, asOf stays in the past, so every bucket after it correctly
// resolves to NoData instead of an open interval's status being fabricated
// forward to now. A bucket with no overlapping interval is NoData.
// intervals need not be pre-sorted or pre-filtered to the window - only
// intervals overlapping a bucket affect it, everything else is ignored.
//
// The current bucket's start is aligned to local-time boundaries of
// bucketWidth rather than being offset from now: dayStart is local
// midnight, and currentStart is dayStart plus the largest whole multiple of
// bucketWidth not exceeding now's offset from dayStart. For bucketWidth=1h
// this always lands on the start of the current clock-hour (identical to
// the old hardcoded-hour behavior); for bucketWidth=6h it aligns to
// 00:00/06:00/12:00/18:00; for bucketWidth=24h it always lands on local
// midnight.
func BuildBuckets(intervals []db.StatusInterval, now, asOf time.Time, loc *time.Location, bucketCount int, bucketWidth time.Duration) []Bucket {
	nowLocal := now.In(loc)
	dayStart := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, loc)
	elapsed := nowLocal.Sub(dayStart)
	currentStart := dayStart.Add((elapsed / bucketWidth) * bucketWidth)
	leftmostStart := currentStart.Add(-time.Duration(bucketCount-1) * bucketWidth)

	buckets := make([]Bucket, bucketCount)
	for i := range buckets {
		buckets[i] = Bucket{
			Start:  leftmostStart.Add(time.Duration(i) * bucketWidth),
			Status: NoData,
		}
	}

	for _, interval := range intervals {
		startLocal := interval.StartsAt.In(loc)
		endLocal := asOf
		if interval.EndsAt != nil {
			endLocal = *interval.EndsAt
		}
		endLocal = endLocal.In(loc)

		if !endLocal.After(startLocal) {
			continue
		}

		firstIndex := int(startLocal.Sub(leftmostStart) / bucketWidth)
		if firstIndex < 0 {
			firstIndex = 0
		}

		endOffset := endLocal.Sub(leftmostStart)
		lastIndex := int(endOffset / bucketWidth)
		if endOffset%bucketWidth == 0 {
			// endLocal lands exactly on a bucket boundary: the bucket that
			// starts there is not overlapped (the interval already ended).
			lastIndex--
		}
		if lastIndex >= bucketCount {
			lastIndex = bucketCount - 1
		}

		for i := firstIndex; i <= lastIndex; i++ {
			if i < 0 || i >= bucketCount {
				continue
			}
			if statusPriority[interval.Status] > statusPriority[buckets[i].Status] {
				buckets[i].Status = interval.Status
			}
		}
	}

	return buckets
}
