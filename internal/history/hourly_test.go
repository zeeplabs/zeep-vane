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

// SHU-08: exactly windowHours buckets, oldest first, current hour last -
// unchanged contract from the snapshot-based version.
func TestBuildHourly_ReturnsWindowHoursBucketsOldestFirst(t *testing.T) {
	loc := mustLoadSaoPaulo(t)
	now := time.Date(2026, 8, 24, 14, 37, 0, 0, loc)

	buckets := BuildHourly(nil, now, loc, 24)

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
}

// SHU-06 core case: an hour containing both operational (55 min) and outage
// (5 min) resolves to outage, not the last-observed status.
func TestBuildHourly_WorstStatusWinsWithinBucket(t *testing.T) {
	loc := mustLoadSaoPaulo(t)
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, loc)
	hourStart := time.Date(2026, 8, 24, 9, 0, 0, 0, loc)

	intervals := []db.StatusInterval{
		closedInterval("operational", hourStart, hourStart.Add(55*time.Minute)),
		closedInterval("outage", hourStart.Add(55*time.Minute), hourStart.Add(60*time.Minute)),
	}

	buckets := BuildHourly(intervals, now, loc, 24)

	if got := buckets[22].Status; got != "outage" {
		t.Errorf("buckets[22].Status = %q, want %q (worst status in the hour wins)", got, "outage")
	}
}

// SHU-06: priority order is outage > degraded > operational, regardless of
// which interval is listed first.
func TestBuildHourly_PriorityOrder_OutageBeatsDegradedBeatsOperational(t *testing.T) {
	loc := mustLoadSaoPaulo(t)
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, loc)
	hourStart := time.Date(2026, 8, 24, 9, 0, 0, 0, loc)

	intervals := []db.StatusInterval{
		closedInterval("degraded", hourStart.Add(20*time.Minute), hourStart.Add(40*time.Minute)),
		closedInterval("operational", hourStart, hourStart.Add(20*time.Minute)),
		closedInterval("outage", hourStart.Add(40*time.Minute), hourStart.Add(41*time.Minute)),
		closedInterval("operational", hourStart.Add(41*time.Minute), hourStart.Add(60*time.Minute)),
	}

	buckets := BuildHourly(intervals, now, loc, 24)

	if got := buckets[22].Status; got != "outage" {
		t.Errorf("buckets[22].Status = %q, want %q", got, "outage")
	}

	// Without the outage interval, degraded should beat operational.
	intervalsNoOutage := []db.StatusInterval{
		closedInterval("degraded", hourStart.Add(20*time.Minute), hourStart.Add(40*time.Minute)),
		closedInterval("operational", hourStart, hourStart.Add(20*time.Minute)),
		closedInterval("operational", hourStart.Add(40*time.Minute), hourStart.Add(60*time.Minute)),
	}
	buckets2 := BuildHourly(intervalsNoOutage, now, loc, 24)
	if got := buckets2[22].Status; got != "degraded" {
		t.Errorf("buckets[22].Status = %q, want %q", got, "degraded")
	}
}

// SHU-07: a bucket with no overlapping interval resolves to NoData.
func TestBuildHourly_NoOverlappingInterval_ResolvesToNoData(t *testing.T) {
	loc := mustLoadSaoPaulo(t)
	now := time.Date(2026, 8, 24, 14, 0, 0, 0, loc)

	buckets := BuildHourly(nil, now, loc, 24)

	for i, b := range buckets {
		if b.Status != NoData {
			t.Errorf("buckets[%d].Status = %q, want %q", i, b.Status, NoData)
		}
	}
}

// SHU-09: an interval spanning multiple bucket boundaries contributes its
// status to every bucket it overlaps, not just the one containing StartsAt.
func TestBuildHourly_IntervalSpanningMultipleBuckets_CoversEveryOverlappedBucket(t *testing.T) {
	loc := mustLoadSaoPaulo(t)
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, loc)

	// Spans hours 9, 10, 11 (starts mid-hour-9, ends mid-hour-11).
	start := time.Date(2026, 8, 24, 9, 30, 0, 0, loc)
	end := time.Date(2026, 8, 24, 11, 30, 0, 0, loc)
	intervals := []db.StatusInterval{closedInterval("outage", start, end)}

	buckets := BuildHourly(intervals, now, loc, 24)

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
}

// An open-ended interval (EndsAt nil) still overlapping is counted as
// covering up through now/the current bucket.
func TestBuildHourly_OpenIntervalCoversUpToNow(t *testing.T) {
	loc := mustLoadSaoPaulo(t)
	now := time.Date(2026, 8, 24, 14, 5, 0, 0, loc)

	intervals := []db.StatusInterval{
		openInterval("outage", time.Date(2026, 8, 24, 13, 30, 0, 0, loc)),
	}

	buckets := BuildHourly(intervals, now, loc, 24)

	if got := buckets[22].Status; got != "outage" {
		t.Errorf("hour 13 bucket = %q, want %q", got, "outage")
	}
	if got := buckets[23].Status; got != "outage" {
		t.Errorf("current (hour 14) bucket = %q, want %q (open interval covers up to now)", got, "outage")
	}
}

// Intervals entirely outside the window are ignored, not mis-bucketed.
func TestBuildHourly_IntervalsOutsideWindowAreIgnored(t *testing.T) {
	loc := mustLoadSaoPaulo(t)
	now := time.Date(2026, 8, 24, 14, 0, 0, 0, loc)

	intervals := []db.StatusInterval{
		closedInterval("outage", time.Date(2026, 8, 20, 12, 0, 0, 0, loc), time.Date(2026, 8, 20, 13, 0, 0, 0, loc)),
	}

	buckets := BuildHourly(intervals, now, loc, 24)

	for i, b := range buckets {
		if b.Status != NoData {
			t.Errorf("buckets[%d].Status = %q, want %q (out-of-window interval must not leak in)", i, b.Status, NoData)
		}
	}
}
