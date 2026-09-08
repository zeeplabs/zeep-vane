package history

import (
	"testing"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

func mustLoadSaoPaulo(t *testing.T) *time.Location {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatalf("LoadLocation(America/Sao_Paulo) failed: %v", err)
	}
	return loc
}

func closedInterval(status string, startsAt, endsAt time.Time) db.StatusInterval {
	e := endsAt
	return db.StatusInterval{Status: status, StartsAt: startsAt, EndsAt: &e}
}

func openInterval(status string, startsAt time.Time) db.StatusInterval {
	return db.StatusInterval{Status: status, StartsAt: startsAt, EndsAt: nil}
}

// Regression test (design.md Risks & Concerns): calling BuildBuckets with
// bucketWidth=1h must produce byte-identical output (same Start/Status per
// bucket) to the old BuildHourly's behavior, for every fixture this file
// used to exercise against BuildHourly. Written and passing before any
// 6h/24h-specific test is added, so a generalization mistake can't hide
// behind a passing wide-bucket test.
func TestBuildBuckets_OneHourWidth_MatchesOldBuildHourlyBehavior(t *testing.T) {
	loc := mustLoadSaoPaulo(t)

	t.Run("ReturnsWindowHoursBucketsOldestFirst", func(t *testing.T) {
		now := time.Date(2026, 8, 24, 14, 37, 0, 0, loc)

		buckets := BuildBuckets(nil, now, now, loc, 24, time.Hour)

		if len(buckets) != 24 {
			t.Fatalf("len(buckets) = %d, want 24", len(buckets))
		}
		wantFirst := time.Date(2026, 8, 23, 15, 0, 0, 0, loc)
		wantLast := time.Date(2026, 8, 24, 14, 0, 0, 0, loc)
		if !buckets[0].Start.Equal(wantFirst) {
			t.Errorf("buckets[0].Start = %v, want %v", buckets[0].Start, wantFirst)
		}
		if !buckets[23].Start.Equal(wantLast) {
			t.Errorf("buckets[23].Start = %v, want %v", buckets[23].Start, wantLast)
		}
		for i := 1; i < len(buckets); i++ {
			if !buckets[i].Start.After(buckets[i-1].Start) {
				t.Fatalf("buckets not strictly increasing at index %d", i)
			}
		}
	})

	t.Run("WorstStatusWinsWithinBucket", func(t *testing.T) {
		now := time.Date(2026, 8, 24, 10, 0, 0, 0, loc)
		hourStart := time.Date(2026, 8, 24, 9, 0, 0, 0, loc)

		intervals := []db.StatusInterval{
			closedInterval("operational", hourStart, hourStart.Add(55*time.Minute)),
			closedInterval("outage", hourStart.Add(55*time.Minute), hourStart.Add(60*time.Minute)),
		}

		buckets := BuildBuckets(intervals, now, now, loc, 24, time.Hour)

		if got := buckets[22].Status; got != "outage" {
			t.Errorf("buckets[22].Status = %q, want %q (worst status in the hour wins)", got, "outage")
		}
	})

	t.Run("PriorityOrder_OutageBeatsDegradedBeatsOperational", func(t *testing.T) {
		now := time.Date(2026, 8, 24, 10, 0, 0, 0, loc)
		hourStart := time.Date(2026, 8, 24, 9, 0, 0, 0, loc)

		intervals := []db.StatusInterval{
			closedInterval("degraded", hourStart.Add(20*time.Minute), hourStart.Add(40*time.Minute)),
			closedInterval("operational", hourStart, hourStart.Add(20*time.Minute)),
			closedInterval("outage", hourStart.Add(40*time.Minute), hourStart.Add(41*time.Minute)),
			closedInterval("operational", hourStart.Add(41*time.Minute), hourStart.Add(60*time.Minute)),
		}

		buckets := BuildBuckets(intervals, now, now, loc, 24, time.Hour)

		if got := buckets[22].Status; got != "outage" {
			t.Errorf("buckets[22].Status = %q, want %q", got, "outage")
		}

		// Without the outage interval, degraded should beat operational.
		intervalsNoOutage := []db.StatusInterval{
			closedInterval("degraded", hourStart.Add(20*time.Minute), hourStart.Add(40*time.Minute)),
			closedInterval("operational", hourStart, hourStart.Add(20*time.Minute)),
			closedInterval("operational", hourStart.Add(40*time.Minute), hourStart.Add(60*time.Minute)),
		}
		buckets2 := BuildBuckets(intervalsNoOutage, now, now, loc, 24, time.Hour)
		if got := buckets2[22].Status; got != "degraded" {
			t.Errorf("buckets[22].Status = %q, want %q", got, "degraded")
		}
	})

	t.Run("NoOverlappingInterval_ResolvesToNoData", func(t *testing.T) {
		now := time.Date(2026, 8, 24, 14, 0, 0, 0, loc)

		buckets := BuildBuckets(nil, now, now, loc, 24, time.Hour)

		for i, b := range buckets {
			if b.Status != NoData {
				t.Errorf("buckets[%d].Status = %q, want %q", i, b.Status, NoData)
			}
		}
	})

	t.Run("IntervalSpanningMultipleBuckets_CoversEveryOverlappedBucket", func(t *testing.T) {
		now := time.Date(2026, 8, 24, 12, 0, 0, 0, loc)

		// Spans hours 9, 10, 11 (starts mid-hour-9, ends mid-hour-11).
		start := time.Date(2026, 8, 24, 9, 30, 0, 0, loc)
		end := time.Date(2026, 8, 24, 11, 30, 0, 0, loc)
		intervals := []db.StatusInterval{closedInterval("outage", start, end)}

		buckets := BuildBuckets(intervals, now, now, loc, 24, time.Hour)

		// index 23 = hour 11 (current), 22 = hour 11? recompute: now=12:00 so
		// current bucket (index 23) covers [12:00,13:00). hour 11 is index 22,
		// hour 10 is index 21, hour 9 is index 20.
		if got := buckets[20].Status; got != "outage" {
			t.Errorf("hour 9 bucket = %q, want %q", got, "outage")
		}
		if got := buckets[21].Status; got != "outage" {
			t.Errorf("hour 10 bucket = %q, want %q", got, "outage")
		}
		if got := buckets[22].Status; got != "outage" {
			t.Errorf("hour 11 bucket = %q, want %q", got, "outage")
		}
		if got := buckets[23].Status; got != NoData {
			t.Errorf("hour 12 (current) bucket = %q, want %q (interval ended before it)", got, NoData)
		}
	})

	t.Run("OpenIntervalCoversUpToNow", func(t *testing.T) {
		now := time.Date(2026, 8, 24, 14, 5, 0, 0, loc)

		intervals := []db.StatusInterval{
			openInterval("outage", time.Date(2026, 8, 24, 13, 30, 0, 0, loc)),
		}

		buckets := BuildBuckets(intervals, now, now, loc, 24, time.Hour)

		if got := buckets[22].Status; got != "outage" {
			t.Errorf("hour 13 bucket = %q, want %q", got, "outage")
		}
		if got := buckets[23].Status; got != "outage" {
			t.Errorf("current (hour 14) bucket = %q, want %q (open interval covers up to now)", got, "outage")
		}
	})

	t.Run("OpenIntervalClampsToAsOf_NotNow", func(t *testing.T) {
		now := time.Date(2026, 8, 24, 14, 5, 0, 0, loc)
		asOf := time.Date(2026, 8, 24, 11, 30, 0, 0, loc) // poller stalled ~2.5h ago

		intervals := []db.StatusInterval{
			openInterval("outage", time.Date(2026, 8, 24, 10, 0, 0, 0, loc)),
		}

		buckets := BuildBuckets(intervals, now, asOf, loc, 24, time.Hour)

		if got := buckets[23].Status; got != NoData {
			t.Errorf("current (hour 14) bucket = %q, want %q (asOf is 11:30, not now)", got, NoData)
		}
		if got := buckets[21].Status; got != NoData {
			t.Errorf("hour 12 bucket = %q, want %q (asOf 11:30 never reaches hour 12)", got, NoData)
		}
		if got := buckets[20].Status; got != "outage" {
			t.Errorf("hour 11 bucket = %q, want %q (interval still covers up to asOf)", got, "outage")
		}
		if len(buckets) != 24 {
			t.Fatalf("len(buckets) = %d, want 24 (window still anchored on now, not asOf)", len(buckets))
		}
		wantLast := time.Date(2026, 8, 24, 14, 0, 0, 0, loc)
		if !buckets[23].Start.Equal(wantLast) {
			t.Errorf("buckets[23].Start = %v, want %v (window unaffected by asOf)", buckets[23].Start, wantLast)
		}
	})

	t.Run("IntervalsOutsideWindowAreIgnored", func(t *testing.T) {
		now := time.Date(2026, 8, 24, 14, 0, 0, 0, loc)

		intervals := []db.StatusInterval{
			closedInterval("outage", time.Date(2026, 8, 20, 12, 0, 0, 0, loc), time.Date(2026, 8, 20, 13, 0, 0, 0, loc)),
		}

		buckets := BuildBuckets(intervals, now, now, loc, 24, time.Hour)

		for i, b := range buckets {
			if b.Status != NoData {
				t.Errorf("buckets[%d].Status = %q, want %q (out-of-window interval must not leak in)", i, b.Status, NoData)
			}
		}
	})
}

