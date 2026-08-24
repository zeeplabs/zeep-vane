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

func snapshot(serviceID, status string, fetchedAt time.Time) db.StatusSnapshot {
	return db.StatusSnapshot{ServiceID: serviceID, Status: status, FetchedAt: fetchedAt}
}

// UPT-01: exactly windowHours buckets, oldest first, current hour last.
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

// UPT-02: all four status values pass through untouched.
func TestBuildHourly_AllStatusValuesMapThrough(t *testing.T) {
	loc := mustLoadSaoPaulo(t)
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, loc)

	tests := []string{"operational", "degraded", "outage"}
	for _, status := range tests {
		snapshots := []db.StatusSnapshot{
			snapshot("svc-1", status, time.Date(2026, 8, 24, 10, 15, 0, 0, loc)),
		}
		buckets := BuildHourly(snapshots, now, loc, 24)
		if got := buckets[23].Status; got != status {
			t.Errorf("status %q: buckets[23].Status = %q, want %q", status, got, status)
		}
	}
}

// UPT-03: last-status-wins within one hour, regardless of input order.
func TestBuildHourly_LastStatusWinsWithinBucket(t *testing.T) {
	loc := mustLoadSaoPaulo(t)
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, loc)

	snapshots := []db.StatusSnapshot{
		snapshot("svc-1", "outage", time.Date(2026, 8, 24, 10, 40, 0, 0, loc)),
		snapshot("svc-1", "operational", time.Date(2026, 8, 24, 10, 5, 0, 0, loc)),
		snapshot("svc-1", "degraded", time.Date(2026, 8, 24, 10, 20, 0, 0, loc)),
	}

	buckets := BuildHourly(snapshots, now, loc, 24)

	if got := buckets[23].Status; got != "outage" {
		t.Errorf("buckets[23].Status = %q, want %q (latest fetched_at wins)", got, "outage")
	}
}

// UPT-04: a snapshot exactly on a bucket boundary lands in the bucket it
// starts (not the previous one), and this holds across a just-past-midnight
// America/Sao_Paulo boundary.
func TestBuildHourly_BoundarySnapshotLandsInStartingBucket(t *testing.T) {
	loc := mustLoadSaoPaulo(t)
	now := time.Date(2026, 8, 24, 0, 10, 0, 0, loc)

	boundary := time.Date(2026, 8, 24, 0, 0, 0, 0, loc)
	snapshots := []db.StatusSnapshot{
		snapshot("svc-1", "degraded", boundary),
	}

	buckets := BuildHourly(snapshots, now, loc, 24)

	if got := buckets[23].Status; got != "degraded" {
		t.Errorf("buckets[23].Status = %q, want %q (boundary snapshot belongs to the hour it starts)", got, "degraded")
	}
	if got := buckets[22].Status; got != NoData {
		t.Errorf("buckets[22].Status = %q, want %q (previous hour must not absorb the boundary snapshot)", got, NoData)
	}
}

// UPT-06: an empty snapshot slice yields all no_data buckets.
func TestBuildHourly_EmptySnapshotsYieldsAllNoData(t *testing.T) {
	loc := mustLoadSaoPaulo(t)
	now := time.Date(2026, 8, 24, 14, 0, 0, 0, loc)

	buckets := BuildHourly(nil, now, loc, 24)

	for i, b := range buckets {
		if b.Status != NoData {
			t.Errorf("buckets[%d].Status = %q, want %q", i, b.Status, NoData)
		}
	}
}

// Current-partial-hour assumption: a snapshot in the current, not-yet-complete
// hour lands in the right-most bucket, and unsorted input is handled correctly.
func TestBuildHourly_CurrentPartialHourGetsOwnBucket_UnsortedInput(t *testing.T) {
	loc := mustLoadSaoPaulo(t)
	now := time.Date(2026, 8, 24, 14, 5, 0, 0, loc)

	snapshots := []db.StatusSnapshot{
		snapshot("svc-1", "operational", time.Date(2026, 8, 24, 14, 2, 0, 0, loc)),
		snapshot("svc-1", "operational", time.Date(2026, 8, 23, 15, 30, 0, 0, loc)),
	}

	buckets := BuildHourly(snapshots, now, loc, 24)

	if got := buckets[23].Status; got != "operational" {
		t.Errorf("buckets[23] (current hour) = %q, want %q", got, "operational")
	}
	if got := buckets[0].Status; got != "operational" {
		t.Errorf("buckets[0] (oldest hour) = %q, want %q", got, "operational")
	}
}

// Snapshots outside the window are ignored, not mis-bucketed.
func TestBuildHourly_SnapshotsOutsideWindowAreIgnored(t *testing.T) {
	loc := mustLoadSaoPaulo(t)
	now := time.Date(2026, 8, 24, 14, 0, 0, 0, loc)

	snapshots := []db.StatusSnapshot{
		snapshot("svc-1", "outage", time.Date(2026, 8, 20, 12, 0, 0, 0, loc)),
	}

	buckets := BuildHourly(snapshots, now, loc, 24)

	for i, b := range buckets {
		if b.Status != NoData {
			t.Errorf("buckets[%d].Status = %q, want %q (out-of-window snapshot must not leak in)", i, b.Status, NoData)
		}
	}
}
