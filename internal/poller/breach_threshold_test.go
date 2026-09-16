package poller

import (
	"math"
	"testing"
)

// TestBreachBound_HighVolumeWindow_ExactValue pins the bound's arithmetic
// against a hand-computed value (spec BTR-01): target 99.5, 10000 requests,
// 3 sigmas -> 99.2884. A wrong formula (e.g. forgetting the sigmas factor,
// or using target as p0) lands far from this, so the assertion discriminates.
func TestBreachBound_HighVolumeWindow_ExactValue(t *testing.T) {
	got := breachBound(99.5, 10000, breachThresholdSigmas)

	if math.Abs(got-99.2884) > 0.001 {
		t.Errorf("breachBound(99.5, 10000, 3) = %v, want ~99.2884", got)
	}
}

// TestBreachBound_ConvergesToTargetAsVolumeGrows covers the spec's first
// edge case: with a large enough sample the band collapses onto the SLO's
// own target, so a sustained deviation below target still breaches.
func TestBreachBound_ConvergesToTargetAsVolumeGrows(t *testing.T) {
	got := breachBound(99.5, 1_000_000, breachThresholdSigmas)

	if math.Abs(got-99.5) > 0.1 {
		t.Errorf("breachBound(99.5, 1000000, 3) = %v, want within 0.1 of 99.5", got)
	}
}

// TestBreachBound_NeverExceedsTarget covers the invariant the design relies
// on: the band is always at least as lenient as the SLO's own target, and
// a smaller sample widens it (strictly lower bound).
func TestBreachBound_NeverExceedsTarget(t *testing.T) {
	for _, target := range []float64{90, 99, 99.5, 99.9, 99.99} {
		for _, n := range []int64{10, 100, 10000} {
			got := breachBound(target, n, breachThresholdSigmas)
			if got > target {
				t.Errorf("breachBound(%v, %d, 3) = %v, want <= target %v", target, n, got, target)
			}
		}

		small := breachBound(target, 10, breachThresholdSigmas)
		large := breachBound(target, 10000, breachThresholdSigmas)
		if small >= large {
			t.Errorf("breachBound(%v, 10) = %v, want strictly below breachBound(%v, 10000) = %v (smaller sample, wider band)", target, small, target, large)
		}
	}
}

// TestBreachBound_NonPositiveRequestCount_ReturnsTarget covers the guard:
// no usable sample means no widening, never a division by zero.
func TestBreachBound_NonPositiveRequestCount_ReturnsTarget(t *testing.T) {
	for _, n := range []int64{0, -1} {
		if got := breachBound(99.5, n, breachThresholdSigmas); got != 99.5 {
			t.Errorf("breachBound(99.5, %d, 3) = %v, want target 99.5", n, got)
		}
	}
}

// TestBreachBound_TargetOutsideRange_IsFinite covers the total-function
// requirement: malformed targets must not panic or produce NaN/Inf.
func TestBreachBound_TargetOutsideRange_IsFinite(t *testing.T) {
	for _, target := range []float64{0, -5, 150} {
		got := breachBound(target, 10000, breachThresholdSigmas)
		if math.IsNaN(got) || math.IsInf(got, 0) {
			t.Errorf("breachBound(%v, 10000, 3) = %v, want a finite value", target, got)
		}
	}
}

// TestBreachBound_ZeroSigmas_EqualsTarget is the degenerate check: with no
// tolerance the bound is exactly the SLO's target, proving the sigmas term
// is what supplies the leniency.
func TestBreachBound_ZeroSigmas_EqualsTarget(t *testing.T) {
	if got := breachBound(99.5, 10000, 0); got != 99.5 {
		t.Errorf("breachBound(99.5, 10000, 0) = %v, want 99.5", got)
	}
}