// bucketWidth=6h must align bucket starts to 00:00/06:00/12:00/18:00 local
// time, not an arbitrary offset back from now.
func TestBuildBuckets_SixHourWidth_AlignsToSixHourBoundaries(t *testing.T) {
	loc := mustLoadSaoPaulo(t)
	// 14:37 falls inside the [12:00,18:00) span, so the current bucket must
	// start at 12:00, not 14:37 or 14:00.
	now := time.Date(2026, 8, 24, 14, 37, 0, 0, loc)

	buckets := BuildBuckets(nil, now, now, loc, 4, 6*time.Hour)

	if len(buckets) != 4 {
		t.Fatalf("len(buckets) = %d, want 4", len(buckets))
	}
	wantStarts := []time.Time{
		time.Date(2026, 8, 23, 18, 0, 0, 0, loc),
		time.Date(2026, 8, 24, 0, 0, 0, 0, loc),
		time.Date(2026, 8, 24, 6, 0, 0, 0, loc),
		time.Date(2026, 8, 24, 12, 0, 0, 0, loc),
	}
	for i, want := range wantStarts {
		if !buckets[i].Start.Equal(want) {
			t.Errorf("buckets[%d].Start = %v, want %v", i, buckets[i].Start, want)
		}
	}
}

// bucketWidth=24h must align the "today" bucket to local midnight and
// correctly represent a partial day, mirroring how the old code represented
// a partial current hour.
func TestBuildBuckets_TwentyFourHourWidth_AlignsToLocalMidnightAndIsPartial(t *testing.T) {
	loc := mustLoadSaoPaulo(t)
	now := time.Date(2026, 8, 24, 9, 15, 0, 0, loc)

	buckets := BuildBuckets(nil, now, now, loc, 3, 24*time.Hour)

	if len(buckets) != 3 {
		t.Fatalf("len(buckets) = %d, want 3", len(buckets))
	}
	wantStarts := []time.Time{
		time.Date(2026, 8, 22, 0, 0, 0, 0, loc),
		time.Date(2026, 8, 23, 0, 0, 0, 0, loc),
		time.Date(2026, 8, 24, 0, 0, 0, 0, loc), // "today", partial (now is 09:15)
	}
	for i, want := range wantStarts {
		if !buckets[i].Start.Equal(want) {
			t.Errorf("buckets[%d].Start = %v, want %v", i, buckets[i].Start, want)
		}
	}

	// An interval covering only up to 09:00 today still overlaps the
	// partial "today" bucket, same treatment BuildHourly gave a partial
	// current hour.
	intervals := []db.StatusInterval{
		closedInterval("outage", time.Date(2026, 8, 24, 8, 0, 0, 0, loc), time.Date(2026, 8, 24, 9, 0, 0, 0, loc)),
	}
	withData := BuildBuckets(intervals, now, now, loc, 3, 24*time.Hour)
	if got := withData[2].Status; got != "outage" {
		t.Errorf("today (partial) bucket = %q, want %q", got, "outage")
	}
}

