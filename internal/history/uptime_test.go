package history

import (
	"testing"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// SHU-10/11: a 24h window with exactly 6h of outage and the rest
// operational returns 75.0.
func TestUptimePercent_SixHoursOutageInTwentyFourHourWindow_Returns75(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	windowStart := now.Add(-24 * time.Hour)

	intervals := []db.StatusInterval{
		closedInterval("operational", windowStart, windowStart.Add(6*time.Hour)),
		closedInterval("outage", windowStart.Add(6*time.Hour), windowStart.Add(12*time.Hour)),
		closedInterval("operational", windowStart.Add(12*time.Hour), now),
	}

	percent, ok := UptimePercent(intervals, windowStart, now)
	if !ok {
		t.Fatalf("UptimePercent() ok = false, want true")
	}
	if percent != 75.0 {
		t.Errorf("UptimePercent() = %v, want 75.0", percent)
	}
}

// SHU-12/13: a service whose earliest interval starts 2h into the window
// uses a 2h denominator, not the full 24h.
func TestUptimePercent_ServiceNewerThanWindow_UsesClippedDenominator(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	windowStart := now.Add(-24 * time.Hour)
	earliestStart := now.Add(-2 * time.Hour)

	intervals := []db.StatusInterval{
		openInterval("operational", earliestStart),
	}

	percent, ok := UptimePercent(intervals, windowStart, now)
	if !ok {
		t.Fatalf("UptimePercent() ok = false, want true")
	}
	if percent != 100.0 {
		t.Errorf("UptimePercent() = %v, want 100.0", percent)
	}
}

// SHU-15: zero intervals reports undefined.
func TestUptimePercent_ZeroIntervals_ReturnsUndefined(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	windowStart := now.Add(-24 * time.Hour)

	_, ok := UptimePercent(nil, windowStart, now)
	if ok {
		t.Errorf("UptimePercent() ok = true, want false for zero intervals")
	}
}

// SHU-10: degraded intervals do not count as downtime, only outage does.
func TestUptimePercent_DegradedDoesNotCountAsDowntime(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	windowStart := now.Add(-24 * time.Hour)

	intervals := []db.StatusInterval{
		closedInterval("degraded", windowStart, windowStart.Add(12*time.Hour)),
		closedInterval("operational", windowStart.Add(12*time.Hour), now),
	}

	percent, ok := UptimePercent(intervals, windowStart, now)
	if !ok {
		t.Fatalf("UptimePercent() ok = false, want true")
	}
	if percent != 100.0 {
		t.Errorf("UptimePercent() = %v, want 100.0 (degraded is not downtime)", percent)
	}
}

// SHU-14: a pathological case that would compute outside [0, 100] is
// clamped before rounding - here an outage interval extending beyond `now`
// (should not happen in practice, but guards the invariant) must not push
// downtime past 100% of the window.
func TestUptimePercent_ClampsToZeroHundredRange(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	windowStart := now.Add(-24 * time.Hour)

	intervals := []db.StatusInterval{
		closedInterval("outage", windowStart.Add(-10*time.Hour), now.Add(10*time.Hour)),
	}

	percent, ok := UptimePercent(intervals, windowStart, now)
	if !ok {
		t.Fatalf("UptimePercent() ok = false, want true")
	}
	if percent < 0 || percent > 100 {
		t.Fatalf("UptimePercent() = %v, want clamped to [0, 100]", percent)
	}
	if percent != 0.0 {
		t.Errorf("UptimePercent() = %v, want 0.0 (entire window is outage)", percent)
	}
}

// SHU-15: rounding always floors, never rounds up - 99.97% must floor to
// 99.9%, not round to 100.0%.
func TestUptimePercent_RoundingAlwaysFloors(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	windowStart := now.Add(-24 * time.Hour)

	// 24h window = 1440 minutes. 0.03% downtime ~= 0.432 min ~= 25.92s.
	// Use exactly 25 seconds of outage: 25/86400 = 0.02894% downtime,
	// uptime = 99.97106% which must floor to 99.9, not round to 100.0.
	outageStart := windowStart.Add(5 * time.Hour)
	intervals := []db.StatusInterval{
		closedInterval("outage", outageStart, outageStart.Add(25*time.Second)),
		closedInterval("operational", windowStart, outageStart),
		closedInterval("operational", outageStart.Add(25*time.Second), now),
	}

	percent, ok := UptimePercent(intervals, windowStart, now)
	if !ok {
		t.Fatalf("UptimePercent() ok = false, want true")
	}
	if percent != 99.9 {
		t.Errorf("UptimePercent() = %v, want 99.9 (floored, never rounded up to 100.0)", percent)
	}
}

// Edge case: a clipped denominator of zero (service created essentially
// now) reports undefined, same as the zero-intervals case.
func TestUptimePercent_ZeroDenominator_ReturnsUndefined(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	windowStart := now.Add(-24 * time.Hour)

	intervals := []db.StatusInterval{
		openInterval("operational", now),
	}

	_, ok := UptimePercent(intervals, windowStart, now)
	if ok {
		t.Errorf("UptimePercent() ok = true, want false for a zero-length denominator")
	}
}