// Generalizes the existing 1h-width "spans multiple buckets" test: an
// interval spanning multiple wide buckets still contributes its status to
// every bucket it overlaps (worst-status-wins), at bucketWidth=6h.
func TestBuildBuckets_IntervalSpanningMultipleWideBuckets_CoversEveryOverlappedBucket(t *testing.T) {
	loc := mustLoadSaoPaulo(t)
	now := time.Date(2026, 8, 24, 20, 0, 0, 0, loc)

	// Buckets (6h wide, bucketCount=4) span local midnight to midnight:
	// [00:00-06:00, 06:00-12:00, 12:00-18:00, 18:00-24:00). Interval spans
	// from 05:00 (inside bucket 0) to 16:00 (inside bucket 2), crossing 3
	// of the 4 buckets, leaving the last (18:00-24:00) untouched.
	start := time.Date(2026, 8, 24, 5, 0, 0, 0, loc)
	end := time.Date(2026, 8, 24, 16, 0, 0, 0, loc)
	intervals := []db.StatusInterval{closedInterval("outage", start, end)}

	buckets := BuildBuckets(intervals, now, now, loc, 4, 6*time.Hour)

	wantStarts := []time.Time{
		time.Date(2026, 8, 24, 0, 0, 0, 0, loc),
		time.Date(2026, 8, 24, 6, 0, 0, 0, loc),
		time.Date(2026, 8, 24, 12, 0, 0, 0, loc),
		time.Date(2026, 8, 24, 18, 0, 0, 0, loc),
	}
	for i, want := range wantStarts {
		if !buckets[i].Start.Equal(want) {
			t.Fatalf("buckets[%d].Start = %v, want %v (test setup assumption broken)", i, buckets[i].Start, want)
		}
	}

	if got := buckets[0].Status; got != "outage" {
		t.Errorf("buckets[0] (00:00-06:00) = %q, want %q (interval overlaps 05:00-06:00)", got, "outage")
	}
	if got := buckets[1].Status; got != "outage" {
		t.Errorf("buckets[1] (06:00-12:00) = %q, want %q (interval fully covers this bucket)", got, "outage")
	}
	if got := buckets[2].Status; got != "outage" {
		t.Errorf("buckets[2] (12:00-18:00) = %q, want %q (interval overlaps 12:00-16:00)", got, "outage")
	}
	if got := buckets[3].Status; got != NoData {
		t.Errorf("buckets[3] (18:00-24:00) = %q, want %q (interval ended at 16:00, before this bucket)", got, NoData)
	}
}

// no_data still renders for a bucket with zero overlapping intervals, at a
// non-default bucket width (TRS-06).
func TestBuildBuckets_NoOverlappingInterval_ResolvesToNoData_AtWideBucketWidth(t *testing.T) {
	loc := mustLoadSaoPaulo(t)
	now := time.Date(2026, 8, 24, 14, 0, 0, 0, loc)

	buckets := BuildBuckets(nil, now, now, loc, 90, 24*time.Hour)

	if len(buckets) != 90 {
		t.Fatalf("len(buckets) = %d, want 90", len(buckets))
	}
	for i, b := range buckets {
		if b.Status != NoData {
			t.Errorf("buckets[%d].Status = %q, want %q", i, b.Status, NoData)
		}
	}
}
